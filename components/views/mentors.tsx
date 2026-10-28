'use client';
/* Mentors (prompt 07): /mentors (ask for a mentor, honest numbers, group meetings), /mentors/become,
   /mentors/me (own mentor profile, pairs with check-ins, end, report), and the manual matching board used by
   /admin/mentors (admin) and /ambassador/mentors (ambassador team lead, own university only). */
import { useState } from 'react';
import { useApp } from '@/components/app';
import { Chip, Empty, Icon, Row, Seg, SectionHead } from '@/components/ui';
import { AdminOnly, ErrorLine, errText, Feature, hasAccount, Loading, NotFound, ReasonSheet, SignInCard, useLoad, useNeedAccount, ymd } from '@/components/views/common';
import { api, useAccount } from '@/lib/account';
import {
  EXPECTS, FORMATS, NEEDS, PAIR_REPORT_REASONS, SAFETY,
  type AdminPair, type Candidate, type Mentor, type MentorStats, type MentorSummary, type Pair, type WaitingRequest,
} from '@/lib/mentors';
import { openTelegramLink } from '@/lib/telegram';

export function Mentors({ prm }: { prm: string }) {
  return (
    <Feature name="mentors">
      {!prm ? <MentorsHome /> : prm === 'become' ? <Become /> : prm === 'me' ? <MentorsMe /> : <NotFound />}
    </Feature>
  );
}

function StatsLine({ s }: { s: MentorStats }) {
  const { tt } = useApp();
  const days = s.typicalDays == null ? tt('Жұптау уақыты туралы дерек әлі аз.', 'Данных о сроке подбора пока мало.')
    : tt(`Әдетте жұптау ${Math.round(s.typicalDays)} күн алады.`, `Обычно подбор занимает ${Math.round(s.typicalDays)} дн.`);
  return <p className="small">{tt(`Қазір ${s.mentors} ментор, ${s.waiting} сұраныс күтіп тұр.`, `Сейчас ${s.mentors} менторов, ${s.waiting} заявок ждут.`)} {days}</p>;
}

function GroupMeetings({ list }: { list: MentorSummary['groupMeetings'] }) {
  const { tt, go } = useApp();
  if (!list.length) return <p className="note">{tt('Жақын топтық кездесу әлі жоқ. Ашылса — Telegram-да хабарлаймыз.', 'Ближайших групповых встреч пока нет. Когда появятся — сообщим в Telegram.')}</p>;
  return (
    <div className="group">
      {list.map(g => <Row key={g.id} icon="calendar" title={g.title} sub={`${g.startsAt.replace('T', ' ')}${g.place ? ' · ' + g.place : ''}`} onClick={() => go('event:' + g.id)} />)}
    </div>
  );
}

