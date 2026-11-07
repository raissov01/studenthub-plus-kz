/* «Кофе / Кофейня» client (docs/cafe.md, backend/internal/cafe). Prices are integers in tiyn (1 ₸ = 100 tiyn).
   The device only previews a cart total; the server recomputes every order from its own menu. Pure helpers are
   tested in lib/cafe.test.ts. */

export type PaymentMethod = 'pickup' | 'kaspi';
export type OrderStatus = 'new' | 'accepted' | 'ready' | 'picked_up' | 'cancelled' | 'no_show';
export type PaymentStatus = 'unpaid' | 'pending' | 'paid' | 'refunded' | 'failed';

export interface Choice { id: string; nameKk: string; nameRu: string; delta: number }
export interface Option { id: string; nameKk: string; nameRu: string; required: boolean; multi: boolean; choices: Choice[] }
export interface Cafe {
  id: number; nameKk: string; nameRu: string; building: string; placeKk: string; placeRu: string;
  openTime: string; closeTime: string; openDays: number[]; slotMinutes: number; slotCapacity: number; leadMinutes: number;
  active: boolean; paymentMode: 'pickup' | 'kaspi' | 'both'; isDemo: boolean; openNow: boolean; payments: PaymentMethod[];
  kaspiMerchantRef?: string; kaspiOrgId?: number | null; myRole?: string;
  kaspi?: { configured: boolean; loggedIn: boolean; broken: boolean; phoneMasked: string; orgName: string; lastError: string | null; updatedAt: string | null };
  staff?: { userId: number; name: string | null; username: string | null; role: 'barista' | 'manager' }[];
}
export interface Category { id: number; nameKk: string; nameRu: string; sort: number; active: boolean }
export interface Item {
  id: number; cafeId: number; categoryId: number | null; nameKk: string; nameRu: string; descKk: string; descRu: string;
  price: number; available: boolean; archived: boolean; options: Option[]; sort: number;
}
export interface Slot { at: string; full: boolean }
export interface Menu { cafe: Cafe; categories: Category[]; items: Item[]; slots: Slot[] }
export interface LineOption { group: string; choice: string; nameKk: string; nameRu: string; delta: number }
export interface Line { itemId: number; nameKk: string; nameRu: string; qty: number; unitPrice: number; options: LineOption[]; total: number }
export interface Order {
  id: number; cafeId: number; cafeNameKk: string; cafeNameRu: string; placeKk: string; placeRu: string; items: Line[]; total: number;
  pickupAt: string; pickupCode: string; note: string; status: OrderStatus; paymentMethod: PaymentMethod; paymentStatus: PaymentStatus;
  refundManual: boolean; pay: { url: string; qr: string; expiresAt: string | null } | null; cancelReason: string | null;
  createdAt: string; acceptedAt: string | null; readyAt: string | null; closedAt: string | null;
  customer?: string; userId?: number;
  events?: { id: number; actorId: number | null; actorKind: string; kind: string; detail: Record<string, unknown>; createdAt: string }[];
}

/** a cart line on the device: the item and the chosen option ids, never a price the server would trust */
export interface CartLine { key: string; itemId: number; qty: number; options: Record<string, string[]> }

type TT = (kk: string, ru: string) => string;

/** «1 250 ₸» from tiyn */
export function money(tiyn: number): string {
  const t = Math.round(tiyn / 100);
  const s = String(Math.abs(t)).replace(/\B(?=(\d{3})+(?!\d))/g, ' ');
  return `${t < 0 ? '−' : ''}${s} ₸`;
}

/** whole tenge typed by a manager → tiyn; null when not a positive whole number */
export function tengeToTiyn(v: string): number | null {
  const s = v.replace(/[\s ₸]/g, '');
  if (!/^\d{1,6}$/.test(s)) return null;
  const n = Number(s);
  return n > 0 ? n * 100 : null;
}

/** price of one unit with the chosen options; unknown choices count as nothing (the server refuses them anyway) */
export function unitPrice(item: Item, options: Record<string, string[]>): number {
  let p = item.price;
  for (const o of item.options) {
    for (const id of options[o.id] ?? []) p += o.choices.find(c => c.id === id)?.delta ?? 0;
  }
  return p;
}

/** what is missing before the item can go into the cart: the required groups without a choice */
export function missingOptions(item: Item, options: Record<string, string[]>): Option[] {
  return item.options.filter(o => o.required && !(options[o.id]?.length));
}

/** the default choice of each required single group (the first one), so «Add» works with one tap */
export function defaultOptions(item: Item): Record<string, string[]> {
  const out: Record<string, string[]> = {};
  for (const o of item.options) if (o.required && !o.multi && o.choices[0]) out[o.id] = [o.choices[0].id];
  return out;
}

/** toggles a choice: single groups replace, multi groups add/remove */
export function pickChoice(cur: Record<string, string[]>, o: Option, choiceId: string): Record<string, string[]> {
  const was = cur[o.id] ?? [];
  let next: string[];
  if (o.multi) next = was.includes(choiceId) ? was.filter(x => x !== choiceId) : [...was, choiceId];
  else next = o.required ? [choiceId] : was[0] === choiceId ? [] : [choiceId];
  return { ...cur, [o.id]: next };
}

/** one cart key per item + options, so the same drink twice becomes qty 2 */
export function lineKey(itemId: number, options: Record<string, string[]>): string {
  return itemId + '|' + Object.keys(options).sort().map(k => k + ':' + [...(options[k] ?? [])].sort().join(',')).filter(s => !s.endsWith(':')).join(';');
}

