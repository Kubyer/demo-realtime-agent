'use client';

import { useState, useEffect } from 'react';

interface Contact { id: string; name: string; phone: string; }

const STORAGE_KEY = 'outreach_contacts';

function loadContacts(): Contact[] {
  if (typeof window === 'undefined') return [];
  try { return JSON.parse(localStorage.getItem(STORAGE_KEY) || '[]'); } catch { return []; }
}

function saveContacts(c: Contact[]) {
  localStorage.setItem(STORAGE_KEY, JSON.stringify(c));
}

type DialState = 'idle' | 'dialing' | 'success' | 'error';

export default function OutreachView() {
  const [contacts, setContacts] = useState<Contact[]>([]);
  const [phone, setPhone] = useState('');
  const [dialState, setDialState] = useState<DialState>('idle');
  const [dialMsg, setDialMsg] = useState('');
  const [addName, setAddName] = useState('');
  const [addPhone, setAddPhone] = useState('');
  const [showAdd, setShowAdd] = useState(false);

  useEffect(() => { setContacts(loadContacts()); }, []);

  const dial = async (number: string) => {
    if (!number.trim()) return;
    setDialState('dialing');
    setDialMsg('');
    try {
      const res = await fetch('/api/outreach/call', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ to: number.trim() }),
      });
      let data: any = {};
      try { data = await res.json(); } catch { /* non-JSON body */ }
      if (!res.ok) {
        setDialState('error');
        setDialMsg(data.error || `HTTP ${res.status} — check that the backend is running and Twilio env vars are set`);
      } else {
        setDialState('success');
        setDialMsg(`Calling… SID: ${data.call_sid}`);
        setTimeout(() => setDialState('idle'), 6000);
      }
    } catch (e: any) {
      setDialState('error');
      setDialMsg(e.message);
    }
  };

  const addContact = () => {
    if (!addPhone.trim()) return;
    const c: Contact = { id: Date.now().toString(), name: addName.trim() || addPhone.trim(), phone: addPhone.trim() };
    const updated = [...contacts, c];
    setContacts(updated);
    saveContacts(updated);
    setAddName('');
    setAddPhone('');
    setShowAdd(false);
  };

  const deleteContact = (id: string) => {
    const updated = contacts.filter(c => c.id !== id);
    setContacts(updated);
    saveContacts(updated);
  };

  return (
    <div className="max-w-3xl flex flex-col gap-6">

      {/* Header info */}
      <div className="bg-primary/5 border border-primary/20 rounded-xl px-5 py-4 flex items-start gap-3">
        <span className="material-symbols-outlined text-primary text-[22px] mt-0.5" style={{ fontVariationSettings: "'FILL' 1" }}>campaign</span>
        <div>
          <p className="text-[14px] font-semibold text-on-surface mb-0.5">Outbound Calls via Twilio</p>
          <p className="text-[12px] text-on-surface-variant">
            Léa calls the number, the voice agent handles the conversation. Requires{' '}
            <code className="font-mono bg-surface px-1 rounded">TWILIO_ACCOUNT_SID</code>,{' '}
            <code className="font-mono bg-surface px-1 rounded">TWILIO_AUTH_TOKEN</code> and{' '}
            <code className="font-mono bg-surface px-1 rounded">TWILIO_FROM_NUMBER</code> in your <code className="font-mono bg-surface px-1 rounded">.env</code>.
          </p>
        </div>
      </div>

      {/* Dial pad */}
      <div className="bg-surface rounded-xl border border-outline-variant shadow-[0px_4px_12px_rgba(15,23,42,0.05)] p-6">
        <p className="text-[11px] font-bold uppercase tracking-widest text-on-surface-variant mb-4">New Outbound Call</p>
        <div className="flex gap-3">
          <div className="relative flex-1">
            <span className="material-symbols-outlined absolute left-3 top-1/2 -translate-y-1/2 text-on-surface-variant text-[18px]">call</span>
            <input
              value={phone}
              onChange={e => { setPhone(e.target.value); setDialState('idle'); }}
              onKeyDown={e => e.key === 'Enter' && dial(phone)}
              placeholder="+33 6 12 34 56 78"
              className="w-full bg-surface-container-lowest border border-outline-variant rounded-lg py-2.5 pl-10 pr-4 text-[14px] font-mono focus:outline-none focus:border-primary focus:ring-1 focus:ring-primary transition-colors"
            />
          </div>
          <button
            onClick={() => dial(phone)}
            disabled={dialState === 'dialing' || !phone.trim()}
            className="px-5 py-2.5 rounded-lg bg-primary text-on-primary text-[13px] font-semibold flex items-center gap-2 disabled:opacity-50 disabled:cursor-not-allowed hover:opacity-90 transition-opacity"
          >
            {dialState === 'dialing' ? (
              <span className="w-4 h-4 rounded-full border-2 border-on-primary border-t-transparent animate-spin" />
            ) : (
              <span className="material-symbols-outlined text-[18px]" style={{ fontVariationSettings: "'FILL' 1" }}>call</span>
            )}
            {dialState === 'dialing' ? 'Calling…' : 'Call'}
          </button>
        </div>

        {dialState === 'success' && (
          <div className="mt-3 flex items-center gap-2 text-[12px] text-secondary bg-secondary/5 border border-secondary/20 rounded-lg px-3 py-2">
            <span className="material-symbols-outlined text-[16px]">check_circle</span>
            {dialMsg}
          </div>
        )}
        {dialState === 'error' && (
          <div className="mt-3 flex items-start gap-2 text-[12px] text-red-600 bg-red-50 border border-red-200 rounded-lg px-3 py-2">
            <span className="material-symbols-outlined text-[16px] mt-0.5 shrink-0">error</span>
            {dialMsg}
          </div>
        )}
      </div>

      {/* Contacts */}
      <div className="bg-surface rounded-xl border border-outline-variant shadow-[0px_4px_12px_rgba(15,23,42,0.05)] overflow-hidden">
        <div className="flex items-center justify-between px-5 py-3 border-b border-outline-variant">
          <p className="text-[11px] font-bold uppercase tracking-widest text-on-surface-variant">Contacts</p>
          <button
            onClick={() => setShowAdd(v => !v)}
            className="flex items-center gap-1.5 text-[12px] font-semibold text-primary hover:underline"
          >
            <span className="material-symbols-outlined text-[16px]">add</span>
            Add contact
          </button>
        </div>

        {showAdd && (
          <div className="px-5 py-4 bg-surface-container-lowest border-b border-outline-variant flex gap-3 flex-wrap items-end">
            <div className="flex flex-col gap-1">
              <label className="text-[11px] font-bold text-on-surface-variant uppercase tracking-wider">Name</label>
              <input
                value={addName}
                onChange={e => setAddName(e.target.value)}
                placeholder="Acme Corp"
                className="bg-surface border border-outline-variant rounded-lg px-3 py-2 text-[13px] focus:outline-none focus:border-primary focus:ring-1 focus:ring-primary w-44"
              />
            </div>
            <div className="flex flex-col gap-1">
              <label className="text-[11px] font-bold text-on-surface-variant uppercase tracking-wider">Phone (E.164)</label>
              <input
                value={addPhone}
                onChange={e => setAddPhone(e.target.value)}
                onKeyDown={e => e.key === 'Enter' && addContact()}
                placeholder="+33612345678"
                className="bg-surface border border-outline-variant rounded-lg px-3 py-2 text-[13px] font-mono focus:outline-none focus:border-primary focus:ring-1 focus:ring-primary w-44"
              />
            </div>
            <button
              onClick={addContact}
              disabled={!addPhone.trim()}
              className="px-4 py-2 rounded-lg bg-primary text-on-primary text-[13px] font-semibold disabled:opacity-40"
            >
              Save
            </button>
            <button onClick={() => setShowAdd(false)} className="px-3 py-2 text-[13px] text-on-surface-variant hover:text-on-surface">
              Cancel
            </button>
          </div>
        )}

        {contacts.length === 0 && !showAdd && (
          <p className="px-5 py-10 text-center text-[13px] text-on-surface-variant">
            No contacts yet — add one above or dial a number directly.
          </p>
        )}

        <div className="divide-y divide-outline-variant">
          {contacts.map(c => (
            <div key={c.id} className="flex items-center px-5 py-3 hover:bg-surface-container-lowest transition-colors group">
              <div className="w-8 h-8 rounded-full bg-primary/10 text-primary flex items-center justify-center text-[13px] font-bold shrink-0 mr-3">
                {c.name.charAt(0).toUpperCase()}
              </div>
              <div className="flex-1 min-w-0">
                <p className="text-[13px] font-medium text-on-surface truncate">{c.name}</p>
                <p className="text-[12px] text-on-surface-variant font-mono">{c.phone}</p>
              </div>
              <div className="flex items-center gap-2 opacity-0 group-hover:opacity-100 transition-opacity">
                <button
                  onClick={() => { setPhone(c.phone); window.scrollTo({ top: 0, behavior: 'smooth' }); }}
                  className="flex items-center gap-1 px-3 py-1.5 rounded-lg bg-primary text-on-primary text-[12px] font-semibold hover:opacity-90"
                >
                  <span className="material-symbols-outlined text-[14px]" style={{ fontVariationSettings: "'FILL' 1" }}>call</span>
                  Call
                </button>
                <button
                  onClick={() => deleteContact(c.id)}
                  className="p-1.5 rounded-lg text-on-surface-variant hover:text-red-500 hover:bg-red-50 transition-colors"
                >
                  <span className="material-symbols-outlined text-[18px]">delete</span>
                </button>
              </div>
            </div>
          ))}
        </div>
      </div>

      {/* Upcoming channels */}
      <div className="bg-surface rounded-xl border border-outline-variant p-5">
        <p className="text-[11px] font-bold uppercase tracking-widest text-on-surface-variant mb-4">Coming Soon in Logs</p>
        <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
          {[
            { icon: 'sms', label: 'SMS', desc: 'Auto-reply to inbound SMS via Twilio. Webhook ready at /twilio/sms.', blocker: 'Point your Twilio SMS webhook URL → live today' },
            { icon: 'email', label: 'Email', desc: 'Process inbound emails with LLM and reply.', blocker: 'Needs SendGrid Inbound Parse or Mailgun domain setup' },
            { icon: 'confirmation_number', label: 'Tickets', desc: 'Handle support tickets from CRM / helpdesk.', blocker: 'Needs Zendesk / HubSpot / Intercom webhook integration' },
          ].map(ch => (
            <div key={ch.label} className="bg-surface-container rounded-lg p-4 flex flex-col gap-2">
              <div className="flex items-center gap-2">
                <span className="material-symbols-outlined text-on-surface-variant text-[20px]">{ch.icon}</span>
                <span className="text-[13px] font-semibold text-on-surface">{ch.label}</span>
                {ch.label === 'SMS' ? (
                  <span className="ml-auto text-[10px] font-bold uppercase px-1.5 py-0.5 rounded bg-secondary/10 text-secondary border border-secondary/20">Ready</span>
                ) : (
                  <span className="ml-auto text-[10px] font-bold uppercase px-1.5 py-0.5 rounded bg-surface-variant text-on-surface-variant border border-outline-variant">Soon</span>
                )}
              </div>
              <p className="text-[12px] text-on-surface-variant">{ch.desc}</p>
              <p className="text-[11px] text-on-surface-variant/70 italic">{ch.blocker}</p>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}
