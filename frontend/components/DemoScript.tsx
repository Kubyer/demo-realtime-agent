'use client';

import { useState } from 'react';

interface ScriptLine { speaker: 'agent' | 'prospect'; text: string }
interface ScriptStep { phase: string; lines: ScriptLine[] }

const SCRIPT: ScriptStep[] = [
  {
    phase: 'Ouverture',
    lines: [
      { speaker: 'agent',   text: "Bonjour, c'est Léa de Legalplace. Je peux vous aider à planifier une discussion avec notre équipe — quel jour vous arrangerait ?" },
      { speaker: 'prospect', text: "Bonjour, euh… plutôt en fin de semaine si possible." },
    ],
  },
  {
    phase: 'Qualification',
    lines: [
      { speaker: 'agent',   text: "Parfait ! Jeudi ou vendredi vous conviendrait mieux ?" },
      { speaker: 'prospect', text: "Jeudi de préférence." },
    ],
  },
  {
    phase: 'Vérification du calendrier',
    lines: [
      { speaker: 'agent',   text: "Je vérifie les disponibilités pour jeudi…" },
      { speaker: 'agent',   text: "J'ai deux créneaux : neuf heures du matin ou quatorze heures trente. Lequel vous convient ?" },
      { speaker: 'prospect', text: "Quatorze heures trente c'est parfait." },
    ],
  },
  {
    phase: 'Collecte des informations',
    lines: [
      { speaker: 'agent',   text: "Super ! Pour finaliser la réservation, pouvez-vous me donner votre prénom et votre adresse email ?" },
      { speaker: 'prospect', text: "Je m'appelle Jean Dupont, mon email c'est jean.dupont@exemple.fr." },
    ],
  },
  {
    phase: 'Confirmation de réservation',
    lines: [
      { speaker: 'agent',   text: "Je bloque le créneau pour vous…" },
      { speaker: 'agent',   text: "C'est confirmé, Jean ! Vous recevrez une invitation calendrier à jean.dupont@exemple.fr. À jeudi !" },
      { speaker: 'prospect', text: "Merci beaucoup, à jeudi !" },
    ],
  },
];

export default function DemoScript() {
  const [collapsed, setCollapsed] = useState(false);

  return (
    <div
      style={{
        display: 'flex',
        flexDirection: 'column',
        height: '100%',
        background: '#fafafa',
        borderRight: '1px solid #e2e8f0',
        transition: 'width 0.25s ease',
        overflow: 'hidden',
        width: collapsed ? 48 : 280,
        minWidth: collapsed ? 48 : 280,
        maxWidth: collapsed ? 48 : 280,
        flexShrink: 0,
      }}
    >
      {/* Header */}
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          padding: '0.75rem',
          borderBottom: '1px solid #e2e8f0',
          background: '#fff',
          flexShrink: 0,
        }}
      >
        {!collapsed && (
          <span style={{ fontSize: '0.7rem', fontWeight: 700, textTransform: 'uppercase', letterSpacing: '0.08em', color: '#64748b' }}>
            Script démo
          </span>
        )}
        <button
          onClick={() => setCollapsed(c => !c)}
          style={{
            padding: '4px',
            borderRadius: '6px',
            color: '#64748b',
            background: 'transparent',
            border: 'none',
            cursor: 'pointer',
            display: 'flex',
            alignItems: 'center',
            marginLeft: collapsed ? 'auto' : 0,
          }}
          title={collapsed ? 'Afficher le script' : 'Masquer le script'}
        >
          <span className="material-symbols-outlined" style={{ fontSize: 18 }}>
            {collapsed ? 'chevron_right' : 'chevron_left'}
          </span>
        </button>
      </div>

      {!collapsed && (
        <div style={{ flex: 1, overflowY: 'auto', padding: '0.75rem' }}>
          {SCRIPT.map((step, si) => (
            <div key={si} style={{ marginBottom: '1.25rem' }}>
              {/* Phase header */}
              <div style={{
                fontSize: '0.62rem',
                fontWeight: 700,
                textTransform: 'uppercase',
                letterSpacing: '0.08em',
                color: '#3b82f6',
                marginBottom: '0.5rem',
                paddingBottom: '0.3rem',
                borderBottom: '1px solid #e2e8f0',
              }}>
                {si + 1}. {step.phase}
              </div>

              {/* Lines */}
              <div style={{ display: 'flex', flexDirection: 'column', gap: '0.5rem' }}>
                {step.lines.map((line, li) => (
                  <div key={li} style={{ display: 'flex', gap: '0.4rem', alignItems: 'flex-start' }}>
                    <span style={{
                      fontSize: '0.58rem',
                      fontWeight: 700,
                      textTransform: 'uppercase',
                      padding: '2px 5px',
                      borderRadius: '4px',
                      background: line.speaker === 'agent' ? 'rgba(16,185,129,0.12)' : 'rgba(59,130,246,0.12)',
                      color: line.speaker === 'agent' ? '#10b981' : '#3b82f6',
                      flexShrink: 0,
                      marginTop: '2px',
                    }}>
                      {line.speaker === 'agent' ? 'Léa' : 'Client'}
                    </span>
                    <p style={{ fontSize: '0.76rem', lineHeight: 1.5, color: '#374151', margin: 0 }}>
                      {line.text}
                    </p>
                  </div>
                ))}
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
