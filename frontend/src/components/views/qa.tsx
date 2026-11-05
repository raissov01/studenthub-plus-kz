'use client';
/* «Пән бойынша сұрақ-жауап» (FEATURE_COURSEQA): /qa recent questions of my university, /qa?id=s:<subject> one subject's
   board (linked from the subject page), /qa?id=new[:<subject>] ask, /qa?id=<n> a thread. Reading is open; writing needs
   an account. «Anonymous» hides the author from classmates only (moderators still know). Exam answers are not posted:
   moderators lock such threads. */
import { useState } from 'react';
import { useApp } from '@/components/app';
import { Chip, ConfirmSheet, Empty, Icon, Row, SectionHead } from '@/components/ui';
import { agoDays, ErrorLine, errText, Feature, hasAccount, Loading, NotFound, SignInCard, useLoad, useNeedAccount } from '@/components/views/common';
import { ReportLink, RuleWarning } from '@/components/views/community-ui';
import { useQueryId, useSubjects } from '@/components/views/subjects';
import { api, useAccount } from '@/lib/account';
import {
  hasPhoneOrHandle, parseQaRoute, parseTags, QA_LIMITS, qaBoard, type QAAnswer, type QADetail, type QAList, type QAQuestion,
} from '@/lib/community';

export function QAScreen() {
  const r = parseQaRoute(useQueryId());
  return (
    <Feature name="courseqa">
      {r.kind === 'list' ? <QAListView subject="" /> : r.kind === 'board' ? <QAListView subject={r.subject} />
        : r.kind === 'new' ? <QAAsk subject={r.subject} /> : r.kind === 'question' ? <QAThread id={r.id} /> : <NotFound />}
    </Feature>
  );
}

function HonestyRule() {
  const { tt } = useApp();
  return (
    <p className="note">{tt('Академиялық адалдық: емтихан не бақылау кезінде тапсырма жауаптарын жарияламаңыз. Модераторлар мұндай тақырыпты жабады.',
      'Академическая честность: не публикуйте ответы во время экзамена или контрольной. Модераторы закрывают такие темы.')}</p>
  );
}

const daysAgo = (iso: string) => Math.max(0, Math.floor((Date.now() - Date.parse(iso)) / 86_400_000));

function QuestionRow({ q }: { q: QAQuestion }) {
  const { tt } = useApp();
  return (
    <Row go={'qa:' + q.id} icon="qa" title={q.title}
      sub={[q.subject, tt(`${q.answers} жауап`, `ответов: ${q.answers}`), agoDays(daysAgo(q.createdAt), tt)].join(' · ')}
      end={q.acceptedAnswerId ? <span className="badge b-ok">{tt('Шешілді', 'Решено')}</span> : q.locked ? <span className="badge b-line">{tt('Жабық', 'Закрыто')}</span> : undefined} />
  );
}

function QAListView({ subject }: { subject: string }) {
  const { tt, go } = useApp();
  const acc = useAccount();
  const [faculty, setFaculty] = useState<'mine' | 'all'>('mine');
  const [open, setOpen] = useState(false);
  const [q, setQ] = useState('');
  const [query, setQuery] = useState('');
  const hasFaculty = !!acc.user?.faculty;
  const qs = new URLSearchParams();
  if (subject) qs.set('subject', subject);
  if (hasFaculty && faculty === 'all') qs.set('faculty', 'all');
  if (open) qs.set('sort', 'open');
  if (query) qs.set('q', query);
  const { data, err } = useLoad<QAList>(acc.loaded ? '/api/qa/questions?' + qs.toString() : null);
  return (
    <>
      {subject ? <div className="card"><span className="eyebrow">{tt('Пән', 'Предмет')}</span><h1 className="subj-title">{subject}</h1></div> : null}
      <button className="btn block" type="button" onClick={() => go(subject ? 'qa:new:' + subject : 'qa:new')}><Icon n="plus" s={18} /> {tt('Сұрақ қою', 'Задать вопрос')}</button>
      <form className="field" role="search" onSubmit={e => { e.preventDefault(); setQuery(q.trim()); }}>
        <label htmlFor="qaSearch">{tt('Сұрақтардан іздеу', 'Поиск по вопросам')}</label>
        <input className="input" id="qaSearch" type="search" maxLength={64} value={q} onChange={e => setQ(e.target.value)} onBlur={() => setQuery(q.trim())} />
      </form>
      <div className="chips">
        {hasFaculty ? <>
          <Chip pressed={faculty === 'mine'} onClick={() => setFaculty('mine')}>{tt('Менің факультетім', 'Мой факультет')}</Chip>
          <Chip pressed={faculty === 'all'} onClick={() => setFaculty('all')}>{tt('Бүкіл университет', 'Весь университет')}</Chip>
        </> : null}
        <Chip pressed={open} onClick={() => setOpen(x => !x)}>{tt('Шешілмегендер', 'Без решения')}</Chip>
      </div>
      <ErrorLine err={err} />
      {!data && !err ? <Loading /> : data?.questions.length ? (
        <div className="group" aria-label={tt('Сұрақтар', 'Вопросы')}>{data.questions.map(x => <QuestionRow key={x.id} q={x} />)}</div>
      ) : data ? (
        <Empty icon="qa" action={<button className="btn secondary" type="button" onClick={() => go(subject ? 'qa:new:' + subject : 'qa:new')}>{tt('Бірінші сұрақ', 'Первый вопрос')}</button>}>
          {subject ? tt('Бұл пән бойынша әзірге сұрақ жоқ.', 'По этому предмету пока нет вопросов.') : tt('Әзірге сұрақ жоқ.', 'Пока нет вопросов.')}
        </Empty>
      ) : null}
      <HonestyRule />
    </>
  );
}