function MentorsHome() {
  const { tt, L, lang, toast, go } = useApp();
  const acc = useAccount();
  const need = useNeedAccount();
  const sum = useLoad<MentorSummary>(acc.loaded ? '/api/mentors/summary' + (acc.user ? '?u=' + acc.user.id : '') : null);
  const [needK, setNeedK] = useState('');
  const [goal, setGoal] = useState('');
  const [expect, setExpect] = useState('any');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  const [after, setAfter] = useState<{ offerGroup: boolean; groupOnly: boolean } | null>(null);
  if (!sum.data) return sum.err ? <ErrorLine err={sum.err} /> : <Loading />;
  const s = sum.data;
  const minor = s.me ? s.me.isAdult !== true : false;
  const req = s.me?.request;
  const submit = async () => {
    if (!need() || !needK) return;
    setBusy(true); setErr('');
    try {
      const r = await api<{ offerGroup: boolean; groupOnly: boolean }>('/api/mentors/requests', { method: 'POST', body: { need: needK, goal: goal.trim(), expect: minor ? 'group' : expect }, lang });
      setAfter({ offerGroup: r.offerGroup, groupOnly: r.groupOnly });
      sum.reload();
    } catch (e) { setErr(errText(e)); } finally { setBusy(false); }
  };
  return (
    <>
      <div className="card">
        <h2>{tt('Алғашқы айда көмек', 'Помощь в первый месяц')}</h2>
        <p className="small muted">{tt('Әр студентке жеке ментор уәде етпейміз: менторлар — ерікті студенттер. Ментор жетпесе, топтық кездесу ұсынамыз.', 'Мы не обещаем личного ментора каждому: менторы — студенты-волонтёры. Если менторов не хватает, предложим групповую встречу.')}</p>
        {s.stats ? <StatsLine s={s.stats} /> : null}
      </div>
      {req && req.status !== 'closed' ? (
        <div className="card">
          <span className="eyebrow">{tt('Сіздің сұранысыңыз', 'Ваша заявка')}</span>
          <b>{L(NEEDS[req.need])}</b>
          {req.goal ? <p className="small" style={{ overflowWrap: 'anywhere' }}>{req.goal}</p> : null}
          <span className="badges"><span className={`badge ${req.status === 'matched' ? 'b-ok' : 'b-line'}`}>{req.status === 'matched' ? tt('Ментор табылды', 'Ментор найден') : tt('Күтуде', 'Ожидает')}</span>
            <span className="badge b-line">{L(EXPECTS[req.expect])}</span></span>
          {after?.groupOnly || (after && minor) ? <p className="small">{tt('Сізге топтық менторлық пен университеттегі ашық кеңес сағаттары ұсынылады.', 'Вам доступны групповое менторство и открытые часы консультаций в университете.')}</p> : null}
          {req.status === 'matched'
            ? <button className="btn" type="button" onClick={() => go('mentors:me')}>{tt('Жұбымды ашу', 'Открыть мою пару')}</button>
            : <button className="btn ghost" type="button" disabled={busy} onClick={async () => {
              setBusy(true);
              try { await api('/api/mentors/requests/close', { method: 'POST', lang }); toast(tt('Сұраныс қайтарылды', 'Заявка отозвана')); setAfter(null); sum.reload(); }
              catch (e) { toast(errText(e)); } finally { setBusy(false); }
            }}>{tt('Сұранысты қайтару', 'Отозвать заявку')}</button>}
        </div>
      ) : (
        <form className="form" onSubmit={e => { e.preventDefault(); void submit(); }}>
          <div className="field"><span className="lbl" id="mNeedL">{tt('Не керек?', 'Что нужно?')}</span>
            <div className="chips wrap" role="radiogroup" aria-labelledby="mNeedL">{Object.entries(NEEDS).map(([k, n]) => <Chip key={k} pressed={needK === k} onClick={() => setNeedK(k)}>{L(n)}</Chip>)}</div></div>
          <div className="field"><label htmlFor="mGoal">{tt('Нақты не сұрағың келеді?', 'Что конкретно хочешь спросить?')}</label>
            <textarea className="input" id="mGoal" maxLength={300} value={goal} onChange={e => setGoal(e.target.value)} />
            <span className="tiny muted num">{goal.length} / 300</span></div>
          {minor ? <p className="note">{tt('18-ге толмағандарға жеке ментор берілмейді: сізге топтық менторлық пен ашық кеңес сағаттары ұсынылады.', 'Тем, кому нет 18, личный ментор не назначается: вам доступны групповое менторство и открытые часы консультаций.')}</p>
            : <div className="field"><span className="lbl" id="mExpL">{tt('Не күтесің?', 'Чего ждёшь?')}</span>
              <div className="chips wrap" role="radiogroup" aria-labelledby="mExpL">{Object.entries(EXPECTS).map(([k, n]) => <Chip key={k} pressed={expect === k} onClick={() => setExpect(k)}>{L(n)}</Chip>)}</div></div>}
          <ErrorLine err={err} />
          <button className="btn block" type="submit" disabled={!needK || busy}>{tt('Сұраныс қалдыру', 'Оставить заявку')}</button>
          {!acc.user ? <p className="tiny muted">{tt('Сұраныс үшін Telegram арқылы кіру керек.', 'Для заявки нужен вход через Telegram.')}</p> : null}
        </form>
      )}
      {after?.offerGroup || minor || (s.stats && s.stats.freeMentors === 0) || s.groupMeetings.length ? (
        <section className="section"><SectionHead title={tt('Жақын топтық кездесулер', 'Ближайшие групповые встречи')} /><GroupMeetings list={s.groupMeetings} /></section>
      ) : null}
      <div className="group">
        {s.me?.mentorStatus ? <Row go="mentors:me" icon="cap" title={tt('Менің менторлығым', 'Моё менторство')} />
          : <Row go="mentors:become" icon="cap" title={tt('Ментор болу', 'Стать ментором')} sub={tt('2–4 курс студенттері мен түлектер, 18+', 'Студенты 2–4 курса и выпускники, 18+')} />}
        {req?.status === 'matched' && !s.me?.mentorStatus ? null : acc.user ? <Row go="mentors:me" icon="user" title={tt('Менің жұптарым', 'Мои пары')} /> : null}
      </div>
    </>
  );
}

