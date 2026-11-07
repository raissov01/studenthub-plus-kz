'use client';
/* Optional account (docs/ACCOUNTS.md): Telegram, or email + password (docs/api/email-auth.md). The device profile works without it; the account only exists for
   actions that need the shared database. Session: an HttpOnly cookie set by the API; inside a Telegram Mini App the
   cookie may be blocked, so the token returned by /api/auth/telegram/webapp is kept in memory only and sent as
   «Authorization: Bearer». */
import { useSyncExternalStore } from 'react';
import { isOnline } from '@/lib/pwa';
import { clearTickets } from '@/lib/tickets';
import { apiLang, tr } from '@/lib/i18n';
import type { Lang } from '@/lib/types';

export type Role = 'student' | 'ambassador' | 'admin' | 'university_staff' | 'employer';
export interface AccountUser {
  id: number;
  /** null for an email-only account */
  telegramId: number | null;
  /** null until email sign-in is set up */
  email: string | null;
  emailVerified: boolean;
  /** a Google account is linked («Sign in with Google», docs/GOOGLE_LOGIN.md); absent from old replies */
  googleLinked?: boolean;
  username: string | null;
  name: string | null;
  lang: Lang;
  university: string | null;
  faculty: string | null;
  course: number | null;
  city: string | null;
  role: Role;
  consentAt: string | null;
  isAdult: boolean | null;
  createdAt: string;
  /** scoped admin rights (admin_grants: 'moderator'); null in sign-in replies */
  scopes?: string[] | null;
  /** an admin restricted the account: reads work, writes answer 403 account_restricted */
  blocked?: boolean;
}
/** inboxUnread: unread in-site inbox items (GET /api/me, docs/api/inbox.md); 0 when signed out.
    console: the account has at least one /admin panel (a role, a scoped grant or a staff row) */
export interface AccountState { loaded: boolean; user: AccountUser | null; needsConsent: boolean; inboxUnread: number; console?: boolean }

let token = '';
let state: AccountState = { loaded: false, user: null, needsConsent: false, inboxUnread: 0 };
const SERVER: AccountState = state;
const listeners = new Set<() => void>();
const subscribe = (l: () => void) => { listeners.add(l); return () => { listeners.delete(l); }; };
function set(next: Partial<AccountState>) {
  state = { ...state, ...next };
  listeners.forEach(l => l());
}
export const getAccount = () => state;
/** an admin, or a holder of the scoped right (admin_grants); the server re-checks every request */
export const canScope = (u: AccountUser | null | undefined, scope: string) => !!u && (u.role === 'admin' || !!u.scopes?.includes(scope));
/** the inbox endpoints answer the fresh unread count (GET /api/me/inbox, POST /api/me/inbox/read) */
export function setInboxUnread(n: number) {
  if (!state.user || !Number.isFinite(n) || n < 0 || n === state.inboxUnread) return;
  set({ inboxUnread: n });
}
export function useAccount(): AccountState {
  return useSyncExternalStore(subscribe, () => state, () => SERVER);
}

export class ApiError extends Error {
  /** seconds from the Retry-After header of a 429, 0 otherwise */
  retryAfter = 0;
  /** `detail`: the whole `error` object, for the extra fields some endpoints add (admin content: `field`, `path`) */
  constructor(public status: number, public code: string, message: string, public detail: Record<string, unknown> = {}, public data?: unknown) { super(message); }
}

/** the device knows it has no network: fail at once with a clear reason instead of a long timeout */
const offlineError = (lang?: Lang) =>
  new ApiError(0, 'offline', tr(lang ?? 'kk', 'Интернет жоқ — бұл әрекетке интернет керек.', 'Нет интернета — для этого нужно подключение.'));

/** Mini App sessions slide: a Bearer request may come back with a fresh token in X-Session-Token (docs/ACCOUNTS.md).
    It replaces the in-memory token (never stored); cookie sessions are renewed by the server itself. */
export const SESSION_TOKEN_HEADER = 'x-session-token';
function renew(r: Response) {
  const fresh = r.headers.get(SESSION_TOKEN_HEADER)?.trim();
  if (fresh && token) token = fresh;
}

