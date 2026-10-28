/* Runtime switches. These are the defaults; the client loads GET /api/config once on start and replaces them
   (loadConfig). Screens read them through useConfig() so they re-render when the server's values arrive. */
import { useSyncExternalStore } from 'react';

export interface Config {
  /** where «Қате не алаяқтық туралы хабарлау» leads; a t.me link gets the item prefilled as ?text= */
  reportUrl: string;
  /** Telegram bot username without @, empty until the bot is public */
  botUsername: string;
  /** form for suggesting a new opportunity, empty until it exists */
  suggestFormUrl: string;
  features: {
    events: boolean;
    ambassador: boolean;
    mentors: boolean;
    market: boolean;
    portfolio: boolean;
    b2b: boolean;
  };
}

export const CONFIG: Config = {
  reportUrl: 'https://t.me/raissov01',
  botUsername: '',
  suggestFormUrl: '',
  features: { events: false, ambassador: false, mentors: false, market: false, portfolio: false, b2b: false },
};

export type FeatureName = keyof Config['features'];

/* ---------- runtime copy, filled from GET /api/config ---------- */
let loaded = false;
const listeners = new Set<() => void>();
const subscribe = (l: () => void) => { listeners.add(l); return () => { listeners.delete(l); }; };
let snapshot: Config = CONFIG;

const str = (v: unknown, fb: string) => (typeof v === 'string' ? v : fb);

/** Fetch /api/config once per page load; on any failure the defaults above stay. */
export async function loadConfig(): Promise<void> {
  if (loaded || typeof window === 'undefined') return;
  loaded = true;
  try {
    const r = await fetch('/api/config', { headers: { accept: 'application/json' } });
    if (!r.ok) return;
    const j = await r.json() as Partial<Config> & { features?: Record<string, unknown> };
    const features = { ...CONFIG.features };
    for (const k of Object.keys(features) as FeatureName[]) if (typeof j.features?.[k] === 'boolean') features[k] = j.features[k] as boolean;
    // CONFIG is mutated in place so plain helpers (reportHref) see the server's values too
    Object.assign(CONFIG, {
      reportUrl: str(j.reportUrl, '') || CONFIG.reportUrl,
      botUsername: str(j.botUsername, CONFIG.botUsername).replace(/^@/, ''),
      suggestFormUrl: str(j.suggestFormUrl, CONFIG.suggestFormUrl),
      features,
    });
    snapshot = { ...CONFIG, features: { ...features } };
    listeners.forEach(l => l());
  } catch { /* offline or no API: defaults */ }
}

const DEFAULT_SNAPSHOT: Config = { ...CONFIG, features: { ...CONFIG.features } };
export function useConfig(): Config {
  return useSyncExternalStore(subscribe, () => snapshot, () => DEFAULT_SNAPSHOT);
}
export const featureOn = (c: Config, f: FeatureName) => !!c.features[f];

/** Report link with the item prefilled where the target supports it. */
export function reportHref(itemId: string, kind: string, pageUrl: string, lang: 'kk' | 'ru'): string {
  const text = lang === 'ru'
    ? `Ошибка или мошенничество: ${kind} ${itemId}\n${pageUrl}\n`
    : `Қате не алаяқтық: ${kind} ${itemId}\n${pageUrl}\n`;
  try {
    const u = new URL(CONFIG.reportUrl);
    if (u.hostname === 't.me') u.searchParams.set('text', text);
    else u.searchParams.set('id', itemId);
    return u.toString();
  } catch {
    return CONFIG.reportUrl;
  }
}
