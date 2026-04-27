'use client';

import { useState, useEffect } from 'react';

type SaveState = 'idle' | 'saving' | 'saved' | 'error';

export default function SystemPromptEditor() {
  const [prompt, setPrompt]     = useState('');
  const [save, setSave]         = useState<SaveState>('idle');
  const [loading, setLoading]   = useState(true);

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

  const saveLabel: Record<SaveState, string> = {
    idle:   'Enregistrer',
    saving: 'Enregistrement…',
    saved:  'Enregistré',
    error:  'Erreur',
  };

  const saveColor: Record<SaveState, string> = {
    idle:   'var(--accent)',
    saving: 'var(--text-secondary)',
    saved:  'var(--success)',
    error:  'var(--danger)',
  };

  return (
    <div style={{ width: '100%' }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'baseline', marginBottom: '0.6rem' }}>
        <p style={{ fontSize: '0.7rem', fontWeight: 700, textTransform: 'uppercase', letterSpacing: '0.08em', color: 'var(--text-secondary)' }}>
          System Prompt
        </p>
        <span style={{ fontSize: '0.7rem', color: 'var(--text-secondary)' }}>
          Prend effet sur le prochain appel
        </span>
      </div>

      <textarea
        value={loading ? 'Chargement…' : prompt}
        disabled={loading}
        onChange={e => setPrompt(e.target.value)}
        rows={4}
        style={{
          width: '100%',
          background: 'var(--bg)',
          border: '1px solid var(--border)',
          borderRadius: '8px',
          color: 'var(--text-primary)',
          padding: '0.75rem',
          fontSize: '0.9rem',
          lineHeight: 1.6,
          resize: 'vertical',
          fontFamily: 'inherit',
          outline: 'none',
          transition: 'border-color 0.15s ease',
        }}
        onFocus={e => (e.currentTarget.style.borderColor = 'var(--accent)')}
        onBlur={e  => (e.currentTarget.style.borderColor = 'var(--border)')}
      />

      <div style={{ display: 'flex', justifyContent: 'flex-end', marginTop: '0.5rem' }}>
        <button
          onClick={handleSave}
          disabled={save === 'saving' || loading}
          style={{
            background: saveColor[save],
            color: '#fff',
            padding: '0.4rem 1.1rem',
            borderRadius: '6px',
            fontSize: '0.85rem',
            fontWeight: 600,
            opacity: save === 'saving' ? 0.7 : 1,
            transition: 'background 0.2s ease',
          }}
        >
          {saveLabel[save]}
        </button>
      </div>
    </div>
  );
}
