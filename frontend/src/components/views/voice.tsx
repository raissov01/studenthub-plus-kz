'use client';
/* «University voice» (docs/UNIVOICE.md): student surveys (one question per screen), anonymous suggestions and complaints
   with a device-held ticket code, the wellbeing section, and the staff cabinet /voice (survey editor + k-anonymous
   results, feedback inbox, recipients and psychologist contacts). */
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { copyText, useApp } from '@/components/app';
import { Empty, Icon, Seg } from '@/components/ui';
import { ErrorLine, Feature, Loading, NotFound, SignInCard, errCode, errText, hasAccount, useLoad } from '@/components/views/common';
import { useQueryId } from '@/components/views/subjects';
import { api, apiDownload, apiUpload, ApiError, useAccount } from '@/lib/account';
import { useConfig } from '@/lib/config';
import { SPECS } from '@/lib/data/static';
import { shrinkPhoto } from '@/lib/market';
import { almatyToday, awardText, pointsChanged, type PointsAward } from '@/lib/points';
import {
  addMood, addToken, answersPayload, canContinue, CATEGORIES, detectFlag, EMERGENCY_LINES, FEEDBACK_MAX, flowPct, HELP_LINES, K_MIN, pct,
  readMood, readTokens, removeToken, SELF_HELP, telHref, templates, TOKEN_RE, toggle,
  type Answers, type AnswerValue, type MoodEntry, type QResult, type QType, type Question, type Recipient, type StoredTicket, type Survey,
  type SurveyResults, type Ticket, type WellbeingContact,
} from '@/lib/voice';
import { tr } from '@/lib/i18n';
import type { Lang } from '@/lib/types';

type TT = (kk: string, ru: string) => string;
const ls = () => (typeof window === 'undefined' ? null : window.localStorage);
const day = (iso: string | null | undefined) => (iso ? iso.slice(0, 10) : '');

/* ====================================================================== surveys (student) */

export function SurveysScreen() {
  return <Feature name="surveys"><SurveysGate /></Feature>;
}

function SurveysGate() {
  const { tt } = useApp();
  const acc = useAccount();
  const id = useQueryId();
  if (!acc.loaded) return <Loading />;
  if (!hasAccount(acc)) return <SignInCard text={tt('Сауалнамаға жауап беру үшін аккаунтқа кіріңіз. Анонимді сауалнамада жауабыңыз атыңызбен байланыспайды.', 'Чтобы отвечать на опросы, войдите в аккаунт. В анонимном опросе ответ не связывается с вашим именем.')} />;
  return id ? <SurveyFlow id={id} /> : <SurveyList />;
}

function SurveyList() {
  const { tt, go } = useApp();
  const { data, err, loading } = useLoad<{ surveys: Survey[] }>('/api/surveys');
  if (loading && !data) return <Loading />;
  const list = data?.surveys ?? [];
  return (
    <div className="stack">
      <ErrorLine err={err} />
      {list.length === 0 && !err ? <Empty icon="chart">{tt('Қазір сізге арналған ашық сауалнама жоқ.', 'Сейчас нет открытых опросов для вас.')}</Empty> : null}
      {list.map(s => (
        <div className="card" key={s.id}>
          <h2 className="uv-title">{s.title}</h2>
          <div className="badges">
            {s.anonymous ? <span className="badge b-ok">{tt('Анонимді', 'Анонимно')}</span> : <span className="badge b-line">{tt('Атыңызбен', 'С именем')}</span>}
            <span className="badge b-line">{tt(`${s.questions.length} сұрақ`, `${s.questions.length} вопр.`)}</span>
            {s.closesAt ? <span className="badge b-line">{tt('Соңғы күн: ', 'До: ') + day(s.closesAt)}</span> : null}
            {s.done ? <span className="badge b-ok"><Icon n="check" s={14} /> {tt('Жауап берілді', 'Ответ отправлен')}</span> : null}
          </div>
          {s.done ? null : <button className="btn block" type="button" onClick={() => go('surveys:' + s.id)}>{tt('Бастау', 'Начать')}</button>}
        </div>
      ))}
    </div>
  );
}

function SurveyFlow({ id }: { id: string }) {
  const { tt, lang, go, toast } = useApp();
  const { data, err, code, loading } = useLoad<{ survey: Survey }>('/api/surveys/' + encodeURIComponent(id));
  const [step, setStep] = useState(-1); // -1 = intro
  const [a, setA] = useState<Answers>({});
  const [busy, setBusy] = useState(false);
  const [sendErr, setSendErr] = useState('');
  const [done, setDone] = useState<null | { award: PointsAward | null }>(null);
  const head = useRef<HTMLHeadingElement>(null);
  useEffect(() => { head.current?.focus(); }, [step]);
  if (loading && !data) return <Loading />;
  if (code === 'not_found' || (!data && err)) return <Empty icon="chart" action={<button className="btn secondary" type="button" onClick={() => go('surveys')}>{tt('Сауалнамалар', 'Опросы')}</button>}>{tt('Сауалнама табылмады не жабылған.', 'Опрос не найден или закрыт.')}</Empty>;
  const s = data!.survey;
  if (done || s.done) {
    const text = done ? awardText(done.award, tt) : '';
    return (
      <div className="card uv-done" role="status">
        <span className="ico ok"><Icon n="check" /></span>
        <h2>{tt('Рақмет! Жауабыңыз жіберілді.', 'Спасибо! Ответ отправлен.')}</h2>
        <p className="small muted">{tt('Жіберілген жауапты өзгертуге болмайды.', 'Отправленный ответ изменить нельзя.')}{text ? ' ' + text : ''}</p>
        <button className="btn secondary" type="button" onClick={() => go('surveys')}>{tt('Барлық сауалнамалар', 'Все опросы')}</button>
      </div>
    );
  }
  const qs = s.questions;
  const submit = async () => {
    setBusy(true); setSendErr('');
    try {
      const r = await api<{ award: PointsAward | null }>(`/api/surveys/${s.id}/responses`, { method: 'POST', body: { answers: answersPayload(qs, a) }, lang });
      if (r.award) pointsChanged();
      setDone({ award: r.award });
      toast(tt('Жауап жіберілді', 'Ответ отправлен'));
    } catch (e) {
      if (errCode(e) === 'already_answered') setDone({ award: null });
      else setSendErr(errText(e));
    } finally { setBusy(false); }
  };
  if (step < 0) {
    return (
      <div className="card">
        <h2 tabIndex={-1} ref={head}>{s.title}</h2>
        {s.intro ? <p className="small uv-pre">{s.intro}</p> : null}
        <p className="note">{s.anonymous
          ? tt('Анонимді сауалнама: жауаптар атыңызсыз, уақытсыз сақталады; ұйымдастырушылар тек кемінде 5 адамның жиынтық нәтижесін көреді. Сіздің жауап бергеніңіз тек қайта дауыс бермеу үшін бір жақты кодпен белгіленеді.',
            'Анонимный опрос: ответы хранятся без имени и времени; организаторы видят только сводку минимум по 5 людям. Факт ответа отмечается односторонним кодом — только чтобы нельзя было голосовать дважды.')
          : tt('Бұл сауалнама анонимді емес: жауабыңыз аккаунтыңызбен байланысады, бірақ ұйымдастырушылар тек жиынтық нәтижені көреді.',
            'Этот опрос не анонимный: ответ связан с аккаунтом, но организаторы видят только сводные результаты.')}</p>
        <button className="btn block" type="button" onClick={() => setStep(0)}>{tt('Бастау', 'Начать')} · {tt(`${qs.length} сұрақ`, `${qs.length} вопр.`)}</button>
      </div>
    );
  }
  const q = qs[step];
  const last = step === qs.length - 1;
  const ok = canContinue(q, a[q.id]);
  const p = flowPct(step + 1, qs.length);
  return (
    <div className="stack">
      <div className="uv-progress" role="progressbar" aria-label={tt('Сауалнама прогресі', 'Прогресс опроса')} aria-valuemin={0} aria-valuemax={100} aria-valuenow={p}
        aria-valuetext={tt(`${step + 1}/${qs.length} сұрақ`, `Вопрос ${step + 1} из ${qs.length}`)}>
        <span style={{ width: p + '%' }} />
      </div>
      <p className="tiny muted">{tt(`${step + 1}/${qs.length} сұрақ`, `Вопрос ${step + 1} из ${qs.length}`)}</p>
      <form className="card" onSubmit={e => { e.preventDefault(); if (!ok) return; if (last) void submit(); else setStep(step + 1); }}>
        <QuestionInput q={q} v={a[q.id]} set={v => setA(x => ({ ...x, [q.id]: v }))} head={head} tt={tt} />
        <ErrorLine err={sendErr} />
        <div className="btn-row">
          <button className="btn ghost" type="button" onClick={() => setStep(step - 1)}>{tt('Артқа', 'Назад')}</button>
          <button className="btn" type="submit" disabled={!ok || busy}>{last ? tt('Жіберу', 'Отправить') : tt('Келесі', 'Далее')}</button>
        </div>
        {last ? <p className="tiny muted">{tt('Жібергеннен кейін жауапты өзгертуге болмайды.', 'После отправки ответ изменить нельзя.')}</p> : null}
      </form>
    </div>
  );
}

