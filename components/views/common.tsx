'use client';
/* Shared pieces of the account-backed features (events, ambassador, mentors, market, moderation): the neutral
   not-found screen, feature/role gates, the sign-in prompt, a small fetch hook and Almaty date helpers. */
import { useCallback, useEffect, useState, type ReactNode } from 'react';
import { useApp } from '@/components/app';
import { Empty, Icon } from '@/components/ui';
import { ConsentSheet, SignInSheet } from '@/components/views/account';
import { api, ApiError, useAccount, type AccountState } from '@/lib/account';
import { featureOn, useConfig, useConfigReady, type FeatureName } from '@/lib/config';

/** the same screen as an unknown page: a switched-off feature or a foreign role must not be distinguishable */
export function NotFound() {
  const { tt, go } = useApp();
  return <Empty icon="search" action={<button className="btn secondary" type="button" onClick={() => go('home')}>{tt('Басты бетке', 'На главную')}</button>}>{tt('Мұндай бет жоқ.', 'Такой страницы нет.')}</Empty>;
}

export function Loading() {
  const { tt } = useApp();
  return <p className="muted small" role="status">{tt('Жүктеліп жатыр…', 'Загрузка…')}</p>;
}

export const errText = (e: unknown, fb = '') => (e instanceof ApiError || e instanceof Error ? e.message : fb);
export const errCode = (e: unknown) => (e instanceof ApiError ? e.code : '');

export function ErrorLine({ err }: { err: string }) {
  return err ? <p className="small urgent" role="alert">{err}</p> : null;
}

/** renders children only while the feature switch is on; waits for /api/config first */
export function Feature({ name, children }: { name: FeatureName; children: ReactNode }) {
  const cfg = useConfig();
  const ready = useConfigReady();
  if (!ready) return <Loading />;
  if (!featureOn(cfg, name)) return <NotFound />;
  return <>{children}</>;
}

/** true when the switch is on (false while /api/config is loading, so entry points never flash) */
export function useFeature(name: FeatureName): boolean {
  const cfg = useConfig();
  return featureOn(cfg, name);
}

/** admin-only screens: everyone else sees the neutral not-found screen */
export function AdminOnly({ children }: { children: ReactNode }) {
  const acc = useAccount();
  if (!acc.loaded) return <Loading />;
  if (acc.user?.role !== 'admin') return <NotFound />;
  return <>{children}</>;
}

/** Opens the «Telegram арқылы кір» / consent sheet when the action needs an account. Returns true when ready. */
export function useNeedAccount(): () => boolean {
  const acc = useAccount();
  const { openSheet } = useApp();
  return useCallback(() => {
    if (!acc.user) { openSheet(<SignInSheet />); return false; }
    if (acc.needsConsent) { openSheet(<ConsentSheet />); return false; }
    return true;
  }, [acc.user, acc.needsConsent, openSheet]);
}

export const hasAccount = (acc: AccountState) => !!acc.user && !acc.needsConsent;

/** inline «sign in to continue» card for screens that need an account */
export function SignInCard({ text }: { text: string }) {
  const { tt } = useApp();
  const need = useNeedAccount();
  const acc = useAccount();
  return (
    <div className="card">
      <p className="small">{text}</p>
      <button className="btn block" type="button" onClick={need}>
        <Icon n="send" s={18} /> {acc.user && acc.needsConsent ? tt('Келісім беру', 'Дать согласие') : tt('Telegram арқылы кір', 'Войти через Telegram')}
      </button>
    </div>
  );
}

