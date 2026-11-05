'use client';
/* «Пәндер»: the subject list (from the timetable, plus own ones) and one screen per subject that gathers what
   already exists elsewhere — next class and room, deadlines, notes, flashcard decks, spaced repetition, exam date
   and plan, the expected grade. /subjects lists them; /subjects?id=<id> is one subject (see lib/routes QUERY_ID). */
import { useState, useSyncExternalStore } from 'react';
import { useSearchParams } from 'next/navigation';
import { useApp, type App } from '@/components/app';
import { ConfirmSheet, Empty, Icon, Row, SectionHead } from '@/components/ui';
import { NewNoteSheet } from '@/components/views/notes';
import { SubjectScoreRow } from '@/components/views/grades';
import { buildPlan } from '@/lib/domain';
import { diffDays, plural } from '@/lib/format';
import { almatyYmd } from '@/lib/calendar';
import { noteTitle, TEMPLATE_LABEL } from '@/lib/notes';
import { dueQueue } from '@/lib/srs';
import { saveSubject, useStudy, type StudySnap } from '@/lib/studydb';
import { useFeature } from '@/components/views/common';
import { qaBoard } from '@/lib/community';
import { belongs, mentions, nextSlot, subjectsFrom, type Subject } from '@/lib/subjects';
import type { AppState, Deck } from '@/lib/types';
import { apiLang } from '@/lib/i18n';

export const todayYmd = () => almatyYmd(Date.now());

const noSub = () => () => {};
/** the ?id= of /subjects, /notes, /review. The router's value, or the address bar's when the service worker answered
    offline with the saved /notes page (its router state has no query, the address bar has). */
export function useQueryId(): string | null {
  const fromRouter = useSearchParams().get('id');
  const fromBar = useSyncExternalStore(noSub, () => new URLSearchParams(window.location.search).get('id'), () => null);
  return fromRouter ?? fromBar;
}

/** every subject the student sees, in timetable order (the student's own timetable), own ones last */
export function useSubjects(): { list: Subject[]; study: StudySnap } {
  const { S } = useApp();
  const study = useStudy();
  return { list: subjectsFrom(S.timetable, study.subjects), study };
}

/** the student's own decks of a subject: made for it, or named after it */
export const subjectDecks = (S: AppState, s: Subject): Deck[] =>
  S.decks.filter(d => belongs(s, d, d.name));

/** how many cards are due today in these decks */
export const dueCount = (decks: Deck[], study: StudySnap) => dueQueue(decks, study.srs, todayYmd()).length;

const cardsTxt = (n: number, tt: App['tt']) => tt(`${n} карточка`, `${n} ${plural(n, ['карточка', 'карточки', 'карточек'])}`);

export function SubjectsScreen() {
  const id = useQueryId();
  return id ? <SubjectPage id={id} /> : <SubjectList />;
}

