import type { Metadata } from 'next';
import { Screen } from '@/components/Screen';

/* /admin/content/new?kind=… and /admin/content/adm-<n>: the admin content editor, rendered on the client; everyone but an
   admin gets the neutral not-found screen. /admin/content itself (the list) is served by the catch-all route. */
export const metadata: Metadata = { robots: { index: false, follow: false } };

export default async function Page({ params }: PageProps<'/admin/content/[...rest]'>) {
  const { rest } = await params;
  return <Screen v={`admin:content/${rest.join('/')}`} />;
}
