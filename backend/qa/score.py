#!/usr/bin/env python3
"""
Post-call audio quality scorer.

Reads a JSON payload from stdin:
  { "session_id": "...", "user_wav": "path/to/user.wav", "agent_wav": "path/to/agent.wav" }

Prints a JSON result to stdout:
  {
    "talk_over_rate": 0.04,      # fraction 0.0–1.0 (always computed)
    "mos_sig":  4.1,             # only if dnsmos_p835.onnx is present
    "mos_bak":  4.3,
    "mos_ovrl": 4.0,
    "error": "..."               # only on partial failure
  }

Exit code 0 even on partial failure — Go caller checks the "error" field.
The DNSMOS model is optional: download dnsmos_p835.onnx from
https://github.com/microsoft/DNS-Challenge and place it next to this script.
"""

from __future__ import annotations

import json
import sys
import os
import struct
import math

SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))


# ---------------------------------------------------------------------------
# WAV reader (no external deps)
# ---------------------------------------------------------------------------

def read_wav_mono_s16(path: str) -> tuple[list[int], int]:
    """Read a mono PCM-16 WAV and return (samples_as_ints, sample_rate)."""
    with open(path, "rb") as f:
        riff = f.read(4)
        if riff != b"RIFF":
            raise ValueError(f"Not a WAV file: {path}")
        f.read(4)  # chunk size
        f.read(4)  # WAVE
        # Find fmt chunk
        while True:
            chunk_id = f.read(4)
            if not chunk_id:
                raise ValueError("No fmt chunk")
            chunk_size = struct.unpack("<I", f.read(4))[0]
            if chunk_id == b"fmt ":
                fmt_data = f.read(chunk_size)
                audio_fmt, n_channels, sample_rate = struct.unpack_from("<HHI", fmt_data)
                if audio_fmt != 1:
                    raise ValueError("Only PCM WAV supported")
                if n_channels != 1:
                    raise ValueError("Only mono WAV supported")
                break
            f.read(chunk_size)
        # Find data chunk
        while True:
            chunk_id = f.read(4)
            if not chunk_id:
                raise ValueError("No data chunk")
            chunk_size = struct.unpack("<I", f.read(4))[0]
            if chunk_id == b"data":
                raw = f.read(chunk_size)
                break
            f.read(chunk_size)

    n_samples = len(raw) // 2
    samples = list(struct.unpack(f"<{n_samples}h", raw))
    return samples, sample_rate


# ---------------------------------------------------------------------------
# Energy-based Voice Activity Detection
# ---------------------------------------------------------------------------

def vad_frames(samples: list[int], sample_rate: int, frame_ms: int = 100, threshold_rms: float = 180.0) -> list[bool]:
    """Return a bool list: True = speech active in that frame."""
    frame_size = sample_rate * frame_ms // 1000
    active = []
    for i in range(0, len(samples), frame_size):
        frame = samples[i:i + frame_size]
        if not frame:
            break
        rms = math.sqrt(sum(s * s for s in frame) / len(frame))
        active.append(rms > threshold_rms)
    return active


# ---------------------------------------------------------------------------
# Overlap rate
# ---------------------------------------------------------------------------

def compute_overlap_rate(user_path: str, agent_path: str) -> float:
    """
    Fraction of call frames where both user AND agent are simultaneously active.
    Uses energy-based VAD on each separate WAV — no external deps needed.
    """
    user_samples, user_sr = read_wav_mono_s16(user_path)
    agent_samples, agent_sr = read_wav_mono_s16(agent_path)

    # Both should be 8 kHz from the recorder, but be defensive.
    user_frames = vad_frames(user_samples, user_sr)
    agent_frames = vad_frames(agent_samples, agent_sr)

    n = min(len(user_frames), len(agent_frames))
    if n == 0:
        return 0.0

    overlap = sum(1 for i in range(n) if user_frames[i] and agent_frames[i])
    return overlap / n


