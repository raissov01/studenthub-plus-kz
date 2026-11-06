'use client';
/* Spec 04 «Бағалар»: the student's own score sheet in the KZ format (RK1, RK2 → admission rating, exam → final and
   letter), «what do I need on the exam» and absences. Device-only (AppState.scores), works offline, sends nothing.
   /grades lists the subjects; /grades?id=<subject id> is one subject's sheet (lib/routes QUERY_ID). The formula is a
   labelled default (lib/scores.ts GRADING); the student can adjust the exam weight and pass mark for their faculty. */
import { DeskDetail } from '@/components/desk';
import { useDesktop } from '@/lib/desktop';
import { useState, useSyncExternalStore } from 'react';
import { useApp, type App } from '@/components/app';
import { Empty, Icon, Row, SectionHead } from '@/components/ui';
import { useQueryId, useSubjects } from '@/components/views/subjects';
import { saveSubject } from '@/lib/studydb';
import {
  absenceState, admission, admissionPartial, admissionShortfall, emptyEntry, examOf, fillGpa, final, fmtScore, GRADING,
  letter, needTable, nextReachable, parseScore, scoreState, type Grading, type ScoreEntry,
} from '@/lib/scores';
import type { Subject } from '@/lib/subjects';

type TT = App['tt'];

/* ---------- the student's own formula (device-only, beside the scores) ---------- */
const FORMULA_KEY = 'studenthub.grading.v1';
const listeners = new Set<() => void>();
let cached: { raw: string | null; g: Grading } | null = null;
function readGrading(): Grading {
  let raw: string | null = null;
  try { raw = localStorage.getItem(FORMULA_KEY); } catch { /* private mode */ }
  if (cached && cached.raw === raw) return cached.g;
  let g = GRADING;
  try {
    const x = raw ? JSON.parse(raw) as { exam?: number; min?: number } : null;
    if (x && typeof x.exam === 'number' && x.exam >= 0.2 && x.exam <= 0.6 && typeof x.min === 'number' && x.min >= 40 && x.min <= 60) {
      g = { ...GRADING, exam: x.exam, admission: Math.round((1 - x.exam) * 100) / 100, minAdmission: x.min, minExam: x.min, minFinal: x.min };
    }
  } catch { /* damaged: defaults */ }
  cached = { raw, g };
  return g;
}
function saveGrading(v: { exam: number; min: number } | null) {
  try { if (v) localStorage.setItem(FORMULA_KEY, JSON.stringify(v)); else localStorage.removeItem(FORMULA_KEY); } catch { /* ignore */ }
  listeners.forEach(l => l());
}
export function useGrading(): Grading {
  return useSyncExternalStore(l => { listeners.add(l); return () => { listeners.delete(l); }; }, readGrading, () => GRADING);
}
const isDefault = (g: Grading) => g.exam === GRADING.exam && g.minFinal === GRADING.minFinal;

/** subjects with a sheet: the timetable/own subjects, plus entries whose subject has gone (kept by their saved name) */
function useGradeSubjects(): { list: { id: string; name: string; names: string[] }[]; ready: boolean } {
  const { S } = useApp();
  const { list, study } = useSubjects();
  const ids = new Set(list.map(s => s.id));
  const extra = S.scores.filter(e => !ids.has(e.subject)).map(e => ({ id: e.subject, name: e.name || e.subject, names: [e.name || e.subject] }));
  return { list: [...list.map((s: Subject) => ({ id: s.id, name: s.name, names: s.names })), ...extra], ready: study.ready };
}

