import type { Metadata } from 'next';
import { Screen } from '@/components/Screen';

/* /market/<id> (and /market/new, /market/mine): device- and account-specific, rendered on the client; the screen shows the
   neutral not-found view when the feature switch is off or the id is unknown. */
export const metadata: Metadata = { robots: { index: false, follow: false } };

export default async function Page({ params }: PageProps<'/market/[id]'>) {
  const { id } = await params;
  return <Screen v={`market:${id}`} />;
}
