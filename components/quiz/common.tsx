'use client';
import { useEffect, useId, useRef, useState, type ReactNode } from 'react';
import Link from 'next/link';
import { useApp } from '@/components/app';
import { Empty } from '@/components/ui';
import { useAccount } from '@/lib/account';
import { useConfig, useConfigReady } from '@/lib/config';
import { NotFound } from '@/components/views/common';
import { SignInSheet, ConsentSheet } from '@/components/views/account';
import { quizApi, type QuestionType } from '@/lib/quiz-api';
export const TYPE_NAMES: Record<QuestionType, [string, string]> = {
  mcq: ['Бір жауап', 'Один ответ'],
  mcq_multi: ['Бірнеше жауап', 'Несколько ответов'],
  true_false: ['Дұрыс / бұрыс', 'Верно / неверно'],
  fill_blank: ['Қысқа жауап', 'Короткий ответ'],
  reorder: ['Ретімен қою', 'Порядок'],
  matching: ['Сәйкестендіру', 'Соответствие'],
  categorization: ['Топтарға бөлу', 'Категории'],
  dropdown: ['Тізімнен таңдау', 'Выпадающий список'],
  poll: ['Сауалнама', 'Опрос'],
  hotspot: ['Суреттегі аймақ', 'Область на изображении'],
  labeling: ['Суретке атау қою', 'Подписи к изображению'],
  comprehension: ['Мәтінді түсіну', 'Понимание текста'],
};
export function QuizGate({ children, account = false }: { children: ReactNode; account?: boolean }) {
  const { tt, update, lang } = useApp();
  const config = useConfig(),
    ready = useConfigReady();
  if (!ready) return <Loading />;
  if (!config.features.quiz) return <NotFound />;
  return (
    <div className="quiz">
      <nav className="quiz-nav" aria-label={tt('Квиз навигациясы', 'Навигация квиза')}>
        <Link href="/quiz">{tt('Квиздер', 'Квизы')}</Link>
        <div className="quiz-actions">
          {(['kk', 'ru'] as const).map((l) => (
            <button
              type="button"
              className="chip"
              key={l}
              aria-pressed={lang === l}
              onClick={() =>
                update((s) => {
                  s.lang = l;
                })
              }
            >
              {l === 'kk' ? 'Қазақша' : 'Русский'}
            </button>
          ))}
        </div>
      </nav>
      {account ? <AccountGate>{children}</AccountGate> : children}
    </div>
  );
}
export function AccountGate({ children }: { children: ReactNode }) {
  const acc = useAccount();
  const { tt, openSheet } = useApp();
  if (!acc.loaded) return <Loading />;
  if (!acc.user || acc.needsConsent)
    return (
      <Empty
        action={
          <button className="btn" onClick={() => openSheet(acc.user ? <ConsentSheet /> : <SignInSheet />)}>
            {acc.user ? tt('Келісім беру', 'Дать согласие') : tt('Кіру', 'Войти')}
          </button>
        }
      >
        {tt(
          'Квиз жасау және тарихты сақтау үшін аккаунт керек.',
          'Для создания квизов и сохранения истории нужен аккаунт.',
        )}
      </Empty>
    );
  return children;
}
export function Loading() {
  const { tt } = useApp();
  return <p role="status">{tt('Жүктелуде…', 'Загрузка…')}</p>;
}
export function ErrorText({ error }: { error: string }) {
  return error ? (
    <p className="quiz-error" role="alert">
      {error}
    </p>
  ) : null;
}
export function useTask() {
  const [busy, setBusy] = useState(false),
    [error, setError] = useState('');
  const lock = useRef(false);
  const { tt } = useApp();
  const run = async (fn: () => Promise<void>) => {
    if (lock.current) return;
    lock.current = true;
    setBusy(true);
    setError('');
    try {
      await fn();
    } catch (e) {
      setError(e instanceof Error ? e.message : tt('Қате. Қайталап көріңіз.', 'Ошибка. Попробуйте ещё раз.'));
    } finally {
      lock.current = false;
      setBusy(false);
    }
  };
  return { busy, error, run, setError };
}
export function Field({
  label,
  value,
  onChange,
  multiline = false,
  required = false,
  maxLength,
  type = 'text',
  min,
  max,
}: {
  label: string;
  value: string | number;
  onChange: (s: string) => void;
  multiline?: boolean;
  required?: boolean;
  maxLength?: number;
  type?: string;
  min?: number;
  max?: number;
}) {
  const id = useId();
  const props = {
    id,
    className: 'input',
    value,
    required,
    maxLength,
    onChange: (e: React.ChangeEvent<HTMLInputElement | HTMLTextAreaElement>) => onChange(e.target.value),
  };
  return (
    <div className="field">
      <label htmlFor={id}>{label}</label>
      {multiline ? <textarea {...props} rows={3} /> : <input {...props} type={type} min={min} max={max} />}
    </div>
  );
}
export function Select({
  label,
  value,
  onChange,
  children,
}: {
  label: string;
  value: string | number;
  onChange: (v: string) => void;
  children: ReactNode;
}) {
  const id = useId();
  return (
    <div className="field">
      <label htmlFor={id}>{label}</label>
      <select id={id} className="input" value={value} onChange={(e) => onChange(e.target.value)}>
        {children}
      </select>
    </div>
  );
}
export function Modal({ title, children, close }: { title: string; children: ReactNode; close: () => void }) {
  const ref = useRef<HTMLDialogElement>(null);
  const id = useId();
  const { tt } = useApp();
  useEffect(() => {
    const prev = document.activeElement as HTMLElement;
    ref.current?.showModal();
    return () => {
      prev?.focus();
    };
  }, []);
  return (
    <dialog
      ref={ref}
      className="quiz-dialog quiz"
      aria-labelledby={id}
      onCancel={close}
      onClick={(e) => {
        if (e.target === e.currentTarget) close();
      }}
    >
      <h2 id={id}>{title}</h2>
      {children}
      <button type="button" className="btn ghost" onClick={close}>
        {tt('Жабу', 'Закрыть')}
      </button>
    </dialog>
  );
}

/** Blob URLs preserve cookie + Mini App Bearer auth for private/draft media. */
export function QuizMedia({ src, alt, audio = false }: { src: string; alt: string; audio?: boolean }) {
  const { lang, tt } = useApp();
  const [media, setMedia] = useState<{ src: string; url: string } | null>(null),
    [error, setError] = useState('');
  useEffect(() => {
    let active = true,
      objectUrl = '';
    quizApi.media(src, lang).then(
      (blob) => {
        if (!active) return;
        objectUrl = URL.createObjectURL(blob);
        setMedia({ src, url: objectUrl });
        setError('');
      },
      (e) => {
        if (active) setError(e.message);
      },
    );
    return () => {
      active = false;
      if (objectUrl) URL.revokeObjectURL(objectUrl);
    };
  }, [src, lang]);
  if (error) return <ErrorText error={error} />;
  if (media?.src !== src) return <span role="status">{tt('Медиа жүктелуде…', 'Загрузка медиа…')}</span>;
  if (audio) return <audio controls src={media.url} aria-label={alt} />;
  // Go already resizes media; Next's image proxy cannot forward the in-memory Bearer token.
  // eslint-disable-next-line @next/next/no-img-element
  return <img src={media.url} alt={alt} />;
}
