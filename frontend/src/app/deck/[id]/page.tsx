/* Device-local decks: the id exists only in this browser's storage, so the page renders on demand. */
import { Screen } from '@/components/Screen';

export const metadata = { robots: { index: false } };

export default async function Page({ params }: PageProps<'/deck/[id]'>) {
  const { id } = await params;
  return <Screen v={`deck:${id}`} />;
}