/** fetch against our API with the session (cookie, or the in-memory Mini App token) and the UI language */
export async function api<T = unknown>(path: string, opts: { method?: string; body?: unknown; lang?: Lang; cache?: RequestCache; signal?: AbortSignal } = {}): Promise<T> {
  const headers: Record<string, string> = { accept: 'application/json' };
  if (opts.body !== undefined) headers['content-type'] = 'application/json';
  if (token) headers.authorization = `Bearer ${token}`;
  if (opts.lang) headers['accept-language'] = apiLang(opts.lang);
  if (!isOnline()) throw offlineError(opts.lang);
  let r: Response;
  try {
    r = await fetch(path, { method: opts.method || 'GET', cache: opts.cache, signal: opts.signal, headers, credentials: 'same-origin', body: opts.body === undefined ? undefined : JSON.stringify(opts.body) });
  } catch {
    throw new ApiError(0, 'network', tr(opts.lang ?? 'kk', 'Серверге қосылу мүмкін болмады. Интернетті тексеріңіз.', 'Нет связи с сервером. Проверьте интернет.'));
  }
  renew(r);
  let j: unknown = null;
  try { j = await r.json(); } catch { /* empty body */ }
  if (!r.ok) {
    const e = (j as { error?: { code?: string; message?: string } & Record<string, unknown> } | null)?.error;
    const err = new ApiError(r.status, e?.code || 'http_' + r.status, e?.message || (tr(opts.lang ?? 'kk', 'Болмады. Кейінірек қайталаңыз.', 'Не получилось. Попробуйте позже.')), e ?? {}, j);
    const ra = Number(r.headers.get('retry-after'));
    if (Number.isFinite(ra) && ra > 0) err.retryAfter = Math.ceil(ra);
    throw err;
  }
  return j as T;
}

/** multipart upload (market photos) with the same session and error handling as api() */
export async function apiUpload<T = unknown>(path: string, form: FormData, lang?: Lang): Promise<T> {
  const headers: Record<string, string> = { accept: 'application/json' };
  if (token) headers.authorization = `Bearer ${token}`;
  if (lang) headers['accept-language'] = apiLang(lang);
  if (!isOnline()) throw offlineError(lang);
  let r: Response;
  try {
    r = await fetch(path, { method: 'POST', headers, credentials: 'same-origin', body: form });
  } catch {
    throw new ApiError(0, 'network', tr(lang ?? 'kk', 'Серверге қосылу мүмкін болмады. Интернетті тексеріңіз.', 'Нет связи с сервером. Проверьте интернет.'));
  }
  renew(r);
  let j: unknown = null;
  try { j = await r.json(); } catch { /* empty body */ }
  if (!r.ok) {
    const e = (j as { error?: { code?: string; message?: string } & Record<string, unknown> } | null)?.error;
    throw new ApiError(r.status, e?.code || 'http_' + r.status, e?.message || (tr(lang ?? 'kk', 'Болмады. Кейінірек қайталаңыз.', 'Не получилось. Попробуйте позже.')), e ?? {}, j);
  }
  return j as T;
}

/** Authenticated binary reads (private quiz media also needs the Mini App Bearer token). */
export async function apiBlob(path: string, lang: Lang): Promise<Blob> {
  const headers: Record<string, string> = { 'accept-language': apiLang(lang) };
  if (token) headers.authorization = `Bearer ${token}`;
  let r: Response;
  try { r = await fetch(path, { headers, credentials: 'same-origin' }); }
  catch { throw new ApiError(0, 'network', tr(lang ?? 'kk', 'Серверге қосылу мүмкін болмады.', 'Нет связи с сервером.')); }
  if (!r.ok) throw new ApiError(r.status, 'http_' + r.status, tr(lang ?? 'kk', 'Медиа жүктелмеді.', 'Не удалось загрузить медиа.'));
  return r.blob();
}

/** a file the API serves only with the session (CSV export): fetched and saved through a blob link */
export async function apiDownload(path: string, filename: string, lang?: Lang): Promise<void> {
  const headers: Record<string, string> = {};
  if (token) headers.authorization = `Bearer ${token}`;
  if (lang) headers['accept-language'] = apiLang(lang);
  if (!isOnline()) throw offlineError(lang);
  const r = await fetch(path, { headers, credentials: 'same-origin' });
  renew(r);
  if (!r.ok) throw new ApiError(r.status, 'http_' + r.status, tr(lang ?? 'kk', 'Файлды жүктеу мүмкін болмады.', 'Не удалось скачать файл.'));
  const url = URL.createObjectURL(await r.blob());
  const a = document.createElement('a');
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}

