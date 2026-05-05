'use client';

import { useEffect, useRef, useState, useCallback } from 'react';
import TranscriptFeed from './TranscriptFeed';
import DemoScript from './DemoScript';
import CalendlyWidget from './CalendlyWidget';
import type { Chunk, Metrics, ToolEvent } from '@/hooks/useEventsSocket';

// ---------------------------------------------------------------------------


// ---------------------------------------------------------------------------
// ToolEventPanel — live API call log
// ---------------------------------------------------------------------------


// ---------------------------------------------------------------------------
// Main LiveCallView
// ---------------------------------------------------------------------------

interface Props {
  chunks: Chunk[];
  metrics: Metrics | null;
  sessionId: string | null;
  connected: boolean;
  recording: boolean;
  onStart: () => void;
  onStop: () => void;
  error: string | null;
  getToolEvents: () => ToolEvent[];
  onToolEvent: (fn: () => void) => () => void;
}

export default function LiveCallView({
  chunks,
  metrics,
  sessionId,
  connected,
  recording,
  onStart,
  onStop,
  error,
  getToolEvents,
  onToolEvent,
}: Props) {
  // Bump this whenever a book_meeting result arrives to refresh the calendar.
  const [calRefreshKey, setCalRefreshKey] = useState(0);

  const handleToolEvent = useCallback(() => {
    const evts = getToolEvents();
    const last = evts[evts.length - 1];
    if (last?.kind === 'result' && last.name === 'book_meeting') {
      setCalRefreshKey(k => k + 1);
    }
  }, [getToolEvents]);

  useEffect(() => {
    const unsub = onToolEvent(handleToolEvent);
    return unsub;
  }, [onToolEvent, handleToolEvent]);

  return (
    <div style={{ display: 'flex', height: '100%', overflow: 'hidden' }}>
      {/* ── Left: demo script ── */}
      <DemoScript />

      {/* ── Center: transcript + mic controls ── */}
      <div style={{
        flex: 1,
        display: 'flex',
        flexDirection: 'column',
        minWidth: 0,
        borderRight: '1px solid #e2e8f0',
      }}>
        {/* Top bar */}
        <div style={{
          display: 'flex',
          alignItems: 'center',
          gap: '0.75rem',
          padding: '0.6rem 1rem',
          borderBottom: '1px solid #e2e8f0',
          background: '#fff',
          flexShrink: 0,
        }}>
          <div style={{
            width: 8,
            height: 8,
            borderRadius: '50%',
            background: connected ? '#10b981' : '#ef4444',
            boxShadow: connected && recording ? '0 0 0 3px rgba(16,185,129,0.25)' : 'none',
          }} />
          <span style={{ fontSize: '0.72rem', color: '#64748b' }}>
            {connected ? (recording ? 'En écoute' : 'Connecté') : 'Déconnecté'}
          </span>
        </div>

        {error && (
          <div style={{ padding: '0.5rem 1rem', background: '#fef2f2', color: '#ef4444', fontSize: '0.8rem', flexShrink: 0, borderBottom: '1px solid #fecaca' }}>
            {error}
          </div>
        )}

        {/* Transcript area */}
        <div style={{ flex: 1, overflow: 'hidden', display: 'flex', flexDirection: 'column', padding: '0.75rem 1rem', minHeight: 0 }}>
          <p style={{ fontSize: '0.68rem', fontWeight: 700, textTransform: 'uppercase', letterSpacing: '0.08em', color: '#94a3b8', marginBottom: '0.5rem', flexShrink: 0 }}>
            Transcription en direct
          </p>
          <TranscriptFeed chunks={chunks} getToolEvents={getToolEvents} onToolEvent={onToolEvent} />
        </div>

        {/* Legend */}
        <div style={{ display: 'flex', gap: '1rem', padding: '0.5rem 1rem', borderTop: '1px solid #e2e8f0', flexShrink: 0, background: '#fafafa' }}>
          <span style={{ fontSize: '0.68rem', color: '#64748b', display: 'flex', alignItems: 'center', gap: '4px' }}>
            <span style={{ color: '#10b981', fontWeight: 700 }}>■</span> Léa
          </span>
          <span style={{ fontSize: '0.68rem', color: '#64748b', display: 'flex', alignItems: 'center', gap: '4px' }}>
            <span style={{ color: '#3b82f6', fontWeight: 700 }}>■</span> Vous
          </span>
          <span style={{ fontSize: '0.68rem', color: '#64748b', display: 'flex', alignItems: 'center', gap: '4px' }}>
            <span style={{ color: '#ef4444', fontWeight: 700 }}>■</span> Interrompu
          </span>
        </div>
      </div>

      {/* ── Right column: Calendly widget only ── */}
      <div style={{ width: 380, minWidth: 380, maxWidth: 380, display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
        <div style={{ flex: 1, display: 'flex', flexDirection: 'column', minHeight: 0 }}>
          <div style={{ padding: '0.6rem 0.75rem', borderBottom: '1px solid #e2e8f0', background: '#fff', flexShrink: 0 }}>
            <span style={{ fontSize: '0.7rem', fontWeight: 700, textTransform: 'uppercase', letterSpacing: '0.08em', color: '#64748b' }}>
              Calendly — Semaine en cours
            </span>
          </div>
          <div style={{ flex: 1, overflow: 'hidden', minHeight: 0 }}>
            <CalendlyWidget refreshKey={calRefreshKey} />
          </div>
        </div>
      </div>
    </div>
  );
}
