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
      <div className="hw-grid">
        {Array.from({ length: 7 }, (_, i) => {
          const d = addDays(from, i);
          const list = days.get(d) ?? [];
          return (
            <button key={d} type="button" className="hw-day" aria-current={d === today ? 'date' : undefined} onClick={() => open(d)}
              aria-label={`${I.fDate(new Date(d + 'T12:00:00'))}: ${list.length}`}>
              <span className="hw-head"><span className="hw-wd">{WEEK_SHORT[lang][i]}</span><b className="hw-num num">{+d.slice(8)}</b></span>
              {list.slice(0, 3).map(it => <span key={it.key} className={`hw-it cal-k-${it.kind}`} title={it.time ? `${it.time} ${it.title}` : it.title}>{it.title}</span>)}
              {list.length > 3 ? <span className="hw-more">+{list.length - 3}</span> : null}
            </button>
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
    ['timetable', 'clock', tt('Сабақ кестесі', 'Расписание занятий')],
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
