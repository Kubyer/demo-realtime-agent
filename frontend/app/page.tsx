'use client';

import { useState } from 'react';
import CallHistory from '@/components/CallHistory';
import SystemPromptEditor from '@/components/SystemPromptEditor';
import StartCallPanel from '@/components/StartCallPanel';

type View = 'dashboard' | 'settings';

const NAV = [
  { id: 'dashboard', label: 'Dashboard',  icon: 'dashboard',     view: 'dashboard' as View },
  { id: 'calls',     label: 'Call Logs',  icon: 'call',          view: 'dashboard' as View },
  { id: 'contacts',  label: 'Contacts',   icon: 'group',         view: null },
  { id: 'analytics', label: 'Analytics',  icon: 'insert_chart',  view: null },
  { id: 'settings',  label: 'Settings',   icon: 'settings',      view: 'settings' as View },
];

export default function Home() {
  const [view, setView]           = useState<View>('dashboard');
  const [activeNav, setActiveNav] = useState('dashboard');
  const [panelOpen, setPanelOpen] = useState(false);
  const [search, setSearch]       = useState('');

  const pageTitle = view === 'settings' ? 'Settings' : 'Call History';

  return (
    <div className="h-screen overflow-hidden flex">

      {/* ── Sidebar ───────────────────────────────────── */}
      <nav className="w-[240px] h-screen fixed left-0 top-0 border-r border-slate-200 bg-slate-50 flex flex-col py-6 px-4 z-50 shrink-0">

        {/* Logo */}
        <div className="flex items-center gap-3 mb-8 px-2">
          <div className="w-8 h-8 rounded-lg bg-primary flex items-center justify-center shrink-0">
            <span className="material-symbols-outlined text-on-primary text-[20px]" style={{ fontVariationSettings: "'FILL' 1" }}>headset_mic</span>
          </div>
          <div>
            <p className="text-[17px] font-bold text-slate-900 leading-none mb-0.5">CommCenter</p>
            <p className="text-[12px] text-slate-500">Professional Dialer</p>
          </div>
        </div>

        {/* CTA */}
        <button
          onClick={() => setPanelOpen(true)}
          className="w-full bg-primary text-on-primary rounded-lg py-2.5 px-4 mb-6 text-[14px] font-medium flex items-center justify-center gap-2 active:scale-95 transition-transform shadow-sm"
        >
          <span className="material-symbols-outlined text-[18px]" style={{ fontVariationSettings: "'FILL' 1" }}>call</span>
          Start Call
        </button>

        {/* Nav links */}
        <div className="flex-1 flex flex-col gap-0.5 overflow-y-auto">
          {NAV.map(item => {
            const active = activeNav === item.id;
            return (
              <a
                key={item.id}
                href="#"
                onClick={e => {
                  e.preventDefault();
                  setActiveNav(item.id);
                  if (item.view) setView(item.view);
                }}
                className={`flex items-center gap-3 px-3 py-2.5 rounded-lg text-[13px] transition-colors duration-150 ${
                  active
                    ? 'text-blue-600 bg-blue-50 font-semibold border-r-2 border-blue-600'
                    : 'text-slate-500 hover:text-slate-900 hover:bg-slate-100'
                }`}
              >
                <span
                  className="material-symbols-outlined text-[20px]"
                  style={active ? { fontVariationSettings: "'FILL' 1" } : {}}
                >
                  {item.icon}
                </span>
                {item.label}
              </a>
            );
          })}
        </div>

        {/* Footer */}
        <div className="pt-4 border-t border-slate-200">
          <a href="#" className="flex items-center gap-3 px-3 py-2 rounded-lg text-slate-500 hover:text-slate-900 hover:bg-slate-100 transition-colors text-[13px]">
            <span className="material-symbols-outlined text-[20px]">help_outline</span>
            Help Support
          </a>
        </div>
      </nav>

      {/* ── Right column ─────────────────────────────── */}
      <div className="ml-[240px] flex-1 flex flex-col overflow-hidden min-w-0">

        {/* TopBar */}
        <header className="fixed top-0 right-0 w-[calc(100%-240px)] border-b border-slate-200 shadow-sm bg-white/80 backdrop-blur-md flex justify-between items-center h-16 px-8 z-40">
          <div className="flex items-center gap-4 flex-1">
            <h2 className="text-[24px] font-semibold text-slate-900 leading-none tracking-tight mr-4 shrink-0">
              {pageTitle}
            </h2>
            {view === 'dashboard' && (
              <div className="relative w-64 hidden sm:block">
                <span className="material-symbols-outlined absolute left-3 top-1/2 -translate-y-1/2 text-slate-400 text-[18px]">search</span>
                <input
                  value={search}
                  onChange={e => setSearch(e.target.value)}
                  className="w-full bg-slate-50 border border-slate-200 rounded-full py-1.5 pl-9 pr-4 text-[13px] focus:outline-none focus:border-primary focus:ring-1 focus:ring-primary transition-colors"
                  placeholder="Search logs…"
                />
              </div>
            )}
          </div>
          <div className="flex items-center gap-1">
            <button className="p-2 rounded-full text-slate-500 hover:bg-slate-100 transition-colors">
              <span className="material-symbols-outlined text-[22px]">notifications</span>
            </button>
            <button className="p-2 rounded-full text-slate-500 hover:bg-slate-100 transition-colors">
              <span className="material-symbols-outlined text-[22px]">history</span>
            </button>
            <div className="w-px h-6 bg-slate-200 mx-2" />
            <div className="w-8 h-8 rounded-[50%] bg-primary flex items-center justify-center text-on-primary text-[13px] font-bold cursor-pointer">
              A
            </div>
          </div>
        </header>

        {/* Main canvas */}
        <main className="flex-1 overflow-y-auto bg-background" style={{ paddingTop: 88, paddingLeft: 24, paddingRight: 24, paddingBottom: 24 }}>
          <div className="max-w-7xl mx-auto">
            {view === 'dashboard' && <CallHistory searchQuery={search} />}
            {view === 'settings' && (
              <div className="max-w-2xl">
                <div className="bg-surface rounded-xl border border-outline-variant shadow-sm p-6">
                  <p className="text-[11px] font-bold uppercase tracking-widest text-on-surface-variant mb-4">System Prompt</p>
                  <SystemPromptEditor />
                </div>
              </div>
            )}
          </div>
        </main>
      </div>

      {/* ── Start Call Slide-in Panel ─────────────────── */}
      <StartCallPanel open={panelOpen} onClose={() => setPanelOpen(false)} />
    </div>
  );
}