function Become() {
  const { tt, L, lang } = useApp();
  const acc = useAccount();
  const me = useLoad<{ mentor: Mentor | null }>(acc.user ? '/api/mentors/me' : null);
  const [areas, setAreas] = useState<string[]>([]);
  const [format, setFormat] = useState('both');
  const [about, setAbout] = useState('');
  const [alumni, setAlumni] = useState(false);
  const [cap, setCap] = useState(2);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  const [done, setDone] = useState(false);
  if (!acc.loaded) return <Loading />;
  if (!hasAccount(acc)) return <SignInCard text={tt('Ментор болу үшін Telegram арқылы кіріңіз.', 'Чтобы стать ментором, войдите через Telegram.')} />;
  const m = me.data?.mentor;
  if (!me.data) return me.err ? <ErrorLine err={me.err} /> : <Loading />;
  if (done || m?.status === 'pending') {
    return <div className="card" role="status"><b>{tt('Өтінім тексерілуде', 'Заявка на проверке')}</b>
      <p className="small">{tt('Бекітер алдында әкімші сізбен 10–15 минут қысқа әңгімелеседі: неге ментор болғыңыз келеді, қанша уақыт бере аласыз, қауіпсіздік ережелері.', 'Перед одобрением администратор коротко (10–15 минут) поговорит с вами: почему хотите быть ментором, сколько времени готовы уделять, правила безопасности.')}</p></div>;
  }
  if (m && (m.status === 'approved' || m.status === 'paused')) return <div className="card"><b>{tt('Сіз — менторсыз', 'Вы — ментор')}</b></div>;
  if (m?.status === 'removed') return <p className="note">{tt('Ментор рөлі әкімшінің шешімімен алынған.', 'Роль ментора снята решением администратора.')}</p>;
  const u = acc.user!;
  const adult = u.isAdult === true;
  const courseOk = u.course != null && u.course >= 2 && u.course <= 4;
  return (
    <form className="form" onSubmit={async e => {
      e.preventDefault();
      if (!areas.length || busy) return;
      setBusy(true); setErr('');
      try {
        await api('/api/mentors/apply', { method: 'POST', body: { areas, format, about: about.trim(), alumni, capacity: cap }, lang });
        setDone(true);
      } catch (e2) { setErr(errText(e2)); } finally { setBusy(false); }
    }}>
      <div className="card">
        <b>{tt('Кім бола алады', 'Кто может стать')}</b>
        <span className="small elig"><Icon n={adult ? 'check' : 'x'} s={16} /> {tt('18 жасқа толған', 'Есть 18 лет')}</span>
        <span className="small elig"><Icon n={courseOk || alumni ? 'check' : 'x'} s={16} /> {tt('2–4 курс студенті не түлек', 'Студент 2–4 курса или выпускник')}{u.course ? tt(` (профильде: ${u.course}-курс)`, ` (в профиле: ${u.course} курс)`) : ''}</span>
        {m?.status === 'rejected' ? <span className="small muted">{tt('Бұрынғы өтінім қайтарылған — қайта беруге болады.', 'Прошлая заявка отклонена — можно подать снова.')}</span> : null}
      </div>
      <label className="check"><input type="checkbox" checked={alumni} onChange={e => setAlumni(e.target.checked)} /><span className="small">{tt('Мен түлекпін', 'Я выпускник')}</span></label>
      <div className="field"><span className="lbl" id="bAreasL">{tt('Қай бағытта көмектесе аласыз?', 'С чем можете помочь?')}</span>
        <div className="chips wrap" aria-labelledby="bAreasL">{Object.entries(NEEDS).map(([k, n]) => <Chip key={k} pressed={areas.includes(k)} onClick={() => setAreas(a => a.includes(k) ? a.filter(x => x !== k) : [...a, k])}>{L(n)}</Chip>)}</div></div>
      <div className="field"><span className="lbl">{tt('Формат', 'Формат')}</span>
        <Seg items={Object.entries(FORMATS).map(([k, f]) => [k, L(f)])} cur={format} onPick={setFormat} /></div>
      <div className="field"><label htmlFor="bCap">{tt('Бір мезгілде қанша менти (1–5)', 'Сколько менти одновременно (1–5)')}</label>
        <select className="input" id="bCap" value={cap} onChange={e => setCap(+e.target.value)}>{[1, 2, 3, 4, 5].map(n => <option key={n} value={n}>{n}</option>)}</select></div>
      <div className="field"><label htmlFor="bAbout">{tt('Өзіңіз туралы', 'О себе')}</label>
        <textarea className="input" id="bAbout" maxLength={300} value={about} onChange={e => setAbout(e.target.value)} />
        <span className="tiny muted num">{about.length} / 300</span></div>
      <p className="note">{L(SAFETY)}</p>
      <ErrorLine err={err} />
      <button className="btn block" type="submit" disabled={!areas.length || busy || !adult || !(courseOk || alumni)}>{tt('Өтінім жіберу', 'Отправить заявку')}</button>
    </form>
  );
}

