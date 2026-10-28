/* Shared metadata: site URL, the default card and per-page Open Graph / Twitter tags. */
import type { Metadata } from 'next';

export const SITE_URL = (process.env.NEXT_PUBLIC_SITE_URL || 'https://studenthub-plus-kz.vercel.app').replace(/\/+$/, '');
export const SITE_NAME = 'Student Hub';
export const DEFAULT_TITLE = 'Student Hub — студентке бәрі бір жерде';
export const DEFAULT_DESCRIPTION = 'Жұмыс, тағылымдама, гранттар, жеңілдіктер және AI-көмекші — Сейфуллин университетінің студенттеріне бір жерде.';
const OG_IMAGE = { url: '/og.png', width: 1200, height: 630 };

const clip = (s: string, n = 200) => (s.length > n ? s.slice(0, n - 1).trimEnd() + '…' : s);

/** title/description plus matching og:* and twitter:* (child metadata replaces openGraph wholesale, so it is complete here) */
export function pageMeta({ title, description, path, type = 'website' }: { title: string; description: string; path: string; type?: 'website' | 'article' }): Metadata {
  const d = clip(description);
  return {
    title,
    description: d,
    alternates: { canonical: path },
    openGraph: { type, siteName: SITE_NAME, url: path, title, description: d, images: [OG_IMAGE], locale: 'kk_KZ' },
    twitter: { card: 'summary_large_image', title, description: d, images: [OG_IMAGE.url] },
  };
}
