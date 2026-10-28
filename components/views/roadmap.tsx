'use client';
/* «Даму жоспары»: the student picks 3–5 items of the 24-item checklist, sees 1–3 concrete steps a week, marks them
   done, can pause for the exam session. Rules live in features/roadmap (tested); state stays on the device. */
import { useState } from 'react';
import { useApp, type App } from '@/components/app';
import { Chip, Icon, SectionHead } from '@/components/ui';
import { track } from '@/features/analytics/track';
import { CATEGORIES, ITEMS, ITEM_BY_KEY, type RoadmapCategory, type RoadmapLink } from '@/features/roadmap/items';
import {
  completeStep, isItemDone, isPaused, MAX_CHOSEN, MIN_CHOSEN, pause, progress, resume, statusLine, streak, toggleChosen,
  undoStep, weeklySteps, type RoadmapState,
} from '@/features/roadmap/logic';
import { parseYmd } from '@/lib/format';
import { getState } from '@/lib/store';

/** where an item's «link» leads in the app */
const LINKS: Record<RoadmapLink, { view: string; oppType?: string }> = {
  internships: { view: 'opps:intern', oppType: 'all' },
  mobility: { view: 'search:mobility' },
  grants: { view: 'search:grant' },
  courses: { view: 'opps:courses' },
  events: { view: 'opps:events' },
  jobs: { view: 'opps:jobs' },
  resume: { view: 'resume' },
  ai: { view: 'ai' },
  interview: { view: 'interview' },
};

function openLink(app: App, link: RoadmapLink) {
  const l = LINKS[link];
  if (l.oppType) app.setUi({ oppType: l.oppType });
  app.go(l.view);
}

function markStep(app: App, item: string, index: number, done: boolean) {
  const now = new Date();
  const before = getState().roadmap;
  const next = done ? completeStep(before, item, index, now) : undoStep(before, item, index);
  app.update(s => { s.roadmap = next; });
  if (!done) return;
  track('roadmap_item_done', { item });
  if (!isItemDone(before, item) && isItemDone(next, item)) app.toast(app.tt('Пункт толық орындалды. Жарайсың!', 'Пункт полностью выполнен. Отлично!'));
}

/** this week's steps as checkable rows (shared by the screen and the home board) */
function WeekSteps({ r, links = true }: { r: RoadmapState; links?: boolean }) {
  const app = useApp();
  const { tt, lang } = app;
  const steps = weeklySteps(r, new Date());
  if (!steps.length) {
    return <div className="group"><div className="empty">{isPaused(r, new Date())
      ? tt('Кідірту кезінде қадамдар көрсетілмейді.', 'Во время паузы шаги не показываются.')
      : tt('Таңдаған пункттеріңнің бәрі орындалды. Жаңа пункт қосып көр.', 'Все выбранные пункты выполнены. Добавьте новый.')}</div></div>;
  }
  return (
    <div className="group">
      {steps.map(st => {
        const item = ITEM_BY_KEY[st.item];
        return (
          <div key={st.item} className="row rm-step">
            <label className="check rm-check">
              <input type="checkbox" checked={false} onChange={() => markStep(app, st.item, st.index, true)} />
              <span className="row-main"><span className="row-title">{item.steps[st.index][lang]}</span><span className="row-sub">{item.title[lang]}</span></span>
            </label>
            {links && item.link ? (
              <button className="icon-btn" type="button" aria-label={tt('Бөлімге өту', 'Перейти в раздел')} onClick={() => openLink(app, item.link!)}><Icon n="chev" s={18} /></button>
            ) : null}
          </div>
        );
      })}
    </div>
  );
}

function ProgressCard({ r }: { r: RoadmapState }) {
  const { tt, lang } = useApp();
  const now = new Date();
  const pr = progress(r), pct = pr.total ? Math.round((pr.done / pr.total) * 100) : 0, n = streak(r, now);
  return (
    <div className="card">
      <p className="row-title">{statusLine(r, now, lang)}</p>
      <span className="meter" role="progressbar" aria-label={tt('Жоспар прогресі', 'Прогресс плана')} aria-valuenow={pct} aria-valuemin={0} aria-valuemax={100}><i style={{ width: `${pct}%` }}></i></span>
      <span className="row-sub num">{tt(`${pr.done}/${pr.total} қадам · ${pr.itemsDone} пункт орындалды · ${n} апта қатарынан`, `${pr.done}/${pr.total} шагов · выполнено пунктов: ${pr.itemsDone} · недель подряд: ${n}`)}</span>
    </div>
  );
}