function QAAsk({ subject: initial }: { subject: string }) {
  const { tt, lang, go } = useApp();
  const acc = useAccount();
  const { list } = useSubjects();
  const [f, setF] = useState({ subject: initial, title: '', body: '', tags: '', anonymous: false, faculty: false, honesty: false });
  const [err, setErr] = useState('');
  const [busy, setBusy] = useState(false);
  if (!acc.loaded) return <Loading />;
  if (!hasAccount(acc)) return <SignInCard text={tt('Сұрақ қою үшін кіріңіз. Курстастарыңыздан аноним түрде де сұрай аласыз.', 'Чтобы задать вопрос, войдите. Можно спросить однокурсников и анонимно.')} />;
  const tags = parseTags(f.tags);
  const titleLen = Array.from(f.title.trim()).length;
  const problem = tags === null ? 'bad_tags' : hasPhoneOrHandle(f.title + '\n' + f.body) ? 'has_contact' : '';
  const ok = !problem && f.honesty && titleLen >= 8 && Array.from(f.subject.trim()).length >= 2;
  return (
    <form className="form" onSubmit={async e => {
      e.preventDefault();
      if (!ok || busy) return;
      setBusy(true); setErr('');
      try {
        const r = await api<{ question: QAQuestion }>('/api/qa/questions', {
          method: 'POST', lang,
          body: { subject: f.subject.trim(), title: f.title.trim(), body: f.body.trim(), tags, anonymous: f.anonymous, scope: f.faculty ? 'faculty' : 'university', honesty: f.honesty },
        });
        go('qa:' + r.question.id, { replace: true });
      } catch (e2) { setErr(errText(e2)); } finally { setBusy(false); }
    }}>
      <div className="field"><label htmlFor="qaSubject">{tt('Пән', 'Предмет')}</label>
        <input className="input" id="qaSubject" list="qaSubjects" maxLength={QA_LIMITS.subject} value={f.subject} onChange={e => setF(x => ({ ...x, subject: e.target.value }))} />
        <datalist id="qaSubjects">{list.map(s => <option key={s.id} value={s.name} />)}</datalist></div>
      <div className="field"><label htmlFor="qaTitle">{tt('Сұрақ', 'Вопрос')}</label>
        <input className="input" id="qaTitle" maxLength={QA_LIMITS.title} value={f.title} onChange={e => setF(x => ({ ...x, title: e.target.value }))}
          placeholder={tt('Мысалы: Туындыны қалай табамыз?', 'Например: как найти производную?')} />
        <span className="tiny muted num">{titleLen} / {QA_LIMITS.title}</span></div>
      <div className="field"><label htmlFor="qaBody">{tt('Толығырақ (міндетті емес)', 'Подробнее (необязательно)')}</label>
        <textarea className="input" id="qaBody" maxLength={QA_LIMITS.body} rows={5} value={f.body} onChange={e => setF(x => ({ ...x, body: e.target.value }))} /></div>
      <div className="field"><label htmlFor="qaTags">{tt('Тегтер (5-ке дейін, үтірмен)', 'Теги (до 5, через запятую)')}</label>
        <input className="input" id="qaTags" maxLength={140} value={f.tags} onChange={e => setF(x => ({ ...x, tags: e.target.value }))} placeholder={tt('туынды, емтихан', 'производная, экзамен')} /></div>
      {acc.user?.faculty ? (
        <label className="check"><input type="checkbox" checked={f.faculty} onChange={e => setF(x => ({ ...x, faculty: e.target.checked }))} />
          <span>{tt(`Тек менің факультетім (${acc.user.faculty})`, `Только мой факультет (${acc.user.faculty})`)}</span></label>
      ) : null}
      <label className="check"><input type="checkbox" checked={f.anonymous} onChange={e => setF(x => ({ ...x, anonymous: e.target.checked }))} />
        <span>{tt('Курстастарға атымды көрсетпеу (модераторлар көреді)', 'Не показывать имя однокурсникам (модераторы видят)')}</span></label>
      <label className="check cm-rule"><input type="checkbox" checked={f.honesty} onChange={e => setF(x => ({ ...x, honesty: e.target.checked }))} />
        <span>{tt('Бұл емтихан не бақылау тапсырмасының жауабын сұрау емес', 'Это не просьба дать ответы на экзамен или контрольную')}</span></label>
      <RuleWarning code={problem} />
      <ErrorLine err={err} />
      <button className="btn block" type="submit" disabled={!ok || busy}>{tt('Жариялау', 'Опубликовать')}</button>
      <HonestyRule />
    </form>
  );
}