function ScaleRow({ name, max, v, set, label, minLabel, maxLabel }: { name: string; max: number; v: number; set: (n: number) => void; label: string; minLabel?: string; maxLabel?: string }) {
  return (
    <fieldset className="uv-scale">
      <legend className="small">{label}</legend>
      <div className={'uv-scale-row' + (max > 5 ? ' ten' : '')}>
        {Array.from({ length: max }, (_, i) => i + 1).map(n => (
          <label key={n} className="uv-pick">
            <input type="radio" name={name} value={n} checked={v === n} onChange={() => set(n)} />
            <span>{n}</span>
          </label>
        ))}
      </div>
      {minLabel || maxLabel ? <div className="uv-anchors tiny muted"><span>1 — {minLabel}</span><span>{max} — {maxLabel}</span></div> : null}
    </fieldset>
  );
}

function QuestionInput({ q, v, set, head, tt }: { q: Question; v: AnswerValue | undefined; set: (v: AnswerValue) => void; head: React.RefObject<HTMLHeadingElement | null>; tt: TT }) {
  const title = (
    <>
      <h2 tabIndex={-1} ref={head} id={'uvq_' + q.id}>{q.text}{q.required ? <span aria-hidden="true"> *</span> : null}</h2>
      <p className="tiny muted">{q.required ? tt('Міндетті сұрақ', 'Обязательный вопрос') : tt('Міндетті емес — өткізіп жіберуге болады', 'Необязательный — можно пропустить')}</p>
      {q.help ? <p className="small muted">{q.help}</p> : null}
    </>
  );
  switch (q.type) {
    case 'single': case 'multiple':
      return (
        <fieldset className="uv-options" aria-labelledby={'uvq_' + q.id}>
          {title}
          {(q.options ?? []).map((o, i) => (
            <label key={i} className="check uv-option">
              {q.type === 'single'
                ? <input type="radio" name={q.id} checked={v === i} onChange={() => set(i)} />
                : <input type="checkbox" checked={Array.isArray(v) && v.includes(i)} onChange={() => set(toggle(v, i))} />}
              <span>{o}</span>
            </label>
          ))}
        </fieldset>
      );
    case 'scale':
      return <div>{title}<ScaleRow name={q.id} max={q.max ?? 5} v={typeof v === 'number' ? v : 0} set={set} label={tt('Бағаңыз', 'Ваша оценка')} minLabel={q.minLabel} maxLabel={q.maxLabel} /></div>;
    case 'text':
      return (
        <div className="field">
          {title}
          <label className="sr-only" htmlFor={'uvt_' + q.id}>{q.text}</label>
          <textarea className="input" id={'uvt_' + q.id} rows={4} maxLength={500} value={typeof v === 'string' ? v : ''} onChange={e => set(e.target.value)} />
          <span className="tiny muted">{(typeof v === 'string' ? v.length : 0)}/500</span>
        </div>
      );
    case 'matrix': {
      const rows = q.rows ?? [];
      const cur = Array.isArray(v) && v.length === rows.length ? v : rows.map(() => 0);
      return (
        <div>
          {title}
          {rows.map((r, i) => (
            <ScaleRow key={i} name={`${q.id}_${i}`} max={q.max ?? 5} v={cur[i]} label={r} minLabel={i === rows.length - 1 ? q.minLabel : undefined} maxLabel={i === rows.length - 1 ? q.maxLabel : undefined}
              set={n => set(cur.map((x, j) => (j === i ? n : x)))} />
          ))}
        </div>
      );
    }
  }
}

/* ====================================================================== feedback (student) */

const catLabel = (c: string, tt: TT) => ({
  suggestion: tt('Ұсыныс', 'Предложение'), complaint: tt('Шағым', 'Жалоба'), question: tt('Сұрақ', 'Вопрос'), gratitude: tt('Алғыс', 'Благодарность'),
} as Record<string, string>)[c] ?? c;

function statusBadge(s: string, tt: TT) {
  const m: Record<string, [string, string]> = {
    review: ['b-line', tt('Тексерілуде', 'На проверке')], held: ['b-bad', tt('Модерацияда', 'На модерации')], new: ['b-sky', tt('Жаңа', 'Новое')],
    in_progress: ['b-sky', tt('Қаралуда', 'В работе')], answered: ['b-ok', tt('Жауап берілді', 'Есть ответ')], closed: ['b-line', tt('Жабылды', 'Закрыто')],
  };
  const [cls, label] = m[s] ?? ['b-line', s];
  return <span className={'badge ' + cls}>{label}</span>;
}

const recName = (r: Recipient, lang: string) => (lang !== 'kk' ? r.nameRu : r.nameKk);

export function FeedbackScreen() {
  return <Feature name="feedback"><FeedbackHome /></Feature>;
}

function FeedbackHome() {
  const { tt } = useApp();
  const [tab, setTab] = useState('new');
  const [stamp, setStamp] = useState(0);
  return (
    <div className="stack">
      <Seg items={[['new', tt('Жаңа өтініш', 'Новое обращение')], ['mine', tt('Менің өтініштерім', 'Мои обращения')]]} cur={tab} onPick={setTab} />
      {tab === 'new' ? <Compose onSent={() => { setStamp(n => n + 1); setTab('mine'); }} /> : <MyTickets key={stamp} />}
    </div>
  );
}

