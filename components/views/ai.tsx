'use client';
/* AI helper and offline study tools: chat, résumé, interview practice, exam plans, flashcards, job match. */
import { useEffect, useLayoutEffect, useRef, useState, type ReactNode } from 'react';
import { useApp, copyText, type App } from '@/components/app';
import { track } from '@/features/analytics/track';
import { ConfirmSheet, Icon, Row, Seg } from '@/components/ui';
import { PlanRow } from '@/components/views/opps';
import { CourseSheet } from '@/components/views/sheets';
import { COURSES, INTERVIEW, SKILLS, SPECS } from '@/lib/data/static';
import { allDecks, buildPlan, cardsFromText, matchJob, planDay, resumeText } from '@/lib/domain';
import { dOff, diffDays, isoLocal, plural } from '@/lib/format';
import { getState, getUi, setUi, update } from '@/lib/store';
import type { IconName } from '@/lib/types';

/* ---------- chat ---------- */
/** send a message to /api/chat with the last 16 messages and a few profile fields; errors show the server's text */
export async function sendChat(text: string, lang: 'kk' | 'ru') {
  text = (text || '').trim();
  if (!text || getUi().chatBusy) return;
  const tt = (kk: string, ru: string) => (lang === 'ru' ? ru : kk);
  update(s => { s.chat.push({ role: 'user', content: text }); });
  track('ai_message_sent'); // the count only, never the text
  setUi({ chatDraft: '', chatErr: '', chatBusy: true });
  const S = getState(), p = S.profile;
  const msgs = S.chat.slice(-16).map(m => ({ role: m.role, content: m.content }));
  while (msgs.length && msgs[0].role !== 'user') msgs.shift();
  let err = '';
  try {
    const r = await fetch('/api/chat', {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ lang, messages: msgs, profile: { name: p.name, spec: p.spec, course: p.course, gpa: p.gpa, en: p.en, skills: p.skills, exp: p.exp } }),
    });
    const data: { text?: string; error?: { code?: string; message?: string } } = await r.json().catch(() => ({}));
    if (!r.ok) {
      err = data?.error?.message ? data.error.message
        : r.status === 503 ? tt('AI чат уақытша қолжетімсіз.', 'AI-чат временно недоступен.')
          : tt('Қате: ', 'Ошибка: ') + 'HTTP ' + r.status;
    } else {
      const txt = (data.text || '').trim();
      update(s => { s.chat.push({ role: 'assistant', content: txt || '…' }); });
    }
  } catch {
    err = tt('Байланыс жоқ. Интернетті тексеріңіз.', 'Нет соединения. Проверьте интернет.');
  }
  setUi({ chatBusy: false, chatErr: err });
}
const toChat = (app: App, prompt: string) => { app.setUi({ chatDraft: prompt }); app.go('chat'); };

export function Ai() {
  const app = useApp();
  const { tt, go, S } = app;
  const tools: [string, IconName, string, string][] = [
    ['resume', 'doc', tt('Түйіндеме құрастыру', 'Конструктор резюме'), tt('Профиліңізден қазақша, орысша, ағылшынша', 'Из профиля: на казахском, русском, английском')],
    ['interview', 'mic', tt('Сұхбатқа дайындық', 'Подготовка к собеседованию'), tt('Жиі қойылатын 10 сұрақ және жаттығу', '10 частых вопросов и тренировка')],
    ['exam', 'calendar', tt('Емтиханға дайында', 'Подготовь меня к экзамену'), tt('Күн бойынша жоспар және тексеру', 'План по дням и самопроверка')],
    ['cards', 'cards', tt('Карточкалар және тест', 'Карточки и тесты'), tt('Қате жауаптарды қайталау режимі', 'Повторение ошибок')],
    ['match', 'target', tt('Вакансияға сәйкестік', 'Совпадение с вакансией'), tt('Күшті жақтар және не жетіспейді', 'Сильные стороны и что подтянуть')],
    ['opps:courses', 'book', tt('Мамандығыма арналған курстар', 'Курсы для моей профессии'), tt('Пайдалы тегін курстар', 'Полезные бесплатные курсы')],
  ];
  const starters = [
    tt('Маған тәжірибесіз түйіндеме жазып бер', 'Напиши мне резюме без опыта'),
    tt('Банкке тағылымдамаға сұхбат өткізіп көр', 'Проведи со мной пробное собеседование в банк'),
    tt('GPA-ымды 3,5-ке қалай көтеремін?', 'Как поднять GPA до 3,5?'),
    tt('Дәріс мәтінінен 5 тест сұрағын құрастыр', 'Составь 5 тестовых вопросов по тексту лекции'),
  ];
  return (
    <>
      <section className="hero">
        <div className="hero-top"><span className="avatar"><Icon n="spark" s={26} /></span>
          <div style={{ minWidth: 0 }}><h1>Student Hub AI</h1><p className="muted small">{tt('Чат қосулы', 'Чат подключён')}</p></div></div>
        <p className="small" style={{ color: 'var(--head-muted)' }}>{tt('Түйіндеме, сұхбат, дәріс конспектісі және емтиханға дайындық — бір жерде.', 'Резюме, собеседование, конспекты лекций и подготовка к экзамену — в одном месте.')}</p>
        <button className="btn sun" type="button" onClick={() => go('chat')}><Icon n="chat" s={18} /> {tt('Чатты ашу', 'Открыть чат')}</button>
      </section>
      <section className="section"><h2>{tt('Тез сұрақтар', 'Быстрые вопросы')}</h2>
        <div className="chips wrap">{starters.map(s => (
          <button key={s} className="chip" type="button" onClick={() => { setUi({ chatDraft: '' }); go('chat'); void sendChat(s, S.lang); }}>{s}</button>
        ))}</div></section>
      <section className="section"><h2>{tt('Құралдар', 'Инструменты')}</h2>
        <div className="group">{tools.map(([g, i, t, s]) => <Row key={g} go={g} icon={i} title={t} sub={s} />)}</div>
        <p className="tiny muted">{tt('Құралдар интернетсіз жұмыс істейді. AI чатқа интернет керек.', 'Инструменты работают без интернета. AI-чату нужен интернет.')}</p>
      </section>
    </>
  );
}