/** one chosen item with all its steps */
function ItemSheet({ k }: { k: string }) {
  const app = useApp();
  const { S, tt, lang, closeSheetDom } = app;
  const item = ITEM_BY_KEY[k];
  if (!item) return null;
  const done = S.roadmap.done[k] ?? [];
  return (
    <>
      <span className="eyebrow">{CATEGORIES[item.cat][lang]}</span>
      <h2>{item.title[lang]}</h2>
      <div className="group">
        {item.steps.map((st, i) => (
          <label key={i} className="row check">
            <input type="checkbox" checked={done.includes(i)} onChange={e => markStep(app, k, i, e.target.checked)} />
            <span className="row-main"><span className="row-title">{st[lang]}</span></span>
          </label>
        ))}
      </div>
      {item.link ? <button className="btn ghost" type="button" onClick={() => { closeSheetDom(); openLink(app, item.link!); }}><Icon n="ext" s={18} /> {tt('Бөлімге өту', 'Перейти в раздел')}</button> : null}
    </>
  );
}

function Picker({ onDone }: { onDone: () => void }) {
  const { S, tt, lang, update, toast } = useApp();
  const chosen = S.roadmap.chosen;
  const cats = Object.keys(CATEGORIES) as RoadmapCategory[];
  return (
    <>
      <section className="section">
        <p className="small">{tt(
          `Студенттік өмірдің ${ITEMS.length} пунктінен өзіңе маңыздысын таңда: ${MIN_CHOSEN}–${MAX_CHOSEN}. Аптасына тек 1–3 нақты қадам көрсетеміз.`,
          `Выберите из ${ITEMS.length} пунктов студенческой жизни важные для вас: ${MIN_CHOSEN}–${MAX_CHOSEN}. Каждую неделю покажем только 1–3 конкретных шага.`)}</p>
        <p className="tiny muted num" aria-live="polite">{tt(`Таңдалды: ${chosen.length} / ${MAX_CHOSEN}`, `Выбрано: ${chosen.length} / ${MAX_CHOSEN}`)}</p>
      </section>
      {cats.map(c => (
        <section key={c} className="section">
          <h2>{CATEGORIES[c][lang]}</h2>
          <div className="chips wrap">
            {ITEMS.filter(i => i.cat === c).map(i => (
              <Chip key={i.key} pressed={chosen.includes(i.key)} onClick={() => {
                if (!chosen.includes(i.key) && chosen.length >= MAX_CHOSEN) { toast(tt(`Ең көбі ${MAX_CHOSEN} пункт`, `Не больше ${MAX_CHOSEN} пунктов`)); return; }
                update(s => { s.roadmap = toggleChosen(s.roadmap, i.key); });
              }}>{chosen.includes(i.key) ? <Icon n="check" s={15} /> : null}{i.title[lang]}</Chip>
            ))}
          </div>
        </section>
      ))}
      <div className="sticky-cta">
        <button className="btn block" type="button" disabled={chosen.length < MIN_CHOSEN} onClick={onDone}>
          {chosen.length < MIN_CHOSEN ? tt(`Тағы ${MIN_CHOSEN - chosen.length} пункт таңда`, `Выберите ещё ${MIN_CHOSEN - chosen.length}`) : tt('Жоспарды сақтау', 'Сохранить план')}
        </button>
      </div>
    </>
  );
}

