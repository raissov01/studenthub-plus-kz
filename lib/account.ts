/* Optional Telegram account (docs/ACCOUNTS.md). The device profile works without it; the account only exists for
   actions that need the shared database. Session: an HttpOnly cookie set by the API; inside a Telegram Mini App the
   cookie may be blocked, so the token returned by /api/auth/telegram/webapp is kept in memory only and sent as
   «Authorization: Bearer». */
import { useSyncExternalStore } from 'react';
import { isOnline } from '@/lib/pwa';
import { clearTickets } from '@/lib/tickets';

export type Role = 'student' | 'ambassador' | 'admin' | 'university_staff' | 'employer';
export interface AccountUser {
  id: number;
  telegramId: number;
  username: string | null;
  name: string | null;
  lang: 'kk' | 'ru';
  university: string | null;
  faculty: string | null;
  course: number | null;
  city: string | null;
  role: Role;
  consentAt: string | null;
  isAdult: boolean | null;
  createdAt: string;
}
export interface AccountState { loaded: boolean; user: AccountUser | null; needsConsent: boolean }

let token = '';
let state: AccountState = { loaded: false, user: null, needsConsent: false };
const SERVER: AccountState = state;
const listeners = new Set<() => void>();
const subscribe = (l: () => void) => { listeners.add(l); return () => { listeners.delete(l); }; };
function set(next: Partial<AccountState>) {
  state = { ...state, ...next };
  listeners.forEach(l => l());
}
export const getAccount = () => state;
export function useAccount(): AccountState {
  return useSyncExternalStore(subscribe, () => state, () => SERVER);
}

export class ApiError extends Error {
  constructor(public status: number, public code: string, message: string) { super(message); }
}

/** the device knows it has no network: fail at once with a clear reason instead of a long timeout */
const offlineError = (lang?: 'kk' | 'ru') =>
  new ApiError(0, 'offline', lang === 'ru' ? 'Нет интернета — для этого нужно подключение.' : 'Интернет жоқ — бұл әрекетке интернет керек.');

/** fetch against our API with the session (cookie, or the in-memory Mini App token) and the UI language */
export async function api<T = unknown>(path: string, opts: { method?: string; body?: unknown; lang?: 'kk' | 'ru' } = {}): Promise<T> {
  const headers: Record<string, string> = { accept: 'application/json' };
  if (opts.body !== undefined) headers['content-type'] = 'application/json';
  if (token) headers.authorization = `Bearer ${token}`;
  if (opts.lang) headers['accept-language'] = opts.lang;
  if (!isOnline()) throw offlineError(opts.lang);
  let r: Response;
  try {
    r = await fetch(path, { method: opts.method || 'GET', headers, credentials: 'same-origin', body: opts.body === undefined ? undefined : JSON.stringify(opts.body) });
  } catch {
    throw new ApiError(0, 'network', opts.lang === 'ru' ? 'Нет связи с сервером. Проверьте интернет.' : 'Серверге қосылу мүмкін болмады. Интернетті тексеріңіз.');
  }
  let j: unknown = null;
  try { j = await r.json(); } catch { /* empty body */ }
  if (!r.ok) {
    const e = (j as { error?: { code?: string; message?: string } } | null)?.error;
    throw new ApiError(r.status, e?.code || 'http_' + r.status, e?.message || (opts.lang === 'ru' ? 'Не получилось. Попробуйте позже.' : 'Болмады. Кейінірек қайталаңыз.'));
  }
  return j as T;
}

/** multipart upload (market photos) with the same session and error handling as api() */
export async function apiUpload<T = unknown>(path: string, form: FormData, lang?: 'kk' | 'ru'): Promise<T> {
  const headers: Record<string, string> = { accept: 'application/json' };
  if (token) headers.authorization = `Bearer ${token}`;
  if (lang) headers['accept-language'] = lang;
  if (!isOnline()) throw offlineError(lang);
  let r: Response;
  try {
    r = await fetch(path, { method: 'POST', headers, credentials: 'same-origin', body: form });
  } catch {
    throw new ApiError(0, 'network', lang === 'ru' ? 'Нет связи с сервером. Проверьте интернет.' : 'Серверге қосылу мүмкін болмады. Интернетті тексеріңіз.');
  }
  let j: unknown = null;
  try { j = await r.json(); } catch { /* empty body */ }
  if (!r.ok) {
    const e = (j as { error?: { code?: string; message?: string } } | null)?.error;
    throw new ApiError(r.status, e?.code || 'http_' + r.status, e?.message || (lang === 'ru' ? 'Не получилось. Попробуйте позже.' : 'Болмады. Кейінірек қайталаңыз.'));
  }
  return j as T;
}

