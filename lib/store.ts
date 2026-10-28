/* Device-local state. One store, persisted to localStorage "studenthub.v1" in exactly the legacy shape so existing
   users keep their data; a one-time move from the old "studygo.v1" key runs before the first read.
   A second, in-memory store keeps transient UI state (filters, search text, chat draft) for the session. */
import { useSyncExternalStore } from 'react';
import type { AppState } from '@/lib/types';
import { defaults, UI_INIT, type UiState } from '@/lib/defaults';

export { defaults, type UiState };

export const KEY = 'studenthub.v1';
export const OLD_KEYS = ['studygo.v1'];

/** one-time move from the app's old storage name; the old copy is removed only after the new one is written */
export function migrateStorage() {
  try {
    const ls = window.localStorage;
    if (ls.getItem(KEY) != null) return;
    for (const k of OLD_KEYS) {
      const v = ls.getItem(k);
      if (v == null) continue;
      ls.setItem(KEY, v);
      if (ls.getItem(KEY) === v) ls.removeItem(k);
      return;
    }
  } catch { /* storage unavailable */ }
}

function lsGet(): string | null {
  try { return window.localStorage.getItem(KEY); } catch { return null; }
}
function lsSet(v: string) {
  try { window.localStorage.setItem(KEY, v); } catch { /* storage unavailable */ }
}

function load(): AppState {
  migrateStorage();
  const d = defaults();
  let s: Partial<AppState> & { ai?: unknown } | null = null;
  try { const raw = lsGet(); if (raw) s = JSON.parse(raw); } catch { s = null; }
  if (!s || typeof s !== 'object') return d;
  const merged = Object.assign(d, s, { profile: Object.assign(d.profile, s.profile || {}) }) as AppState & { ai?: unknown };
  delete merged.ai; // old client-side API key setting, no longer used
  return merged;
}

/* ---------- persisted store ---------- */
let state: AppState | null = null;
const SERVER_STATE = defaults('kk');
const listeners = new Set<() => void>();
const emit = () => listeners.forEach(l => l());
const subscribe = (l: () => void) => { listeners.add(l); return () => { listeners.delete(l); }; };

export function getState(): AppState {
  if (state == null) state = load();
  return state;
}

/** Apply a change. The mutator gets a deep copy, so React sees a new object and nothing shared is mutated. */
export function update(mutate: (s: AppState) => void) {
  const next = structuredClone(getState());
  mutate(next);
  state = next;
  lsSet(JSON.stringify(next));
  emit();
}

/** wipe everything (settings → delete all data) */
export function resetState() {
  try { window.localStorage.removeItem(KEY); } catch { /* ignore */ }
  state = defaults();
  state.onboarded = false;
  lsSet(JSON.stringify(state));
  emit();
}

if (typeof window !== 'undefined') {
  // another tab changed the data
  window.addEventListener('storage', e => { if (e.key === KEY) { state = load(); emit(); } });
}

export function useAppState(): AppState {
  return useSyncExternalStore(subscribe, getState, () => SERVER_STATE);
}

/* ---------- transient UI state (lives for the tab session, like the legacy `ui` object) ---------- */
let ui: UiState = UI_INIT;
const uiListeners = new Set<() => void>();
const subscribeUi = (l: () => void) => { uiListeners.add(l); return () => { uiListeners.delete(l); }; };
export const getUi = () => ui;
export function setUi(patch: Partial<UiState> | ((u: UiState) => Partial<UiState>)) {
  const p = typeof patch === 'function' ? patch(ui) : patch;
  ui = { ...ui, ...p };
  uiListeners.forEach(l => l());
}
export function useUi(): UiState {
  return useSyncExternalStore(subscribeUi, getUi, () => UI_INIT);
}

/* ---------- "has the client taken over" flag ---------- */
const noop = () => () => {};
export function useHydrated(): boolean {
  return useSyncExternalStore(noop, () => true, () => false);
}