/** **bold** and markdown headings, nothing else (the legacy mdLite) */
function MdLite({ s }: { s: string }) {
  const out: ReactNode[] = [];
  s.split('\n').forEach((line, li) => {
    if (li) out.push('\n');
    const h = line.match(/^#{1,4}\s*(.+)$/);
    if (h) { out.push(<b key={`h${li}`}>{h[1]}</b>); return; }
    line.split(/\*\*(.+?)\*\*/g).forEach((part, i) => out.push(i % 2 ? <b key={`${li}-${i}`}>{part}</b> : part));
  });
  return <>{out}</>;
}

export function Chat() {
  const { S, ui, tt, setUi, update: upd, toast } = useApp();
  const ta = useRef<HTMLTextAreaElement>(null);
  const grow = () => { const t = ta.current; if (!t) return; t.style.height = 'auto'; t.style.height = Math.min(140, t.scrollHeight + 2) + 'px'; };
  useLayoutEffect(grow, [ui.chatDraft]);
  useEffect(() => { window.scrollTo(0, document.body.scrollHeight); }, [S.chat.length, ui.chatBusy, ui.chatErr]);
  const msgs = S.chat;
  return (
    <>
      <div className="chat" id="chat">
        {msgs.length ? null : <div className="msg ai">{tt(`Сәлем, ${S.profile.name}! Мен Student Hub AI-мын. Түйіндеме, сұхбат, оқу немесе мансап туралы сұраңыз.`, `Привет, ${S.profile.name}! Я Student Hub AI. Спрашивай про резюме, собеседование, учёбу или карьеру.`)}</div>}
        {msgs.map((m, i) => <div key={i} className={`msg ${m.role === 'user' ? 'me' : 'ai'}`}>{m.role === 'user' ? m.content : <MdLite s={m.content} />}</div>)}
        {ui.chatBusy ? <div className="msg ai"><span className="typing"><i></i><i></i><i></i></span></div> : null}
        {ui.chatErr ? <div className="msg err">{ui.chatErr}</div> : null}
      </div>
      <form className="composer" id="composer" onSubmit={e => { e.preventDefault(); void sendChat(ta.current?.value || '', S.lang); }}>
        <textarea ref={ta} className="input" id="chatIn" rows={1} maxLength={2000} placeholder={tt('Сұрағыңызды жазыңыз…', 'Напишите вопрос…')} value={ui.chatDraft} onChange={e => setUi({ chatDraft: e.target.value })} />
        <button className="btn" type="submit" aria-label={tt('Жіберу', 'Отправить')} disabled={ui.chatBusy}><Icon n="send" s={20} /></button>
      </form>
      {msgs.length ? <button className="link-btn" type="button" onClick={() => { upd(s => { s.chat = []; }); setUi({ chatErr: '' }); toast(tt('Чат тазаланды', 'Чат очищен')); }}>{tt('Чатты тазалау', 'Очистить чат')}</button> : null}
    </>
  );
}

/* ---------- résumé ---------- */
export function Resume() {
  const app = useApp();
  const { S, ui, I, tt, go, setUi, toast } = app;
  const lang = (ui.resumeLang || S.lang) as 'kk' | 'ru' | 'en';
  const text = resumeText(lang, S, I);
  return (
    <>
      <p className="small muted">{tt('Түйіндеме профиліңіз бен портфолиоңыздан жиналады. Деректерді өзгерту үшін профильді түзетіңіз.', 'Резюме собирается из профиля и портфолио. Чтобы изменить данные, отредактируйте профиль.')}</p>
      <Seg items={[['kk', 'Қазақша'], ['ru', 'Русский'], ['en', 'English']]} cur={lang} onPick={v => setUi({ resumeLang: v })} />
      <div className="pre" id="resumeText">{text}</div>
      <div className="btn-row">
        <button className="btn" type="button" onClick={() => copyText(text, toast, tt)}><Icon n="copy" s={18} /> {tt('Көшіру', 'Скопировать')}</button>
        <button className="btn ghost" type="button" onClick={() => go('edit')}><Icon n="edit" s={18} /> {tt('Профильді түзету', 'Изменить профиль')}</button>
      </div>
      <button className="btn secondary block" type="button" onClick={() => toChat(app, tt('Мына түйіндемені жұмыс берушіге тартымды етіп жақсартып бер, құрылымын сақта:\n\n', 'Улучши это резюме, чтобы оно было убедительнее для работодателя, сохрани структуру:\n\n') + text)}>
        <Icon n="spark" s={18} /> {tt('AI арқылы жақсарту', 'Улучшить с помощью AI')}
      </button>
    </>
  );
}

/* ---------- interview practice ---------- */
export function Interview() {
  const app = useApp();
  const { ui, tt, L, setUi } = app;
  const i = ui.iv % INTERVIEW.length, q = INTERVIEW[i];
  const ans = useRef<HTMLTextAreaElement>(null);
  const [startedAt, setStartedAt] = useState(() => Date.now());
  const [feedback, setFeedback] = useState<null | { notes: [string, string][]; secs: number }>(null);
  const check = () => {
    const a = (ans.current?.value || '').trim();
    const words = a ? a.split(/\s+/).length : 0;
    const secs = Math.round((Date.now() - startedAt) / 1000);
    const notes: [string, string][] = [];
    if (!words) notes.push(['no', tt('Жауап бос. Алдымен жазып көріңіз.', 'Ответ пустой. Сначала попробуйте написать.')]);
    else {
      notes.push(words < 25 ? ['mid', tt(`Жауап қысқа (${words} сөз). Нақты мысал қосыңыз.`, `Ответ короткий (${words} слов). Добавьте конкретный пример.`)]
        : words > 180 ? ['mid', tt(`Жауап ұзын (${words} сөз). 1–2 минутқа қысқартыңыз.`, `Ответ длинный (${words} слов). Сократите до 1–2 минут.`)]
          : ['ok', tt(`Көлемі жақсы (${words} сөз).`, `Хороший объём (${words} слов).`)]);
      notes.push(/\d/.test(a) ? ['ok', tt('Нақты сандар бар — бұл сенімді көрінеді.', 'Есть конкретные цифры — это убедительно.')] : ['mid', tt('Сан қосыңыз: мерзім, нәтиже, адам саны.', 'Добавьте цифры: сроки, результат, количество людей.')]);
      if (/(нәтиже|результат|соның арқасында|в итоге|удалось|қол жеткіздім)/i.test(a)) notes.push(['ok', tt('Нәтиже айтылған.', 'Результат назван.')]);
      else notes.push(['mid', tt('Соңында нәтижені айтыңыз (STAR әдісі).', 'Закончите результатом (метод STAR).')]);
    }
    setFeedback({ notes, secs });
  };
  const next = () => {
    setUi({ iv: (ui.iv + 1) % INTERVIEW.length });
    if (ans.current) ans.current.value = '';
    setFeedback(null);
    setStartedAt(Date.now());
  };
  return (
    <>
      <div className="card">
        <span className="eyebrow num">{tt('Сұрақ', 'Вопрос')} {i + 1} / {INTERVIEW.length}</span>
        <h2>{L(q.q)}</h2>
        <div className="field"><label htmlFor="ivAns">{tt('Жауабыңыз', 'Ваш ответ')}</label><textarea ref={ans} className="input" id="ivAns" placeholder={tt('Жазыңыз немесе дауыстап айтып, қысқаша жазып алыңыз…', 'Напишите или проговорите вслух и кратко запишите…')}></textarea></div>
        <div id="ivFeedback">
          {feedback ? (
            <div>
              {feedback.notes.map(([k, t], n) => <div key={n} className="res"><span className={`dot ${k}`}>{k === 'ok' ? '✓' : '!'}</span><span className="small">{t}</span></div>)}
              <div className="res"><span className="dot mid">i</span><span className="small"><b>{tt('Кеңес', 'Совет')}:</b> {L(q.tip)}</span></div>
              <p className="tiny muted num">{tt('Уақыт', 'Время')}: {Math.floor(feedback.secs / 60)}:{String(feedback.secs % 60).padStart(2, '0')}</p>
            </div>
          ) : null}
        </div>
        <div className="btn-row">
          <button className="btn" type="button" onClick={check}>{tt('Тексеру', 'Проверить')}</button>
          <button className="btn ghost" type="button" onClick={next}>{tt('Келесі сұрақ', 'Следующий вопрос')}</button>
        </div>
        <button className="link-btn" type="button" onClick={() => {
          const a = (ans.current?.value || '').trim();
          toChat(app, tt(`Сұхбат сұрағы: «${L(q.q)}»\nМенің жауабым: «${a || '…'}»\nЖауабымды 10 балдық шкаламен бағалап, қалай жақсартуға болатынын айт.`, `Вопрос собеседования: «${L(q.q)}»\nМой ответ: «${a || '…'}»\nОцени мой ответ по 10-балльной шкале и подскажи, как его улучшить.`));
        }}><Icon n="spark" s={15} /> {tt('AI-дан баға алу', 'Получить оценку от AI')}</button>
      </div>
      <section className="section"><h2>{tt('Барлық сұрақтар мен кеңестер', 'Все вопросы и советы')}</h2>
        <div className="group">{INTERVIEW.map((x, k) => (
          <details key={k} className="row" style={{ display: 'block' }}><summary className="row-title" style={{ cursor: 'pointer' }}>{k + 1}. {L(x.q)}</summary><p className="row-sub" style={{ marginTop: 6 }}>{L(x.tip)}</p></details>
        ))}</div>
      </section>
    </>
  );
}

/* ---------- exam plans ---------- */
export function Exam() {
  const { S, tt, I, go, toast, update: upd } = useApp();
  const plans = S.plans;
  return (
    <>
      {plans.length ? <section className="section"><h2>{tt('Менің жоспарларым', 'Мои планы')}</h2><div className="group">{plans.map(pl => <PlanRow key={pl.id} pl={pl} sep=" ·" tone />)}</div></section> : null}
      <section className="card">
        <h2>{tt('Мені емтиханға дайында', 'Подготовь меня к экзамену')}</h2>
        <p className="small muted">{tt('Пәнді, емтихан күнін және дәрістер санын енгізіңіз — тақырыптарды күндерге бөліп, тест күндерін қосамыз.', 'Укажите предмет, дату экзамена и число лекций — распределим темы по дням и добавим дни проверки.')}</p>
        <form className="form" id="planForm" onSubmit={e => {
          e.preventDefault();
          const f = e.currentTarget, val = (id: string) => f.querySelector<HTMLInputElement>('#' + id)?.value || '';
          const subj = val('pSubj').trim() || tt('Пән', 'Предмет');
          const date = val('pDate');
          const n = Math.max(1, Math.min(60, parseInt(val('pLec'), 10) || 1));
          const hard = val('pHard').trim();
          const pl = date ? buildPlan(subj, date + 'T00:00:00', n, hard, I) : null;
          if (!pl) { toast(tt('Емтихан күні ертеңнен кейін болуы керек', 'Дата экзамена должна быть не раньше завтра')); return; }
          upd(s => { s.plans.unshift(pl); });
          go('plan:' + pl.id);
        }}>
          <div className="field"><label htmlFor="pSubj">{tt('Пән', 'Предмет')}</label><input className="input" id="pSubj" required defaultValue="Микроэкономика" /></div>
          <div className="grid-2">
            <div className="field"><label htmlFor="pDate">{tt('Емтихан күні', 'Дата экзамена')}</label><input className="input" id="pDate" type="date" min={isoLocal(dOff(1))} defaultValue={isoLocal(dOff(10))} required /></div>
            <div className="field"><label htmlFor="pLec">{tt('Дәрістер саны', 'Число лекций')}</label><input className="input" id="pLec" type="number" min="1" max="60" defaultValue="10" required /></div>
          </div>
          <div className="field"><label htmlFor="pHard">{tt('Қиын тақырыптар (міндетті емес)', 'Сложные темы (необязательно)')}</label><input className="input" id="pHard" placeholder={tt('мысалы: икемділік, шығындар', 'например: эластичность, издержки')} /></div>
          <button className="btn" type="submit"><Icon n="bolt" s={18} /> {tt('Жоспар құру', 'Составить план')}</button>
        </form>
      </section>
    </>
  );
}

export function PlanView({ id }: { id: string }) {
  const { S, tt, I, monS, go, openSheet, update: upd } = useApp();
  const pl = S.plans.find(x => x.id === id);
  if (!pl) return <Exam />;
  const exam = new Date(pl.exam), dl = diffDays(exam);
  const done = pl.items.filter(x => x.done).length, pct = Math.round(100 * done / pl.items.length);
  return (
    <>
      <div className="card">
        <div className="card-row" style={{ justifyContent: 'space-between', alignItems: 'baseline' }}>
          <div><span className="eyebrow">{tt('Емтихан', 'Экзамен')}</span><h2>{I.fDate(exam)}</h2></div>
          <span className={`badge ${dl <= 3 ? 'b-sun' : 'b-sky'}`}>{dl >= 0 ? I.daysLeftTxt(dl) : tt('Өтті', 'Прошёл')}</span>
        </div>
        <div className="meter" role="progressbar" aria-valuenow={pct} aria-valuemin={0} aria-valuemax={100}><i style={{ width: `${pct}%` }}></i></div>
        <span className="tiny muted num">{done}/{pl.items.length} {tt('тапсырма орындалды', 'заданий выполнено')} · {pl.lectures} {tt('дәріс', plural(pl.lectures, ['лекция', 'лекции', 'лекций']))}</span>
      </div>
      <div className="group">{pl.items.map((x, i) => {
        const d = planDay(pl, x.d);
        return (
          <label key={i} className={`plan-day check ${x.done ? 'done' : ''}`}>
            <span className="dnum"><b>{d.getDate()}</b><span className="tiny muted">{monS[d.getMonth()]}</span></span>
            <span className="row-main"><span className="row-title">{x.t}</span><span className="row-sub">{tt('Күн', 'День')} {x.d + 1}</span></span>
            <input type="checkbox" checked={x.done} aria-label={tt('Орындалды', 'Выполнено')}
              onChange={e => { const v = e.target.checked; upd(s => { const p = s.plans.find(y => y.id === pl.id); if (p) p.items[i].done = v; }); }} />
          </label>
        );
      })}</div>
      <p className="tiny muted">{tt('Бұл үлгі жоспар: оны өз қарқыныңызға қарай бейімдеңіз. Тест күндері үшін «Карточкалар» құралын қолданыңыз.', 'Это примерный план: подстройте его под свой темп. Для дней проверки используйте «Карточки».')}</p>
      <div className="btn-row">
        <button className="btn secondary" type="button" onClick={() => go('cards')}><Icon n="cards" s={18} /> {tt('Карточкалар', 'Карточки')}</button>
        <button className="btn danger" type="button" onClick={() => openSheet(<ConfirmSheet msg={tt('Жоспарды жою керек пе?', 'Удалить план?')} yes={() => { update(s => { s.plans = s.plans.filter(x => x.id !== pl.id); }); go('exam', { replace: true }); }} />)}>
          <Icon n="trash" s={18} /> {tt('Жою', 'Удалить')}
        </button>
      </div>
    </>
  );
}

/* ---------- flashcards ---------- */
export function Cards() {
  const app = useApp();
  const { S, I, tt, go, toast } = app;
  const decks = allDecks(S, I);
  return (
    <>
      <div className="group">{decks.length ? decks.map(d => <Row key={d.id} go={'deck:' + d.id} icon="cards" title={d.name} sub={tt(`${d.cards.length} карточка`, `${d.cards.length} ${plural(d.cards.length, ['карточка', 'карточки', 'карточек'])}`)} />)
        : <div className="empty">{tt('Әзірге жиын жоқ', 'Пока нет наборов')}</div>}</div>
      <section className="card">
        <h2>{tt('Дәріс мәтінінен жасау', 'Создать из текста лекции')}</h2>
        <p className="small muted">{tt('«Термин — анықтама» немесе «Термин: анықтама» түріндегі жолдар карточкаға айналады.', 'Строки вида «Термин — определение» или «Термин: определение» превратятся в карточки.')}</p>
        <form className="form" id="deckForm" onSubmit={e => {
          e.preventDefault();
          const f = e.currentTarget;
          const name = (f.querySelector<HTMLInputElement>('#dName')?.value || '').trim();
          const cards = cardsFromText(f.querySelector<HTMLTextAreaElement>('#dText')?.value || '');
          const deck = { id: 'dk' + Date.now(), name: name || tt('Жаңа жиын', 'Новый набор'), cards };
          update(s => { s.decks.push(deck); });
          toast(tt(`${cards.length} карточка қосылды`, `Добавлено карточек: ${cards.length}`));
          go('deck:' + deck.id);
        }}>
          <div className="field"><label htmlFor="dName">{tt('Жиын атауы', 'Название набора')}</label><input className="input" id="dName" required placeholder={tt('мысалы: Статистика, 3-апта', 'например: Статистика, неделя 3')} /></div>
          <div className="field"><label htmlFor="dText">{tt('Мәтін', 'Текст')}</label><textarea className="input" id="dText" placeholder={tt('Медиана — реттелген қатардың ортасындағы мән\nМода: ең жиі кездесетін мән', 'Медиана — значение в середине упорядоченного ряда\nМода: самое частое значение')}></textarea></div>
          <button className="btn" type="submit"><Icon n="plus" s={18} /> {tt('Жиын құру', 'Создать набор')}</button>
        </form>
        <button className="link-btn" type="button" onClick={() => toChat(app, tt('Төмендегі дәріс мәтінінен 10 тест сұрағын (4 нұсқа, дұрыс жауабы белгіленген) және 10 карточка «термин — анықтама» түрінде құрастыр:\n\n', 'По тексту лекции ниже составь 10 тестовых вопросов (4 варианта, правильный отмечен) и 10 карточек вида «термин — определение»:\n\n'))}>
          <Icon n="spark" s={15} /> {tt('Ұзын дәрістен AI арқылы тест құрастыру', 'Составить тест из длинной лекции с AI')}
        </button>
      </section>
    </>
  );
}

function startStudy(app: App, id: string, mode: string) {
  const d = allDecks(app.S, app.I).find(x => x.id === id);
  if (!d) return;
  const cards = d.cards.slice();
  if (mode === 'exam') cards.sort(() => Math.random() - 0.5);
  app.setUi({ study: { id: d.id, mode, queue: cards, total: cards.length, firstOk: 0, wrong: [], flipped: false } });
}

export function DeckView({ id }: { id: string }) {
  const app = useApp();
  const { S, I, tt, go, toast, openSheet } = app;
  const d = allDecks(S, I).find(x => x.id === id);
  if (!d) return <Cards />;
  return (
    <>
      <div className="btn-row">
        <button className="btn" type="button" disabled={!d.cards.length} onClick={() => { startStudy(app, d.id, 'learn'); go('study'); }}><Icon n="cards" s={18} /> {tt('Оқу', 'Учить')}</button>
        <button className="btn sun" type="button" disabled={!d.cards.length} onClick={() => { startStudy(app, d.id, 'exam'); go('study'); }}><Icon n="target" s={18} /> {tt('Емтихан режимі', 'Режим экзамена')}</button>
      </div>
      <div className="group">{d.cards.length ? d.cards.map((c, i) => (
        <div key={i} className="row"><span className="row-main"><span className="row-title">{c.q}</span><span className="row-sub">{c.a}</span></span>
          {d.demo ? null : <button className="icon-btn" type="button" aria-label={tt('Жою', 'Удалить')} style={{ color: 'var(--muted)' }}
            onClick={() => update(s => { s.decks.find(x => x.id === d.id)?.cards.splice(i, 1); })}><Icon n="trash" s={18} /></button>}</div>
      )) : <div className="empty">{tt('Карточка жоқ. Төменнен қосыңыз.', 'Карточек нет. Добавьте ниже.')}</div>}</div>
      {d.demo ? (
        <>
          <p className="tiny muted">{tt('Бұл үлгі жиын. Өз жиыныңызды «Карточкалар» бетінде құрыңыз.', 'Это пример. Создайте свой набор на странице «Карточки».')}</p>
          <button className="btn ghost" type="button" onClick={() => { update(s => { s.hideDemoDeck = true; }); go('cards', { replace: true }); }}>{tt('Үлгі жиынды жасыру', 'Скрыть пример')}</button>
        </>
      ) : (
        <>
          <form className="card form" id="cardForm" onSubmit={e => {
            e.preventDefault();
            const f = e.currentTarget;
            const q = f.querySelector<HTMLInputElement>('#cQ'), a = f.querySelector<HTMLTextAreaElement>('#cA');
            const card = { q: (q?.value || '').trim(), a: (a?.value || '').trim() };
            update(s => { s.decks.find(x => x.id === d.id)?.cards.push(card); });
            f.reset();
            toast(tt('Қосылды', 'Добавлено'));
          }}>
            <h3>{tt('Карточка қосу', 'Добавить карточку')}</h3>
            <div className="field"><label htmlFor="cQ">{tt('Сұрақ / термин', 'Вопрос / термин')}</label><input className="input" id="cQ" required /></div>
            <div className="field"><label htmlFor="cA">{tt('Жауап', 'Ответ')}</label><textarea className="input" id="cA" required style={{ minHeight: 70 }}></textarea></div>
            <button className="btn" type="submit"><Icon n="plus" s={18} /> {tt('Қосу', 'Добавить')}</button>
          </form>
          <button className="btn danger" type="button" onClick={() => openSheet(<ConfirmSheet msg={tt('Жиынды жою керек пе?', 'Удалить набор?')} yes={() => { update(s => { s.decks = s.decks.filter(x => x.id !== d.id); }); go('cards', { replace: true }); }} />)}>
            <Icon n="trash" s={18} /> {tt('Жиынды жою', 'Удалить набор')}
          </button>
        </>
      )}
    </>
  );
}

export function Study() {
  const app = useApp();
  const { ui, tt, setUi, back } = app;
  const s = ui.study;
  if (!s) return <Cards />;
  if (!s.queue.length) {
    const pct = s.total ? Math.round(100 * s.firstOk / s.total) : 0;
    return (
      <div className="card" style={{ alignItems: 'center', textAlign: 'center' }}>
        <span className="eyebrow">{s.mode === 'exam' ? tt('Емтихан режимі', 'Режим экзамена') : tt('Оқу', 'Учёба')}</span>
        <span className="big-num">{pct}%</span>
        <p>{tt(`Бірінші реттен ${s.firstOk} / ${s.total} дұрыс`, `С первого раза верно: ${s.firstOk} из ${s.total}`)}</p>
        <p className="small muted">{s.wrong.length ? tt('Қате жауаптар қайталанды. Ертең тағы өтіп шығыңыз.', 'Ошибки были повторены. Пройдите ещё раз завтра.') : tt('Тамаша! Барлығын білесіз.', 'Отлично! Вы знаете всё.')}</p>
        <div className="btn-row" style={{ width: '100%' }}><button className="btn" type="button" onClick={() => startStudy(app, s.id, s.mode)}>{tt('Қайта бастау', 'Начать заново')}</button><button className="btn ghost" type="button" onClick={back}>{tt('Жиынға оралу', 'К набору')}</button></div>
      </div>
    );
  }
  const c = s.queue[0];
  const grade = (ok: boolean) => {
    const queue = s.queue.slice(1);
    let { firstOk } = s;
    const wrong = s.wrong.slice();
    if (ok) { if (!wrong.includes(c.q)) firstOk++; } else { if (!wrong.includes(c.q)) wrong.push(c.q); queue.push(c); }
    setUi({ study: { ...s, queue, firstOk, wrong, flipped: false } });
  };
  return (
    <>
      <div className="card-row" style={{ justifyContent: 'space-between' }}><span className="tiny muted num">{tt('Қалды', 'Осталось')}: {s.queue.length}</span><span className="tiny muted num">{tt('Дұрыс', 'Верно')}: {s.firstOk}</span></div>
      <button className={`flash ${s.flipped ? 'back' : ''}`} type="button" onClick={() => setUi({ study: { ...s, flipped: !s.flipped } })}>
        <span className="eyebrow">{s.flipped ? tt('Жауап', 'Ответ') : tt('Сұрақ', 'Вопрос')}</span>
        <span className="q">{s.flipped ? c.a : c.q}</span>
        {s.flipped ? null : <span className="tiny muted">{s.mode === 'exam' ? tt('Алдымен ойша жауап беріңіз, сосын түртіңіз', 'Сначала ответьте мысленно, затем нажмите') : tt('Жауапты көру үшін түртіңіз', 'Нажмите, чтобы увидеть ответ')}</span>}
      </button>
      {s.flipped ? <div className="btn-row"><button className="btn danger" type="button" onClick={() => grade(false)}><Icon n="x" s={18} /> {tt('Қайталау керек', 'Повторить')}</button><button className="btn" type="button" onClick={() => grade(true)}><Icon n="check" s={18} /> {tt('Білемін', 'Знаю')}</button></div> : null}
      {s.mode === 'exam' ? <p className="tiny muted">{tt('Емтихан режимінде кеңес жоқ: қате жауаптар соңында қайта беріледі.', 'В режиме экзамена нет подсказок: ошибки вернутся в конце.')}</p> : null}
    </>
  );
}

/* ---------- job match ---------- */
export function Match({ id }: { id?: string }) {
  const { S, ui, tt, L, go, openSheet, content } = useApp();
  const jid = id || ui.matchId;
  const j = content.jobs.find(x => x.id === jid) || content.jobs[0];
  if (!j) return null;
  const m = matchJob(j, S.profile);
  const tips = m.miss.map(k => ({ k, c: COURSES.find(c => c.skills.includes(k)) }));
  return (
    <>
      <div className="field"><label htmlFor="mJob">{tt('Вакансия', 'Вакансия')}</label>
        <select className="input" id="mJob" value={j.id} onChange={e => go('match:' + e.target.value, { replace: true })}>{content.jobs.map(x => <option key={x.id} value={x.id}>{L(x.title)}</option>)}</select></div>
      <div className="card" style={{ gap: 12 }}>
        <div className="card-row" style={{ alignItems: 'center', justifyContent: 'space-between' }}>
          <div><span className="eyebrow">{tt('Сәйкестік', 'Соответствие')}</span><div className="big-num">{m.pct}%</div></div>
          <span className={`badge ${m.pct >= 75 ? 'b-ok' : m.pct >= 50 ? 'b-sun' : 'b-bad'}`}>{m.pct >= 75 ? tt('Жақсы сәйкестік', 'Хорошее совпадение') : m.pct >= 50 ? tt('Орташа', 'Среднее') : tt('Төмен', 'Низкое')}</span>
        </div>
        <div className="meter"><i style={{ width: `${m.pct}%` }}></i></div>
        <p className="tiny muted">{tt('Бұл жүйенің шамамен бағасы, жұмысқа орналасу кепілі емес.', 'Это ориентировочная оценка системы, а не гарантия трудоустройства.')}</p>
      </div>
      <section className="section"><h2>{tt('Күшті жақтарыңыз', 'Сильные стороны')}</h2>
        <div className="card">
          {m.have.map(k => <div key={k} className="res"><span className="dot ok">✓</span><span>{L(SKILLS[k])}</span></div>)}
          {m.specNeed && m.specOk ? <div className="res"><span className="dot ok">✓</span><span>{tt('Мамандығыңыз сәйкес', 'Подходящая специальность')}: {L(SPECS[S.profile.spec])}</span></div> : null}
          {m.hasExp ? <div className="res"><span className="dot ok">✓</span><span>{tt('Жұмыс тәжірибесі бар', 'Есть опыт работы')}</span></div> : null}
          {!m.have.length && !m.hasExp ? <p className="small muted">{tt('Сәйкес дағды табылмады.', 'Совпадающих навыков не найдено.')}</p> : null}
        </div></section>
      <section className="section"><h2>{tt('Нені жақсартуға болады', 'Что можно улучшить')}</h2>
        <div className="card">
          {tips.map(({ k, c }) => (
            <div key={k} className="res"><span className="dot no">!</span><div className="row-main"><span className="row-title">{L(SKILLS[k])}</span>
              {c ? <button className="link-btn" type="button" style={{ textAlign: 'left' }} onClick={() => openSheet(<CourseSheet id={c.id} />)}>{tt('Курс', 'Курс')}: {L(c.title)} →</button> : null}</div></div>
          ))}
          {m.specNeed && !m.specOk ? <div className="res"><span className="dot mid">!</span><span>{tt('Вакансия мына мамандықтарға арналған', 'Вакансия ориентирована на специальности')}: {j.spec.map(s => L(SPECS[s])).join(', ')}</span></div> : null}
          {!m.hasExp ? <div className="res"><span className="dot mid">!</span><span>{tt('Профильге тәжірибе қосыңыз (ерікті жұмыс та есептеледі)', 'Добавьте опыт в профиль (волонтёрство тоже считается)')}</span></div> : null}
          {!tips.length && m.hasExp && (!m.specNeed || m.specOk) ? <p className="small muted">{tt('Барлық негізгі талап орындалған!', 'Все основные требования выполнены!')}</p> : null}
        </div></section>
      <div className="btn-row"><button className="btn" type="button" onClick={() => go('job:' + j.id)}>{tt('Вакансияны ашу', 'Открыть вакансию')}</button><button className="btn ghost" type="button" onClick={() => go('edit')}>{tt('Дағдыларды өзгерту', 'Изменить навыки')}</button></div>
    </>
  );
}
