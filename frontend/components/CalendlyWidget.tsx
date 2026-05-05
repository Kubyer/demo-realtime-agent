'use client';

import { useState, useEffect, useCallback } from 'react';

interface CalendlyEvent {
  id: string;
  name: string;
  start_time: string;
  end_time: string;
  status: string;
}

function apiUrl(path: string): string {
  const base = process.env.NEXT_PUBLIC_BACKEND_URL;
  if (base) return `${base.replace(/\/$/, '')}${path}`;
  return path;
}

// Returns the Monday of the week containing `date`.
function weekStart(date: Date): Date {
  const d = new Date(date);
  const day = d.getDay();
  const diff = day === 0 ? -6 : 1 - day;
  d.setDate(d.getDate() + diff);
  d.setHours(0, 0, 0, 0);
  return d;
}

function formatDate(d: Date): string {
  const yyyy = d.getFullYear();
  const mm = String(d.getMonth() + 1).padStart(2, '0');
  const dd = String(d.getDate()).padStart(2, '0');
  return `${yyyy}-${mm}-${dd}`;
}

function sameDay(a: Date, b: Date): boolean {
  return a.getFullYear() === b.getFullYear() &&
    a.getMonth() === b.getMonth() &&
    a.getDate() === b.getDate();
}

function timeStr(iso: string): string {
  return new Date(iso).toLocaleTimeString('fr-FR', { hour: '2-digit', minute: '2-digit', hour12: false });
}

const DAYS_FR = ['Lun', 'Mar', 'Mer', 'Jeu', 'Ven', 'Sam', 'Dim'];
const HOURS = [8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18];

interface Props {
  // Bump this to force a refresh (e.g. after a tool_call event)
  refreshKey?: number;
}

