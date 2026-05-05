'use client';

interface Props {
  open: boolean;
  onClose: () => void;
  onStartCall: () => Promise<void>;
  callActive: boolean;
  startError?: string | null;
}

export default function StartCallPanel({ open, onClose, onStartCall, callActive, startError }: Props) {
  const handleStart = async () => {
    await onStartCall();
  };

  return (
    <>
      {open && (
        <div
          className="fixed inset-0 bg-black/30 backdrop-blur-[2px] z-[60]"
          onClick={onClose}
        />
      )}

      <div
        className={`fixed top-0 right-0 h-screen w-[360px] bg-white border-l border-slate-200 z-[70] flex flex-col overflow-hidden shadow-2xl transition-transform duration-300 ease-in-out ${
          open ? 'translate-x-0' : 'translate-x-full'
        }`}
      >
        {/* Header */}
        <div className="flex items-center justify-between px-6 py-4 border-b border-slate-200 shrink-0">
          <div className="flex items-center gap-3">
            <div className="w-8 h-8 rounded-[50%] bg-primary flex items-center justify-center">
              <span className="material-symbols-outlined text-on-primary text-[18px]" style={{ fontVariationSettings: "'FILL' 1" }}>call</span>
            </div>
            <p className="text-[16px] font-semibold text-slate-900">
              {callActive ? 'Appel en cours' : 'Démarrer un appel'}
            </p>
          </div>
          <button onClick={onClose} className="p-1.5 rounded-full hover:bg-slate-100 text-slate-500 transition-colors">
            <span className="material-symbols-outlined text-[20px]">close</span>
          </button>
        </div>

        {/* Body */}
        <div className="flex-1 flex flex-col items-center justify-center px-8 gap-4">
          {callActive ? (
            <p className="text-[14px] text-slate-500 text-center">
              Un appel est déjà en cours. Cliquez sur <strong>Reprendre</strong> pour y revenir, ou attendez qu'il se termine.
            </p>
          ) : (
            <>
              <div className="w-16 h-16 rounded-full bg-blue-50 flex items-center justify-center mb-2">
                <span className="material-symbols-outlined text-primary text-[36px]" style={{ fontVariationSettings: "'FILL' 1" }}>mic</span>
              </div>
              <p className="text-[14px] text-slate-500 text-center">
                Cliquez sur <strong>Démarrer</strong> pour lancer la session vocale.<br/>
                Configurez la voix et le prompt dans <strong>Settings</strong>.
              </p>
            </>
          )}
        </div>

        {/* Footer */}
        <div className="px-6 py-5 border-t border-slate-200 shrink-0">
          {startError && (
            <p className="text-[12px] text-red-500 mb-3 text-center">{startError}</p>
          )}
          {!callActive && (
            <button
              onClick={handleStart}
              className="w-full bg-primary text-on-primary rounded-xl py-3.5 text-[15px] font-semibold flex items-center justify-center gap-2 active:scale-95 transition-transform shadow-sm"
            >
              <span className="material-symbols-outlined text-[20px]" style={{ fontVariationSettings: "'FILL' 1" }}>mic</span>
              Démarrer la session
            </button>
          )}
        </div>
      </div>
    </>
  );
}
