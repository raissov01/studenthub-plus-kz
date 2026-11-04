'use client';
/* Shell screens: home, notifications, saved, the search result row, and the two hubs reached from home tiles. */
import { useRef } from 'react';
import { useApp } from '@/components/app';
import { Hero3D } from '@/components/views/hero3d';
import { NewsCarousel } from '@/components/views/news-carousel';
import { Chip, Empty, Heart, Icon, Row, SectionHead } from '@/components/ui';
import { ServicesCommunity } from '@/components/views/entries';
import { hasAccount } from '@/components/views/common';
import { InboxSection } from '@/components/views/inbox';
import { PointsChip } from '@/components/views/points';
import { PinButton, saveFile, UpcomingDeadlines } from '@/components/views/calendar';
import { useAccount } from '@/lib/account';
import { CourseSheet, DealSheet } from '@/components/views/sheets';
import { useTelegram } from '@/lib/telegram';
import { classWhere } from '@/lib/campus';
import { classesOn, nextClass, weekdayOf } from '@/lib/timetable';
import { hue, notifs, OPP_KINDS, oppLeft, plainOrg, searchItems, type Notif, type SearchItem } from '@/lib/domain';
import { dOff } from '@/lib/format';
import { buildScheduleIcs } from '@/lib/ics';
import type { ClassItem, IconName } from '@/lib/types';
import { HomeReviewDue } from '@/components/views/review';

const firstName = (name: string, fb: string) => (name || '').trim().split(/\s+/)[0] || fb;
const Mark = ({ id, label }: { id: string; label: string }) => (
  <span className={`mk mk-${hue(id)}`} aria-hidden="true">{plainOrg(label).charAt(0).toUpperCase()}</span>
);
const agoTxt = (h: number, tt: (a: string, b: string) => string) =>
  (h < 24 ? tt(`${h} сағ бұрын`, `${h} ч назад`) : tt(`${Math.round(h / 24)} күн бұрын`, `${Math.round(h / 24)} дн. назад`));

/* ---------- home ---------- */
/** no timetable yet: the one next step instead of a placeholder schedule */
function AddTimetableCard() {
  const { tt, go } = useApp();
  return (
    <div className="card next-class" id="addTimetable">
      <span className="row-title">{tt('Кестеңізді қосыңыз', 'Добавьте своё расписание')}</span>
      <span className="row-sub">{tt('Сабақтарыңызды бір рет енгізіңіз: мұнда келесі сабақ, күнтізбеде апталық кесте, «Пәндер» бөлімінде әр пәннің беті шығады. Кесте тек осы құрылғыда сақталады.', 'Внесите пары один раз: здесь появится следующая пара, в календаре — недельное расписание, в «Предметах» — страница каждого предмета. Расписание хранится только на этом устройстве.')}</span>
      <button className="btn" type="button" onClick={() => go('timetable')}><Icon n="plus" s={18} /> {tt('Кестені қосу', 'Добавить расписание')}</button>
    </div>
  );
}

function NextClassCard({ c }: { c: ClassItem | null }) {
  const { tt, I, go, setUi } = useApp();
  if (!c) {
    return (
      <div className="card next-class">
        <span className="row-title">{tt('Бүгін басқа сабақ жоқ', 'Сегодня больше нет пар')}</span>
        <span className="row-sub">{tt('Аптаның кестесі күнтізбеде.', 'Расписание недели — в календаре.')}</span>
        <button className="link-btn" type="button" onClick={() => go('calendar')}>{tt('Күнтізбені ашу', 'Открыть календарь')}</button>
      </div>
    );
  }
  const now = new Date(), [h, m] = c.t.split(':').map(Number), start = new Date(now);
  start.setHours(h, m, 0, 0);
  const left = Math.round((start.getTime() - now.getTime()) / 60000);
  const when = left >= 60 ? tt(`${Math.floor(left / 60)} сағ ${left % 60} мин кейін`, `через ${Math.floor(left / 60)} ч ${left % 60} мин`) : tt(`${left} минуттан кейін`, `через ${left} мин`);
  const where = classWhere(c, I);
  return (
    <div className="card next-class" id="nextClass">
      <span className="nc-top"><span className="tiny muted">{left > 0 ? tt('Келесі сабақ', 'Следующая пара') + ', ' + when : tt('Қазір жүріп жатыр', 'Идёт сейчас')}</span><b className="num">{c.t}</b></span>
      <span className="row-title">{c.name}</span>
      {c.kind || where ? <span className="row-sub">{[c.kind, where].filter(Boolean).join(', ')}</span> : null}
      {c.room ? (
        <button className="btn" type="button" onClick={() => { setUi({ mapQ: c.room, mapSel: null, geoAuto: true }); go('map:campus'); }}>
          <Icon n="pin" s={18} /> {tt('Картадан жолды көру', 'Показать путь на карте')}
        </button>
      ) : null}
    </div>
  );
}

