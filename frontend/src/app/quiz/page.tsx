import { QuizCatalog } from '@/components/quiz/catalog';
export default async function Page({ searchParams }: { searchParams: Promise<{ tab?: string }> }) {
  return <QuizCatalog initialTab={(await searchParams).tab} />;
}