function VoteButton({ kind, id, score, voted, own, onDone }: { kind: 'question' | 'answer'; id: number; score: number; voted: boolean; own: boolean; onDone: () => void }) {
  const { tt, lang, toast } = useApp();
  const need = useNeedAccount();
  const [busy, setBusy] = useState(false);
  return (
    <button className="cm-vote" type="button" aria-pressed={voted} disabled={own || busy}
      aria-label={tt(`Пайдалы, ${score} дауыс`, `Полезно, голосов: ${score}`)}
      onClick={async () => {
        if (!need()) return;
        setBusy(true);
        try { await api('/api/qa/vote', { method: 'POST', body: { kind, id, up: !voted }, lang }); onDone(); } catch (e) { toast(errText(e)); } finally { setBusy(false); }
      }}>
      <Icon n="check" s={16} /> <span className="num">{score}</span>
    </button>
  );
}

function AnswerCard({ a, q, reload }: { a: QAAnswer; q: QAQuestion; reload: () => void }) {
  const { tt, lang, toast, openSheet } = useApp();
  const accept = async (answerId: number | null) => {
    try {
      const r = await api<{ award: unknown }>(`/api/qa/questions/${q.id}/accept`, { method: 'POST', body: { answerId }, lang });
      void r; toast(answerId ? tt('Шешім белгіленді', 'Решение отмечено') : tt('Белгі алынды', 'Отметка снята')); reload();
    } catch (e) { toast(errText(e)); }
  };
  return (
    <article className={`card cm-answer${a.accepted ? ' accepted' : ''}`} aria-label={a.accepted ? tt('Шешім', 'Решение') : tt('Жауап', 'Ответ')}>
      {a.accepted ? <span className="badges"><span className="badge b-ok"><Icon n="check" s={14} /> {tt('Шешілді', 'Решение')}</span></span> : null}
      <p className="cm-body">{a.body}</p>
      <div className="cm-meta">
        <span>{a.anonymous ? tt('Аноним', 'Аноним') : a.author?.name ?? tt('Студент', 'Студент')}{a.authorId ? ` · id ${a.authorId}` : ''}</span>
        <span>{agoDays(daysAgo(a.createdAt), tt)}</span>
      </div>
      <div className="btn-row">
        <VoteButton kind="answer" id={a.id} score={a.score} voted={a.voted} own={a.mine} onDone={reload} />
        {q.mine && q.status === 'active' ? (a.accepted
          ? <button className="btn ghost" type="button" onClick={() => accept(null)}>{tt('Белгіні алу', 'Снять отметку')}</button>
          : <button className="btn secondary" type="button" onClick={() => accept(a.id)}>{tt('Шешім деп белгілеу', 'Отметить решением')}</button>) : null}
        {a.mine ? <button className="btn ghost" type="button" onClick={() => openSheet(<ConfirmSheet msg={tt('Жауапты өшіру керек пе?', 'Удалить ответ?')} yes={async () => {
          try { await api(`/api/qa/answers/${a.id}`, { method: 'DELETE', lang }); reload(); } catch (e) { toast(errText(e)); }
        }} />)}>{tt('Өшіру', 'Удалить')}</button> : null}
      </div>
      {!a.mine ? <ReportLink kind="qa_answer" id={a.id} /> : null}
    </article>
  );
}