/** GET a JSON endpoint; `path` null = do not load. `reload()` refetches. */
export function useLoad<T>(path: string | null): { data: T | null; err: string; code: string; loading: boolean; reload: () => void } {
  const { lang } = useApp();
  const [n, setN] = useState(0);
  const key = path ? `${path}#${n}#${lang}` : '';
  const [res, setRes] = useState<{ key: string; data: T | null; err: string; code: string }>({ key: '', data: null, err: '', code: '' });
  useEffect(() => {
    if (!path) return;
    let live = true;
    api<T>(path, { lang }).then(
      d => { if (live) setRes({ key, data: d, err: '', code: '' }); },
      e => { if (live) setRes(r => ({ key, data: r.data, err: errText(e), code: errCode(e) })); },
    );
    return () => { live = false; };
  }, [path, key, lang]);
  const reload = useCallback(() => setN(x => x + 1), []);
  const loading = !!path && res.key !== key;
  return { data: path ? res.data : null, err: loading ? '' : res.err, code: loading ? '' : res.code, loading, reload };
}

/* ---------- dates: the API speaks UTC, people read Almaty time (UTC+5) ---------- */
const ALMATY_MS = 5 * 3600_000;
const pad = (n: number) => String(n).padStart(2, '0');
/** a Date whose local fields are the Almaty wall clock (for I.fDate and friends) */
export function almatyWall(iso: string): Date {
  const a = new Date(new Date(iso).getTime() + ALMATY_MS);
  return new Date(a.getUTCFullYear(), a.getUTCMonth(), a.getUTCDate(), a.getUTCHours(), a.getUTCMinutes());
}
export const hhmm = (iso: string) => { const d = almatyWall(iso); return `${pad(d.getHours())}:${pad(d.getMinutes())}`; };
/** '<input type=datetime-local>' value (Almaty) <-> RFC 3339 UTC */
export function toLocalInput(iso: string): string {
  const d = almatyWall(iso);
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}
export function fromLocalInput(v: string): string {
  const m = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})/.exec(v);
  if (!m) return '';
  return new Date(Date.UTC(+m[1], +m[2] - 1, +m[3], +m[4], +m[5]) - ALMATY_MS).toISOString().replace('.000Z', 'Z');
}
/** 'YYYY-MM-DD' of an RFC 3339 date-or-timestamp, read as a calendar day */
export const ymd = (s: string | null | undefined) => (s ? s.slice(0, 10) : '');

/** «N күн бұрын» */
export function agoDays(n: number, tt: (kk: string, ru: string) => string): string {
  if (n <= 0) return tt('бүгін', 'сегодня');
  if (n === 1) return tt('кеше', 'вчера');
  return tt(`${n} күн бұрын`, `${n} ${n % 10 === 1 && n % 100 !== 11 ? 'день' : n % 10 >= 2 && n % 10 <= 4 && (n % 100 < 10 || n % 100 >= 20) ? 'дня' : 'дней'} назад`);
}

/** a reason textarea + confirm button inside a sheet (admin decisions, rejections) */
export function ReasonSheet({ title, label, min = 3, max = 500, required = true, action, danger, onSubmit, extra }: {
  title: string; label: string; min?: number; max?: number; required?: boolean; action: string; danger?: boolean;
  onSubmit: (reason: string) => Promise<void>; extra?: ReactNode;
}) {
  const { tt, closeSheet } = useApp();
  const [reason, setReason] = useState('');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  const r = reason.trim();
  const ok = (!required && r.length === 0) || (r.length >= min && r.length <= max);
  return (
    <form className="form" onSubmit={async e => {
      e.preventDefault();
      if (!ok || busy) return;
      setBusy(true); setErr('');
      try { await onSubmit(r); } catch (e2) { setErr(errText(e2, tt('Болмады', 'Не получилось'))); } finally { setBusy(false); }
    }}>
      <h2>{title}</h2>
      {extra}
      <div className="field">
        <label htmlFor="rsReason">{label}</label>
        <textarea className="input" id="rsReason" maxLength={max} value={reason} onChange={e => setReason(e.target.value)} />
        <span className="tiny muted num">{r.length} / {max}</span>
      </div>
      <ErrorLine err={err} />
      <div className="btn-row">
        <button className="btn ghost" type="button" onClick={closeSheet}>{tt('Бас тарту', 'Отмена')}</button>
        <button className={`btn${danger ? ' danger' : ''}`} type="submit" disabled={!ok || busy}>{action}</button>
      </div>
    </form>
  );
}
