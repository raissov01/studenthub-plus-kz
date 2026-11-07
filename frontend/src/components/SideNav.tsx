'use client';
/* The laptop sidebar (≥1024px, lib/desktop.ts): the four tabs, notifications, every menu section from the same list
   as the phone menu (NavDrawer's useNavGroups), the profile block at the bottom and the admin entry for an admin.
   Collapsible to icons; the choice is remembered on the device. Rendered by the Shell only on a wide window. */
import type { ReactNode } from 'react';
import { useApp } from '@/components/app';
import { Logo } from '@/components/Logo';
import { Icon } from '@/components/ui';
import { useNavGroups } from '@/components/NavDrawer';
import { useAccount } from '@/lib/account';
import { useSideMini } from '@/lib/desktop';
import { parseView } from '@/lib/routes';
import { initialOf } from '@/lib/format';
import { sectionOfView } from '@/lib/sections';
import type { IconName } from '@/lib/icons';

/** the same destination: 'opps:jobs?t=youth' is the «Жұмыс» section */
function sameView(a: string, b: string): boolean {
  const [an, ap] = parseView(a), [bn, bp] = parseView(b);
  if (an !== bn) return false;
  const as = sectionOfView(an, ap), bs = sectionOfView(bn, bp);
  if (as || bs) return as === bs;
  return (ap || '') === (bp || '');
}

export function SideNav({ v, tabRoot, unread, badge, onSignIn }: { v: string; tabRoot: string; unread: number; badge: string; onSignIn?: () => void }) {
  const { S, tt, go } = useApp();
  const groups = useNavGroups();
  const acc = useAccount();
  const [mini, setMini] = useSideMini();
  const tabs: [string, IconName, string][] = [
    ['home', 'home', tt('Басты бет', 'Главная')], ['search', 'search', tt('Іздеу', 'Поиск')],
    ['calendar', 'calendar', tt('Күнтізбе', 'Календарь')],
  ];
  const inTabs = new Set(tabs.map(t => t[0]));
  const inGroup = groups.some(([, items]) => items.some(([to]) => !inTabs.has(to) && sameView(to, v)));
  const name = S.profile.name || acc.user?.name || tt('Студент', 'Студент');
  const initial = initialOf(name, tt('Студент', 'Студент'));
  const here = parseView(v)[0];
  const onProfile = ['profile', 'edit', 'portfolio', 'gpa', 'docs'].includes(here);
  const onNotif = here === 'notif';
  const item = (to: string, icon: IconName, label: string, current: boolean, extra?: ReactNode) => (
    <button key={to} type="button" className="side-item" aria-current={current ? 'page' : undefined} onClick={() => go(to)} title={mini ? label : undefined}>
      <Icon n={icon} s={20} /><span className="side-label">{label}</span>{extra}
    </button>
  );
  return (
    <div className="side-in">
      <div className="side-head">
        <span className="brand side-brand"><Logo /><span className="side-label">Student Hub</span></span>
        <button type="button" className="icon-btn side-toggle" onClick={() => setMini(!mini)} aria-pressed={mini}
          aria-label={mini ? tt('Мәзірді ашу', 'Развернуть меню') : tt('Мәзірді жию', 'Свернуть меню')} title={mini ? tt('Мәзірді ашу', 'Развернуть меню') : tt('Мәзірді жию', 'Свернуть меню')}>
          <Icon n="menu" s={20} />
        </button>
      </div>
      <nav className="side-nav" aria-label="Student Hub">
        <div className="side-group">
          {tabs.map(([k, i, l]) => item(k, i, l, !inGroup && !onNotif && !onProfile && k === tabRoot && (k !== 'home' || here === 'home')))}
          {item('notif', 'bell', tt('Хабарламалар', 'Уведомления'), onNotif,
            badge ? <span className="side-count" aria-label={tt(`${unread} оқылмаған`, `${unread} непрочитанных`)}>{badge}</span> : null)}
        </div>
        {groups.map(([title, items]) => {
          const rest = items.filter(([to]) => !inTabs.has(to));
          if (!rest.length) return null;
          return (
            <div key={title} className="side-group" role="group" aria-label={title}>
              <p className="side-title" aria-hidden="true"><span className="side-label">{title}</span></p>
              {rest.map(([to, icon, label]) => item(to, icon, label, sameView(to, v)))}
            </div>
          );
        })}
      </nav>
      <div className="side-foot">
        <button type="button" className="side-me" aria-current={onProfile ? 'page' : undefined} onClick={() => go('profile')} title={mini ? name : undefined}>
          <span className="side-avatar" aria-hidden="true">{initial}</span>
          <span className="side-label side-me-txt"><b>{name}</b><span>{S.profile.course ? tt(`${S.profile.course}-курс`, `${S.profile.course} курс`) : tt('Профиль', 'Профиль')}</span></span>
        </button>
        {onSignIn ? <button type="button" className="btn ghost side-signin" onClick={onSignIn} title={mini ? tt('Кіру', 'Вход') : undefined}><Icon n="user" s={18} /><span className="side-label">{tt('Кіру', 'Вход')}</span></button> : null}
      </div>
    </div>
  );
}
