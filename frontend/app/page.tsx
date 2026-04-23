import VoiceSession from '@/components/VoiceSession';

export default function Home() {
  return (
    <main style={{
      display: 'flex',
      flexDirection: 'column',
      alignItems: 'center',
      justifyContent: 'center',
      minHeight: '100vh',
      padding: '2rem',
      gap: '2rem',
    }}>
      <div style={{ textAlign: 'center' }}>
        <h1 style={{ fontSize: '1.75rem', fontWeight: 700, marginBottom: '0.5rem' }}>
          LegalPlace Voice Agent
        </h1>
        <p style={{ color: 'var(--text-secondary)', fontSize: '0.95rem' }}>
          Assistant juridique IA — Ultra-basse latence
        </p>
      </div>
      <VoiceSession />
    </main>
  );
}
