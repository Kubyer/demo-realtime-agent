'use client';

import { useState, useEffect, useRef } from 'react';

interface TurnEntry { role: string; text: string; ts: number; audio_start_ms?: number; tts_latency?: number; ttft_ms?: number; e2e_ms?: number; }
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
  const s = Math.floor(ms / 1000);
  return `${String(Math.floor(s / 60)).padStart(2, '0')}:${String(s % 60).padStart(2, '0')}`;
}

function formatTimestamp(ms: number): string {
  const d = new Date(ms);
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
  const audioRef = useRef<HTMLAudioElement | null>(null);
  const [isPlaying, setIsPlaying] = useState(false);
  const [currentTimeMs, setCurrentTimeMs] = useState(0);
  const [durationMs, setDurationMs] = useState(0);
  const [playbackRate, setPlaybackRate] = useState(1);

  useEffect(() => {
    if (!audioRef.current) return;
    const a = audioRef.current;
    const updateTime = () => setCurrentTimeMs(a.currentTime * 1000);
    const updateDuration = () => {
      if (isFinite(a.duration)) setDurationMs(a.duration * 1000);
    };
    const onPlay = () => setIsPlaying(true);
    const onPause = () => setIsPlaying(false);
    
    a.addEventListener('timeupdate', updateTime);
    a.addEventListener('loadedmetadata', updateDuration);
    a.addEventListener('durationchange', updateDuration);
    a.addEventListener('play', onPlay);
    a.addEventListener('pause', onPause);
    
    return () => {
      a.removeEventListener('timeupdate', updateTime);
      a.removeEventListener('loadedmetadata', updateDuration);
      a.removeEventListener('durationchange', updateDuration);
      a.removeEventListener('play', onPlay);
      a.removeEventListener('pause', onPause);
    };
  }, []);

  const togglePlay = () => {
    if (!audioRef.current) return;
    if (isPlaying) audioRef.current.pause();
    else audioRef.current.play();
  };

  const cycleSpeed = () => {
    if (!audioRef.current) return;
    const next = playbackRate === 1 ? 1.5 : playbackRate === 1.5 ? 2 : 1;
    audioRef.current.playbackRate = next;
    setPlaybackRate(next);
  };

  const skip = (delta: number) => {
    if (!audioRef.current) return;
    audioRef.current.currentTime += delta;
  };

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
        
        {call.status === 'done' && (
          <div className="px-6 py-4 border-b border-outline-variant bg-surface-container-lowest flex items-center justify-between shrink-0">
            <audio ref={audioRef} src={`/recordings/${call.id}.wav`} preload="metadata" />
            <div className="flex items-center gap-4">
              <button onClick={togglePlay} className="w-10 h-10 rounded-full bg-primary text-on-primary flex items-center justify-center hover:opacity-90 transition-opacity shadow-sm">
                <span className="material-symbols-outlined text-[24px]">
                  {isPlaying ? 'pause' : 'play_arrow'}
                </span>
              </button>
              <button onClick={cycleSpeed} className="text-[13px] font-bold w-10 text-on-surface hover:text-primary transition-colors">
                {playbackRate}x
              </button>
              <div className="flex items-center gap-1">
                <button onClick={() => skip(-10)} className="p-1.5 rounded-full hover:bg-surface-variant text-on-surface transition-colors" title="Rewind 10s">
                  <span className="material-symbols-outlined text-[20px]">replay_10</span>
                </button>
                <button onClick={() => skip(10)} className="p-1.5 rounded-full hover:bg-surface-variant text-on-surface transition-colors" title="Forward 10s">
                  <span className="material-symbols-outlined text-[20px]">forward_10</span>
                </button>
              </div>
            </div>
            
            <div className="flex items-center gap-3">
              <div className="h-1.5 bg-surface-variant rounded-full w-48 overflow-hidden relative cursor-pointer" onClick={(e) => {
                if (!audioRef.current || !durationMs) return;
                const rect = e.currentTarget.getBoundingClientRect();
                const x = e.clientX - rect.left;
                audioRef.current.currentTime = (x / rect.width) * (durationMs / 1000);
              }}>
                <div className="absolute top-0 left-0 bottom-0 bg-primary pointer-events-none" style={{ width: `${durationMs ? (currentTimeMs / durationMs) * 100 : 0}%` }} />
              </div>
              <span className="text-[12px] font-mono text-on-surface-variant">
                {formatDuration(0, currentTimeMs)} / {formatDuration(0, durationMs)}
              </span>
            </div>
          </div>
        )}
        <div className="flex-1 overflow-y-auto p-6 flex flex-col gap-3">
          {call.transcript.length === 0 ? (
            <p className="text-center text-on-surface-variant text-[14px] py-10">No transcript available yet.</p>
          ) : (
            call.transcript.map((turn, i) => {
              const nextTurn = call.transcript[i + 1];
              const isActive = isPlaying && currentTimeMs >= (turn.audio_start_ms || 0) && (!nextTurn || currentTimeMs < (nextTurn.audio_start_ms || Infinity));
              const activeBorder = isActive ? 'border-primary shadow-md' : 'border-transparent';

              if (turn.role === 'tool_call' || turn.role === 'tool_result') {
                const isCall = turn.role === 'tool_call';
                const accent = isCall ? '#6366f1' : '#10b981';
                const bg = isCall ? 'rgba(99,102,241,0.06)' : 'rgba(16,185,129,0.06)';
                const borderColor = isCall ? 'rgba(99,102,241,0.2)' : 'rgba(16,185,129,0.2)';

                let parsed: any = {};
                try { parsed = JSON.parse(turn.text); } catch { }
                const label = parsed.name || 'API Call';

                let payloadRaw = isCall ? parsed.arguments : parsed.result;
                let payloadStr = typeof payloadRaw === 'string' ? payloadRaw : JSON.stringify(payloadRaw, null, 2);
                try {
                  if (typeof payloadRaw === 'string') payloadStr = JSON.stringify(JSON.parse(payloadRaw), null, 2);
                } catch { }

                return (
                  <div key={i} className="flex justify-center my-1">
                    <div style={{ background: bg, border: isActive ? undefined : `1px solid ${borderColor}` }} className={`w-full max-w-[85%] rounded-lg p-3 border-2 transition-all ${isActive ? 'border-primary shadow-md' : 'border-transparent'}`}>
                      <div className="flex items-center gap-2 mb-2">
                        <span className="material-symbols-outlined" style={{ fontSize: 15, color: accent }}>api</span>
                        <span className="text-[12px] font-bold text-slate-800 flex-1">{label}</span>
                        <span style={{ color: accent, background: `${accent}18` }} className="text-[10px] font-bold uppercase tracking-wider px-1.5 py-0.5 rounded">
                          {isCall ? 'CALL' : 'RESULT'}
                        </span>
                        <span className="text-[11px] text-slate-500 font-mono">
                          {new Date(turn.ts).toLocaleTimeString('en-US', { hour: '2-digit', minute: '2-digit', second: '2-digit' })}
                        </span>
                      </div>
                      <pre className="text-[10px] text-slate-500 font-mono whitespace-pre-wrap break-all bg-slate-900 text-slate-300 p-2 rounded">
                        {payloadStr}
                      </pre>
                    </div>
                  </div>
                );
              }

              return (
                <div key={i} className={`flex ${turn.role === 'user' ? 'justify-end' : 'justify-start'}`}>
                  <div className={`max-w-[75%] rounded-xl px-4 py-2.5 border-2 transition-all ${activeBorder} ${turn.role === 'user' ? 'bg-primary text-on-primary' : 'bg-surface-container text-on-surface'}`}>
                    <p className="text-[14px] leading-relaxed">{turn.text}</p>
                    <div className="flex items-center gap-2 mt-1">
                      <p className={`text-[11px] ${turn.role === 'user' ? 'opacity-70' : 'text-on-surface-variant'}`}>
                        {new Date(turn.ts).toLocaleTimeString('en-US', { hour: '2-digit', minute: '2-digit' })}
                      </p>
                      {turn.ttft_ms != null && (
                        <span className="inline-flex items-center gap-1 bg-surface text-on-surface px-1.5 py-[1px] rounded text-[10px] font-bold border border-outline-variant shadow-sm" title="Time to First Token">
                          TTFT <span className="text-primary">{turn.ttft_ms}ms</span>
                        </span>
                      )}
                      {turn.tts_latency != null && (
                        <span className="inline-flex items-center gap-1 bg-surface text-on-surface px-1.5 py-[1px] rounded text-[10px] font-bold border border-outline-variant shadow-sm" title="Time to First Audio">
                          TTFA <span className="text-secondary">{turn.tts_latency}ms</span>
                        </span>
                      )}
                      {turn.e2e_ms != null && (
                        <span className="inline-flex items-center gap-1 bg-surface text-on-surface px-1.5 py-[1px] rounded text-[10px] font-bold border border-outline-variant shadow-sm" title="End-to-End Latency">
                          E2E <span className="text-amber-500">{turn.e2e_ms}ms</span>
                        </span>
                      )}
                    </div>
                  </div>
                </div>
              );
            })
          )}
        </div>
      </div>
    </div>
  );
}

// ── Main component ────────────────────────────────────────────────────────────
interface Props { searchQuery?: string; }

export default function CallHistory({ searchQuery = '' }: Props) {
  const [calls, setCalls] = useState<CallRecord[]>([]);
  const [selectedCall, setSelectedCall] = useState<CallRecord | null>(null);

  useEffect(() => {
    const load = () =>
      fetch('/api/calls').then(r => r.json()).then(setCalls).catch(() => { });
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
  const pct = calls.length
    ? Math.round((calls.filter(c => c.status === 'done').length / calls.length) * 100)
    : 0;

  return (
    <>
      {/* Stats row */}
      <div className="grid grid-cols-2 md:grid-cols-4 gap-4 mb-6">
        {[
          { label: 'Total Calls', value: calls.length.toLocaleString(), color: 'text-on-surface' },
          { label: 'Completed', value: `${pct}%`, color: 'text-secondary' },
          { label: 'Avg Duration', value: formatAvgDuration(calls), color: 'text-on-surface' },
          { label: 'Active Now', value: String(active), color: active > 0 ? 'text-primary' : 'text-on-surface' },
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
                      <div className={`w-8 h-8 rounded-[50%] flex items-center justify-center shrink-0 ${call.source === 'twilio'
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
