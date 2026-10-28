'use client';
import type { ReactNode } from 'react';
import { usePathname } from 'next/navigation';
import { AppProvider } from '@/components/app';
import { Shell } from '@/components/Shell';
import { PwaRegister } from '@/components/Pwa';
import type { Content } from '@/lib/types';

/** pages shown to people outside the app (the public portfolio /p/<slug>): server-rendered, no app chrome, no onboarding */
const isBare = (path: string | null) => !!path && /^\/p\//.test(path);

export function AppRoot({ content, children }: { content: Content; children: ReactNode }) {
  const pathname = usePathname();
  return (
    <AppProvider content={content}>
      <PwaRegister />
      {isBare(pathname) ? <main id="main" className="bare">{children}</main> : <Shell>{children}</Shell>}
    </AppProvider>
  );
}