/** F5 «Кестені күнтізбеге қосу»: the student's own timetable as an .ics file, made on the device (lib/ics.ts) */
export function ScheduleIcsButton() {
  const { S, tt, I, lang, toast } = useApp();
  if (!S.timetable.length) return null;
  const save = () => {
    const text = buildScheduleIcs(S.timetable.map(c => ({
      key: `${c.id}|${c.wd}|${c.t}|${c.room}`, start: c.t, end: c.end, days: [c.wd],
      summary: c.kind ? `${c.name} (${c.kind})` : c.name, location: classWhere(c, I),
      description: tt('Student Hub: менің сабақ кестем.', 'Student Hub: моё расписание.'),
    })), { now: new Date(), calName: tt('Сабақ кестесі — Student Hub', 'Расписание — Student Hub'), lang });
    saveFile(text, 'studenthub-schedule.ics');
    toast(tt('Файл жүктелді — оны күнтізбемен ашыңыз', 'Файл скачан — откройте его календарём'));
  };
  return (
    <button className="btn ghost block" type="button" id="scheduleIcs" onClick={save}>
      <Icon n="calendar" s={18} /> {tt('Кестені күнтізбеге қосу', 'Добавить расписание в календарь')}
    </button>
  );
}

export function Home() {
  const { S, tt, L, I, wd, go, setUi, content } = useApp();
  const today = classesOn(S.timetable, weekdayOf(new Date()));
  const tg = useTelegram();
  const who = tg.name || firstName(S.profile.name, tt('студент', 'студент'));
  const now = new Date();
  const news = [
    ...(content.events ?? []).slice(0, 3).map(e => ({ go: 'event:' + e.id, icon: 'calendar' as IconName, id: String(e.id), kind: tt('Іс-шара', 'Мероприятие'), title: S.lang === 'ru' ? e.title_ru || e.title_kk : e.title_kk, sub: `${I.fDate(new Date(e.starts_at))}, ${e.place_text || ''}` })),
    ...content.opportunities.slice(0, 3).map(o => {
      const left = oppLeft(o);
      const sub = Number.isFinite(left) ? tt(`Өтінім мерзімі: ${I.fDate(dOff(left))}`, `Приём заявок до ${I.fDate(dOff(left))}`) : tt('Мерзімі шектелмеген', 'Без срока');
      return { go: 'opp:' + o.id, icon: 'globe' as IconName, id: o.id, kind: tt('Мүмкіндік', 'Возможность'), title: L(o.title), sub };
    }),
  ];
  const focused = useRef(false);
  return (
    <>
      <form className="search" id="homeSearch" role="search" onSubmit={e => {
        e.preventDefault();
        const inp = e.currentTarget.querySelector('input');
        setUi({ sq: (inp?.value || '').trim(), sk: 'all', sqFocus: true });
        go('search');
      }}>
        <Icon n="search" s={18} />
        <input className="input" id="hq" type="search" enterKeyHint="search" autoComplete="off" placeholder={tt('Не іздейсің?', 'Что ищешь?')} aria-label={tt('Іздеу', 'Поиск')}
          onFocus={e => {
            // the search screen takes over with whatever was typed so far, and keeps the keyboard focus
            if (focused.current) return;
            focused.current = true;
            setUi({ sq: e.target.value, sk: 'all', sqFocus: true });
            go('search');
          }}
          onChange={e => setUi({ sq: e.target.value })} />
      </form>

      <Hero3D who={who} />
      <PointsChip />

      {news.length ? (
        <section className="section" id="homeNews">
          <SectionHead title={tt('Жаңалықтар', 'Новости')} more={<button className="link-btn" type="button" onClick={() => go('opps:events')}>{tt('Барлығын қарау', 'Смотреть все')}</button>} />
          <NewsCarousel items={news} />
        </section>
      ) : null}

      <UpcomingDeadlines />
      <HomeReviewDue />

      <section className="section" id="homeClasses">
        <SectionHead title={tt('Бүгінгі сабақтар', 'Пары сегодня')} more={S.timetable.length
          ? <button className="link-btn" type="button" onClick={() => go('timetable')}>{tt('Кестені өзгерту', 'Изменить расписание')}</button>
          : <span className="tiny muted">{wd[now.getDay()]}</span>} />
        {S.timetable.length ? <NextClassCard c={nextClass(S.timetable, now)} /> : <AddTimetableCard />}
        {today.length ? (
          <details className="day-more">
            <summary className="link-btn">{tt(`Бүгінгі барлық сабақ (${today.length})`, `Все пары сегодня (${today.length})`)}</summary>
            <div className="group">
              {today.map(c => (
                <button key={c.t + c.name} className="row" type="button" onClick={() => { if (c.room) { setUi({ mapQ: c.room, mapSel: null }); go('map:campus'); } }}>
                  <span className="up-when"><b className="num">{c.t}</b><span className="num">{c.end}</span></span>
                  <span className="row-main"><span className="row-title">{c.name}</span><span className="row-sub">{[c.kind, c.room ? tt('аудитория', 'аудитория') + ' ' + c.room : ''].filter(Boolean).join(', ')}</span></span>
                  {c.room ? <span className="chev"><Icon n="pin" s={18} /></span> : null}
                </button>
              ))}
            </div>
            <ScheduleIcsButton />
          </details>
        ) : null}
      </section>
    </>
  );
}

