'use client';
/* Shared laptop page patterns (≥1024px, lib/desktop.ts; the CSS lives in app/desktop.css):
   - DeskDetail: a reading/working column and a sticky right rail (facts, the main action, contacts);
   - PlaceCard: one campus place (café, print shop) as a card in a .desk-grid, with its key facts.
   Phones never render these: a screen asks useDesktop() and keeps its single-column markup otherwise. */
import type { ReactNode } from 'react';
import { useApp } from '@/components/app';
import { Icon } from '@/components/ui';
import type { IconName } from '@/lib/icons';

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
        {facts.filter(f => f.text).map(f => <span key={f.icon + f.text} className="place-fact"><Icon n={f.icon} s={16} /><span>{f.text}</span></span>)}
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
    <div className="search-layout desk-list">
      <aside className="search-side" aria-label={label}>{filters}</aside>
      <div className="search-main">{children}</div>
    </div>
  );
}
