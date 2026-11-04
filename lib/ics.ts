/* «Кестені күнтізбеге қосу» (F5): the class schedule as an iCalendar file built in the browser (RFC 5545).
   CRLF line endings, lines folded at 75 octets without splitting a UTF-8 character, TZID Asia/Almaty with its
   VTIMEZONE, one VEVENT per class repeating every weekday until the end of the semester, UIDs that stay the same
   between exports (so importing again updates the events instead of duplicating them). Pure: no DOM here. */

export interface IcsClass {
  /** stable key parts (not translated), used for the UID */
  key: string;
  /** 'HH:MM' Almaty wall clock */
  start: string;
  end: string;
  summary: string;
  location: string;
  description: string;
}

export const ICS_TZID = 'Asia/Almaty';
/** Kazakhstan has been UTC+5 all year since 2024-03-01 */
const OFFSET_MS = 5 * 3600_000;
/** classes run on weekdays (the sample timetable is the same every weekday) */
export const CLASS_DAYS = ['MO', 'TU', 'WE', 'TH', 'FR'] as const;

const pad = (n: number, w = 2) => String(n).padStart(w, '0');

/** escape a TEXT value (RFC 5545 3.3.11) */
export function escText(s: string): string {
  return s.replace(/\\/g, '\\\\').replace(/;/g, '\\;').replace(/,/g, '\\,').replace(/\r\n|\r|\n/g, '\\n');
}

/** fold one content line at 75 octets; continuation lines start with a space (which counts toward the 75) */
export function foldLine(line: string): string {
  const enc = new TextEncoder();
  const out: string[] = [];
  let cur = '', bytes = 0, limit = 75;
  for (const ch of line) {
    const n = enc.encode(ch).length;
    if (bytes + n > limit) {
      out.push(cur);
      cur = ' ';
      bytes = 1;
      limit = 75;
    }
    cur += ch;
    bytes += n;
  }
  out.push(cur);
  return out.join('\r\n');
}

/** FNV-1a, hex: a short stable id from the class's untranslated fields */
export function stableHash(s: string): string {
  let h = 0x811c9dc5;
  for (const b of new TextEncoder().encode(s)) {
    h ^= b;
    h = Math.imul(h, 0x01000193) >>> 0;
  }
  return h.toString(16).padStart(8, '0');
}

/** the Almaty calendar day of an instant, as UTC fields of a Date at 00:00 */
function almatyDay(at: Date): Date {
  const a = new Date(at.getTime() + OFFSET_MS);
  return new Date(Date.UTC(a.getUTCFullYear(), a.getUTCMonth(), a.getUTCDate()));
}

/** Semester the export covers: from today (or the next semester's start in the summer) to its last day.
    Autumn: 1 Sep – 31 Dec, spring: 15 Jan – 31 May (Almaty calendar days). */
export function semesterRange(now: Date): { from: Date; until: Date } {
  const d = almatyDay(now);
  const y = d.getUTCFullYear(), m = d.getUTCMonth();
  if (m >= 8) return { from: d, until: new Date(Date.UTC(y, 11, 31)) };
  if (m <= 4) {
    const start = new Date(Date.UTC(y, 0, 15));
    return { from: d < start ? start : d, until: new Date(Date.UTC(y, 4, 31)) };
  }
  return { from: new Date(Date.UTC(y, 8, 1)), until: new Date(Date.UTC(y, 11, 31)) };
}

const WD = ['SU', 'MO', 'TU', 'WE', 'TH', 'FR', 'SA'];
/** the first class day on or after `d` */
function firstClassDay(d: Date): Date {
  const x = new Date(d);
  while (!(CLASS_DAYS as readonly string[]).includes(WD[x.getUTCDay()])) x.setUTCDate(x.getUTCDate() + 1);
  return x;
}

const ymd = (d: Date) => `${d.getUTCFullYear()}${pad(d.getUTCMonth() + 1)}${pad(d.getUTCDate())}`;
const local = (d: Date, hm: string) => `${ymd(d)}T${hm.replace(':', '')}00`;
const utcStamp = (d: Date) => `${d.getUTCFullYear()}${pad(d.getUTCMonth() + 1)}${pad(d.getUTCDate())}T${pad(d.getUTCHours())}${pad(d.getUTCMinutes())}${pad(d.getUTCSeconds())}Z`;

const VTIMEZONE = [
  'BEGIN:VTIMEZONE',
  `TZID:${ICS_TZID}`,
  `X-LIC-LOCATION:${ICS_TZID}`,
  'BEGIN:STANDARD',
  'TZOFFSETFROM:+0500',
  'TZOFFSETTO:+0500',
  'TZNAME:+05',
  'DTSTART:19700101T000000',
  'END:STANDARD',
  'END:VTIMEZONE',
];

