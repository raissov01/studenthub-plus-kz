'use client';
/* «Мүмкіндіктер»: jobs, internships, events and courses tabs; «Менің тізімім». */
import { useApp } from '@/components/app';
import { Chip, DemoBadge, Empty, Icon, Row, Seg, SectionHead } from '@/components/ui';
import { CourseSheet } from '@/components/views/sheets';
import { CITIES, COURSES, EVENTS, EVENT_CATS, JOB_TAGS, OPP_TYPES } from '@/lib/data/static';
import { deadlineText, hourly, oppLeft, payText, qualify, typeLabel } from '@/lib/domain';
import { dOff, diffDays, plural } from '@/lib/format';
import type { EventItem, Job, Plan } from '@/lib/types';

export function JobRow({ j }: { j: Job }) {
  const { S, tt, tp, L, I, go } = useApp();
  const pt = payText(j, I);
  const tags = j.tags.slice(0, 2).filter(k => JOB_TAGS[k]);
  const applied = !!S.applied[j.id];
  return (
    <button className="row" type="button" onClick={() => go('job:' + j.id)} style={{ alignItems: 'flex-start' }}>
      <span className={`ico ${j.today ? 'ok' : ''}`}><Icon n="briefcase" /></span>
      <span className="row-main">
        <span className="row-title">{L(j.title)}</span>
        <span className="row-sub">{L(j.org)}, {L(CITIES[j.city])}</span>
        <span className="job-pay"><b className="pay">{pt.main}</b> <span className="tiny muted">{pt.sub}</span><DemoBadge show={j.is_demo} /></span>
        {tags.length || applied ? (
          <span className="badges">
            {applied ? <span className="badge b-ok"><Icon n="check" s={12} /> {tt('Өтінім жіберілді', 'Отклик отправлен')}</span> : null}
            {tags.map(k => <span key={k} className="badge b-line">{tp(JOB_TAGS[k])}</span>)}
          </span>
        ) : null}
      </span>
      <span className="chev" style={{ alignSelf: 'center' }}><Icon n="chev" s={18} /></span>
    </button>
  );
}

export function EventCard({ e }: { e: EventItem }) {
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
          </span>
        </div>
      </div>
    </button>
  );
}

