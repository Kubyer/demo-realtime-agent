'use client';

import { useState, useRef, useCallback, useEffect } from 'react';
import TranscriptFeed from './TranscriptFeed';
import { useEventsSocket } from '@/hooks/useEventsSocket';
import type { Chunk } from '@/hooks/useEventsSocket';

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

function wsUrl(path: string): string {
  const backendBase = process.env.NEXT_PUBLIC_BACKEND_URL;
  if (backendBase) {
    const proto = backendBase.startsWith('https') ? 'wss' : 'ws';
    return `${proto}://${backendBase.replace(/^https?:\/\//, '')}${path}`;
  }
  if (typeof window === 'undefined') return `ws://localhost:8080${path}`;
  const proto = window.location.protocol === 'https:' ? 'wss' : 'ws';
  return `${proto}://${window.location.host}${path}`;
}

export default function VoiceSession() {
  const { chunks, connected, sessionId } = useEventsSocket(wsUrl('/events'));
  const [recording, setRecording] = useState(false);
  const [error, setError]         = useState<string | null>(null);

  const wsRef          = useRef<WebSocket | null>(null);
  const audioCtxRef    = useRef<AudioContext | null>(null);
  const workletRef     = useRef<AudioWorkletNode | null>(null);
  const streamRef      = useRef<MediaStream | null>(null);
  const nextPlayRef    = useRef<number>(0);
  const sourcesRef     = useRef<AudioBufferSourceNode[]>([]);
  const cancelledRef   = useRef<Set<string>>(new Set());

  // Stop all queued TTS audio when a barge-in cancellation event arrives.
  useEffect(() => {
    const newCancels = chunks.filter(
      (c: Chunk) => c.status === 'cancelled' && !cancelledRef.current.has(c.chunkId),
    );
    if (newCancels.length === 0) return;
    newCancels.forEach((c: Chunk) => cancelledRef.current.add(c.chunkId));
    sourcesRef.current.forEach(s => { try { s.stop(); } catch { /* already stopped */ } });
    sourcesRef.current = [];
    if (audioCtxRef.current) nextPlayRef.current = audioCtxRef.current.currentTime;
  }, [chunks]);

  const startSession = useCallback(async () => {
    setError(null);
    try {
      // Request mic at 16 kHz mono to match Soniox pcm_s16le config.
      const stream = await navigator.mediaDevices.getUserMedia({
        audio: { sampleRate: 16000, channelCount: 1, echoCancellation: true },
        video: false,
      });
      streamRef.current = stream;

      // AudioContext at 16 kHz — browser resamples internally if needed.
      const audioCtx = new AudioContext({ sampleRate: 16000 });
      audioCtxRef.current = audioCtx;
      nextPlayRef.current = audioCtx.currentTime;

      // Connect to backend browser stream.
      const ws = new WebSocket(wsUrl('/browser/stream'));
      ws.binaryType = 'arraybuffer';
      wsRef.current = ws;

      await new Promise<void>((resolve, reject) => {
        ws.onopen  = () => resolve();
        ws.onerror = () => reject(new Error('WebSocket connection failed'));
      });

      // Incoming binary frames = mulaw 8 kHz TTS audio → decode and schedule.
      ws.onmessage = (ev) => {
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
      // Do NOT connect worklet to destination — avoids mic echo.

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

    setRecording(false);
  }, []);

  return (
    <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', gap: '1.5rem', width: '100%' }}>
      {/* Connection status */}
      <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', fontSize: '0.8rem', color: 'var(--text-secondary)' }}>
        <div style={{ width: 8, height: 8, borderRadius: '50%', background: connected ? 'var(--success)' : 'var(--danger)' }} />
        {connected ? 'Connecté au serveur' : 'Déconnecté'}
        {sessionId && (
          <span style={{ marginLeft: '0.5rem', opacity: 0.6 }}>Session: {sessionId.slice(0, 12)}…</span>
        )}
      </div>

      {/* Mic button */}
      <button
        onClick={recording ? stopSession : startSession}
        style={{
          width: 80, height: 80, borderRadius: '50%',
          background: recording ? 'var(--danger)' : 'var(--accent)',
          color: '#fff', fontSize: '2rem',
          display: 'flex', alignItems: 'center', justifyContent: 'center',
          transition: 'background 0.2s ease, transform 0.1s ease',
          boxShadow: recording ? '0 0 0 6px rgba(255,92,92,0.2)' : '0 0 0 0px transparent',
        }}
        title={recording ? 'Arrêter' : 'Démarrer la session'}
      >
        {recording ? '◼' : '🎙'}
      </button>

      <p style={{ fontSize: '0.85rem', color: 'var(--text-secondary)' }}>
        {recording ? 'En écoute — cliquez pour arrêter' : 'Cliquez pour démarrer'}
      </p>

      {error && <p style={{ color: 'var(--danger)', fontSize: '0.85rem' }}>{error}</p>}

      {/* Live transcript */}
      <div style={{ width: '100%', background: 'var(--surface)', border: '1px solid var(--border)', borderRadius: '12px', padding: '1rem' }}>
        <p style={{ fontSize: '0.7rem', fontWeight: 700, textTransform: 'uppercase', letterSpacing: '0.08em', color: 'var(--text-secondary)', marginBottom: '0.75rem' }}>
          Transcription en direct
        </p>
        <TranscriptFeed chunks={chunks} />
      </div>

      <div style={{ display: 'flex', gap: '1.5rem', fontSize: '0.75rem', color: 'var(--text-secondary)' }}>
        <span><span style={{ color: 'var(--success)' }}>■</span> Finalisé</span>
        <span><span style={{ color: 'var(--accent)' }}>■</span> En cours</span>
        <span><span style={{ color: 'var(--danger)' }}>■</span> Interrompu</span>
      </div>
    </div>
  );
}