function MentorsMe() {
  const { tt, L, lang, toast } = useApp();
  const acc = useAccount();
  const me = useLoad<{ mentor: Mentor | null }>(acc.user ? '/api/mentors/me' : null);
  const pairs = useLoad<{ pairs: Pair[] }>(acc.user ? '/api/mentors/pairs' : null);
  const [busy, setBusy] = useState(false);
  if (!acc.loaded) return <Loading />;
  if (!acc.user) return <SignInCard text={tt('Кіріңіз.', 'Войдите.')} />;
  const m = me.data?.mentor;
  const reload = () => { me.reload(); pairs.reload(); };
  return (
    <>
      {m ? (
        <div className="card">
          <span className="eyebrow">{tt('Ментор профилі', 'Профиль ментора')}</span>
          <b>{m.areas.map(a => L(NEEDS[a])).join(', ')}</b>
          <span className="small muted">{L(FORMATS[m.format])} · {tt(`жұптар ${m.activePairs} / ${m.capacity}`, `пары ${m.activePairs} из ${m.capacity}`)}</span>
          <span className="badges">
            <span className={`badge ${m.status === 'approved' ? 'b-ok' : 'b-line'}`}>{({ approved: tt('Белсенді', 'Активен'), paused: tt('Кідіртілген', 'На паузе'), pending: tt('Тексерілуде', 'На проверке'), rejected: tt('Қайтарылған', 'Отклонено'), removed: tt('Алынған', 'Снят') } as Record<string, string>)[m.status]}</span>
            {m.badge ? <span className="badge b-sun"><Icon n="award" s={12} /> {tt('Ментор · расталған', 'Ментор · подтверждено')}</span> : null}
          </span>
          {m.status === 'approved' || m.status === 'paused' ? (
            <button className="btn secondary" type="button" disabled={busy} onClick={async () => {
              setBusy(true);
              try { await api('/api/mentors/me', { method: 'PATCH', body: { status: m.status === 'paused' ? 'approved' : 'paused' }, lang }); toast(m.status === 'paused' ? tt('Жалғастырдыңыз', 'Вы снова активны') : tt('Кідіртілді — жаңа менти берілмейді', 'Пауза — новых менти не будет')); reload(); }
              catch (e) { toast(errText(e)); } finally { setBusy(false); }
            }}>{m.status === 'paused' ? tt('Жалғастыру', 'Возобновить') : tt('Сессия кезеңі — кідірту', 'Сессия — пауза')}</button>
          ) : null}
        </div>
      ) : null}
      <section className="section"><SectionHead title={tt('Жұптар', 'Пары')} />
        {pairs.err ? <ErrorLine err={pairs.err} /> : !pairs.data ? <Loading /> : pairs.data.pairs.length
          ? pairs.data.pairs.map(p => <PairCard key={p.id} p={p} onChange={reload} />)
          : <Empty icon="user">{tt('Әзірге жұп жоқ.', 'Пар пока нет.')}</Empty>}
      </section>
    </>
  );
}