export default function CalendlyWidget({ refreshKey = 0 }: Props) {
  const [baseDate, setBaseDate] = useState(() => weekStart(new Date()));
  const [events, setEvents]     = useState<CalendlyEvent[]>([]);
  const [loading, setLoading]   = useState(true);
  const [error, setError]       = useState<string | null>(null);

  const load = useCallback(() => {
    setLoading(true);
    setError(null);
    fetch(apiUrl(`/api/calendly/events?date=${formatDate(baseDate)}`))
      .then(r => r.json())
      .then((data: CalendlyEvent[]) => {
        setEvents(data ?? []);
        setLoading(false);
      })
      .catch(() => {
        setError('Impossible de charger le calendrier');
        setLoading(false);
      });
  }, [baseDate]);

  useEffect(() => { load(); }, [load, refreshKey]);

  const weekDays = Array.from({ length: 7 }, (_, i) => {
    const d = new Date(baseDate);
    d.setDate(d.getDate() + i);
    return d;
  });

  const today = new Date();

  function eventsForDay(day: Date): CalendlyEvent[] {
    return events.filter(e => sameDay(new Date(e.start_time), day));
  }

  function eventTopPct(iso: string): number {
    const d = new Date(iso);
    const mins = (d.getHours() - 8) * 60 + d.getMinutes();
    return (mins / (10 * 60)) * 100;
  }

  function eventHeightPct(start: string, end: string): number {
    const s = new Date(start);
    const e = new Date(end);
    const mins = (e.getTime() - s.getTime()) / 60000;
    return (mins / (10 * 60)) * 100;
  }

  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100%', background: '#fff', minHeight: 0 }}>
      {/* Toolbar */}
      <div style={{
        display: 'flex',
        alignItems: 'center',
        gap: '0.5rem',
        padding: '0.6rem 0.75rem',
        borderBottom: '1px solid #e2e8f0',
        background: '#fff',
        flexShrink: 0,
      }}>
        <button
          onClick={() => setBaseDate(d => { const n = new Date(d); n.setDate(n.getDate() - 7); return weekStart(n); })}
          style={{ padding: '4px 6px', borderRadius: '6px', border: '1px solid #e2e8f0', background: '#f8fafc', color: '#64748b', cursor: 'pointer', fontSize: 13 }}
        >‹</button>
        <span style={{ flex: 1, textAlign: 'center', fontSize: '0.75rem', fontWeight: 600, color: '#334155' }}>
          {baseDate.toLocaleDateString('fr-FR', { day: 'numeric', month: 'long' })}
          {' – '}
          {weekDays[6].toLocaleDateString('fr-FR', { day: 'numeric', month: 'long', year: 'numeric' })}
        </span>
        <button
          onClick={() => setBaseDate(d => { const n = new Date(d); n.setDate(n.getDate() + 7); return weekStart(n); })}
          style={{ padding: '4px 6px', borderRadius: '6px', border: '1px solid #e2e8f0', background: '#f8fafc', color: '#64748b', cursor: 'pointer', fontSize: 13 }}
        >›</button>
        <button
          onClick={load}
          title="Actualiser"
          style={{ padding: '4px 6px', borderRadius: '6px', border: '1px solid #e2e8f0', background: '#f8fafc', color: '#64748b', cursor: 'pointer' }}
        >
          <span className="material-symbols-outlined" style={{ fontSize: 15, display: 'block' }}>refresh</span>
        </button>
        <button
          onClick={() => setBaseDate(weekStart(new Date()))}
          style={{ padding: '3px 8px', borderRadius: '6px', border: '1px solid #e2e8f0', background: '#f8fafc', color: '#3b82f6', cursor: 'pointer', fontSize: '0.68rem', fontWeight: 600 }}
        >
          Auj.
        </button>
      </div>

      {/* Status */}
      {loading && (
        <div style={{ textAlign: 'center', padding: '0.5rem', fontSize: '0.72rem', color: '#94a3b8' }}>
          Chargement…
        </div>
      )}
      {error && (
        <div style={{ textAlign: 'center', padding: '0.5rem', fontSize: '0.72rem', color: '#ef4444' }}>
          {error}
        </div>
      )}

      {/* Calendar grid */}
      <div style={{ flex: 1, overflowY: 'auto', minHeight: 0 }}>
        {/* Day headers */}
        <div style={{ display: 'grid', gridTemplateColumns: '36px repeat(7, 1fr)', borderBottom: '1px solid #e2e8f0', position: 'sticky', top: 0, background: '#fff', zIndex: 2 }}>
          <div />
          {weekDays.map((day, i) => {
            const isToday = sameDay(day, today);
            return (
              <div key={i} style={{
                padding: '6px 2px',
                textAlign: 'center',
                borderLeft: '1px solid #f1f5f9',
              }}>
                <div style={{ fontSize: '0.6rem', fontWeight: 600, color: '#94a3b8', textTransform: 'uppercase' }}>{DAYS_FR[i]}</div>
                <div style={{
                  width: 26,
                  height: 26,
                  borderRadius: '50%',
                  margin: '2px auto 0',
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'center',
                  fontSize: '0.78rem',
                  fontWeight: isToday ? 700 : 500,
                  background: isToday ? '#3b82f6' : 'transparent',
                  color: isToday ? '#fff' : '#334155',
                }}>
                  {day.getDate()}
                </div>
              </div>
            );
          })}
        </div>

        {/* Time slots */}
        <div style={{ display: 'grid', gridTemplateColumns: '36px repeat(7, 1fr)', position: 'relative' }}>
          {/* Hour labels */}
          <div style={{ gridColumn: '1', gridRow: '1 / -1' }}>
            {HOURS.map(h => (
              <div key={h} style={{
                height: 52,
                display: 'flex',
                alignItems: 'flex-start',
                justifyContent: 'flex-end',
                paddingRight: '4px',
                paddingTop: '2px',
                fontSize: '0.6rem',
                color: '#94a3b8',
                fontFamily: 'monospace',
              }}>
                {h}h
              </div>
            ))}
          </div>

          {/* Day columns */}
          {weekDays.map((day, di) => {
            const dayEvts = eventsForDay(day);
            const isToday = sameDay(day, today);
            return (
              <div
                key={di}
                style={{
                  position: 'relative',
                  borderLeft: '1px solid #f1f5f9',
                  background: isToday ? 'rgba(59,130,246,0.02)' : 'transparent',
                }}
              >
                {/* Hour lines */}
                {HOURS.map(h => (
                  <div key={h} style={{ height: 52, borderTop: '1px solid #f1f5f9' }} />
                ))}

                {/* Events overlay */}
                {dayEvts.map(evt => {
                  const top    = eventTopPct(evt.start_time);
                  const height = Math.max(eventHeightPct(evt.start_time, evt.end_time), 4);
                  return (
                    <div
                      key={evt.id}
                      title={`${evt.name}\n${timeStr(evt.start_time)} – ${timeStr(evt.end_time)}`}
                      style={{
                        position: 'absolute',
                        top: `${top}%`,
                        left: '2px',
                        right: '2px',
                        height: `${height}%`,
                        background: 'rgba(59,130,246,0.85)',
                        borderRadius: '4px',
                        padding: '2px 4px',
                        overflow: 'hidden',
                        cursor: 'default',
                        zIndex: 1,
                      }}
                    >
                      <p style={{ fontSize: '0.6rem', fontWeight: 700, color: '#fff', margin: 0, lineHeight: 1.2, whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>
                        {timeStr(evt.start_time)} {evt.name}
                      </p>
                    </div>
                  );
                })}
              </div>
            );
          })}
        </div>
      </div>

      {/* Footer: event count */}
      <div style={{
        padding: '0.4rem 0.75rem',
        borderTop: '1px solid #e2e8f0',
        fontSize: '0.68rem',
        color: '#94a3b8',
        flexShrink: 0,
        background: '#fafafa',
      }}>
        {events.length} rendez-vous cette semaine
      </div>
    </div>
  );
}