function summary(e: ScoreEntry | undefined, g: Grading, tt: TT): string {
  if (!e) return tt('Балл енгізілмеген', 'Баллы не введены');
  const parts: string[] = [];
  const r = admission(e, g);
  const f = final(e, g);
  if (r != null) parts.push(admissionPartial(e) ? tt(`АБ1 ғана: ${fmtScore(r)}`, `только РК1: ${fmtScore(r)}`) : `R ${fmtScore(r)}`);
  if (f != null) parts.push(`F ${fmtScore(f)} · ${letter(f, g)}`);
  else if (r != null && !admissionPartial(e)) parts.push(scoreState(e, g) === 'not_admitted' ? tt('рұқсат жоқ', 'нет допуска') : tt('емтихан күтілуде', 'ждёт экзамена'));
  if (e.absences || e.absenceLimit) parts.push(tt(`жоқ: ${e.absences}${e.absenceLimit ? ' / ' + e.absenceLimit : ''}`, `пропуски: ${e.absences}${e.absenceLimit ? ' / ' + e.absenceLimit : ''}`));
  return parts.join(' · ') || tt('Балл енгізілмеген', 'Баллы не введены');
}

export function GradesScreen() {
  const id = useQueryId();
  return id ? <GradeSheet id={id} /> : <GradeList />;
}

function GradeList() {
  const { S, tt, go, toast } = useApp();
  const desktop = useDesktop();
  const g = useGrading();
  const { list, ready } = useGradeSubjects();
  const [name, setName] = useState('');
  const add = () => {
    const n = name.trim().slice(0, 120);
    if (!n) return;
    const dup = list.find(s => s.names.some(x => x.toLowerCase() === n.toLowerCase()));
    if (dup) { go('grades:' + dup.id); return; }
    const id = 'c' + Date.now().toString(36);
    void saveSubject({ id, name: n, custom: true, exam: '' });
    setName('');
    toast(tt('Пән қосылды', 'Предмет добавлен'));
  };
  const intro = (
      <p className="small muted">{tt('Аралық бақылау (АБ), емтихан мен қорытынды балл және сабақтан қалғаныңыз — өз есебіңіз. Деректер тек осы құрылғыда.',
        'Рубежные контроли (РК), экзамен, итог и пропуски — ваш личный учёт. Данные только на этом устройстве.')}</p>
  );
  const listEl = (
    <>
      {!ready ? <p className="small muted" role="status">{tt('Жүктелуде…', 'Загрузка…')}</p> : list.length ? (
        <div className={`group${desktop ? ' desk-cards' : ''}`} id="gradeList">
          {list.map(s => {
            const e = S.scores.find(x => x.subject === s.id);
            const abs = e ? absenceState(e) : 'none';
            return <Row key={s.id} go={'grades:' + s.id} icon="grades" tone={abs === 'limit' ? 'bad' : ''} title={s.name} sub={summary(e, g, tt)} />;
          })}
        </div>
      ) : <Empty icon="grades">{tt('Пәндер сабақ кестесінен алынады. Кесте әлі жоқ болса, пәнді төменде өзіңіз қосыңыз.', 'Предметы берутся из расписания. Если его пока нет, добавьте предмет ниже.')}</Empty>}
    </>
  );
  const side = (
    <>
      <form className="card form" onSubmit={e => { e.preventDefault(); add(); }}>
        <div className="field">
          <label htmlFor="gradeNew">{tt('Пән қосу', 'Добавить предмет')}</label>
          <input className="input" id="gradeNew" value={name} maxLength={120} onChange={e => setName(e.target.value)} placeholder={tt('мысалы: Экология', 'например: Экология')} />
        </div>
        <button className="btn" type="submit" disabled={!name.trim()}><Icon n="plus" s={18} /> {tt('Қосу', 'Добавить')}</button>
      </form>
      <FormulaCard g={g} />
      {desktop ? <div className="group"><Row go="gpa" icon="calc" title={tt('GPA калькуляторы', 'Калькулятор GPA')} sub={tt('Қорытынды бағаларды GPA-ға толтыру', 'Заполнить GPA итоговыми оценками')} /></div>
        : <Row go="gpa" icon="calc" title={tt('GPA калькуляторы', 'Калькулятор GPA')} sub={tt('Қорытынды бағаларды GPA-ға толтыру', 'Заполнить GPA итоговыми оценками')} />}
    </>
  );
  // a laptop: the subjects' scores as tiles; adding a subject, the formula and GPA on the right
  if (desktop) return <DeskDetail label={tt('Баға есебі', 'Учёт оценок')} main={<>{intro}{listEl}</>} rail={side} />;
  return <>{intro}{listEl}{side}</>;
}

