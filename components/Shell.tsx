'use client';
/* Page frame: sticky header (back / title / bell or gear), the screen, the four-tab bar, the bottom sheet and the toast.
   Screens render only after hydration: they depend on the device's data, date and clock, which the server cannot know. */
import { useEffect, useState, type ReactNode } from 'react';
import { usePathname } from 'next/navigation';
import { useApp } from '@/components/app';
import { Boot } from '@/components/Boot';
import { Icon } from '@/components/ui';
import { Onboarding } from '@/components/views/profile';
import { hasAccount } from '@/components/views/common';
import { useInboxPoll } from '@/components/views/inbox';
import { useAccount } from '@/lib/account';
import { badgeText } from '@/lib/inbox';
import { OfflineBanner, UpdateBar } from '@/components/Pwa';
import { unreadCount } from '@/lib/domain';
import { IC } from '@/lib/icons';
import { CHECKIN_KEY } from '@/lib/events';
import { ROOTS, viewOfLegacyHash, viewOfPath } from '@/lib/routes';
import { screenMeta } from '@/lib/screens';
import { useHydrated } from '@/lib/store';
import { useTelegram } from '@/lib/telegram';

const TAB_OF: Record<string, string> = { opps: 'search', bonus: 'search', ai: 'home' };

export function Shell({ children }: { children: ReactNode }) {
  const app = useApp();
  const { S, ui, I, tt, go, back, sheet, toastMsg, closeSheet, content, openSheet, setActiveTab, fromOf } = app;
  const hydrated = useHydrated();
  const tg = useTelegram();
  const pathname = usePathname();
  const v = viewOfPath(pathname) ?? 'home';
  const meta = screenMeta(v, { I, S, ui });
  const name = meta.name;
  // /jobs, /internships, /events, /courses, /discounts are the «Іздеу» screen with a category: same header and tab
  const searchScreen = name === 'opps' || name === 'bonus';
  const root = ROOTS.includes(name) || searchScreen;

  // the tab that lights up: fixed for root/profile/home screens, otherwise the tab the student came from
  const from = hydrated ? fromOf(pathname) : '';
  const fixed = searchScreen ? 'search' : root ? name : (meta.tab === 'profile' || meta.tab === 'home') ? meta.tab : '';
  const tabRoot = fixed || (ROOTS.includes(from) && from) || TAB_OF[meta.tab] || meta.tab || TAB_OF[name] || '';
  useEffect(() => { setActiveTab(tabRoot); }, [tabRoot, setActiveTab]);

  // old #/job/j1 links land on the real path (on load, and if such a link is followed inside the page)
  useEffect(() => {
    const redirect = () => {
      if (/^#tgWebApp/.test(window.location.hash)) return; // Telegram's launch parameters, not an old link
      // QR check-in link outside Telegram: SITE_URL/#/checkin?t=ci_… (the token rides along to /checkin)
      const ci = /^#\/?checkin\?(?:.*&)?t=(ci_[A-Za-z0-9_-]+)/.exec(window.location.hash);
      if (ci) { try { window.sessionStorage.setItem(CHECKIN_KEY, ci[1]); } catch { /* ignore */ } }
      const hv = viewOfLegacyHash(window.location.hash);
      if (hv) go(hv, { replace: true });
    };
    redirect();
    window.addEventListener('hashchange', redirect);
    return () => window.removeEventListener('hashchange', redirect);
  }, [go]);

  // language, theme and tab title follow the state
  // inside Telegram the theme follows Telegram (features/telegram applies it)
  useEffect(() => {
    const r = document.documentElement;
    r.lang = S.lang === 'ru' ? 'ru' : 'kk';
    if (tg.app) return;
    if (S.theme === 'light' || S.theme === 'dark') r.setAttribute('data-theme', S.theme);
    else r.removeAttribute('data-theme');
  }, [S.lang, S.theme, tg.app]);
  useEffect(() => {
    document.title = name === 'home' ? 'Student Hub' : `${meta.title} — Student Hub`;
  }, [name, meta.title]);

  // header border once the page scrolls
  const [scrolled, setScrolled] = useState(false);
  useEffect(() => {
    const on = () => setScrolled(window.scrollY > 4);
    on();
    window.addEventListener('scroll', on, { passive: true });
    return () => window.removeEventListener('scroll', on);
  }, []);

  // first visit: onboarding sheet
  useEffect(() => {
    if (!hydrated || S.onboarded) return;
    const id = setTimeout(() => openSheet(<Onboarding />), 300);
    return () => clearTimeout(id);
    // only on first load; later resets open it themselves
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [hydrated]);

  // Escape closes a sheet
  useEffect(() => {
    if (!sheet) return;
    const on = (e: KeyboardEvent) => { if (e.key === 'Escape') closeSheet(); };
    window.addEventListener('keydown', on);
    return () => window.removeEventListener('keydown', on);
  }, [sheet, closeSheet]);

  // badge: the device's own unread notices + the account inbox (GET /api/me inboxUnread), kept fresh by the poll
  const acc = useAccount();
  useInboxPoll();
  const unread = hydrated ? unreadCount(S, I, content) + (hasAccount(acc) ? acc.inboxUnread : 0) : 0;
  const badge = badgeText(unread);
  const onProfile = name === 'profile';
  const tabs: [string, 'home' | 'search' | 'bell' | 'user', string][] = [
    ['home', 'home', tt('Басты бет', 'Главная')], ['search', 'search', tt('Іздеу', 'Поиск')],
    ['notif', 'bell', tt('Хабарлар', 'Уведомления')], ['profile', 'user', tt('Профиль', 'Профиль')],
  ];

  return (
    <>
      <header className={`top${scrolled ? ' scrolled' : ''}`} id="top">
        <div className="top-row">
          <button className="icon-btn" id="back" type="button" onClick={back} aria-label={tt('Артқа', 'Назад')} hidden={root || !!tg.app}>
            <svg viewBox="0 0 24 24" width="24" height="24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d="M15 5l-7 7 7 7" /></svg>
          </button>
          <div className="top-title" id="title">
            {name === 'home'
              ? <span className="brand"><i className="mark" aria-hidden="true"><b></b><b></b><b></b><b></b><b></b></i>Student Hub</span>
              : searchScreen ? screenMeta('search', { I, S, ui }).title : meta.title}
          </div>
          <button className="icon-btn" id="mine-btn" type="button" hidden={!(name === 'home' || onProfile)}
            onClick={() => go(onProfile ? 'settings' : 'notif')}
            aria-label={onProfile ? tt('Параметрлер', 'Параметры') : tt('Хабарламалар', 'Уведомления')}>
            <svg viewBox="0 0 24 24" width="23" height="23" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"
              dangerouslySetInnerHTML={{ __html: onProfile ? IC.gear : IC.bell }} />
            <span className="count" id="mine-count" hidden={onProfile || !badge}>{badge}</span>
          </button>
        </div>
      </header>
      <main id="main" className={/^\/quiz\/[^/]+\/host$/.test(pathname) ? 'quiz-host-main' : undefined} aria-live={name === 'quiz' ? undefined : 'polite'}>{hydrated ? <><OfflineBanner />{children}</> : null}</main>
      {hydrated ? <Boot root={root} /> : null}
      <nav className="tabbar" aria-label="Student Hub">
        <div className="tabbar-in">
          {tabs.map(([k, i, l]) => (
            <button key={k} className="tab" type="button" aria-current={k === tabRoot ? 'page' : undefined} onClick={() => go(k)}>
              <span className="pill"><Icon n={i} s={22} />{k === 'notif' && badge
                ? <span className="count tab-count" aria-label={tt(`${unread} оқылмаған`, `${unread} непрочитанных`)}>{badge}</span> : null}</span><span>{l}</span>
            </button>
          ))}
        </div>
      </nav>
      {sheet ? (
        <div className="sheet-back" onClick={e => { if (e.target === e.currentTarget) closeSheet(); }}>
          <div className="sheet" role="dialog" aria-modal="true"><div className="grab"></div>{sheet}</div>
        </div>
      ) : null}
      <UpdateBar />
      {toastMsg ? <div className="toast" role="status" key={toastMsg.key}>{toastMsg.text}</div> : null}
    </>
  );
}
