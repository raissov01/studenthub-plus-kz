'use client';
import { useCallback, useEffect, useRef, useState } from 'react';
import Link from 'next/link';
import { useApp } from '@/components/app';
import { useAccount } from '@/lib/account';
import { quizApi, type Answer, type AnswerInput, type QuizPlay, type QuizResult } from '@/lib/quiz-api';
import { AnswerControl, QuestionMedia, ReviewAnswer, Timer } from './answer';
import { ErrorText, Loading, useTask } from './common';
export function QuizPlayScreen({
  id,
  share = '',
  attempt = '',
}: {
  id: string;
  share?: string;
  attempt?: string;
}) {
  const { tt, lang } = useApp();
  const account = useAccount();
  const [initialLang] = useState(lang);
  const [data, setData] = useState<QuizPlay | null>(null),
    [result, setResult] = useState<QuizResult | null>(null),
    [loadError, setLoadError] = useState('');
  const [index, setIndex] = useState(0),
    [answers, setAnswers] = useState<AnswerInput[]>([]),
    [answer, setAnswer] = useState<Answer>(null),
    [deadline, setDeadline] = useState(0),
    [expired, setExpired] = useState(false);
  const start = useRef(0),
    heading = useRef<HTMLHeadingElement>(null);
  const task = useTask();
  useEffect(() => {
    let active = true;
    const p = attempt
      ? quizApi.result(attempt, initialLang).then((r) => {
          if (active) setResult(r);
        })
      : quizApi.play(id, initialLang, share).then((r) => {
          if (active) {
            setData(r);
            start.current = Date.now();
            setDeadline(Date.now() + (r.questions[0]?.timeLimit || r.quiz.timePerQuestion) * 1000);
          }
        });
    p.catch((e) => {
      if (active) setLoadError(e.message);
    });
    return () => {
      active = false;
    };
  }, [id, share, attempt, initialLang]);
  const expire = useCallback(() => setExpired(true), []);
  const retry = () =>
    void task.run(async () => {
      if (!result) return;
      let r: QuizPlay;
      if (result.attemptId) r = await quizApi.retry(result.attemptId, lang);
      else {
        r = await quizApi.play(id, lang, share);
        const wrong = new Set(
          result.answers.filter((a) => a.graded && !a.isCorrect).map((a) => a.questionId),
        );
        r.questions = r.questions.filter((q) => wrong.has(q.id));
      }
      if (!r.questions.length) throw new Error(tt('Қайталайтын сұрақ жоқ.', 'Нет вопросов для повторения.'));
      setData(r);
      setIndex(0);
      setAnswers([]);
      setAnswer(null);
      setExpired(false);
      start.current = Date.now();
      setDeadline(Date.now() + (r.questions[0].timeLimit || r.quiz.timePerQuestion) * 1000);
      setResult(null);
    });
  if (loadError) return <ErrorText error={loadError} />;
  const resultShare = share || (data?.quiz.visibility === 'unlisted' ? data.quiz.shareCode : '');
  if (result)
    return (
      <>
        <header className="quiz-stack">
          <h1>{tt('Нәтиже', 'Результат')}</h1>
          <p>{result.quizTitle}</p>
          <p className="quiz-score">{result.percentage}%</p>
          <p>
            {tt(
              `Дұрыс: ${result.correctCount} / ${result.maxScore}`,
              `Верно: ${result.correctCount} / ${result.maxScore}`,
            )}
          </p>
        </header>
        <div className="quiz-actions">
          {result.wrongCount > 0 && (
            <button className="btn" disabled={task.busy} onClick={retry}>
              {tt('Қате жауаптарды қайталау', 'Повторить ошибки')}
            </button>
          )}
          <Link
            className="btn ghost"
            href={resultShare
              ? `/quiz/s/${encodeURIComponent(resultShare)}`
              : data ? `/quiz/${result.quizId}` : '/quiz'}
          >
            {data || share ? tt('Квизге оралу', 'К квизу') : tt('Квиздерге оралу', 'К квизам')}
          </Link>
          <Link className="btn ghost" href="/quiz?tab=history">
            {tt('Тарих', 'История')}
          </Link>
        </div>
        {!account.user && (
          <p className="note">
            {tt(
              'Қонақ нәтижесі сақталмайды. Қателерді осы бетте қайталауға болады.',
              'Результат гостя не сохраняется. Ошибки можно повторить на этой странице.',
            )}
          </p>
        )}
        <ErrorText error={task.error} />
        {result.answers.map((a, i) => (
          <ReviewAnswer key={i} a={a} />
        ))}
      </>
    );
  if (!data) return <Loading />;
  const q = data.questions[index];
  if (!q) return <p>{tt('Сұрақтар жоқ.', 'Нет вопросов.')}</p>;
  const next = () =>
    void task.run(async () => {
      const all = [
        ...answers,
        { questionId: q.id, answer, timeSpent: Math.round((Date.now() - start.current) / 1000) },
      ];
      if (index + 1 === data.questions.length) {
        setResult(
          await quizApi.submit(
            data.quiz.id,
            all,
            lang,
            data.questions.map((x) => x.id),
            data.retryOf,
            share || data.quiz.shareCode || '',
          ),
        );
        return;
      }
      setAnswers(all);
      setIndex(index + 1);
      setAnswer(null);
      setExpired(false);
      start.current = Date.now();
      setDeadline(Date.now() + (data.questions[index + 1].timeLimit || data.quiz.timePerQuestion) * 1000);
      heading.current?.focus();
    });
  return (
    <>
      <p className="eyebrow">{data.quiz.title}</p>
      <div className="quiz-inline">
        <p>
          {tt('Сұрақ', 'Вопрос')} {index + 1} / {data.questions.length}
        </p>
        {deadline > 0 && <Timer key={q.id} deadline={deadline} expire={expire} />}
      </div>
      <progress
        className="quiz-progress"
        value={index + 1}
        max={data.questions.length}
        aria-label={tt('Орындалуы', 'Прогресс')}
      />
      <h1 ref={heading} tabIndex={-1}>
        {q.prompt}
      </h1>
      {!['hotspot', 'labeling'].includes(q.type) && <QuestionMedia q={q} />}
      <AnswerControl
        key={q.id}
        q={q}
        value={answer}
        change={setAnswer}
        disabled={task.busy}
        shuffle={data.quiz.shuffle}
      />
      {expired && (
        <p role="status">
          {tt(
            'Уақыт бітті. Жаттығуда жауап беруді жалғастыра аласыз.',
            'Время вышло. В тренировке можно продолжить отвечать.',
          )}
        </p>
      )}
      <button type="button" className="btn" disabled={task.busy} onClick={next}>
        {index + 1 === data.questions.length ? tt('Аяқтау', 'Завершить') : tt('Келесі', 'Далее')}
      </button>
      {answer === null && (
        <p className="note">
          {tt(
            'Жауапсыз жалғастырсаңыз, сұрақ өткізіледі.',
            'Если продолжить без ответа, вопрос будет пропущен.',
          )}
        </p>
      )}
      <ErrorText error={task.error} />
    </>
  );
}
