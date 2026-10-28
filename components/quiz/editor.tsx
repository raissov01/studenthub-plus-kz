'use client';
import { useEffect, useState } from 'react';
import { useRouter } from 'next/navigation';
import { useApp } from '@/components/app';
import { quizApi, type QuizInput, type QuestionType, type QuizLang } from '@/lib/quiz-api';
import { ErrorText, Field, Loading, Select, TYPE_NAMES, useTask } from './common';
import { emptyQuestion, QuestionEditor } from './question-editor';
const blank = (): QuizInput => ({
  title: '',
  description: '',
  subject: '',
  visibility: 'private',
  isDraft: false,
  timePerQuestion: 30,
  shuffle: true,
  version: 0,
  questions: [emptyQuestion()],
});
export function QuizEditor({ id, notes = false }: { id?: string; notes?: boolean }) {
  const { tt, lang } = useApp();
  const router = useRouter();
  const task = useTask();
  const [initialLang] = useState(lang),
    [doc, setDoc] = useState<QuizInput>(blank),
    [savedId, setSavedId] = useState(id ? Number(id) : undefined);
  const [loaded, setLoaded] = useState(!id),
    [loadError, setLoadError] = useState(''),
    [review, setReview] = useState(false),
    [dirty, setDirty] = useState(false);
  const [mode, setMode] = useState<'manual' | 'text' | 'files' | 'excel'>(notes ? 'files' : 'manual');
  const [text, setText] = useState(''),
    [files, setFiles] = useState<File[]>([]),
    [notice, setNotice] = useState('');
  const [count, setCount] = useState(5),
    [difficulty, setDifficulty] = useState<'easy' | 'medium' | 'hard'>('medium'),
    [types, setTypes] = useState<QuestionType[]>(['mcq']),
    [outputLang, setOutputLang] = useState<QuizLang>(lang);
  useEffect(() => {
    if (!id) return;
    let active = true;
    quizApi.edit(id, initialLang).then(
      (r) => {
        if (active) {
          setDoc({ ...r.quiz, questions: r.questions });
          setReview(r.quiz.isDraft && r.quiz.source.startsWith('ai_'));
          setLoaded(true);
        }
      },
      (e) => {
        if (active) setLoadError(e.message);
      },
    );
    return () => {
      active = false;
    };
  }, [id, initialLang]);
  useEffect(() => {
    if (!dirty) return;
    const on = (e: BeforeUnloadEvent) => {
      e.preventDefault();
    };
    window.addEventListener('beforeunload', on);
    return () => window.removeEventListener('beforeunload', on);
  }, [dirty]);
  const patch = (v: Partial<QuizInput>) => {
    setDoc((d) => ({ ...d, ...v }));
    setDirty(true);
  };
  const generate = () =>
    void task.run(async () => {
      const opts = { count, difficulty, types, lang: outputLang, title: doc.title };
      const r =
        mode === 'text' ? await quizApi.generateText(text, opts) : await quizApi.generateFiles(files, opts);
      const draft = await quizApi.edit(r.id, lang);
      setDoc({ ...draft.quiz, questions: draft.questions });
      setSavedId(r.id);
      setReview(true);
      setMode('manual');
      setDirty(true);
      setNotice(
        tt(
          `Жасалды: ${r.generated}. Алып тасталды: ${r.dropped}.`,
          `Создано: ${r.generated}. Отброшено: ${r.dropped}.`,
        ) + (r.truncated ? tt(' Ұзын мәтін қысқартылды.', ' Длинный текст был сокращён.') : ''),
      );
    });
  if (loadError) return <ErrorText error={loadError} />;
  if (!loaded) return <Loading />;
  return (
    <>
      <header className="quiz-stack">
        <h1>
          {review
            ? tt('Жобаны тексеру', 'Проверка черновика')
            : id
              ? tt('Квизді өңдеу', 'Редактировать квиз')
              : tt('Квиз жасау', 'Создать квиз')}
        </h1>
        <p className="muted">
          {review
            ? tt(
                'Әр жауапты конспект үзіндісімен салыстырыңыз. Сұрақтарды өзгертіп не жойып, содан кейін сақтаңыз.',
                'Сверьте каждый ответ с цитатой из конспекта. Измените или удалите вопросы, затем сохраните.',
              )
            : tt(
                'Өзіңіз құрастырыңыз немесе конспектіңізді пайдаланыңыз.',
                'Составьте сами или используйте свой конспект.',
              )}
        </p>
      </header>
      {!id && !review && (
        <div className="quiz-actions">
          {(
            [
              ['manual', 'Қолмен', 'Вручную'],
              ['files', 'Конспектіден квиз', 'Квиз из конспекта'],
              ['text', 'Мәтінді қою', 'Вставить текст'],
              ['excel', 'Excel импорт', 'Импорт Excel'],
            ] as const
          ).map(([v, kk, ru]) => (
            <button
              className="chip"
              type="button"
              key={v}
              aria-pressed={mode === v}
              disabled={task.busy}
              onClick={() => setMode(v)}
            >
              {tt(kk, ru)}
            </button>
          ))}
        </div>
      )}
      {mode === 'text' || mode === 'files' ? (
        <form
          className="card quiz-stack"
          onSubmit={(e) => {
            e.preventDefault();
            generate();
          }}
        >
          <Field
            label={tt('Квиз атауы (міндетті емес)', 'Название квиза (необязательно)')}
            value={doc.title}
            maxLength={200}
            onChange={(title) => patch({ title })}
          />
          {mode === 'text' ? (
            <>
              <Field
                label={tt('Дәріс мәтіні', 'Текст лекции')}
                value={text}
                multiline
                required
                maxLength={100000}
                onChange={setText}
              />
              <p className="note">
                {tt('Кемінде 200 таңба.', 'Не менее 200 символов.')} {text.length}
              </p>
            </>
          ) : (
            <>
              <label className="field">
                {tt('Конспект файлдары', 'Файлы конспекта')}
                <input
                  type="file"
                  className="input"
                  multiple
                  accept=".pdf,.docx,.pptx,.txt,image/jpeg,image/png,image/webp"
                  onChange={(e) => setFiles(Array.from(e.target.files || []))}
                />
              </label>
              <label className="field">
                {tt('Камерамен түсіру', 'Снять на камеру')}
                <input
                  className="input"
                  type="file"
                  accept="image/jpeg,image/png,image/webp"
                  capture="environment"
                  onChange={(e) => setFiles((f) => [...f, ...Array.from(e.target.files || [])].slice(0, 5))}
                />
              </label>
              <p className="note">
                {tt(
                  'PDF, DOCX, PPTX, TXT немесе фото. 5 файлға, барлығы 4 МБ және 30 бетке дейін. Үлкен фотолар автоматты түрде кішірейтіледі. Мәтінсіз PDF үшін беттерді фотоға түсіріңіз.',
                  'PDF, DOCX, PPTX, TXT или фото. До 5 файлов, всего до 4 МБ и 30 страниц. Большие фото автоматически уменьшаются. Для PDF без текста сфотографируйте страницы.',
                )}
              </p>
              {files.map((f, i) => (
                <div className="quiz-inline" key={i}>
                  <span>{f.name}</span>
                  <button
                    type="button"
                    className="link-btn"
                    onClick={() => setFiles(files.filter((_, n) => n !== i))}
                  >
                    {tt('Жою', 'Удалить')}
                  </button>
                </div>
              ))}
            </>
          )}
          <div className="quiz-pair">
            <Select
              label={tt('Сұрақ саны', 'Количество вопросов')}
              value={count}
              onChange={(s) => setCount(Number(s))}
            >
              {[5, 10, 20].map((n) => (
                <option key={n}>{n}</option>
              ))}
            </Select>
            <Select
              label={tt('Қиындық', 'Сложность')}
              value={difficulty}
              onChange={(s) => setDifficulty(s as typeof difficulty)}
            >
              <option value="easy">{tt('Жеңіл', 'Легко')}</option>
              <option value="medium">{tt('Орташа', 'Средне')}</option>
              <option value="hard">{tt('Қиын', 'Сложно')}</option>
            </Select>
          </div>
          <Select
            label={tt('Сұрақтар тілі', 'Язык вопросов')}
            value={outputLang}
            onChange={(s) => setOutputLang(s as QuizLang)}
          >
            <option value="kk">Қазақша</option>
            <option value="ru">Русский</option>
          </Select>
          <fieldset>
            <legend>{tt('Сұрақ түрлері', 'Типы вопросов')}</legend>
            {(['mcq', 'mcq_multi', 'true_false'] as const).map((t) => (
              <label className="quiz-check" key={t}>
                <input
                  type="checkbox"
                  checked={types.includes(t)}
                  onChange={() => setTypes(types.includes(t) ? types.filter((x) => x !== t) : [...types, t])}
                />
                {tt(...TYPE_NAMES[t])}
              </label>
            ))}
          </fieldset>
          <button
            className="btn"
            disabled={
              task.busy ||
              !types.length ||
              (mode === 'text' ? text.trim().length < 200 : !files.length || files.length > 5)
            }
          >
            {tt('Квиз құрастыру', 'Составить квиз')}
          </button>
        </form>
      ) : mode === 'excel' ? (
        <section className="card quiz-stack">
          <h2>{tt('Excel импорт', 'Импорт Excel')}</h2>
          <a href={quizApi.templateUrl} className="link-btn">
            {tt('Excel үлгісін жүктеу', 'Скачать шаблон Excel')}
          </a>
          <label>
            {tt('Excel файлы (.xlsx)', 'Файл Excel (.xlsx)')}
            <input
              type="file"
              className="input"
              accept=".xlsx"
              disabled={task.busy}
              onChange={(e) => {
                const f = e.target.files?.[0];
                if (f)
                  void task.run(async () => {
                    const r = await quizApi.importExcel(f, lang);
                    patch({ questions: r.questions });
                    setReview(true);
                    setMode('manual');
                    setNotice(
                      tt(
                        `Импортталды: ${r.imported}. Өткізілген жолдар: ${r.warnings.map((w) => w.row).join(', ') || '0'}.`,
                        `Импортировано: ${r.imported}. Пропущенные строки: ${r.warnings.map((w) => w.row).join(', ') || '0'}.`,
                      ),
                    );
                  });
              }}
            />
          </label>
        </section>
      ) : (
        <form
          className="quiz-stack"
          onSubmit={(e) => {
            e.preventDefault();
            void task.run(async () => {
              const r = await quizApi.save({ ...doc, isDraft: false }, lang, savedId, review ? 'excel' : '');
              setDirty(false);
              router.push(`/quiz/${r.quiz.id}`);
            });
          }}
        >
          <fieldset disabled={task.busy} className="quiz-stack">
            <legend>{tt('Квиз баптаулары', 'Настройки квиза')}</legend>
            <Field
              label={tt('Атауы', 'Название')}
              value={doc.title}
              required
              maxLength={200}
              onChange={(title) => patch({ title })}
            />
            <Field
              label={tt('Сипаттама', 'Описание')}
              value={doc.description}
              multiline
              maxLength={1000}
              onChange={(description) => patch({ description })}
            />
            <Field
              label={tt('Пән', 'Предмет')}
              value={doc.subject}
              maxLength={80}
              onChange={(subject) => patch({ subject })}
            />
            <Select
              label={tt('Кім көре алады', 'Кто может видеть')}
              value={doc.visibility}
              onChange={(v) => patch({ visibility: v as QuizInput['visibility'] })}
            >
              <option value="private">{tt('Тек өзім', 'Только я')}</option>
              <option value="unlisted">{tt('Сілтеме арқылы', 'По ссылке')}</option>
              <option value="public">{tt('Барлығына ашық', 'Для всех')}</option>
            </Select>
            <Field
              label={tt('Бір сұраққа уақыт (секунд)', 'Время на вопрос (секунды)')}
              type="number"
              min={5}
              max={300}
              required
              value={doc.timePerQuestion}
              onChange={(v) => patch({ timePerQuestion: Number(v) })}
            />
            <label className="quiz-check">
              <input
                type="checkbox"
                checked={doc.shuffle}
                onChange={(e) => patch({ shuffle: e.target.checked })}
              />
              {tt('Сұрақтарды араластыру', 'Перемешивать вопросы')}
            </label>
            {doc.questions.map((q, i) => (
              <section className="card quiz-stack" key={`${savedId || 'new'}-${i}`}>
                <h2>
                  {tt('Сұрақ', 'Вопрос')} {i + 1}
                </h2>
                <QuestionEditor
                  index={i}
                  review={review}
                  q={q}
                  change={(v) => patch({ questions: doc.questions.map((x, n) => (n === i ? v : x)) })}
                />
                <div className="quiz-actions">
                  <button
                    type="button"
                    className="btn ghost"
                    onClick={() => patch({ questions: doc.questions.filter((_, n) => n !== i) })}
                  >
                    {tt('Сұрақты жою', 'Удалить вопрос')}
                  </button>
                  <button
                    type="button"
                    className="btn ghost"
                    disabled={i === 0}
                    onClick={() => {
                      const qs = [...doc.questions];
                      [qs[i - 1], qs[i]] = [qs[i], qs[i - 1]];
                      patch({ questions: qs });
                    }}
                  >
                    {tt('Жоғары жылжыту', 'Выше')}
                  </button>
                </div>
              </section>
            ))}
            <button
              type="button"
              className="btn ghost"
              disabled={doc.questions.length >= 200}
              onClick={() => patch({ questions: [...doc.questions, emptyQuestion()] })}
            >
              {tt('Сұрақ қосу', 'Добавить вопрос')}
            </button>
            <button className="btn" type="submit" disabled={!doc.questions.length}>
              {tt('Сақтау', 'Сохранить')}
            </button>
          </fieldset>
        </form>
      )}
      {task.busy && (
        <div className="card" role="status">
          <p>
            {mode === 'files' || mode === 'text'
              ? tt(
                  'Конспект оқылып, сұрақтар құрастырылуда… Бұл бірнеше минут алуы мүмкін.',
                  'Читаем конспект и составляем вопросы… Это может занять несколько минут.',
                )
              : tt('Сақталуда…', 'Сохранение…')}
          </p>
          <progress aria-label={tt('Өңдеу', 'Обработка')} />
        </div>
      )}
      {notice && (
        <p role="status" className="note">
          {notice}
        </p>
      )}
      <ErrorText error={task.error} />
    </>
  );
}
