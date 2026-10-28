'use client';
/* Rows and cards of the search lists (vacancy, opportunity, event), the «Ұсын» link, and «Менің тізімім». */
import { useApp } from '@/components/app';
import { SuggestButton } from '@/components/views/ambassador';
import { MyDbEvents } from '@/components/views/events';
import { EmployerJobBits, MyApplications, OfficialBadge } from '@/components/views/jobs-b2b';
import { DemoBadge, Empty, Heart, Icon, Row, SectionHead } from '@/components/ui';
import { CITIES, EVENTS, EVENT_CATS, JOB_TAGS } from '@/lib/data/static';
import { deadlineText, jobApplyText, oppLeft, payText, qualify, typeLabel } from '@/lib/domain';
import { dOff, diffDays } from '@/lib/format';
import type { EventItem, Job, Opportunity, Plan } from '@/lib/types';

/** a vacancy row; `heart` = the search list's variant with ♥ in place of the chevron */
export function JobRow({ j, heart }: { j: Job; heart?: boolean }) {
  const { S, tt, tp, L, I, go } = useApp();
  const pt = payText(j, I);
  const tags = j.tags.slice(0, 2).filter(k => JOB_TAGS[k]);
  const applied = !!S.applied[j.id];
  const row = (
    <button className="row" type="button" onClick={() => go('job:' + j.id)} style={{ alignItems: 'flex-start' }}>
      <span className={`ico ${j.today ? 'ok' : ''}`}><Icon n="briefcase" /></span>
      <span className="row-main">
        <span className="row-title">{L(j.title)}</span>
        <span className="row-sub">{L(j.org)}, {L(CITIES[j.city])}</span>
        <span className="job-pay"><b className="pay">{pt.main}</b> <span className="tiny muted">{pt.sub}</span><DemoBadge show={j.is_demo} /></span>
        <EmployerJobBits j={j} />
        <span className="row-sub card-detail">{tt("Өтінім: ", "Отклик: ")}{jobApplyText(j, I)}</span>
        {tags.length || applied ? (
          <span className="badges">
            {applied ? <span className="badge b-ok"><Icon n="check" s={12} /> {tt('Өтінім жіберілді', 'Отклик отправлен')}</span> : null}
            {tags.map(k => <span key={k} className="badge b-line">{tp(JOB_TAGS[k])}</span>)}
          </span>
        ) : null}
      </span>
      {heart ? null : <span className="chev" style={{ alignSelf: 'center' }}><Icon n="chev" s={18} /></span>}
    </button>
  );
  return heart ? <div className="sr">{row}<span className="sr-end sr-heart"><Heart id={j.id} opp={false} /></span></div> : row;
}

export function EventCard({ e, demo }: { e: EventItem; demo?: boolean }) {
  const { S, tt, L, monS, wd, go } = useApp();
  const d = dOff(e.day), reg = S.events[e.id];
  return (
    <button className="card" type="button" onClick={() => go('event:' + e.id)}>
      <div className="card-row">
        <div className="date"><small>{monS[d.getMonth()]}</small><b>{d.getDate()}</b></div>
        <div className="row-main">
          <span className="row-title">{L(e.title)}</span>
          <span className="row-sub">{wd[d.getDay()]}, {e.time}. {L(e.place)}</span>
          <span className="badges" style={{ marginTop: 6 }}>
            <span className="badge b-line">{L(EVENT_CATS[e.cat])}</span>
            {e.cert ? <span className="badge b-sun">{tt('Сертификат', 'Сертификат')}</span> : null}
            {reg ? <span className="badge b-ok"><Icon n="check" s={12} /> {tt('Тіркелдіңіз', 'Вы записаны')}</span> : null}
            <DemoBadge show={!!demo} />
          </span>
        </div>
      </div>
    </button>
  );
}

/** «Мүмкіндік білесің бе? Ұсын» at the end of job/opportunity lists: the site's form, whatever the switches say */
export function SuggestLink() {
  return <SuggestButton />;
}

/** an internship / grant / mobility card: type, funding, deadline and «Сізге сәйкес» match badge, with ♥ */
export function OppCard({ o }: { o: Opportunity }) {
  const { S, tt, L, I, go } = useApp();
  const q = qualify(o, S.profile, I), left = oppLeft(o);
  return (
    <div className="fav-card">
      <button className="card" type="button" onClick={() => go('opp:' + o.id)}>
        <div className="badges">
          <span className="badge b-sky">{typeLabel(o.type, I)}</span>
          {o.funded ? <span className="badge b-line">{tt('Қаржыландырылады', 'Финансируется')}</span> : null}
          <OfficialBadge o={o} />
          {S.saved[o.id] ? <span className="badge b-sun">{tt('Сақталған', 'Сохранено')}</span> : null}
          <DemoBadge show={o.is_demo} />
        </div>
        <h3>{L(o.title)}</h3>
        <span className="row-sub">{L(o.org)}</span>
        <div className="card-row" style={{ justifyContent: 'space-between', alignItems: 'center', flexWrap: 'wrap', gap: 8 }}>
          <span className={`tiny ${left <= 14 ? 'urgent' : 'muted'}`}><Icon n="clock" s={13} /> {deadlineText(o, I)}</span>
          <span className={`badge ${q.ok ? 'b-ok' : 'b-line'}`}>{q.ok ? <><Icon n="check" s={12} /> {tt('Сізге сәйкес', 'Подходите')}</> : tt('Талаптарды тексеріңіз', 'Проверьте требования')}</span>
        </div>
      </button>
      <Heart id={o.id} opp />
    </div>
  );
}

