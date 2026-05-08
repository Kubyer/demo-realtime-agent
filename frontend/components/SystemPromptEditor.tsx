'use client';

import { useState, useEffect } from 'react';

type SaveState = 'idle' | 'saving' | 'saved' | 'error';

const ELEVENLABS_VOICES = [
  { id: '3C1zYzXNXNzrB66ON8rj', label: 'Léa (FR) — défaut' },
  { id: 'TX3LPaxmHKxFdv7VOQHJ', label: 'Liam (EN)' },
  { id: '21m00Tcm4TlvDq8ikWAM', label: 'Rachel (EN)' },
  { id: 'AZnzlk1XvdvUeBnXmlld', label: 'Domi (EN)' },
  { id: 'EXAVITQu4vr4xnSDxMaL', label: 'Bella (EN)' },
  { id: 'ErXwobaYiN019PkySvjV', label: 'Antoni (EN)' },
  { id: 'MF3mGyEYCl7XYWbV9V6O', label: 'Elli (EN)' },
  { id: 'TxGEqnHWrfWFTfGW9XjX', label: 'Josh (EN)' },
  { id: 'VR6AewLTigWG4xSOukaG', label: 'Arnold (EN)' },
  { id: 'XB0fDUnXU5powFXDhCwa', label: 'Charlotte (EN)' },
  { id: 'custom', label: 'ID personnalisé…' },
];

const ELEVENLABS_MODELS = [
  { id: 'eleven_turbo_v2_5',      label: 'Turbo v2.5 (recommandé)' },
  { id: 'eleven_flash_v2_5',      label: 'Flash v2.5 (le plus rapide)' },
  { id: 'eleven_multilingual_v2', label: 'Multilingual v2 (meilleure qualité)' },
  { id: 'eleven_v3',              label: 'Eleven v3 (Flagship / Narrative)' },
];

const CARTESIA_MODELS = [
  { id: 'sonic-2',       label: 'Sonic 2 (recommandé)' },
  { id: 'sonic-english', label: 'Sonic English' },
];

const LLM_PROVIDERS = [
  { id: 'groq',   label: 'Groq' },
  { id: 'gemini', label: 'Google Gemini' },
];

const LLM_MODELS: Record<string, { id: string; label: string }[]> = {
  groq: [
    { id: 'openai/gpt-oss-20b',                          label: 'GPT-OSS 20B (défaut, ~1000 t/s)' },
    { id: 'openai/gpt-oss-120b',                         label: 'GPT-OSS 120B (~500 t/s)' },
    { id: 'meta-llama/llama-4-scout-17b-16e-instruct',   label: 'Llama 4 Scout 17B (~594 t/s)' },
    { id: 'llama-3.3-70b-versatile',                     label: 'LLaMA 3.3 70B (~394 t/s)' },
    { id: 'qwen/qwen3-32b',                              label: 'Qwen 3 32B (~662 t/s)' },
    { id: 'llama-3.1-8b-instant',                        label: 'LLaMA 3.1 8B (ultra-rapide, ~840 t/s)' },
  ],
  gemini: [
    { id: 'gemini-3.1-flash-lite',             label: 'Gemini 3.1 Flash Lite (le plus rapide)' },
    { id: 'gemini-3.1-flash-lite-preview',     label: 'Gemini 3.1 Flash Lite Preview' },
    { id: 'gemini-3-flash-preview',            label: 'Gemini 3 Flash Preview' },
    { id: 'gemini-3.1-pro-preview',            label: 'Gemini 3.1 Pro Preview' },
    { id: 'gemini-2.5-flash',                  label: 'Gemini 2.5 Flash (stable)' },
    { id: 'gemini-2.5-flash-lite',             label: 'Gemini 2.5 Flash Lite' },
    { id: 'gemini-2.5-pro',                    label: 'Gemini 2.5 Pro' },
  ],
};