function PairCard({ p, onChange }: { p: Pair; onChange: () => void }) {
  const { tt, L, lang, toast, openSheet } = useApp();
  const [met, setMet] = useState<boolean | null>(null);
  const [score, setScore] = useState(0);
  const [rematch, setRematch] = useState(false);
  const [busy, setBusy] = useState(false);
  const active = !p.endedAt;
  const post = async (path: string, body: unknown) => {
    setBusy(true);
    try { return await api<Record<string, unknown>>(`/api/mentors/pairs/${p.id}/${path}`, { method: 'POST', body, lang }); }
    catch (e) { toast(errText(e)); return null; } finally { setBusy(false); }
  };
  return (
    <div className="card">
      <div className="card-row">
        <span className="ico"><Icon n={p.role === 'mentor' ? 'user' : 'cap'} /></span>
        <div className="row-main">
          <span className="eyebrow">{p.role === 'mentor' ? tt('Сіздің ментиіңіз', 'Ваш менти') : tt('Сіздің менторыңыз', 'Ваш ментор')}</span>
          <span className="row-title">{p.partner.name}</span>
          <span className="row-sub">{[p.partner.faculty, p.partner.university, L(NEEDS[p.need])].filter(Boolean).join(' · ')}</span>
          <span className="tiny muted">{tt(`${ymd(p.startedAt)} бастап`, `с ${ymd(p.startedAt)}`)}{p.endedAt ? tt(` · ${ymd(p.endedAt)} жабылды`, ` · закрыта ${ymd(p.endedAt)}`) : ''}</span>
        </div>
      </div>
      {active && p.partner.username ? <button className="btn" type="button" onClick={() => openTelegramLink('https://t.me/' + p.partner.username)}><Icon n="send" s={18} /> @{p.partner.username}</button>
        : active ? <p className="tiny muted">{tt('Байланыс Telegram-бот арқылы өтеді.', 'Связь идёт через Telegram-бота.')}</p> : null}
      {active ? <p className="note">{L(SAFETY)}</p> : null}
      {active && p.due.includes('check3') ? (
        <div className="form">
          <b>{tt('Кездестіңдер ме?', 'Вы встретились?')}</b>
          <div className="chips"><Chip pressed={met === true} onClick={() => setMet(true)}>{tt('Иә', 'Да')}</Chip><Chip pressed={met === false} onClick={() => setMet(false)}>{tt('Жоқ', 'Нет')}</Chip></div>
          <span className="small">{tt('Пайдасы бар ма? (1–5, міндетті емес)', 'Есть ли польза? (1–5, необязательно)')}</span>
          <div className="chips fb-score">{[1, 2, 3, 4, 5].map(n => <Chip key={n} pressed={score === n} onClick={() => setScore(score === n ? 0 : n)}>{n}</Chip>)}</div>
          <button className="btn secondary" type="button" disabled={met === null || busy} onClick={async () => {
            const r = await post('checkin', score ? { kind: 'check3', met, score } : { kind: 'check3', met });
            if (r) { setRematch(!!r.offerRematch); toast(tt('Рақмет!', 'Спасибо!')); if (!r.offerRematch) onChange(); }
          }}>{tt('Жауап беру', 'Ответить')}</button>
        </div>
      ) : null}
      {rematch && p.role === 'mentee' ? (
        <div className="card"><p className="small">{tt('Ешкім кінәлі емес. Басқа ментор іздейік пе?', 'Никто не виноват. Поищем другого ментора?')}</p>
          <button className="btn" type="button" disabled={busy} onClick={async () => { if (await post('end', { rematch: true })) { toast(tt('Жаңа сұраныс ашылды', 'Открыта новая заявка')); onChange(); } }}>{tt('Иә, басқа ментор', 'Да, другого ментора')}</button></div>
      ) : null}
      {active && p.due.includes('month') ? (
        <div className="form"><b>{tt('Алғашқы ай аяқталды. Жалғастырамыз ба?', 'Первый месяц позади. Продолжаем?')}</b>
          <span className="tiny muted">{tt('Екеуіңіз де «иә» десеңіз ғана жұп жалғасады.', 'Пара продолжится, только если оба ответят «да».')}</span>
          <div className="btn-row">
            <button className="btn" type="button" disabled={busy} onClick={async () => { const r = await post('checkin', { kind: 'month', continue: true }); if (r) { toast(r.continued ? tt('Жұп жалғасады', 'Пара продолжается') : tt('Жауабыңыз сақталды', 'Ответ сохранён')); onChange(); } }}>{tt('Иә', 'Да')}</button>
            <button className="btn ghost" type="button" disabled={busy} onClick={async () => { if (await post('checkin', { kind: 'month', continue: false })) { toast(tt('Жұп жабылды', 'Пара закрыта')); onChange(); } }}>{tt('Жоқ', 'Нет')}</button>
          </div></div>
      ) : null}
      {!active && p.myScore == null ? (
        <div className="form"><span className="small">{tt('Жұпты бағалаңыз', 'Оцените пару')}</span>
          <div className="chips fb-score">{[1, 2, 3, 4, 5].map(n => <Chip key={n} pressed={false} onClick={async () => { if (await post('score', { score: n })) { toast(tt('Рақмет!', 'Спасибо!')); onChange(); } }}>{n}</Chip>)}</div></div>
      ) : null}
      <div className="btn-row">
        {active ? <button className="btn ghost" type="button" onClick={() => openSheet(<EndPairSheet p={p} onDone={onChange} />)}>{tt('Жұпты жабу', 'Закрыть пару')}</button> : null}
        <button className="btn danger" type="button" onClick={() => openSheet(<PairReportSheet p={p} onDone={onChange} />)}>{tt('Шағымдану', 'Пожаловаться')}</button>
      </div>
    </div>
  );
}

