import type { Metadata } from 'next';
import { Screen } from '@/components/Screen';

/* /organize/<id> (and /organize/new, /organize/mine): device- and account-specific, rendered on the client; the screen shows the
   neutral not-found view when the feature switch is off or the id is unknown. */
export const metadata: Metadata = { robots: { index: false, follow: false } };

export default async function Page({ params }: PageProps<'/organize/[id]'>) {
  const { id } = await params;
  return <Screen v={`organize:${id}`} />;
}