const GEMINI_TTS_MODELS = [
  { id: 'gemini-3.1-flash-tts-preview',   label: 'Flash 3.1 TTS Preview (le plus récent)' },
  { id: 'gemini-2.5-flash-preview-tts',   label: 'Flash 2.5 TTS Preview (rapide)' },
  { id: 'gemini-2.5-pro-preview-tts',     label: 'Pro 2.5 TTS Preview (haute qualité)' },
];

const GEMINI_TTS_VOICES = [
  { id: 'Aoede',    label: 'Aoede (F) — douce, naturelle' },
  { id: 'Kore',     label: 'Kore (F) — chaleureuse' },
  { id: 'Leda',     label: 'Leda (F) — claire' },
  { id: 'Zephyr',   label: 'Zephyr (F) — vive' },
  { id: 'Charon',   label: 'Charon (M) — posé' },
  { id: 'Fenrir',   label: 'Fenrir (M) — dynamique' },
  { id: 'Orus',     label: 'Orus (M) — calme' },
  { id: 'Puck',     label: 'Puck (M) — énergique' },
];

const GRADIUM_MODELS = [
  { id: 'default', label: 'Default Model' }
];

const GRADIUM_VOICES = [
  { id: 'b35yykvVppLXyw_l', label: 'Emma / Léa (Female)' },
  { id: 'axlOaUiFyOZhy4nv', label: 'Mathieu / Hugo (Male)' }
];

function Slider({ label, value, min, max, step, onChange }: {
  label: string; value: number; min: number; max: number; step: number;
  onChange: (v: number) => void;
}) {
  return (
    <div>
      <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: 4 }}>
        <label className="text-[11px] font-semibold text-on-surface">{label}</label>
        <span className="text-[11px] text-on-surface-variant font-mono">{value.toFixed(2)}</span>
      </div>
      <input
        type="range"
        min={min} max={max} step={step}
        value={value}
        onChange={e => onChange(Number(e.target.value))}
        className="w-full accent-primary"
      />
      <div style={{ display: 'flex', justifyContent: 'space-between' }}>
        <span className="text-[10px] text-on-surface-variant">{min}</span>
        <span className="text-[10px] text-on-surface-variant">{max}</span>
      </div>
    </div>
  );
}

