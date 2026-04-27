'use client';

import { useState, useEffect, useCallback } from 'react';

interface TurnEntry {
  role: 'user' | 'assistant';
  text: string;
  ts: number;
}

interface CallRecord {
  id: string;
  source: 'twilio' | 'browser';
  status: 'ongoing' | 'done';
  started_at: number;
  ended_at?: number;
  transcript: TurnEntry[];
}

function duration(r: CallRecord): string {
  if (!r.ended_at) return '—';
  const sec = Math.round((r.ended_at - r.started_at) / 1000);
  if (sec < 60) return `${sec}s`;
  return `${Math.floor(sec / 60)}m ${sec % 60}s`;
}

function timeLabel(ms: number): string {
  return new Date(ms).toLocaleTimeString('fr-FR', { hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false });
}

function StatusBadge({ status }: { status: CallRecord['status'] }) {
  const cfg = status === 'ongoing'
    ? { label: 'En cours', color: 'var(--accent)' }
    : { label: 'Terminé',  color: 'var(--success)' };
  return (
    <span style={{
      fontSize: '0.65rem', fontWeight: 700, textTransform: 'uppercase' as const,
      letterSpacing: '0.06em', color: cfg.color,
      padding: '2px 7px', borderRadius: '4px', border: `1px solid ${cfg.color}`,
    }}>
      {cfg.label}
    </span>
  );
}

function SourceBadge({ source }: { source: CallRecord['source'] }) {
  return (
    <span style={{
      fontSize: '0.65rem', fontWeight: 600,
      color: 'var(--text-secondary)',
      padding: '2px 7px', borderRadius: '4px',
      border: '1px solid var(--border)',
    }}>
      {source === 'twilio' ? 'Téléphone' : 'Navigateur'}
    </span>
  );
}

