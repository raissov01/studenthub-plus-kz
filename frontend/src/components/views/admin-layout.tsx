'use client';
/* The frame of every /admin screen. On a phone it is just the screen (the console home's cards are the menu). From
   1024px it adds a left section list for the person's own rights and Ctrl+K / ⌘K to jump to a section. The list
   is built from the account alone (role, scopes), so it makes no request; the server still checks each screen. */
import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { useApp } from '@/components/app';
import { Icon } from '@/components/ui';
import { useFeature } from '@/components/views/common';
import { canScope, useAccount } from '@/lib/account';
import type { IconName } from '@/lib/icons';

type Item = { view: string; icon: IconName; label: string; more?: boolean };

function useNav(): [string, Item[]][] {
  const { tt } = useApp();
  const acc = useAccount();
  const f = {
    cafe: useFeature('cafe'), print: useFeature('print'), clubs: useFeature('clubs'), feeds: useFeature('feeds'), market: useFeature('market'),
    points: useFeature('points'), b2b: useFeature('b2b'), amb: useFeature('ambassador'), mentors: useFeature('mentors'), events: useFeature('events'),
  };
  const surveys = useFeature('surveys'), feedback = useFeature('feedback'), wellbeing = useFeature('wellbeing');
  const voice = surveys || feedback || wellbeing;
  const u = acc.user;
  const admin = u?.role === 'admin', mod = canScope(u, 'moderator');
  const groups: [string, Item[]][] = [[tt('Басқару', 'Управление'), [{ view: 'admin', icon: 'home', label: admin ? tt('Басқару басты беті', 'Главная управления') : tt('Менің панельдерім', 'Мои панели') }]]];
  if (admin) {
    groups[0][1].push(
      { view: 'admin:users', icon: 'user', label: tt('Қолданушылар және рөлдер', 'Пользователи и роли') },
      ...(f.cafe || f.print ? [{ view: 'admin:payments', icon: 'tenge' as IconName, label: tt('Төлемдер', 'Платежи') }] : []),
      { view: 'admin:settings', icon: 'gear' as IconName, label: tt('Баптаулар', 'Настройки'), more: true },
      { view: 'admin:roles', icon: 'shield', label: tt('Кім нені басқарады', 'Кто чем управляет'), more: true },
      { view: 'admin:audit', icon: 'clock', label: tt('Өзгерістер тарихы', 'История изменений'), more: true },
      { view: 'admin:exports', icon: 'doc', label: tt('Excel-ге жүктеу', 'Скачать для Excel'), more: true },
      { view: 'admin:health', icon: 'bolt', label: tt('Сайттың жұмысы', 'Работа сайта'), more: true },
    );
  }
  if (f.events && (admin || u?.role === 'ambassador' || u?.role === 'university_staff')) {
    groups.push([tt('Іс-шаралар', 'Мероприятия'), [
      { view: 'admin:schedule', icon: 'calendar' as IconName, label: tt('Күнтізбе', 'Календарь') },
      { view: 'admin:attendance', icon: 'check' as IconName, label: tt('Кім келді', 'Кто пришёл') },
    ]]);
  }
  if (mod) {
    groups.push([tt('Модерация', 'Модерация'), [
      ...(admin ? [] : [{ view: 'admin:moderation', icon: 'shield' as IconName, label: tt('Жұмыс орны', 'Рабочее место') }]),
      { view: 'admin:queue', icon: 'check', label: tt('Тексеруді күтетіндер', 'Ждут проверки') },
      { view: 'admin:reports', icon: 'bell', label: tt('Шағымдар', 'Жалобы') },
      { view: 'admin:content', icon: 'file', label: tt('Жазбалар', 'Записи') },
      ...(f.feeds ? [{ view: 'admin:feeds', icon: 'spark' as IconName, label: tt('Мүмкіндіктер ағыны', 'Лента возможностей'), more: true }] : []),
      ...(f.market ? [{ view: 'admin:market', icon: 'gift' as IconName, label: tt('Тегін заттар', 'Отдам даром'), more: true }] : []),
    ]]);
  }
  if (admin) {
    groups.push([tt('Бөлімдер', 'Разделы'), [
      ...(voice ? [{ more: true, view: 'admin:voice', icon: 'chat' as IconName, label: tt('Студент дауысы', 'Голос студентов') }] : []),
      ...(f.cafe ? [{ view: 'admin:cafe', icon: 'cup' as IconName, label: tt('Кофеханалар', 'Кофейни') }] : []),
      ...(f.print ? [{ view: 'admin:print', icon: 'file' as IconName, label: tt('Полиграфиялар', 'Полиграфии') }] : []),
      ...(f.clubs ? [{ view: 'admin:clubs', icon: 'user' as IconName, label: tt('Клубтар', 'Клубы') }] : []),
      ...(f.b2b ? [{ more: true, view: 'admin:employers', icon: 'briefcase' as IconName, label: tt('Жұмыс берушілер', 'Работодатели') },
        { more: true, view: 'admin:staff', icon: 'cap' as IconName, label: tt('Университет қызметкерлері', 'Сотрудники университетов') }] : []),
      ...(f.amb ? [{ more: true, view: 'admin:ambassadors', icon: 'award' as IconName, label: tt('Амбассадорлар', 'Амбассадоры') }] : []),
      ...(f.mentors ? [{ more: true, view: 'admin:mentors', icon: 'cap' as IconName, label: tt('Менторларды жұптау', 'Подбор менторов') }] : []),
      ...(f.points ? [{ more: true, view: 'admin:points', icon: 'trophy' as IconName, label: tt('Ұпайлар мен сыйлықтар', 'Баллы и награды') }] : []),
    ]]);
  }
  return groups;
}

