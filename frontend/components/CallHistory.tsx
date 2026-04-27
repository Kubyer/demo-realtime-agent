'use client';

import { useState, useEffect } from 'react';

interface TurnEntry { role: string; text: string; ts: number; }
interface CallRecord {
  id: string;
  source: string;
  status: string;
  started_at: number;
  ended_at: number | null;
  transcript: TurnEntry[];
}

function formatDuration(startMs: number, endMs: number | null): string {
  const ms = Math.max(0, (endMs ?? Date.now()) - startMs);
  const s  = Math.floor(ms / 1000);
  return `${String(Math.floor(s / 60)).padStart(2, '0')}:${String(s % 60).padStart(2, '0')}`;
}

function formatTimestamp(ms: number): string {
  const d   = new Date(ms);
  const now = new Date();
  const yes = new Date(now);
  yes.setDate(now.getDate() - 1);
  const t = d.toLocaleTimeString('en-US', { hour: '2-digit', minute: '2-digit' });
  if (d.toDateString() === now.toDateString()) return `Today, ${t}`;
  if (d.toDateString() === yes.toDateString()) return `Yesterday, ${t}`;
  return d.toLocaleDateString('en-US', { month: 'short', day: 'numeric' }) + `, ${t}`;
}

function formatAvgDuration(calls: CallRecord[]): string {
  const done = calls.filter(c => c.status === 'done' && c.ended_at);
  if (!done.length) return '--:--';
  const avg = done.reduce((s, c) => s + (c.ended_at! - c.started_at), 0) / done.length;
  const sec = Math.floor(avg / 1000);
  return `${String(Math.floor(sec / 60)).padStart(2, '0')}:${String(sec % 60).padStart(2, '0')}`;
}

// ── Transcript modal ──────────────────────────────────────────────────────────
function TranscriptModal({ call, onClose }: { call: CallRecord; onClose: () => void }) {
  return (
    <div
      className="fixed inset-0 bg-black/40 z-[80] flex items-center justify-center p-4"
      onClick={e => e.target === e.currentTarget && onClose()}
    >
      <div className="bg-white rounded-xl w-full max-w-2xl max-h-[80vh] flex flex-col shadow-2xl border border-outline-variant">
        <div className="flex items-center justify-between px-6 py-4 border-b border-outline-variant shrink-0">
          <div>
            <p className="font-semibold text-on-surface text-[16px]">
              {call.source === 'twilio' ? 'Phone Call' : 'Browser Session'}
            </p>
            <p className="text-[12px] text-on-surface-variant mt-0.5">
              {formatTimestamp(call.started_at)} · {call.transcript.length} turns
            </p>
          </div>
          <button
            onClick={onClose}
            className="p-1.5 rounded-full hover:bg-surface-container text-on-surface-variant transition-colors"
          >
            <span className="material-symbols-outlined text-[20px]">close</span>
          </button>
        </div>
        <div className="flex-1 overflow-y-auto p-6 flex flex-col gap-3">
          {call.transcript.length === 0 ? (
            <p className="text-center text-on-surface-variant text-[14px] py-10">No transcript available yet.</p>
          ) : (
            call.transcript.map((turn, i) => (
              <div key={i} className={`flex ${turn.role === 'user' ? 'justify-end' : 'justify-start'}`}>
                <div className={`max-w-[75%] rounded-xl px-4 py-2.5 ${
                  turn.role === 'user' ? 'bg-primary text-on-primary' : 'bg-surface-container text-on-surface'
                }`}>
                  <p className="text-[14px] leading-relaxed">{turn.text}</p>
                  <p className={`text-[11px] mt-1 ${turn.role === 'user' ? 'opacity-70' : 'text-on-surface-variant'}`}>
                    {new Date(turn.ts).toLocaleTimeString('en-US', { hour: '2-digit', minute: '2-digit' })}
                  </p>
                </div>
              </div>
            ))
          )}
        </div>
      </div>
    </div>
  );
}

// ── Main component ────────────────────────────────────────────────────────────
interface Props { searchQuery?: string; }

