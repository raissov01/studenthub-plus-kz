/* «Мүмкіндік ұсыну» (docs/SUGGEST.md, docs/api/admin-content.md): the suggestion payload a student sends to
   POST /api/submissions and an admin corrects with PATCH /api/admin/queue/{id}. Same rules as pkg/ambassador
   CleanPayload, so the form can mark a field before the round trip. Pure: no React, no fetch. */
import type { Msg } from './admincontent';
import { CITY_CODES, httpsOk, OPP_TYPE_CODES, validDate } from './admincontent';

export type SugKind = 'opportunity' | 'place';
export const PLACE_CAT_CODES = ['canteen', 'cafe', 'print', 'copy', 'library', 'shop', 'pharmacy', 'sport', 'cowork', 'other'];

/** every field of both kinds as form strings (booleans separately) */
export interface SugForm {
  type: string; title_kk: string; title_ru: string; org_kk: string; org_ru: string; desc_kk: string; desc_ru: string;
  source_url: string; deadline: string; noDeadline: boolean; city: string; funded: boolean; free: boolean;
  cat: string; name_kk: string; name_ru: string; address: string; building_id: string; hours: string;
}

export function emptySug(): SugForm {
  return {
    type: '', title_kk: '', title_ru: '', org_kk: '', org_ru: '', desc_kk: '', desc_ru: '', source_url: '', deadline: '', noDeadline: false,
    city: '', funded: false, free: true, cat: '', name_kk: '', name_ru: '', address: '', building_id: '', hours: '',
  };
}

/** the form of a stored payload (the admin queue editor) */
export function sugOfPayload(p: Record<string, unknown>): SugForm {
  const f = emptySug();
  for (const k of Object.keys(f) as (keyof SugForm)[]) {
    const v = p[k];
    if (typeof v === 'string' && typeof f[k] === 'string') (f as unknown as Record<string, unknown>)[k] = v;
    if (typeof v === 'boolean' && typeof f[k] === 'boolean') (f as unknown as Record<string, unknown>)[k] = v;
  }
  f.noDeadline = !f.deadline;
  return f;
}

const len = (v: string) => [...v.trim()].length;
const CONTROL = /[\u0000-\u0009\u000B-\u001F\u007F-\u009F]/;

export const SUG_MESSAGES: Record<string, Msg> = {
  type: ['Түрін таңдаңыз', 'Выберите тип'],
  title_kk: ['Атауы 3–140 таңба', 'Название 3–140 символов'],
  title_ru: ['Атауы 140 таңбадан аспасын', 'Название не длиннее 140 символов'],
  org_kk: ['Ұйымды жазыңыз (2–140 таңба)', 'Укажите организацию (2–140 символов)'],
  org_ru: ['Ұйым атауы 140 таңбадан аспасын', 'Организация не длиннее 140 символов'],
  desc_kk: ['Сипаттама кемінде 10 таңба (2000-ға дейін)', 'Описание не короче 10 символов (до 2000)'],
  desc_ru: ['Сипаттама 2000 таңбадан аспасын', 'Описание не длиннее 2000 символов'],
  source_url: ['Ресми сілтеме https:// деп басталуы керек', 'Официальная ссылка должна начинаться с https://'],
  deadline: ['Өткен күнді таңдауға болмайды', 'Нельзя выбрать прошедшую дату'],
  city: ['Қаланы таңдаңыз', 'Выберите город'],
  cat: ['Түрін таңдаңыз', 'Выберите тип'],
  name_kk: ['Атауы 2–120 таңба', 'Название 2–120 символов'],
  name_ru: ['Атауы 120 таңбадан аспасын', 'Название не длиннее 120 символов'],
  address: ['Мекенжайы 3–200 таңба', 'Адрес 3–200 символов'],
  building_id: ['Корпусты тізімнен таңдаңыз', 'Выберите корпус из списка'],
  hours: ['Жұмыс уақыты 100 таңбадан аспасын', 'Часы работы не длиннее 100 символов'],
  place_desc: ['Сипаттама 1000 таңбадан аспасын', 'Описание не длиннее 1000 символов'],
  place_url: ['Сілтеме https:// деп басталуы керек', 'Ссылка должна начинаться с https://'],
};

/** every broken field, keyed by payload field name; `today` is YYYY-MM-DD (Almaty) */
export function checkSug(kind: SugKind, f: SugForm, today: string): Record<string, Msg> {
  const e: Record<string, Msg> = {};
  const txt = (k: keyof SugForm, min: number, max: number, msg: Msg = SUG_MESSAGES[k]) => {
    const v = f[k] as string;
    if (len(v) < min || len(v) > max || CONTROL.test(v)) e[k] = msg;
  };
  if (kind === 'opportunity') {
    if (!OPP_TYPE_CODES.includes(f.type)) e.type = SUG_MESSAGES.type;
    txt('title_kk', 3, 140); txt('title_ru', 0, 140); txt('org_kk', 2, 140); txt('org_ru', 0, 140); txt('desc_kk', 10, 2000); txt('desc_ru', 0, 2000);
    const u = f.source_url.trim();
    if (!httpsOk(u) || len(u) > 500) e.source_url = SUG_MESSAGES.source_url;
    if (f.city && !CITY_CODES.includes(f.city)) e.city = SUG_MESSAGES.city;
    if (!f.noDeadline && f.deadline && (!validDate(f.deadline) || f.deadline < today)) e.deadline = SUG_MESSAGES.deadline;
  } else {
    if (!PLACE_CAT_CODES.includes(f.cat)) e.cat = SUG_MESSAGES.cat;
    txt('name_kk', 2, 120); txt('name_ru', 0, 120); txt('address', 3, 200); txt('hours', 0, 100);
    txt('desc_kk', 0, 1000, SUG_MESSAGES.place_desc); txt('desc_ru', 0, 1000, SUG_MESSAGES.place_desc);
    if (f.building_id && !/^[A-Za-z0-9_-]{1,32}$/.test(f.building_id)) e.building_id = SUG_MESSAGES.building_id;
    const u = f.source_url.trim();
    if (u && (!httpsOk(u) || len(u) > 500)) e.source_url = SUG_MESSAGES.place_url;
  }
  return e;
}

/** the payload sent to the API: only this kind's fields, trimmed, empty optional ones left out */
export function sugPayload(kind: SugKind, f: SugForm): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  const put = (k: keyof SugForm) => { const v = (f[k] as string).trim(); if (v) out[k] = v; };
  if (kind === 'opportunity') {
    (['type', 'title_kk', 'title_ru', 'org_kk', 'org_ru', 'desc_kk', 'desc_ru', 'source_url', 'city'] as const).forEach(put);
    if (!f.noDeadline) put('deadline');
    out.funded = f.funded;
    out.free = f.free;
  } else {
    (['cat', 'name_kk', 'name_ru', 'address', 'building_id', 'hours', 'desc_kk', 'desc_ru', 'source_url'] as const).forEach(put);
  }
  return out;
}

/** `invalid_title_kk` -> `title_kk` (the API's per-field codes); '' for other codes */
export function fieldOfCode(code: string): string {
  const m = /^invalid_([a-z_]+)$/.exec(code);
  return m && m[1] !== 'submission' ? m[1] : '';
}