/** Ctrl+K / ⌘K: type a section name, Enter to open */
function Palette({ items, onClose }: { items: Item[]; onClose: () => void }) {
  const { tt, go } = useApp();
  const [q, setQ] = useState('');
  const [i, setI] = useState(0);
  const list = useMemo(() => items.filter(x => x.label.toLowerCase().includes(q.trim().toLowerCase())), [items, q]);
  const pick = (x: Item | undefined) => { if (x) { onClose(); go(x.view); } };
  return (
    <div className="adm-palette-back" onClick={e => { if (e.target === e.currentTarget) onClose(); }}>
      <div className="adm-palette" role="dialog" aria-modal="true" aria-label={tt('Бөлімге өту', 'Перейти к разделу')}>
        <input className="input" autoFocus aria-label={tt('Бөлім атауы', 'Название раздела')} placeholder={tt('Бөлім атауы…', 'Название раздела…')} value={q}
          onChange={e => { setQ(e.target.value); setI(0); }}
          onKeyDown={e => {
            if (e.key === 'Escape') onClose();
            else if (e.key === 'ArrowDown') { e.preventDefault(); setI(n => Math.min(n + 1, list.length - 1)); }
            else if (e.key === 'ArrowUp') { e.preventDefault(); setI(n => Math.max(n - 1, 0)); }
            else if (e.key === 'Enter') { e.preventDefault(); pick(list[i]); }
          }} />
        <ul role="listbox" aria-label={tt('Бөлімдер', 'Разделы')}>
          {list.map((x, n) => (
            <li key={x.view} role="option" aria-selected={n === i}>
              <button type="button" onClick={() => pick(x)} onMouseEnter={() => setI(n)}><Icon n={x.icon} s={18} /> {x.label}</button>
            </li>
          ))}
          {!list.length ? <li className="small muted">{tt('Табылмады', 'Не найдено')}</li> : null}
        </ul>
        <p className="tiny muted">↑ ↓ · Enter · Esc</p>
      </div>
    </div>
  );
}

function NavItem({ x, current }: { x: Item; current: boolean }) {
  const { go } = useApp();
  return (
    <button type="button" className="adm-nav-item" aria-current={current ? 'page' : undefined} onClick={() => go(x.view)}>
      <Icon n={x.icon} s={18} /> <span>{x.label}</span>
    </button>
  );
}

export function AdminLayout({ view, children }: { view: string; children: ReactNode }) {
  const { tt, go } = useApp();
  const acc = useAccount();
  const groups = useNav();
  const [palette, setPalette] = useState(false);
  const items = groups.flatMap(([, x]) => x);
  const signed = !!acc.user;
  const open = useRef(setPalette);
  useEffect(() => {
    if (!signed) return;
    const onKey = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'k' && window.matchMedia('(min-width: 1024px)').matches) { e.preventDefault(); open.current(p => !p); }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [signed]);
  const base = view.replace(/\?.*$/, '');
  return (
    <div className="adm-layout">
      {signed && items.length > 1 ? (
        <nav className="adm-nav" aria-label={tt('Басқару бөлімдері', 'Разделы управления')}>
          <button className="adm-k" type="button" onClick={() => setPalette(true)}><Icon n="search" s={16} /> {tt('Бөлімге өту', 'Перейти к разделу')} <kbd>Ctrl K</kbd></button>
          {groups.filter(([, x]) => x.length).map(([title, list]) => (
            <div key={title} className="adm-nav-group">
              <span className="adm-nav-title">{title}</span>
              {list.filter(x => !x.more).map(x => <NavItem key={x.view} x={x} current={base === x.view} />)}
              {list.some(x => x.more) ? (
                <details className="adm-nav-more" open={list.some(x => x.more && base === x.view)}>
                  <summary className="adm-nav-item"><Icon n="chev" s={18} /> <span>{tt('Тағы', 'Ещё')}</span></summary>
                  {list.filter(x => x.more).map(x => <NavItem key={x.view} x={x} current={base === x.view} />)}
                </details>
              ) : null}
            </div>
          ))}
        </nav>
      ) : null}
      <div className="adm-main">{children}</div>
      {palette ? <Palette items={items} onClose={() => setPalette(false)} /> : null}
    </div>
  );
}
