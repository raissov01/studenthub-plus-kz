/* «Отдам даром» market (prompt 08) on the client: API shapes (docs/api/market.md, the Go code wins), labels,
   the browser-side photo re-encode and the search-item builder (a separate «Тегін заттар» search category). */
import type { LText } from '@/lib/types';

export interface MarketAuthor { name: string; username: string | null; telegramUrl: string | null }
export interface MarketItem {
  id: number; title: string; desc: string; category: string; condition: string; university: string; photos: string[];
  status: 'pending' | 'active' | 'reserved' | 'given' | 'hidden' | 'expired'; hiddenReason?: 'reports' | 'admin';
  createdAt: string; expiresAt: string; givenAt?: string; daysAgo: number; mine: boolean;
  pickup: string | null; pickupHint: string; author: MarketAuthor | null;
}
export interface MarketList { items: MarketItem[]; university: string; season: { from: string; to: string; active: boolean } | null; nextBefore?: number }
export interface MarketMe { canPublish: boolean; blockers: string[]; rulesAccepted: boolean; hasUsername: boolean; banUntil: string | null; today: number; dailyLimit: number }
export interface AdminMarketItem extends MarketItem { ownerId: number; reports: number; countedReports: number; reportReasons: string[]; submissionId: number | null }

export const MARKET_CATS: Record<string, LText> = {
  books: { kk: 'Кітап, конспект', ru: 'Книги, конспекты' },
  dorm: { kk: 'Жатақхана, тұрмыс', ru: 'Общежитие, быт' },
  tech: { kk: 'Техника', ru: 'Техника' },
  clothes: { kk: 'Киім', ru: 'Одежда' },
  sport: { kk: 'Спорт', ru: 'Спорт' },
  other: { kk: 'Басқа', ru: 'Другое' },
};
export const MARKET_CONDITIONS: Record<string, LText> = {
  new: { kk: 'Жаңа', ru: 'Новое' },
  good: { kk: 'Жақсы', ru: 'Хорошее' },
  used: { kk: 'Қолданылған', ru: 'Б/у' },
};
export const MARKET_REPORT_REASONS: Record<string, LText> = {
  selling: { kk: 'Сатып жатыр', ru: 'Продаёт' },
  prohibited: { kk: 'Тыйым салынған зат', ru: 'Запрещённая вещь' },
  scam: { kk: 'Алаяқтық', ru: 'Мошенничество' },
  other: { kk: 'Басқа', ru: 'Другое' },
};
export const MARKET_LIMITS = { title: 60, desc: 300, pickup: 120, photos: 3 };

/** a search row for the global search («Тегін заттар» category); built from GET /api/market/items?q= */
export interface MarketSearchItem { k: 'market'; id: string; go: string; org: string; title: string; meta: string }
export function marketSearchItems(items: MarketItem[], L: (t: LText) => string, freeLabel: string): MarketSearchItem[] {
  return items.map(it => ({
    k: 'market', id: 'mk' + it.id, go: 'market:' + it.id, org: it.title,
    title: [L(MARKET_CATS[it.category] ?? MARKET_CATS.other), L(MARKET_CONDITIONS[it.condition] ?? MARKET_CONDITIONS.used)].join(' · '),
    meta: freeLabel,
  }));
}

/** Re-encode a photo in the browser: long side ≤ 1280 px, JPEG ≤ ~500 KB. Drawing to a canvas drops EXIF/GPS. */
export async function shrinkPhoto(file: File, maxSide = 1280, maxBytes = 500_000): Promise<Blob> {
  const bmp = await createImageBitmap(file, { imageOrientation: 'from-image' } as ImageBitmapOptions).catch(() => null);
  let w: number, h: number, draw: (c: CanvasRenderingContext2D, W: number, H: number) => void;
  if (bmp) {
    w = bmp.width; h = bmp.height;
    draw = (c, W, H) => c.drawImage(bmp, 0, 0, W, H);
  } else {
    const url = URL.createObjectURL(file);
    const img = await new Promise<HTMLImageElement>((res, rej) => { const i = new Image(); i.onload = () => res(i); i.onerror = rej; i.src = url; });
    URL.revokeObjectURL(url);
    w = img.naturalWidth; h = img.naturalHeight;
    draw = (c, W, H) => c.drawImage(img, 0, 0, W, H);
  }
  const k = Math.min(1, maxSide / Math.max(w, h));
  const W = Math.max(1, Math.round(w * k)), H = Math.max(1, Math.round(h * k));
  const canvas = document.createElement('canvas');
  canvas.width = W; canvas.height = H;
  const ctx = canvas.getContext('2d');
  if (!ctx) throw new Error('canvas');
  ctx.fillStyle = '#FFFFFF';
  ctx.fillRect(0, 0, W, H);
  draw(ctx, W, H);
  let q = 0.85, blob: Blob | null = null;
  for (let i = 0; i < 6; i++) {
    blob = await new Promise<Blob | null>(res => canvas.toBlob(res, 'image/jpeg', q));
    if (blob && blob.size <= maxBytes) break;
    q -= 0.12;
  }
  if (!blob) throw new Error('encode');
  return blob;
}