export function buildScheduleIcs(classes: IcsClass[], o: { now: Date; calName: string; lang: 'kk' | 'ru' }): string {
  const { from, until } = semesterRange(o.now);
  const first = firstClassDay(from);
  // UNTIL is in UTC when DTSTART has a TZID (RFC 5545 3.3.10): the semester's last day, 23:59:59 Almaty
  const untilUtc = new Date(until.getTime() + 24 * 3600_000 - 1000 - OFFSET_MS);
  const stamp = utcStamp(o.now);
  const lines: string[] = [
    'BEGIN:VCALENDAR',
    'VERSION:2.0',
    `PRODID:-//Student Hub//Schedule//${o.lang.toUpperCase()}`,
    'CALSCALE:GREGORIAN',
    'METHOD:PUBLISH',
    `X-WR-CALNAME:${escText(o.calName)}`,
    `X-WR-TIMEZONE:${ICS_TZID}`,
    ...VTIMEZONE,
  ];
  for (const c of classes) {
    lines.push(
      'BEGIN:VEVENT',
      `UID:class-${stableHash(c.key)}@studenthub`,
      `DTSTAMP:${stamp}`,
      `DTSTART;TZID=${ICS_TZID}:${local(first, c.start)}`,
      `DTEND;TZID=${ICS_TZID}:${local(first, c.end)}`,
      `RRULE:FREQ=WEEKLY;BYDAY=${CLASS_DAYS.join(',')};UNTIL=${utcStamp(untilUtc)}`,
      `SUMMARY:${escText(c.summary)}`,
      `LOCATION:${escText(c.location)}`,
      `DESCRIPTION:${escText(c.description)}`,
      'TRANSP:OPAQUE',
      'END:VEVENT',
    );
  }
  lines.push('END:VCALENDAR');
  return lines.map(foldLine).join('\r\n') + '\r\n';
}

export interface IcsOne {
  /** stable key (an item id), used for the UID so a second import updates the event */
  key: string;
  /** 'YYYY-MM-DD' Almaty day */
  date: string;
  /** 'HH:MM' Almaty wall clock; empty = all-day */
  time?: string;
  summary: string;
  description?: string;
  /** minutes, for a timed event (default 60) */
  minutes?: number;
}

/** One deadline or event as an .ics file (the calendar screen's «.ics жүктеу»): all-day (DATE) or timed in Asia/Almaty,
    with a reminder one day before. */
export function buildOneIcs(e: IcsOne, o: { now: Date; lang: 'kk' | 'ru' }): string {
  const d = new Date(e.date.slice(0, 10) + 'T00:00:00Z');
  const lines: string[] = [
    'BEGIN:VCALENDAR',
    'VERSION:2.0',
    `PRODID:-//Student Hub//Calendar//${o.lang.toUpperCase()}`,
    'CALSCALE:GREGORIAN',
    'METHOD:PUBLISH',
  ];
  const ev: string[] = ['BEGIN:VEVENT', `UID:cal-${stableHash(e.key)}@studenthub`, `DTSTAMP:${utcStamp(o.now)}`];
  if (e.time && /^\d{2}:\d{2}$/.test(e.time)) {
    lines.push(...VTIMEZONE);
    const start = new Date(d.getTime() + (+e.time.slice(0, 2) * 60 + +e.time.slice(3, 5)) * 60_000);
    const end = new Date(start.getTime() + (e.minutes ?? 60) * 60_000);
    const wall = (x: Date) => `${ymd(x)}T${pad(x.getUTCHours())}${pad(x.getUTCMinutes())}00`;
    ev.push(`DTSTART;TZID=${ICS_TZID}:${wall(start)}`, `DTEND;TZID=${ICS_TZID}:${wall(end)}`);
  } else {
    ev.push(`DTSTART;VALUE=DATE:${ymd(d)}`, `DTEND;VALUE=DATE:${ymd(new Date(d.getTime() + 24 * 3600_000))}`, 'TRANSP:TRANSPARENT');
  }
  ev.push(`SUMMARY:${escText(e.summary)}`);
  if (e.description) ev.push(`DESCRIPTION:${escText(e.description)}`);
  ev.push('BEGIN:VALARM', 'ACTION:DISPLAY', `DESCRIPTION:${escText(e.summary)}`, 'TRIGGER:-P1D', 'END:VALARM', 'END:VEVENT');
  lines.push(...ev, 'END:VCALENDAR');
  return lines.map(foldLine).join('\r\n') + '\r\n';
}
