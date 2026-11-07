'use client';
/* Shared laptop page patterns (≥1024px, lib/desktop.ts; the CSS lives in app/desktop.css):
   - DeskDetail: a reading/working column and a sticky right rail (facts, the main action, contacts);
   - PlaceCard: one campus place (café, print shop) as a card in a .desk-grid, with its key facts.
   Phones never render these: a screen asks useDesktop() and keeps its single-column markup otherwise. */
import type { ReactNode } from 'react';
import { useApp } from '@/components/app';
import { Icon } from '@/components/ui';
import type { IconName } from '@/lib/icons';
import { useDesktop } from '@/lib/desktop';

/** content column + right rail; `label` names the rail for screen readers */
export function DeskDetail({ main, rail, label, className = '' }: { main: ReactNode; rail: ReactNode; label: string; className?: string }) {
  return (
    <div className={`desk-detail ${className}`.trim()}>
      <div className="desk-main">{main}</div>
      <aside className="desk-rail" aria-label={label}>{rail}</aside>
    </div>
  );
}

export interface PlaceFact { icon: IconName; text: string }

/** a café or print shop in the laptop grid: name, open/closed, facts, one clear action */
export function PlaceCard({ go, icon, name, open, openLabel, closedLabel, facts, badge, cta }: {
  go: string; icon: IconName; name: string; open: boolean; openLabel: string; closedLabel: string;
  facts: PlaceFact[]; badge?: ReactNode; cta: string;
}) {
  const { go: nav } = useApp();
  return (
    <button type="button" className={`card place-card${open ? ' is-open' : ''}`} onClick={() => nav(go)}>
      <span className="place-top">
        <span className={`ico ${open ? 'nico-2' : ''}`}><Icon n={icon} /></span>
        <span className={`badge ${open ? 'b-ok' : 'b-line'}`}>{open ? openLabel : closedLabel}</span>
      </span>
      <span className="place-name">{name}{badge ? <> {badge}</> : null}</span>
      <span className="place-facts">
        {facts.map(f => (f.icon === 'pin' ? { ...f, text: placeParts(f.text)[0] } : f)).filter(f => f.text)
          .map(f => <span key={f.icon + f.text} className="place-fact"><Icon n={f.icon} s={16} /><span>{f.text}</span></span>)}
        {facts.filter(f => f.icon === 'pin').map(f => placeParts(f.text)[1]).filter(Boolean)
          .map(n => <span key={n} className="tiny muted place-note">{n}</span>)}
      </span>
      <span className="place-cta">{cta}<Icon n="chev" s={16} /></span>
    </button>
  );
}

/** open places first, then by name (the laptop grid; the phone list keeps the server's order) */
export function openFirst<T extends { openNow: boolean }>(list: T[], name: (x: T) => string): T[] {
  return [...list].sort((a, b) => Number(b.openNow) - Number(a.openNow) || name(a).localeCompare(name(b)));
}

/** «Kaspi · кассада»: how a shop takes money (a laptop card's fact line) */
export function payLine(payments: ('pickup' | 'kaspi')[], tt: (kk: string, ru: string) => string): string {
  return payments.map(p => (p === 'kaspi' ? 'Kaspi' : tt('кассада', 'на кассе'))).join(' · ');
}

/** a list with filters: the filters (and the page's actions) in a left panel, the results beside them */
export function DeskList({ filters, children, label }: { filters: ReactNode; children: ReactNode; label: string }) {
  return (
    <DeskPage wide className="search-layout desk-list" panelLabel={label} panel={<div className="search-side">{filters}</div>}>
      <div className="search-main">{children}</div>
    </DeskPage>
  );
}

/** a form on a laptop: the form in a centred column, a help card beside it (tips, rules, what happens next).
    Phones get the form alone, unchanged. */
export function DeskForm({ title, tips, extra, children }: { title: string; tips: string[]; extra?: ReactNode; children: ReactNode }) {
  const desktop = useDesktop();
  if (!desktop) return <>{children}</>;
  return (
    <div className="desk-form">
      <div className="desk-form-main">{children}</div>
      <aside className="desk-aside card" aria-label={title}>
        <h2 className="rail-h"><Icon n="spark" s={18} /> {title}</h2>
        <ul className="desk-tips">{tips.map(t => <li key={t}>{t}</li>)}</ul>
        {extra}
      </aside>
    </div>
  );
}

/** a place's location with any sample notice taken out of it: «Бас ғимарат, Үлгі: тапсырыс нақты орындалмайды» ->
    ['Бас ғимарат', 'Үлгі: тапсырыс нақты орындалмайды'], so the notice gets its own line instead of reading as an address */
export function placeParts(text: string): [string, string] {
  const m = /(?:^|,\s*)((?:Үлгі|Пример|Sample|Demo|Демо)\s*[:—–-].*)$/i.exec(text || '');
  if (!m) return [text || '', ''];
  return [text.slice(0, m.index).replace(/[,\s]+$/, ''), m[1].trim()];
}

/** a café or print shop in the phone list: the name and its open/closed badge on one line, the place and hours under
    it at full width, a sample notice on its own line */
export function PlaceRow({ go, icon, name, open, openLabel, closedLabel, place, hours, demo }: {
  go: string; icon: IconName; name: string; open: boolean; openLabel: string; closedLabel: string;
  place: string; hours: string; demo?: ReactNode;
}) {
  const { go: nav } = useApp();
  const [where, note] = placeParts(place);
  return (
    <button className="row place-row" type="button" onClick={() => nav(go)}>
      <span className={`ico ${open ? 'nico-2' : ''}`}><Icon n={icon} /></span>
      <span className="row-main">
        <span className="place-row-top"><span className="row-title">{name}</span>
          <span className={`badge ${open ? 'b-ok' : 'b-line'}`}>{open ? openLabel : closedLabel}</span>{demo}</span>
        <span className="row-sub">{[where, hours].filter(Boolean).join(' · ')}</span>
        {note ? <span className="tiny muted place-note">{note}</span> : null}
      </span>
      <span className="chev"><Icon n="chev" s={18} /></span>
    </button>
  );
}

/** The one laptop page layout: an optional filter/section panel (≤300px), the main column (≤760px, or the rest of the
    width with `wide` for card grids) and an optional rail (340px). Every page starts at the same left edge.
    A phone gets the same blocks stacked in reading order: panel, main, rail. */
export function DeskPage({ panel, panelLabel, rail, railLabel, wide, className = '', children }: {
  panel?: ReactNode; panelLabel?: string; rail?: ReactNode; railLabel?: string; wide?: boolean; className?: string; children: ReactNode;
}) {
  const desktop = useDesktop();
  if (!desktop) return <>{panel}{children}{rail}</>;
  const cls = ['desk-page', panel ? 'has-panel' : '', rail ? 'has-rail' : '', wide ? 'is-wide' : '', className].filter(Boolean).join(' ');
  return (
    <div className={cls}>
      {panel ? <aside className="desk-page-panel" aria-label={panelLabel}>{panel}</aside> : null}
      <div className="desk-page-main">{children}</div>
      {rail ? <aside className="desk-page-rail" aria-label={railLabel}>{rail}</aside> : null}
    </div>
  );
}
