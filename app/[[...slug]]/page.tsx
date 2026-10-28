/* Every screen except the content pages with their own metadata (job, opportunity, event) and the device-local
   ones (plan, deck) and /match/<job id>. All paths are known at build time; anything else is a plain 404. */
import type { Metadata } from 'next';
import { notFound } from 'next/navigation';
import { Screen } from '@/components/Screen';
import { DOCS } from '@/lib/data/static';
import { i18n } from '@/lib/format';
import { staticSlugs, viewOfPath } from '@/lib/routes';
import { screenMeta } from '@/lib/screens';
import { DEFAULT_DESCRIPTION, DEFAULT_TITLE, pageMeta } from '@/lib/seo';
import { defaults, UI_INIT } from '@/lib/defaults';

export function generateStaticParams() {
  return [...staticSlugs().map(slug => ({ slug })), ...DOCS.map(d => ({ slug: ['doc', d.id] }))];
}

export const dynamicParams = false;

const viewOf = (slug: string[] = []) => viewOfPath('/' + slug.map(encodeURIComponent).join('/'));

export async function generateMetadata({ params }: PageProps<'/[[...slug]]'>): Promise<Metadata> {
  const { slug } = await params;
  const v = viewOf(slug);
  if (!v) return {};
  const path = '/' + (slug || []).join('/');
  if (v === 'home') return { ...pageMeta({ title: DEFAULT_TITLE, description: DEFAULT_DESCRIPTION, path: '/' }), title: { absolute: 'Student Hub' } };
  const kk = i18n('kk');
  const { title } = screenMeta(v, { I: kk, S: defaults('kk'), ui: UI_INIT });
  const robots = v === 'admin' ? { robots: { index: false, follow: false } } : {};
  return { ...pageMeta({ title: `${title} — Student Hub`, description: DEFAULT_DESCRIPTION, path }), title, ...robots };
}

export default async function Page({ params }: PageProps<'/[[...slug]]'>) {
  const { slug } = await params;
  const v = viewOf(slug);
  if (!v) notFound();
  return <Screen v={v} />;
}