function EndPairSheet({ p, onDone }: { p: Pair; onDone: () => void }) {
  const { tt, lang, toast, closeSheet } = useApp();
  const [reason, setReason] = useState('');
  const [score, setScore] = useState(0);
  const [err, setErr] = useState('');
  return (
    <form className="form" onSubmit={async e => {
      e.preventDefault();
      try {
        await api(`/api/mentors/pairs/${p.id}/end`, { method: 'POST', body: score ? { reason: reason.trim(), score } : { reason: reason.trim() }, lang });
        closeSheet(); toast(tt('Жұп жабылды', 'Пара закрыта')); onDone();
      } catch (e2) { setErr(errText(e2)); }
    }}>
      <h2>{tt('Жұпты жабу', 'Закрыть пару')}</h2>
      <div className="field"><label htmlFor="epReason">{tt('Себебі (міндетті емес)', 'Причина (необязательно)')}</label>
        <textarea className="input" id="epReason" maxLength={300} value={reason} onChange={e => setReason(e.target.value)} /></div>
      <span className="small">{tt('Баға (міндетті емес)', 'Оценка (необязательно)')}</span>
      <div className="chips fb-score">{[1, 2, 3, 4, 5].map(n => <Chip key={n} pressed={score === n} onClick={() => setScore(score === n ? 0 : n)}>{n}</Chip>)}</div>
      <ErrorLine err={err} />
      <div className="btn-row">
        <button className="btn ghost" type="button" onClick={closeSheet}>{tt('Бас тарту', 'Отмена')}</button>
        <button className="btn danger" type="submit">{tt('Жабу', 'Закрыть')}</button>
      </div>
    </form>
  );
}

function PairReportSheet({ p, onDone }: { p: Pair; onDone: () => void }) {
  const { tt, L, lang, toast, closeSheet } = useApp();
  const [reason, setReason] = useState('');
  const [text, setText] = useState('');
  const [endPair, setEndPair] = useState(true);
  const [err, setErr] = useState('');
  return (
    <form className="form" onSubmit={async e => {
      e.preventDefault();
      if (!reason) return;
      try {
        await api(`/api/mentors/pairs/${p.id}/report`, { method: 'POST', body: { reason, text: text.trim(), endPair: !p.endedAt && endPair }, lang });
        closeSheet(); toast(tt('Шағым әкімшіге жіберілді', 'Жалоба отправлена администратору')); onDone();
      } catch (e2) { setErr(errText(e2)); }
    }}>
      <h2>{tt('Шағымдану', 'Пожаловаться')}</h2>
      <div className="chips wrap" role="radiogroup">{Object.entries(PAIR_REPORT_REASONS).map(([k, r]) => <Chip key={k} pressed={reason === k} onClick={() => setReason(k)}>{L(r)}</Chip>)}</div>
      <div className="field"><label htmlFor="prText">{tt('Не болды? (міндетті емес)', 'Что случилось? (необязательно)')}</label>
        <textarea className="input" id="prText" maxLength={500} value={text} onChange={e => setText(e.target.value)} /></div>
      {!p.endedAt ? <label className="check"><input type="checkbox" checked={endPair} onChange={e => setEndPair(e.target.checked)} /><span className="small">{tt('Жұпты да бірден жабу', 'Сразу закрыть и пару')}</span></label> : null}
      <ErrorLine err={err} />
      <div className="btn-row">
        <button className="btn ghost" type="button" onClick={closeSheet}>{tt('Бас тарту', 'Отмена')}</button>
        <button className="btn danger" type="submit" disabled={!reason}>{tt('Жіберу', 'Отправить')}</button>
      </div>
    </form>
  );
}

/* ---------- matching board ---------- */
export function AdminMentors() {
  return <Feature name="mentors"><AdminOnly><MatchBoard base="/api/admin" admin /></AdminOnly></Feature>;
}
export function AmbassadorMentors() {
  const acc = useAccount();
  return (
    <Feature name="mentors">
      {!acc.loaded ? <Loading /> : acc.user?.role !== 'ambassador' ? <NotFound /> : <MatchBoard base="/api/ambassador" />}
    </Feature>
  );
}