function Compose({ onSent }: { onSent: () => void }) {
  const { tt, lang, openSheet } = useApp();
  const acc = useAccount();
  const signedIn = hasAccount(acc);
  const { data, err } = useLoad<{ recipients: Recipient[]; university: string }>(signedIn ? '/api/feedback/recipients' : null);
  const [rec, setRec] = useState(0);
  const [cat, setCat] = useState('suggestion');
  const [body, setBody] = useState('');
  const [anon, setAnon] = useState(true);
  const [ack, setAck] = useState(false);
  const [photo, setPhoto] = useState<File | null>(null);
  const [busy, setBusy] = useState(false);
  const [sendErr, setSendErr] = useState('');
  const flag = useMemo(() => detectFlag(body), [body]);
  if (!acc.loaded) return <Loading />;
  if (!signedIn) return <SignInCard text={tt('Өтініш жіберу үшін кіріңіз (тек университет студенттері жаза алуы үшін). Анонимді режимде атыңыз өтінішпен бірге сақталмайды.', 'Чтобы отправить обращение, войдите (так пишут только студенты университета). В анонимном режиме имя не сохраняется вместе с обращением.')} />;
  const recs = data?.recipients ?? [];
  if (data && recs.length === 0) return <Empty icon="chat">{data.university ? tt('Университетіңіз үшін алушылар әлі бапталмаған.', 'Для вашего университета получатели ещё не настроены.') : tt('Профильде университетіңізді көрсетіңіз.', 'Укажите университет в профиле.')}</Empty>;
  const len = body.trim().length;
  const valid = rec > 0 && len >= 10 && len <= FEEDBACK_MAX && (flag !== 'threat' || ack);
  const send = async () => {
    setBusy(true); setSendErr('');
    const payload = { recipientId: rec, category: cat, body, anonymous: anon, ack };
    try {
      let r: { id: number; token?: string; held: boolean };
      if (photo) {
        const fd = new FormData();
        fd.append('data', JSON.stringify(payload));
        fd.append('photo', await shrinkPhoto(photo), 'photo.jpg');
        r = await apiUpload(`/api/feedback/tickets`, fd, lang);
      } else {
        r = await api(`/api/feedback/tickets`, { method: 'POST', body: payload, lang });
      }
      if (r.token) {
        addToken(ls(), { token: r.token, id: r.id, createdAt: new Date().toISOString() });
        openSheet(<TokenSheet token={r.token} held={r.held} />);
      } else openSheet(<SentSheet held={r.held} />);
      setBody(''); setPhoto(null); setAck(false);
      onSent();
    } catch (e) {
      if (errCode(e) === 'escalation_notice') setAck(false);
      setSendErr(errText(e));
    } finally { setBusy(false); }
  };
  return (
    <form className="card form" noValidate onSubmit={e => { e.preventDefault(); if (valid && !busy) void send(); }}>
      <ErrorLine err={err} />
      <div className="field">
        <label htmlFor="fbRec">{tt('Кімге', 'Кому')}</label>
        <select className="input" id="fbRec" value={rec} onChange={e => setRec(Number(e.target.value))}>
          <option value={0}>{tt('Таңдаңыз…', 'Выберите…')}</option>
          {recs.map(r => <option key={r.id} value={r.id}>{recName(r, lang)}</option>)}
        </select>
      </div>
      <fieldset className="field">
        <legend>{tt('Түрі', 'Тип')}</legend>
        <div className="chips wrap">
          {CATEGORIES.map(c => <button key={c} type="button" className="chip" aria-pressed={cat === c} onClick={() => setCat(c)}>{catLabel(c, tt)}</button>)}
        </div>
      </fieldset>
      <div className="field">
        <label htmlFor="fbBody">{tt('Мәтін', 'Текст')}</label>
        <textarea className="input" id="fbBody" rows={6} maxLength={FEEDBACK_MAX} value={body} onChange={e => setBody(e.target.value)} aria-describedby="fbBodyHint" />
        <span className="tiny muted" id="fbBodyHint">{tt('Кемінде 10 таңба. Басқа адамдардың жеке деректерін жазбаңыз.', 'Минимум 10 символов. Не пишите чужие персональные данные.')} {len}/{FEEDBACK_MAX}</span>
      </div>
      {flag === 'threat' ? (
        <div className="uv-alert" role="alert">
          <p><b>{tt('Маңызды', 'Важно')}</b></p>
          <p className="small">{tt('Мәтінде қауіп не төтенше жағдай белгілері бар. Мұндай өтініш алушыға бармас бұрын платформа әкімшісіне жедел жіберіледі (анонимді болса да). Біреудің өміріне қазір қауіп төнсе — күтпеңіз:',
            'В тексте есть признаки угрозы или экстренной ситуации. Такое обращение сначала срочно уходит администратору платформы (даже анонимное). Если чья-то жизнь в опасности прямо сейчас — не ждите:')}</p>
          <div className="btn-row">{EMERGENCY_LINES.slice(0, 1).map(h => <a key={h.number} className="btn danger" href={telHref(h.number)}>{tt('Қоңырау: ', 'Позвонить: ')}{h.number}</a>)}</div>
          <label className="check"><input type="checkbox" checked={ack} onChange={e => setAck(e.target.checked)} /><span className="small">{tt('Оқыдым, жіберуді жалғастырамын', 'Я прочитал(а), продолжить отправку')}</span></label>
        </div>
      ) : flag === 'profanity' ? (
        <p className="note">{tt('Мәтінде дөрекі сөздер бар — өтініш алушыға жетпес бұрын әкімші тексереді.', 'В тексте есть грубые слова — администратор проверит обращение перед доставкой.')}</p>
      ) : null}
      <div className="field">
        <label htmlFor="fbPhoto">{tt('Фото (міндетті емес)', 'Фото (необязательно)')}</label>
        <input className="input" id="fbPhoto" type="file" accept="image/jpeg,image/png,image/webp" onChange={e => setPhoto(e.target.files?.[0] ?? null)} />
        <span className="tiny muted">{tt('Фотодан орын (GPS) мен басқа метадеректер өшіріледі. Адамдардың беті көрінбесін.', 'Из фото удаляются геометки (GPS) и другие метаданные. Не снимайте лица людей.')}</span>
      </div>
      <fieldset className="field">
        <legend>{tt('Режим', 'Режим')}</legend>
        <label className="check"><input type="radio" name="fbMode" checked={anon} onChange={() => setAnon(true)} /><span className="small"><b>{tt('Анонимді', 'Анонимно')}</b></span></label>
        <label className="check"><input type="radio" name="fbMode" checked={!anon} onChange={() => setAnon(false)} /><span className="small"><b>{tt('Атыммен', 'С моим именем')}</b></span></label>
        <p className="tiny muted">{anon
          ? tt('Аты-жөніңіз өтінішпен бірге сақталмайды — қызметкерлер де, әкімшілер де жіберушіні көрмейді. Жауапты тек осы құрылғыда сақталатын құпия кодпен көресіз: кодты жоғалтсаңыз, жауапты қалпына келтіру мүмкін емес.',
            'Имя не сохраняется вместе с обращением — ни сотрудники, ни администраторы не видят отправителя. Ответ вы увидите по секретному коду, который хранится только на этом устройстве: если код потерять, восстановить доступ нельзя.')
          : tt('Алушы атыңызды көреді; жауап туралы хабарлама Telegram-ға және хабарламаларға келеді.', 'Получатель видит ваше имя; об ответе придёт уведомление в Telegram и в уведомления.')}</p>
      </fieldset>
      <ErrorLine err={sendErr} />
      <button className="btn block" type="submit" disabled={!valid || busy}><Icon n="send" s={18} /> {tt('Жіберу', 'Отправить')}</button>
    </form>
  );
}

function TokenSheet({ token, held }: { token: string; held: boolean }) {
  const { tt, toast, closeSheet } = useApp();
  return (
    <>
      <h2>{tt('Өтініш жіберілді', 'Обращение отправлено')}</h2>
      {held ? <p className="small">{tt('Ол алдымен әкімшінің тексеруінен өтеді.', 'Сначала его проверит администратор.')}</p> : null}
      <p className="small">{tt('Бұл — өтінішіңіздің құпия коды. Ол тек осы құрылғыда сақталды. Басқа құрылғыдан жауапты көру үшін көшіріп, қауіпсіз жерге сақтаңыз.',
        'Это секретный код обращения. Он сохранён только на этом устройстве. Чтобы видеть ответ с другого устройства, скопируйте его и сохраните в надёжном месте.')}</p>
      <code className="uv-token" aria-label={tt('Өтініш коды', 'Код обращения')}>{token}</code>
      <div className="btn-row">
        <button className="btn secondary" type="button" onClick={() => copyText(token, toast, tt)}><Icon n="copy" s={18} /> {tt('Көшіру', 'Копировать')}</button>
        <button className="btn" type="button" onClick={closeSheet}>{tt('Жақсы', 'Понятно')}</button>
      </div>
    </>
  );
}

function SentSheet({ held }: { held: boolean }) {
  const { tt, closeSheet } = useApp();
  return (
    <>
      <h2>{tt('Өтініш жіберілді', 'Обращение отправлено')}</h2>
      <p className="small">{held ? tt('Ол алдымен әкімшінің тексеруінен өтеді.', 'Сначала его проверит администратор.') : tt('Жауап келгенде хабарлама аласыз.', 'Когда придёт ответ, вы получите уведомление.')}</p>
      <button className="btn block" type="button" onClick={closeSheet}>{tt('Жақсы', 'Понятно')}</button>
    </>
  );
}

interface Mine { ticket: Ticket; token?: string }

/** the device's anonymous tickets (by their stored codes) and, when signed in, the signed ones */
async function fetchMine(signed: boolean, lang: 'kk' | 'ru' | 'en'): Promise<Mine[]> {
  const stored = readTokens(ls());
  const out: Mine[] = [];
  if (stored.length) {
    const r = await api<{ tickets: { token: string; ticket: Ticket }[] }>('/api/feedback/lookup', { method: 'POST', body: { tokens: stored.map(s => s.token) }, lang });
    out.push(...r.tickets.map(x => ({ ticket: x.ticket, token: x.token })));
  }
  if (signed) {
    const r = await api<{ tickets: Ticket[] }>('/api/feedback/mine', { lang });
    out.push(...r.tickets.map(t => ({ ticket: t })));
  }
  return out.sort((a, b) => b.ticket.updatedAt.localeCompare(a.ticket.updatedAt));
}

