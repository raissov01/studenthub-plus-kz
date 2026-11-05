'use client';
/* The hamburger menu: every section in four groups, so the home screen does not have to show them all.
   Opens from the header on the root screens and from the home «Барлығы» tile (window event OPEN_MENU). */
import { useEffect, useRef } from 'react';
import { useApp } from '@/components/app';
import { Logo } from '@/components/Logo';
import { Icon } from '@/components/ui';
import { useConfig } from '@/lib/config';
import { useAccount } from '@/lib/account';
import { SECTIONS, sectionOfView, visibleTabs, type SectionId } from '@/lib/sections';
import type { IconName } from '@/lib/types';

export const OPEN_MENU = 'studenthub:menu';
export const openMenu = () => window.dispatchEvent(new Event(OPEN_MENU));

type Item = [string, IconName, string];
const SECTION_MENU: [SectionId, IconName][] = [['jobs', 'briefcase'], ['intern', 'globe'], ['events', 'calendar'], ['courses', 'award'], ['deals', 'tag']];

export function NavDrawer({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { tt, go, content } = useApp();
  const cfg = useConfig();
  const acc = useAccount();
  const role = acc.user?.role;
  const hasConsole = !!acc.user && (role === 'admin' || !!acc.console || !!acc.user.scopes?.length);
  const panel = useRef<HTMLDivElement>(null);
  const opener = useRef<Element | null>(null);

  // focus moves into the menu and back to whatever opened it; Escape closes
  useEffect(() => {
    if (!open) return;
    opener.current = document.activeElement;
    panel.current?.querySelector<HTMLElement>('button')?.focus();
    const on = (e: KeyboardEvent) => { if (e.key === 'Escape') onClose(); };
    window.addEventListener('keydown', on);
    document.body.style.overflow = 'hidden';
    return () => {
      window.removeEventListener('keydown', on);
      document.body.style.overflow = '';
      (opener.current as HTMLElement | null)?.focus?.();
    };
  }, [open, onClose]);

  if (!open) return null;
  const clubs = !!(cfg.features as Partial<Record<string, boolean>>).clubs;
  const groups: [string, Item[]][] = [
    // the sections, each with its own sub-tabs (lib/sections.ts), listed under the name
    [tt('Мүмкіндіктер', 'Возможности'), SECTION_MENU.map(([id, icon]): Item => [SECTIONS[id].view, icon, tt(...SECTIONS[id].label)])],
    [tt('Оқу', 'Учёба'), [
      ['materials', 'book', tt('Оқу материалдары', 'Учебные материалы')],
      ['ai', 'spark', tt('AI көмекші', 'AI-помощник')],
      ...(cfg.features.quiz ? [['quiz', 'cards', tt('Квиздер', 'Квизы')] as Item] : []),
      ['roadmap', 'target', tt('Даму жоспары', 'План развития')],
      ['timetable', 'clock', tt('Сабақ кестесі', 'Расписание занятий')],
      ...(cfg.features.courseqa ? [['qa', 'chat', tt('Сұрақ-жауап', 'Вопросы и ответы')] as Item] : []),
    ]],
    [tt('Университет', 'Университет'), [
      ['services', 'cap', tt('Университет қызметтері', 'Услуги университета')],
      ['map', 'map', tt('Кампус картасы', 'Карта кампуса')],
      ...(cfg.features.cafe ? [['cafe', 'cup', tt('Кофе / Кофейня', 'Кофе / Кофейня')] as Item] : []),
      ...(cfg.features.print ? [['print', 'file', tt('Полиграфия', 'Полиграфия')] as Item] : []),
      ...(cfg.features.surveys ? [['surveys', 'chart', tt('Сауалнамалар', 'Опросы')] as Item] : []),
      ...(cfg.features.feedback ? [['feedback', 'chat', tt('Ұсыныс және шағым', 'Предложения и жалобы')] as Item] : []),
      ...(cfg.features.wellbeing ? [['wellbeing', 'heart', tt('Әл-ауқат', 'Благополучие')] as Item] : []),
      ...(cfg.features.lostfound ? [['lostfound', 'search', tt('Табылған заттар', 'Бюро находок')] as Item] : []),
      ...(cfg.features.roommates ? [['roommates', 'home', tt('Пәтерлес', 'Соседи по жилью')] as Item] : []),
      ...(cfg.features.clubs ? [['clubs', 'user', tt('Клубтар', 'Клубы')] as Item] : []),
    ]],
    [tt('Менікі', 'Моё'), [
      ['calendar', 'calendar', tt('Күнтізбе', 'Календарь')],
      ...(cfg.features.points ? [['points', 'trophy', tt('Ұпайлар', 'Баллы')] as Item] : []),
      ['saved', 'heart', tt('Сақталғандар', 'Сохранённое')],
      ['settings', 'gear', tt('Параметрлер', 'Параметры')],
      ...((role === 'admin' || role === 'university_staff') && (cfg.features.surveys || cfg.features.feedback || cfg.features.wellbeing)
        ? [['voice', 'shield', tt('Студент дауысы: басқару', 'Голос студентов: управление')] as Item] : []),
      ...(hasConsole ? [['admin', 'chart', tt('Басқару панелі', 'Панель управления')] as Item] : []),
    ]],
  ];
  return (
    <div className="drawer-back" onClick={e => { if (e.target === e.currentTarget) onClose(); }}>
      <div className="drawer" ref={panel} role="dialog" aria-modal="true" aria-label={tt('Мәзір', 'Меню')} id="nav-drawer">
        <div className="drawer-head">
          <span className="brand"><Logo />Student Hub</span>
          <button className="icon-btn" type="button" onClick={onClose} aria-label={tt('Жабу', 'Закрыть')}><Icon n="x" s={22} /></button>
        </div>
        <nav aria-label={tt('Бөлімдер', 'Разделы')}>
          {groups.map(([title, items]) => (
            <section key={title} className="drawer-group">
              <h2 className="drawer-title">{title}</h2>
              {items.map(([to, icon, label]) => {
                const [n, prm] = to.split(':');
                const sec = sectionOfView(n, prm ?? '');
                const tabs = sec ? visibleTabs(sec, { jobs: content.jobs, clubs, market: !!cfg.features.market }) : [];
                const subId = 'dsub-' + to.replace(/\W/g, '-');
                return (
                  <button key={to} type="button" className="drawer-item" onClick={() => { onClose(); go(to); }} aria-describedby={tabs.length > 1 ? subId : undefined}>
                    <Icon n={icon} s={20} />
                    <span className="drawer-label"><span>{label}</span>
                      {tabs.length > 1 ? <span className="drawer-sub" id={subId} aria-hidden="true">{tabs.map(t => tt(...t.label)).join(' · ')}</span> : null}</span>
                  </button>
                );
              })}
            </section>
          ))}
        </nav>
      </div>
    </div>
  );
}
