'use client';

import { useState, useEffect } from 'react';

type SaveState = 'idle' | 'saving' | 'saved' | 'error';

export default function SystemPromptEditor() {
  const [prompt, setPrompt]   = useState('');
  const [voiceProvider, setVoiceProvider] = useState('elevenlabs');
  const [voiceId, setVoiceId] = useState('');
  const [voiceModel, setVoiceModel] = useState('');
  const [openingSentence, setOpeningSentence] = useState('');
  const [save, setSave]       = useState<SaveState>('idle');
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    fetch('/api/settings')
      .then(r => r.json())
      .then(d => { 
        setPrompt(d.prompt ?? ''); 
        setVoiceProvider(d.voice_provider ?? 'elevenlabs');
        setVoiceId(d.voice_id ?? '3C1zYzXNXNzrB66ON8rj');
        setVoiceModel(d.voice_model ?? 'eleven_flash_v2_5');
        setOpeningSentence(d.opening_sentence ?? '');
        setLoading(false); 
      })
      .catch(() => setLoading(false));
  }, []);

  const handleSave = async () => {
    setSave('saving');
    try {
      const res = await fetch('/api/settings', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ 
          prompt,
          voice_provider: voiceProvider,
          voice_id: voiceId,
          voice_model: voiceModel,
          opening_sentence: openingSentence
        }),
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
    idle: 'Save', saving: 'Saving…', saved: 'Saved', error: 'Error',
  };

  return (
    <div className="w-full flex flex-col gap-4">
      <div className="flex justify-between items-baseline mb-2">
        <span className="text-[11px] text-on-surface-variant uppercase font-semibold">Prompt & Voice Settings</span>
        <span className="text-[11px] text-on-surface-variant">Takes effect on the next call</span>
      </div>

      <div className="grid grid-cols-3 gap-4">
        <div>
          <label className="block text-[12px] font-semibold text-on-surface mb-1">Provider</label>
          <select 
            value={voiceProvider}
            onChange={(e) => setVoiceProvider(e.target.value)}
            disabled={loading}
            className="w-full bg-white border border-slate-200 rounded-lg text-on-surface text-[14px] px-3 py-2 focus:outline-none focus:border-primary disabled:opacity-60"
          >
            <option value="elevenlabs">ElevenLabs</option>
            <option value="cartesia">Cartesia</option>
          </select>
        </div>
        <div>
          <label className="block text-[12px] font-semibold text-on-surface mb-1">Voice ID</label>
          <input 
            type="text"
            value={voiceId}
            onChange={(e) => setVoiceId(e.target.value)}
            disabled={loading}
            className="w-full bg-white border border-slate-200 rounded-lg text-on-surface text-[14px] px-3 py-2 focus:outline-none focus:border-primary disabled:opacity-60"
          />
        </div>
        <div>
          <label className="block text-[12px] font-semibold text-on-surface mb-1">Voice Model</label>
          <input 
            type="text"
            value={voiceModel}
            onChange={(e) => setVoiceModel(e.target.value)}
            disabled={loading}
            className="w-full bg-white border border-slate-200 rounded-lg text-on-surface text-[14px] px-3 py-2 focus:outline-none focus:border-primary disabled:opacity-60"
          />
        </div>
      </div>

      <div>
        <label className="block text-[12px] font-semibold text-on-surface mb-1">Opening Sentence (Assistant starts the call)</label>
        <textarea
          value={loading ? 'Loading…' : openingSentence}
          disabled={loading}
          onChange={e => setOpeningSentence(e.target.value)}
          rows={2}
          className="w-full bg-white border border-slate-200 rounded-lg text-on-surface text-[14px] leading-relaxed px-3 py-2 focus:outline-none focus:border-primary focus:ring-1 focus:ring-primary transition-colors font-sans disabled:opacity-60"
        />
      </div>

      <div>
        <label className="block text-[12px] font-semibold text-on-surface mb-1">System Prompt</label>
        <textarea
          value={loading ? 'Loading…' : prompt}
          disabled={loading}
          onChange={e => setPrompt(e.target.value)}
          rows={6}
          className="w-full bg-white border border-slate-200 rounded-lg text-on-surface text-[14px] leading-relaxed px-3 py-2.5 resize-y focus:outline-none focus:border-primary focus:ring-1 focus:ring-primary transition-colors font-sans disabled:opacity-60"
        />
      </div>

      <div className="flex justify-end mt-2">
        <button
          onClick={handleSave}
          disabled={save === 'saving' || loading}
          className={`px-4 py-1.5 rounded-lg text-[13px] font-semibold transition-colors ${btnClass[save]}`}
        >
          {btnLabel[save]}
        </button>
      </div>
    </div>
  );
}
