'use client';

import { useState, useEffect } from 'react';

interface TurnEntry { role: string; e2e_ms?: number; tts_latency?: number; }
interface CallRecord {
  id: string;
  source: string;
  status: string;
  started_at: number;
  ended_at: number | null;
  transcript: TurnEntry[];
}

function formatDuration(ms: number): string {
  const s = Math.floor(ms / 1000);
  return `${String(Math.floor(s / 60)).padStart(2, '0')}:${String(s % 60).padStart(2, '0')}`;
}

function formatTimestamp(ms: number): string {
  const d = new Date(ms);
  const now = new Date();
  const t = d.toLocaleTimeString('en-US', { hour: '2-digit', minute: '2-digit' });
  if (d.toDateString() === now.toDateString()) return `Today, ${t}`;
  return d.toLocaleDateString('en-US', { month: 'short', day: 'numeric' }) + `, ${t}`;
}

function StatCard({ label, value, sub, color = 'text-on-surface' }: { label: string; value: string; sub?: string; color?: string }) {
  return (
    <div className="bg-surface rounded-xl p-md border border-outline-variant shadow-[0px_4px_12px_rgba(15,23,42,0.05)]">
      <p className="text-[11px] font-bold uppercase tracking-widest text-on-surface-variant mb-1">{label}</p>
      <p className={`text-[32px] font-bold leading-none tracking-tight ${color}`}>{value}</p>
      {sub && <p className="text-[11px] text-on-surface-variant mt-1">{sub}</p>}
    </div>
  );
}

