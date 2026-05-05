'use client';

import React, { useEffect, useRef, useState, useCallback } from 'react';
import type { Chunk, Metrics, ToolEvent } from '@/hooks/useEventsSocket';

interface Props {
  chunks: Chunk[];
  getToolEvents?: () => ToolEvent[];
  onToolEvent?: (fn: () => void) => () => void;
}

const TOOL_LABELS: Record<string, string> = {
  check_availability: 'Calendly · Disponibilités',
  book_meeting:       'Calendly · Réservation',
  fetch_prospect:     'CRM · Prospect',
};

function toolIcon(name: string): string {
  if (name === 'book_meeting') return 'event_available';
  if (name === 'check_availability') return 'calendar_month';
  return 'api';
}

function prettyJson(raw: string): string {
  try { return JSON.stringify(JSON.parse(raw), null, 2); } catch { return raw; }
}

function timeLabel(ts: number): string {
  return new Date(ts).toLocaleTimeString('fr-FR', { hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false });
}

function ToolItem({ ev }: { ev: ToolEvent }) {
  const [expanded, setExpanded] = useState(false);
  const isCall = ev.kind === 'call';
  const label = TOOL_LABELS[ev.name] ?? ev.name;
  const accent = isCall ? '#6366f1' : '#10b981';
  const bg     = isCall ? 'rgba(99,102,241,0.06)' : 'rgba(16,185,129,0.06)';
  const border = isCall ? 'rgba(99,102,241,0.2)' : 'rgba(16,185,129,0.2)';

  return (
    <div
      onClick={() => setExpanded(!expanded)}
      style={{
        background: bg,
        border: `1px solid ${border}`,
        borderRadius: '8px',
        padding: '0.45rem 0.6rem',
        cursor: 'pointer',
        transition: 'all 0.15s',
        marginBottom: '0.4rem',
      }}
    >
      <div style={{ display: 'flex', alignItems: 'center', gap: '0.4rem' }}>
        <span className="material-symbols-outlined" style={{ fontSize: 15, color: accent, fontVariationSettings: "'FILL' 1" }}>
          {toolIcon(ev.name)}
        </span>
        <span style={{ flex: 1, fontSize: '0.72rem', fontWeight: 600, color: '#1e293b' }}>{label}</span>
        <span style={{
          fontSize: '0.6rem', fontWeight: 700, textTransform: 'uppercase', letterSpacing: '0.06em',
          color: accent, background: `${accent}18`, padding: '1px 5px', borderRadius: '4px',
        }}>
          {isCall ? 'CALL' : 'RESULT'}
        </span>
        <span style={{ fontSize: '0.62rem', color: '#94a3b8', fontFamily: 'monospace' }}>
          {timeLabel(ev.ts)}
        </span>
        <span className="material-symbols-outlined" style={{ fontSize: 14, color: '#94a3b8' }}>
          {expanded ? 'expand_less' : 'expand_more'}
        </span>
      </div>

      {expanded && (
        <pre style={{
          marginTop: '0.4rem',
          padding: '0.5rem',
          background: '#0f172a',
          borderRadius: '6px',
          fontSize: '0.62rem',
          color: '#94a3b8',
          overflowX: 'auto',
          whiteSpace: 'pre-wrap',
          wordBreak: 'break-all',
          lineHeight: 1.5,
        }}>
          {prettyJson(ev.payload)}
        </pre>
      )}
    </div>
  );
}

function ChunkItem({ chunk }: { chunk: Chunk }) {
  const isCancelled = chunk.status === 'cancelled';
  const isFinal     = chunk.status === 'final';
  const isUser      = chunk.role === 'user';

  const bg     = isUser ? 'rgba(59,130,246,0.08)' : isFinal ? 'rgba(76,175,136,0.08)' : 'var(--surface)';
  const border = isUser
    ? 'rgba(59,130,246,0.3)'
    : isFinal
    ? 'rgba(76,175,136,0.25)'
    : 'var(--border)';

  return (
    <div
      style={{
        padding: '0.55rem 0.85rem',
        borderRadius: '8px',
        background: bg,
        border: `1px solid ${border}`,
        marginBottom: '0.4rem',
        opacity: isCancelled ? 0.45 : 1,
        transition: 'opacity 0.2s ease, background 0.3s ease',
      }}
    >
      <div style={{ display: 'flex', alignItems: 'baseline', gap: '0.4rem', marginBottom: '0.15rem' }}>
        <RoleBadge role={chunk.role} />
        <StatusBadge status={chunk.status} />
        <span style={{ fontSize: '0.65rem', color: 'var(--text-secondary)', marginLeft: 'auto' }}>
          {new Date(chunk.ts).toLocaleTimeString('fr-FR', { hour12: false })}
        </span>
      </div>
      <p
        style={{
          fontSize: '0.9rem',
          lineHeight: 1.5,
          textDecoration: isCancelled ? 'line-through' : 'none',
          color: isCancelled ? 'var(--text-secondary)' : 'var(--text-primary)',
          margin: 0,
        }}
      >
        {chunk.text}
      </p>
      {chunk.role === 'assistant' && chunk.metrics && (
        <InlineMetrics metrics={chunk.metrics} />
      )}
    </div>
  );
}