function AnswerForm({ q, done }: { q: QAQuestion; done: () => void }) {
  const { tt, lang } = useApp();
  const acc = useAccount();
  const [body, setBody] = useState('');
  const [anon, setAnon] = useState(false);
  const [err, setErr] = useState('');
  const [busy, setBusy] = useState(false);
  if (!hasAccount(acc)) return <SignInCard text={tt('Жауап беру үшін кіріңіз.', 'Чтобы ответить, войдите.')} />;
  const n = Array.from(body.trim()).length;
  const contact = hasPhoneOrHandle(body);
  return (
    <form className="form card" onSubmit={async e => {
      e.preventDefault();
      if (busy || n < 2 || contact) return;
      setBusy(true); setErr('');
      try { await api(`/api/qa/questions/${q.id}/answers`, { method: 'POST', body: { body: body.trim(), anonymous: anon }, lang }); setBody(''); done(); }
      catch (e2) { setErr(errText(e2)); } finally { setBusy(false); }
    }}>
      <div className="field"><label htmlFor="qaAnswer">{tt('Сіздің жауабыңыз', 'Ваш ответ')}</label>
        <textarea className="input" id="qaAnswer" rows={4} maxLength={QA_LIMITS.answer} value={body} onChange={e => setBody(e.target.value)} /></div>
      <label className="check"><input type="checkbox" checked={anon} onChange={e => setAnon(e.target.checked)} />
        <span>{tt('Атымды көрсетпеу', 'Не показывать имя')}</span></label>
      {contact ? <RuleWarning code="has_contact" /> : null}
      <ErrorLine err={err} />
      <button className="btn" type="submit" disabled={busy || n < 2 || contact}>{tt('Жауап беру', 'Ответить')}</button>
    </form>
  );
}

function QAThread({ id }: { id: string }) {
  const { tt, go, lang, toast, openSheet } = useApp();
  const acc = useAccount();
  const { data, err, code, reload } = useLoad<QADetail>(acc.loaded ? `/api/qa/questions/${id}${acc.user ? '?u=' + acc.user.id : ''}` : null);
  if (code === 'not_found') return <NotFound />;
  if (err) return <ErrorLine err={err} />;
  if (!data) return <Loading />;
  const q = data.question;
  return (
    <>
      <article className="card" aria-labelledby="qaTitleH">
        <button className="link-btn" type="button" style={{ alignSelf: 'flex-start' }} onClick={() => go(qaBoard(q.subject))}>{q.subject}</button>
        <h1 id="qaTitleH" className="subj-title">{q.title}</h1>
        {q.body ? <p className="cm-body">{q.body}</p> : null}
        {q.tags.length ? <div className="badges">{q.tags.map(t => <span key={t} className="badge b-line">#{t}</span>)}</div> : null}
        <div className="cm-meta">
          <span>{q.anonymous ? tt('Аноним', 'Аноним') : q.author?.name ?? tt('Студент', 'Студент')}{q.authorId ? ` · id ${q.authorId}` : ''}</span>
          {q.facultyOnly ? <span>{tt('Факультет ішінде', 'Внутри факультета')}</span> : null}
          <span>{agoDays(daysAgo(q.createdAt), tt)}</span>
        </div>
        <div className="btn-row">
          <VoteButton kind="question" id={q.id} score={q.score} voted={q.voted} own={q.mine} onDone={reload} />
          {q.mine ? <button className="btn ghost" type="button" onClick={() => openSheet(<ConfirmSheet msg={tt('Сұрақты өшіру керек пе?', 'Удалить вопрос?')} yes={async () => {
            try { await api(`/api/qa/questions/${q.id}`, { method: 'DELETE', lang }); go(qaBoard(q.subject), { replace: true }); } catch (e) { toast(errText(e)); }
          }} />)}>{tt('Өшіру', 'Удалить')}</button> : null}
        </div>
      </article>
      {q.locked ? (
        <div className="card cm-lock" role="status">
          <b>{tt('Тақырып модератормен жабылған', 'Тема закрыта модератором')}</b>
          {q.lockedReason ? <span className="small">{q.lockedReason}</span> : null}
        </div>
      ) : null}
      <SectionHead title={tt(`Жауаптар (${data.answers.length})`, `Ответы (${data.answers.length})`)} />
      {data.answers.length ? data.answers.map(a => <AnswerCard key={a.id} a={a} q={q} reload={reload} />)
        : <p className="small muted">{tt('Әзірге жауап жоқ. Бірінші болып көмектесіңіз.', 'Пока нет ответов. Помогите первым.')}</p>}
      {!q.locked && q.status === 'active' ? <AnswerForm q={q} done={reload} /> : null}
      {!q.mine ? <ReportLink kind="qa_question" id={q.id} /> : null}
      <HonestyRule />
    </>
  );
}