# ---------------------------------------------------------------------------
# DNSMOS (optional — requires onnxruntime + dnsmos_p835.onnx)
# ---------------------------------------------------------------------------

def try_dnsmos(agent_path: str) -> dict | None:
    """
    Run DNSMOS P.835 on the agent WAV. Returns {"sig", "bak", "ovrl"} or None.
    Silent failure if onnxruntime or the model file are not installed.
    """
    model_path = next(
        (os.path.join(SCRIPT_DIR, f) for f in ("sig_bak_ovr.onnx", "dnsmos_p835.onnx")
         if os.path.exists(os.path.join(SCRIPT_DIR, f))),
        None,
    )
    if model_path is None:
        return None
    model_path = model_path  # reassign for the exists check below
    if not os.path.exists(model_path):
        return None

    try:
        import onnxruntime as ort  # type: ignore
    except Exception:
        return None

    try:
        agent_samples, sr = read_wav_mono_s16(agent_path)

        # DNSMOS expects float32 normalised to [-1, 1] at 16 kHz.
        if sr == 8000:
            # Simple 2× upsample (duplicate each sample — adequate for MOS).
            upsampled = []
            for s in agent_samples:
                upsampled.extend([s, s])
            agent_samples = upsampled
        elif sr != 16000:
            return None  # unexpected rate

        n = len(agent_samples)
        if n == 0:
            return None

        # Normalise to float32.
        audio = [s / 32768.0 for s in agent_samples]

        sess = ort.InferenceSession(model_path, providers=["CPUExecutionProvider"])
        input_name = sess.get_inputs()[0].name

        # Use the model's own expected frame length (144160 for sig_bak_ovr, 160000 for p835).
        target = sess.get_inputs()[0].shape[1]
        if not isinstance(target, int):
            target = 160000  # fallback if shape is symbolic
        if n < target:
            audio = audio + [0.0] * (target - len(audio))
        else:
            audio = audio[:target]

        # onnxruntime accepts nested lists for float32 inputs.
        scores = sess.run(None, {input_name: [list(audio)]})[0]

        # Output shape: (1, 3) — [DNSMOS, SIG, BAK, OVRL] varies by model version.
        # P.835 model returns [SIG, BAK, OVRL] at indices [0], [1], [2].
        row = scores[0]
        if len(row) >= 3:
            return {"sig": float(row[0]), "bak": float(row[1]), "ovrl": float(row[2])}
        return None

    except Exception:
        return None


# ---------------------------------------------------------------------------
# Entry point
# ---------------------------------------------------------------------------

def main():
    try:
        payload = json.loads(sys.stdin.read())
    except Exception as e:
        print(json.dumps({"error": f"invalid stdin JSON: {e}"}))
        sys.exit(0)

    user_wav = payload.get("user_wav", "")
    agent_wav = payload.get("agent_wav", "")
    result: dict = {}
    errors: list[str] = []

    # Overlap rate — always attempt.
    if os.path.exists(user_wav) and os.path.exists(agent_wav):
        try:
            result["talk_over_rate"] = round(compute_overlap_rate(user_wav, agent_wav), 4)
        except Exception as e:
            errors.append(f"overlap: {e}")
    else:
        errors.append(f"WAV files not found: user={user_wav} agent={agent_wav}")

    # DNSMOS — optional, silent on missing model.
    if os.path.exists(agent_wav):
        mos = try_dnsmos(agent_wav)
        if mos:
            result["mos_sig"] = round(mos["sig"], 3)
            result["mos_bak"] = round(mos["bak"], 3)
            result["mos_ovrl"] = round(mos["ovrl"], 3)

    if errors:
        result["error"] = "; ".join(errors)

    print(json.dumps(result))


if __name__ == "__main__":
    try:
        main()
    except Exception as e:
        import traceback
        print(json.dumps({"error": f"scorer crashed: {e}", "traceback": traceback.format_exc()}))
        sys.exit(0)