function FormulaCard({ g }: { g: Grading }) {
  const { tt, L } = useApp();
  const [exam, setExam] = useState(String(Math.round(g.exam * 100)));
  const [min, setMin] = useState(String(g.minFinal));
  const [err, setErr] = useState('');
  const save = () => {
    const e = Number(exam), m = Number(min);
    if (!Number.isInteger(e) || e < 20 || e > 60 || !Number.isInteger(m) || m < 40 || m > 60) {
      setErr(tt('Емтихан үлесі 20–60 %, өту шегі 40–60 балл.', 'Доля экзамена 20–60 %, порог 40–60 баллов.'));
      return;
    }
    setErr('');
    saveGrading(e === Math.round(GRADING.exam * 100) && m === GRADING.minFinal ? null : { exam: e / 100, min: m });
  };
  return (
    <details className="card" id="gradeFormula">
      <summary className="row-title">{tt('Есептеу формуласы', 'Формула расчёта')}{GRADING.confirmed ? null : <span className="badge b-sun" style={{ marginInlineStart: 8 }}>{tt('расталмаған', 'не подтверждена')}</span>}</summary>
      <p className="small">{tt(`R = (АБ1 + АБ2) / 2; қорытынды = ${fmtScore(g.admission * 100)} % · R + ${fmtScore(g.exam * 100)} % · емтихан. Рұқсат пен өту шегі: ${g.minFinal}.`,
        `R = (РК1 + РК2) / 2; итог = ${fmtScore(g.admission * 100)} % · R + ${fmtScore(g.exam * 100)} % · экзамен. Допуск и порог: ${g.minFinal}.`)}</p>
      <p className="tiny muted">{L(GRADING.note)} {isDefault(g) ? '' : tt('Сіз өзгерткен мәндер қолданылуда.', 'Используются ваши значения.')}</p>
      <div className="grid-2">
        <div className="field"><label htmlFor="gfExam">{tt('Емтихан үлесі, %', 'Доля экзамена, %')}</label>
          <input className="input num" id="gfExam" inputMode="numeric" value={exam} onChange={e => setExam(e.target.value)} /></div>
        <div className="field"><label htmlFor="gfMin">{tt('Өту шегі', 'Проходной балл')}</label>
          <input className="input num" id="gfMin" inputMode="numeric" value={min} onChange={e => setMin(e.target.value)} /></div>
      </div>
      <div className="btn-row">
        <button className="btn secondary" type="button" onClick={save}>{tt('Сақтау', 'Сохранить')}</button>
        {isDefault(g) ? null : <button className="btn ghost" type="button" onClick={() => { saveGrading(null); setExam(String(Math.round(GRADING.exam * 100))); setMin(String(GRADING.minFinal)); }}>{tt('Әдепкіге қайтару', 'Сбросить')}</button>}
      </div>
      {err ? <p className="tiny urgent" role="alert">{err}</p> : null}
    </details>
  );
}

function ScoreField({ id, label, value, onSave, disabled }: { id: string; label: string; value: number | null; onSave: (v: number | null) => void; disabled?: boolean }) {
  const { tt } = useApp();
  const [text, setText] = useState(value == null ? '' : String(value));
  const [bad, setBad] = useState(false);
  const commit = (raw: string) => {
    const v = parseScore(raw);
    if (v === 'invalid') { setBad(true); return; }
    setBad(false);
    if (v !== value) onSave(v);
  };
  return (
    <div className="field">
      <label htmlFor={id}>{label}</label>
      <input className="input num" id={id} inputMode="decimal" autoComplete="off" value={text} disabled={disabled} aria-invalid={bad}
        aria-describedby={bad ? id + 'Err' : undefined}
        onChange={e => { setText(e.target.value); commit(e.target.value); }} onBlur={e => commit(e.target.value)} />
      {bad ? <span className="tiny urgent" id={id + 'Err'} role="alert">{tt('0–100, бір ондық таңбаға дейін', 'От 0 до 100, не больше одного знака после запятой')}</span> : null}
    </div>
  );
}