function MatchBoard({ base, admin }: { base: string; admin?: boolean }) {
  const { tt, L, lang, toast, openSheet } = useApp();
  const [uni, setUni] = useState('');
  const [uniInput, setUniInput] = useState('');
  const board = useLoad<{ university: string | null; requests: WaitingRequest[]; stats: MentorStats | null }>(`${base}/mentors${admin && uni ? '?university=' + encodeURIComponent(uni) : ''}`);
  const [sel, setSel] = useState<number | null>(null);
  const cands = useLoad<{ requestId: number; groupOnly: boolean; candidates: Candidate[] }>(sel ? `${base}/mentors/requests/${sel}/candidates` : null);
  const [busy, setBusy] = useState(false);
  if (board.code === 'not_lead' || board.code === 'forbidden' || board.code === 'no_university') return board.code === 'forbidden' ? <NotFound /> : <p className="note">{board.err}</p>;
  if (board.err) return <ErrorLine err={board.err} />;
  if (!board.data) return <Loading />;
  const reqs = board.data.requests;
  const cur = reqs.find(r => r.id === sel);
  return (
    <>
      {admin ? (
        <form className="search" role="search" onSubmit={e => { e.preventDefault(); setUni(uniInput.trim()); setSel(null); }}>
          <Icon n="search" s={18} /><input className="input" type="search" aria-label={tt('Университет бойынша сүзу', 'Фильтр по университету')} placeholder={tt('Университет (бос = бәрі)', 'Университет (пусто = все)')} value={uniInput} onChange={e => setUniInput(e.target.value)} />
        </form>
      ) : <p className="small muted">{board.data.university}</p>}
      {board.data.stats ? <StatsLine s={board.data.stats} /> : null}
      <p className="note">{tt('Сұрыптау — тек ұсыныс. Шешімді адам қабылдайды.', 'Сортировка — только подсказка. Решение принимает человек.')}</p>
      <div className="match-board">
        <section className="section"><SectionHead title={tt('Күтіп тұрған сұраныстар', 'Ожидающие заявки')} more={<span className="tiny muted num">{reqs.length}</span>} />
          <div className="group">{reqs.length ? reqs.map(r => (
            <button key={r.id} className="row" type="button" aria-pressed={sel === r.id} onClick={() => setSel(r.id)}>
              <span className="row-main">
                <span className="row-title">{r.name}</span>
                <span className="row-sub">{[r.faculty, r.course ? tt(`${r.course}-курс`, `${r.course} курс`) : '', L(NEEDS[r.need])].filter(Boolean).join(' · ')}</span>
                {r.goal ? <span className="row-sub" style={{ overflowWrap: 'anywhere' }}>{r.goal}</span> : null}
                <span className="badges"><span className="badge b-line num">{tt(`${r.waitingDays} күн`, `${r.waitingDays} дн.`)}</span>
                  {r.groupOnly ? <span className="badge b-sun">{tt('Тек топтық', 'Только группа')}</span> : null}
                  {sel === r.id ? <span className="badge b-ok">{tt('Таңдалды', 'Выбрано')}</span> : null}</span>
              </span>
            </button>
          )) : <div className="empty">{tt('Күтіп тұрған сұраныс жоқ', 'Ожидающих заявок нет')}</div>}</div>
        </section>
        <section className="section"><SectionHead title={tt('Сай менторлар', 'Подходящие менторы')} />
          {!cur ? <p className="note">{tt('Сол жақтан сұранысты таңдаңыз.', 'Выберите заявку слева.')}</p>
            : cur.groupOnly ? <p className="note">{tt('Бұл студентке жеке жұп жасалмайды — топтық менторлық ұсыныңыз.', 'Этому студенту личная пара не назначается — предложите групповое менторство.')}</p>
              : !cands.data ? (cands.err ? <ErrorLine err={cands.err} /> : <Loading />)
                : cands.data.candidates.length ? <div className="group">{cands.data.candidates.map(c => (
                  <div key={c.userId} className="row">
                    <span className="row-main">
                      <span className="row-title">{c.name}</span>
                      <span className="row-sub">{[c.faculty, c.alumni ? tt('түлек', 'выпускник') : c.course ? tt(`${c.course}-курс`, `${c.course} курс`) : '', c.areas.map(a => L(NEEDS[a])).join(', ')].filter(Boolean).join(' · ')}</span>
                      <span className="badges">
                        {c.reasons.areaMatch ? <span className="badge b-ok">{tt('Бағыты сай', 'Совпадает направление')}</span> : null}
                        {c.reasons.sameFaculty ? <span className="badge b-ok">{tt('Бір факультет', 'Тот же факультет')}</span> : null}
                        <span className="badge b-line num">{tt(`Бос орын ${c.freeSlots}`, `Свободно ${c.freeSlots}`)}</span>
                        {c.avgScore != null ? <span className="badge b-line num">{c.avgScore.toFixed(1)} / 5</span> : null}
                      </span>
                    </span>
                    <button className="btn secondary reg-btn" type="button" disabled={busy} onClick={async () => {
                      setBusy(true);
                      try { await api(`${base}/mentors/match`, { method: 'POST', body: { requestId: cur.id, mentorId: c.userId }, lang }); toast(tt('Жұп құрылды. Екеуіне хабар кетті', 'Пара создана. Обоим отправлено сообщение')); setSel(null); board.reload(); }
                      catch (e) { toast(errText(e)); } finally { setBusy(false); }
                    }}>{tt('Жұптау', 'Связать')}</button>
                  </div>
                ))}</div> : <p className="note">{tt('Бос орны бар сай ментор жоқ. Топтық кездесу ұсыныңыз.', 'Свободных подходящих менторов нет. Предложите групповую встречу.')}</p>}
        </section>
      </div>
      {admin ? <AdminPairs uni={uni} openSheet={openSheet} /> : null}
    </>
  );
}