function SubjectList() {
  const { S, tt, go, toast } = useApp();
  const { list, study } = useSubjects();
  const [name, setName] = useState('');
  const add = () => {
    const n = name.trim().slice(0, 120);
    if (!n) return;
    const dup = list.find(s => s.names.some(x => x.toLowerCase() === n.toLowerCase()));
    if (dup) { go('subjects:' + dup.id); return; }
    const id = 'c' + Date.now().toString(36);
    void saveSubject({ id, name: n, custom: true, exam: '' });
    setName('');
    toast(tt('Пән қосылды', 'Предмет добавлен'));
  };
  return (
    <>
      {S.timetable.length
        ? <p className="small muted">{tt('Пәндер сіздің сабақ кестеңізден алынды. Әр пәннің бетінде сабақ, дедлайн, конспект, карточка, емтихан мен баға бір жерде.', 'Предметы взяты из вашего расписания. На странице предмета — пары, дедлайны, конспекты, карточки, экзамен и оценка в одном месте.')}</p>
        : (
          <Empty icon="clock" action={<button className="btn secondary" type="button" id="subjAddTimetable" onClick={() => go('timetable')}><Icon n="plus" s={18} /> {tt('Кестені қосу', 'Добавить расписание')}</button>}>
            {tt('Сабақ кестеңізді қосыңыз — пәндер одан өздігінен шығады. Немесе пәнді төменнен қолмен қосыңыз.', 'Добавьте своё расписание — предметы появятся из него сами. Или добавьте предмет вручную ниже.')}
          </Empty>
        )}
      <div className="group" id="subjectList">
        {list.map(s => {
          const nx = nextSlot(s, new Date());
          const notes = study.notes.filter(n => n.subject === s.id).length;
          const due = dueCount(subjectDecks(S, s), study);
          const sub = [
            nx ? `${nx.today ? tt('бүгін', 'сегодня') : tt('келесі', 'след.')} ${nx.slot.t}, ${nx.slot.room}` : '',
            notes ? tt(`${notes} конспект`, `конспектов: ${notes}`) : '',
          ].filter(Boolean).join(' · ');
          return <Row key={s.id} go={'subjects:' + s.id} icon="book" title={s.name} sub={sub || undefined}
            end={due ? <span className="badge b-sky num">{due}</span> : undefined} />;
        })}
      </div>
      <form className="card form" onSubmit={e => { e.preventDefault(); add(); }}>
        <div className="field">
          <label htmlFor="subjNew">{tt('Пән қосу', 'Добавить предмет')}</label>
          <input className="input" id="subjNew" value={name} maxLength={120} onChange={e => setName(e.target.value)} placeholder={tt('мысалы: Философия', 'например: Философия')} />
        </div>
        <button className="btn" type="submit" disabled={!name.trim()}><Icon n="plus" s={18} /> {tt('Қосу', 'Добавить')}</button>
      </form>
      <Row go="notes" icon="doc" title={tt('Барлық конспектілер', 'Все конспекты')} sub={tt('Іздеу және экспорт', 'Поиск и экспорт')} />
    </>
  );
}