export function addToCart(cart: CartLine[], itemId: number, options: Record<string, string[]>, qty = 1): CartLine[] {
  const key = lineKey(itemId, options);
  const i = cart.findIndex(l => l.key === key);
  if (i >= 0) return cart.map((l, j) => j === i ? { ...l, qty: Math.min(10, l.qty + qty) } : l);
  return [...cart, { key, itemId, qty: Math.min(10, qty), options }];
}

export function setQty(cart: CartLine[], key: string, qty: number): CartLine[] {
  return qty <= 0 ? cart.filter(l => l.key !== key) : cart.map(l => l.key === key ? { ...l, qty: Math.min(10, qty) } : l);
}

/** preview of the cart: lines whose item is gone or unavailable are reported, not counted */
export function cartTotal(cart: CartLine[], items: Item[]): { total: number; count: number; gone: CartLine[] } {
  let total = 0, count = 0;
  const gone: CartLine[] = [];
  for (const l of cart) {
    const it = items.find(i => i.id === l.itemId);
    if (!it || !it.available || it.archived) { gone.push(l); continue; }
    total += unitPrice(it, l.options) * l.qty;
    count += l.qty;
  }
  return { total, count, gone };
}

/* ---------- cart storage: one per café, on the device only ---------- */
const CART_KEY = (cafeId: number) => `studenthub.cafe.cart.${cafeId}`;
export function loadCart(cafeId: number): CartLine[] {
  try {
    const v = JSON.parse(localStorage.getItem(CART_KEY(cafeId)) || '[]');
    return Array.isArray(v) ? v.filter(l => l && typeof l.itemId === 'number' && typeof l.qty === 'number' && typeof l.key === 'string') : [];
  } catch { return []; }
}
export function saveCart(cafeId: number, cart: CartLine[]) {
  try { if (cart.length) localStorage.setItem(CART_KEY(cafeId), JSON.stringify(cart)); else localStorage.removeItem(CART_KEY(cafeId)); } catch { /* private mode */ }
}

/* ---------- labels ---------- */

export const isActive = (s: OrderStatus) => s === 'new' || s === 'accepted' || s === 'ready';

export function statusLabel(s: OrderStatus, tt: TT): string {
  return ({
    new: tt('Жаңа', 'Новый'), accepted: tt('Дайындалуда', 'Готовится'), ready: tt('Дайын — алып кетіңіз', 'Готов — заберите'),
    picked_up: tt('Алынды', 'Выдан'), cancelled: tt('Болдырылмады', 'Отменён'), no_show: tt('Алынбады', 'Не забран'),
  } as Record<OrderStatus, string>)[s] ?? s;
}

export function paymentLabel(o: Pick<Order, 'paymentMethod' | 'paymentStatus' | 'refundManual'>, tt: TT): string {
  if (o.refundManual) return tt('Ақшаны кофехана қайтарады', 'Деньги вернёт кофейня');
  if (o.paymentMethod === 'pickup') return o.paymentStatus === 'paid' ? tt('Кассада төленді', 'Оплачено на кассе') : tt('Кассада төлейсіз', 'Оплата на кассе');
  return ({
    unpaid: tt('Kaspi: төленбеген', 'Kaspi: не оплачен'), pending: tt('Kaspi: төлем күтілуде', 'Kaspi: ждём оплату'),
    paid: tt('Kaspi арқылы төленді', 'Оплачено через Kaspi'), refunded: tt('Kaspi-ге қайтарылды', 'Возвращено в Kaspi'),
    failed: tt('Kaspi: төлем өтпеді', 'Kaspi: оплата не прошла'),
  } as Record<PaymentStatus, string>)[o.paymentStatus];
}

const DAYS_KK = ['Дс', 'Сс', 'Ср', 'Бс', 'Жм', 'Сб', 'Жс'];
const DAYS_RU = ['Пн', 'Вт', 'Ср', 'Чт', 'Пт', 'Сб', 'Вс'];
const DAYS_EN = ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun'];
/** «Дс–Жм 08:00–18:00» */
export function hoursLabel(c: Pick<Cafe, 'openDays' | 'openTime' | 'closeTime'>, lang: 'kk' | 'ru' | 'en'): string {
  const names = lang === 'en' ? DAYS_EN : lang !== 'kk' ? DAYS_RU : DAYS_KK;
  const d = [...c.openDays].sort((a, b) => a - b);
  let days: string;
  if (d.length === 7) days = lang === 'en' ? 'Daily' : lang !== 'kk' ? 'Ежедневно' : 'Күн сайын';
  else if (d.length > 1 && d.every((x, i) => i === 0 || x === d[i - 1] + 1)) days = `${names[d[0] - 1]}–${names[d[d.length - 1] - 1]}`;
  else days = d.map(x => names[x - 1]).join(', ');
  return `${days} ${c.openTime}–${c.closeTime}`;
}

/** order progress for the status screen: index of the current step in new → accepted → ready → picked_up */
export function stepIndex(s: OrderStatus): number {
  return s === 'new' ? 0 : s === 'accepted' ? 1 : s === 'ready' ? 2 : s === 'picked_up' ? 3 : -1;
}

/** the status screen keeps polling while something can still change */
export const shouldPoll = (o: Pick<Order, 'status' | 'paymentStatus'>) => isActive(o.status) || o.paymentStatus === 'pending';

/** a phone (or a Telegram Mini App) opens Kaspi from the link; a wide screen shows the QR to scan */
export const preferLink = (w: { innerWidth: number; matchMedia?: (q: string) => { matches: boolean } }) =>
  w.innerWidth < 720 || !!w.matchMedia?.('(pointer: coarse)').matches;

/** orders that appeared since the last board refresh (for the counter's sound) */
export function newArrivals(prev: Set<number> | null, orders: Pick<Order, 'id' | 'status'>[]): number[] {
  if (!prev) return [];
  return orders.filter(o => o.status === 'new' && !prev.has(o.id)).map(o => o.id);
}