function MyTickets() {
  const { tt, lang } = useApp();
  const acc = useAccount();
  const signed = hasAccount(acc);
  const [items, setItems] = useState<Mine[] | null>(null);
  const [err, setErr] = useState('');
  const [open, setOpen] = useState(0);
  const [code, setCode] = useState('');
  const [n, setN] = useState(0);
  useEffect(() => {
    let live = true;
    fetchMine(signed, lang).then(
      r => { if (live) { setItems(r); setErr(''); } },
      e => { if (live) { setErr(errText(e)); setItems(x => x ?? []); } },
    );
    return () => { live = false; };
  }, [signed, lang, n]);
  const load = useCallback(async () => { setN(x => x + 1); }, []);
  const addCode = () => {
    const c = code.trim();
    if (!TOKEN_RE.test(c)) { setErr(tt('Код дұрыс емес.', 'Неверный код.')); return; }
    addToken(ls(), { token: c, id: 0, createdAt: new Date().toISOString() } as StoredTicket);
    setCode('');
    void load();
  };
  if (!items) return <Loading />;
  return (
    <div className="stack">
      <ErrorLine err={err} />
      {items.length === 0 ? <Empty icon="chat">{tt('Әзірге өтініш жоқ.', 'Пока нет обращений.')}</Empty> : null}
      {items.map(m => <TicketCard key={m.ticket.id} m={m} open={open === m.ticket.id} toggle={() => setOpen(open === m.ticket.id ? 0 : m.ticket.id)} reload={load} />)}
      <details className="card">
        <summary className="small">{tt('Басқа құрылғыдағы анонимді өтінішті кодпен ашу', 'Открыть анонимное обращение по коду с другого устройства')}</summary>
        <div className="field">
          <label htmlFor="fbCode">{tt('Өтініш коды', 'Код обращения')}</label>
          <input className="input" id="fbCode" autoComplete="off" spellCheck={false} value={code} onChange={e => setCode(e.target.value)} />
        </div>
        <button className="btn secondary" type="button" onClick={addCode}>{tt('Ашу', 'Открыть')}</button>
      </details>
    </div>
  );
}

function Thread({ t, tt }: { t: Ticket; tt: TT }) {
  const { lang } = useApp();
  return (
    <ol className="uv-thread" aria-label={tt('Хат алмасу', 'Переписка')}>
      <li className="uv-msg me"><span className="tiny muted">{t.anonymous ? tt('Сіз (анонимді)', 'Вы (анонимно)') : tt('Сіз', 'Вы')} · {day(t.createdAt)}</span><p className="small uv-pre">{t.body}</p></li>
      {t.messages.map((m, i) => (
        <li key={i} className={'uv-msg' + (m.fromStaff ? ' staff' : ' me')}>
          <span className="tiny muted">{m.fromStaff ? recName(t.recipient, lang) : tt('Сіз', 'Вы')} · {day(m.createdAt)}</span>
          <p className="small uv-pre">{m.body}</p>
        </li>
      ))}
    </ol>
  );
}

function TicketCard({ m, open, toggle: flip, reload }: { m: Mine; open: boolean; toggle: () => void; reload: () => Promise<void> }) {
  const { tt, lang } = useApp();
  const t = m.ticket;
  const [reply, setReply] = useState('');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  const body = m.token ? { token: m.token } : {};
  const openIt = () => {
    flip();
    if (!open && t.unread) void api(`/api/feedback/tickets/${t.id}/read`, { method: 'POST', body, lang }).then(() => reload(), () => {});
  };
  const send = async () => {
    setBusy(true); setErr('');
    try {
      await api(`/api/feedback/tickets/${t.id}/messages`, { method: 'POST', body: { ...body, body: reply }, lang });
      setReply('');
      await reload();
    } catch (e) { setErr(errText(e)); } finally { setBusy(false); }
  };
  return (
    <div className="card">
      <button type="button" className="uv-ticket-head" aria-expanded={open} onClick={openIt}>
        <span className="uv-title">{recName(t.recipient, lang)} · {catLabel(t.category, tt)}</span>
        <span className="badges">{statusBadge(t.status, tt)}{t.anonymous ? <span className="badge b-line">{tt('Анонимді', 'Анонимно')}</span> : null}
          {t.unread ? <span className="badge b-ok">{tt('Жаңа жауап', 'Новый ответ')}</span> : null}</span>
      </button>
      {open ? (
        <>
          <Thread t={t} tt={tt} />
          {t.status !== 'closed' && t.status !== 'review' ? (
            <div className="field">
              <label htmlFor={'fbReply' + t.id}>{tt('Жауап жазу', 'Написать ответ')}</label>
              <textarea className="input" id={'fbReply' + t.id} rows={3} maxLength={FEEDBACK_MAX} value={reply} onChange={e => setReply(e.target.value)} />
              <ErrorLine err={err} />
              <button className="btn secondary" type="button" disabled={busy || !reply.trim()} onClick={() => void send()}>{tt('Жіберу', 'Отправить')}</button>
            </div>
          ) : null}
          {m.token ? <button className="btn ghost" type="button" onClick={() => { removeToken(ls(), m.token!); void reload(); }}>{tt('Осы құрылғыдан жою', 'Убрать с этого устройства')}</button> : null}
        </>
      ) : null}
    </div>
  );
}

/* ====================================================================== wellbeing */

export function WellbeingScreen() {
  return <Feature name="wellbeing"><Wellbeing /></Feature>;
}

function LineRow({ h, tt, lang }: { h: (typeof HELP_LINES)[number]; tt: TT; lang: string }) {
  return (
    <li className="uv-line">
      <a className="uv-num" href={telHref(h.number)} aria-label={tt('Қоңырау шалу: ', 'Позвонить: ') + h.number}>{h.number}</a>
      <div>
        <p className="small">{tr(lang as Lang, h.kk, h.ru)}</p>
        <p className="tiny muted">{tr(lang as Lang, h.hours.kk, h.hours.ru)}{h.extra ? ' · ' + h.extra : ''}</p>
      </div>
    </li>
  );
}

export function HelpNowSheet({ contacts }: { contacts: WellbeingContact[] }) {
  const { tt, lang, closeSheet } = useApp();
  return (
    <>
      <h2 id="helpNowTitle">{tt('Қазір көмек керек болса', 'Если помощь нужна сейчас')}</h2>
      <p className="small">{tt('Өміріңізге не басқа біреудің өміріне қауіп төнсе — бірден қоңырау шалыңыз:', 'Если есть угроза вашей или чужой жизни — звоните сразу:')}</p>
      <ul className="uv-lines" aria-label={tt('Шұғыл нөмірлер', 'Экстренные номера')}>{EMERGENCY_LINES.map(h => <LineRow key={h.number} h={h} tt={tt} lang={lang} />)}</ul>
      <p className="small">{tt('Сөйлесу керек болса:', 'Если нужно поговорить:')}</p>
      <ul className="uv-lines">{HELP_LINES.filter(h => !h.emergency).map(h => <LineRow key={h.number} h={h} tt={tt} lang={lang} />)}</ul>
      {contacts.length ? <ContactList contacts={contacts} /> : null}
      <button className="btn ghost block" type="button" onClick={closeSheet}>{tt('Жабу', 'Закрыть')}</button>
    </>
  );
}

function ContactList({ contacts }: { contacts: WellbeingContact[] }) {
  const { tt } = useApp();
  return (
    <ul className="uv-lines" aria-label={tt('Университет психологы', 'Психолог университета')}>
      {contacts.map((c, i) => (
        <li key={i} className="uv-contact">
          <p className="small"><b>{c.name}</b>{c.role ? ' · ' + c.role : ''}</p>
          {c.phone ? <a className="small" href={telHref(c.phone)}>{c.phone}</a> : null}
          {c.email ? <a className="small" href={'mailto:' + c.email}>{c.email}</a> : null}
          {c.place || c.hours ? <p className="tiny muted">{[c.place, c.hours].filter(Boolean).join(' · ')}</p> : null}
        </li>
      ))}
    </ul>
  );
}

const MOODS: [number, string, string][] = [[1, 'Өте ауыр', 'Очень тяжело'], [2, 'Ауыр', 'Тяжело'], [3, 'Орташа', 'Средне'], [4, 'Жақсы', 'Хорошо'], [5, 'Өте жақсы', 'Отлично']];

