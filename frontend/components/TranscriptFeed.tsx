'use client';

import React, { useEffect, useRef } from 'react';
import type { Chunk } from '@/hooks/useEventsSocket';

interface Props {
  chunks: Chunk[];
}

function ChunkItem({ chunk }: { chunk: Chunk }) {
  const isCancelled = chunk.status === 'cancelled';
  const isFinal = chunk.status === 'final';

  return (
    <div
      style={{
        padding: '0.6rem 0.9rem',
        borderRadius: '8px',
        background: isFinal ? 'rgba(76,175,136,0.08)' : 'var(--surface)',
        border: `1px solid ${isFinal ? 'rgba(76,175,136,0.25)' : 'var(--border)'}`,
        marginBottom: '0.5rem',
        transition: 'opacity 0.2s ease',
        opacity: isCancelled ? 'var(--cancelled-opacity)' : 1,
      }}
    >
      <div
        style={{
          display: 'flex',
          alignItems: 'baseline',
          gap: '0.5rem',
          marginBottom: '0.2rem',
        }}
      >
        <StatusBadge status={chunk.status} />
        <span style={{ fontSize: '0.7rem', color: 'var(--text-secondary)' }}>
          {new Date(chunk.ts).toLocaleTimeString('fr-FR', { hour12: false })}
        </span>
      </div>
      <p
        style={{
          fontSize: '0.95rem',
          lineHeight: 1.5,
          textDecoration: isCancelled ? 'line-through' : 'none',
          color: isCancelled ? 'var(--text-secondary)' : 'var(--text-primary)',
        }}
      >
        {chunk.text || <span style={{ color: 'var(--text-secondary)', fontStyle: 'italic' }}>…</span>}
      </p>
    </div>
  );
}

function StatusBadge({ status }: { status: Chunk['status'] }) {
  const map: Record<Chunk['status'], { label: string; color: string }> = {
    playing: { label: 'En cours', color: 'var(--accent)' },
    cancelled: { label: 'Interrompu', color: 'var(--danger)' },
    final: { label: 'Finalisé', color: 'var(--success)' },
  };
  const { label, color } = map[status];
  return (
    <span
      style={{
        fontSize: '0.65rem',
        fontWeight: 600,
        textTransform: 'uppercase',
        letterSpacing: '0.06em',
        color,
        padding: '1px 6px',
        borderRadius: '3px',
        border: `1px solid ${color}`,
      }}
    >
      {label}
    </span>
  );
}

export default React.memo(function TranscriptFeed({ chunks }: Props) {
  const bottomRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [chunks.length]);

  return (
    <div
      style={{
        width: '100%',
        maxWidth: '700px',
        maxHeight: '400px',
        overflowY: 'auto',
        display: 'flex',
        flexDirection: 'column',
        gap: '0',
      }}
    >
      {chunks.length === 0 ? (
        <p style={{ color: 'var(--text-secondary)', textAlign: 'center', fontSize: '0.9rem', padding: '2rem 0' }}>
          En attente de la session…
        </p>
      ) : (
        chunks.map((chunk) => <ChunkItem key={chunk.chunkId} chunk={chunk} />)
      )}
      <div ref={bottomRef} />
    </div>
  );
});
