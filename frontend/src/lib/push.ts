/* Web Push on the client (docs/PUSH.md). The browser asks for permission ONLY from a tap on the settings switch
   «Браузер хабарламалары» — never on load. The subscription goes to POST /api/me/push; public/sw.js shows the
   notification (push) and opens its link (notificationclick). Off unless /api/config says webPush.enabled. */
import { api } from '@/lib/account';

export type PushSupport =
  | 'ok'
  /** iPhone/iPad Safari outside the home screen app: Web Push works only after «Add to Home Screen» (iOS 16.4+) */
  | 'ios-install'
  /** no service worker / PushManager / Notification in this browser (or a dev build without the worker) */
  | 'unsupported';

export function isIOS(): boolean {
  if (typeof navigator === 'undefined') return false;
  const ua = navigator.userAgent || '';
  // iPadOS 13+ reports itself as a Mac with touch
  return /iPad|iPhone|iPod/.test(ua) || (/Macintosh/.test(ua) && (navigator.maxTouchPoints || 0) > 1);
}

export function isStandalone(): boolean {
  if (typeof window === 'undefined') return false;
  const nav = navigator as Navigator & { standalone?: boolean };
  return nav.standalone === true || !!window.matchMedia?.('(display-mode: standalone)').matches;
}

export function pushSupport(): PushSupport {
  if (typeof window === 'undefined') return 'unsupported';
  const has = 'serviceWorker' in navigator && 'PushManager' in window && 'Notification' in window;
  if (isIOS() && !isStandalone()) return 'ios-install';
  return has ? 'ok' : 'unsupported';
}

export function permission(): NotificationPermission | 'unsupported' {
  return typeof Notification === 'undefined' ? 'unsupported' : Notification.permission;
}

/** base64url VAPID public key -> the bytes PushManager.subscribe wants */
export function keyBytes(b64url: string): Uint8Array<ArrayBuffer> {
  const pad = '='.repeat((4 - (b64url.length % 4)) % 4);
  const raw = atob((b64url + pad).replace(/-/g, '+').replace(/_/g, '/'));
  const out = new Uint8Array(new ArrayBuffer(raw.length));
  for (let i = 0; i < raw.length; i++) out[i] = raw.charCodeAt(i);
  return out;
}

/** the worker registration, without waiting forever when there is none (dev build, blocked worker) */
async function registration(): Promise<ServiceWorkerRegistration | null> {
  if (!('serviceWorker' in navigator)) return null;
  const now = await navigator.serviceWorker.getRegistration();
  if (now) return now;
  return Promise.race([navigator.serviceWorker.ready, new Promise<null>(r => setTimeout(() => r(null), 4000))]);
}

/** this browser's current subscription, or null */
export async function currentSubscription(): Promise<PushSubscription | null> {
  if (pushSupport() !== 'ok') return null;
  const reg = await registration();
  return reg ? reg.pushManager.getSubscription() : null;
}

export class PushError extends Error {
  constructor(public code: 'unsupported' | 'denied' | 'no-worker', message = code) { super(message); }
}

/** Ask for permission (call from a tap), subscribe and register the subscription with the server. */
export async function enablePush(publicKey: string, lang: 'kk' | 'ru' | 'en'): Promise<PushSubscription> {
  if (pushSupport() !== 'ok') throw new PushError('unsupported');
  const perm = await Notification.requestPermission();
  if (perm !== 'granted') throw new PushError('denied');
  const reg = await registration();
  if (!reg) throw new PushError('no-worker');
  let sub = await reg.pushManager.getSubscription();
  // a subscription made with another server key (keys rotated) cannot receive our pushes
  const key = keyBytes(publicKey);
  const old = sub?.options.applicationServerKey;
  if (sub && old && !sameBytes(new Uint8Array(old), key)) { await sub.unsubscribe(); sub = null; }
  if (!sub) sub = await reg.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: key });
  await api('/api/me/push', { method: 'POST', body: sub.toJSON(), lang });
  return sub;
}

/** Remove this browser's subscription on the server and in the browser. */
export async function disablePush(lang: 'kk' | 'ru' | 'en'): Promise<void> {
  const sub = await currentSubscription();
  if (!sub) return;
  try { await api('/api/me/push', { method: 'DELETE', body: { endpoint: sub.endpoint }, lang }); }
  finally { await sub.unsubscribe().catch(() => false); }
}

export async function testPush(lang: 'kk' | 'ru' | 'en'): Promise<{ delivered: number }> {
  // api() sends Russian to the server for the English UI; ?lang=en asks for the English test text
  return api(`/api/me/push/test${lang === 'en' ? '?lang=en' : ''}`, { method: 'POST', lang });
}

function sameBytes(a: Uint8Array, b: Uint8Array): boolean {
  if (a.length !== b.length) return false;
  for (let i = 0; i < a.length; i++) if (a[i] !== b[i]) return false;
  return true;
}