function Wellbeing() {
  const { tt, lang, openSheet } = useApp();
  const acc = useAccount();
  const univ = acc.user?.university ?? '';
  const { data } = useLoad<{ contacts: WellbeingContact[] }>('/api/wellbeing/contacts' + (univ ? '?university=' + encodeURIComponent(univ) : ''));
  const contacts = useMemo(() => (data?.contacts ?? []).filter(c => !univ || c.university === univ), [data, univ]);
  const [mood, setMood] = useState<MoodEntry[] | null>(null);
  useEffect(() => { void Promise.resolve().then(() => setMood(readMood(ls()))); }, []);
  const today = almatyToday();
  return (
    <div className="stack uv-calm">
      <button className="btn danger block uv-help" type="button" id="helpNow" onClick={() => openSheet(<HelpNowSheet contacts={contacts} />)}>
        <Icon n="bell" s={20} /> {tt('Маған қазір көмек керек', 'Мне нужна помощь сейчас')}
      </button>
      <p className="note">{tt('Бұл бөлім тек ақпарат береді: диагноз қоймайды және дәрігердің не психологтың кеңесін алмастырмайды.', 'Этот раздел только информирует: он не ставит диагнозов и не заменяет консультацию врача или психолога.')}</p>

      <section className="card" aria-labelledby="wbPsy">
        <h2 id="wbPsy">{tt('Университет психологы', 'Психолог университета')}</h2>
        {contacts.length ? <ContactList contacts={contacts} /> : <p className="small muted">{tt('Университет психологының байланыстары әлі қосылмаған. Төмендегі сенім телефондарына хабарласуға болады.', 'Контакты психолога университета пока не добавлены. Можно обратиться на линии доверия ниже.')}</p>}
      </section>

      <section className="card" aria-labelledby="wbLines">
        <h2 id="wbLines">{tt('Қазақстандағы көмек телефондары', 'Телефоны помощи в Казахстане')}</h2>
        <ul className="uv-lines">{HELP_LINES.map(h => <LineRow key={h.number} h={h} tt={tt} lang={lang} />)}</ul>
        <details>
          <summary className="tiny">{tt('Дереккөздер (2026-11-05 тексерілді)', 'Источники (проверено 2026-11-05)')}</summary>
          <ul className="tiny uv-src">{Array.from(new Set(HELP_LINES.flatMap(h => h.sources))).map(u => <li key={u}><a href={u} target="_blank" rel="noopener noreferrer">{u.replace(/^https:\/\//, '').slice(0, 60)}</a></li>)}</ul>
        </details>
      </section>

      <section aria-labelledby="wbSelf" className="stack">
        <h2 id="wbSelf">{tt('Өзіңізге көмек', 'Помочь себе')}</h2>
        {SELF_HELP.map(c => (
          <details key={c.key} className="card uv-card">
            <summary><b>{tr(lang as Lang, c.title.kk, c.title.ru)}</b></summary>
            <ol className="small">{c.steps.map((s, i) => <li key={i}>{tr(lang as Lang, s.kk, s.ru)}</li>)}</ol>
            <p className="tiny muted">{tt('Дереккөз: ', 'Источник: ')}<a href={c.source.url} target="_blank" rel="noopener noreferrer">{c.source.name}</a></p>
          </details>
        ))}
      </section>

      <section className="card" aria-labelledby="wbMood">
        <h2 id="wbMood">{tt('Бүгінгі көңіл-күйім', 'Моё настроение сегодня')}</h2>
        <p className="tiny muted">{tt('Тек осы құрылғыда сақталады, ешқайда жіберілмейді.', 'Хранится только на этом устройстве и никуда не отправляется.')}</p>
        <div className="chips wrap" role="group" aria-label={tt('Көңіл-күй', 'Настроение')}>
          {MOODS.map(([m, kk, ru]) => (
            <button key={m} type="button" className="chip" aria-pressed={mood?.find(x => x.d === today)?.m === m} onClick={() => setMood(addMood(ls(), today, m))}>{tt(kk, ru)}</button>
          ))}
        </div>
        {mood && mood.length > 1 ? (
          <p className="tiny muted">{tt('Соңғы күндер: ', 'Последние дни: ')}{mood.slice(0, 7).map(x => `${x.d.slice(5)} — ${tt(MOODS[x.m - 1][1], MOODS[x.m - 1][2])}`).join(', ')}</p>
        ) : null}
        {mood && mood.length ? <button className="btn ghost" type="button" onClick={() => { try { ls()?.removeItem('studenthub.mood.v1'); } catch { /* ignore */ } setMood([]); }}>{tt('Тарихты өшіру', 'Стереть историю')}</button> : null}
        {mood?.find(x => x.d === today && x.m <= 2) ? <p className="small">{tt('Ауыр болса, жалғыз қалмаңыз — жоғарыдағы нөмірлерге не жақын адамға хабарласыңыз.', 'Если тяжело, не оставайтесь одни — позвоните по номерам выше или близкому человеку.')}</p> : null}
      </section>
    </div>
  );
}

/* ====================================================================== staff cabinet /voice */

export function VoiceStaff() {
  const acc = useAccount();
  const cfg = useConfig();
  const { tt } = useApp();
  const tabs: [string, string][] = [];
  if (cfg.features.surveys) tabs.push(['surveys', tt('Сауалнамалар', 'Опросы')]);
  if (cfg.features.feedback) tabs.push(['inbox', tt('Өтініштер', 'Обращения')]);
  if (cfg.features.feedback || cfg.features.wellbeing) tabs.push(['settings', tt('Баптау', 'Настройки')]);
  const [tab, setTab] = useState('');
  if (!acc.loaded) return <Loading />;
  if (acc.user?.role !== 'admin' && acc.user?.role !== 'university_staff') return <NotFound />;
  if (!tabs.length) return <NotFound />;
  const cur = tabs.some(t => t[0] === tab) ? tab : tabs[0][0];
  return (
    <div className="stack">
      {tabs.length > 1 ? <Seg items={tabs} cur={cur} onPick={setTab} /> : null}
      {cur === 'surveys' ? <StaffSurveys /> : cur === 'inbox' ? <StaffInbox /> : <StaffSettings />}
    </div>
  );
}

/* ---------- surveys: list, editor, results ---------- */

function StaffSurveys() {
  const { tt, lang } = useApp();
  const { data, err, reload } = useLoad<{ surveys: Survey[]; university: string; admin: boolean }>('/api/staff/surveys');
  const [view, setView] = useState<{ k: 'list' } | { k: 'edit'; s: Survey | null } | { k: 'results'; s: Survey }>({ k: 'list' });
  const [busyErr, setBusyErr] = useState('');
  if (!data && !err) return <Loading />;
  if (view.k === 'edit') return <SurveyEditor s={view.s} admin={!!data?.admin} done={() => { setView({ k: 'list' }); reload(); }} />;
  if (view.k === 'results') return <Results s={view.s} back={() => setView({ k: 'list' })} />;
  const act = async (s: Survey, what: 'publish' | 'close' | 'delete') => {
    setBusyErr('');
    try {
      await api(`/api/staff/surveys/${s.id}${what === 'delete' ? '' : '/' + what}`, { method: what === 'delete' ? 'DELETE' : 'POST', lang });
      reload();
    } catch (e) { setBusyErr(errText(e)); }
  };
  const status = (s: Survey) => s.status === 'published' ? <span className="badge b-ok">{tt('Ашық', 'Открыт')}</span>
    : s.status === 'closed' ? <span className="badge b-line">{tt('Жабық', 'Закрыт')}</span> : <span className="badge b-sky">{tt('Жоба', 'Черновик')}</span>;
  return (
    <div className="stack">
      <ErrorLine err={err || busyErr} />
      <button className="btn block" type="button" onClick={() => setView({ k: 'edit', s: null })}><Icon n="plus" s={18} /> {tt('Жаңа сауалнама', 'Новый опрос')}</button>
      {data?.surveys.length === 0 ? <Empty icon="chart">{tt('Әзірге сауалнама жоқ. Үлгіден бастауға болады.', 'Опросов пока нет. Можно начать с шаблона.')}</Empty> : null}
      {data?.surveys.map(s => (
        <div className="card" key={s.id}>
          <h2 className="uv-title">{s.title}</h2>
          <div className="badges">{status(s)}{s.anonymous ? <span className="badge b-line">{tt('Анонимді', 'Анонимный')}</span> : null}
            <span className="badge b-line">{tt(`${s.responses} жауап`, `${s.responses} ответ.`)}</span>{data.admin ? <span className="badge b-line">{s.university}</span> : null}</div>
          <div className="btn-row">
            {s.status === 'draft' ? <button className="btn secondary" type="button" onClick={() => setView({ k: 'edit', s })}>{tt('Өзгерту', 'Изменить')}</button> : null}
            {s.status === 'draft' ? <button className="btn" type="button" onClick={() => void act(s, 'publish')}>{tt('Жариялау', 'Опубликовать')}</button> : null}
            {s.status !== 'draft' ? <button className="btn secondary" type="button" onClick={() => setView({ k: 'results', s })}>{tt('Нәтиже', 'Результаты')}</button> : null}
            {s.status === 'published' ? <button className="btn ghost" type="button" onClick={() => void act(s, 'close')}>{tt('Жабу', 'Закрыть')}</button> : null}
            {s.status !== 'published' ? <button className="btn ghost" type="button" onClick={() => void act(s, 'delete')}>{tt('Жою', 'Удалить')}</button> : null}
          </div>
        </div>
      ))}
    </div>
  );
}

const TYPE_LABEL = (t: QType, tt: TT) => ({
  single: tt('Бір жауап', 'Один ответ'), multiple: tt('Бірнеше жауап', 'Несколько ответов'), scale: tt('Шкала', 'Шкала'),
  text: tt('Қысқа мәтін', 'Короткий текст'), matrix: tt('Матрица', 'Матрица'),
} as Record<QType, string>)[t];

const lines = (s: string) => s.split('\n').map(x => x.trim()).filter(Boolean);

function SurveyEditor({ s, admin, done }: { s: Survey | null; admin: boolean; done: () => void }) {
  const { tt, lang } = useApp();
  const [title, setTitle] = useState(s?.title ?? '');
  const [intro, setIntro] = useState(s?.intro ?? '');
  const [univ, setUniv] = useState(s?.university ?? '');
  const [anon, setAnon] = useState(s?.anonymous ?? true);
  const [closes, setCloses] = useState(day(s?.closesAt));
  const [qs, setQs] = useState<Question[]>(s?.questions ?? []);
  const [fac, setFac] = useState<string[]>(s?.audience.faculties ?? []);
  const [courses, setCourses] = useState<number[]>(s?.audience.courses ?? []);
  const [groups, setGroups] = useState((s?.audience.groups ?? []).join(', '));
  const [err, setErr] = useState('');
  const [busy, setBusy] = useState(false);
  const setQ = (i: number, p: Partial<Question>) => setQs(list => list.map((q, j) => (j === i ? { ...q, ...p } : q)));
  const add = (type: QType) => setQs(list => [...list, {
    id: 'q' + (Math.max(0, ...list.map(q => Number(q.id.slice(1)) || 0)) + 1), type, text: '', required: false,
    ...(type === 'single' || type === 'multiple' ? { options: ['', ''] } : {}), ...(type === 'scale' || type === 'matrix' ? { max: 5 } : {}), ...(type === 'matrix' ? { rows: ['', ''] } : {}),
  }]);
  const save = async () => {
    setBusy(true); setErr('');
    const body = {
      ...(admin ? { university: univ } : {}), title, intro, anonymous: anon, questions: qs,
      audience: { faculties: fac, courses, groups: groups.split(',').map(g => g.trim()).filter(Boolean) },
      closesAt: closes ? new Date(closes + 'T23:59:00+05:00').toISOString() : null,
    };
    try {
      await api(s ? `/api/staff/surveys/${s.id}` : '/api/staff/surveys', { method: s ? 'PUT' : 'POST', body, lang });
      done();
    } catch (e) { setErr(errText(e) + (e instanceof ApiError && e.detail.field ? '' : '')); } finally { setBusy(false); }
  };
  return (
    <form className="stack" onSubmit={e => { e.preventDefault(); void save(); }}>
      {!s ? (
        <div className="card">
          <h2>{tt('Үлгіден бастау', 'Начать с шаблона')}</h2>
          <p className="tiny muted">{tt('Үлгіде тек сұрақтар бар, ешқандай дерек жоқ.', 'В шаблоне только вопросы, никаких данных.')}</p>
          <div className="chips wrap">{templates(tt).map(t => <button key={t.key} type="button" className="chip" onClick={() => { setTitle(t.title); setQs(t.questions); }}>{t.title}</button>)}</div>
        </div>
      ) : null}
      <div className="card form">
        {admin ? <div className="field"><label htmlFor="svU">{tt('Университет', 'Университет')}</label><input className="input" id="svU" value={univ} onChange={e => setUniv(e.target.value)} /></div> : null}
        <div className="field"><label htmlFor="svT">{tt('Атауы', 'Название')}</label><input className="input" id="svT" maxLength={160} value={title} onChange={e => setTitle(e.target.value)} /></div>
        <div className="field"><label htmlFor="svI">{tt('Кіріспе (міндетті емес)', 'Вступление (необязательно)')}</label><textarea className="input" id="svI" rows={3} maxLength={1000} value={intro} onChange={e => setIntro(e.target.value)} /></div>
        <label className="check"><input type="checkbox" checked={anon} onChange={e => setAnon(e.target.checked)} /><span className="small">{tt('Анонимді (ұсынылады): жауаптар атсыз сақталады', 'Анонимный (рекомендуется): ответы хранятся без имени')}</span></label>
        <div className="field"><label htmlFor="svC">{tt('Жабылу күні (міндетті емес)', 'Дата закрытия (необязательно)')}</label><input className="input" id="svC" type="date" value={closes} onChange={e => setCloses(e.target.value)} /></div>
        <fieldset className="field"><legend>{tt('Кімге: факультет (бос — бәріне)', 'Кому: факультет (пусто — всем)')}</legend>
          <div className="chips wrap">{Object.keys(SPECS).filter(k => k !== 'other').map(k => <button key={k} type="button" className="chip" aria-pressed={fac.includes(k)} onClick={() => setFac(f => f.includes(k) ? f.filter(x => x !== k) : [...f, k])}>{tr(lang as Lang, SPECS[k].kk, SPECS[k].ru)}</button>)}</div>
        </fieldset>
        <fieldset className="field"><legend>{tt('Курс (бос — бәріне)', 'Курс (пусто — всем)')}</legend>
          <div className="chips">{[1, 2, 3, 4, 5, 6].map(c => <button key={c} type="button" className="chip" aria-pressed={courses.includes(c)} onClick={() => setCourses(f => f.includes(c) ? f.filter(x => x !== c) : [...f, c])}>{c}</button>)}</div>
        </fieldset>
        <div className="field"><label htmlFor="svG">{tt('Топтар, үтір арқылы (бос — бәріне)', 'Группы через запятую (пусто — всем)')}</label><input className="input" id="svG" value={groups} onChange={e => setGroups(e.target.value)} /></div>
      </div>
      {qs.map((q, i) => (
        <fieldset className="card form" key={q.id}>
          <legend className="small"><b>{i + 1}. {TYPE_LABEL(q.type, tt)}</b></legend>
          <div className="field"><label htmlFor={'qt' + i}>{tt('Сұрақ', 'Вопрос')}</label><input className="input" id={'qt' + i} maxLength={300} value={q.text} onChange={e => setQ(i, { text: e.target.value })} /></div>
          {q.options ? <div className="field"><label htmlFor={'qo' + i}>{tt('Жауап нұсқалары (әр жолға біреу)', 'Варианты (по одному в строке)')}</label><textarea className="input" id={'qo' + i} rows={4} value={q.options.join('\n')} onChange={e => setQ(i, { options: e.target.value.split('\n') })} onBlur={e => setQ(i, { options: lines(e.target.value) })} /></div> : null}
          {q.rows ? <div className="field"><label htmlFor={'qr' + i}>{tt('Жолдар (әр жолға біреу)', 'Строки (по одной в строке)')}</label><textarea className="input" id={'qr' + i} rows={4} value={q.rows.join('\n')} onChange={e => setQ(i, { rows: e.target.value.split('\n') })} onBlur={e => setQ(i, { rows: lines(e.target.value) })} /></div> : null}
          {q.max ? <div className="field"><label htmlFor={'qm' + i}>{tt('Шкала', 'Шкала')}</label><select className="input" id={'qm' + i} value={q.max} onChange={e => setQ(i, { max: Number(e.target.value) })}><option value={5}>1–5</option><option value={10}>1–10</option></select></div> : null}
          <label className="check"><input type="checkbox" checked={q.required} onChange={e => setQ(i, { required: e.target.checked })} /><span className="small">{tt('Міндетті', 'Обязательный')}</span></label>
          <div className="btn-row">
            <button className="btn ghost" type="button" disabled={i === 0} onClick={() => setQs(l => { const c = [...l]; [c[i - 1], c[i]] = [c[i], c[i - 1]]; return c; })}>{tt('Жоғары', 'Выше')}</button>
            <button className="btn ghost" type="button" onClick={() => setQs(l => l.filter((_, j) => j !== i))}>{tt('Өшіру', 'Удалить')}</button>
          </div>
        </fieldset>
      ))}
      <div className="card">
        <p className="small">{tt('Сұрақ қосу:', 'Добавить вопрос:')}</p>
        <div className="chips wrap">{(['single', 'multiple', 'scale', 'text', 'matrix'] as QType[]).map(t => <button key={t} type="button" className="chip" onClick={() => add(t)}>+ {TYPE_LABEL(t, tt)}</button>)}</div>
      </div>
      <ErrorLine err={err} />
      <div className="btn-row">
        <button className="btn ghost" type="button" onClick={done}>{tt('Болдырмау', 'Отмена')}</button>
        <button className="btn" type="submit" disabled={busy || !qs.length}>{tt('Жобаны сақтау', 'Сохранить черновик')}</button>
      </div>
    </form>
  );
}

function Bars({ q, tt }: { q: QResult; tt: TT }) {
  if (q.hidden) return <p className="small muted">{tt(`Жасырылды: ${K_MIN}-тен аз жауап (${q.answered}).`, `Скрыто: меньше ${K_MIN} ответов (${q.answered}).`)}</p>;
  if (q.type === 'text') {
    return (
      <>
        <p className="tiny muted">{tt('Жауаптар кездейсоқ ретпен', 'Ответы в случайном порядке')}</p>
        <ul className="uv-texts">{(q.texts ?? []).map((t, i) => <li key={i} className="small uv-pre">{t}</li>)}</ul>
      </>
    );
  }
  if (q.type === 'matrix') {
    return (
      <ul className="uv-bars">{(q.rows ?? []).map((r, i) => (
        <li key={i}><span className="small">{r}</span><span className="uv-bar"><span style={{ width: pct(q.rowMeans?.[i] ?? 0, Number(q.labels?.length ?? 5)) + '%' }} /></span><b className="small num">{q.rowMeans?.[i]}</b></li>
      ))}</ul>
    );
  }
  const total = q.type === 'multiple' ? q.answered : (q.counts ?? []).reduce((a, b) => a + b, 0);
  return (
    <>
      {q.mean !== undefined ? <p className="small">{tt('Орташа: ', 'Среднее: ')}<b>{q.mean}</b></p> : null}
      <ul className="uv-bars">{(q.labels ?? []).map((l, i) => (
        <li key={i}><span className="small">{l}</span><span className="uv-bar" aria-hidden="true"><span style={{ width: pct(q.counts?.[i] ?? 0, total) + '%' }} /></span>
          <b className="small num">{q.counts?.[i]} · {pct(q.counts?.[i] ?? 0, total)}%</b></li>
      ))}</ul>
    </>
  );
}

function Results({ s, back }: { s: Survey; back: () => void }) {
  const { tt, lang } = useApp();
  const [by, setBy] = useState('');
  const { data, err } = useLoad<SurveyResults>(`/api/staff/surveys/${s.id}/results${by ? '?by=' + by : ''}`);
  const [dlErr, setDlErr] = useState('');
  const segName = (k: string) => k === 'other' ? tt('Басқалары', 'Остальные') : by === 'faculty' ? (SPECS[k] ? tr(lang as Lang, SPECS[k].kk, SPECS[k].ru) : k) : tt(`${k}-курс`, `${k} курс`);
  return (
    <div className="stack">
      <button className="btn ghost" type="button" onClick={back}><Icon n="back" s={18} /> {tt('Тізімге', 'К списку')}</button>
      <ErrorLine err={err || dlErr} />
      {!data ? <Loading /> : (
        <>
          <div className="card">
            <h2>{data.survey.title}</h2>
            <p className="small">{tt('Жауап бергендер: ', 'Ответили: ')}<b className="num">{data.responses}</b></p>
            <p className="tiny muted">{tt(`Құпиялық: ${K_MIN}-тен аз жауабы бар топ пен сұрақ көрсетілмейді.`, `Конфиденциальность: группы и вопросы с ответами меньше ${K_MIN} не показываются.`)}</p>
            <div className="btn-row">
              <button className="btn secondary" type="button" onClick={() => apiDownload(`/api/staff/surveys/${s.id}/results.csv`, `survey-${s.id}.csv`, lang).catch(e => setDlErr(errText(e)))}>
                <Icon n="doc" s={18} /> CSV ({tt('тек жиынтық', 'только сводка')})</button>
            </div>
            <Seg items={[['', tt('Барлығы', 'Все')], ['faculty', tt('Факультет', 'Факультет')], ['course', tt('Курс', 'Курс')]]} cur={by} onPick={setBy} />
          </div>
          {data.hidden ? <Empty icon="shield">{tt(`Нәтиже кемінде ${K_MIN} жауап жиналғанда көрсетіледі.`, `Результаты появятся, когда наберётся минимум ${K_MIN} ответов.`)}</Empty> : null}
          {!data.hidden && by && data.breakdownHidden ? <p className="note">{tt('Бұл бөлініс жасырылды: кейбір топтар тым шағын, оларды есептеп шығаруға болар еді.', 'Эта разбивка скрыта: некоторые группы слишком малы, их можно было бы вычислить.')}</p> : null}
          {!data.hidden && !by ? data.questions.map((q, i) => (
            <section className="card" key={q.id} aria-label={q.text}>
              <h3 className="small"><b>{i + 1}. {q.text}</b> <span className="tiny muted">({q.answered})</span></h3>
              <Bars q={q} tt={tt} />
            </section>
          )) : null}
          {!data.hidden && by && data.segments ? data.segments.map(sg => (
            <section className="card" key={sg.key}>
              <h3>{segName(sg.key)} <span className="tiny muted">({sg.responses})</span></h3>
              {sg.questions.filter(q => q.type !== 'text').map(q => <div key={q.id}><p className="small"><b>{q.text}</b></p><Bars q={q} tt={tt} /></div>)}
            </section>
          )) : null}
        </>
      )}
    </div>
  );
}

/* ---------- inbox ---------- */

function StaffInbox() {
  const { tt, lang } = useApp();
  const [status, setStatus] = useState('open');
  const { data, err, reload } = useLoad<{ tickets: Ticket[]; recipients: Recipient[] }>('/api/staff/feedback?status=' + status);
  const [open, setOpen] = useState(0);
  return (
    <div className="stack">
      <Seg items={[['open', tt('Ашық', 'Открытые')], ['answered', tt('Жауап берілген', 'С ответом')], ['closed', tt('Жабық', 'Закрытые')], ['all', tt('Барлығы', 'Все')]]} cur={status} onPick={setStatus} />
      <ErrorLine err={err} />
      {data && data.recipients.length === 0 ? <p className="note">{tt('Сізге әлі бірде-бір алушы (бөлім) бекітілмеген. Әкімшіге хабарласыңыз.', 'Вам ещё не назначен ни один получатель (отдел). Обратитесь к администратору.')}</p> : null}
      {!data ? <Loading /> : data.tickets.length === 0 ? <Empty icon="chat">{tt('Өтініш жоқ.', 'Обращений нет.')}</Empty> : null}
      {data?.tickets.map(t => (
        <div className="card" key={t.id}>
          <button type="button" className="uv-ticket-head" aria-expanded={open === t.id} onClick={() => setOpen(open === t.id ? 0 : t.id)}>
            <span className="uv-title">#{t.id} · {recName(t.recipient, lang)} · {catLabel(t.category, tt)}</span>
            <span className="badges">{statusBadge(t.status, tt)}
              {t.overdue ? <span className="badge b-bad">{tt('Мерзімі өтті', 'Просрочено')}</span> : null}
              <span className="badge b-line">{t.anonymous ? tt('Анонимді', 'Анонимно') : (t.author?.name ?? tt('Атымен', 'С именем'))}</span></span>
          </button>
          {open === t.id ? <StaffTicket t={t} reload={reload} /> : <p className="small uv-clip">{t.body}</p>}
        </div>
      ))}
    </div>
  );
}

function StaffTicket({ t, reload }: { t: Ticket; reload: () => void }) {
  const { tt, lang } = useApp();
  const { data } = useLoad<{ ticket: Ticket }>('/api/staff/feedback/' + t.id);
  const [reply, setReply] = useState('');
  const [st, setSt] = useState('answered');
  const [err, setErr] = useState('');
  const [busy, setBusy] = useState(false);
  const full = data?.ticket ?? t;
  const send = async () => {
    setBusy(true); setErr('');
    try { await api(`/api/staff/feedback/${t.id}/reply`, { method: 'POST', body: { body: reply, status: st }, lang }); setReply(''); reload(); }
    catch (e) { setErr(errText(e)); } finally { setBusy(false); }
  };
  const setStatus = async (s: string) => {
    try { await api(`/api/staff/feedback/${t.id}/status`, { method: 'POST', body: { status: s }, lang }); reload(); } catch (e) { setErr(errText(e)); }
  };
  return (
    <>
      <p className="tiny muted">{full.anonymous ? tt('Анонимді: жіберушіні ешкім көрмейді, тіпті жүйе де білмейді.', 'Анонимно: отправителя не видит никто, даже система.') : ''} {tt('Жауап мерзімі: ', 'Срок ответа: ')}{day(full.slaDueAt)}</p>
      <ol className="uv-thread">
        <li className="uv-msg me"><span className="tiny muted">{tt('Студент', 'Студент')} · {day(full.createdAt)}</span><p className="small uv-pre">{full.body}</p></li>
        {full.messages.map((m, i) => <li key={i} className={'uv-msg' + (m.fromStaff ? ' staff' : ' me')}><span className="tiny muted">{m.fromStaff ? tt('Қызметкер', 'Сотрудник') : tt('Студент', 'Студент')} · {day(m.createdAt)}</span><p className="small uv-pre">{m.body}</p></li>)}
      </ol>
      {/* eslint-disable-next-line @next/next/no-img-element -- private, session-only photo; not optimizable */}
      {full.photo ? <img className="uv-photo" src={full.photo} alt={tt('Өтінішке қосылған фото', 'Фото к обращению')} /> : null}
      <div className="field">
        <label htmlFor={'stR' + t.id}>{tt('Жауап', 'Ответ')}</label>
        <textarea className="input" id={'stR' + t.id} rows={3} maxLength={FEEDBACK_MAX} value={reply} onChange={e => setReply(e.target.value)} />
      </div>
      <div className="field">
        <label htmlFor={'stS' + t.id}>{tt('Жауаптан кейінгі күй', 'Статус после ответа')}</label>
        <select className="input" id={'stS' + t.id} value={st} onChange={e => setSt(e.target.value)}>
          <option value="answered">{tt('Жауап берілді', 'Есть ответ')}</option><option value="in_progress">{tt('Қаралуда', 'В работе')}</option><option value="closed">{tt('Жабылды', 'Закрыто')}</option>
        </select>
      </div>
      <ErrorLine err={err} />
      <div className="btn-row">
        <button className="btn" type="button" disabled={busy || !reply.trim()} onClick={() => void send()}>{tt('Жауап жіберу', 'Отправить ответ')}</button>
        {full.status !== 'closed' ? <button className="btn ghost" type="button" onClick={() => void setStatus('closed')}>{tt('Жабу', 'Закрыть')}</button> : null}
      </div>
    </>
  );
}

/* ---------- settings: recipients (admin) and psychologist contacts ---------- */

function StaffSettings() {
  const cfg = useConfig();
  const acc = useAccount();
  return (
    <div className="stack">
      {cfg.features.feedback && acc.user?.role === 'admin' ? <RecipientsAdmin /> : null}
      {cfg.features.wellbeing ? <ContactsEditor /> : null}
    </div>
  );
}

function RecipientsAdmin() {
  const { tt, lang } = useApp();
  const [univ, setUniv] = useState('');
  const { data, err, reload } = useLoad<{ recipients: Recipient[] }>('/api/admin/feedback/recipients' + (univ ? '?university=' + encodeURIComponent(univ) : ''));
  const [msg, setMsg] = useState('');
  const run = async (fn: () => Promise<unknown>) => { setMsg(''); try { await fn(); reload(); } catch (e) { setMsg(errText(e)); } };
  return (
    <section className="card" aria-labelledby="rcpH">
      <h2 id="rcpH">{tt('Өтініш алушылар', 'Получатели обращений')}</h2>
      <div className="field"><label htmlFor="rcpU">{tt('Университет', 'Университет')}</label><input className="input" id="rcpU" value={univ} onChange={e => setUniv(e.target.value)} /></div>
      <button className="btn secondary" type="button" disabled={!univ.trim()} onClick={() => void run(() => api('/api/admin/feedback/recipients', { method: 'POST', body: { university: univ.trim(), defaults: true }, lang }))}>
        {tt('Әдепкі тізімді қосу (Деканат, Ректорат, …)', 'Добавить стандартный список (Деканат, Ректорат, …)')}</button>
      <ErrorLine err={err || msg} />
      <p className="tiny muted">{tt('Студенттер алушыны кемінде бір қызметкер бекітілгенде ғана көреді. Қызметкер — admin не осы университетке бекітілген university_staff.', 'Студенты видят получателя, только когда назначен хотя бы один сотрудник: admin или university_staff этого университета.')}</p>
      <ul className="uv-lines">
        {data?.recipients.map(r => <RecipientRow key={r.id} r={r} run={run} />)}
      </ul>
    </section>
  );
}

function RecipientRow({ r, run }: { r: Recipient; run: (fn: () => Promise<unknown>) => Promise<void> }) {
  const { tt, lang } = useApp();
  const [ids, setIds] = useState((r.staff ?? []).map(s => s.userId).join(', '));
  return (
    <li className="uv-contact">
      <p className="small"><b>{recName(r, lang)}</b> · {r.university} · SLA {r.slaHours} {tt('сағ', 'ч')} {r.active ? '' : '· ' + tt('өшірулі', 'выключен')}</p>
      <p className="tiny muted">{(r.staff ?? []).map(s => `${s.name ?? '—'} (#${s.userId})`).join(', ') || tt('Қызметкер жоқ', 'Нет сотрудников')}</p>
      <label className="tiny" htmlFor={'rcpS' + r.id}>{tt('Қызметкер id-лері, үтір арқылы', 'id сотрудников через запятую')}</label>
      <input className="input" id={'rcpS' + r.id} value={ids} onChange={e => setIds(e.target.value)} />
      <div className="btn-row">
        <button className="btn secondary" type="button" onClick={() => void run(() => api(`/api/admin/feedback/recipients/${r.id}/staff`, { method: 'PUT', body: { userIds: ids.split(',').map(x => Number(x.trim())).filter(n => n > 0) }, lang }))}>{tt('Сақтау', 'Сохранить')}</button>
        <button className="btn ghost" type="button" onClick={() => void run(() => api(`/api/admin/feedback/recipients/${r.id}`, { method: 'PATCH', body: { active: !r.active }, lang }))}>{r.active ? tt('Өшіру', 'Выключить') : tt('Қосу', 'Включить')}</button>
      </div>
    </li>
  );
}

const emptyContact = (): WellbeingContact => ({ university: '', name: '', role: '', phone: '', email: '', place: '', hours: '', updatedAt: '' });

function ContactsEditor() {
  const { tt, lang, toast } = useApp();
  const acc = useAccount();
  const admin = acc.user?.role === 'admin';
  const [univ, setUniv] = useState('');
  const { data: cur } = useLoad<{ contacts: WellbeingContact[]; university: string }>(admin && !univ.trim() ? null
    : '/api/staff/wellbeing/contacts' + (admin ? '?university=' + encodeURIComponent(univ.trim()) : ''));
  const target = admin ? univ.trim() : cur?.university ?? '';
  const [list, setList] = useState<WellbeingContact[] | null>(null);
  const shown = list ?? cur?.contacts ?? [];
  const [err, setErr] = useState('');
  const set = (i: number, p: Partial<WellbeingContact>) => setList(shown.map((c, j) => (j === i ? { ...c, ...p } : c)));
  const save = async () => {
    setErr('');
    try {
      await api('/api/staff/wellbeing/contacts', { method: 'PUT', body: { university: target, contacts: shown.map(c => ({ ...c, phone: c.phone || null, email: c.email || null })) }, lang });
      toast(tt('Сақталды', 'Сохранено'));
    } catch (e) { setErr(errText(e)); }
  };
  const field = (i: number, k: 'name' | 'role' | 'phone' | 'email' | 'place' | 'hours', label: string) => (
    <div className="field"><label htmlFor={`wc${k}${i}`}>{label}</label><input className="input" id={`wc${k}${i}`} value={shown[i][k] ?? ''} onChange={e => set(i, { [k]: e.target.value })} /></div>
  );
  return (
    <section className="card form" aria-labelledby="wcH">
      <h2 id="wcH">{tt('Психолог байланыстары', 'Контакты психолога')}</h2>
      <p className="tiny muted">{tt('Тек нақты, тексерілген байланыстарды енгізіңіз. Бос болса, студенттер «әлі қосылмаған» деп көреді.', 'Вносите только реальные проверенные контакты. Пока пусто, студенты видят «ещё не добавлены».')}</p>
      {admin ? <div className="field"><label htmlFor="wcU">{tt('Университет', 'Университет')}</label><input className="input" id="wcU" value={univ} onChange={e => { setUniv(e.target.value); setList(null); }} /></div> : null}
      {shown.map((_, i) => (
        <fieldset key={i} className="uv-contact">
          <legend className="small"><b>#{i + 1}</b></legend>
          {field(i, 'name', tt('Аты не қызмет атауы', 'Имя или название службы'))}
          {field(i, 'role', tt('Лауазымы', 'Должность'))}
          {field(i, 'phone', tt('Телефон', 'Телефон'))}
          {field(i, 'email', 'Email')}
          {field(i, 'place', tt('Орны', 'Где'))}
          {field(i, 'hours', tt('Қабылдау уақыты', 'Часы приёма'))}
          <button className="btn ghost" type="button" onClick={() => setList(shown.filter((_, j) => j !== i))}>{tt('Өшіру', 'Удалить')}</button>
        </fieldset>
      ))}
      <ErrorLine err={err} />
      <div className="btn-row">
        <button className="btn secondary" type="button" disabled={!target || shown.length >= 12} onClick={() => setList([...shown, emptyContact()])}><Icon n="plus" s={18} /> {tt('Қосу', 'Добавить')}</button>
        <button className="btn" type="button" disabled={!target} onClick={() => void save()}>{tt('Сақтау', 'Сохранить')}</button>
      </div>
    </section>
  );
}

