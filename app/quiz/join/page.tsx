import { QuizJoin } from '@/components/quiz/live';
export default async function Page({ searchParams }: { searchParams: Promise<{ code?: string }> }) {
  return <QuizJoin initialCode={(await searchParams).code} />;
}