type MeResp = { user: AccountUser | null; needsConsent: boolean; token?: string; inboxUnread?: number; console?: boolean };
/* sign-in and profile replies carry no inboxUnread: the last known count stays until the next GET /api/me */
/* replies other than GET /api/me carry no scopes / block: keep the last known ones for the same account */
const withRights = (u: AccountUser | null): AccountUser | null =>
  u && u.scopes == null && state.user?.id === u.id ? { ...u, scopes: state.user.scopes, blocked: state.user.blocked } : u;
const apply = (r: MeResp) => set({
  loaded: true, user: withRights(r.user), needsConsent: !!r.user && r.needsConsent,
  inboxUnread: !r.user ? 0 : typeof r.inboxUnread === 'number' ? r.inboxUnread : state.user?.id === r.user.id ? state.inboxUnread : 0,
  console: !r.user ? false : typeof r.console === 'boolean' ? r.console : state.user?.id === r.user.id ? state.console : false,
});

/** GET /api/me once on start; failures mean «not signed in». A definite «nobody» from the server also drops the
    offline ticket copies (lib/tickets.ts); a network failure keeps them. */
let loading: Promise<void> | null = null;
export function loadMe(force = false): Promise<void> {
  if (loading && !force) return loading;
  loading = api<MeResp>('/api/me').then(r => { if (!r.user) clearTickets(); apply(r); }, () => set({ loaded: true, user: null, needsConsent: false, inboxUnread: 0 }));
  return loading;
}

/** quiet re-check of GET /api/me (focus, visibility, the inbox poll): only a real answer changes the state, so a
    network hiccup never looks like a sign-out */
let refreshing: Promise<void> | null = null;
export function refreshMe(): Promise<void> {
  if (refreshing) return refreshing;
  refreshing = api<MeResp>('/api/me').then(r => { if (!r.user) clearTickets(); apply(r); }, () => { /* keep the state */ })
    .finally(() => { refreshing = null; });
  return refreshing;
}

const refParam = () => {
  try { return window.localStorage.getItem('studenthub.ref') || ''; } catch { return ''; }
};

/** Mini App: the initData Telegram signed. The referral the device came with goes along (server may record it). */
export async function signInWebApp(initData: string, lang?: Lang): Promise<AccountState> {
  const r = await api<MeResp>('/api/auth/telegram/webapp', { method: 'POST', body: { initData, ref: refParam() }, lang });
  if (r.token) token = r.token;
  apply(r);
  return state;
}

/** Login Widget: the object Telegram passed to data-onauth. The server removes our extra `ref`
    field before checking Telegram's hash, so it can travel in the same body. */
export async function signInWidget(user: Record<string, unknown>, lang: Lang): Promise<AccountState> {
  const ref = refParam();
  const r = await api<MeResp>('/api/auth/telegram/widget', { method: 'POST', body: ref ? { ...user, ref } : user, lang });
  apply(r);
  return state;
}

/* ---------- email + password (docs/api/email-auth.md) ----------
   Passwords, codes and the address live only in component state for the screen that needs them; nothing here is
   stored. Email sign-in answers with the cookie only (no bearer token). */
export type { Lang };
export interface Sent { ok: true; resendAfter: number }
export type CodePurpose = 'verify_email' | 'reset_password';

/** sign-in endpoints answer {user, needsConsent} and set the cookie; a stale Mini App token must not shadow it */
async function session(path: string, body: unknown, lang: Lang, dropToken = false): Promise<AccountState> {
  const r = await api<MeResp>(path, { method: 'POST', body, lang });
  if (dropToken) token = '';
  apply(r);
  return state;
}

export const emailRegister = (email: string, password: string, lang: Lang) => {
  const ref = refParam();
  return api<Sent>('/api/auth/email/register', { method: 'POST', body: ref ? { email, password, lang, ref } : { email, password, lang }, lang });
};
/** verify needs the password typed at registration (or at the login that found the address unconfirmed) */
export const emailVerify = (email: string, code: string, password: string, lang: Lang) =>
  session('/api/auth/email/verify', { email, code, password }, lang, true);
