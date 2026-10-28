// «Даму жоспары» state and rules. Everything stays on the device (inside the app's
// localStorage state). Weeks are ISO weeks in Asia/Almaty time.
//
// Forgiving streak: a week counts when at least one step was done. One missed week between
// active weeks does not break the streak; paused weeks (exam session) are skipped entirely.

import { ITEM_BY_KEY } from "./items";

export type RoadmapState = {
  chosen: string[];                      // 3–5 item keys the student picked
  done: Record<string, number[]>;        // item key -> indexes of finished steps
  activeWeeks: string[];                 // ISO week keys with at least one finished step
  pausedWeeks: string[];                 // weeks covered by "емтихан кезеңі — кідірту"
  pausedUntil?: string;                  // YYYY-MM-DD, inclusive
};

export const MIN_CHOSEN = 3;
export const MAX_CHOSEN = 5;
export const MAX_WEEKLY_STEPS = 3;

export const emptyRoadmap = (): RoadmapState => ({ chosen: [], done: {}, activeWeeks: [], pausedWeeks: [] });

const ALMATY_OFFSET_MS = 5 * 3600 * 1000;

/** Almaty calendar date (YYYY-MM-DD) of an instant. */
export function almatyDate(now: Date): string {
  return new Date(now.getTime() + ALMATY_OFFSET_MS).toISOString().slice(0, 10);
}

/** ISO week key like "2026-W44" for an Almaty date. */
export function weekKey(now: Date): string {
  const d = new Date(almatyDate(now) + "T00:00:00Z");
  const day = (d.getUTCDay() + 6) % 7; // Monday = 0
  d.setUTCDate(d.getUTCDate() - day + 3); // Thursday of this week
  const year = d.getUTCFullYear();
  const firstThu = new Date(Date.UTC(year, 0, 4));
  const week = 1 + Math.round(((d.getTime() - firstThu.getTime()) / 86400000 - 3 + ((firstThu.getUTCDay() + 6) % 7)) / 7);
  return `${year}-W${String(week).padStart(2, "0")}`;
}

function addDays(now: Date, days: number): Date {
  return new Date(now.getTime() + days * 86400000);
}

/** Toggle an item in the chosen list (max 5). Returns the state unchanged when full. */
export function toggleChosen(s: RoadmapState, key: string): RoadmapState {
  if (!ITEM_BY_KEY[key]) return s;
  if (s.chosen.includes(key)) return { ...s, chosen: s.chosen.filter((k) => k !== key) };
  if (s.chosen.length >= MAX_CHOSEN) return s;
  return { ...s, chosen: [...s.chosen, key] };
}

export const isPaused = (s: RoadmapState, now: Date) => !!s.pausedUntil && almatyDate(now) <= s.pausedUntil;

export type Step = { item: string; index: number };

/** This week's 1–3 next steps: the first unfinished step of each chosen item, in order. */
export function weeklySteps(s: RoadmapState, now: Date): Step[] {
  if (isPaused(s, now)) return [];
  const out: Step[] = [];
  for (const key of s.chosen) {
    const item = ITEM_BY_KEY[key];
    if (!item) continue;
    const done = s.done[key] ?? [];
    const index = item.steps.findIndex((_, i) => !done.includes(i));
    if (index >= 0) out.push({ item: key, index });
    if (out.length === MAX_WEEKLY_STEPS) break;
  }
  return out;
}

export function completeStep(s: RoadmapState, item: string, index: number, now: Date): RoadmapState {
  const steps = ITEM_BY_KEY[item]?.steps;
  if (!steps || index < 0 || index >= steps.length) return s;
  const done = Array.from(new Set([...(s.done[item] ?? []), index])).sort((a, b) => a - b);
  const wk = weekKey(now);
  return {
    ...s,
    done: { ...s.done, [item]: done },
    activeWeeks: s.activeWeeks.includes(wk) ? s.activeWeeks : [...s.activeWeeks, wk],
  };
}

export function undoStep(s: RoadmapState, item: string, index: number): RoadmapState {
  return { ...s, done: { ...s.done, [item]: (s.done[item] ?? []).filter((i) => i !== index) } };
}

/** «Емтихан кезеңі — кідірту»: pause for `weeks` weeks starting now. */
export function pause(s: RoadmapState, now: Date, weeks = 2): RoadmapState {
  const until = almatyDate(addDays(now, weeks * 7 - 1));
  const paused = new Set(s.pausedWeeks);
  for (let i = 0; i < weeks; i++) paused.add(weekKey(addDays(now, i * 7)));
  return { ...s, pausedUntil: until, pausedWeeks: [...paused] };
}

export function resume(s: RoadmapState): RoadmapState {
  return { ...s, pausedUntil: undefined };
}

export const isItemDone = (s: RoadmapState, key: string) => {
  const item = ITEM_BY_KEY[key];
  return !!item && (s.done[key]?.length ?? 0) >= item.steps.length;
};

/** Progress over the chosen items: finished steps / all steps. */
export function progress(s: RoadmapState): { done: number; total: number; itemsDone: number } {
  let done = 0, total = 0, itemsDone = 0;
  for (const key of s.chosen) {
    const item = ITEM_BY_KEY[key];
    if (!item) continue;
    total += item.steps.length;
    done += Math.min(s.done[key]?.length ?? 0, item.steps.length);
    if (isItemDone(s, key)) itemsDone++;
  }
  return { done, total, itemsDone };
}

/**
 * Weeks in a row with activity, counted back from this week. The current week may still be
 * empty (the student has time). Paused weeks are skipped; a single empty week is forgiven.
 */
export function streak(s: RoadmapState, now: Date): number {
  const active = new Set(s.activeWeeks), paused = new Set(s.pausedWeeks);
  let count = 0, misses = 0;
  for (let i = 0; i < 520; i++) {
    const wk = weekKey(addDays(now, -7 * i));
    if (active.has(wk)) { count++; misses = 0; continue; }
    if (paused.has(wk) || i === 0) continue;
    misses++;
    if (misses > 1) break;
  }
  return count;
}

/** Warm, guilt-free status line for the home board. */
export function statusLine(s: RoadmapState, now: Date, lang: "kk" | "ru"): string {
  if (isPaused(s, now)) return lang === "ru" ? "Пауза на время сессии. Удачи на экзаменах!" : "Емтихан кезеңіне кідіртілді. Емтихандарың сәтті өтсін!";
  const n = streak(s, now);
  const thisWeek = s.activeWeeks.includes(weekKey(now));
  if (n === 0) return lang === "ru" ? "Начните с одного небольшого шага на этой неделе." : "Осы аптада бір шағын қадамнан баста.";
  if (thisWeek) return lang === "ru" ? `${n} нед. подряд с движением вперёд. Отлично!` : `${n} апта қатарынан алға жылжып келесің. Керемет!`;
  return lang === "ru" ? `${n} нед. подряд. Один шаг на этой неделе — и серия продолжится.` : `${n} апта қатарынан. Осы аптада бір қадам жасасаң, жалғаса береді.`;
}