function CallDetail({ call, onClose }: { call: CallRecord; onClose: () => void }) {
  return (
    <div style={{
      position: 'fixed', inset: 0,
      background: 'rgba(0,0,0,0.6)', backdropFilter: 'blur(4px)',
      display: 'flex', alignItems: 'center', justifyContent: 'center',
      zIndex: 100, padding: '1.5rem',
    }} onClick={onClose}>
      <div
        style={{
          background: 'var(--surface)', border: '1px solid var(--border)',
          borderRadius: '14px', width: '100%', maxWidth: '660px',
          maxHeight: '80vh', display: 'flex', flexDirection: 'column',
          overflow: 'hidden',
        }}
        onClick={e => e.stopPropagation()}
      >
        {/* Header */}
        <div style={{ padding: '1rem 1.25rem', borderBottom: '1px solid var(--border)', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
          <div style={{ display: 'flex', gap: '0.5rem', alignItems: 'center' }}>
            <StatusBadge status={call.status} />
            <SourceBadge source={call.source} />
            <span style={{ fontSize: '0.8rem', color: 'var(--text-secondary)' }}>
              {timeLabel(call.started_at)} — {duration(call)}
            </span>
          </div>
          <button
            onClick={onClose}
            style={{ background: 'transparent', color: 'var(--text-secondary)', fontSize: '1.2rem', lineHeight: 1 }}
          >
            ✕
          </button>
        </div>

        {/* Transcript */}
        <div style={{ overflowY: 'auto', padding: '1rem 1.25rem', flex: 1 }}>
          {call.transcript.length === 0 ? (
            <p style={{ color: 'var(--text-secondary)', textAlign: 'center', padding: '2rem 0', fontSize: '0.9rem' }}>
              Aucune transcription disponible
            </p>
          ) : (
            call.transcript.map((t, i) => (
              <div key={i} style={{
                display: 'flex',
                flexDirection: t.role === 'user' ? 'row' : 'row-reverse',
                gap: '0.6rem', marginBottom: '0.75rem',
              }}>
                <div style={{
                  fontSize: '0.65rem', fontWeight: 700, color: 'var(--text-secondary)',
                  textTransform: 'uppercase' as const, letterSpacing: '0.06em',
                  minWidth: 48, textAlign: t.role === 'user' ? 'left' : 'right',
                  paddingTop: '0.4rem',
                }}>
                  {t.role === 'user' ? 'Vous' : 'Agent'}
                </div>
                <div style={{
                  background: t.role === 'user' ? 'var(--bg)' : 'rgba(124,131,253,0.12)',
                  border: `1px solid ${t.role === 'user' ? 'var(--border)' : 'rgba(124,131,253,0.3)'}`,
                  borderRadius: '10px', padding: '0.5rem 0.85rem',
                  fontSize: '0.9rem', lineHeight: 1.5, maxWidth: '75%',
                  color: 'var(--text-primary)',
                }}>
                  {t.text}
                  <div style={{ fontSize: '0.65rem', color: 'var(--text-secondary)', marginTop: '0.3rem', textAlign: 'right' }}>
                    {timeLabel(t.ts)}
                  </div>
                </div>
              </div>
            ))
          )}
        </div>
      </div>
    </div>
  );
}

export default function CallHistory() {
  const [calls, setCalls]       = useState<CallRecord[]>([]);
  const [loading, setLoading]   = useState(true);
  const [selected, setSelected] = useState<CallRecord | null>(null);

  const refresh = useCallback(() => {
    fetch('/api/calls')
      .then(r => r.json())
      .then(data => { setCalls(Array.isArray(data) ? data : []); setLoading(false); })
      .catch(() => setLoading(false));
  }, []);

  useEffect(() => {
    refresh();
    // Poll every 5 s so ongoing calls update their status.
    const id = setInterval(refresh, 5000);
    return () => clearInterval(id);
  }, [refresh]);

  return (
    <div style={{ width: '100%' }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'baseline', marginBottom: '1rem' }}>
        <p style={{ fontSize: '0.7rem', fontWeight: 700, textTransform: 'uppercase', letterSpacing: '0.08em', color: 'var(--text-secondary)' }}>
          Historique des appels
        </p>
        <button
          onClick={refresh}
          style={{ background: 'transparent', color: 'var(--accent)', fontSize: '0.8rem', textDecoration: 'underline' }}
        >
          Actualiser
        </button>
      </div>

      {loading ? (
        <p style={{ color: 'var(--text-secondary)', textAlign: 'center', padding: '2rem 0', fontSize: '0.9rem' }}>Chargement…</p>
      ) : calls.length === 0 ? (
        <p style={{ color: 'var(--text-secondary)', textAlign: 'center', padding: '3rem 0', fontSize: '0.9rem' }}>
          Aucun appel enregistré. Les appels apparaissent ici dès leur démarrage.
        </p>
      ) : (
        <div style={{ display: 'flex', flexDirection: 'column', gap: '0.5rem' }}>
          {calls.map(c => (
            <button
              key={c.id}
              onClick={() => setSelected(c)}
              style={{
                width: '100%', textAlign: 'left',
                background: 'var(--surface)', border: '1px solid var(--border)',
                borderRadius: '10px', padding: '0.75rem 1rem',
                display: 'flex', alignItems: 'center', gap: '0.75rem',
                cursor: 'pointer', transition: 'border-color 0.15s ease',
              }}
              onMouseEnter={e => (e.currentTarget.style.borderColor = 'var(--accent)')}
              onMouseLeave={e => (e.currentTarget.style.borderColor = 'var(--border)')}
            >
              {/* Icon */}
              <span style={{ fontSize: '1.1rem' }}>{c.source === 'twilio' ? '📞' : '🌐'}</span>

              {/* Meta */}
              <div style={{ flex: 1, minWidth: 0 }}>
                <div style={{ display: 'flex', gap: '0.5rem', alignItems: 'center', marginBottom: '0.2rem' }}>
                  <StatusBadge status={c.status} />
                  <SourceBadge source={c.source} />
                </div>
                <div style={{ fontSize: '0.8rem', color: 'var(--text-secondary)' }}>
                  {timeLabel(c.started_at)}
                  {' · '}
                  {duration(c)}
                  {' · '}
                  {c.transcript.length} tour{c.transcript.length !== 1 ? 's' : ''}
                </div>
              </div>

              {/* Chevron */}
              <span style={{ color: 'var(--text-secondary)', fontSize: '0.9rem' }}>›</span>
            </button>
          ))}
        </div>
      )}

      {selected && (
        <CallDetail
          call={selected}
          onClose={() => setSelected(null)}
        />
      )}
    </div>
  );
}
