'use client';
import { useEffect } from 'react';
import { startClientErrorReporter } from '@/lib/client-errors';

export function ClientErrorReporter() {
  useEffect(startClientErrorReporter, []);
  return null;
}
