import type { Metadata } from 'next';
import { redirect } from 'next/navigation';
import { Screen } from '@/components/Screen';
import { CITIES } from '@/lib/data/static';
import { apiOrigin, getContent, getJob } from '@/lib/content';
import { jobDbId } from '@/lib/b2b';
import type { Job } from '@/lib/types';
import { money } from '@/lib/format';
import { pageMeta } from '@/lib/seo';

export async function generateStaticParams() {
  return (await getContent()).jobs.map(j => ({ id: j.id }));
}

/** an employer job approved after the content cache was filled: GET /api/jobs/{id} (anonymous = no age filter) */
async function liveJob(id: string): Promise<Job | null> {
  const n = jobDbId(id);
  if (!n) return null;
  try {
    const r = await fetch(`${apiOrigin()}/api/jobs/${n}`, { cache: 'no-store', signal: AbortSignal.timeout(3000), headers: { accept: 'application/json' } });
    return r.ok ? ((await r.json()) as { job: Job }).job : null;
  } catch { return null; }
}
const findJob = async (id: string) => (await getJob(id)) ?? (await liveJob(id));

export async function generateMetadata({ params }: PageProps<'/job/[id]'>): Promise<Metadata> {
  const { id } = await params;
  const j = await findJob(id);
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
  if (!(await findJob(id))) redirect('/jobs');
  return <Screen v={`job:${id}`} />;
}
