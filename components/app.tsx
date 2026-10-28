'use client';
/* App-wide client context: content from the server, device state, navigation (legacy go/back semantics on top of
   the Next router), bottom sheets with history integration, and toasts. */
import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { usePathname, useRouter } from 'next/navigation';
import { i18n, type I18n } from '@/lib/format';
import { openOpps } from '@/lib/domain';
import { useAccount } from '@/lib/account';
import { jobsForViewer } from '@/lib/b2b';
import { parseView, pathOf, ROOTS } from '@/lib/routes';
import { setUi, update, useAppState, useUi, type UiState } from '@/lib/store';
import type { AppState, Content } from '@/lib/types';

interface GoOpts { replace?: boolean }
interface Ctx {
  content: Content;
  refreshContent: () => Promise<Content>;
  go: (v: string, opts?: GoOpts) => void;
  back: () => void;
  openSheet: (node: ReactNode) => void;
  closeSheet: () => void;
  /** remove the sheet without touching history (used right before a navigation that replaces its entry) */
  closeSheetDom: () => void;
  toast: (msg: string) => void;
  sheet: ReactNode;
  toastMsg: { text: string; key: number } | null;
  /** the tab that should look active on the current screen; set by the shell */
  setActiveTab: (t: string) => void;
  fromOf: (path: string) => string;
}
const AppCtx = createContext<Ctx | null>(null);

/* Which tab a screen was opened from, per path (survives refresh like the legacy history.state did). */
const FROM_KEY = 'studenthub.from';
function readFrom(): Record<string, string> {
  try { return JSON.parse(window.sessionStorage.getItem(FROM_KEY) || '{}'); } catch { return {}; }
}
function writeFrom(m: Record<string, string>) {
  try { window.sessionStorage.setItem(FROM_KEY, JSON.stringify(m)); } catch { /* ignore */ }
}

