/* Job match for a specific vacancy; rendered on demand so vacancies added after the build work too. */
import { Screen } from '@/components/Screen';
import { getContent } from '@/lib/content';

export async function generateStaticParams() {
  return (await getContent()).jobs.map(j => ({ id: j.id }));
}

export default async function Page({ params }: PageProps<'/match/[id]'>) {
  const { id } = await params;
  return <Screen v={`match:${id}`} />;
}
