'use client';
import type { ReactNode } from 'react';
import { usePathname } from 'next/navigation';
import { ClientErrorReporter } from '@/components/ClientErrorReporter';
import { AppProvider } from '@/components/app';
import { Shell } from '@/components/Shell';
import { PwaRegister } from '@/components/Pwa';

/** pages shown to people outside the app (the public portfolio /p/<slug>): server-rendered, no app chrome, no onboarding */
const isBare = (path: string | null) => !!path && /^\/p\//.test(path);

export function AppRoot({ children }: { children: ReactNode }) {
  const pathname = usePathname();
  return (
    <AppProvider>
      <PwaRegister />
      <ClientErrorReporter />
      {isBare(pathname) ? <main id="main" className="bare">{children}</main> : <Shell>{children}</Shell>}
    </AppProvider>
  );
}