function SubjectPage({ id }: { id: string }) {
  const app = useApp();
  const { S, I, tt, L, go, setUi, update, openSheet, toast } = app;
  const { list, study } = useSubjects();
  const qaOn = useFeature('courseqa');
  const s = list.find(x => x.id === id);
  if (!study.ready) return <p className="small muted" role="status">{tt('Жүктелуде…', 'Загрузка…')}</p>;
  if (!s) {
    return <Empty icon="book" action={<button className="btn secondary" type="button" onClick={() => go('subjects', { replace: true })}>{tt('Пәндер', 'Предметы')}</button>}>{tt('Пән табылмады.', 'Предмет не найден.')}</Empty>;
  }
  const nx = nextSlot(s, new Date());
  const decks = subjectDecks(S, s);
  const due = dueCount(decks, study);
  const total = decks.reduce((n, d) => n + d.cards.length, 0);
  const notes = study.notes.filter(n => n.subject === s.id).sort((a, b) => (b.date || '').localeCompare(a.date || '') || b.updated - a.updated);

  // deadlines and to-do items: linked by id once the to-do list has one, otherwise by naming the subject
  // (`courseId` is read if the to-do model ever gets one; until then the title decides)
  type Dl = AppState['deadlines'][number] & { courseId?: string };
  const deadlines = (S.deadlines as Dl[]).filter(d => !d.done && belongs(s, d, d.title + ' ' + d.note))
    .sort((a, b) => (a.date || '9999').localeCompare(b.date || '9999') || (a.time || '').localeCompare(b.time || ''));
  const pins = (S.calendarPins ?? []).filter(p => !p.done && (mentions(s, p.title.kk) || mentions(s, p.title.ru)));
  const ymdTxt = (d: string, t = '') => (d ? I.fDate(new Date(d + 'T00:00:00')) + (t ? ', ' + t : '') : tt('Мерзімсіз', 'Без срока'));

  const plan = S.plans.find(p => mentions(s, p.subject));
  const grade = S.gpaPlan.find(g => mentions(s, g.n));
  const examLeft = s.exam ? diffDays(s.exam + 'T00:00:00') : null;
  const setExam = (v: string) => {
    const prev = study.subjects.find(m => m.id === s.id);
    void saveSubject({ id: s.id, name: s.custom ? s.name : (prev?.name ?? ''), custom: s.custom, exam: v, ...(prev?.hidden ? { hidden: true } : {}) });
  };
  const makePlan = () => {
    const pl = s.exam && examLeft != null && examLeft >= 1 ? buildPlan(s.name, s.exam + 'T00:00:00', 10, '', I) : null;
    if (!pl) { go('exam'); return; }
    update(x => { x.plans.unshift(pl); });
    go('plan:' + pl.id);
  };

  return (
    <>
      <section className="card" aria-labelledby="subjTitle">
        <span className="eyebrow">{s.custom ? tt('Өз пәнім', 'Мой предмет') : tt('Кестеден', 'Из расписания')}</span>
        <h1 id="subjTitle" className="subj-title">{s.name}</h1>
        {nx ? (
          <>
            <span className="row-sub">
              {tt('Келесі сабақ', 'Следующая пара')}: <b className="num">{nx.today ? tt('бүгін', 'сегодня') : `${app.wd[nx.at.getDay()]}`} {nx.slot.t}–{nx.slot.end}</b>{nx.slot.kind ? `, ${nx.slot.kind}` : ''}
            </span>
            {nx.slot.room ? (
              <button className="btn ghost" type="button" onClick={() => { setUi({ mapQ: nx.slot.room, mapSel: null }); go('map:campus'); }}>
                <Icon n="pin" s={18} /> {tt(`${nx.slot.room} аудитория — картада`, `Аудитория ${nx.slot.room} на карте`)}
              </button>
            ) : null}
          </>
        ) : <span className="row-sub">{tt('Бұл пән кестеде жоқ.', 'Этого предмета нет в расписании.')}</span>}
      </section>

      {due > 0 ? (
        <section className="card review-due" aria-label={tt('Қайталау', 'Повторение')}>
          <span className="row-title">{tt(`Бүгін қайталау: ${cardsTxt(due, tt)}`, `Повторить сегодня: ${cardsTxt(due, tt)}`)}</span>
          <button className="btn" type="button" onClick={() => go('review:' + s.id)}><Icon n="cards" s={18} /> {tt('Қайталауды бастау', 'Начать повторение')}</button>
        </section>
      ) : null}

      <section className="section" aria-labelledby="subjTasks">
        <SectionHead title={<span id="subjTasks">{tt('Тапсырмалар мен дедлайндар', 'Задания и дедлайны')}</span>}
          more={<button className="link-btn" type="button" onClick={() => go('calendar')}>{tt('Күнтізбе', 'Календарь')}</button>} />
        {deadlines.length || pins.length ? (
          <div className="group">
            {deadlines.map(d => <Row key={d.id} onClick={() => go('calendar')} icon="clock" title={d.title} sub={ymdTxt(d.date, d.time)} />)}
            {pins.map(p => <Row key={p.ref} go={p.ref} icon="calendar" title={L(p.title)} sub={ymdTxt(p.date)} />)}
          </div>
        ) : <p className="small muted">{tt(`Атауында «${s.name}» бар дедлайн осында шығады. Оны күнтізбеге қосыңыз.`, `Здесь появятся дедлайны, в названии которых есть «${s.name}». Добавьте их в календаре.`)}</p>}
      </section>

      <section className="section" aria-labelledby="subjNotes">
        <SectionHead title={<span id="subjNotes">{tt('Конспектілер', 'Конспекты')}</span>}
          more={<button className="link-btn" type="button" onClick={() => openSheet(<NewNoteSheet subject={s.id} />)}>+ {tt('Конспект', 'Конспект')}</button>} />
        {notes.length ? (
          <div className="group">{notes.map(n => (
            <Row key={n.id} go={'notes:' + n.id} icon="doc" title={noteTitle(n, apiLang(S.lang))}
              sub={[L(TEMPLATE_LABEL[n.template]), n.date ? ymdTxt(n.date, n.classT) : ''].filter(Boolean).join(' · ')} />
          ))}</div>
        ) : (
          <button className="btn secondary block" type="button" onClick={() => openSheet(<NewNoteSheet subject={s.id} />)}>
            <Icon n="plus" s={18} /> {tt('Алғашқы конспект', 'Первый конспект')}
          </button>
        )}
      </section>

      <section className="section" aria-labelledby="subjCards">
        <SectionHead title={<span id="subjCards">{tt('Карточкалар', 'Карточки')}</span>}
          more={<button className="link-btn" type="button" onClick={() => go('cards')}>{tt('Барлығы', 'Все')}</button>} />
        {decks.length ? (
          <div className="group">{decks.map(d => <Row key={d.id} go={'deck:' + d.id} icon="cards" title={d.name} sub={cardsTxt(d.cards.length, tt)} />)}</div>
        ) : <p className="small muted">{tt('Конспектіден «Қорытып, карточка жаса» батырмасымен жасаңыз.', 'Создайте из конспекта кнопкой «Итог и карточки».')}</p>}
        {total > 0 && due === 0 ? <p className="tiny muted">{tt('Бүгінге қайталайтын карточка жоқ.', 'На сегодня повторять нечего.')}</p> : null}
      </section>

      {qaOn ? (
        <section className="section" aria-labelledby="subjQa">
          <SectionHead title={<span id="subjQa">{tt('Сұрақ-жауап', 'Вопросы и ответы')}</span>} />
          <Row go={qaBoard(s.name)} icon="chat" title={tt('Курстастардан сұрау', 'Спросить однокурсников')}
            sub={tt('Осы пән бойынша сұрақтар мен жауаптар', 'Вопросы и ответы по этому предмету')} />
        </section>
      ) : null}

      <section className="section" aria-labelledby="subjExam">
        <SectionHead title={<span id="subjExam">{tt('Емтихан', 'Экзамен')}</span>} />
        <div className="card">
          <div className="field">
            <label htmlFor="subjExamDate">{tt('Емтихан күні', 'Дата экзамена')}</label>
            <input className="input" id="subjExamDate" type="date" value={s.exam} onChange={e => setExam(e.target.value)} />
          </div>
          {examLeft != null ? <span className={`badge ${examLeft <= 3 ? 'b-sun' : 'b-sky'}`}>{examLeft >= 0 ? I.daysLeftTxt(examLeft) : tt('Өтті', 'Прошёл')}</span> : null}
          {plan
            ? <Row go={'plan:' + plan.id} icon="calendar" title={tt('Дайындық жоспары', 'План подготовки')} sub={`${plan.items.filter(x => x.done).length}/${plan.items.length}`} />
            : <button className="btn ghost" type="button" onClick={makePlan}><Icon n="calendar" s={18} /> {tt('Дайындық жоспарын құру', 'Составить план подготовки')}</button>}
        </div>
      </section>

      <section className="section" aria-labelledby="subjGrade">
        <SectionHead title={<span id="subjGrade">{tt('Баға', 'Оценка')}</span>} />
        <SubjectScoreRow id={s.id} />
        {grade
          ? <Row go="gpa" icon="chart" title={tt(`Күтілетін баға: ${grade.g}`, `Ожидаемая оценка: ${grade.g}`)} sub={tt(`${grade.cr} кредит · GPA есебі`, `${grade.cr} кредитов · расчёт GPA`)} />
          : <Row onClick={() => { update(x => { x.gpaPlan.push({ n: s.name, cr: 5, g: 'B' }); }); go('gpa'); }} icon="chart"
            title={tt('Бағаны енгізу', 'Ввести оценку')} sub={tt('GPA калькуляторына қосылады', 'Добавится в калькулятор GPA')} />}
      </section>

      {s.custom ? (
        <button className="btn danger" type="button" onClick={() => openSheet(<ConfirmSheet msg={tt('Пәнді тізімнен алып тастау керек пе? Конспектілер сақталады.', 'Убрать предмет из списка? Конспекты сохранятся.')}
          yesLabel={tt('Иә, алып тастау', 'Да, убрать')}
          yes={() => { void saveSubject({ id: s.id, name: s.name, custom: true, exam: s.exam, hidden: true }); toast(tt('Пән алынды', 'Предмет убран')); go('subjects', { replace: true }); }} />)}>
          <Icon n="trash" s={18} /> {tt('Пәнді алып тастау', 'Убрать предмет')}
        </button>
      ) : null}
    </>
  );
}
