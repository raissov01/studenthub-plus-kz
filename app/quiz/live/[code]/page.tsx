import { QuizPlayer } from '@/components/quiz/live';
export default async function Page({ params }: { params: Promise<{ code: string }> }) {
  return <QuizPlayer code={(await params).code} />;
}
