'use client';
/* «Қайталау»: today's spaced-repetition session over the student's own decks (/review), or one subject's
   (/review?id=<subject id>). Scheduler: lib/srs.ts (SM-2); states are kept on the device (lib/studydb.ts). */
import { useState } from 'react';
import { useApp } from '@/components/app';
import { Empty, Icon } from '@/components/ui';
import { subjectDecks, todayYmd, useQueryId, useSubjects } from '@/components/views/subjects';
import { plural } from '@/lib/format';
import { dueQueue, GRADES, previewIntervals, review, type Grade, type QueueCard } from '@/lib/srs';
import { saveReview, useStudy, type StudySnap } from '@/lib/studydb';

export function ReviewScreen() {
  const id = useQueryId() ?? '';
  const { tt } = useApp();
  const study = useStudy();
  if (!study.ready) return <p className="small muted" role="status">{tt('Жүктелуде…', 'Загрузка…')}</p>;
  return <Session key={id} subject={id} study={study} />;
}

const GRADE_LABEL: Record<Grade, [string, string]> = {
  again: ['Қайта', 'Снова'], hard: ['Қиын', 'Трудно'], good: ['Жақсы', 'Хорошо'], easy: ['Оңай', 'Легко'],
};
const GRADE_CLASS: Record<Grade, string> = { again: 'btn danger', hard: 'btn ghost', good: 'btn', easy: 'btn secondary' };

function Session({ subject, study }: { subject: string; study: StudySnap }) {
  const { S, tt, go, back } = useApp();
  const { list } = useSubjects();
  const s = subject ? list.find(x => x.id === subject) : undefined;
  const decks = s ? subjectDecks(S, s) : S.decks;
  // the queue is fixed when the session starts; «Қайта» puts a card back at the end once more
  const [queue, setQueue] = useState<QueueCard[]>(() => dueQueue(decks, study.srs, todayYmd()));
  const [flipped, setFlipped] = useState(false);
  const [done, setDone] = useState(0);
  const days = (n: number) => (n === 0 ? tt('бүгін', 'сегодня') : tt(`${n} күн`, `${n} ${plural(n, ['день', 'дня', 'дней'])}`));

  if (!queue.length) {
    return (
      <Empty icon="check" action={
        <div className="btn-row">
          {s ? <button className="btn secondary" type="button" onClick={() => go('subjects:' + s.id, { replace: true })}>{s.name}</button> : null}
          <button className="btn ghost" type="button" onClick={back}>{tt('Артқа', 'Назад')}</button>
        </div>
      }>
        {done ? tt(`Бүгінге бітті: ${done} жауап. Келесі қайталау күні сақталды.`, `На сегодня всё: ответов ${done}. Дата следующего повтора сохранена.`)
          : tt('Бүгін қайталайтын карточка жоқ.', 'Сегодня повторять нечего.')}
      </Empty>
    );
  }
  const c = queue[0];
  const pre = previewIntervals(study.srs[c.key]);
  const answer = (g: Grade) => {
    const st = review(study.srs[c.key], g, todayYmd());
    void saveReview(c.key, st);
    setDone(n => n + 1);
    setFlipped(false);
    const rest = queue.slice(1);
    setQueue(g === 'again' && !rest.some(x => x.key === c.key) ? [...rest, c] : rest);
    // TODO(points): pay XP for a finished session through POST /api/points/claim (like the roadmap step) once
    // backend/internal/points knows a device-only reason for it; today claim accepts only 'roadmap_step' and rejects the rest.
  };
  return (
    <>
      <div className="card-row" style={{ justifyContent: 'space-between' }}>
        <span className="tiny muted num" id="reviewLeft">{tt('Қалды', 'Осталось')}: {queue.length}</span>
        <span className="tiny muted">{s ? s.name : tt('Барлық жиындар', 'Все наборы')}{c.isNew ? ' · ' + tt('жаңа', 'новая') : ''}</span>
      </div>
      <button className={`flash ${flipped ? 'back' : ''}`} type="button" aria-live="polite" onClick={() => setFlipped(f => !f)}>
        <span className="eyebrow">{flipped ? tt('Жауап', 'Ответ') : tt('Сұрақ', 'Вопрос')}</span>
        <span className="q">{flipped ? c.a : c.q}</span>
        {flipped ? null : <span className="tiny muted">{tt('Ойша жауап беріп, түртіңіз', 'Ответьте мысленно и нажмите')}</span>}
      </button>
      {flipped ? (
        <div className="sr-grades" role="group" aria-label={tt('Қаншалықты есте қалды?', 'Насколько хорошо помните?')}>
          {GRADES.map(g => (
            <button key={g} className={GRADE_CLASS[g]} type="button" onClick={() => answer(g)}>
              <span>{tt(...GRADE_LABEL[g])}</span><span className="tiny num">{days(pre[g])}</span>
            </button>
          ))}
        </div>
      ) : (
        <button className="btn block" type="button" onClick={() => setFlipped(true)}>{tt('Жауапты көрсету', 'Показать ответ')}</button>
      )}
      <p className="tiny muted">{tt('Аралықпен қайталау (SM-2): жақсы білетін карточка сирек, қиыны жиі шығады.', 'Интервальное повторение (SM-2): то, что знаете хорошо, — реже, трудное — чаще.')}</p>
    </>
  );
}

/** home: one short row, only when something is due today */
export function HomeReviewDue() {
  const { S, tt, go } = useApp();
  const study = useStudy();
  if (!study.ready || !S.decks.length) return null;
  const n = dueQueue(S.decks, study.srs, todayYmd()).length;
  if (!n) return null;
  return (
    <button className="row review-home" type="button" id="homeReview" onClick={() => go('review')}>
      <span className="ico"><Icon n="cards" /></span>
      <span className="row-main">
        <span className="row-title">{tt(`Бүгін қайталау: ${n} карточка`, `Повторить сегодня: ${n} ${plural(n, ['карточка', 'карточки', 'карточек'])}`)}</span>
      </span>
      <span className="chev"><Icon n="chev" s={18} /></span>
    </button>
  );
}
