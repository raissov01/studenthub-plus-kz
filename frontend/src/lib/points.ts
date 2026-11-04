/* «Ұпайлар / Баллы» client (docs/api/points.md). Every number comes from the server: the device never
   counts coins or XP itself, and a signed-out student has none. Pure helpers are tested in lib/points.test.ts. */

export interface PointsAward { xp: number; coins: number; duplicate?: boolean; capped?: boolean; reason: string }
export interface PointsTask { id: string; xp: number; coins: number; perDay: number; perWeek?: number; deviceOnly: boolean; done: number; week: number }
export interface PointsEntry { delta: number; reason: string; note?: string; createdAt: string }
export interface PointsSummary {
  xp: number; level: number; levelXp: number; nextLevelXp: number; coins: number;
  streak: { days: number; best: number; freezes: number; maxFreezes: number; freezeCost: number; activeToday: boolean };
  today: { xp: number; coins: number; xpCap: number; coinsCap: number };
  tasks: PointsTask[];
  settings: { leaderboardOptOut: boolean; studyGroup: string | null };
  recent: PointsEntry[];
}
export interface PointsReply { award: PointsAward | null; me: PointsSummary }
export type RewardKind = 'discount' | 'merch' | 'print_quota' | 'priority_event';
export interface Reward {
  id: number; kind: RewardKind; titleKk: string; titleRu: string; descKk: string; descRu: string; cost: number;
  stock: number | null; perUserLimit: number; discountId: string | null; active: boolean; codesLeft?: number; mine?: number;
}
export interface Redemption {
  id: number; rewardId: number; kind: RewardKind; titleKk: string; titleRu: string; cost: number;
  status: 'pending' | 'approved' | 'rejected'; code: string | null; note: string | null; createdAt: string; decidedAt: string | null;
  userId?: number; userName?: string | null; userBalance?: number;
}
export interface Leader { rank: number; name: string; xp: number; me: boolean }
export interface Leaderboard {
  scope: 'group' | 'faculty'; status: 'ok' | 'too_small' | 'no_scope'; members?: number; minMembers: number;
  leaders?: Leader[]; optedOut: boolean; weekStart: string; resetsAt: string;
}

type TT = (kk: string, ru: string) => string;

/** «+10 XP · +2 монета» for a toast, or '' when nothing was earned */
export function awardText(a: PointsAward | null | undefined, tt: TT): string {
  if (!a || (a.xp <= 0 && a.coins <= 0)) return '';
  const parts = [];
  if (a.xp > 0) parts.push(`+${a.xp} XP`);
  if (a.coins > 0) parts.push(tt(`+${a.coins} монета`, `+${a.coins} ${plural(a.coins, 'монета', 'монеты', 'монет')}`));
  return parts.join(' · ');
}

export function plural(n: number, one: string, few: string, many: string): string {
  const m10 = n % 10, m100 = n % 100;
  if (m10 === 1 && m100 !== 11) return one;
  if (m10 >= 2 && m10 <= 4 && (m100 < 10 || m100 >= 20)) return few;
  return many;
}

/** progress inside the current level, 0..100 */
export function levelPct(s: Pick<PointsSummary, 'xp' | 'levelXp' | 'nextLevelXp'>): number {
  const span = s.nextLevelXp - s.levelXp;
  if (span <= 0) return 100;
  return Math.max(0, Math.min(100, Math.round(((s.xp - s.levelXp) / span) * 100)));
}

/** a fresh idempotency key: one per user intent, reused when the same request is retried */
export function newKey(): string {
  const b = new Uint8Array(12);
  crypto.getRandomValues(b);
  return Array.from(b, x => x.toString(16).padStart(2, '0')).join('');
}

/** the screens fire this after anything that may change the balance, so the home chip refreshes */
export const POINTS_CHANGED = 'studenthub:points';
export const pointsChanged = () => { if (typeof window !== 'undefined') window.dispatchEvent(new Event(POINTS_CHANGED)); };

/** Almaty date of now, the key of the once-a-day «first open» request */
export function almatyToday(now = new Date()): string {
  return new Date(now.getTime() + 5 * 3600_000).toISOString().slice(0, 10);
}