export function AppProvider({ content: raw, children }: { content: Content; children: ReactNode }) {
  const router = useRouter();
  const pathname = usePathname();
  const pathRef = useRef(pathname);
  useEffect(() => { pathRef.current = pathname; }, [pathname]);

  const [fresh, setFresh] = useState<{ base: Content; data: Content } | null>(null);
  const feed = fresh?.base === raw ? fresh.data : raw;
  const refreshContent = useCallback(async () => {
    const r = await fetch('/api/content', { cache: 'no-store', signal: AbortSignal.timeout(12_000) });
    if (!r.ok) throw new Error('Content unavailable');
    const data = await r.json() as Content;
    if (!Array.isArray(data.jobs) || !Array.isArray(data.opportunities) || !Array.isArray(data.discounts)) throw new Error('Content unavailable');
    setFresh({ base: raw, data });
    return data;
  }, [raw]);

  // past deadlines never show, whatever the source said; adult-only employer jobs never reach a signed-in minor
  const acc = useAccount();
  const signedIn = !!acc.user, isAdult = acc.user?.isAdult;
  const content = useMemo(() => ({ ...feed, opportunities: openOpps(feed.opportunities), jobs: jobsForViewer(feed.jobs, signedIn, isAdult) }), [feed, signedIn, isAdult]);

  const [sheet, setSheet] = useState<ReactNode>(null);
  const sheetOpen = useRef(false);
  const sheetVersion = useRef(0);
  const [toastMsg, setToastMsg] = useState<{ text: string; key: number } | null>(null);
  const toastTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const navStarted = useRef(false);
  const activeTab = useRef('');
  const fromMap = useRef<Record<string, string> | null>(null);
  const froms = () => (fromMap.current ??= readFrom());

  const closeSheetDom = useCallback(() => { sheetOpen.current = false; setSheet(null); }, []);

  const go = useCallback((v: string, opts: GoOpts = {}) => {
    const [n, prm] = parseView(v);
    // screens whose legacy parameter only selected a filter
    if (n === 'search' && prm) setUi({ sk: prm });
    if (n === 'opps' && prm) setUi({ oppTab: prm });
    if (n === 'match' && prm) setUi({ matchId: prm });
    let replace = !!opts.replace;
    if (sheetOpen.current) { closeSheetDom(); replace = true; }
    if (!replace) navStarted.current = true;
    const path = pathOf(v);
    // shared screens (a job, a match check, AI tools) stay under the tab the student came from
    const m = froms();
    m[path] = replace ? (m[pathRef.current] ?? '') : activeTab.current;
    writeFrom(m);
    if (replace) router.replace(path); else router.push(path);
  }, [router, closeSheetDom]);

  const openSheet = useCallback((node: ReactNode) => {
    sheetVersion.current++;
    setSheet(node);
    if (!sheetOpen.current) {
      try { window.history.pushState({ sheet: true }, ''); } catch { /* ignore */ }
    }
    sheetOpen.current = true;
  }, []);

  const closeSheet = useCallback(() => {
    if (!sheetOpen.current) return;
    const closingVersion = sheetVersion.current;
    try { window.history.back(); } catch { closeSheetDom(); return; }
    // some Android WebViews ignore history.back(); never leave a sheet stuck open
    setTimeout(() => { if (sheetOpen.current && sheetVersion.current === closingVersion) closeSheetDom(); }, 350);
  }, [closeSheetDom]);

  useEffect(() => {
    const onPop = () => { if (sheetOpen.current) closeSheetDom(); };
    window.addEventListener('popstate', onPop);
    return () => window.removeEventListener('popstate', onPop);
  }, [closeSheetDom]);

  const back = useCallback(() => {
    if (sheetOpen.current) { closeSheet(); return; }
    if (window.history.length <= 1 || !navStarted.current) { go('home', { replace: true }); return; }
    const before = pathRef.current;
    router.back();
    setTimeout(() => {
      if (pathRef.current === before && !ROOTS.includes(parseView(before)[0])) go('home', { replace: true });
    }, 350);
  }, [closeSheet, go, router]);

  const toast = useCallback((text: string) => {
    setToastMsg({ text, key: Date.now() });
    clearTimeout(toastTimer.current);
    toastTimer.current = setTimeout(() => setToastMsg(null), 2400);
  }, []);

  const setActiveTab = useCallback((t: string) => { activeTab.current = t; }, []);
  const fromOf = useCallback((path: string) => froms()[path] ?? '', []);

  const value = useMemo<Ctx>(() => ({ content, refreshContent, go, back, openSheet, closeSheet, closeSheetDom, toast, sheet, toastMsg, setActiveTab, fromOf }),
    [content, refreshContent, go, back, openSheet, closeSheet, closeSheetDom, toast, sheet, toastMsg, setActiveTab, fromOf]);
  return <AppCtx.Provider value={value}>{children}</AppCtx.Provider>;
}

export interface App extends Ctx, I18n {
  S: AppState;
  ui: UiState;
  I: I18n;
  update: typeof update;
  setUi: typeof setUi;
}

/** everything a screen needs: state, UI state, i18n bound to the current language, navigation, sheets, toasts */
export function useApp(): App {
  const ctx = useContext(AppCtx);
  if (!ctx) throw new Error('useApp outside AppProvider');
  const S = useAppState();
  const transient = useUi();
  const ui = { ...transient, jf: { ...transient.jf, noexp: S.jobNoExperience ?? [1, 2].includes(S.profile.course) } };
  const I = useMemo(() => i18n(S.lang), [S.lang]);
  return { ...ctx, ...I, I, S, ui, update, setUi };
}

/** copy to clipboard with the legacy fallbacks and toasts */
export function copyText(text: string, toast: (m: string) => void, tt: I18n['tt']) {
  const done = () => toast(tt('Көшірілді', 'Скопировано'));
  const fallback = () => {
    const ta = document.createElement('textarea');
    ta.value = text; ta.style.position = 'fixed'; ta.style.opacity = '0';
    document.body.appendChild(ta); ta.select();
    let ok = false;
    try { ok = document.execCommand('copy'); } catch { ok = false; }
    ta.remove();
    toast(ok ? tt('Көшірілді', 'Скопировано') : tt('Мәтінді белгілеп, қолмен көшіріңіз', 'Выделите текст и скопируйте вручную'));
  };
  try {
    if (navigator.clipboard && navigator.clipboard.writeText) navigator.clipboard.writeText(text).then(done, fallback);
    else fallback();
  } catch { fallback(); }
}
