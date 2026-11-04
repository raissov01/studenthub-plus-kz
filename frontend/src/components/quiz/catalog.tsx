'use client';
import { useEffect, useState } from 'react';
import Link from 'next/link';
import { useRouter } from 'next/navigation';
import { useApp } from '@/components/app';
import { Qr } from '@/components/ui';
import { useAccount } from '@/lib/account';
import { quizApi, type AttemptSummary, type QuizMeta } from '@/lib/quiz-api';
import { AccountGate, ErrorText, Field, Loading, Modal, useTask } from './common';
export function QuizCatalog({ initialTab = 'discover' }: { initialTab?: string }) {
  const { tt, lang } = useApp();
  const acc = useAccount();
  const [tab, setTab] = useState(initialTab),
    [query, setQuery] = useState(''),
    [search, setSearch] = useState(''),
    [page, setPage] = useState(0);
  const [quizzes, setQuizzes] = useState<QuizMeta[]>([]),
    [history, setHistory] = useState<AttemptSummary[]>([]),
    [busy, setBusy] = useState(true),
    [error, setError] = useState(''),
    [more, setMore] = useState(false);
  useEffect(() => {
    let active = true;
    if (tab !== 'discover' && !acc.user) return;
    const p =
      tab === 'history'
        ? quizApi.history(lang).then((r) => {
            if (active) setHistory(r.attempts);
          })
        : (tab === 'mine' ? quizApi.mine(lang) : quizApi.browse(lang, search, page)).then((r) => {
            if (active) {
              setQuizzes(r.quizzes);
              setMore('hasMore' in r && r.hasMore === true);
            }
          });
    p.then(
      () => {
        if (active) setBusy(false);
      },
      (e) => {
        if (active) {
          setError(e.message);
          setBusy(false);
        }
      },
    );
    return () => {
      active = false;
    };
  }, [tab, search, page, lang, acc.user]);
  const body = (
    <>
      <ErrorText error={error} />
      {busy ? (
        <Loading />
      ) : tab === 'history' ? (
        <>
          <h2>{tt('Соңғы нәтижелер', 'Последние результаты')}</h2>
          {history.length ? (
            history.map((a) => (
              <Link className="card quiz-stack" key={a.id} href={`/quiz/${a.quizId}/play?attempt=${a.id}`}>
                <b>{a.quizTitle || tt('Квиз', 'Квиз')}</b>
                <span>
                  {a.percentage}% ·{' '}
                  {new Date(a.createdAt).toLocaleDateString(lang === 'kk' ? 'kk-KZ' : 'ru-RU')}
                </span>
              </Link>
            ))
          ) : (
            <p>{tt('Әзірге нәтижелер жоқ.', 'Пока нет результатов.')}</p>
          )}
        </>
      ) : (
        <>
          <div className="quiz-grid">
            {quizzes.map((q) => (
              <Link
                className="card quiz-stack"
                key={q.id}
                href={q.isDraft ? `/quiz/${q.id}/edit` : `/quiz/${q.id}`}
              >
                <span className="eyebrow">
                  {q.subject || tt('Квиз', 'Квиз')}
                  {q.isDraft ? ` · ${tt('Жоба', 'Черновик')}` : ''}
                </span>
                <h2>{q.title}</h2>
                <p>{q.description}</p>
                <span className="note">
                  {tt(
                    `${q.questionCount} сұрақ · ${q.plays} өту`,
                    `${q.questionCount} вопросов · ${q.plays} прохождений`,
                  )}
                </span>
              </Link>
            ))}
          </div>
          {!quizzes.length && (
            <p>{tt('Квиз табылмады. Жаңасын жасаңыз.', 'Квизы не найдены. Создайте новый.')}</p>
          )}
          {tab === 'discover' && (
            <div className="quiz-actions">
              {page > 0 && (
                <button
                  className="btn ghost"
                  onClick={() => {
                    setBusy(true);
                    setPage(page - 1);
                  }}
                >
                  {tt('Алдыңғы бет', 'Предыдущая страница')}
                </button>
              )}
              {more && (
                <button
                  className="btn ghost"
                  onClick={() => {
                    setBusy(true);
                    setPage(page + 1);
                  }}
                >
                  {tt('Келесі бет', 'Следующая страница')}
                </button>
              )}
            </div>
          )}
        </>
      )}
    </>
  );
  return (
    <>
      <header className="quiz-stack">
        <h1>{tt('Біліміңді тексер', 'Проверь свои знания')}</h1>
        <p className="muted">
          {tt(
            'Өз қарқыныңмен жаттық немесе достарыңмен бірге ойна.',
            'Тренируйся в своём темпе или играй вместе с друзьями.',
          )}
        </p>
        <div className="quiz-actions">
          <Link className="btn" href="/quiz/new">
            {tt('Квиз жасау', 'Создать квиз')}
          </Link>
          <Link className="btn ghost" href="/quiz/join">
            {tt('Кодпен қосылу', 'Войти по коду')}
          </Link>
        </div>
      </header>
      <div className="quiz-actions">
        {[
          ['discover', 'Шолу', 'Обзор'],
          ['mine', 'Менің квиздерім', 'Мои квизы'],
          ['history', 'Тарих', 'История'],
        ].map(([v, kk, ru]) => (
          <button
            className="chip"
            aria-pressed={tab === v}
            key={v}
            onClick={() => {
              if (v === tab) return;
              setBusy(true);
              setError('');
              setTab(v);
              setPage(0);
            }}
          >
            {tt(kk, ru)}
          </button>
        ))}
      </div>
      {tab === 'discover' && (
        <form
          className="quiz-inline"
          onSubmit={(e) => {
            e.preventDefault();
            setBusy(true);
            setError('');
            setSearch(query);
            setPage(0);
            if (query === search && page === 0) setBusy(false);
          }}
        >
          <Field label={tt('Квиз іздеу', 'Поиск квиза')} value={query} maxLength={100} onChange={setQuery} />
          <button className="btn ghost">{tt('Іздеу', 'Найти')}</button>
        </form>
      )}
      {tab !== 'discover' ? <AccountGate>{body}</AccountGate> : body}
    </>
  );
}
export function ShareQuiz({ quiz, close }: { quiz: QuizMeta; close: () => void }) {
  const { tt } = useApp();
  const [copied, setCopied] = useState(false);
  const task = useTask();
  const path = quiz.shareCode ? `/quiz/s/${quiz.shareCode}` : `/quiz/${quiz.id}`;
  const url = typeof window !== 'undefined' ? new URL(path, window.location.origin).href : path;
  return (
    <Modal title={tt('Квизбен бөлісу', 'Поделиться квизом')} close={close}>
      {quiz.visibility === 'private' || quiz.isDraft ? (
        <>
          <p>
            {tt(
              'Квиз тек сізге көрінеді. Бөлісу үшін қолжетімділікті өзгертіп, сақтаңыз.',
              'Квиз виден только вам. Для публикации измените доступ и сохраните.',
            )}
          </p>
          <Link className="btn" href={`/quiz/${quiz.id}/edit`} onClick={close}>
            {tt('Қолжетімділікті өзгерту', 'Изменить доступ')}
          </Link>
        </>
      ) : (
        <>
          <Qr text={url} />
          <label className="field">
            {tt('Сілтеме', 'Ссылка')}
            <input className="input" readOnly value={url} onFocus={(e) => e.target.select()} />
          </label>
          <button
            className="btn"
            onClick={() =>
              void task.run(async () => {
                try {
                  await navigator.clipboard.writeText(url);
                  setCopied(true);
                } catch {
                  throw new Error(
                    tt('Сілтемені белгілеп, қолмен көшіріңіз.', 'Выделите ссылку и скопируйте вручную.'),
                  );
                }
              })
            }
          >
            {copied ? tt('Көшірілді', 'Скопировано') : tt('Сілтемені көшіру', 'Копировать ссылку')}
          </button>
          <a
            className="btn ghost"
            href={`https://t.me/share/url?url=${encodeURIComponent(url)}&text=${encodeURIComponent(quiz.title)}`}
            target="_blank"
            rel="noopener noreferrer"
          >
            Telegram
          </a>
          <ErrorText error={task.error} />
        </>
      )}
    </Modal>
  );
}
export function QuizDetail({ id, share = '' }: { id: string; share?: string }) {
  const { tt, lang } = useApp();
  const [quiz, setQuiz] = useState<QuizMeta | null>(null),
    [error, setError] = useState(''),
    [sharing, setSharing] = useState(false);
  const task = useTask();
  const router = useRouter();
  const acc = useAccount();
  useEffect(() => {
    let active = true;
    quizApi.play(id, lang, share).then(
      (r) => {
        if (active) setQuiz(r.quiz);
      },
      (e) => {
        if (active) setError(e.message);
      },
    );
    return () => {
      active = false;
    };
  }, [id, lang, share]);
  if (error) return <ErrorText error={error} />;
  if (!quiz) return <Loading />;
  return (
    <>
      <header className="quiz-stack">
        <p className="eyebrow">{quiz.subject}</p>
        <h1>{quiz.title}</h1>
        <p>{quiz.description}</p>
        <p>
          {tt(
            `${quiz.questionCount} сұрақ · әр сұраққа ${quiz.timePerQuestion} с`,
            `${quiz.questionCount} вопросов · ${quiz.timePerQuestion} с на вопрос`,
          )}
        </p>
      </header>
      <div className="quiz-actions">
        <Link
          className="btn"
          href={`/quiz/${quiz.id}/play${share ? '?share=' + encodeURIComponent(share) : ''}`}
        >
          {tt('Жаттығуды бастау', 'Начать тренировку')}
        </Link>
        <button className="btn ghost" onClick={() => setSharing(true)}>
          {tt('Бөлісу', 'Поделиться')}
        </button>
        {quiz.isMine && (
          <>
            <Link className="btn ghost" href={`/quiz/${quiz.id}/edit`}>
              {tt('Өңдеу', 'Редактировать')}
            </Link>
            <Link className="btn ghost" href={`/quiz/${quiz.id}/host`}>
              {tt('Тікелей ойын өткізу', 'Провести игру')}
            </Link>
          </>
        )}
        {acc.user && !acc.needsConsent && (
          <button
            className="btn ghost"
            disabled={task.busy}
            onClick={() =>
              void task.run(async () => {
                const r = await quizApi.clone(quiz.id, lang, share);
                router.push(`/quiz/${r.id}/edit`);
              })
            }
          >
            {tt('Көшірме жасау', 'Создать копию')}
          </button>
        )}
        <Link className="btn ghost" href="/quiz?tab=history">
          {tt('Нәтижелер тарихы', 'История результатов')}
        </Link>
      </div>
      <ErrorText error={task.error} />
      {sharing && <ShareQuiz quiz={quiz} close={() => setSharing(false)} />}
    </>
  );
}