function GradeSheet({ id }: { id: string }) {
  const { S, tt, go, update } = useApp();
  const g = useGrading();
  const { list, ready } = useGradeSubjects();
  const s = list.find(x => x.id === id);
  if (!ready) return <p className="small muted" role="status">{tt('Жүктелуде…', 'Загрузка…')}</p>;
  if (!s) return <Empty icon="grades" action={<button className="btn secondary" type="button" onClick={() => go('grades', { replace: true })}>{tt('Бағалар', 'Оценки')}</button>}>{tt('Пән табылмады.', 'Предмет не найден.')}</Empty>;
  const e = S.scores.find(x => x.subject === id) ?? emptyEntry(id, s.name);
  const ro = e.source === 'import';
  const set = (patch: Partial<ScoreEntry>) => update(st => {
    const i = st.scores.findIndex(x => x.subject === id);
    const next = { ...(i >= 0 ? st.scores[i] : emptyEntry(id, s.name)), ...patch, name: s.name, updated: Date.now() };
    if (i >= 0) st.scores[i] = next; else st.scores.push(next);
  });
  const state = scoreState(e, g);
  const r = admission(e, g), f = final(e, g);
  const abs = absenceState(e);
  const reach = nextReachable(e, g);
  return (
    <>
      <section className="card" aria-labelledby="gradeTitle">
        <span className="eyebrow">{tt('Өз есебіңіз, ресми журнал емес', 'Ваш личный учёт, не официальный журнал')}</span>
        <h1 id="gradeTitle" className="subj-title">{s.name}</h1>
        <div className="stat-grid">
          <div className="stat"><b className="num" id="gradeR">{fmtScore(r)}</b><span>{admissionPartial(e) ? tt('рейтинг (АБ1 ғана)', 'рейтинг (только РК1)') : tt('рұқсат рейтингі', 'рейтинг допуска')}</span></div>
          <div className="stat"><b className="num" id="gradeF">{f == null ? '—' : `${fmtScore(f)} · ${letter(f, g)}`}</b><span>{tt('қорытынды', 'итог')}</span></div>
        </div>
        <StateLine e={e} g={g} state={state} />
      </section>

      <section className="section" aria-labelledby="gradeScores">
        <SectionHead title={<span id="gradeScores">{tt('Баллдар', 'Баллы')}</span>} />
        <div className="card">
          <div className="grid-2">
            <ScoreField id="gRk1" label={tt('АБ1 (РК1)', 'РК1')} value={e.rk1} disabled={ro} onSave={v => set({ rk1: v })} />
            <ScoreField id="gRk2" label={tt('АБ2 (РК2)', 'РК2')} value={e.rk2} disabled={ro} onSave={v => set({ rk2: v })} />
            <ScoreField id="gExam" label={tt('Емтихан', 'Экзамен')} value={e.exam} disabled={ro} onSave={v => set({ exam: v })} />
            <ScoreField id="gRetake" label={tt('Қайта тапсыру (FX)', 'Пересдача (FX)')} value={e.retake ?? null} disabled={ro} onSave={v => set({ retake: v })} />
          </div>
          <p className="tiny muted">{tt('Әр балл 0–100. Қайта тапсырсаңыз, соңғы нәтиже есептеледі.', 'Каждый балл 0–100. При пересдаче считается последний результат.')}</p>
        </div>
      </section>

      {r != null && !admissionPartial(e) && state !== 'not_admitted' && examOf(e) == null ? (
        <section className="section" aria-labelledby="gradeNeed">
          <SectionHead title={<span id="gradeNeed">{tt('Емтиханнан қанша керек', 'Сколько нужно на экзамене')}</span>} />
          <div className="group table-wrap"><table id="gradeNeedTable"><thead><tr><th>{tt('Баға', 'Оценка')}</th><th className="r">{tt('Емтихан', 'Экзамен')}</th></tr></thead>
            <tbody>{needTable(e, g).map(row => (
              <tr key={row.letter}><td><b>{row.letter}</b></td><td className="r num">{row.need == null ? tt('жетпейді', 'недостижимо') : tt(`кемінде ${fmtScore(row.need)}`, `не меньше ${fmtScore(row.need)}`)}</td></tr>
            ))}</tbody></table></div>
          {reach ? <p className="small">{tt(`Ең жақын қолжетімді баға — ${reach.letter}: емтиханнан кемінде ${fmtScore(reach.need)} балл.`, `Ближайшая достижимая оценка — ${reach.letter}: на экзамене нужно не меньше ${fmtScore(reach.need)}.`)}</p> : null}
        </section>
      ) : null}

      <section className="section" aria-labelledby="gradeAbs">
        <SectionHead title={<span id="gradeAbs">{tt('Сабақтан қалу', 'Пропуски')}</span>} />
        <div className="card">
          <div className="card-row" style={{ alignItems: 'center', justifyContent: 'space-between', gap: 8 }}>
            <span className="big-num num" id="gradeAbsCount">{e.absences}{e.absenceLimit ? ` / ${e.absenceLimit}` : ''}</span>
            <div className="btn-row" style={{ flex: 'none' }}>
              <button className="btn ghost" type="button" disabled={ro || e.absences <= 0} aria-label={tt('Бір қалуды алып тастау', 'Убрать один пропуск')} onClick={() => set({ absences: Math.max(0, e.absences - 1) })}>−1</button>
              <button className="btn secondary" type="button" disabled={ro} onClick={() => set({ absences: Math.min(999, e.absences + 1) })}>{tt('+1 жоқ', '+1 пропуск')}</button>
            </div>
          </div>
          <LimitField value={e.absenceLimit} disabled={ro} onSave={v => set({ absenceLimit: v })} />
          {abs === 'limit' ? <p className="small" role="alert" id="gradeAbsLimit">{tt('Қалу шегіне жеттіңіз. Эдвайзерге не деканатқа хабарласып, себебін (анықтама болса) айтыңыз — көбіне жұмыс істеп шығуға болады.', 'Достигнут лимит пропусков. Напишите эдвайзеру или в деканат и объясните причину (если есть справка) — часто пропуски можно отработать.')}</p>
            : abs === 'near' ? <p className="small">{tt('Шекке бір сабақ қалды.', 'До лимита остался один пропуск.')}</p> : null}
        </div>
      </section>
      <p className="note">{tt('Бұл өз есебіңіз, ресми журнал емес. Ресми бағаны университеттің жүйесінен тексеріңіз.', 'Это ваш личный учёт, не официальный журнал. Официальные оценки проверяйте в системе университета.')}</p>
      <button className="btn ghost" type="button" onClick={() => go('subjects:' + id)}><Icon n="book" s={18} /> {tt('Пән беті', 'Страница предмета')}</button>
    </>
  );
}

