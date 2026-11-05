'use client';
/* Pieces the campus shops share (the café, cafe*.tsx, and the print shop, print*.tsx; backend/internal/shopkit): the
   Kaspi payment block for a student, the counter's sound, and the admin's Kaspi cashier login and staff panels. */
import { useState, useSyncExternalStore } from 'react';
import { useApp } from '@/components/app';
import { Qr } from '@/components/ui';
import { ErrorLine, errText, hhmm } from '@/components/views/common';
import { api } from '@/lib/account';
import { preferLink } from '@/lib/cafe';
import { useTelegram, type TgApp } from '@/lib/telegram';

export interface PayInfo { url: string; qr: string; expiresAt: string | null }
export interface KaspiState { configured: boolean; loggedIn: boolean; broken: boolean; phoneMasked: string; orgName: string; lastError: string | null; updatedAt: string | null }
export interface StaffRow { userId: number; name: string | null; username: string | null; role: string }

const noSub = () => () => {};

/** the Kaspi link (a phone, the Mini App) or QR (a wide screen) of a pending payment, in the shop's own name */
export function KaspiPay({ pay }: { pay: PayInfo | null }) {
  const { tt } = useApp();
  // the server render (and the first paint) assume a phone: the link first
  const link = useSyncExternalStore(noSub, () => preferLink(window), () => true);
  const tg = useTelegram().app as (TgApp & { openLink?: (url: string) => void }) | null;
  if (!pay) return null;
  return (
    <div className="cafe-kaspi">
      <a className="btn block" href={pay.url} target="_blank" rel="noopener" id="kaspiLink" onClick={e => {
        // inside the Telegram Mini App the link must leave Telegram to reach the Kaspi app
        if (tg?.openLink) { e.preventDefault(); tg.openLink(pay.url); }
      }}>{tt('Kaspi қосымшасында ашу', 'Открыть в приложении Kaspi')}</a>
      {link ? (
        <details><summary className="link-btn">{tt('QR кодты көрсету', 'Показать QR-код')}</summary><Qr text={pay.qr || pay.url} /></details>
      ) : (
        <><p className="small muted">{tt('Телефондағы Kaspi-мен сканерлеңіз:', 'Отсканируйте в Kaspi на телефоне:')}</p><Qr text={pay.qr || pay.url} /></>
      )}
      {pay.expiresAt ? <p className="tiny muted num">{tt(`QR ${hhmm(pay.expiresAt)} дейін жарамды`, `QR действует до ${hhmm(pay.expiresAt)}`)}</p> : null}
    </div>
  );
}

/** a short beep for the counter's tablet (only after a tap: browsers allow sound then) */
export function beep() {
  try {
    const Ctx = window.AudioContext || (window as unknown as { webkitAudioContext: typeof AudioContext }).webkitAudioContext;
    const ctx = new Ctx();
    const o = ctx.createOscillator(), g = ctx.createGain();
    o.frequency.value = 880; g.gain.value = 0.15;
    o.connect(g); g.connect(ctx.destination);
    o.start(); o.stop(ctx.currentTime + 0.25);
    setTimeout(() => { void ctx.close(); }, 500);
  } catch { /* no audio */ }
}

/** admin: the shop's Kaspi cashier login (the shop added our cashier number in Kaspi Pay; SMS code step).
    base is the shop's admin path, e.g. /api/admin/cafes/3 or /api/admin/print/shops/2 */