export default function DashboardView() {
  const [calls, setCalls] = useState<CallRecord[]>([]);

  useEffect(() => {
    const load = () => fetch('/api/calls').then(r => r.json()).then(data => setCalls(Array.isArray(data) ? data : [])).catch(() => {});
    load();
    const id = setInterval(load, 5000);
    return () => clearInterval(id);
  }, []);

  const done = calls.filter(c => c.status === 'done' && c.ended_at);
  const active = calls.filter(c => c.status === 'ongoing').length;

  const avgDurationMs = done.length
    ? done.reduce((s, c) => s + (c.ended_at! - c.started_at), 0) / done.length
    : 0;

  const allTTFAs = calls.flatMap(c =>
    c.transcript.filter(t => t.role === 'assistant' && t.e2e_ms != null).map(t => t.e2e_ms!)
  );
  const avgTTFA = allTTFAs.length ? Math.round(allTTFAs.reduce((a, b) => a + b, 0) / allTTFAs.length) : null;

  const completionPct = calls.length ? Math.round((done.length / calls.length) * 100) : 0;

  // Last 7 days call volume
  const now = Date.now();
  const dayMs = 86400000;
  const days = Array.from({ length: 7 }, (_, i) => {
    const start = now - (6 - i) * dayMs;
    const end = start + dayMs;
    const label = new Date(start).toLocaleDateString('en-US', { weekday: 'short' });
    const count = calls.filter(c => c.started_at >= start && c.started_at < end).length;
    return { label, count };
  });
  const maxDay = Math.max(...days.map(d => d.count), 1);

  const recent = calls.slice(0, 8);

  return (
    <div className="flex flex-col gap-6">

      {/* Stats row */}
      <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
        <StatCard label="Total Calls" value={calls.length.toLocaleString()} />
        <StatCard label="Completed" value={`${completionPct}%`} color="text-secondary" />
        <StatCard label="Avg Duration" value={avgDurationMs ? formatDuration(avgDurationMs) : '--:--'} />
        <StatCard
          label="Avg Response"
          value={avgTTFA != null ? `${avgTTFA}ms` : '—'}
          sub="end-to-end latency"
          color={avgTTFA != null && avgTTFA > 2000 ? 'text-amber-500' : 'text-on-surface'}
        />
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-3 gap-4">

        {/* Call volume chart */}
        <div className="lg:col-span-2 bg-surface rounded-xl border border-outline-variant p-md shadow-[0px_4px_12px_rgba(15,23,42,0.05)]">
          <p className="text-[11px] font-bold uppercase tracking-widest text-on-surface-variant mb-4">Call Volume — Last 7 Days</p>
          <div className="flex items-end gap-2 h-28">
            {days.map(d => (
              <div key={d.label} className="flex-1 flex flex-col items-center gap-1">
                <span className="text-[11px] font-bold text-on-surface-variant">{d.count || ''}</span>
                <div
                  className="w-full rounded-t-md bg-primary transition-all"
                  style={{ height: `${Math.max(4, (d.count / maxDay) * 80)}px`, opacity: d.count ? 1 : 0.15 }}
                />
                <span className="text-[10px] text-on-surface-variant">{d.label}</span>
              </div>
            ))}
          </div>
        </div>

        {/* Live status */}
        <div className="bg-surface rounded-xl border border-outline-variant p-md shadow-[0px_4px_12px_rgba(15,23,42,0.05)] flex flex-col gap-3">
          <p className="text-[11px] font-bold uppercase tracking-widest text-on-surface-variant">Live Status</p>
          <div className="flex items-center gap-3">
            <div className={`w-10 h-10 rounded-full flex items-center justify-center ${active > 0 ? 'bg-secondary/10' : 'bg-surface-container'}`}>
              <span className={`material-symbols-outlined text-[20px] ${active > 0 ? 'text-secondary' : 'text-on-surface-variant'}`}
                style={active > 0 ? { fontVariationSettings: "'FILL' 1" } : {}}>
                phone_in_talk
              </span>
            </div>
            <div>
              <p className={`text-[24px] font-bold leading-none ${active > 0 ? 'text-secondary' : 'text-on-surface'}`}>{active}</p>
              <p className="text-[12px] text-on-surface-variant">{active === 1 ? 'active call' : 'active calls'}</p>
            </div>
          </div>
          <div className="border-t border-outline-variant pt-3 flex flex-col gap-2">
            {[
              { label: 'Browser sessions', value: calls.filter(c => c.source === 'browser').length },
              { label: 'Twilio calls', value: calls.filter(c => c.source === 'twilio').length },
            ].map(row => (
              <div key={row.label} className="flex justify-between text-[13px]">
                <span className="text-on-surface-variant">{row.label}</span>
                <span className="font-bold text-on-surface">{row.value}</span>
              </div>
            ))}
          </div>
        </div>
      </div>

      {/* Recent calls */}
      <div className="bg-surface rounded-xl border border-outline-variant shadow-[0px_4px_12px_rgba(15,23,42,0.05)] overflow-hidden">
        <div className="px-md py-3 border-b border-outline-variant">
          <p className="text-[11px] font-bold uppercase tracking-widest text-on-surface-variant">Recent Calls</p>
        </div>
        <table className="w-full text-left border-collapse">
          <thead className="bg-surface-container text-[11px] font-bold uppercase tracking-widest text-on-surface-variant border-b border-outline-variant">
            <tr>
              <th className="px-md py-2.5">Session</th>
              <th className="px-md py-2.5">Source</th>
              <th className="px-md py-2.5">Turns</th>
              <th className="px-md py-2.5">Duration</th>
              <th className="px-md py-2.5">Status</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-outline-variant">
            {recent.length === 0 && (
              <tr>
                <td colSpan={5} className="px-md py-8 text-center text-on-surface-variant text-[13px]">
                  No calls yet — start a browser session or dial from Outreach.
                </td>
              </tr>
            )}
            {recent.map(call => (
              <tr key={call.id} className="hover:bg-surface-container-lowest transition-colors">
                <td className="px-md py-sm">
                  <div>
                    <p className="text-[13px] font-medium text-on-surface">
                      {call.source === 'twilio' ? 'Phone Call' : 'Browser Session'}
                    </p>
                    <p className="text-[11px] text-on-surface-variant font-mono">{call.id.slice(0, 20)}…</p>
                  </div>
                </td>
                <td className="px-md py-sm">
                  <span className={`text-[12px] flex items-center gap-1 ${call.source === 'twilio' ? 'text-primary' : 'text-on-surface-variant'}`}>
                    <span className="material-symbols-outlined text-[14px]">
                      {call.source === 'twilio' ? 'call' : 'laptop_chromebook'}
                    </span>
                    {call.source === 'twilio' ? 'Twilio' : 'Browser'}
                  </span>
                </td>
                <td className="px-md py-sm font-mono text-[13px] text-on-surface">{call.transcript.length}</td>
                <td className="px-md py-sm font-mono text-[13px] text-on-surface">
                  {call.ended_at ? formatDuration(call.ended_at - call.started_at) : '—'}
                </td>
                <td className="px-md py-sm">
                  {call.status === 'ongoing' ? (
                    <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full bg-secondary/10 border border-secondary text-secondary text-[11px] font-bold uppercase">
                      <span className="w-1.5 h-1.5 rounded-full bg-secondary animate-pulse" /> Live
                    </span>
                  ) : (
                    <span className="text-[12px] text-on-surface-variant">{formatTimestamp(call.started_at)}</span>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}
