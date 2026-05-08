'use client';

import { useState, useRef, useCallback } from 'react';
import type { Chunk } from './useEventsSocket';

// G.711 µ-law → Float32 decode table (precomputed once).
const ULAW_TABLE: Float32Array = (() => {
  const t = new Float32Array(256);
  for (let i = 0; i < 256; i++) {
    let u = ~i;
    const sign = u & 0x80;
    const exp  = (u >> 4) & 0x07;
    const mant = u & 0x0f;
    let s = ((mant << 1) + 33) << exp;
    s -= 33;
    t[i] = (sign ? -s : s) / 32768.0;
  }
  return t;
})();

export function wsUrl(path: string): string {
  const backendBase = process.env.NEXT_PUBLIC_BACKEND_URL;
  if (backendBase) {
    const proto = backendBase.startsWith('https') ? 'wss' : 'ws';
    return `${proto}://${backendBase.replace(/^https?:\/\//, '')}${path}`;
  }
  if (typeof window === 'undefined') return `ws://localhost:8080${path}`;
  const proto = window.location.protocol === 'https:' ? 'wss' : 'ws';
  return `${proto}://${window.location.host}${path}`;
}

export function useVoiceSession(chunks: Chunk[]) {
  const [recording, setRecording] = useState(false);
  const [interrupted, setInterrupted] = useState(false);
  const [error, setError]         = useState<string | null>(null);

  const wsRef        = useRef<WebSocket | null>(null);
  const audioCtxRef  = useRef<AudioContext | null>(null);
  const workletRef   = useRef<AudioWorkletNode | null>(null);
  const streamRef    = useRef<MediaStream | null>(null);
  const nextPlayRef  = useRef<number>(0);
  const sourcesRef   = useRef<AudioBufferSourceNode[]>([]);
  const cancelledRef = useRef<Set<string>>(new Set());

  // Stop all queued TTS audio when a barge-in cancellation arrives.
  const handleChunks = useCallback((newChunks: Chunk[]) => {
    const newCancels = newChunks.filter(
      c => c.status === 'cancelled' && !cancelledRef.current.has(c.chunkId),
    );
    if (newCancels.length === 0) return;
    newCancels.forEach(c => cancelledRef.current.add(c.chunkId));
    sourcesRef.current.forEach(s => { try { s.stop(); } catch { /* already stopped */ } });
    sourcesRef.current = [];
    if (audioCtxRef.current) nextPlayRef.current = audioCtxRef.current.currentTime;
  }, []);

  // Keep cancellation handler in sync with latest chunks
  // (called by the consumer component in a useEffect).
  const syncCancels = useCallback(() => handleChunks(chunks), [chunks, handleChunks]);

  const startSession = useCallback(async () => {
    setError(null);
    try {
      const stream = await navigator.mediaDevices.getUserMedia({
        audio: { sampleRate: 16000, channelCount: 1, echoCancellation: true },
        video: false,
      });
      streamRef.current = stream;

      const audioCtx = new AudioContext({ sampleRate: 16000 });
      audioCtxRef.current = audioCtx;
      nextPlayRef.current = audioCtx.currentTime;

      const ws = new WebSocket(wsUrl('/browser/stream'));
      ws.binaryType = 'arraybuffer';
      wsRef.current = ws;

      await new Promise<void>((resolve, reject) => {
        ws.onopen  = () => resolve();
        ws.onerror = () => reject(new Error('WebSocket connection failed'));
      });

      // Incoming messages: binary = mulaw TTS audio; text = control frame.
      ws.onmessage = (ev) => {
        // Fix #3: Handle backend clear signal — stop all queued audio immediately.
        if (typeof ev.data === 'string') {
          try {
            const ctrl = JSON.parse(ev.data);
            if (ctrl.type === 'clear') {
              // Only show the barge-in indicator for actual user interruptions,
              // not routine filler→TTS audio swaps.
              if (ctrl.bargein === true) {
                setInterrupted(true);
                setTimeout(() => setInterrupted(false), 1000);
              }
              sourcesRef.current.forEach(s => { try { s.stop(); } catch { /* already stopped */ } });
              sourcesRef.current = [];
              if (audioCtxRef.current) nextPlayRef.current = audioCtxRef.current.currentTime;
            }
          } catch { /* not JSON, ignore */ }
          return;
        }

        if (!(ev.data instanceof ArrayBuffer)) return;
        const ctx = audioCtxRef.current;
        if (!ctx) return;

        const ulawBytes = new Uint8Array(ev.data);
        const pcm = new Float32Array(ulawBytes.length);
        for (let i = 0; i < ulawBytes.length; i++) pcm[i] = ULAW_TABLE[ulawBytes[i]];

        const buf = ctx.createBuffer(1, pcm.length, 8000);
        buf.copyToChannel(pcm, 0);

        const src = ctx.createBufferSource();
        src.buffer = buf;
        src.connect(ctx.destination);

        const startAt = Math.max(ctx.currentTime, nextPlayRef.current);
        src.start(startAt);
        nextPlayRef.current = startAt + buf.duration;

        sourcesRef.current.push(src);
        src.onended = () => {
          sourcesRef.current = sourcesRef.current.filter(s => s !== src);
        };
      };

      // AudioWorklet: captures PCM s16le frames and sends as binary WS messages.
      await audioCtx.audioWorklet.addModule('/worklets/pcm-processor.js');
      const worklet = new AudioWorkletNode(audioCtx, 'pcm-processor');
      workletRef.current = worklet;

      worklet.port.onmessage = (e: MessageEvent<ArrayBuffer>) => {
        if (ws.readyState === WebSocket.OPEN) ws.send(e.data);
      };

      const source = audioCtx.createMediaStreamSource(stream);
      source.connect(worklet);

      setRecording(true);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Erreur inconnue');
    }
  }, []);

  const stopSession = useCallback(() => {
    workletRef.current?.disconnect();
    workletRef.current = null;
    streamRef.current?.getTracks().forEach(t => t.stop());
    streamRef.current = null;

    if (wsRef.current?.readyState === WebSocket.OPEN) wsRef.current.close();
    wsRef.current = null;

    audioCtxRef.current?.close();
    audioCtxRef.current = null;
    sourcesRef.current = [];
    cancelledRef.current.clear();

    setRecording(false);
  }, []);

  return { recording, interrupted, startSession, stopSession, error, syncCancels };
}
