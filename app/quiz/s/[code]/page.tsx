import type { Metadata } from 'next';
import { QuizDetail } from '@/components/quiz/catalog';
import { quizServerInfo } from '@/lib/quiz-api';
type Props = { params: Promise<{ code: string }> };
export async function generateMetadata({ params }: Props): Promise<Metadata> {
  const { quiz } = await quizServerInfo(undefined, (await params).code);
  if (!quiz) return { title: 'Student Hub', robots: { index: false, follow: false } };
  return {
    title: quiz.title,
    description: quiz.description,
    robots: { index: false },
    openGraph: { title: quiz.title, description: quiz.description, type: 'website' },
  };
}
export default async function Page({ params }: Props) {
  return <QuizDetail id="" share={(await params).code} />;
}
