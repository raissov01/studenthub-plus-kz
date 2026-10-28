import type { Metadata } from 'next';
import { redirect } from 'next/navigation';
import { Screen } from '@/components/Screen';
import { EVENTS, EVENT_CATS } from '@/lib/data/static';
import { pageMeta } from '@/lib/seo';

export function generateStaticParams() {
  return EVENTS.map(e => ({ id: e.id }));
}

export async function generateMetadata({ params }: PageProps<'/event/[id]'>): Promise<Metadata> {
  const { id } = await params;
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
  if (!EVENTS.some(x => x.id === id)) redirect('/events');
  return <Screen v={`event:${id}`} />;
}
