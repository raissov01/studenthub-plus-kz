'use client';
import { useEffect, useRef, useState } from 'react';
import Link from 'next/link';
import { useRouter } from 'next/navigation';
import { useApp } from '@/components/app';
import { Qr } from '@/components/ui';
import {
  quizApi,
  readLiveIdentity,
  saveLiveIdentity,
  type Answer,
  type Leader,
  type LiveIdentity,
  type LiveMessage,
  type LivePlayer,
  type PlayQuestion,
} from '@/lib/quiz-api';
import { connectQuizLive, type ConnectionState } from '@/lib/quiz-live';
import { AnswerControl, answerText, QuestionMedia, Timer } from './answer';
import { ErrorText, Field, Loading, useTask } from './common';
export function QuizJoin({ initialCode = '' }: { initialCode?: string }) {
  const { tt, lang } = useApp();
  const router = useRouter();
  const task = useTask();
  const [code, setCode] = useState(initialCode),
    [nickname, setNickname] = useState('');
  return (
    <>
      <h1>{tt('Ойынға қосылу', 'Присоединиться к игре')}</h1>
      <p>
        {tt('Жүргізуші көрсеткен 6 таңбалы кодты енгізіңіз.', 'Введите шестизначный код с экрана ведущего.')}
      </p>
      <form
        className="card quiz-stack"
        onSubmit={(e) => {
          e.preventDefault();
          void task.run(async () => {
            const t = await quizApi.join(code, nickname.trim(), lang);
            saveLiveIdentity({ role: 'player', code, nickname: t.nickname, rejoinKey: t.rejoinKey });
            router.push(`/quiz/live/${code}`);
          });
        }}
      >
        <label className="field">
          {tt('Ойын коды', 'Код игры')}
          <input
            className="input quiz-code-input"
            inputMode="numeric"
            autoComplete="off"
            pattern="[0-9]{6}"
            maxLength={6}
            required
            value={code}
            onChange={(e) => setCode(e.target.value.replace(/\D/g, ''))}
          />
        </label>
        <Field
          label={tt('Лақап ат', 'Никнейм')}
          value={nickname}
          required
          maxLength={24}
          onChange={setNickname}
        />
        <button className="btn" disabled={task.busy || !/^\d{6}$/.test(code) || nickname.trim().length < 2}>
          {tt('Қосылу', 'Присоединиться')}
        </button>
        <ErrorText error={task.error} />
      </form>
    </>
  );
}
export function QuizHost({ id }: { id: string }) {
  const { tt, lang } = useApp();
  const task = useTask();
  const [identity, setIdentity] = useState<LiveIdentity | null>(null),
    [title, setTitle] = useState('');
  useEffect(() => {
    const code = sessionStorage.getItem(`studenthub.quiz.host.${id}`);
    const i = code ? readLiveIdentity('host', code) : null;
    if (i) queueMicrotask(() => setIdentity(i));
  }, [id]);
  if (identity) return <LiveBoard identity={identity} />;
  return (
    <>
      <h1>{tt('Тікелей ойын', 'Игра в прямом эфире')}</h1>
      <p>
        {tt(
          'Экранды проекторға шығарыңыз. Қатысушылар телефоннан кодпен қосылады.',
          'Выведите экран на проектор. Участники присоединятся с телефонов по коду.',
        )}
      </p>
      <button
        className="btn"
        disabled={task.busy}
        onClick={() =>
          void task.run(async () => {
            const t = await quizApi.host(Number(id), lang);
            const i: LiveIdentity = { role: 'host', code: t.code };
            saveLiveIdentity(i);
            sessionStorage.setItem(`studenthub.quiz.host.${id}`, t.code);
            setTitle(t.title);
            setIdentity(i);
          })
        }
      >
        {tt('Ойын бөлмесін ашу', 'Открыть игровую комнату')}
      </button>
      {title && <p>{title}</p>}
      <ErrorText error={task.error} />
    </>
  );
}
export function QuizPlayer({ code }: { code: string }) {
  const [identity, setIdentity] = useState<LiveIdentity | null | undefined>(undefined);
  const { tt } = useApp();
  useEffect(() => {
    queueMicrotask(() => setIdentity(readLiveIdentity('player', code)));
  }, [code]);
  if (identity === undefined) return <Loading />;
  if (!identity)
    return (
      <>
        <h1>{tt('Ойынға қосылыңыз', 'Присоединитесь к игре')}</h1>
        <Link className="btn" href={`/quiz/join?code=${encodeURIComponent(code)}`}>
          {tt('Лақап атты енгізу', 'Ввести никнейм')}
        </Link>
      </>
    );
  return <LiveBoard identity={identity} />;
}
function LiveBoard({ identity }: { identity: LiveIdentity }) {
  const { tt, lang } = useApp();
  const [initialLang] = useState(lang);
  const host = identity.role === 'host';
  const [connection, setConnection] = useState<ConnectionState>('connecting'),
    [phase, setPhase] = useState('lobby'),
    [title, setTitle] = useState('');
  const [players, setPlayers] = useState<LivePlayer[]>([]),
    [leaders, setLeaders] = useState<Leader[]>([]),
    [question, setQuestion] = useState<PlayQuestion | null>(null);
  const [index, setIndex] = useState(0),
    [total, setTotal] = useState(0),
    [deadline, setDeadline] = useState(0),
    [answer, setAnswer] = useState<Answer>(null),
    [acked, setAcked] = useState(false),
    [pending, setPending] = useState(false);
  const [reveal, setReveal] = useState<Extract<LiveMessage, { type: 'reveal' }>['data'] | null>(null),
    [errorCode, setErrorCode] = useState(''),
    [progress, setProgress] = useState(0),
    [url, setUrl] = useState('');
  const socket = useRef<ReturnType<typeof connectQuizLive> | null>(null);
  useEffect(() => {
    queueMicrotask(() => setUrl(new URL(`/quiz/join?code=${identity.code}`, window.location.origin).href));
    const receive = (m: LiveMessage) => {
      switch (m.type) {
        case 'hello':
          setTitle(m.data.title);
          setTotal(m.data.total);
          setPhase(m.data.status);
          setAcked(!!m.data.you?.answered);
          setPending(false);
          break;
        case 'lobby':
          setPlayers(m.data.players);
          break;
        case 'question':
          setQuestion(m.data.question);
          setIndex(m.data.index);
          setTotal(m.data.total);
          setDeadline(Date.now() + m.data.deadline - m.data.serverTime);
          setPhase('question');
          setReveal(null);
          setAnswer(null);
          setPending(false);
          setProgress(0);
          setErrorCode('');
          break;
        case 'progress':
          setProgress(m.data.answered);
          break;
        case 'answer_ack':
          setAcked(true);
          setPending(false);
          break;
        case 'answer_rejected':
          setErrorCode(m.data.reason);
          setPending(false);
          if (m.data.reason === 'duplicate') setAcked(true);
          break;
        case 'reveal':
          setReveal(m.data);
          setPhase('reveal');
          setLeaders(m.data.leaderboard);
          setPending(false);
          setAcked(false);
          break;
        case 'ended':
          setPhase('ended');
          setLeaders(m.data.leaderboard);
          setPending(false);
          break;
        case 'kicked':
          setPhase('ended');
          break;
        case 'error':
          setErrorCode(m.data.code);
          setPending(false);
          break;
      }
    };
    socket.current = connectQuizLive(
      async () =>
        host
          ? (await quizApi.hostToken(identity.code, initialLang)).hostToken
          : (await quizApi.join(identity.code, identity.nickname!, initialLang, identity.rejoinKey)).token,
      receive,
      setConnection,
    );
    return () => {
      socket.current?.close();
      socket.current = null;
    };
  }, [identity, host, initialLang]);
  const ended = phase === 'ended' || phase === 'finished' || connection === 'ended';
  const send = (type: 'start' | 'next' | 'end') => {
    if (!socket.current?.send({ type })) setErrorCode('offline');
  };
  const messages: Record<string, [string, string]> = {
    late: ['Уақыт бітті.', 'Время истекло.'],
    bad_answer: ['Жауапты толықтырыңыз.', 'Заполните ответ полностью.'],
    duplicate: ['Жауап қабылданған.', 'Ответ уже принят.'],
    no_players: ['Қатысушыларды күтіңіз.', 'Дождитесь участников.'],
    offline: ['Қайта қосылуды күтіңіз.', 'Дождитесь переподключения.'],
    not_active: ['Келесі сұрақты күтіңіз.', 'Дождитесь следующего вопроса.'],
  };
  return (
    <div className={`quiz-stack ${host ? 'quiz-projector' : ''}`}>
      <header className="quiz-stack">
        <p className="eyebrow">{host ? tt('Жүргізуші', 'Ведущий') : identity.nickname}</p>
        <h1>{title || tt('Тікелей ойын', 'Игра в прямом эфире')}</h1>
        <p role="status">
          {ended
            ? tt('Ойын аяқталды', 'Игра завершена')
            : connection === 'connected'
              ? tt('Байланыс орнатылды', 'Соединение установлено')
              : connection === 'reconnecting'
                ? tt(
                    'Қайта қосылуда… Жауабыңыз серверде сақталады.',
                    'Переподключение… Ваш ответ сохранён на сервере.',
                  )
                : tt('Қосылуда…', 'Подключение…')}
        </p>
      </header>
      {!ended && phase === 'lobby' && (
        <>
          {host ? (
            <div className="card quiz-lobby">
              <div>
                <p>{tt('Ойын коды', 'Код игры')}</p>
                <strong className="quiz-live-code">{identity.code}</strong>
                <p>
                  <a href={url}>{url}</a>
                </p>
              </div>
              {url && <Qr text={url} big />}
            </div>
          ) : (
            <p>{tt('Жүргізуші ойынды бастағанша күтіңіз.', 'Дождитесь, пока ведущий начнёт игру.')}</p>
          )}
          <h2>
            {tt('Қатысушылар', 'Участники')} · {players.length}
          </h2>
          <ul className="quiz-players">
            {players.map((p) => (
              <li key={p.id}>
                {p.nickname}
                {!p.online && <span> · {tt('байланыс жоқ', 'нет связи')}</span>}
              </li>
            ))}
          </ul>
          {host && (
            <button
              className="btn"
              disabled={!players.length || connection !== 'connected'}
              onClick={() => send('start')}
            >
              {tt('Бастау', 'Начать')}
            </button>
          )}
        </>
      )}
      {!ended && question && (phase === 'question' || phase === 'reveal') && (
        <>
          <p>
            {tt('Сұрақ', 'Вопрос')} {index + 1} / {total}
          </p>
          <h2>{question.prompt}</h2>
          {phase === 'question' && <Timer key={`${index}-${deadline}`} deadline={deadline} />}
          {!['hotspot', 'labeling'].includes(question.type) && <QuestionMedia q={question} />}
          {host ? (
            <>
              {question.passage && <p>{question.passage}</p>}
              {question.options && (
                <ol className="quiz-host-options">
                  {question.options.map((o, i) => (
                    <li key={i}>{o}</li>
                  ))}
                </ol>
              )}
              <p>
                {tt('Жауап берді', 'Ответили')}: {progress} / {players.length}
              </p>
            </>
          ) : (
            phase === 'question' && (
              <>
                <AnswerControl
                  q={question}
                  value={answer}
                  change={setAnswer}
                  disabled={acked || pending || connection !== 'connected'}
                />
                {acked ? (
                  <p role="status">
                    {tt('Жауап қабылданды. Нәтижені күтіңіз.', 'Ответ принят. Дождитесь результата.')}
                  </p>
                ) : (
                  <button
                    className="btn"
                    disabled={answer === null || pending || connection !== 'connected'}
                    onClick={() => {
                      if (socket.current?.send({ type: 'answer', index, answer })) setPending(true);
                      else setErrorCode('offline');
                    }}
                  >
                    {pending ? tt('Жіберілуде…', 'Отправка…') : tt('Жауап беру', 'Ответить')}
                  </button>
                )}
              </>
            )
          )}
          {reveal && (
            <div className="card quiz-stack">
              <p>
                <b>{tt('Дұрыс жауап:', 'Верный ответ:')}</b> {answerText(reveal.correct, question, tt)}
              </p>
              <p>{reveal.explanation}</p>
              {reveal.you && (
                <p>
                  {tt(
                    `Ұпай: ${reveal.you.score} · орын: ${reveal.you.rank}`,
                    `Баллы: ${reveal.you.score} · место: ${reveal.you.rank}`,
                  )}
                </p>
              )}
            </div>
          )}
          {host && (
            <button className="btn" disabled={connection !== 'connected'} onClick={() => send('next')}>
              {phase === 'question'
                ? tt('Жауаптарды көрсету', 'Показать ответы')
                : reveal?.last
                  ? tt('Қорытынды', 'Итоги')
                  : tt('Келесі сұрақ', 'Следующий вопрос')}
            </button>
          )}
        </>
      )}
      {(phase === 'reveal' || ended) && (
        <section className="quiz-stack">
          <h2>{ended ? tt('Қорытынды орындар', 'Итоговый пьедестал') : tt('Көшбасшылар', 'Лидеры')}</h2>
          <ol className={ended ? 'quiz-podium' : 'quiz-ranking'}>
            {leaders.map((p) => (
              <li key={p.id}>
                <b>
                  {p.rank}. {p.nickname}
                </b>
                <span>
                  {p.score} {tt('ұпай', 'баллов')}
                </span>
              </li>
            ))}
          </ol>
        </section>
      )}
      <ErrorText
        error={
          errorCode
            ? tt(
                ...(messages[errorCode] || [
                  'Әрекет орындалмады. Қайталап көріңіз.',
                  'Действие не выполнено. Попробуйте снова.',
                ]),
              )
            : ''
        }
      />
      {host && !ended && (
        <button className="btn ghost" disabled={connection !== 'connected'} onClick={() => send('end')}>
          {tt('Ойынды аяқтау', 'Завершить игру')}
        </button>
      )}
      {ended && (
        <Link className="btn" href="/quiz">
          {tt('Квиздерге оралу', 'К квизам')}
        </Link>
      )}
    </div>
  );
}
