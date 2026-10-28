'use client';
/* Small building blocks with the exact legacy markup (row, chip, seg, section head, notes, share row, QR). */
import type { ReactNode } from 'react';
import qrcode from 'qrcode-generator';
import { IC, type IconName } from '@/lib/icons';
import { useApp, copyText } from '@/components/app';
import { track, withShareRef } from '@/features/analytics/track';

export function Icon({ n, s = 22, className }: { n: IconName; s?: number; className?: string }) {
  return (
    <svg viewBox="0 0 24 24" width={s} height={s} fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round"
      strokeLinejoin="round" aria-hidden="true" className={className} dangerouslySetInnerHTML={{ __html: IC[n] }} />
  );
}

interface RowProps {
  go?: string;
  onClick?: () => void;
  icon?: IconName;
  tone?: string;
  title: ReactNode;
  sub?: ReactNode;
  end?: ReactNode;
  chev?: boolean;
  href?: string;
}
export function Row({ go, onClick, icon, tone = '', title, sub, end, chev = true, href }: RowProps) {
  const app = useApp();
  const inner = (
    <>
      {icon ? <span className={`ico ${tone}`}><Icon n={icon} /></span> : null}
      <span className="row-main"><span className="row-title">{title}</span>{sub ? <span className="row-sub">{sub}</span> : null}</span>
      {end ? <span className="row-end">{end}</span> : null}
      {chev ? <span className="chev"><Icon n={href ? 'ext' : 'chev'} s={18} /></span> : null}
    </>
  );
  if (href) return <a className="row" href={href} target="_blank" rel="noopener">{inner}</a>;
  if (!go && !onClick) return <div className="row">{inner}</div>;
  return <button className="row" type="button" onClick={() => (onClick ? onClick() : app.go(go!))}>{inner}</button>;
}

export function Chip({ pressed, onClick, children }: { pressed?: boolean; onClick: () => void; children: ReactNode }) {
  return <button className="chip" type="button" aria-pressed={!!pressed} onClick={onClick}>{children}</button>;
}

export function Seg({ items, cur, onPick }: { items: [string, string][]; cur: string; onPick: (k: string) => void }) {
  return (
    <div className="seg" role="tablist">
      {/* legacy markup: the stylesheet keys the selected segment on aria-pressed */}
      {/* eslint-disable-next-line jsx-a11y/role-supports-aria-props */}
      {items.map(([k, l]) => <button key={k} type="button" role="tab" aria-pressed={k === cur} onClick={() => onPick(k)}>{l}</button>)}
    </div>
  );
}

export const SectionHead = ({ title, more }: { title: ReactNode; more?: ReactNode }) => (
  <div className="section-head"><h2>{title}</h2>{more}</div>
);
export const Note = ({ children }: { children: ReactNode }) => <p className="note">{children}</p>;

export function Empty({ icon, children, action }: { icon?: IconName; children: ReactNode; action?: ReactNode }) {
  return <div className="empty">{icon ? <Icon n={icon} s={28} /> : null}<span>{children}</span>{action}</div>;
}

/** «үлгі» marker for demo records (cards and detail) */
export function DemoBadge({ show }: { show: boolean }) {
  const { tt } = useApp();
  if (!show) return null;
  return <span className="badge b-line b-demo">{tt('үлгі', 'пример')}</span>;
}

/** Bytes of the UTF-8 encoding as a latin-1 string, so the byte-mode QR carries Cyrillic names intact. */
function utf8Latin1(s: string) {
  return Array.from(new TextEncoder().encode(s), b => String.fromCharCode(b)).join('');
}
export function Qr({ text }: { text: string }) {
  let svg = '';
  try {
    const q = qrcode(0, 'M');
    q.addData(utf8Latin1(text));
    q.make();
    svg = q.createSvgTag({ cellSize: 4, margin: 8, scalable: true });
  } catch {
    return <div className="qr"><div className="code">{text}</div></div>;
  }
  return <div className="qr" dangerouslySetInnerHTML={{ __html: svg }} />;
}

/** «Бөлісу» (native share on touch devices, otherwise copy) + Telegram, for the real URL of a screen.
    The shared link carries ?ref=share so visits from friends can be counted (anonymously). */
export function ShareRow({ path, title, kind, id }: { path: string; title: string; kind: string; id: string }) {
  const { tt, toast } = useApp();
  const url = typeof window !== 'undefined' ? withShareRef(window.location.origin + path) : path;
  const text = title + ' — Student Hub';
  const tg = 'https://t.me/share/url?url=' + encodeURIComponent(url) + '&text=' + encodeURIComponent(text);
  const share = () => {
    if (navigator.share && window.matchMedia('(pointer: coarse)').matches) {
      track('share_click', { kind, id, channel: 'native' });
      navigator.share({ title: text, text, url }).catch(() => { /* cancelled */ });
    } else {
      track('share_click', { kind, id, channel: 'copy' });
      copyText(url, toast, tt);
    }
  };
  return (
    <div className="share-row">
      <button className="chip" type="button" onClick={share}><Icon n="share" s={16} /> {tt('Бөлісу', 'Поделиться')}</button>
      <a className="chip" href={tg} target="_blank" rel="noopener" onClick={() => track('share_click', { kind, id, channel: 'telegram' })}><Icon n="send" s={16} /> Telegram</a>
    </div>
  );
}

/** «Иә, жою» confirmation sheet */
export function ConfirmSheet({ msg, yes }: { msg: string; yes: () => void }) {
  const { tt, closeSheet, closeSheetDom } = useApp();
  return (
    <>
      <h2>{msg}</h2>
      <div className="btn-row">
        <button className="btn ghost" type="button" onClick={closeSheet}>{tt('Жоқ', 'Нет')}</button>
        <button className="btn danger" type="button" onClick={() => { closeSheetDom(); yes(); }}>{tt('Иә, жою', 'Да, удалить')}</button>
      </div>
    </>
  );
}
