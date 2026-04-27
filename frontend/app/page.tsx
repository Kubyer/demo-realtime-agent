'use client';

import { useState } from 'react';
import VoiceSession from '@/components/VoiceSession';
import SystemPromptEditor from '@/components/SystemPromptEditor';
import CallHistory from '@/components/CallHistory';

type Tab = 'session' | 'history';

export default function Home() {
  const [tab, setTab] = useState<Tab>('session');

  return (
    <main style={{ minHeight: '100vh', padding: '2rem 1rem', display: 'flex', flexDirection: 'column', alignItems: 'center', gap: '2rem' }}>
      {/* Header */}
      <div style={{ textAlign: 'center' }}>
        <h1 style={{ fontSize: '1.75rem', fontWeight: 700, marginBottom: '0.4rem' }}>Voice Agent</h1>
        <p style={{ color: 'var(--text-secondary)', fontSize: '0.9rem' }}>Ultra-low-latency AI voice assistant</p>
      </div>

      {/* Tab bar */}
      <div style={{
        display: 'flex', gap: '0.25rem',
        background: 'var(--surface)', border: '1px solid var(--border)',
        borderRadius: '10px', padding: '4px',
        width: '100%', maxWidth: '700px',
      }}>
        {(['session', 'history'] as Tab[]).map(t => (
          <button
            key={t}
            onClick={() => setTab(t)}
            style={{
              flex: 1, padding: '0.45rem 0',
              borderRadius: '7px', fontSize: '0.85rem', fontWeight: 600,
              background: tab === t ? 'var(--accent)' : 'transparent',
              color: tab === t ? '#fff' : 'var(--text-secondary)',
              transition: 'background 0.15s ease, color 0.15s ease',
            }}
          >
            {t === 'session' ? 'Session' : 'Historique'}
          </button>
        ))}
      </div>

      {/* Content */}
      <div style={{ width: '100%', maxWidth: '700px', display: 'flex', flexDirection: 'column', gap: '1.5rem' }}>
        {tab === 'session' && (
          <>
            {/* System prompt card */}
            <div style={{ background: 'var(--surface)', border: '1px solid var(--border)', borderRadius: '12px', padding: '1rem' }}>
              <SystemPromptEditor />
            </div>

            {/* Voice controls + live transcript */}
            <VoiceSession />
          </>
        )}

        {tab === 'history' && (
          <div style={{ background: 'var(--surface)', border: '1px solid var(--border)', borderRadius: '12px', padding: '1rem' }}>
            <CallHistory />
          </div>
        )}
      </div>
    </main>
  );
}