/* ---------- search ---------- */
export function SearchRow({ it }: { it: SearchItem }) {
  const { go, openSheet } = useApp();
  const open = () => (it.go ? go(it.go) : openSheet(it.act === 'course' ? <CourseSheet id={it.id} /> : <DealSheet id={it.id} />));
  return (
    <div className="sr">
      <button className="sr-main" type="button" onClick={open}>
        <Mark id={it.id} label={it.org} />
        <span className="row-main">
          <span className="row-title">{it.org}</span>
          <span className="sr-role">{it.title}</span>
          <span className="row-sub">{it.meta}</span>
          {it.detail ? <span className={`row-sub card-detail${it.truncateDetail ? " card-detail-truncate" : ""}`} title={it.detail}>{it.detail}</span> : null}
          {it.badge ? <span className={`tag tag-${it.badge[1]} tag-inline`}>{it.badge[0]}</span> : null}
        </span>
      </button>
      <span className="sr-end">
        {it.badge ? <span className={`tag tag-${it.badge[1]}`}>{it.badge[0]}</span> : null}
        {it.noFav ? null : <Heart id={it.id} opp={OPP_KINDS.includes(it.k)} />}
        {it.go ? <PinButton item={it.go} /> : null}
      </span>
    </div>
  );
}

/* ---------- notifications ---------- */
function NotifRow({ n }: { n: Notif }) {
  const { S, tt, go, update, openSheet } = useApp();
  const read = !!S.read[n.id];
  const open = () => {
    update(s => { s.read[n.id] = Date.now(); });
    if (n.go) go(n.go); else if (n.data) openSheet(<DealSheet id={n.data} />);
  };
  return (
    <button className={`nrow ${read ? '' : 'unread'}`} type="button" onClick={open}>
      <span className={`nico nico-${n.tone}`}><Icon n={n.icon} s={22} /></span>
      <span className="row-main"><span className="row-title">{n.title}</span><span className="row-sub">{n.text}</span><span className="tiny muted">{agoTxt(n.h, tt)}</span></span>
      <span className="ndot" aria-label={read ? '' : tt('Оқылмаған', 'Непрочитано')}></span>
    </button>
  );
}

/** signed in: the server inbox first, then the device's own notices under their own heading; signed out: only those */
export function Notifications() {
  const signedIn = hasAccount(useAccount());
  if (!signedIn) return <LocalNotices />;
  return (
    <>
      <InboxSection />
      <LocalNotices heading />
    </>
  );
}

