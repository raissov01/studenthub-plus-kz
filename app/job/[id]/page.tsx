import type { Metadata } from 'next';
import { redirect } from 'next/navigation';
import { Screen } from '@/components/Screen';
import { CITIES } from '@/lib/data/static';
import { getContent, getJob } from '@/lib/content';
import { money } from '@/lib/format';
import { pageMeta } from '@/lib/seo';

export async function generateStaticParams() {
  return (await getContent()).jobs.map(j => ({ id: j.id }));
}

export async function generateMetadata({ params }: PageProps<'/job/[id]'>): Promise<Metadata> {
  const { id } = await params;
  const j = await getJob(id);
  if (!j) return {};
  const pay = `${money(j.pay)} ${j.per === 'month' ? 'айына' : j.per === 'hour' ? 'сағатына' : 'ауысымға'}`;
  return pageMeta({
    title: `${j.title.kk} — ${j.org.kk}`,
    description: `${pay}, ${CITIES[j.city]?.kk ?? ''}. ${j.desc.kk}`,
    path: `/job/${encodeURIComponent(j.id)}`,
    type: 'article',
  });
}

export default async function Page({ params }: PageProps<'/job/[id]'>) {
  const { id } = await params;
  if (!(await getJob(id))) redirect('/jobs');
  return <Screen v={`job:${id}`} />;
}