function LimitField({ value, disabled, onSave }: { value: number | null; disabled?: boolean; onSave: (v: number | null) => void }) {
  const { tt } = useApp();
  const [text, setText] = useState(value == null ? '' : String(value));
  const commit = (raw: string) => {
    const t = raw.trim();
    const n = t === '' ? null : Number(t);
    if (n !== null && (!Number.isInteger(n) || n < 1 || n > 999)) { setText(value == null ? '' : String(value)); return; }
    if (n !== value) onSave(n);
  };
  return (
    <div className="field">
      <label htmlFor="gAbsLimit">{tt('Рұқсат етілген шек (білмесеңіз, бос қалдырыңыз)', 'Допустимый лимит (если не знаете, оставьте пустым)')}</label>
      <input className="input num" id="gAbsLimit" inputMode="numeric" value={text} disabled={disabled} onChange={e => setText(e.target.value)} onBlur={e => commit(e.target.value)} />
    </div>
  );
}

function StateLine({ e, g, state }: { e: ScoreEntry; g: Grading; state: ReturnType<typeof scoreState> }) {
  const { tt } = useApp();
  const f = final(e, g);
  switch (state) {
    case 'empty': return <p className="small muted">{tt('АБ1 мен АБ2 баллын енгізіңіз — рейтинг пен емтихан талабы бірден шығады.', 'Введите баллы РК1 и РК2 — рейтинг и требования к экзамену появятся сразу.')}</p>;
    case 'partial': return <p className="small muted">{tt('Екінші АБ енгізілгенде рұқсат рейтингі мен емтихан талабы есептеледі.', 'Когда будет второй РК, посчитаем рейтинг допуска и требования к экзамену.')}</p>;
    case 'not_admitted': return <p className="small" role="status" id="gradeState">{tt(`Емтиханға рұқсат жоқ: рейтинг ${g.minAdmission}-ден төмен, АБ бойынша ${fmtScore(admissionShortfall(e, g))} балл жетпейді. Оқытушымен не эдвайзермен қайта тапсыру мүмкіндігін сөйлесіңіз.`,
      `Нет допуска к экзамену: рейтинг ниже ${g.minAdmission}, по РК не хватает ${fmtScore(admissionShortfall(e, g))} балла. Обсудите с преподавателем или эдвайзером возможность пересдачи.`)}</p>;
    case 'awaiting_exam': return <p className="small" id="gradeState">{tt('Рұқсат бар, емтихан күтілуде.', 'Допуск есть, экзамен впереди.')}</p>;
    case 'exam_failed': return <p className="small" role="status" id="gradeState">{tt(`Емтихан ${g.minExam}-ден төмен — пән жабылмады. Қайта тапсыру (FX) нәтижесін жоғарыға енгізіңіз.`, `Экзамен ниже ${g.minExam} — предмет не закрыт. Внесите результат пересдачи (FX) выше.`)}</p>;
    case 'failed': return <p className="small" role="status" id="gradeState">{tt('Қорытынды өту шегінен төмен.', 'Итог ниже проходного.')}</p>;
    default: return <p className="small" id="gradeState">{tt(`Пән жабылды: ${letter(f!, g)}.`, `Предмет закрыт: ${letter(f!, g)}.`)}</p>;
  }
}

