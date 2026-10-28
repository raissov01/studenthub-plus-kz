'use client';
import { useApp } from '@/components/app';
import { quizApi, QUESTION_TYPES, type Question, type QuestionType, type SubQuestion } from '@/lib/quiz-api';
import { ErrorText, Field, Select, TYPE_NAMES, useTask, QuizMedia } from './common';
export function emptyQuestion(type: QuestionType = 'mcq'): Question {
  const base: Question = { type, prompt: '' };
  if (['mcq', 'mcq_multi', 'dropdown', 'poll'].includes(type))
    Object.assign(base, { options: ['', '', ''], correctIndex: 0, correctIndexes: [0] });
  if (type === 'true_false') base.correctBool = true;
  if (type === 'fill_blank') base.answers = [''];
  if (type === 'reorder') base.items = ['', ''];
  if (type === 'matching' || type === 'categorization')
    base.pairs = [
      { left: '', right: '' },
      { left: '', right: '' },
    ];
  if (type === 'hotspot' || type === 'labeling') {
    base.zones = [
      { id: 'z1', x: 10, y: 10, w: 25, h: 25, label: '' },
      { id: 'z2', x: 55, y: 55, w: 25, h: 25, label: '' },
    ];
    base.correctZone = 'z1';
  }
  if (type === 'comprehension') {
    base.passage = '';
    base.subQuestions = [{ id: 's1', type: 'fill_blank', prompt: '', answers: [''] }];
  }
  return base;
}
export function QuestionEditor({
  q,
  change,
  index,
  review = false,
  nested = false,
}: {
  q: Question;
  change: (q: Question) => void;
  index: number;
  review?: boolean;
  nested?: boolean;
}) {
  const { tt, lang } = useApp();
  const task = useTask();
  const patch = (p: Partial<Question>) => change({ ...q, ...p });
  const options = q.options || [];
  const image = q.image ? quizApi.imageUrl(q.image) : '';
  const list = (key: 'items' | 'answers', label: string, max: number) => (
    <fieldset>
      <legend>{label}</legend>
      {(q[key] || []).map((s, i) => (
        <div className="quiz-inline" key={i}>
          <Field
            label={`${i + 1}`}
            value={s}
            required
            maxLength={key === 'answers' ? 100 : 200}
            onChange={(v) => patch({ [key]: q[key]!.map((x, n) => (n === i ? v : x)) })}
          />
          <button
            className="btn ghost"
            type="button"
            disabled={q[key]!.length <= (key === 'answers' ? 1 : 2)}
            onClick={() => patch({ [key]: q[key]!.filter((_, n) => n !== i) })}
          >
            {tt('Жою', 'Удалить')}
          </button>
        </div>
      ))}
      <button
        type="button"
        className="btn ghost"
        disabled={(q[key]?.length || 0) >= max}
        onClick={() => patch({ [key]: [...q[key]!, ''] })}
      >
        {tt('Қосу', 'Добавить')}
      </button>
    </fieldset>
  );
  return (
    <div className="quiz-stack">
      <Select
        label={tt('Сұрақ түрі', 'Тип вопроса')}
        value={q.type}
        onChange={(v) =>
          change({
            ...emptyQuestion(v as QuestionType),
            id: q.id,
            prompt: q.prompt,
            image: q.image,
            sourceQuote: q.sourceQuote,
            explanation: q.explanation,
          })
        }
      >
        {(nested ? (['mcq', 'true_false', 'fill_blank'] as QuestionType[]) : QUESTION_TYPES).map((t) => (
          <option value={t} key={t}>
            {tt(...TYPE_NAMES[t])}
          </option>
        ))}
      </Select>
      <Field
        label={tt('Сұрақ', 'Вопрос')}
        value={q.prompt}
        onChange={(prompt) => patch({ prompt })}
        required
        maxLength={1000}
        multiline
      />
      {options.length > 0 && (
        <fieldset>
          <legend>{tt('Жауап нұсқалары · дұрысын белгілеңіз', 'Варианты · отметьте верный')}</legend>
          {options.map((o, i) => (
            <div className="quiz-option-edit" key={i}>
              {q.type !== 'poll' && (
                <label className="quiz-check">
                  <input
                    type={q.type === 'mcq_multi' ? 'checkbox' : 'radio'}
                    name={`correct-${index}`}
                    checked={q.type === 'mcq_multi' ? !!q.correctIndexes?.includes(i) : q.correctIndex === i}
                    onChange={() =>
                      patch(
                        q.type === 'mcq_multi'
                          ? {
                              correctIndexes: q.correctIndexes?.includes(i)
                                ? q.correctIndexes.filter((x) => x !== i)
                                : [...(q.correctIndexes || []), i],
                            }
                          : { correctIndex: i },
                      )
                    }
                  />
                  <span>
                    {tt('Дұрыс', 'Верный')} {i + 1}
                  </span>
                </label>
              )}
              <Field
                label={`${tt('Нұсқа', 'Вариант')} ${i + 1}`}
                required
                maxLength={200}
                value={o}
                onChange={(v) => patch({ options: options.map((x, n) => (n === i ? v : x)) })}
              />
              <button
                type="button"
                className="link-btn"
                disabled={options.length <= (q.type === 'mcq_multi' ? 3 : 2)}
                onClick={() =>
                  patch({ options: options.filter((_, n) => n !== i), correctIndex: 0, correctIndexes: [0] })
                }
              >
                {tt('Нұсқаны жою', 'Удалить вариант')}
              </button>
            </div>
          ))}
          <button
            type="button"
            className="btn ghost"
            disabled={options.length >= 6}
            onClick={() => patch({ options: [...options, ''] })}
          >
            {tt('Нұсқа қосу', 'Добавить вариант')}
          </button>
        </fieldset>
      )}
      {q.type === 'true_false' && (
        <Select
          label={tt('Дұрыс жауап', 'Верный ответ')}
          value={String(q.correctBool)}
          onChange={(v) => patch({ correctBool: v === 'true' })}
        >
          <option value="true">{tt('Дұрыс', 'Верно')}</option>
          <option value="false">{tt('Бұрыс', 'Неверно')}</option>
        </Select>
      )}
      {q.type === 'fill_blank' && list('answers', tt('Қабылданатын жауаптар', 'Допустимые ответы'), 5)}
      {q.type === 'reorder' &&
        list('items', tt('Элементтерді дұрыс ретпен жазыңыз', 'Запишите элементы в верном порядке'), 10)}
      {q.pairs && (
        <fieldset>
          <legend>{tt('Дұрыс жұптар', 'Верные пары')}</legend>
          {q.pairs.map((p, i) => (
            <div className="quiz-pair" key={i}>
              <Field
                label={`${tt('Элемент', 'Элемент')} ${i + 1}`}
                value={p.left}
                required
                maxLength={200}
                onChange={(left) => patch({ pairs: q.pairs!.map((x, n) => (n === i ? { ...x, left } : x)) })}
              />
              <Field
                label={tt('Сәйкестік / топ', 'Соответствие / категория')}
                required
                maxLength={200}
                value={p.right}
                onChange={(right) =>
                  patch({ pairs: q.pairs!.map((x, n) => (n === i ? { ...x, right } : x)) })
                }
              />
              <button
                type="button"
                className="link-btn"
                disabled={q.pairs!.length <= 2}
                onClick={() => patch({ pairs: q.pairs!.filter((_, n) => n !== i) })}
              >
                {tt('Жою', 'Удалить')}
              </button>
            </div>
          ))}
          <button
            type="button"
            className="btn ghost"
            disabled={q.pairs.length >= (q.type === 'matching' ? 8 : 12)}
            onClick={() => patch({ pairs: [...q.pairs!, { left: '', right: '' }] })}
          >
            {tt('Жұп қосу', 'Добавить пару')}
          </button>
        </fieldset>
      )}
      {!nested && (
        <div className="field">
          <label>
            {tt('Сурет жүктеу (JPEG, PNG, WebP)', 'Загрузить изображение (JPEG, PNG, WebP)')}
            <input
              className="input"
              type="file"
              accept="image/jpeg,image/png,image/webp"
              disabled={task.busy}
              onChange={(e) => {
                const file = e.target.files?.[0];
                if (file)
                  void task.run(async () => {
                    const r = await quizApi.uploadImage(file, lang);
                    patch({ image: r.name });
                  });
              }}
            />
          </label>
          {image && (
            <>
              <div className="quiz-image">
                <QuizMedia src={image} alt={tt('Сұрақ суреті', 'Изображение вопроса')} />
                {q.zones?.map((z, i) => (
                  <span
                    className="quiz-zone"
                    key={z.id}
                    style={{ left: `${z.x}%`, top: `${z.y}%`, width: `${z.w}%`, height: `${z.h}%` }}
                  >
                    {i + 1}
                  </span>
                ))}
              </div>
              <button
                type="button"
                className="link-btn"
                onClick={() => {
                  patch({ image: undefined });
                }}
              >
                {tt('Суретті жою', 'Удалить изображение')}
              </button>
            </>
          )}
          <ErrorText error={task.error} />
        </div>
      )}
      {q.zones && (
        <fieldset>
          <legend>{tt('Аймақтар (сурет өлшемінің пайызы)', 'Области (проценты размера изображения)')}</legend>
          {q.zones.map((z, i) => (
            <div className="card quiz-stack" key={z.id}>
              <b>
                {tt('Аймақ', 'Область')} {i + 1}
              </b>
              <div className="quiz-coordinates">
                {(['x', 'y', 'w', 'h'] as const).map((key, n) => (
                  <Field
                    key={key}
                    label={
                      [
                        tt('Сол жақ', 'Слева'),
                        tt('Жоғары', 'Сверху'),
                        tt('Ені', 'Ширина'),
                        tt('Биіктігі', 'Высота'),
                      ][n]
                    }
                    type="number"
                    min={key === 'w' || key === 'h' ? 1 : 0}
                    max={100}
                    value={z[key]}
                    onChange={(v) =>
                      patch({ zones: q.zones!.map((x, j) => (j === i ? { ...x, [key]: Number(v) } : x)) })
                    }
                  />
                ))}
              </div>
              {q.type === 'labeling' ? (
                <Field
                  label={tt('Дұрыс атау', 'Верная подпись')}
                  value={z.label || ''}
                  required
                  maxLength={60}
                  onChange={(label) =>
                    patch({ zones: q.zones!.map((x, j) => (j === i ? { ...x, label } : x)) })
                  }
                />
              ) : (
                <label className="quiz-check">
                  <input
                    type="radio"
                    name={`zone-${index}`}
                    checked={q.correctZone === z.id}
                    onChange={() => patch({ correctZone: z.id })}
                  />
                  {tt('Дұрыс аймақ', 'Верная область')}
                </label>
              )}
              <button
                type="button"
                className="link-btn"
                disabled={q.zones!.length <= (q.type === 'labeling' ? 2 : 1)}
                onClick={() =>
                  patch({
                    zones: q.zones!.filter((x) => x.id !== z.id),
                    correctZone: q.zones!.find((x) => x.id !== z.id)?.id,
                  })
                }
              >
                {tt('Аймақты жою', 'Удалить область')}
              </button>
            </div>
          ))}
          <button
            type="button"
            className="btn ghost"
            disabled={q.zones.length >= 10}
            onClick={() =>
              patch({
                zones: [
                  ...q.zones!,
                  { id: `z${Date.now().toString(36)}`, x: 0, y: 0, w: 20, h: 20, label: '' },
                ],
              })
            }
          >
            {tt('Аймақ қосу', 'Добавить область')}
          </button>
        </fieldset>
      )}
      {q.type === 'comprehension' && (
        <>
          <Field
            label={tt('Мәтін', 'Текст')}
            value={q.passage || ''}
            multiline
            required
            maxLength={5000}
            onChange={(passage) => patch({ passage })}
          />
          {q.subQuestions?.map((s, i) => (
            <fieldset key={s.id}>
              <legend>
                {tt('Ішкі сұрақ', 'Подвопрос')} {i + 1}
              </legend>
              <QuestionEditor
                nested
                q={{ ...s, id: undefined }}
                index={index * 1000 + i + 1000}
                change={(v) =>
                  patch({
                    subQuestions: q.subQuestions!.map((x, j) =>
                      j === i ? ({ ...v, id: s.id } as SubQuestion) : x,
                    ),
                  })
                }
              />
              <button
                type="button"
                className="link-btn"
                disabled={q.subQuestions!.length <= 1}
                onClick={() => patch({ subQuestions: q.subQuestions!.filter((x) => x.id !== s.id) })}
              >
                {tt('Жою', 'Удалить')}
              </button>
            </fieldset>
          ))}
          <button
            type="button"
            className="btn ghost"
            disabled={(q.subQuestions?.length || 0) >= 10}
            onClick={() =>
              patch({
                subQuestions: [
                  ...q.subQuestions!,
                  { id: `s${Date.now().toString(36)}`, type: 'fill_blank', prompt: '', answers: [''] },
                ],
              })
            }
          >
            {tt('Ішкі сұрақ қосу', 'Добавить подвопрос')}
          </button>
        </>
      )}
      {!nested && (
        <>
          <Field
            label={tt('Түсіндірме', 'Объяснение')}
            value={q.explanation || ''}
            multiline
            maxLength={1000}
            onChange={(explanation) => patch({ explanation })}
          />
          {(review || q.sourceQuote) && (
            <div className="quiz-quote">
              <p>{tt('Жауап конспектіде мына жерде:', 'Ответ в конспекте находится здесь:')}</p>
              <blockquote>
                {q.sourceQuote ||
                  tt('Үзінді жоқ. Жауапты өзіңіз тексеріңіз.', 'Цитаты нет. Проверьте ответ самостоятельно.')}
              </blockquote>
            </div>
          )}
        </>
      )}
    </div>
  );
}