export default function SystemPromptEditor() {
  const [prompt, setPrompt]               = useState('');
  const [voiceProvider, setVoiceProvider] = useState('elevenlabs');
  const [voiceId, setVoiceId]             = useState('3C1zYzXNXNzrB66ON8rj');
  const [customVoiceId, setCustomVoiceId] = useState('');
  const [voiceModel, setVoiceModel]       = useState('eleven_turbo_v2_5');
  const [openingSentence, setOpeningSentence] = useState('');
  // ElevenLabs-specific — defaults tuned for naturalness
  const [elStability,  setElStability]  = useState(0.35);
  const [elSimilarity, setElSimilarity] = useState(0.85);
  const [elStyle,      setElStyle]      = useState(0.20);
  const [elSpeed,      setElSpeed]      = useState(1.0);
  // Cartesia-specific
  const [cartesiaSpeed, setCartesiaSpeed] = useState(1.0);
  // LLM provider
  const [llmProvider, setLlmProvider] = useState('groq');
  const [llmModel,    setLlmModel]    = useState('openai/gpt-oss-20b');
  // Gradium-specific
  const [gradiumTemp, setGradiumTemp] = useState(0.0);
  const [gradiumCfg, setGradiumCfg] = useState(2.0);
  const [gradiumPadding, setGradiumPadding] = useState(0.0);

  const [save, setSave]       = useState<SaveState>('idle');
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    fetch('/api/settings')
      .then(r => r.json())
      .then(d => {
        setPrompt(d.prompt ?? '');
        setVoiceProvider(d.voice_provider ?? 'elevenlabs');
        setOpeningSentence(d.opening_sentence ?? '');
        // Voice ID
        const vid = d.voice_id ?? '3C1zYzXNXNzrB66ON8rj';
        const known = [...ELEVENLABS_VOICES, ...GRADIUM_VOICES].find(v => v.id === vid && v.id !== 'custom');
        if (known) {
          setVoiceId(vid);
        } else {
          setVoiceId('custom');
          setCustomVoiceId(vid);
        }
        setVoiceModel(d.voice_model ?? 'eleven_turbo_v2_5');
        // ElevenLabs voice params — field names match what backend sends/expects
        if (d.el_stability  != null) setElStability(d.el_stability);
        if (d.el_similarity != null) setElSimilarity(d.el_similarity);
        if (d.el_style      != null) setElStyle(d.el_style);
        if (d.el_speed      != null) setElSpeed(d.el_speed);
        if (d.cartesia_speed  != null) setCartesiaSpeed(d.cartesia_speed);
        if (d.llm_provider) setLlmProvider(d.llm_provider);
        if (d.llm_model)    setLlmModel(d.llm_model);
        if (d.gradium_temp    != null) setGradiumTemp(d.gradium_temp);
        if (d.gradium_cfg     != null) setGradiumCfg(d.gradium_cfg);
        if (d.gradium_padding != null) setGradiumPadding(d.gradium_padding);
        setLoading(false);
      })
      .catch(() => setLoading(false));
  }, []);

  const effectiveVoiceId = voiceId === 'custom' ? customVoiceId : voiceId;

  const handleSave = async () => {
    setSave('saving');
    try {
      const body: Record<string, unknown> = {
        prompt,
        voice_provider:   voiceProvider,
        voice_id:         effectiveVoiceId,
        voice_model:      voiceModel,
        opening_sentence: openingSentence,
        llm_provider:     llmProvider,
        llm_model:        llmModel,
      };
      if (voiceProvider === 'elevenlabs') {
        body.el_stability  = elStability;
        body.el_similarity = elSimilarity;
        body.el_style      = elStyle;
        body.el_speed      = elSpeed;
      }
      if (voiceProvider === 'cartesia') {
        body.cartesia_speed = cartesiaSpeed;
      }
      if (voiceProvider === 'gradium') {
        body.gradium_temp    = gradiumTemp;
        body.gradium_cfg     = gradiumCfg;
        body.gradium_padding = gradiumPadding;
      }
      const res = await fetch('/api/settings', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      });
      setSave(res.ok ? 'saved' : 'error');
    } catch {
      setSave('error');
    }
    setTimeout(() => setSave('idle'), 2500);
  };

  const btnClass: Record<SaveState, string> = {
    idle:   'bg-primary text-on-primary hover:bg-primary-container',
    saving: 'bg-slate-300 text-slate-500 cursor-not-allowed',
    saved:  'bg-secondary text-on-secondary',
    error:  'bg-error text-on-error',
  };
  const btnLabel: Record<SaveState, string> = {
    idle: 'Save', saving: 'Saving…', saved: 'Saved ✓', error: 'Error',
  };

  return (
    <div className="w-full flex gap-6 items-start">

      {/* ── Left column: voice & TTS settings card ── */}
      <div className="bg-surface rounded-xl border border-outline-variant shadow-sm p-6 flex flex-col gap-4 w-[420px] shrink-0">

          <span className="text-[11px] font-bold uppercase tracking-widest text-on-surface-variant">Voice &amp; TTS Settings</span>

          {/* Provider + Model */}
          <div className="grid grid-cols-2 gap-3">
            <div>
              <label className="block text-[12px] font-semibold text-on-surface mb-1">Provider</label>
              <select
                value={voiceProvider}
                onChange={e => {
                  setVoiceProvider(e.target.value);
                  if (e.target.value === 'elevenlabs') {
                    setVoiceId('3C1zYzXNXNzrB66ON8rj');
                    setVoiceModel('eleven_turbo_v2_5');
                  } else if (e.target.value === 'gradium') {
                    setVoiceId('b35yykvVppLXyw_l');
                    setVoiceModel('default');
                  } else if (e.target.value === 'gemini_tts') {
                    setVoiceId('Aoede');
                    setVoiceModel('gemini-3.1-flash-tts-preview');
                  } else {
                    setVoiceId('custom');
                    setVoiceModel('sonic-2');
                  }
                }}
                disabled={loading}
                className="w-full bg-white border border-slate-200 rounded-lg text-on-surface text-[14px] px-3 py-2 focus:outline-none focus:border-primary disabled:opacity-60"
              >
                <option value="elevenlabs">ElevenLabs</option>
                <option value="cartesia">Cartesia</option>
                <option value="gradium">Gradium</option>
                <option value="gemini_tts">Google Gemini TTS</option>
              </select>
            </div>

            <div>
              <label className="block text-[12px] font-semibold text-on-surface mb-1">Model</label>
              {voiceProvider === 'elevenlabs' ? (
                <select
                  value={voiceModel}
                  onChange={e => setVoiceModel(e.target.value)}
                  disabled={loading}
                  className="w-full bg-white border border-slate-200 rounded-lg text-on-surface text-[14px] px-3 py-2 focus:outline-none focus:border-primary disabled:opacity-60"
                >
                  {ELEVENLABS_MODELS.map(m => (
                    <option key={m.id} value={m.id}>{m.label}</option>
                  ))}
                </select>
              ) : voiceProvider === 'gradium' ? (
                <select
                  value={voiceModel}
                  onChange={e => setVoiceModel(e.target.value)}
                  disabled={loading}
                  className="w-full bg-white border border-slate-200 rounded-lg text-on-surface text-[14px] px-3 py-2 focus:outline-none focus:border-primary disabled:opacity-60"
                >
                  {GRADIUM_MODELS.map(m => (
                    <option key={m.id} value={m.id}>{m.label}</option>
                  ))}
                </select>
              ) : voiceProvider === 'gemini_tts' ? (
                <select
                  value={voiceModel}
                  onChange={e => setVoiceModel(e.target.value)}
                  disabled={loading}
                  className="w-full bg-white border border-slate-200 rounded-lg text-on-surface text-[14px] px-3 py-2 focus:outline-none focus:border-primary disabled:opacity-60"
                >
                  {GEMINI_TTS_MODELS.map(m => (
                    <option key={m.id} value={m.id}>{m.label}</option>
                  ))}
                </select>
              ) : (
                <select
                  value={voiceModel}
                  onChange={e => setVoiceModel(e.target.value)}
                  disabled={loading}
                  className="w-full bg-white border border-slate-200 rounded-lg text-on-surface text-[14px] px-3 py-2 focus:outline-none focus:border-primary disabled:opacity-60"
                >
                  {CARTESIA_MODELS.map(m => (
                    <option key={m.id} value={m.id}>{m.label}</option>
                  ))}
                </select>
              )}
            </div>
          </div>

          {/* Voice ID */}
          <div>
            <label className="block text-[12px] font-semibold text-on-surface mb-1">Voice</label>
            {voiceProvider === 'elevenlabs' ? (
              <div className="flex flex-col gap-2">
                <select
                  value={voiceId}
                  onChange={e => setVoiceId(e.target.value)}
                  disabled={loading}
                  className="w-full bg-white border border-slate-200 rounded-lg text-on-surface text-[14px] px-3 py-2 focus:outline-none focus:border-primary disabled:opacity-60"
                >
                  {ELEVENLABS_VOICES.map(v => (
                    <option key={v.id} value={v.id}>{v.label}</option>
                  ))}
                </select>
                {voiceId === 'custom' && (
                  <input
                    type="text"
                    value={customVoiceId}
                    onChange={e => setCustomVoiceId(e.target.value)}
                    placeholder="Voice ID ElevenLabs…"
                    disabled={loading}
                    className="w-full bg-white border border-slate-200 rounded-lg text-on-surface text-[13px] px-3 py-2 font-mono focus:outline-none focus:border-primary disabled:opacity-60"
                  />
                )}
              </div>
            ) : voiceProvider === 'gradium' ? (
              <select
                value={voiceId}
                onChange={e => setVoiceId(e.target.value)}
                disabled={loading}
                className="w-full bg-white border border-slate-200 rounded-lg text-on-surface text-[14px] px-3 py-2 focus:outline-none focus:border-primary disabled:opacity-60"
              >
                {GRADIUM_VOICES.map(v => (
                  <option key={v.id} value={v.id}>{v.label}</option>
                ))}
              </select>
            ) : voiceProvider === 'gemini_tts' ? (
              <select
                value={voiceId}
                onChange={e => setVoiceId(e.target.value)}
                disabled={loading}
                className="w-full bg-white border border-slate-200 rounded-lg text-on-surface text-[14px] px-3 py-2 focus:outline-none focus:border-primary disabled:opacity-60"
              >
                {GEMINI_TTS_VOICES.map(v => (
                  <option key={v.id} value={v.id}>{v.label}</option>
                ))}
              </select>
            ) : (
              <input
                type="text"
                value={voiceId === 'custom' ? customVoiceId : voiceId}
                onChange={e => { setVoiceId('custom'); setCustomVoiceId(e.target.value); }}
                placeholder="Voice ID Cartesia (UUID)…"
                disabled={loading}
                className="w-full bg-white border border-slate-200 rounded-lg text-on-surface text-[14px] px-3 py-2 font-mono focus:outline-none focus:border-primary disabled:opacity-60"
              />
            )}
          </div>

          {/* Provider-specific parameters */}
          {voiceProvider === 'elevenlabs' && (
            <div className="border border-slate-200 rounded-lg p-3 bg-slate-50 flex flex-col gap-3">
              <span className="text-[11px] font-bold uppercase tracking-widest text-on-surface-variant">ElevenLabs Voice Parameters</span>
              <Slider label="Stability"        value={elStability}  min={0}   max={1}   step={0.01} onChange={setElStability} />
              <Slider label="Similarity Boost" value={elSimilarity} min={0}   max={1}   step={0.01} onChange={setElSimilarity} />
              <Slider label="Style"            value={elStyle}      min={0}   max={1}   step={0.01} onChange={setElStyle} />
              <Slider label="Speed"            value={elSpeed}      min={0.7} max={1.2} step={0.05} onChange={setElSpeed} />
            </div>
          )}

          {voiceProvider === 'cartesia' && (
            <div className="border border-slate-200 rounded-lg p-3 bg-slate-50 flex flex-col gap-3">
              <span className="text-[11px] font-bold uppercase tracking-widest text-on-surface-variant">Cartesia Parameters</span>
              <Slider label="Speed" value={cartesiaSpeed} min={0.5} max={2.0} step={0.1} onChange={setCartesiaSpeed} />
            </div>
          )}

          {voiceProvider === 'gradium' && (
            <div className="border border-slate-200 rounded-lg p-3 bg-slate-50 flex flex-col gap-3">
              <span className="text-[11px] font-bold uppercase tracking-widest text-on-surface-variant">Gradium Parameters</span>
              <Slider label="Temperature"      value={gradiumTemp}    min={0.0} max={1.4} step={0.1} onChange={setGradiumTemp} />
              <Slider label="CFG (Similarity)" value={gradiumCfg}     min={1.0} max={4.0} step={0.1} onChange={setGradiumCfg} />
              <Slider label="Padding (Speed)"  value={gradiumPadding} min={-4.0} max={4.0} step={0.5} onChange={setGradiumPadding} />
            </div>
          )}

          {/* LLM / Brain settings */}
          <div className="border border-slate-200 rounded-lg p-3 bg-slate-50 flex flex-col gap-3">
            <span className="text-[11px] font-bold uppercase tracking-widest text-on-surface-variant">LLM / Brain</span>
            <div className="grid grid-cols-2 gap-3">
              <div>
                <label className="block text-[12px] font-semibold text-on-surface mb-1">Provider</label>
                <select
                  value={llmProvider}
                  onChange={e => {
                    const p = e.target.value;
                    setLlmProvider(p);
                    setLlmModel(LLM_MODELS[p]?.[0]?.id ?? '');
                  }}
                  disabled={loading}
                  className="w-full bg-white border border-slate-200 rounded-lg text-on-surface text-[14px] px-3 py-2 focus:outline-none focus:border-primary disabled:opacity-60"
                >
                  {LLM_PROVIDERS.map(p => (
                    <option key={p.id} value={p.id}>{p.label}</option>
                  ))}
                </select>
              </div>
              <div>
                <label className="block text-[12px] font-semibold text-on-surface mb-1">Model</label>
                <select
                  value={llmModel}
                  onChange={e => setLlmModel(e.target.value)}
                  disabled={loading}
                  className="w-full bg-white border border-slate-200 rounded-lg text-on-surface text-[14px] px-3 py-2 focus:outline-none focus:border-primary disabled:opacity-60"
                >
                  {(LLM_MODELS[llmProvider] ?? []).map(m => (
                    <option key={m.id} value={m.id}>{m.label}</option>
                  ))}
                </select>
              </div>
            </div>
            {llmProvider === 'gemini' && (
              <p className="text-[11px] text-amber-700 bg-amber-50 border border-amber-200 rounded px-2 py-1">
                Requires <code className="font-mono">GEMINI_API_KEY</code> in your <code className="font-mono">.env</code> file.
              </p>
            )}
          </div>

          {/* Opening sentence */}
          <div>
            <label className="block text-[12px] font-semibold text-on-surface mb-1">Opening Sentence</label>
            <textarea
              value={loading ? 'Loading…' : openingSentence}
              disabled={loading}
              onChange={e => setOpeningSentence(e.target.value)}
              rows={2}
              className="w-full bg-white border border-slate-200 rounded-lg text-on-surface text-[14px] leading-relaxed px-3 py-2 focus:outline-none focus:border-primary focus:ring-1 focus:ring-primary transition-colors font-sans disabled:opacity-60"
            />
          </div>

          {/* Save button at bottom of left card */}
          <div className="flex items-center justify-between pt-2 border-t border-slate-100 mt-2">
            <span className="text-[11px] text-on-surface-variant">Takes effect on the next call</span>
            <button
              onClick={handleSave}
              disabled={save === 'saving' || loading}
              className={`px-4 py-1.5 rounded-lg text-[13px] font-semibold transition-colors ${btnClass[save]}`}
            >
              {btnLabel[save]}
            </button>
          </div>
        </div>

        {/* ── Right column: system prompt card ── */}
        <div className="bg-surface rounded-xl border border-outline-variant shadow-sm p-6 flex-1 flex flex-col gap-3">
          <div className="flex items-center justify-between">
            <span className="text-[11px] font-bold uppercase tracking-widest text-on-surface-variant">System Prompt</span>
            <span className="text-[11px] text-on-surface-variant font-mono">{prompt.length} chars</span>
          </div>
          <textarea
            value={loading ? 'Loading…' : prompt}
            disabled={loading}
            onChange={e => setPrompt(e.target.value)}
            className="w-full flex-1 bg-slate-50 border border-slate-200 rounded-lg text-on-surface text-[13px] leading-relaxed px-3 py-2.5 resize-none focus:outline-none focus:border-primary focus:ring-1 focus:ring-primary transition-colors font-mono disabled:opacity-60"
            style={{ minHeight: 480 }}
          />
        </div>

    </div>
  );
}