function OppList({ t }: { t: string }) {
  const { S, ui, I, tt, L, setUi, openSheet, go, content } = useApp();
  const qm = (...parts: string[]) => !ui.q || parts.join(' ').toLowerCase().includes(ui.q.trim().toLowerCase());
  const none = (icon: 'search' | 'globe' | 'calendar' | 'book', msg: string) => (
    <Empty icon={icon} action={ui.q ? <button className="btn ghost" type="button" onClick={() => setUi({ q: '' })}>{tt('Іздеуді тазалау', 'Очистить поиск')}</button> : null}>{msg}</Empty>
  );
  if (t === 'jobs') {
    const f = ui.jf;
    let list = content.jobs.filter(j => (!f.today || j.today) && ['noexp', 'daily', 'remote', 'near'].every(k => !f[k] || j.tags.includes(k)) && (ui.city === 'all' || j.city === ui.city) && qm(L(j.title), L(j.org), L(j.desc)));
    if (ui.sortPay) list = list.slice().sort((a, b) => hourly(b) - hourly(a));
    return (
      <>
        <p className="tiny muted">{tt(`Табылды: ${list.length}`, `Найдено: ${list.length}`)}</p>
        {list.length ? <div className="group">{list.map(j => <JobRow key={j.id} j={j} />)}</div> : none('search', tt('Сәйкес вакансия жоқ. Сүзгіні азайтып көріңіз.', 'Подходящих вакансий нет. Попробуйте убрать фильтры.'))}
      </>
    );
  }
  if (t === 'intern') {
    const list = content.opportunities.filter(o => (ui.oppType === 'all' || o.type === ui.oppType) && qm(L(o.title), L(o.org), L(o.desc))).sort((a, b) => oppLeft(a) - oppLeft(b));
    if (!list.length) return none('globe', tt('Бағдарлама табылмады.', 'Программы не найдены.'));
    return (
      <div className="list">
        {list.map(o => {
          const q = qualify(o, S.profile, I), left = oppLeft(o);
          return (
            <button key={o.id} className="card" type="button" onClick={() => go('opp:' + o.id)}>
              <div className="badges">
                <span className="badge b-sky">{typeLabel(o.type, I)}</span>
                {o.funded ? <span className="badge b-line">{tt('Қаржыландырылады', 'Финансируется')}</span> : null}
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
          );
        })}
      </div>
    );
  }
  if (t === 'events') {
    const list = EVENTS.filter(e => !e.past && (ui.evCat === 'all' || e.cat === ui.evCat) && qm(L(e.title), L(e.place), L(e.desc))).sort((a, b) => a.day - b.day);
    return list.length ? <div className="list">{list.map(e => <EventCard key={e.id} e={e} />)}</div> : none('calendar', tt('Іс-шара табылмады.', 'Событий не найдено.'));
  }
  const list = COURSES.filter(c => (!ui.courseMine || c.spec.includes(S.profile.spec)) && qm(L(c.title), c.platform));
  const certLbl: Record<string, [string, string]> = { free: [tt('Сертификат тегін', 'Сертификат бесплатно'), 'b-ok'], badge: [tt('Тегін бейдж', 'Бесплатный бейдж'), 'b-ok'], paid: [tt('Сертификат ақылы', 'Сертификат платный'), 'b-line'] };
  if (!list.length) return none('book', tt('Курс табылмады.', 'Курсы не найдены.'));
  return (
    <>
      <div className="group">
        {list.map(c => (
          <Row key={c.id} onClick={() => openSheet(<CourseSheet id={c.id} />)} icon={S.done[c.id] ? 'award' : 'book'} tone={S.done[c.id] ? 'ok' : ''} title={L(c.title)}
            sub={<>{c.platform}, {tt(`${c.weeks} апта`, `${c.weeks} ${plural(c.weeks, ['неделя', 'недели', 'недель'])}`)}<span className="badges">{S.done[c.id] ? <span className="badge b-ok"><Icon n="check" s={12} /> {tt('Аяқталды', 'Пройден')}</span> : null}<span className={`badge ${certLbl[c.cert][1]}`}>{certLbl[c.cert][0]}</span></span></>} />
        ))}
      </div>
      <p className="note">{tt('Оқудың өзі тегін. Сертификат құнын курс бетінен тексеріңіз.', 'Само обучение бесплатное. Стоимость сертификата уточняйте на странице курса.')}</p>
    </>
  );
}

