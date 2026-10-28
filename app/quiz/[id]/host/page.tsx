import { AccountGate } from '@/components/quiz/common';
import { QuizHost } from '@/components/quiz/live';
export default async function Page({ params }: { params: Promise<{ id: string }> }) {
  return (
    <AccountGate>
      <QuizHost id={(await params).id} />
    </AccountGate>
  );
}
