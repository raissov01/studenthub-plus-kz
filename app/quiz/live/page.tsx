// Compatibility with joinUrl emitted by the current Go liveCreate handler.
import { redirect } from 'next/navigation';
export default async function Page({ searchParams }: { searchParams: Promise<{ code?: string }> }) {
  redirect('/quiz/join?code=' + encodeURIComponent((await searchParams).code || ''));
}
