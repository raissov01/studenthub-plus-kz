import { AccountGate } from '@/components/quiz/common';
import { QuizEditor } from '@/components/quiz/editor';
export default async function Page({ params }: { params: Promise<{ id: string }> }) {
  return (
    <AccountGate>
      <QuizEditor id={(await params).id} />
    </AccountGate>
  );
}
