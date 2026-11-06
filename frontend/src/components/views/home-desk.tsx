'use client';
/* Home on a laptop (lib/desktop.ts): two more dashboard cards beside the phone home's blocks — this week at a glance
   (classes, deadlines and own events per day, from the calendar's own data) and quick actions. The grid is CSS
   (app/desktop.css, main[data-screen="home"]). */
import { useApp } from '@/components/app';
import { Icon, SectionHead } from '@/components/ui';
import { calItems, classItems, todayYmd, type CalItem } from '@/components/views/calendar';
import { useConfig } from '@/lib/config';
import { addDays, byDay, weekdayMon, WEEK_SHORT } from '@/lib/calendar';
import type { IconName } from '@/lib/icons';

const MAX = 3;

function WeekPreview() {
  const { S, I, tt, go, setUi, content, lang } = useApp();
  const today = todayYmd();
  const from = addDays(today, -weekdayMon(today));
  const to = addDays(from, 6);
  const items: CalItem[] = [...classItems(S.timetable, from, to), ...calItems(S, I, content).filter(it => it.date >= from && it.date <= to && !it.done)];
  const days = byDay(items);
  const open = (d: string) => { setUi({ calSel: d, calMonth: '', calView: 'month' }); go('calendar'); };
  return (
    <section className="section home-week" id="homeWeek">
      <SectionHead title={tt('Осы апта', 'Эта неделя')} more={<button className="link-btn" type="button" onClick={() => go('calendar')}>{tt('Күнтізбе', 'Календарь')}</button>} />
      {/* a day shows up to three items (time + a two-line name); the rest is «+N». Seven columns from 1360px, a strip that
          scrolls sideways below that (app/desktop.css) so a name never shrinks to a few letters */}
      <div className="hw-grid" role="list">
        {Array.from({ length: 7 }, (_, i) => {
          const d = addDays(from, i);
          const list = days.get(d) ?? [];
          const all = list.map(it => (it.time ? `${it.time} ${it.title}` : it.title)).join('; ');
          return (
            <div key={d} role="listitem" className="hw-cell">
              <button type="button" className="hw-day" aria-current={d === today ? 'date' : undefined} onClick={() => open(d)}
                title={all || undefined}
                aria-label={`${I.fDate(new Date(d + 'T12:00:00'))}: ${list.length ? all : tt('бос', 'свободно')}`}>
                <span className="hw-head"><span className="hw-wd">{WEEK_SHORT[lang][i]}</span><b className="hw-num num">{+d.slice(8)}</b></span>
                {list.slice(0, MAX).map(it => (
                  <span key={it.key} className={`hw-it cal-k-${it.kind}`} title={it.time ? `${it.time} ${it.title}` : it.title}>
                    {it.time ? <span className="hw-time num">{it.time}</span> : null}
                    <span className="hw-name">{it.title}</span>
                  </span>
                ))}
                {list.length > MAX ? <span className="hw-more num">+{list.length - MAX}</span> : null}
              </button>
            </div>
          );
        })}
      </div>
    </section>
  );
}

function QuickActions() {
  const { tt, go } = useApp();
  const cfg = useConfig();
  const acts: [string, IconName, string][] = [
    ['timetable', 'timetable', tt('Сабақ кестесі', 'Расписание занятий')],
    ['ai', 'spark', tt('AI көмекші', 'AI-помощник')],
    ['opps:jobs', 'briefcase', tt('Жұмыс', 'Работа')],
    ['opps:intern', 'globe', tt('Тағылымдама', 'Стажировки')],
    ...(cfg.features.cafe ? [['cafe', 'cup', tt('Кофе / Кофейня', 'Кофе / Кофейня')] as [string, IconName, string]] : []),
    ...(cfg.features.print ? [['print', 'file', tt('Полиграфия', 'Полиграфия')] as [string, IconName, string]] : []),
    ['map', 'map', tt('Кампус картасы', 'Карта кампуса')],
    ['saved', 'bookmark', tt('Сақталғандар', 'Сохранённое')],
  ];
  return (
    <section className="section home-quick" id="homeQuick">
      <SectionHead title={tt('Жылдам әрекеттер', 'Быстрые действия')} />
      <div className="hq-grid">
        {acts.map(([to, icon, label]) => (
          <button key={to} type="button" className="hq-act" onClick={() => go(to)}>
            <span className="hq-ico"><Icon n={icon} s={20} /></span><span>{label}</span>
          </button>
        ))}
      </div>
    </section>
  );
}

export function HomeDesk() {
  return <><WeekPreview /><QuickActions /></>;
}
