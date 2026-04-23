'use client';

import { useEffect, useRef } from 'react';

// G.711 u-law decode lookup table (256 entries → 16-bit PCM).
// Precomputed once at module load.
const ULAW_TABLE: Int16Array = (() => {
  const table = new Int16Array(256);
  for (let i = 0; i < 256; i++) {
    let ulaw = ~i; // invert bits
    const sign = ulaw & 0x80;
    const exponent = (ulaw >> 4) & 0x07;
    const mantissa = ulaw & 0x0f;
    let sample = ((mantissa << 1) + 33) << exponent;
    sample -= 33;
    table[i] = sign ? -sample : sample;
  }
  return table;
})();

function decodeUlaw(ulawBytes: Uint8Array): Float32Array {
  const pcm = new Float32Array(ulawBytes.length);
  for (let i = 0; i < ulawBytes.length; i++) {
    pcm[i] = ULAW_TABLE[ulawBytes[i]] / 32768.0;
  }
  return pcm;
}

interface Props {
  // audioCh is an async generator / stream of Uint8Array mulaw chunks.
  // For now, AudioPlayer is wired via the events WS; this prop is reserved
  // for a dedicated audio WebSocket in a production setup.
  serverUrl: string;
}

export default function AudioPlayer({ serverUrl }: Props) {
  const ctxRef = useRef<AudioContext | null>(null);
  const wsRef = useRef<WebSocket | null>(null);
  const nextPlayTime = useRef<number>(0);

  useEffect(() => {
    const audioCtx = new AudioContext({ sampleRate: 8000 });
    ctxRef.current = audioCtx;

    const ws = new WebSocket(serverUrl);
    ws.binaryType = 'arraybuffer';
    wsRef.current = ws;

    ws.onopen = () => {
      nextPlayTime.current = audioCtx.currentTime;
    };

    ws.onmessage = (ev) => {
      if (!(ev.data instanceof ArrayBuffer)) return;
      const ulawBytes = new Uint8Array(ev.data);
      const pcm = decodeUlaw(ulawBytes);

      const buffer = audioCtx.createBuffer(1, pcm.length, 8000);
      buffer.copyToChannel(pcm as Float32Array<ArrayBuffer>, 0);

      const source = audioCtx.createBufferSource();
      source.buffer = buffer;
      source.connect(audioCtx.destination);

      const startAt = Math.max(audioCtx.currentTime, nextPlayTime.current);
      source.start(startAt);
      nextPlayTime.current = startAt + buffer.duration;
    };

    return () => {
      ws.close();
      audioCtx.close();
    };
  }, [serverUrl]);

  return null; // invisible component
}
