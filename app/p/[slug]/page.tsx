import type { Metadata } from 'next';
import { headers } from 'next/headers';
import Link from 'next/link';
import { notFound } from 'next/navigation';
import { cache } from 'react';
import { PortfolioPage } from '@/components/PortfolioPage';
import { apiOrigin } from '@/lib/content';
import type { PortfolioPage as Page } from '@/lib/portfolio';

/* /p/<slug>: the public portfolio (prompt 09), rendered on the server from GET /api/p/{slug} on every request (no
   cache: a page the student turns off or re-links disappears at once). A disabled page, an unknown or old slug and
   the switch being off all get the same neutral not-found screen with HTTP 404. Never indexed. */

const SLUG = /^[a-z0-9]{10}$/;

type Load = { page: Page } | { missing: true } | { unavailable: true };

const load = cache(async (slug: string): Promise<Load> => {
  if (!SLUG.test(slug)) return { missing: true };
  const h = await headers();
  const fwd: Record<string, string> = { accept: 'application/json' };
  // the API rate-limits this endpoint per visitor IP; pass the visitor's address through
  const ip = h.get('x-real-ip') || h.get('x-forwarded-for')?.split(',')[0]?.trim();
  if (ip) { fwd['x-real-ip'] = ip; fwd['x-forwarded-for'] = ip; }
  try {
    const r = await fetch(`${apiOrigin()}/api/p/${slug}`, { cache: 'no-store', headers: fwd, signal: AbortSignal.timeout(5000) });
    if (r.status === 404) return { missing: true };
    if (!r.ok) return { unavailable: true };
    return { page: (await r.json()) as Page };
  } catch {
    return { unavailable: true };
  }
});

async function langOf(searchParams: Promise<Record<string, string | string[] | undefined>>): Promise<'kk' | 'ru'> {
  const q = (await searchParams).lang;
  if (q === 'ru' || q === 'kk') return q;
  const al = (await headers()).get('accept-language') || '';
  return /^\s*ru\b/i.test(al) ? 'ru' : 'kk';
}

const NOINDEX = { index: false, follow: false } as const;

export async function generateMetadata({ params }: PageProps<'/p/[slug]'>): Promise<Metadata> {
  const { slug } = await params;
  const r = await load(slug);
  if (!('page' in r)) return { robots: NOINDEX };
  const og = r.page.og;
  const description = og.description || 'Student Hub';
  return {
    title: { absolute: og.title },
    description,
    robots: NOINDEX,
    openGraph: { type: 'profile', siteName: 'Student Hub', title: og.title, description, url: og.url, images: [{ url: og.image, width: 1200, height: 630 }], locale: 'kk_KZ' },
    twitter: { card: 'summary_large_image', title: og.title, description, images: [og.image] },
  };
}

export default async function Public({ params, searchParams }: PageProps<'/p/[slug]'>) {
  const { slug } = await params;
  const r = await load(slug);
  if ('missing' in r) notFound();
  const lang = await langOf(searchParams);
  if ('unavailable' in r) {
    return (
      <div className="empty" role="status">
        {lang === 'ru' ? 'Страница сейчас недоступна. Попробуйте позже.' : 'Бет қазір ашылмай тұр. Кейінірек қайталаңыз.'}
      </div>
    );
  }
  const other = lang === 'ru' ? 'kk' : 'ru';
  return (
    <>
      <nav className="pf-bar no-print" aria-label={lang === 'ru' ? 'Язык' : 'Тіл'}>
        <Link className="pf-brand" href="/">Student Hub</Link>
        <Link className="chip" href={`/p/${slug}?lang=${other}`} hrefLang={other} lang={other} prefetch={false}>{other === 'ru' ? 'Русский' : 'Қазақша'}</Link>
      </nav>
      <PortfolioPage page={r.page} lang={lang} />
    </>
  );
}
