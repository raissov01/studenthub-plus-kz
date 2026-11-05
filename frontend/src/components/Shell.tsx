'use client';
/* Page frame: sticky header (back / title / bell or gear), the screen, the four-tab bar, the bottom sheet and the toast.
   Screens render only after hydration: they depend on the device's data, date and clock, which the server cannot know. */
import { useCallback, useEffect, useState, type ReactNode } from 'react';
import { usePathname, useRouter } from 'next/navigation';
import { useApp } from '@/components/app';
import { Logo } from '@/components/Logo';
import { Boot } from '@/components/Boot';
import { NavDrawer, OPEN_MENU } from '@/components/NavDrawer';
import { ConsentSheet, SignInSheet } from '@/components/views/account';
import { Icon } from '@/components/ui';
import { Onboarding } from '@/components/views/profile';
import { hasAccount } from '@/components/views/common';
import { useInboxPoll } from '@/components/views/inbox';
import { useAccount } from '@/lib/account';
import { badgeText } from '@/lib/inbox';
import { OfflineBanner, UpdateBar } from '@/components/Pwa';
import { unreadCount } from '@/lib/domain';
import { IC, type IconName } from '@/lib/icons';
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

  // the four tabs are fetched ahead, so a tap shows the next screen (or its loading state) at once
  const router = useRouter();
  useEffect(() => {
    if (!hydrated) return;
    const id = setTimeout(() => { ['/', '/search', '/calendar', '/profile'].forEach(h => router.prefetch(h)); }, 1500);
    return () => clearTimeout(id);
  }, [hydrated, router]);

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
    r.lang = S.lang;
    if (tg.app) return;
    if (S.theme === 'light' || S.theme === 'dark') r.setAttribute('data-theme', S.theme);
    else r.removeAttribute('data-theme');
  }, [S.lang, S.theme, tg.app]);
  useEffect(() => {
    // the prerendered <title> is Kazakh and Next may put it back after hydration (streamed metadata): keep ours
    const want = name === 'home' ? 'Student Hub' : `${meta.title} — Student Hub`;
    const apply = () => { if (document.title !== want) document.title = want; };
    apply();
    const watch = new MutationObserver(apply);
    watch.observe(document.head, { subtree: true, childList: true, characterData: true });
    return () => watch.disconnect();
  }, [name, meta.title]);

  // header border once the page scrolls
  const [scrolled, setScrolled] = useState(false);
  useEffect(() => {
    const on = () => setScrolled(window.scrollY > 4);
    on();
    window.addEventListener('scroll', on, { passive: true });
    return () => window.removeEventListener('scroll', on);
  }, []);

  // the hamburger menu (also opened by the home «Барлығы» tile)
  const [menu, setMenu] = useState(false);
  const closeMenu = useCallback(() => setMenu(false), []);
  useEffect(() => {
    const on = () => setMenu(true);
    window.addEventListener(OPEN_MENU, on);
    return () => window.removeEventListener(OPEN_MENU, on);
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
  // notifications moved to the header bell; the third tab is the deadline calendar
  const tabs: [string, IconName, string][] = [
    ['home', 'home', tt('Басты бет', 'Главная')], ['search', 'search', tt('Іздеу', 'Поиск')],
    ['calendar', 'calendar', tt('Күнтізбе', 'Календарь')], ['profile', 'user', tt('Профиль', 'Профиль')],
  ];

  return (
    <>
      <header className={`top${scrolled ? ' scrolled' : ''}`} id="top">
        <div className="top-row">
          <button className="icon-btn" id="back" type="button" onClick={back} aria-label={tt('Артқа', 'Назад')} hidden={root || !!tg.app}>
            <svg viewBox="0 0 24 24" width="24" height="24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d="M15 5l-7 7 7 7" /></svg>
          </button>
          <button className="icon-btn" id="menu-btn" type="button" hidden={!root} onClick={() => setMenu(true)}
            aria-label={tt('Мәзір', 'Меню')} aria-haspopup="dialog" aria-expanded={menu} aria-controls="nav-drawer">
            <svg viewBox="0 0 24 24" width="24" height="24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden="true"><path d="M4 7h16M4 12h16M4 17h16" /></svg>
          </button>
          <div className="top-title" id="title">
            {name === 'home'
              ? <span className="brand"><Logo />Student Hub</span>
              : searchScreen ? screenMeta('search', { I, S, ui }).title : meta.title}
          </div>
          {hydrated && acc.loaded && !hasAccount(acc) && root ? (
            <button className="btn top-signin" id="signin-btn" type="button" onClick={() => openSheet(acc.user ? <ConsentSheet /> : <SignInSheet />)}>
              {tt('Тіркелу', 'Вход')}
            </button>
          ) : null}
          <button className="icon-btn" id="mine-btn" type="button" hidden={!root}
            onClick={() => go(onProfile ? 'settings' : 'notif')}
            aria-label={onProfile ? tt('Параметрлер', 'Параметры') : badge ? tt(`Хабарламалар, ${unread} оқылмаған`, `Уведомления, ${unread} непрочитанных`) : tt('Хабарламалар', 'Уведомления')}>
            <svg viewBox="0 0 24 24" width="23" height="23" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"
              dangerouslySetInnerHTML={{ __html: onProfile ? IC.gear : IC.bell }} />
            <span className="count" id="mine-count" aria-hidden="true" hidden={onProfile || !badge}>{badge}</span>
          </button>
        </div>
      </header>
      <main id="main" className={/^\/quiz\/[^/]+\/host$/.test(pathname) ? 'quiz-host-main' : undefined} aria-live={name === 'quiz' ? undefined : 'polite'}>{hydrated ? <><OfflineBanner />{children}</> : null}</main>
      {hydrated ? <Boot root={root} /> : null}
      <nav className="tabbar" aria-label="Student Hub">
        <div className="tabbar-in">
          {tabs.map(([k, i, l]) => (
            <button key={k} className="tab" type="button" aria-current={k === tabRoot ? 'page' : undefined} onClick={() => go(k)}>
              <span className="pill"><Icon n={i} s={22} /></span><span>{l}</span>
            </button>
          ))}
        </div>
      </nav>
      {sheet ? (
        <div className="sheet-back" onClick={e => { if (e.target === e.currentTarget) closeSheet(); }}>
          <div className="sheet" role="dialog" aria-modal="true"><div className="grab"></div>{sheet}</div>
        </div>
      ) : null}
      <NavDrawer open={menu} onClose={closeMenu} />
      <UpdateBar />
      {toastMsg ? (
        <div className={`toast${toastMsg.action ? ' toast-act' : ''}`} role="status" key={toastMsg.key}>
          <span>{toastMsg.text}</span>
          {toastMsg.action ? <button type="button" className="toast-btn" onClick={toastMsg.action.run}>{toastMsg.action.label}</button> : null}
        </div>
      ) : null}
    </>
  );
}
