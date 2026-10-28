import type { Metadata } from 'next';
import { Screen } from '@/components/Screen';

/* /certificate/<code>: the printable ambassador certificate (prompt 06), loaded from the API on the client. */
export const metadata: Metadata = { robots: { index: false, follow: false } };

export default async function Page({ params }: PageProps<'/certificate/[code]'>) {
  const { code } = await params;
  return <Screen v={`certificate:${code}`} />;
}