export default function CallHistory({ searchQuery = '' }: Props) {
  const [calls, setCalls]               = useState<CallRecord[]>([]);
  const [selectedCall, setSelectedCall] = useState<CallRecord | null>(null);

  useEffect(() => {
    const load = () =>
      fetch('/api/calls').then(r => r.json()).then(setCalls).catch(() => {});
    load();
    const id = setInterval(load, 5000);
    return () => clearInterval(id);
  }, []);

  const filtered = searchQuery
    ? calls.filter(c =>
        c.id.toLowerCase().includes(searchQuery.toLowerCase()) ||
        c.source.includes(searchQuery.toLowerCase()) ||
        c.transcript.some(t => t.text.toLowerCase().includes(searchQuery.toLowerCase()))
      )
    : calls;

  const active = calls.filter(c => c.status === 'ongoing').length;
  const pct    = calls.length
    ? Math.round((calls.filter(c => c.status === 'done').length / calls.length) * 100)
    : 0;

  return (
    <>
      {/* Stats row */}
      <div className="grid grid-cols-2 md:grid-cols-4 gap-4 mb-6">
        {[
          { label: 'Total Calls',  value: calls.length.toLocaleString(), color: 'text-on-surface' },
          { label: 'Completed',    value: `${pct}%`,                     color: 'text-secondary' },
          { label: 'Avg Duration', value: formatAvgDuration(calls),      color: 'text-on-surface' },
          { label: 'Active Now',   value: String(active),                color: active > 0 ? 'text-primary' : 'text-on-surface' },
        ].map(stat => (
          <div key={stat.label} className="bg-surface rounded-xl p-md border border-outline-variant shadow-[0px_4px_12px_rgba(15,23,42,0.05)]">
            <p className="text-[11px] font-bold uppercase tracking-widest text-on-surface-variant mb-1">{stat.label}</p>
            <p className={`text-[32px] font-bold leading-none tracking-tight ${stat.color}`}>{stat.value}</p>
          </div>
        ))}
      </div>

      {/* Table card */}
      <div
        className="bg-surface rounded-xl border border-outline-variant shadow-[0px_4px_12px_rgba(15,23,42,0.05)] overflow-hidden flex flex-col"
        style={{ height: 'calc(100vh - 280px)' }}
      >
        <div className="overflow-auto flex-1">
          <table className="w-full text-left border-collapse">
            <thead className="bg-surface-container text-[11px] font-bold uppercase tracking-widest text-on-surface-variant sticky top-0 z-10 border-b border-outline-variant">
              <tr>
                <th className="px-md py-3">Session</th>
                <th className="px-md py-3">ID</th>
                <th className="px-md py-3">Source</th>
                <th className="px-md py-3">Turns</th>
                <th className="px-md py-3">Duration</th>
                <th className="px-md py-3">Time / Status</th>
                <th className="px-md py-3 text-right">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-outline-variant">
              {filtered.length === 0 && (
                <tr>
                  <td colSpan={7} className="px-md py-10 text-center text-on-surface-variant text-[14px]">
                    {calls.length === 0
                      ? 'No calls yet — start a browser session or call the Twilio number.'
                      : 'No results match your search.'}
                  </td>
                </tr>
              )}
              {filtered.map(call => (
                <tr
                  key={call.id}
                  onClick={() => setSelectedCall(call)}
                  className="hover:bg-surface-container-lowest transition-colors group cursor-pointer"
                >
                  <td className="px-md py-sm">
                    <div className="flex items-center gap-3">
                      <div className={`w-8 h-8 rounded-[50%] flex items-center justify-center shrink-0 ${
                        call.source === 'twilio'
                          ? 'bg-primary-fixed text-primary'
                          : 'bg-surface-variant text-on-surface-variant'
                      }`}>
                        <span className="material-symbols-outlined text-[18px]">
                          {call.source === 'twilio' ? 'call' : 'computer'}
                        </span>
                      </div>
                      <div>
                        <p className="font-medium text-on-surface text-[14px]">
                          {call.source === 'twilio' ? 'Phone Call' : 'Browser Session'}
                        </p>
                        <p className="text-[12px] text-on-surface-variant">{formatTimestamp(call.started_at)}</p>
                      </div>
                    </div>
                  </td>
                  <td className="px-md py-sm text-on-surface-variant font-mono text-[12px]">
                    {call.id.length > 18 ? call.id.slice(0, 18) + '…' : call.id}
                  </td>
                  <td className="px-md py-sm">
                    <div className={`flex items-center gap-1.5 text-[13px] ${call.source === 'twilio' ? 'text-primary' : 'text-on-surface-variant'}`}>
                      <span className="material-symbols-outlined text-[16px]">
                        {call.source === 'twilio' ? 'call_received' : 'laptop_chromebook'}
                      </span>
                      {call.source === 'twilio' ? 'Twilio' : 'Browser'}
                    </div>
                  </td>
                  <td className="px-md py-sm font-mono text-on-surface text-[14px]">{call.transcript.length}</td>
                  <td className="px-md py-sm font-mono text-on-surface text-[14px]">{formatDuration(call.started_at, call.ended_at)}</td>
                  <td className="px-md py-sm">
                    {call.status === 'ongoing' ? (
                      <span className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full bg-secondary/10 border border-secondary text-secondary text-[11px] font-bold uppercase tracking-wide">
                        <span className="w-1.5 h-1.5 rounded-full bg-secondary animate-pulse" />
                        Ongoing
                      </span>
                    ) : (
                      <span className="text-on-surface-variant text-[13px]">
                        {formatTimestamp(call.ended_at ?? call.started_at)}
                      </span>
                    )}
                  </td>
                  <td className="px-md py-sm text-right">
                    <button className="opacity-0 group-hover:opacity-100 p-1.5 text-on-surface-variant hover:text-primary transition-all rounded">
                      <span className="material-symbols-outlined text-[20px]">open_in_new</span>
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>

        {/* Footer */}
        <div className="border-t border-outline-variant bg-surface-container-lowest px-md py-sm flex items-center justify-between shrink-0">
          <p className="text-[13px] text-on-surface-variant">
            Showing {filtered.length} of {calls.length} {calls.length === 1 ? 'call' : 'calls'}
          </p>
          <div className="flex items-center gap-1">
            <button disabled className="p-1.5 rounded text-on-surface-variant disabled:opacity-40">
              <span className="material-symbols-outlined text-[20px]">chevron_left</span>
            </button>
            <button disabled className="p-1.5 rounded text-on-surface-variant disabled:opacity-40">
              <span className="material-symbols-outlined text-[20px]">chevron_right</span>
            </button>
          </div>
        </div>
      </div>

      {selectedCall && (
        <TranscriptModal call={selectedCall} onClose={() => setSelectedCall(null)} />
      )}
    </>
  );
}