function AdminPairs({ uni, openSheet }: { uni: string; openSheet: (n: React.ReactNode) => void }) {
  const { tt, L, lang, toast, closeSheet } = useApp();
  const [st, setSt] = useState('active');
  const pairs = useLoad<{ pairs: AdminPair[] }>(`/api/admin/mentors/pairs?status=${st}${uni ? '&university=' + encodeURIComponent(uni) : ''}`);
  const mentorStatus = (p: AdminPair) => openSheet(<MentorStatusSheet p={p} onDone={pairs.reload} />);
  return (
    <section className="section"><SectionHead title={tt('Жұптар', 'Пары')} />
      <Seg items={[['active', tt('Белсенді', 'Активные')], ['ended', tt('Жабылған', 'Закрытые')]]} cur={st} onPick={setSt} />
      <div className="group">{pairs.data?.pairs.length ? pairs.data.pairs.map(p => (
        <div key={p.id} className="row">
          <span className="row-main">
            <span className="row-title">{p.mentorName} → {p.menteeName}</span>
            <span className="row-sub">{[p.university, L(NEEDS[p.need]), ymd(p.startedAt)].join(' · ')}{p.endReason ? ' · ' + p.endReason : ''}</span>
            <span className="btn-row" style={{ marginTop: 6 }}>
              {!p.endedAt ? <button className="btn ghost reg-btn" type="button" onClick={() => openSheet(<ReasonSheet title={tt('Жұпты ажырату', 'Разъединить пару')} label={tt('Себебі (міндетті)', 'Причина (обязательно)')} action={tt('Ажырату', 'Разъединить')} danger
                onSubmit={async r => { await api(`/api/admin/mentors/pairs/${p.id}/end`, { method: 'POST', body: { reason: r }, lang }); closeSheet(); toast(tt('Жұп жабылды', 'Пара закрыта')); pairs.reload(); }} />)}>{tt('Ажырату', 'Разъединить')}</button> : null}
              <button className="btn ghost reg-btn" type="button" onClick={() => mentorStatus(p)}>{tt('Ментор күйі', 'Статус ментора')}</button>
            </span>
          </span>
        </div>
      )) : <div className="empty">{pairs.data ? tt('Жұп жоқ', 'Пар нет') : tt('Жүктеліп жатыр…', 'Загрузка…')}</div>}</div>
    </section>
  );
}

function MentorStatusSheet({ p, onDone }: { p: AdminPair; onDone: () => void }) {
  const { tt, lang, toast, closeSheet } = useApp();
  const [status, setStatus] = useState('paused');
  return (
    <ReasonSheet title={tt(`Ментор: ${p.mentorName}`, `Ментор: ${p.mentorName}`)} label={tt('Себебі (міндетті, журналға жазылады)', 'Причина (обязательно, пишется в журнал)')} action={tt('Сақтау', 'Сохранить')} danger={status === 'removed'}
      extra={<Seg items={[['approved', tt('Бекітілген', 'Одобрен')], ['paused', tt('Кідірту', 'Пауза')], ['removed', tt('Рөлді алу', 'Снять роль')]]} cur={status} onPick={setStatus} />}
      onSubmit={async r => {
        const res = await api<{ endedPairs: number }>(`/api/admin/mentors/${p.mentorId}/status`, { method: 'POST', body: { status, reason: r }, lang });
        closeSheet(); toast(tt(`Сақталды. Жабылған жұп: ${res.endedPairs}`, `Сохранено. Закрыто пар: ${res.endedPairs}`)); onDone();
      }} />
  );
}

/** the mentors entry row for the services board / profile; hidden while the switch is off */
export function MentorsEntry() {
  const { tt } = useApp();
  return <Row go="mentors" icon="cap" title={tt('Менторлар', 'Менторы')} sub={tt('Алғашқы айда көмек: жатақхана, құжаттар, оқу', 'Помощь в первый месяц: общежитие, документы, учёба')} />;
}