export function Mine() {
  const { S, tt, I, content } = useApp();
  const jobs = content.jobs.filter(j => S.applied[j.id]);
  const opps = content.opportunities.filter(o => S.saved[o.id]).sort((a, b) => oppLeft(a) - oppLeft(b));
  const evs = EVENTS.filter(e => !e.past && S.events[e.id]).sort((a, b) => a.day - b.day);
  const plans = S.plans;
  const empty = (msg: string, go: string, label: string) => <div className="group"><EmptyGo msg={msg} go={go} label={label} /></div>;
  return (
    <>
      <MyApplications />
      <section className="section"><SectionHead title={tt('Өтінімдер', 'Отклики')} more={<span className="tiny muted num">{jobs.length}</span>} />
        {jobs.length ? <div className="group">{jobs.map(j => <JobRow key={j.id} j={j} />)}</div> : empty(tt('Әзірге өтінім жоқ.', 'Откликов пока нет.'), 'opps:jobs', tt('Жұмыс іздеу', 'Найти работу'))}</section>
      <section className="section"><SectionHead title={tt('Сақталған бағдарламалар', 'Сохранённые программы')} more={<span className="tiny muted num">{opps.length}</span>} />
        {opps.length
          ? <div className="group">{opps.map(o => <Row key={o.id} go={'opp:' + o.id} icon="globe" title={I.L(o.title)} sub={<span className={oppLeft(o) <= 14 ? 'urgent' : ''}>{deadlineText(o, I)}</span>} />)}</div>
          : empty(tt('Ұнаған тағылымдаманы сақтасаңыз, мерзімі осында көрінеді.', 'Сохраните стажировку, и её срок будет виден здесь.'), 'opps:intern', tt('Тағылымдамаларды қарау', 'Смотреть стажировки'))}</section>
      <section className="section"><SectionHead title={tt('Іс-шараларым', 'Мои мероприятия')} more={<span className="tiny muted num">{evs.length}</span>} />
        <MyDbEvents />
        {evs.length ? <div className="list">{evs.map(e => <EventCard key={e.id} e={e} />)}</div> : empty(tt('Әзірге тіркелген іс-шара жоқ.', 'Вы пока никуда не записаны.'), 'opps:events', tt('Іс-шараларды қарау', 'Смотреть события'))}</section>
      <section className="section"><SectionHead title={tt('Емтихан жоспарлары', 'Планы к экзаменам')} more={<span className="tiny muted num">{plans.length}</span>} />
        {plans.length ? <div className="group">{plans.map(pl => <PlanRow key={pl.id} pl={pl} sep="," />)}</div>
          : empty(tt('Емтиханға дайындық жоспары әлі жоқ.', 'Плана подготовки пока нет.'), 'exam', tt('Жоспар құру', 'Составить план'))}</section>
    </>
  );
}

function EmptyGo({ msg, go, label }: { msg: string; go: string; label: string }) {
  const app = useApp();
  return <Empty action={<button className="btn secondary" type="button" onClick={() => app.go(go)}>{label}</button>}>{msg}</Empty>;
}

/** a saved exam plan in a list; «Менің тізімім» uses ", " and the exam screen " · " like the legacy app */
export function PlanRow({ pl, sep, tone }: { pl: Plan; sep: ',' | ' ·'; tone?: boolean }) {
  const { tt, I } = useApp();
  const dl = diffDays(pl.exam), done = pl.items.filter(x => x.done).length;
  const past = sep === ',' ? tt('өтті', 'прошёл') : tt('Емтихан өтті', 'Экзамен прошёл');
  const left = dl >= 0 ? (sep === ',' ? I.daysLeftTxt(dl).toLowerCase() : I.daysLeftTxt(dl)) : past;
  return <Row go={'plan:' + pl.id} icon="calendar" tone={tone && dl <= 3 ? 'sun' : ''} title={pl.subject} sub={`${I.fDate(new Date(pl.exam))}${sep} ${left}`} end={<span className="tiny muted num">{done}/{pl.items.length}</span>} />;
}
