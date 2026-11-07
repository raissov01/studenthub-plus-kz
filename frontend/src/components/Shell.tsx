'use client';
/* Page frame: sticky header (back / title / bell or gear), the screen, the four-tab bar, the bottom sheet and the toast.
   Screens render only after hydration: they depend on the device's data, date and clock, which the server cannot know. */
import { useCallback, useEffect, useRef, useState, type KeyboardEvent as ReactKeyboardEvent, type ReactNode } from 'react';
import { usePathname, useRouter } from 'next/navigation';
import { useApp } from '@/components/app';
import { Logo } from '@/components/Logo';
import { Boot } from '@/components/Boot';
import { NavDrawer, OPEN_MENU } from '@/components/NavDrawer';
import { SideNav } from '@/components/SideNav';
import { useDesktop } from '@/lib/desktop';
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
import { getState, useHydrated } from '@/lib/store';
import { useTelegram } from '@/lib/telegram';

const TAB_OF: Record<string, string> = { opps: 'search', bonus: 'search', ai: 'home' };

export function Shell({ children }: { children: ReactNode }) {
  const app = useApp();
  const { S, ui, I, tt, go, back, sheet, toastMsg, closeSheet, content, openSheet, setActiveTab, fromOf, setUi } = app;
  const hydrated = useHydrated();
  const tg = useTelegram();
  const pathname = usePathname();
  const v = viewOfPath(pathname) ?? 'home';
  const meta = screenMeta(v, { I, S, ui });
  const name = meta.name;
  // the sections (/jobs, /internships, /events, /courses, /discounts): reached from the menu, so the menu button
  // stays and the «Іздеу» tab lights up; the header names the section
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

  // the header title is the page's <h1>, unless the screen has its own (a job, an event, a club…): one h1 per page
  const [pageH1, setPageH1] = useState(false);
  useEffect(() => {
    const m = document.getElementById('main');
    if (!m) return;
    const on = () => setPageH1(!!m.querySelector('h1'));
    on();
    const watch = new MutationObserver(on);
    watch.observe(m, { subtree: true, childList: true });
    return () => watch.disconnect();
  }, []);

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

  // first visit: the onboarding sheet, only on the home screen (never over a deep link) and only once a load;
  // it can be closed or put off («Кейінірек»), and that counts as seen, so it never comes back on its own
  const asked = useRef(false);
  useEffect(() => {
    if (!hydrated || S.onboarded || asked.current || name !== 'home') return;
    const id = setTimeout(() => { asked.current = true; if (!getState().onboarded && !document.querySelector('.sheet')) openSheet(<Onboarding />); }, 300);
    return () => clearTimeout(id);
    // later resets open it themselves
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [hydrated, name]);

  // laptop: «/» focuses the screen's search box (or opens «Іздеу»), like most web apps
  const desktop = useDesktop();
  useEffect(() => {
    const on = (e: KeyboardEvent) => {
      if (e.key !== '/' || e.metaKey || e.ctrlKey || e.altKey || sheet) return;
      const t = e.target as HTMLElement | null;
      if (t && (t.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(t.tagName))) return;
      const box = document.querySelector<HTMLInputElement>('#sq, #hq');
      e.preventDefault();
      if (box?.id === 'sq') { box.focus(); return; }
      setUi({ sqFocus: true });
      go('search');
    };
    window.addEventListener('keydown', on);
    return () => window.removeEventListener('keydown', on);
  }, [sheet, go, setUi]);

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
      <a className="skip-link" href="#main" onClick={e => { e.preventDefault(); document.getElementById('main')?.focus(); }}>{tt('Негізгі мазмұнға өту', 'Перейти к содержимому')}</a>
      <header className={`top${scrolled ? ' scrolled' : ''}`} id="top">
        <div className="top-row">
          <button className="icon-btn" id="back" type="button" onClick={back} aria-label={tt('Артқа', 'Назад')} hidden={root || !!tg.app}>
            <Icon n="back" s={24} />
          </button>
          <button className="icon-btn" id="menu-btn" type="button" hidden={!root} onClick={() => setMenu(true)}
            aria-label={tt('Мәзір', 'Меню')} aria-haspopup="dialog" aria-expanded={menu} aria-controls="nav-drawer">
            <Icon n="menu" s={24} />
          </button>
          {(() => {
            const Title = pageH1 ? 'div' : 'h1';
            return (
              <Title className="top-title" id="title">
                {name === 'home'
                  ? desktop && hydrated ? tt('Басты бет', 'Главная') : <span className="brand"><Logo />Student Hub</span>
                  : meta.title}
              </Title>
            );
          })()}
          {/* signed out: one «Кіру» (it opens the sign-in sheet); on a laptop it lives in the sidebar instead */}
          {hydrated && acc.loaded && !hasAccount(acc) && root && !desktop ? (
            <button className="btn top-signin" id="signin-btn" type="button" onClick={() => openSheet(acc.user ? <ConsentSheet /> : <SignInSheet />)}>
              {tt('Кіру', 'Вход')}
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
      <aside className="sidenav" aria-label={tt('Бөлімдер', 'Разделы')}>
        {hydrated && desktop ? (
          <SideNav v={v} tabRoot={tabRoot} unread={unread} badge={badge}
            onSignIn={acc.loaded && !hasAccount(acc) ? () => openSheet(acc.user ? <ConsentSheet /> : <SignInSheet />) : undefined} />
        ) : null}
      </aside>
      <main id="main" tabIndex={-1} data-screen={name} className={/^\/quiz\/[^/]+\/host$/.test(pathname) ? 'quiz-host-main' : undefined} aria-live={name === 'quiz' ? undefined : 'polite'}>{hydrated ? <><OfflineBanner />{children}</> : null}</main>
      {hydrated ? <Boot root={root} /> : null}
      <nav className="tabbar" aria-label="Student Hub">
        <div className="tabbar-in">
          {tabs.map(([k, i, l]) => (
            <button key={k} className="tab" type="button" aria-current={k === tabRoot && (k !== 'home' || name === 'home') ? 'page' : undefined} onClick={() => go(k)}>
              <span className="pill"><Icon n={i} s={22} /></span><span>{l}</span>
            </button>
          ))}
        </div>
      </nav>
      {sheet ? <SheetDialog onClose={closeSheet} closeLabel={tt('Жабу', 'Закрыть')} fallbackLabel={tt('Терезе', 'Окно')}>{sheet}</SheetDialog> : null}
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

const CLOSE_NAMES = new Set(['Жабу', 'Закрыть', 'Close']);
const FOCUSABLE = 'a[href], button:not([disabled]), input:not([disabled]):not([type="hidden"]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

/** the bottom sheet (a centred dialog on a laptop): named by its first heading, focus moves in on open, stays inside
    while it is open (Tab wraps) and goes back to whatever opened it on close; a 44px close button in the corner */
function SheetDialog({ children, onClose, closeLabel, fallbackLabel }: { children: ReactNode; onClose: () => void; closeLabel: string; fallbackLabel: string }) {
  const box = useRef<HTMLDivElement>(null);
  const [labelId, setLabelId] = useState<string | undefined>(undefined);
  const [ownClose, setOwnClose] = useState(false);
  // focus in once, back out on unmount
  useEffect(() => {
    const opener = document.activeElement as HTMLElement | null;
    const el = box.current;
    if (el) {
      const first = [...el.querySelectorAll<HTMLElement>(FOCUSABLE)].find(x => !x.classList.contains('sheet-x') && x.offsetParent !== null);
      (first && !/^(INPUT|TEXTAREA|SELECT)$/.test(first.tagName) ? first : el).focus({ preventScroll: true });
    }
    return () => { if (opener && document.contains(opener)) opener.focus({ preventScroll: true }); };
  }, []);
  // the accessible name follows the content (sheets replace each other in place)
  useEffect(() => {
    // a sheet that has its own «Жабу» button keeps it; the corner one would only repeat it
    const own = [...(box.current?.querySelectorAll<HTMLElement>('button:not(.sheet-x)') ?? [])]
      .some(b => CLOSE_NAMES.has((b.getAttribute('aria-label') || b.textContent || '').trim()));
    setOwnClose(own);
    const h = box.current?.querySelector<HTMLElement>('h1, h2, h3');
    if (!h) { setLabelId(undefined); return; }
    if (!h.id) h.id = 'sheet-title';
    setLabelId(h.id);
  }, [children]);
  const onKey = (e: ReactKeyboardEvent<HTMLDivElement>) => {
    if (e.key !== 'Tab' || !box.current) return;
    const items = [...box.current.querySelectorAll<HTMLElement>(FOCUSABLE)].filter(x => x.offsetParent !== null);
    if (!items.length) return;
    const first = items[0], last = items[items.length - 1];
    if (e.shiftKey && (document.activeElement === first || document.activeElement === box.current)) { e.preventDefault(); last.focus(); }
    else if (!e.shiftKey && document.activeElement === last) { e.preventDefault(); first.focus(); }
  };
  return (
    <div className="sheet-back" onClick={e => { if (e.target === e.currentTarget) onClose(); }}>
      <div className="sheet" role="dialog" aria-modal="true" aria-labelledby={labelId} aria-label={labelId ? undefined : fallbackLabel}
        ref={box} tabIndex={-1} onKeyDown={onKey}>
        <div className="grab"></div>
        {ownClose ? null : <button type="button" className="icon-btn sheet-x" onClick={onClose} aria-label={closeLabel}><Icon n="x" s={22} /></button>}
        {children}
      </div>
    </div>
  );
}
