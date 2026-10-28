/* The Telegram Mini App instance for React: null outside Telegram (then nothing changes). The glue itself lives in
   features/telegram/webapp.ts. */
import { useSyncExternalStore } from 'react';
import { applyTelegramTheme, firstName, telegram } from '@/features/telegram/webapp';

export type TgApp = NonNullable<Awaited<ReturnType<typeof telegram>>>;
interface TgState { app: TgApp | null; name: string }

let state: TgState = { app: null, name: '' };
const SERVER: TgState = state;
const listeners = new Set<() => void>();
const subscribe = (l: () => void) => { listeners.add(l); return () => { listeners.delete(l); }; };
export const getTelegram = () => state;
export function useTelegram(): TgState {
  return useSyncExternalStore(subscribe, () => state, () => SERVER);
}

/** Resolves the Mini App when opened from Telegram: ready/expand, theme colours, safe top inset. */
export async function initTelegram(): Promise<TgApp | null> {
  const app = await telegram();
  if (!app) return null;
  try { window.sessionStorage.setItem('studenthub.inTelegram', '1'); } catch { /* ignore */ }
  try { app.ready(); app.expand(); } catch { /* old clients */ }
  applyTelegramTheme(app);
  const again = () => applyTelegramTheme(app);
  for (const e of ['themeChanged', 'safeAreaChanged', 'contentSafeAreaChanged']) {
    try { app.onEvent(e, again); } catch { /* unsupported event */ }
  }
  state = { app, name: firstName(app) };
  listeners.forEach(l => l());
  return app;
}

/** Open a t.me link: inside Telegram through the client (stays in the app), otherwise a new tab. */
export function openTelegramLink(url: string) {
  const app = state.app;
  if (app) { app.openTelegramLink(url); return; }
  window.open(url, '_blank', 'noopener');
}

/** Asks Telegram to let the bot write to the student (needed for reminders). Mini App only, once per device. */
export function requestWriteAccess() {
  const app = state.app as (TgApp & { requestWriteAccess?: (cb?: (ok: boolean) => void) => void }) | null;
  if (!app?.requestWriteAccess) return;
  try {
    if (window.localStorage.getItem('studenthub.tgWrite')) return;
    window.localStorage.setItem('studenthub.tgWrite', '1');
  } catch { /* ignore */ }
  try { app.requestWriteAccess(); } catch { /* unsupported */ }
}