export function KaspiPanel({ kaspi, base, onDone }: { kaspi?: KaspiState; base: string; onDone: () => void }) {
  const { tt, lang, toast } = useApp();
  const [phone, setPhone] = useState('');
  const [otp, setOtp] = useState('');
  const [pid, setPid] = useState('');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  const k = kaspi;
  const call = async (fn: () => Promise<void>) => { setBusy(true); setErr(''); try { await fn(); } catch (x) { setErr(errText(x)); } finally { setBusy(false); } };
  return (
    <div className="cafe-kpanel">
      <p className="small"><b>Kaspi:</b>{' '}
        {!k?.loggedIn ? tt('кассир кірмеген', 'кассир не вошёл') : k.broken ? <span className="urgent">{tt('сессия үзілді — қайта кіріңіз', 'сессия прервалась — войдите заново')}</span>
          : tt(`қосулы · ${k.phoneMasked} · ${k.orgName}`, `подключено · ${k.phoneMasked} · ${k.orgName}`)}</p>
      {k?.configured ? (
        <>
          {!pid ? (
            <form className="pts-inline" onSubmit={e => { e.preventDefault(); void call(async () => {
              const r = await api<{ processId: string }>(`${base}/kaspi/login/start`, { method: 'POST', body: { phone }, lang });
              setPid(r.processId); toast(tt('SMS кассир нөміріне жіберілді', 'SMS отправлено на номер кассира'));
            }); }}>
              <input className="input" aria-label={tt('Кассир нөмірі', 'Номер кассира')} placeholder="+7 7__ ___ __ __" inputMode="tel" value={phone} onChange={e => setPhone(e.target.value)} />
              <button className="btn secondary" type="submit" disabled={busy || phone.length < 10}>{tt('SMS', 'SMS')}</button>
            </form>
          ) : (
            <form className="pts-inline" onSubmit={e => { e.preventDefault(); void call(async () => {
              await api(`${base}/kaspi/login/verify`, { method: 'POST', body: { processId: pid, otp }, lang });
              setPid(''); setOtp(''); toast(tt('Kaspi кассир сессиясы сақталды', 'Сессия кассира Kaspi сохранена')); onDone();
            }); }}>
              <input className="input num" aria-label={tt('SMS коды', 'SMS-код')} inputMode="numeric" autoComplete="one-time-code" value={otp} onChange={e => setOtp(e.target.value)} />
              <button className="btn" type="submit" disabled={busy || otp.length < 4}>{tt('Растау', 'Подтвердить')}</button>
            </form>
          )}
          {k.loggedIn ? (
            <div className="btn-row">
              <button className="btn ghost" type="button" disabled={busy} onClick={() => void call(async () => {
                const r = await api<{ active: boolean }>(`${base}/kaspi/check`, { method: 'POST', lang });
                toast(r.active ? tt('Сессия жұмыс істейді', 'Сессия работает') : tt('Сессия жұмыс істемейді', 'Сессия не работает')); onDone();
              })}>{tt('Тексеру', 'Проверить')}</button>
              <button className="btn ghost" type="button" disabled={busy} onClick={() => void call(async () => {
                await api(`${base}/kaspi`, { method: 'DELETE', lang }); onDone();
              })}>{tt('Ажырату', 'Отключить')}</button>
            </div>
          ) : null}
        </>
      ) : null}
      <ErrorLine err={err} />
    </div>
  );
}

/** admin: the shop's staff (a site account by id, with a role) */
export function StaffPanel({ staff, base, roles, onDone }: { staff?: StaffRow[]; base: string; roles: [string, string][]; onDone: () => void }) {
  const { tt, lang } = useApp();
  const [uid, setUid] = useState('');
  const [role, setRole] = useState(roles[0][0]);
  const [err, setErr] = useState('');
  const roleName = (r: string) => roles.find(([v]) => v === r)?.[1] ?? r;
  return (
    <div className="cafe-kpanel">
      <p className="small"><b>{tt('Қызметкерлер', 'Сотрудники')}:</b> {(staff ?? []).length ? '' : tt('жоқ', 'нет')}</p>
      {(staff ?? []).map(s => (
        <div className="btn-row" key={s.userId}>
          <span className="small">{s.name || '—'}{s.username ? ` @${s.username}` : ''} · id {s.userId} · {roleName(s.role)}</span>
          <button className="btn ghost" type="button" onClick={async () => { try { await api(`${base}/staff/${s.userId}`, { method: 'DELETE', lang }); onDone(); } catch (x) { setErr(errText(x)); } }}>{tt('Алып тастау', 'Убрать')}</button>
        </div>
      ))}
      <form className="pts-inline" onSubmit={async e => {
        e.preventDefault();
        try { await api(`${base}/staff`, { method: 'POST', body: { userId: Number(uid), role }, lang }); setUid(''); onDone(); }
        catch (x) { setErr(errText(x)); }
      }}>
        <input className="input num" inputMode="numeric" aria-label={tt('Пайдаланушы id', 'id пользователя')} placeholder="user id" value={uid} onChange={e => setUid(e.target.value)} />
        <select className="input" aria-label={tt('Рөлі', 'Роль')} value={role} onChange={e => setRole(e.target.value)}>
          {roles.map(([v, l]) => <option key={v} value={v}>{l}</option>)}
        </select>
        <button className="btn secondary" type="submit" disabled={!/^\d+$/.test(uid)}>{tt('Қосу', 'Добавить')}</button>
      </form>
      <ErrorLine err={err} />
    </div>
  );
}

/** an order's append-only audit trail, as the admin sees it */
export function AuditTrail({ events }: { events?: { id: number; actorId: number | null; actorKind: string; kind: string; detail: Record<string, unknown>; createdAt: string }[] }) {
  return (
    <ol className="cafe-audit">
      {(events ?? []).map(e => (
        <li key={e.id}><span className="num tiny muted">{e.createdAt.slice(0, 10)} {hhmm(e.createdAt)}</span> <b>{e.kind}</b> <span className="tiny muted">{e.actorKind}{e.actorId ? ` #${e.actorId}` : ''}</span>
          {Object.keys(e.detail || {}).length ? <code className="tiny">{JSON.stringify(e.detail)}</code> : null}</li>
      ))}
    </ol>
  );
}
