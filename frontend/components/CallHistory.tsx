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

interface CallConfig {
  source: string;
  stt_provider: string;
  stt_model: string;
  stt_audio_format: string;
  stt_sample_rate: number;
  stt_language?: string;
  stt_endpoint_detection?: boolean;
  llm_provider: string;
  llm_model: string;
  tts_provider: string;
  tts_voice_id: string;
  tts_model?: string;
  tts_stability?: number;
  tts_similarity_boost?: number;
  tts_style?: number;
  tts_speed?: number;
  prompt_hash: string;
  opening_sentence?: string;
  bargein_min_words: number;
}

interface QAAlert { severity: string; code: string; message: string; }

interface CallQAResult {
  session_id: string;
  analyzed_at: number;
  status: string;
  error?: string;
  mos_sig?: number;
  mos_bak?: number;
  mos_ovrl?: number;
  talk_over_rate?: number;
  avg_ttfa_ms?: number;
  turn_count: number;
  bargein_count: number;
  completed: boolean;
  config: CallConfig;
  alerts?: QAAlert[];
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

// ── MOS score bar ─────────────────────────────────────────────────────────────
function MOSBar({ label, value }: { label: string; value: number }) {
  const pct = Math.max(0, Math.min(100, ((value - 1) / 4) * 100));
  const color = value >= 4 ? '#10b981' : value >= 3 ? '#f59e0b' : '#ef4444';
  return (
    <div className="flex items-center gap-3">
      <span className="text-[12px] text-on-surface-variant w-32 shrink-0">{label}</span>
      <div className="flex-1 h-2 bg-surface-variant rounded-full overflow-hidden">
        <div className="h-full rounded-full transition-all" style={{ width: `${pct}%`, background: color }} />
      </div>
      <span className="text-[13px] font-bold font-mono w-8 text-right" style={{ color }}>{value.toFixed(1)}</span>
    </div>
  );
}

// ── QA Panel ──────────────────────────────────────────────────────────────────
// Module-level cache: survives modal open/close within the same page session.
const qaResultCache = new Map<string, CallQAResult>();

function QAPanel({ callId, isDone }: { callId: string; isDone: boolean }) {
  const [qa, setQa] = useState<CallQAResult | null>(() => qaResultCache.get(callId) ?? null);
  const [loading, setLoading] = useState(() => !qaResultCache.has(callId));

  useEffect(() => {
    if (qaResultCache.has(callId)) return; // already loaded — skip fetch
    let cancelled = false;
    const poll = async () => {
      try {
        const r = await fetch(`/api/calls/${callId}/qa`);
        if (cancelled) return;
        if (r.status === 404) { setLoading(false); return; }
        const data: CallQAResult = await r.json();
        if (data.status === 'pending') {
          setTimeout(poll, 2000);
        } else {
          qaResultCache.set(callId, data);
          setQa(data);
          setLoading(false);
        }
      } catch { setLoading(false); }
    };
    poll();
    return () => { cancelled = true; };
  }, [callId]);

  if (loading) return (
    <div className="flex flex-col items-center justify-center py-16 gap-3 text-on-surface-variant">
      <div className="w-6 h-6 rounded-full border-2 border-primary border-t-transparent animate-spin" />
      <p className="text-[13px]">{isDone ? 'Analyzing audio…' : 'Call in progress — QA runs after hangup'}</p>
    </div>
  );

  if (!qa) return (
    <div className="flex flex-col items-center justify-center py-16 gap-2 text-on-surface-variant">
      <span className="material-symbols-outlined text-[32px]">analytics</span>
      <p className="text-[13px]">No QA data available for this call.</p>
    </div>
  );

  const cfg = qa.config;

  return (
    <div className="p-6 flex flex-col gap-5 overflow-y-auto">

      {/* Alerts */}
      {qa.alerts && qa.alerts.length > 0 && (
        <div className="flex flex-col gap-2">
          {qa.alerts.map((a, i) => (
            <div key={i} className={`flex items-start gap-2 px-3 py-2 rounded-lg text-[12px] border ${a.severity === 'critical' ? 'bg-red-50 border-red-200 text-red-700' : 'bg-amber-50 border-amber-200 text-amber-700'}`}>
              <span className="material-symbols-outlined text-[16px] mt-0.5 shrink-0">{a.severity === 'critical' ? 'error' : 'warning'}</span>
              <span><span className="font-bold">{a.code}</span> — {a.message}</span>
            </div>
          ))}
        </div>
      )}

      {/* MOS Scores */}
      {(qa.mos_ovrl != null || qa.mos_sig != null) ? (
        <div className="flex flex-col gap-2">
          <p className="text-[11px] font-bold uppercase tracking-widest text-on-surface-variant mb-1">Voice Quality (DNSMOS)</p>
          {qa.mos_sig != null && <MOSBar label="Signal clarity" value={qa.mos_sig} />}
          {qa.mos_bak != null && <MOSBar label="Background noise" value={qa.mos_bak} />}
          {qa.mos_ovrl != null && <MOSBar label="Overall naturalness" value={qa.mos_ovrl} />}
        </div>
      ) : (
        <div className="bg-surface-container rounded-lg px-4 py-3 text-[12px] text-on-surface-variant flex items-center gap-2">
          <span className="material-symbols-outlined text-[16px]">info</span>
          MOS scores unavailable — place <code className="font-mono bg-surface px-1 rounded">sig_bak_ovr.onnx</code> (or <code className="font-mono bg-surface px-1 rounded">dnsmos_p835.onnx</code>) in <code className="font-mono bg-surface px-1 rounded">backend/qa/</code> to enable.
        </div>
      )}

      {/* Conversation metrics */}
      <div>
        <p className="text-[11px] font-bold uppercase tracking-widest text-on-surface-variant mb-3">Conversation Metrics</p>
        <div className="grid grid-cols-2 gap-3">
          {[
            { label: 'Talk-over rate', value: qa.talk_over_rate != null ? `${(qa.talk_over_rate * 100).toFixed(1)}%` : '—', ok: qa.talk_over_rate == null || qa.talk_over_rate <= 0.15 },
            { label: 'Avg response latency', value: qa.avg_ttfa_ms != null ? `${qa.avg_ttfa_ms.toLocaleString()}ms` : '—', ok: qa.avg_ttfa_ms == null || qa.avg_ttfa_ms <= 2000 },
            { label: 'Turns', value: String(qa.turn_count), ok: qa.turn_count >= 3 },
            { label: 'Barge-ins', value: String(qa.bargein_count), ok: true },
          ].map(m => (
            <div key={m.label} className="bg-surface-container rounded-lg px-3 py-2.5">
              <p className="text-[11px] text-on-surface-variant">{m.label}</p>
              <p className={`text-[20px] font-bold font-mono leading-tight ${m.ok ? 'text-on-surface' : 'text-amber-500'}`}>{m.value}</p>
            </div>
          ))}
        </div>
      </div>

      {/* Pipeline config */}
      {cfg && (
        <div>
          <p className="text-[11px] font-bold uppercase tracking-widest text-on-surface-variant mb-3">Pipeline Config</p>
          <div className="bg-surface-container rounded-lg overflow-hidden divide-y divide-outline-variant text-[12px]">
            {[
              ['STT', `${cfg.stt_provider} / ${cfg.stt_model} · ${cfg.stt_sample_rate / 1000}kHz · ${cfg.stt_audio_format}`],
              ['LLM', `${cfg.llm_provider} / ${cfg.llm_model}`],
              ['TTS provider', cfg.tts_provider],
              ['TTS voice', cfg.tts_voice_id],
              cfg.tts_model ? ['TTS model', cfg.tts_model] : null,
              cfg.tts_stability != null ? ['Stability', String(cfg.tts_stability)] : null,
              cfg.tts_similarity_boost != null ? ['Similarity boost', String(cfg.tts_similarity_boost)] : null,
              cfg.tts_style != null ? ['Style', String(cfg.tts_style)] : null,
              cfg.tts_speed != null ? ['Speed', String(cfg.tts_speed)] : null,
              ['Barge-in threshold', `≥ ${cfg.bargein_min_words} words`],
              ['Prompt hash', cfg.prompt_hash],
            ].filter((x): x is string[] => x !== null).map(([k, v]) => (
              <div key={k as string} className="flex px-3 py-2 gap-3">
                <span className="text-on-surface-variant w-32 shrink-0">{k}</span>
                <span className="text-on-surface font-mono break-all">{v}</span>
              </div>
            ))}
          </div>
        </div>
      )}

      {qa.error && (
        <p className="text-[11px] text-on-surface-variant bg-surface-container px-3 py-2 rounded font-mono">
          Partial error: {qa.error}
        </p>
      )}
    </div>
  );
}

// ── Transcript modal ──────────────────────────────────────────────────────────
function TranscriptModal({ call, onClose }: { call: CallRecord; onClose: () => void }) {
  const audioRef = useRef<HTMLAudioElement | null>(null);
  const [isPlaying, setIsPlaying] = useState(false);
  const [currentTimeMs, setCurrentTimeMs] = useState(0);
  const [durationMs, setDurationMs] = useState(0);
  const [playbackRate, setPlaybackRate] = useState(1);
  const [activeTab, setActiveTab] = useState<'transcript' | 'qa'>('transcript');
  const [copied, setCopied] = useState(false);

  const handleCopy = async () => {
    const title = `${call.source === 'twilio' ? 'Phone Call' : 'Browser Session'} — ${formatTimestamp(call.started_at)} · ${call.transcript.length} turns`;
    let text = title + '\n\n';
    text += 'TRANSCRIPT\n' + '─'.repeat(60) + '\n';

    for (const turn of call.transcript) {
      const time = new Date(turn.ts).toLocaleTimeString('en-US', { hour: '2-digit', minute: '2-digit', second: '2-digit' });
      if (turn.role === 'tool_call' || turn.role === 'tool_result') {
        let parsed: any = {};
        try { parsed = JSON.parse(turn.text); } catch { }
        const label = parsed.name || 'API Call';
        const rawPayload = turn.role === 'tool_call' ? parsed.arguments : parsed.result;
        let payloadStr: string;
        try {
          const obj = typeof rawPayload === 'string' ? JSON.parse(rawPayload) : rawPayload;
          payloadStr = JSON.stringify(obj, null, 2);
        } catch { payloadStr = String(rawPayload ?? ''); }
        text += `\n[${time}] [${label} — ${turn.role === 'tool_call' ? 'CALL' : 'RESULT'}]\n${payloadStr}\n`;
      } else {
        const roleName = turn.role === 'user' ? 'You' : 'Léa';
        const metrics: string[] = [];
        if (turn.ttft_ms != null) metrics.push(`TTFT: ${turn.ttft_ms}ms`);
        if (turn.tts_latency != null) metrics.push(`TTFA: ${turn.tts_latency}ms`);
        if (turn.e2e_ms != null) metrics.push(`E2E: ${turn.e2e_ms}ms`);
        const metricsStr = metrics.length ? `  [${metrics.join(' | ')}]` : '';
        text += `\n[${time}] ${roleName}: ${turn.text}${metricsStr}\n`;
      }
    }

    const qa = qaResultCache.get(call.id);
    if (qa) {
      text += '\n\nQUALITY REPORT\n' + '─'.repeat(60) + '\n';
      text += JSON.stringify(qa, null, 2);
    }

    try {
      await navigator.clipboard.writeText(text);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch { /* clipboard unavailable */ }
  };

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
        {/* Header */}
        <div className="flex items-center justify-between px-6 py-4 border-b border-outline-variant shrink-0">
          <div className="flex items-center gap-3">
            <div>
              <p className="font-semibold text-on-surface text-[16px]">
                {call.source === 'twilio' ? 'Phone Call' : 'Browser Session'}
              </p>
              <p className="text-[12px] text-on-surface-variant mt-0.5">
                {formatTimestamp(call.started_at)} · {call.transcript.length} turns
              </p>
            </div>
            <button
              onClick={handleCopy}
              className="flex items-center gap-1 px-2.5 py-1 rounded-lg border border-outline-variant text-on-surface-variant hover:text-on-surface hover:bg-surface-container text-[12px] font-medium transition-colors"
              title="Copy transcript + quality report"
            >
              <span className="material-symbols-outlined text-[15px]">{copied ? 'check' : 'content_copy'}</span>
              {copied ? 'Copied!' : 'Copy'}
            </button>
          </div>
          <button
            onClick={onClose}
            className="p-1.5 rounded-full hover:bg-surface-container text-on-surface-variant transition-colors"
          >
            <span className="material-symbols-outlined text-[20px]">close</span>
          </button>
        </div>

        {/* Tabs */}
        <div className="flex border-b border-outline-variant shrink-0 px-6">
          {(['transcript', 'qa'] as const).map(tab => (
            <button
              key={tab}
              onClick={() => setActiveTab(tab)}
              className={`px-4 py-2.5 text-[12px] font-bold uppercase tracking-wider border-b-2 transition-colors -mb-px ${
                activeTab === tab
                  ? 'border-primary text-primary'
                  : 'border-transparent text-on-surface-variant hover:text-on-surface'
              }`}
            >
              {tab === 'transcript' ? 'Transcript' : 'Quality Report'}
            </button>
          ))}
        </div>

        {/* Audio player — only on transcript tab */}
        {activeTab === 'transcript' && call.status === 'done' && (
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

        {/* Tab content */}
        {activeTab === 'transcript' ? (
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
        ) : (
          <div className="flex-1 overflow-y-auto min-h-0">
            <QAPanel callId={call.id} isDone={call.status === 'done'} />
          </div>
        )}
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
