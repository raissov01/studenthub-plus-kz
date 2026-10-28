import { AccountGate } from '@/components/quiz/common';
import { QuizEditor } from '@/components/quiz/editor';
export default async function Page({ searchParams }: { searchParams: Promise<{ notes?: string }> }) {
  return (
    <AccountGate>
      <QuizEditor notes={(await searchParams).notes === '1'} />
    </AccountGate>
  );
}