export function Roadmap() {
  const { S, tt, I, lang, update, toast, openSheet } = useApp();
  const r = S.roadmap;
  const until = r.pausedUntil ? I.fDate(parseYmd(r.pausedUntil)) : '';
  const [editing, setEditing] = useState(r.chosen.length < MIN_CHOSEN);
  if (editing || r.chosen.length < MIN_CHOSEN) return <Picker onDone={() => { setEditing(false); window.scrollTo(0, 0); }} />;
  const now = new Date(), paused = isPaused(r, now);
  return (
    <>
      <ProgressCard r={r} />
      <section className="section">
        <SectionHead title={tt('Осы аптаның қадамдары', 'Шаги на эту неделю')} />
        <WeekSteps r={r} />
      </section>
      <section className="section">
        <SectionHead title={tt('Менің пункттерім', 'Мои пункты')} more={<button className="link-btn" type="button" onClick={() => setEditing(true)}>{tt('Өзгерту', 'Изменить')}</button>} />
        <div className="group">
          {r.chosen.filter(k => ITEM_BY_KEY[k]).map(k => {
            const item = ITEM_BY_KEY[k], d = Math.min(r.done[k]?.length ?? 0, item.steps.length), full = isItemDone(r, k);
            return (
              <button key={k} className="row" type="button" onClick={() => openSheet(<ItemSheet k={k} />)}>
                <span className={`ico ${full ? 'ok' : ''}`}><Icon n={full ? 'check' : 'target'} /></span>
                <span className="row-main"><span className="row-title">{item.title[lang]}</span><span className="row-sub num">{CATEGORIES[item.cat][lang]} · {d}/{item.steps.length} {tt('қадам', 'шаг.')}</span></span>
                <span className="chev"><Icon n="chev" s={18} /></span>
              </button>
            );
          })}
        </div>
      </section>
      <section className="section">
        {paused
          ? <>
            <p className="note">{tt(`Жоспар кідіртілді, кідірту ${until} күні аяқталады. Серияң сақталады.`, `План на паузе до ${until} включительно. Серия сохранится.`)}</p>
            <button className="btn secondary block" type="button" onClick={() => { update(s => { s.roadmap = resume(s.roadmap); }); toast(tt('Жоспар жалғасты', 'План продолжен')); }}>{tt('Жалғастыру', 'Продолжить')}</button>
          </>
          : <>
            <button className="btn ghost block" type="button" onClick={() => { update(s => { s.roadmap = pause(s.roadmap, new Date()); }); toast(tt('2 аптаға кідіртілді', 'Пауза на 2 недели')); }}>{tt('Емтихан кезеңі — кідірту', 'Сессия — поставить на паузу')}</button>
            <p className="tiny muted">{tt('2 аптаға кідіртеміз, сол уақытта серия үзілмейді. Бір апта өткізіп алсаң да, серия нөлге түспейді.', 'Ставим паузу на 2 недели, серия за это время не прервётся. Даже если пропустить одну неделю, серия не обнулится.')}</p>
          </>}
      </section>
      <p className="note">{tt('Жоспарың тек осы құрылғыда сақталады.', 'План хранится только на этом устройстве.')}</p>
    </>
  );
}

/** home board: status, this week's steps, a way in */
export function RoadmapBoard() {
  const { S, tt, go } = useApp();
  const r = S.roadmap;
  const set = r.chosen.length >= MIN_CHOSEN;
  return (
    <section className="section">
      <SectionHead title={tt('Даму жоспары', 'План развития')} more={set ? <button className="link-btn" type="button" onClick={() => go('roadmap')}>{tt('Толығырақ', 'Подробнее')}</button> : null} />
      {set ? (
        <>
          <ProgressCard r={r} />
          <WeekSteps r={r} links={false} />
        </>
      ) : (
        <div className="card">
          <span className="row-title">{tt('Студенттік өмірдің 24 пункті', '24 пункта студенческой жизни')}</span>
          <span className="row-sub">{tt('Тағылымдама, IELTS, жинақ, спорт, саяхат… Өзіңе маңызды 3–5-ін таңда, аптасына 1–3 қадам жаса.', 'Стажировка, IELTS, накопления, спорт, путешествия… Выберите 3–5 важных для вас и делайте 1–3 шага в неделю.')}</span>
          <button className="btn" type="button" onClick={() => go('roadmap')}><Icon n="target" s={18} /> {tt('Жоспар құру', 'Составить план')}</button>
        </div>
      )}
    </section>
  );
}

/** profile: a compact progress card */
export function RoadmapProgress() {
  const { S, tt, go } = useApp();
  const r = S.roadmap;
  const pr = progress(r), pct = pr.total ? Math.round((pr.done / pr.total) * 100) : 0;
  return (
    <button className="card fill" type="button" onClick={() => go('roadmap')}>
      <span className="fill-head"><span className="row-title">{tt('Даму жоспары', 'План развития')}</span><b className="num">{r.chosen.length >= MIN_CHOSEN ? `${pct}%` : ''}</b></span>
      {r.chosen.length >= MIN_CHOSEN
        ? <>
          <span className="meter" role="progressbar" aria-label={tt('Даму жоспары', 'План развития')} aria-valuenow={pct} aria-valuemin={0} aria-valuemax={100}><i style={{ width: `${pct}%` }}></i></span>
          <span className="row-sub num">{tt(`${pr.done}/${pr.total} қадам, ${pr.itemsDone} пункт орындалды`, `${pr.done}/${pr.total} шагов, выполнено пунктов: ${pr.itemsDone}`)}</span>
        </>
        : <span className="row-sub">{tt('Өзіңе маңызды 3–5 пунктті таңдап, жоспар құр.', 'Выберите 3–5 важных пунктов и составьте план.')}</span>}
    </button>
  );
}