/** 403 email_not_verified means the password was right and a fresh code went out */
export const emailLogin = (email: string, password: string, lang: Lang) =>
  session('/api/auth/email/login', { email, password }, lang, true);
export const emailResend = (email: string, purpose: CodePurpose, lang: Lang) =>
  api<Sent>('/api/auth/email/resend', { method: 'POST', body: { email, purpose }, lang });
export const resetRequest = (email: string, lang: Lang) =>
  api<Sent>('/api/auth/email/reset/request', { method: 'POST', body: { email }, lang });
/** every other device is signed out; this one gets a new cookie */
export const resetConfirm = (email: string, code: string, newPassword: string, lang: Lang) =>
  session('/api/auth/email/reset/confirm', { email, code, newPassword }, lang, true);
export const changePassword = (currentPassword: string, newPassword: string, lang: Lang) =>
  session('/api/me/password', { currentPassword, newPassword }, lang, true);
/** add the first address (sets a password) or change it (confirms the current password); a code goes to the address */
export const linkEmail = (email: string, pw: { password: string } | { currentPassword: string }, lang: Lang) =>
  api<Sent>('/api/me/email', { method: 'POST', body: { email, ...pw }, lang });
/** replacing an earlier address signs out other devices (the old Mini App token dies too); adding the first does not */
export const confirmEmail = (code: string, lang: Lang) =>
  session('/api/me/email/confirm', { code }, lang, !!state.user?.email);

/* ---------- Google (docs/GOOGLE_LOGIN.md) ----------
   A full-page round trip through the server: GET /api/auth/google/start sends the browser to Google, the callback
   sets the session cookie and comes back to `next` with ?google=<status> (see GOOGLE_STATUS in views/account.tsx). */
/** where the «Google» button leads: back to this page afterwards; link = attach Google to the signed-in account */
export function googleStartUrl(lang: Lang, link = false): string {
  const q = new URLSearchParams();
  q.set('next', typeof window === 'undefined' ? '/' : window.location.pathname + window.location.search);
  q.set('lang', apiLang(lang));
  if (link) q.set('link', '1');
  else { const ref = refParam(); if (ref) q.set('ref', ref); }
  return `/api/auth/google/start?${q}`;
}
/** DELETE /api/me/google: 409 last_method while Google is the only way in */
export async function unlinkGoogle(lang: Lang): Promise<void> {
  apply({ ...(await api<MeResp>('/api/me/google', { method: 'DELETE', lang })), needsConsent: state.needsConsent });
}

export async function signOut(all: boolean, lang: Lang): Promise<void> {
  await api('/api/auth/logout', { method: 'POST', body: { all }, lang });
  token = '';
  clearTickets();
  set({ user: null, needsConsent: false, inboxUnread: 0 });
}

export async function giveConsent(isAdult: boolean | null, lang: Lang): Promise<void> {
  apply(await api<MeResp>('/api/me/consent', { method: 'POST', body: isAdult === null ? {} : { isAdult }, lang }));
}

/** Only these fields ever leave the device; GPA and phone never do (the server refuses them anyway). */
export interface SyncFields { name: string; lang: Lang; university: string; faculty: string; course: number; city: string }
export async function syncProfile(f: SyncFields, lang: Lang): Promise<void> {
  const body = { name: f.name, lang: apiLang(f.lang), university: f.university, faculty: f.faculty, course: f.course, city: f.city };
  apply(await api<MeResp>('/api/me', { method: 'PATCH', body, lang }));
}

/** GET /api/me/export saved as a JSON file */
export async function downloadExport(lang: Lang): Promise<void> {
  const data = await api('/api/me/export', { lang });
  const blob = new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' });
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = 'studenthub-data.json';
  document.body.appendChild(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}

export async function deleteAccount(lang: Lang): Promise<void> {
  await api('/api/me', { method: 'DELETE', body: { confirm: 'DELETE' }, lang });
  token = '';
  clearTickets();
  set({ user: null, needsConsent: false, inboxUnread: 0 });
}