function LocalNotices({ heading = false }: { heading?: boolean }) {
  const { S, ui, I, tt, setUi, update, content } = useApp();
  const kinds: [string, string][] = [['all', tt('Барлығы', 'Все')], ['jobs', tt('Жұмыс', 'Работа')], ['events', tt('Іс-шаралар', 'События')], ['deals', tt('Жеңілдіктер', 'Скидки')]];
  const all = notifs(S, I, content);
  const list = all.filter(n => ui.nk === 'all' || n.k === ui.nk);
  const unread = all.filter(n => !S.read[n.id]).length;
  const body = (
    <>
      <div className="chips">{kinds.map(([v, l]) => <Chip key={v} pressed={ui.nk === v} onClick={() => setUi({ nk: v })}>{l}</Chip>)}</div>
      {list.length
        ? <div className="group">{list.map(n => <NotifRow key={n.id} n={n} />)}</div>
        : <Empty icon="bell">{all.length ? tt('Бұл санатта хабарлама жоқ.', 'В этой категории уведомлений нет.')
          : tt('Әзірге хабарлама жоқ. Сізге сәйкес вакансия, жақын мерзім немесе жаңа жеңілдік пайда болғанда осында шығады.', 'Уведомлений пока нет. Здесь появятся подходящие вакансии, близкие сроки и новые скидки.')}</Empty>}
      {unread ? (
        <button className="link-btn" type="button" style={{ alignSelf: 'flex-start' }}
          onClick={() => update(s => { all.forEach(n => { s.read[n.id] = s.read[n.id] || Date.now(); }); })}>
          {tt('Барлығын оқылды деп белгілеу', 'Отметить все прочитанными')}
        </button>
      ) : null}
    </>
  );
  if (!heading) return body;
  return (
    <section className="section" id="device-notices">
      <SectionHead title={tt('Осы құрылғыдағы ескертулер', 'Подсказки на этом устройстве')} />
      {body}
    </section>
  );
}

/* ---------- hubs reached from home tiles ---------- */
export function Services() {
  const { tt } = useApp();
  return (
    <div className="group">
      <Row go="map" icon="map" title={tt('Кампус картасы', 'Карта кампуса')} sub={tt('15 нысан: корпустар, жатақханалар, аудитория іздеу', '15 зданий: корпуса, общежития, поиск аудитории')} />
      <Row go="map:near" icon="pin" title={tt('Жақын маңда', 'Рядом с кампусом')} sub={tt('Аялдама, асхана, дәріхана, басып шығару, банкомат', 'Остановки, столовые, аптеки, печать, банкоматы')} />
      <Row go="docs" icon="file" title={tt('Анықтамалар мен құжаттар', 'Справки и документы')} sub={tt('Оқу орнынан анықтама, транскрипт, 073/у', 'Справка с места учёбы, транскрипт, 073/у')} />
      <Row go="gpa" icon="chart" title={tt('GPA және шәкіртақы', 'GPA и стипендия')} sub={tt('Калькулятор, мақсатты GPA', 'Калькулятор, целевой GPA')} />
      <Row go="pay" icon="tenge" title={tt('Оқу ақысы', 'Оплата обучения')} sub={tt('Қайдан тексеру, төлем мерзімін еске салу', 'Где проверить, напоминание о сроке оплаты')} />
      <Row go="opps:events" icon="calendar" title={tt('Іс-шаралар', 'Мероприятия')} sub={tt('Форумдар, жәрмеңкелер, клубтар', 'Форумы, ярмарки, клубы')} />
      <Row go="admission" icon="cap" title={tt('Түсу', 'Поступление')} sub={tt('Бакалавриат, магистратура, докторантура', 'Бакалавриат, магистратура, докторантура')} />
      <ServicesCommunity />
    </div>
  );
}

export function Materials() {
  const { tt } = useApp();
  return (
    <div className="group">
      <Row go="cards" icon="cards" title={tt('Карточкалар мен тесттер', 'Карточки и тесты')} sub={tt('Дәріс мәтінінен жиын құру', 'Набор из текста лекции')} />
      <Row go="exam" icon="calendar" title={tt('Емтиханға дайындық', 'Подготовка к экзамену')} sub={tt('Күн бойынша жоспар', 'План по дням')} />
      <Row go="opps:courses" icon="book" title={tt('Курстар мен сертификаттар', 'Курсы и сертификаты')} sub={tt('Мамандығыңызға арналған тегін курстар', 'Бесплатные курсы по специальности')} />
      <Row go="interview" icon="mic" title={tt('Сұхбатқа дайындық', 'Подготовка к собеседованию')} />
    </div>
  );
}

export function Saved() {
  const { S, I, tt, go, content } = useApp();
  const items = searchItems(S, I, content).filter(it => it.fav);
  return items.length
    ? <div className="group">{items.map(it => <SearchRow key={it.k + it.id} it={it} />)}</div>
    : (
      <Empty icon="heart" action={<button className="btn secondary" type="button" onClick={() => go('search')}>{tt('Іздеуге өту', 'Перейти к поиску')}</button>}>
        {tt('Әзірге ештеңе сақталмаған. Іздеудегі жүрекшені басыңыз.', 'Пока ничего не сохранено. Нажмите на сердечко в поиске.')}
      </Empty>
    );
}
