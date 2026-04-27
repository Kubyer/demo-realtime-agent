'use client';

import VoiceSession from './VoiceSession';
import SystemPromptEditor from './SystemPromptEditor';

interface Props {
  open: boolean;
  onClose: () => void;
}

export default function StartCallPanel({ open, onClose }: Props) {
  return (
    <>
      {/* Dim overlay */}
      {open && (
        <div
          className="fixed inset-0 bg-black/30 backdrop-blur-[2px] z-[60]"
          onClick={onClose}
        />
      )}

      {/* Slide-in panel */}
      <div
        className={`fixed top-0 right-0 h-screen w-[460px] bg-white border-l border-slate-200 z-[70] flex flex-col overflow-hidden shadow-2xl transition-transform duration-300 ease-in-out ${
          open ? 'translate-x-0' : 'translate-x-full'
        }`}
      >
        {/* Header */}
        <div className="flex items-center justify-between px-6 py-4 border-b border-slate-200 shrink-0">
          <div className="flex items-center gap-3">
            <div className="w-8 h-8 rounded-[50%] bg-primary flex items-center justify-center">
              <span className="material-symbols-outlined text-on-primary text-[18px]" style={{ fontVariationSettings: "'FILL' 1" }}>call</span>
            </div>
            <p className="text-[16px] font-semibold text-slate-900">Start a Call</p>
          </div>
          <button
            onClick={onClose}
            className="p-1.5 rounded-full hover:bg-slate-100 text-slate-500 transition-colors"
          >
            <span className="material-symbols-outlined text-[20px]">close</span>
          </button>
        </div>

        {/* Scrollable body */}
        <div className="flex-1 overflow-y-auto px-6 py-5 flex flex-col gap-6">
          {/* System prompt section */}
          <div>
            <p className="text-[11px] font-bold uppercase tracking-widest text-on-surface-variant mb-3">System Prompt</p>
            <div className="bg-slate-50 border border-slate-200 rounded-xl p-4">
              <SystemPromptEditor />
            </div>
          </div>

          {/* Voice session section */}
          <div>
            <p className="text-[11px] font-bold uppercase tracking-widest text-on-surface-variant mb-3">Browser Voice Session</p>
            <div className="bg-slate-50 border border-slate-200 rounded-xl p-4">
              {open && <VoiceSession />}
            </div>
          </div>
        </div>
      </div>
    </>
  );
}
