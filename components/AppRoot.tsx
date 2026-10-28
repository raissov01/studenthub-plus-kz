'use client';
import type { ReactNode } from 'react';
import { AppProvider } from '@/components/app';
import { Shell } from '@/components/Shell';
import type { Content } from '@/lib/types';

export function AppRoot({ content, children }: { content: Content; children: ReactNode }) {
  return (
    <AppProvider content={content}>
      <Shell>{children}</Shell>
    </AppProvider>
  );
}
