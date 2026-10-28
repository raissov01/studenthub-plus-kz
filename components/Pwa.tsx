'use client';
/* Offline support UI (docs/PWA.md): service worker registration, the «no internet» line on top of every screen and
   the «new version ready» bar. The page is never reloaded by itself; only the student's tap applies an update. */
import { useEffect } from 'react';
import { useApp } from '@/components/app';
import { acceptUpdate, registerServiceWorker, useOnline, useUpdateReady } from '@/lib/pwa';

export function PwaRegister() {
  useEffect(() => {
    // after the page has loaded, so the worker's own downloads never compete with the first paint
    if (document.readyState === 'complete') { registerServiceWorker(); return; }
    window.addEventListener('load', registerServiceWorker, { once: true });
    return () => window.removeEventListener('load', registerServiceWorker);
  }, []);
  return null;
}

export function OfflineBanner() {
  const { tt } = useApp();
  const online = useOnline();
  if (online) return null;
  return (
    <div className="offline-bar" role="status">
      <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
        <path d="M2 2l20 20M8.5 16.4a5 5 0 0 1 7 0M5 12.9a10 10 0 0 1 5.2-2.7M19 12.9a10 10 0 0 0-2.3-1.6M1.4 9a15 15 0 0 1 4.4-2.7M10.7 5.1A15 15 0 0 1 22.6 9M12 20h.01" />
      </svg>
      <span>{tt('Интернет жоқ — сақталған деректер көрсетілуде', 'Нет интернета — показаны сохранённые данные')}</span>
    </div>
  );
}

export function UpdateBar() {
  const { tt } = useApp();
  const ready = useUpdateReady();
  if (!ready) return null;
  return (
    <div className="update-bar" role="status">
      <span>{tt('Жаңа нұсқа дайын', 'Доступна новая версия')}</span>
      <button className="update-btn" type="button" onClick={acceptUpdate}>{tt('Жаңарту', 'Обновить')}</button>
    </div>
  );
}
