/* Campus map logic: room search, GPS projection onto the illustrated plan, entrances, where a class is. */
import { BUILDINGS, CITY_SITES, ENTR, GEO, KIND_L, NEARBY } from '@/lib/data/campus';
import type { I18n } from '@/lib/format';
import type { Building, CitySite, ClassItem } from '@/lib/types';

export type MapSearch = null | { room: true; corp: number; floor: number; num: string; b?: Building } | { room?: false; list: Building[]; sites: CitySite[] };

export function mapSearch(q: string, I: I18n): MapSearch {
  q = q.trim();
  if (!q) return null;
  const m = q.match(/^(\d)\s*[-–]?\s*(\d)(\d{2})$/) || q.match(/^(\d)(\d)(\d{2})$/);
  if (m) {
    const corp = +m[1], floor = +m[2], b = BUILDINGS.find(x => x.corp === corp);
    return { room: true, corp, floor, num: `${m[1]}-${m[2]}${m[3]}`, b };
  }
  const ql = q.toLowerCase();
  return {
    list: BUILDINGS.filter(b => (b.n + ' ' + I.L(b.name) + ' ' + I.L(b.has) + ' ' + I.tp(KIND_L[b.kind])).toLowerCase().includes(ql)),
    sites: CITY_SITES.filter(s => [s.name.kk, s.name.ru, s.a.kk, s.a.ru, I.L(s.has), I.tp(KIND_L[s.kind])].join(' ').toLowerCase().includes(ql))
  };
}

/** centre of the main campus (Жеңіс даңғылы, 62) */
export const CAMPUS_LL: [number, number] = [51.18692, 71.41178];
/** map links for a city site: by coordinates when known, otherwise by address */
export function siteLinks(s: CitySite, I: Pick<I18n, 'L'>) {
  if (s.lat != null && s.lon != null) {
    return {
      gis: `https://2gis.kz/astana/geo/${s.lon},${s.lat}?m=${s.lon},${s.lat}/17`,
      google: `https://www.google.com/maps/search/?api=1&query=${s.lat},${s.lon}`
    };
  }
  const q = `${I.L(s.a)}, Астана`;
  return {
    gis: `https://2gis.kz/astana/search/${encodeURIComponent(q)}`,
    google: `https://www.google.com/maps/search/?api=1&query=${encodeURIComponent(q)}`
  };
}

/** GPS on the illustrated map: affine fit of 14 OSM building centres to image pixels (mean error ~11 m) */
export function geoToPx(lat: number, lon: number): [number, number] {
  const u = (lon - GEO.lon0) * 1e4, v = (lat - GEO.lat0) * 1e4;
  return [GEO.cx[0] * u + GEO.cx[1] * v + GEO.cx[2], GEO.cy[0] * u + GEO.cy[1] * v + GEO.cy[2]];
}
export function metresBetween(lat1: number, lon1: number, lat2: number, lon2: number) {
  const R = 6371000, r = Math.PI / 180, dLat = (lat2 - lat1) * r, dLon = (lon2 - lon1) * r;
  const a = Math.sin(dLat / 2) ** 2 + Math.cos(lat1 * r) * Math.cos(lat2 * r) * Math.sin(dLon / 2) ** 2;
  return 2 * R * Math.asin(Math.sqrt(a));
}
export interface Me { lat: number; lon: number; x: number; y: number; acc: number }
export const onCampus = (me: Me | null): boolean => !!me && me.x > -60 && me.x < GEO.w + 60 && me.y > -60 && me.y < GEO.h + 60;
export const walkMin = (m: number) => Math.max(1, Math.round(m / 80));
export const distTxt = (m: number) => (m >= 1000 ? `${(m / 1000).toFixed(1).replace('.', ',')} км` : `${Math.round(m / 10) * 10} м`);
export const entrPx = (id: string) => (ENTR[id] ? geoToPx(ENTR[id][0], ENTR[id][1]) : null);
export const inPlan = ([x, y]: [number, number]) => x >= 24 && x <= GEO.w - 24 && y >= 24 && y <= GEO.h - 24;
export const nearOnMapCount = () => NEARBY.filter(p => p.lat != null && p.lon != null && inPlan(geoToPx(p.lat, p.lon))).length;

/** where a class of the student's timetable is: building and floor when the room number is a campus one ('4-214') */
export function classWhere(c: Pick<ClassItem, 'room'>, I: I18n) {
  if (!c.room) return '';
  const r = mapSearch(c.room, I);
  if (!r || !r.room || !r.b) return I.tt(`${c.room} аудитория`, `аудитория ${c.room}`);
  return I.tt(`${r.b.corp}-корпус, ${r.floor}-қабат, ${c.room} аудитория`, `${r.b.corp}-й корпус, ${r.floor} этаж, ауд. ${c.room}`);
}
