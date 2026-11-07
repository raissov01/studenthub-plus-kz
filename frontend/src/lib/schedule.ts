/* Pure helpers of the admin calendar (/admin/schedule): wall-clock weeks in Astana time and the side-by-side lanes
   of overlapping events. Kept free of React for the unit test. */
export const pad = (n: number) => String(n).padStart(2, '0');
/** a wall-clock Date (local fields = Astana time) -> 'YYYY-MM-DD' */
export const wallYmd = (d: Date) => `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
export const addDays = (d: Date, n: number) => new Date(d.getFullYear(), d.getMonth(), d.getDate() + n);
/** Monday 00:00 of the week holding wall date d */
export const mondayOf = (d: Date) => addDays(new Date(d.getFullYear(), d.getMonth(), d.getDate()), -((d.getDay() + 6) % 7));
export const minutesOf = (d: Date) => d.getHours() * 60 + d.getMinutes();
export const mm = (m: number) => `${pad(Math.floor(m / 60))}:${pad(m % 60)}`;

export const TONE: Record<string, string> = { draft: 'ev-draft', published: 'ev-pub', finished: 'ev-done', cancelled: 'ev-off' };

/** side-by-side lanes for events that overlap within one day */
export function lanes<T extends { starts_at: string; ends_at: string }>(items: T[]): { e: T; lane: number; lanes: number }[] {
  const sorted = [...items].sort((a, b) => a.starts_at.localeCompare(b.starts_at) || b.ends_at.localeCompare(a.ends_at));
  const out: { e: T; lane: number; lanes: number }[] = [];
  let cluster: typeof out = [];
  let end = 0;
  const laneEnds: number[] = [];
  const flush = () => { const n = Math.max(1, ...cluster.map(c => c.lane + 1)); cluster.forEach(c => (c.lanes = n)); out.push(...cluster); cluster = []; laneEnds.length = 0; };
  for (const e of sorted) {
    const st = Date.parse(e.starts_at), en = Date.parse(e.ends_at);
    if (cluster.length && st >= end) flush();
    let lane = laneEnds.findIndex(x => x <= st);
    if (lane < 0) { lane = laneEnds.length; laneEnds.push(en); } else laneEnds[lane] = en;
    cluster.push({ e, lane, lanes: 1 });
    end = Math.max(end, en);
  }
  if (cluster.length) flush();
  return out;
}

