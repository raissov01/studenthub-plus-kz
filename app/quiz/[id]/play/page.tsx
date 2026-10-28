import { QuizPlayScreen } from '@/components/quiz/play';
export default async function Page({
  params,
  searchParams,
}: {
  params: Promise<{ id: string }>;
  searchParams: Promise<{ share?: string; attempt?: string }>;
}) {
  const [p, s] = await Promise.all([params, searchParams]);
  return <QuizPlayScreen key={`${p.id}-${s.attempt || ''}`} id={p.id} share={s.share} attempt={s.attempt} />;
}
