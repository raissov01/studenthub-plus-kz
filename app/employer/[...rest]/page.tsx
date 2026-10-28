import type { Metadata } from 'next';
import { Screen } from '@/components/Screen';

/* /employer/apply, /employer/join/<code>, /employer/jobs/new, /employer/jobs/<id>, /employer/jobs/<id>/edit (prompt 10):
   account-specific, rendered on the client; unknown sub-paths, other roles and a switched-off feature get the neutral
   not-found screen. /employer itself is served by the catch-all route. */
export const metadata: Metadata = { robots: { index: false, follow: false } };

export default async function Page({ params }: PageProps<'/employer/[...rest]'>) {
  const { rest } = await params;
  return <Screen v={`employer:${rest.join('/')}`} />;
}