/** a file the API serves only with the session (CSV export): fetched and saved through a blob link */
export async function apiDownload(path: string, filename: string, lang?: 'kk' | 'ru'): Promise<void> {
  const headers: Record<string, string> = {};
  if (token) headers.authorization = `Bearer ${token}`;
  if (lang) headers['accept-language'] = lang;
  if (!isOnline()) throw offlineError(lang);
  const r = await fetch(path, { headers, credentials: 'same-origin' });
  if (!r.ok) throw new ApiError(r.status, 'http_' + r.status, lang === 'ru' ? 'Не удалось скачать файл.' : 'Файлды жүктеу мүмкін болмады.');
  const url = URL.createObjectURL(await r.blob());
  const a = document.createElement('a');
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}

type MeResp = { user: AccountUser | null; needsConsent: boolean; token?: string };
const apply = (r: MeResp) => set({ loaded: true, user: r.user, needsConsent: !!r.user && r.needsConsent });

/** GET /api/me once on start; failures mean «not signed in». A definite «nobody» from the server also drops the
    offline ticket copies (lib/tickets.ts); a network failure keeps them. */
let loading: Promise<void> | null = null;
export function loadMe(force = false): Promise<void> {
  if (loading && !force) return loading;
  loading = api<MeResp>('/api/me').then(r => { if (!r.user) clearTickets(); apply(r); }, () => set({ loaded: true, user: null, needsConsent: false }));
  return loading;
}

const refParam = () => {
  try { return window.localStorage.getItem('studenthub.ref') || ''; } catch { return ''; }
};

/** Mini App: the initData Telegram signed. The referral the device came with goes along (server may record it). */
export async function signInWebApp(initData: string): Promise<AccountState> {
  const r = await api<MeResp>('/api/auth/telegram/webapp', { method: 'POST', body: { initData, ref: refParam() } });
  if (r.token) token = r.token;
  apply(r);
  return state;
}

/** Login Widget: the object Telegram passed to data-onauth. The server removes our extra `ref`
    field before checking Telegram's hash, so it can travel in the same body. */
export async function signInWidget(user: Record<string, unknown>, lang: 'kk' | 'ru'): Promise<AccountState> {
  const ref = refParam();
  const r = await api<MeResp>('/api/auth/telegram/widget', { method: 'POST', body: ref ? { ...user, ref } : user, lang });
  apply(r);
  return state;
}

export async function signOut(all: boolean, lang: 'kk' | 'ru'): Promise<void> {
  await api('/api/auth/logout', { method: 'POST', body: { all }, lang });
  token = '';
  clearTickets();
  set({ user: null, needsConsent: false });
}

export async function giveConsent(isAdult: boolean | null, lang: 'kk' | 'ru'): Promise<void> {
  apply(await api<MeResp>('/api/me/consent', { method: 'POST', body: isAdult === null ? {} : { isAdult }, lang }));
}

/** Only these fields ever leave the device; GPA and phone never do (the server refuses them anyway). */
export interface SyncFields { name: string; lang: 'kk' | 'ru'; university: string; faculty: string; course: number; city: string }
export async function syncProfile(f: SyncFields, lang: 'kk' | 'ru'): Promise<void> {
  const body: SyncFields = { name: f.name, lang: f.lang, university: f.university, faculty: f.faculty, course: f.course, city: f.city };
  apply(await api<MeResp>('/api/me', { method: 'PATCH', body, lang }));
}

/** GET /api/me/export saved as a JSON file */
export async function downloadExport(lang: 'kk' | 'ru'): Promise<void> {
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

export async function deleteAccount(lang: 'kk' | 'ru'): Promise<void> {
  await api('/api/me', { method: 'DELETE', body: { confirm: 'DELETE' }, lang });
  token = '';
  clearTickets();
  set({ user: null, needsConsent: false });
}