/** the subject page's grade block (spec 04 G3): R/F and absences when the sheet has them */
export function SubjectScoreRow({ id }: { id: string }) {
  const { S, tt } = useApp();
  const g = useGrading();
  const e = S.scores.find(x => x.subject === id);
  if (!e) return <Row go={'grades:' + id} icon="grades" title={tt('Бағалар парағы', 'Лист оценок')} sub={tt('АБ, емтихан, сабақтан қалу', 'РК, экзамен, пропуски')} />;
  return <Row go={'grades:' + id} icon="grades" title={tt('Бағалар парағы', 'Лист оценок')} sub={summary(e, g, tt)} />;
}

/** GPA «Бағалардан толтыру» (spec 04 G4) */
export function FillGpaButton() {
  const { S, tt, go, update, toast } = useApp();
  const g = useGrading();
  const { list } = useGradeSubjects();
  const names = (e: ScoreEntry) => list.find(s => s.id === e.subject)?.names ?? [e.name];
  const run = () => {
    const r = fillGpa(S.gpaPlan, S.scores, names, g);
    if (!r.changed) { toast(tt('Қорытынды балы бар пән әзірге жоқ', 'Пока нет предметов с итоговым баллом')); return; }
    update(s => { s.gpaPlan = r.plan; });
    toast(tt(`${r.changed} пәннің бағасы жаңартылды`, `Обновлено оценок: ${r.changed}`));
  };
  return (
    <div className="btn-row">
      <button className="btn secondary" type="button" onClick={run}><Icon n="check" s={18} /> {tt('Бағалардан толтыру', 'Заполнить из оценок')}</button>
      <button className="btn ghost" type="button" onClick={() => go('grades')}><Icon n="chart" s={18} /> {tt('Бағалар парағы', 'Лист оценок')}</button>
    </div>
  );
}
