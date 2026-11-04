import type { ReactNode } from 'react';
import { notFound } from 'next/navigation';
import { quizServerInfo } from '@/lib/quiz-api';
import { QuizGate } from '@/components/quiz/common';
export default async function Layout({ children }: { children: ReactNode }) {
  if (!(await quizServerInfo()).enabled) notFound();
  return <QuizGate>{children}</QuizGate>;
}