function RoleBadge({ role }: { role: Chunk['role'] }) {
  const isUser = role === 'user';
  return (
    <span style={{
      fontSize: '0.6rem',
      fontWeight: 700,
      textTransform: 'uppercase',
      letterSpacing: '0.07em',
      color: isUser ? '#3b82f6' : '#10b981',
      padding: '1px 5px',
      borderRadius: '3px',
      background: isUser ? 'rgba(59,130,246,0.1)' : 'rgba(16,185,129,0.1)',
    }}>
      {isUser ? 'You' : 'Léa'}
    </span>
  );
}

function StatusBadge({ status }: { status: Chunk['status'] }) {
  const map: Record<Chunk['status'], { label: string; color: string }> = {
    playing:   { label: 'En cours',   color: 'var(--accent)' },
    cancelled: { label: 'Interrompu', color: 'var(--danger)' },
    final:     { label: 'Finalisé',   color: 'var(--success)' },
  };
  const { label, color } = map[status];
  return (
    <span style={{
      fontSize: '0.58rem',
      fontWeight: 600,
      textTransform: 'uppercase',
      letterSpacing: '0.06em',
      color,
      padding: '1px 5px',
      borderRadius: '3px',
      border: `1px solid ${color}`,
    }}>
      {label}
    </span>
  );
}

function InlineMetrics({ metrics }: { metrics: Metrics }) {
  return (
    <div style={{
      display: 'flex',
      gap: '0.75rem',
      marginTop: '0.3rem',
      fontSize: '0.62rem',
      color: 'var(--text-secondary)',
      fontFamily: 'monospace',
    }}>
      <span title="Time to first LLM token">TTFT <strong style={{ color: '#6366f1' }}>{metrics.ttft_ms}ms</strong></span>
      <span title="Time to first audio">TTFA <strong style={{ color: '#10b981' }}>{metrics.ttfa_ms}ms</strong></span>
      <span title="End-to-end latency">E2E <strong style={{ color: '#f59e0b' }}>{metrics.e2e_ms}ms</strong></span>
    </div>
  );
}

type FeedItem = { type: 'chunk'; data: Chunk } | { type: 'tool'; data: ToolEvent };

export default React.memo(function TranscriptFeed({ chunks, getToolEvents, onToolEvent }: Props) {
  const containerRef = useRef<HTMLDivElement>(null);
  const [toolEvents, setToolEvents] = useState<ToolEvent[]>([]);

  useEffect(() => {
    if (!getToolEvents || !onToolEvent) return;
    const refresh = () => setToolEvents([...getToolEvents()]);
    refresh();
    const unsub = onToolEvent(refresh);
    return unsub;
  }, [getToolEvents, onToolEvent]);

  // Auto-scroll on any change
  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    el.scrollTop = el.scrollHeight;
  }, [chunks, toolEvents]);

  // Combine and sort chronologically
  const items: FeedItem[] = [
    ...chunks.map(c => ({ type: 'chunk', data: c } as FeedItem)),
    ...toolEvents.map(t => ({ type: 'tool', data: t } as FeedItem)),
  ].sort((a, b) => a.data.ts - b.data.ts);

  return (
    <div
      ref={containerRef}
      style={{
        width: '100%',
        flex: 1,
        overflowY: 'auto',
        display: 'flex',
        flexDirection: 'column',
        minHeight: 0,
      }}
    >
      {items.length === 0 ? (
        <p style={{ color: 'var(--text-secondary)', textAlign: 'center', fontSize: '0.9rem', padding: '2rem 0' }}>
          En attente de la session…
        </p>
      ) : (
        items.map(item => 
          item.type === 'chunk' ? (
            <ChunkItem key={item.data.chunkId} chunk={item.data as Chunk} />
          ) : (
            <ToolItem key={(item.data as ToolEvent).id} ev={item.data as ToolEvent} />
          )
        )
      )}
    </div>
  );
});
