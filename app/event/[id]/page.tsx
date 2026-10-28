import type { Metadata } from 'next';
import { redirect } from 'next/navigation';
import { Screen } from '@/components/Screen';
import { EVENTS, EVENT_CATS } from '@/lib/data/static';
import { apiOrigin } from '@/lib/content';
import { pageMeta } from '@/lib/seo';

export function generateStaticParams() {
  return EVENTS.map(e => ({ id: e.id }));
}

/** DB events (numeric ids, prompt 05) for link previews; null when the API is off, unreachable or the event is a draft */
async function dbEvent(id: string): Promise<{ title_kk: string; desc_kk: string | null; place_text: string | null } | null> {
  try {
    const r = await fetch(`${apiOrigin()}/api/events/${id}`, { next: { revalidate: 60 }, signal: AbortSignal.timeout(3000), headers: { accept: 'application/json' } });
    if (!r.ok) return null;
    return ((await r.json()) as { event?: { title_kk: string; desc_kk: string | null; place_text: string | null } }).event ?? null;
  } catch { return null; }
}

export async function generateMetadata({ params }: PageProps<'/event/[id]'>): Promise<Metadata> {
  const { id } = await params;
  if (/^\d+$/.test(id)) {
    const d = await dbEvent(id);
    if (!d) return { robots: { index: false, follow: false } };
    return pageMeta({ title: d.title_kk, description: [d.place_text, d.desc_kk].filter(Boolean).join('. ').slice(0, 300), path: `/event/${id}`, type: 'article' });
  }
  const e = EVENTS.find(x => x.id === id);
  if (!e) return {};
  return pageMeta({
    title: e.title.kk,
    description: `${EVENT_CATS[e.cat]?.kk ?? ''} · ${e.place.kk}, ${e.time}. ${e.desc.kk}`.trim(),
    path: `/event/${encodeURIComponent(e.id)}`,
    type: 'article',
  });
}

export default async function Page({ params }: PageProps<'/event/[id]'>) {
  const { id } = await params;
  // numeric ids are DB events; the screen itself shows the neutral not-found view when the events switch is off
  if (/^\d+$/.test(id)) return <Screen v={`event:${id}`} />;
  if (!EVENTS.some(x => x.id === id)) redirect('/events');
  return <Screen v={`event:${id}`} />;
}
