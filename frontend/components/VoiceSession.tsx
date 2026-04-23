'use client';

import { useState, useRef, useCallback } from 'react';
import TranscriptFeed from './TranscriptFeed';
import { useEventsSocket } from '@/hooks/useEventsSocket';

const EVENTS_WS_URL = process.env.NEXT_PUBLIC_EVENTS_WS_URL ?? 'ws://localhost:8080/events';
const BACKEND_STREAM_URL = process.env.NEXT_PUBLIC_STREAM_URL ?? 'ws://localhost:8080/twilio/stream';

export default function VoiceSession() {
  const { chunks, connected, sessionId } = useEventsSocket(EVENTS_WS_URL);
  const [recording, setRecording] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const mediaRecorderRef = useRef<MediaRecorder | null>(null);
  const streamWsRef = useRef<WebSocket | null>(null);
  const streamRef = useRef<MediaStream | null>(null);

  const startSession = useCallback(async () => {
    setError(null);
    try {
      const stream = await navigator.mediaDevices.getUserMedia({ audio: true, video: false });
      streamRef.current = stream;

      const ws = new WebSocket(BACKEND_STREAM_URL);
      streamWsRef.current = ws;

      await new Promise<void>((resolve, reject) => {
        ws.onopen = () => resolve();
        ws.onerror = () => reject(new Error('WebSocket connection failed'));
      });

      // Send a synthetic Twilio `start` event so the Go server initialises the session.
      ws.send(JSON.stringify({
        event: 'start',
        streamSid: `mock-${Date.now()}`,
        start: { streamSid: `mock-${Date.now()}`, callSid: `CA-mock-${Date.now()}` },
      }));

      // MediaRecorder → mulaw approximation → WebSocket.
      // Browser MediaRecorder doesn't support mulaw natively; we send PCM as-is
      // and let the backend handle the encoding if needed. For a production
      // deployment use an AudioWorklet mulaw encoder.
      const mimeType = MediaRecorder.isTypeSupported('audio/webm;codecs=pcm')
        ? 'audio/webm;codecs=pcm'
        : 'audio/webm';

      const recorder = new MediaRecorder(stream, { mimeType, audioBitsPerSecond: 64000 });
      mediaRecorderRef.current = recorder;

      recorder.ondataavailable = (e) => {
        if (e.data.size === 0 || ws.readyState !== WebSocket.OPEN) return;
        const reader = new FileReader();
        reader.onload = () => {
          const b64 = (reader.result as string).split(',')[1];
          ws.send(JSON.stringify({
            event: 'media',
            streamSid: `mock-${Date.now()}`,
            media: { payload: b64 },
          }));
        };
        reader.readAsDataURL(e.data);
      };

      recorder.start(20); // 20ms chunks to match Twilio's cadence
      setRecording(true);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Erreur inconnue');
    }
  }, []);

  const stopSession = useCallback(() => {
    mediaRecorderRef.current?.stop();
    streamRef.current?.getTracks().forEach(t => t.stop());
    if (streamWsRef.current?.readyState === WebSocket.OPEN) {
      streamWsRef.current.send(JSON.stringify({ event: 'stop', streamSid: '' }));
      streamWsRef.current.close();
    }
    setRecording(false);
  }, []);

  return (
    <div style={{
      display: 'flex',
      flexDirection: 'column',
      alignItems: 'center',
      gap: '1.5rem',
      width: '100%',
      maxWidth: '700px',
    }}>
      {/* Status bar */}
      <div style={{
        display: 'flex',
        alignItems: 'center',
        gap: '0.5rem',
        fontSize: '0.8rem',
        color: 'var(--text-secondary)',
      }}>
        <div style={{
          width: 8,
          height: 8,
          borderRadius: '50%',
          background: connected ? 'var(--success)' : 'var(--danger)',
        }} />
        {connected ? 'Connecté au serveur' : 'Déconnecté'}
        {sessionId && (
          <span style={{ marginLeft: '0.5rem', opacity: 0.6 }}>
            Session: {sessionId.slice(0, 12)}…
          </span>
        )}
      </div>

      {/* Microphone button */}
      <button
        onClick={recording ? stopSession : startSession}
        style={{
          width: 80,
          height: 80,
          borderRadius: '50%',
          background: recording ? 'var(--danger)' : 'var(--accent)',
          color: '#fff',
          fontSize: '2rem',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          transition: 'background 0.2s ease, transform 0.1s ease',
          boxShadow: recording
            ? '0 0 0 6px rgba(255,92,92,0.2)'
            : '0 0 0 0px transparent',
        }}
        title={recording ? 'Arrêter' : 'Démarrer la session'}
      >
        {recording ? '◼' : '🎙'}
      </button>

      <p style={{ fontSize: '0.85rem', color: 'var(--text-secondary)' }}>
        {recording ? 'En écoute — cliquez pour arrêter' : 'Cliquez pour démarrer'}
      </p>

      {error && (
        <p style={{ color: 'var(--danger)', fontSize: '0.85rem' }}>{error}</p>
      )}

      {/* Transcript feed */}
      <div style={{
        width: '100%',
        background: 'var(--surface)',
        border: '1px solid var(--border)',
        borderRadius: '12px',
        padding: '1rem',
      }}>
        <p style={{
          fontSize: '0.7rem',
          fontWeight: 700,
          textTransform: 'uppercase',
          letterSpacing: '0.08em',
          color: 'var(--text-secondary)',
          marginBottom: '0.75rem',
        }}>
          Transcription
        </p>
        <TranscriptFeed chunks={chunks} />
      </div>

      {/* Legend */}
      <div style={{
        display: 'flex',
        gap: '1.5rem',
        fontSize: '0.75rem',
        color: 'var(--text-secondary)',
      }}>
        <span><span style={{ color: 'var(--success)' }}>■</span> Finalisé</span>
        <span><span style={{ color: 'var(--accent)' }}>■</span> En cours</span>
        <span><span style={{ color: 'var(--danger)' }}>■</span> Interrompu (barge-in)</span>
      </div>
    </div>
  );
}
