'use client';
/* The hamburger menu: every section in four groups, so the home screen does not have to show them all.
   Opens from the header on the root screens and from the home «Барлығы» tile (window event OPEN_MENU). */
import { useEffect, useRef } from 'react';
import { useApp } from '@/components/app';
import { Logo } from '@/components/Logo';
import { Icon } from '@/components/ui';
import { useConfig } from '@/lib/config';
import type { IconName } from '@/lib/types';

export const OPEN_MENU = 'studenthub:menu';
export const openMenu = () => window.dispatchEvent(new Event(OPEN_MENU));

type Item = [string, IconName, string];

export function NavDrawer({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { tt, go } = useApp();
  const cfg = useConfig();
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
  const groups: [string, Item[]][] = [
    [tt('Мүмкіндіктер', 'Возможности'), [
      ['opps:jobs', 'briefcase', tt('Жұмыс', 'Работа')],
      ['opps:intern', 'globe', tt('Тағылымдама', 'Стажировки')],
      ['opps:events', 'calendar', tt('Іс-шаралар', 'Мероприятия')],
      ['opps:courses', 'award', tt('Курстар', 'Курсы')],
      ['bonus', 'tag', tt('Жеңілдіктер', 'Скидки')],
    ]],
    [tt('Оқу', 'Учёба'), [
      ['materials', 'book', tt('Оқу материалдары', 'Учебные материалы')],
      ['ai', 'spark', tt('AI көмекші', 'AI-помощник')],
      ...(cfg.features.quiz ? [['quiz', 'cards', tt('Квиздер', 'Квизы')] as Item] : []),
      ['roadmap', 'target', tt('Даму жоспары', 'План развития')],
    ]],
    [tt('Университет', 'Университет'), [
      ['services', 'cap', tt('Университет қызметтері', 'Услуги университета')],
      ['map', 'map', tt('Кампус картасы', 'Карта кампуса')],
    ]],
    [tt('Менікі', 'Моё'), [
      ['calendar', 'calendar', tt('Күнтізбе', 'Календарь')],
      ['saved', 'heart', tt('Сақталғандар', 'Сохранённое')],
      ['settings', 'gear', tt('Параметрлер', 'Параметры')],
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
              {items.map(([to, icon, label]) => (
                <button key={to} type="button" className="drawer-item" onClick={() => { onClose(); go(to); }}>
                  <Icon n={icon} s={20} /><span>{label}</span>
                </button>
              ))}
            </section>
          ))}
        </nav>
      </div>
    </div>
  );
}
