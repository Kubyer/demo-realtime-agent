'use client';

import { useState, useEffect } from 'react';

type SaveState = 'idle' | 'saving' | 'saved' | 'error';

export default function SystemPromptEditor() {
  const [prompt, setPrompt]   = useState('');
  const [save, setSave]       = useState<SaveState>('idle');
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    fetch('/api/system-prompt')
      .then(r => r.json())
      .then(d => { setPrompt(d.prompt ?? ''); setLoading(false); })
      .catch(() => setLoading(false));
  }, []);

  const handleSave = async () => {
    setSave('saving');
    try {
      const res = await fetch('/api/system-prompt', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ prompt }),
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
    <div className="w-full">
      <div className="flex justify-between items-baseline mb-2">
        <span className="text-[11px] text-on-surface-variant">Takes effect on the next call</span>
      </div>
      <textarea
        value={loading ? 'Loading…' : prompt}
        disabled={loading}
        onChange={e => setPrompt(e.target.value)}
        rows={4}
        className="w-full bg-white border border-slate-200 rounded-lg text-on-surface text-[14px] leading-relaxed px-3 py-2.5 resize-y focus:outline-none focus:border-primary focus:ring-1 focus:ring-primary transition-colors font-sans disabled:opacity-60"
      />
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
