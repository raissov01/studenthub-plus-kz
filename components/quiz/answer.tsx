'use client';
import { useEffect, useId, useState } from 'react';
import { useApp } from '@/components/app';
import type { Answer, PlayQuestion, ReviewedAnswer } from '@/lib/quiz-api';
import { Field, Select, QuizMedia } from './common';
export function Timer({ deadline, expire }: { deadline: number; expire?: () => void }) {
  const { tt } = useApp();
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const tick = () => {
      const n = Date.now();
      setNow(n);
      if (n >= deadline) expire?.();
    };
    const t = setInterval(tick, 250);
    return () => clearInterval(t);
  }, [deadline, expire]);
  return (
    <p className="quiz-timer" role="timer">
      {tt('Қалған уақыт', 'Осталось времени')}: <b>{Math.max(0, Math.ceil((deadline - now) / 1000))}</b>{' '}
      {tt('с', 'с')}
    </p>
  );
}
export function QuestionMedia({ q }: { q: Pick<PlayQuestion, 'imageUrl' | 'audioUrl' | 'prompt'> }) {
  return (
    <>
      {q.imageUrl && (
        <div className="quiz-image">
          <QuizMedia src={q.imageUrl!} alt={q.prompt} />
        </div>
      )}
      {q.audioUrl && <QuizMedia src={q.audioUrl} alt={q.prompt} audio />}
    </>
  );
}
export function AnswerControl({
  q,
  value,
  change,
  disabled = false,
}: {
  q: PlayQuestion;
  value: Answer;
  change: (v: Answer) => void;
  disabled?: boolean;
}) {
  const { tt } = useApp();
  const id = useId();
  const map = (value && typeof value === 'object' && !Array.isArray(value) ? value : {}) as Record<
    string,
    Answer
  >;
  const choose = tt('Таңдаңыз', 'Выберите');
  const mapping = (keys: string[], choices: string[]) =>
    keys.map((k) => (
      <Select
        key={k}
        label={k}
        value={typeof map[k] === 'string' ? (map[k] as string) : ''}
        onChange={(v) => change({ ...map, [k]: v })}
      >
        <option value="">{choose}</option>
        {choices.map((s) => (
          <option key={s}>{s}</option>
        ))}
      </Select>
    ));
  return (
    <fieldset className="quiz-answers" disabled={disabled}>
      <legend className="sr-only">{tt('Сіздің жауабыңыз', 'Ваш ответ')}</legend>
      {['mcq', 'mcq_multi', 'poll'].includes(q.type) &&
        q.options?.map((o, i) => (
          <label className="quiz-choice" key={i}>
            <input
              type={q.type === 'mcq_multi' ? 'checkbox' : 'radio'}
              name={id}
              checked={
                q.type === 'mcq_multi' ? Array.isArray(value) && (value as number[]).includes(i) : value === i
              }
              onChange={() => {
                if (q.type === 'mcq_multi') {
                  const a = (Array.isArray(value) ? value : []) as number[];
                  change(a.includes(i) ? a.filter((x) => x !== i) : [...a, i]);
                } else change(i);
              }}
            />
            <span>{o}</span>
          </label>
        ))}
      {q.type === 'dropdown' && (
        <Select
          label={tt('Жауап', 'Ответ')}
          value={typeof value === 'number' ? value : ''}
          onChange={(v) => change(v === '' ? null : Number(v))}
        >
          <option value="">{choose}</option>
          {q.options?.map((o, i) => (
            <option value={i} key={i}>
              {o}
            </option>
          ))}
        </Select>
      )}
      {q.type === 'true_false' &&
        [true, false].map((b) => (
          <label key={String(b)} className="quiz-choice">
            <input type="radio" name={id} checked={value === b} onChange={() => change(b)} />
            <span>{b ? tt('Дұрыс', 'Верно') : tt('Бұрыс', 'Неверно')}</span>
          </label>
        ))}
      {q.type === 'fill_blank' && (
        <Field
          label={tt('Жауабыңыз', 'Ваш ответ')}
          value={typeof value === 'string' ? value : ''}
          maxLength={200}
          onChange={change}
        />
      )}
      {q.type === 'reorder' && (
        <>
          <p className="note">
            {tt(
              'Элементтерді жоғары / төмен батырмаларымен реттеңіз.',
              'Расположите элементы кнопками выше / ниже.',
            )}
          </p>
          <ol className="quiz-order">
            {(((Array.isArray(value) ? value : q.items) as string[]) || []).map((s, i, all) => (
              <li key={s}>
                <span>{s}</span>
                <div className="quiz-actions">
                  {[-1, 1].map((d) => (
                    <button
                      className="btn ghost"
                      key={d}
                      type="button"
                      disabled={disabled || i + d < 0 || i + d >= all.length}
                      aria-label={`${s}: ${d < 0 ? tt('Жоғары', 'Выше') : tt('Төмен', 'Ниже')}`}
                      onClick={() => {
                        const a = [...all];
                        [a[i], a[i + d]] = [a[i + d], a[i]];
                        change(a);
                      }}
                    >
                      {d < 0 ? '↑' : '↓'}
                    </button>
                  ))}
                </div>
              </li>
            ))}
          </ol>
          <button
            type="button"
            className="btn ghost"
            onClick={() => change((Array.isArray(value) ? value : q.items || []) as string[])}
          >
            {tt('Ретті растау', 'Подтвердить порядок')}
          </button>
        </>
      )}
      {q.type === 'matching' && mapping(q.lefts || [], q.rights || [])}
      {q.type === 'categorization' && mapping(q.items || [], q.categories || [])}
      {(q.type === 'hotspot' || q.type === 'labeling') && (
        <>
          <div className="quiz-image">
            <QuizMedia src={q.imageUrl!} alt={q.prompt} />
            {q.zones?.map((z, i) =>
              q.type === 'hotspot' ? (
                <button
                  type="button"
                  key={z.id}
                  className="quiz-zone"
                  style={{ left: `${z.x}%`, top: `${z.y}%`, width: `${z.w}%`, height: `${z.h}%` }}
                  aria-pressed={value === z.id}
                  aria-label={`${tt('Аймақ', 'Область')} ${i + 1}`}
                  onClick={() => change(z.id)}
                >
                  {i + 1}
                </button>
              ) : (
                <span
                  className="quiz-zone"
                  key={z.id}
                  style={{ left: `${z.x}%`, top: `${z.y}%`, width: `${z.w}%`, height: `${z.h}%` }}
                >
                  {i + 1}
                </span>
              ),
            )}
          </div>
          {q.type === 'hotspot' ? (
            <Select
              label={tt('Аймақ нөмірі', 'Номер области')}
              value={typeof value === 'string' ? value : ''}
              onChange={change}
            >
              <option value="">{choose}</option>
              {q.zones?.map((z, i) => (
                <option key={z.id} value={z.id}>
                  {i + 1}
                </option>
              ))}
            </Select>
          ) : (
            q.zones?.map((z, i) => (
              <Select
                key={z.id}
                label={`${tt('Аймақ', 'Область')} ${i + 1}`}
                value={String(map[z.id] || '')}
                onChange={(v) => change({ ...map, [z.id]: v })}
              >
                <option value="">{choose}</option>
                {q.labels?.map((l) => (
                  <option key={l}>{l}</option>
                ))}
              </Select>
            ))
          )}
        </>
      )}
      {q.type === 'comprehension' && (
        <>
          <p className="quiz-passage">{q.passage}</p>
          {q.subQuestions?.map((s, i) => (
            <section className="quiz-stack" key={s.id}>
              <h3>{s.prompt}</h3>
              <AnswerControl
                q={{ ...s, id: i, position: i, timeLimit: 0 }}
                value={map[s.id] ?? null}
                change={(v) => change({ ...map, [s.id]: v })}
                disabled={disabled}
              />
            </section>
          ))}
        </>
      )}
    </fieldset>
  );
}
export function answerText(
  value: Answer,
  q: Partial<PlayQuestion>,
  tt: (a: string, b: string) => string,
): string {
  if (value === null || value === undefined || value === '') return tt('Жауап жоқ', 'Нет ответа');
  if (typeof value === 'boolean') return value ? tt('Дұрыс', 'Верно') : tt('Бұрыс', 'Неверно');
  if (typeof value === 'number') return q.options?.[value] || String(value + 1);
  if (Array.isArray(value))
    return value.map((v) => (typeof v === 'number' ? q.options?.[v] || String(v + 1) : v)).join(' · ');
  if (typeof value === 'object')
    return Object.entries(value)
      .map(([k, v]) => {
        const sub = q.subQuestions?.find((s) => s.id === k);
        return `${sub?.prompt || k}: ${answerText(v, sub ? { ...sub, id: undefined } : {}, tt)}`;
      })
      .join('; ');
  if (q.type === 'hotspot')
    return `${tt('Аймақ', 'Область')} ${(q.zones?.findIndex((z) => z.id === value) ?? -1) + 1}`;
  return value;
}
export function ReviewAnswer({ a }: { a: ReviewedAnswer }) {
  const { tt } = useApp();
  return (
    <article className="card quiz-stack">
      <h3>
        {a.position}. {a.prompt}
      </h3>
      <p className="quiz-result-label">
        {!a.graded
          ? tt('Сауалнама · бағаланбайды', 'Опрос · без оценки')
          : a.isCorrect
            ? tt('Дұрыс жауап', 'Верный ответ')
            : tt('Қате жауап', 'Неверный ответ')}
      </p>
      <QuestionMedia q={{ imageUrl: a.imageUrl, prompt: a.prompt }} />
      <p>
        {tt('Сіздің жауабыңыз:', 'Ваш ответ:')} {answerText(a.given, { ...a.view, type: a.type }, tt)}
      </p>
      {a.graded && (
        <p>
          <b>{tt('Дұрыс жауап:', 'Верный ответ:')}</b>{' '}
          {answerText(a.correct, { ...a.view, type: a.type }, tt)}
        </p>
      )}
      {a.explanation && <p>{a.explanation}</p>}
      {a.sourceQuote && (
        <blockquote className="quiz-quote">
          {tt('Жауап конспектіде мына жерде:', 'Ответ в конспекте находится здесь:')} {a.sourceQuote}
        </blockquote>
      )}
    </article>
  );
}
