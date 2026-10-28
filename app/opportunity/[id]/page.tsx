import type { Metadata } from 'next';
import { redirect } from 'next/navigation';
import { Screen } from '@/components/Screen';
import { OPP_TYPES } from '@/lib/data/static';
import { getContent, getOpportunity } from '@/lib/content';
import { pageMeta } from '@/lib/seo';

export async function generateStaticParams() {
  return (await getContent()).opportunities.map(o => ({ id: o.id }));
}

export async function generateMetadata({ params }: PageProps<'/opportunity/[id]'>): Promise<Metadata> {
  const { id } = await params;
  const o = await getOpportunity(id);
  if (!o) return {};
  const until = o.deadline ? ` Өтінім мерзімі: ${o.deadline}.` : '';
  return pageMeta({
    title: o.title.kk,
    description: `${OPP_TYPES[o.type]?.[0] ?? ''} · ${o.org.kk}.${until} ${o.desc.kk}`,
    path: `/opportunity/${encodeURIComponent(o.id)}`,
    type: 'article',
  });
}

export default async function Page({ params }: PageProps<'/opportunity/[id]'>) {
  const { id } = await params;
  // unknown or expired (past deadline) opportunities fall back to the list
  if (!(await getOpportunity(id))) redirect('/internships');
  return <Screen v={`opp:${id}`} />;
}