export function Opps({ tab }: { tab: string }) {
  const { ui, tt, tp, L, setUi, go, content } = useApp();
  const t = tab;
  const tabs: [string, string][] = [['jobs', tt('Жұмыс', 'Работа')], ['intern', tt('Тағылымдама', 'Стажировки')], ['events', tt('Іс-шара', 'События')], ['courses', tt('Курстар', 'Курсы')]];
  const ph = ({ jobs: tt('Вакансияны іздеу', 'Поиск вакансий'), intern: tt('Бағдарламаны іздеу', 'Поиск программ'), events: tt('Іс-шараны іздеу', 'Поиск событий'), courses: tt('Курсты іздеу', 'Поиск курсов') } as Record<string, string>)[t];
  let controls = null;
  if (t === 'jobs') {
    const f = ui.jf;
    const filtersOn = Object.values(f).some(Boolean) || ui.city !== 'all' || ui.sortPay;
    const toggle = (k: string) => setUi(u => ({ jf: { ...u.jf, [k]: !u.jf[k] } }));
    controls = (
      <>
        <div className="chips">
          <Chip pressed={f.today} onClick={() => toggle('today')}><Icon n="bolt" s={15} /> {tt('Бүгінгі', 'На сегодня')}</Chip>
          {Object.entries(JOB_TAGS).map(([k, l]) => <Chip key={k} pressed={f[k]} onClick={() => toggle(k)}>{tp(l)}</Chip>)}
        </div>
        <div className="grid-2">
          <div className="field"><label htmlFor="city">{tt('Қала', 'Город')}</label>
            <select className="input" id="city" value={ui.city} onChange={e => setUi({ city: e.target.value })}>
              <option value="all">{tt('Барлық қала', 'Все города')}</option>
              {Object.entries(CITIES).filter(([k]) => content.jobs.some(j => j.city === k)).map(([k, c]) => <option key={k} value={k}>{L(c)}</option>)}
            </select></div>
          <div className="field"><label htmlFor="sort">{tt('Ретімен', 'Порядок')}</label>
            <select className="input" id="sort" value={ui.sortPay ? 'pay' : 'new'} onChange={e => setUi({ sortPay: e.target.value === 'pay' })}>
              <option value="new">{tt('Жаңалары алдымен', 'Сначала новые')}</option>
              <option value="pay">{tt('Сағаттық төлем бойынша', 'По оплате в час')}</option>
            </select></div>
        </div>
        {filtersOn ? <button className="link-btn" type="button" style={{ alignSelf: 'flex-start' }} onClick={() => setUi({ jf: {}, city: 'all', sortPay: false })}>{tt('Сүзгілерді тазалау', 'Сбросить фильтры')}</button> : null}
      </>
    );
  } else if (t === 'intern') {
    const types: [string, string][] = [['all', tt('Барлығы', 'Все')], ...Object.entries(OPP_TYPES).map(([k, l]): [string, string] => [k, tp(l)])];
    controls = <div className="chips">{types.map(([k, l]) => <Chip key={k} pressed={ui.oppType === k} onClick={() => setUi({ oppType: k })}>{l}</Chip>)}</div>;
  } else if (t === 'events') {
    const cats: [string, string][] = [['all', tt('Барлығы', 'Все')], ...Object.entries(EVENT_CATS).map(([k, c]): [string, string] => [k, L(c)])];
    controls = <div className="chips">{cats.map(([k, l]) => <Chip key={k} pressed={ui.evCat === k} onClick={() => setUi({ evCat: k })}>{l}</Chip>)}</div>;
  } else {
    controls = (
      <div className="chips">
        <Chip pressed={!ui.courseMine} onClick={() => setUi({ courseMine: false })}>{tt('Барлық курстар', 'Все курсы')}</Chip>
        <Chip pressed={ui.courseMine} onClick={() => setUi({ courseMine: true })}><Icon n="target" s={15} /> {tt('Мамандығыма арналған', 'Для моей профессии')}</Chip>
      </div>
    );
  }
  return (
    <>
      <Seg items={tabs} cur={t} onPick={v => { setUi({ oppTab: v, q: '' }); go('opps:' + v, { replace: true }); }} />
      <div className="search"><Icon n="search" s={18} /><input className="input" id="oq" type="search" enterKeyHint="search" autoComplete="off" placeholder={ph} value={ui.q} onChange={e => setUi({ q: e.target.value })} /></div>
      {controls}
      <div id="oppList" className="section"><OppList t={t} /></div>
    </>
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
      <section className="section"><SectionHead title={tt('Өтінімдер', 'Отклики')} more={<span className="tiny muted num">{jobs.length}</span>} />
        {jobs.length ? <div className="group">{jobs.map(j => <JobRow key={j.id} j={j} />)}</div> : empty(tt('Әзірге өтінім жоқ.', 'Откликов пока нет.'), 'opps:jobs', tt('Жұмыс іздеу', 'Найти работу'))}</section>
      <section className="section"><SectionHead title={tt('Сақталған бағдарламалар', 'Сохранённые программы')} more={<span className="tiny muted num">{opps.length}</span>} />
        {opps.length
          ? <div className="group">{opps.map(o => <Row key={o.id} go={'opp:' + o.id} icon="globe" title={I.L(o.title)} sub={<span className={oppLeft(o) <= 14 ? 'urgent' : ''}>{deadlineText(o, I)}</span>} />)}</div>
          : empty(tt('Ұнаған тағылымдаманы сақтасаңыз, мерзімі осында көрінеді.', 'Сохраните стажировку, и её срок будет виден здесь.'), 'opps:intern', tt('Тағылымдамаларды қарау', 'Смотреть стажировки'))}</section>
      <section className="section"><SectionHead title={tt('Іс-шараларым', 'Мои мероприятия')} more={<span className="tiny muted num">{evs.length}</span>} />
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
