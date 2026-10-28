'use client';
/* «Іздеу»: one screen for everything a student looks for. A sticky search box with the filter button, category chips
   that wrap (all visible), removable chips for the active filters, the result count and the list. /jobs, /internships,
   /events, /courses and /discounts are the same screen with their category selected; the filters of the selected
   category open in a bottom sheet on this screen. */
import { useEffect, useLayoutEffect, useRef, type ReactNode } from 'react';
import { useApp } from '@/components/app';
import { Chip, DemoBadge, Empty, Heart, Icon, Row } from '@/components/ui';
import { Loading, useFeature, useLoad } from '@/components/views/common';
import { DbEventCard } from '@/components/views/events';
import { SearchRow } from '@/components/views/home';
import { MarketSearchResults } from '@/components/views/market';
import { EventCard, JobRow, OppCard, SuggestLink } from '@/components/views/opps';
import { BonusTiers, StudentCardHero } from '@/components/views/profile';
import { CourseSheet, DealSheet } from '@/components/views/sheets';
import { useAccount } from '@/lib/account';
import { CITIES, COURSES, DEAL_CATS, EVENTS, EVENT_CATS, JOB_TAGS, OPP_TYPES } from '@/lib/data/static';
import { hourly, oppKind, oppLeft, searchItems, type SearchItem } from '@/lib/domain';
import { ALL_EVENT_CATS, canCreateEvents, eventListItems, evTitle, type DbEvent, type EventListItem } from '@/lib/events';
import { dOff, plural } from '@/lib/format';
import { activeFilters, clearAll, INTERN_TYPES, isSearchCat, JOB_FLAGS, matches, SEARCH_CATS, SEARCH_LABELS, type SearchCat } from '@/lib/search';
import type { App } from '@/components/app';
import type { UiState } from '@/lib/defaults';
import type { Discount } from '@/lib/types';

const OPP_CATS: SearchCat[] = ['intern', 'grant', 'mobility'];

/** label of one active filter chip */
function filterLabel(key: string, app: App, cat: SearchCat): string {
  const { ui, tt, tp, L } = app;
  if (key.startsWith('jf:')) {
    const k = key.slice(3);
    return k === 'today' ? tt('Бүгінгі', 'На сегодня') : tp(JOB_TAGS[k]);
  }
  if (key === 'city') return L(CITIES[ui.city] ?? { kk: ui.city, ru: ui.city });
  if (key === 'sortPay') return tt('Төлем бойынша', 'По оплате');
  if (key === 'oppType') return tp(OPP_TYPES[ui.oppType]);
  if (key === 'evCat') return L(ALL_EVENT_CATS[ui.evCat] ?? { kk: ui.evCat, ru: ui.evCat });
  if (key === 'courseMine') return tt('Мамандығыма', 'Для моей профессии');
  if (key === 'dealCat') return L(DEAL_CATS[ui.dealCat] ?? { kk: ui.dealCat, ru: ui.dealCat });
  return cat;
}

/** the chips that pick a category; `onPick` navigates (the old URL of the category, or /search?k=) */
function CatChips({ cur, onPick }: { cur: SearchCat; onPick: (k: SearchCat) => void }) {
  const { tt } = useApp();
  const marketOn = useFeature('market');
  const cats = SEARCH_CATS.filter(k => k !== 'market' || marketOn);
  return (
    <div className="chips wrap search-cats" role="group" aria-label={tt('Санат', 'Категория')}>
      {cats.map(k => <Chip key={k} pressed={cur === k} onClick={() => onPick(k)}>{tt(...SEARCH_LABELS[k])}</Chip>)}
    </div>
  );
}

/* ---------- the filter sheet ---------- */
function FilterSheet({ cat, onPick }: { cat: SearchCat; onPick: (k: SearchCat) => void }) {
  const app = useApp();
  const { ui, tt, tp, L, setUi, closeSheet, content } = app;
  const eventsOn = useFeature('events');
  const set = (p: Partial<UiState>) => setUi(p);
  const any = activeFilters(cat, ui).length > 0;
  let body: ReactNode;
  if (cat === 'jobs') {
    const toggle = (k: string) => setUi(u => ({ jf: { ...u.jf, [k]: !u.jf[k] } }));
    body = (
      <>
        <div className="field"><span className="lbl">{tt('Көрсету', 'Показать')}</span>
          <div className="chips wrap">
            {JOB_FLAGS.map(k => (
              <Chip key={k} pressed={!!ui.jf[k]} onClick={() => toggle(k)}>
                {k === 'today' ? <><Icon n="bolt" s={15} /> {tt('Бүгінгі', 'На сегодня')}</> : tp(JOB_TAGS[k])}
              </Chip>
            ))}
          </div>
        </div>
        <div className="grid-2">
          <div className="field"><label htmlFor="fCity">{tt('Қала', 'Город')}</label>
            <select className="input" id="fCity" value={ui.city} onChange={e => set({ city: e.target.value })}>
              <option value="all">{tt('Барлық қала', 'Все города')}</option>
              {Object.entries(CITIES).filter(([k]) => content.jobs.some(j => j.city === k)).map(([k, c]) => <option key={k} value={k}>{L(c)}</option>)}
            </select></div>
          <div className="field"><label htmlFor="fSort">{tt('Ретімен', 'Порядок')}</label>
            <select className="input" id="fSort" value={ui.sortPay ? 'pay' : 'new'} onChange={e => set({ sortPay: e.target.value === 'pay' })}>
              <option value="new">{tt('Жаңалары алдымен', 'Сначала новые')}</option>
              <option value="pay">{tt('Сағаттық төлем бойынша', 'По оплате в час')}</option>
            </select></div>
        </div>
      </>
    );
  } else if (OPP_CATS.includes(cat)) {
    // one «Түрі» list: internship sub-types narrow this category, grants and mobility are categories of their own
    const cur = cat === 'intern' ? (INTERN_TYPES.includes(ui.oppType) ? ui.oppType : 'all') : cat;
    const pick = (t: string) => {
      if (t === 'grant' || t === 'mobility') { setUi({ oppType: 'all' }); if (cat !== t) onPick(t); return; }
      setUi({ oppType: t });
      if (cat !== 'intern') onPick('intern');
    };
    const types: [string, string][] = [['all', tt('Барлық тағылымдама', 'Все стажировки')], ...INTERN_TYPES.map((k): [string, string] => [k, tp(OPP_TYPES[k])]),
      ['grant', tp(OPP_TYPES.grant)], ['mobility', tp(OPP_TYPES.mobility)]];
    body = (
      <div className="field"><span className="lbl">{tt('Түрі', 'Тип')}</span>
        <div className="chips wrap">{types.map(([k, l]) => <Chip key={k} pressed={cur === k} onClick={() => pick(k)}>{l}</Chip>)}</div>
      </div>
    );
  } else if (cat === 'events') {
    const src = eventsOn ? ALL_EVENT_CATS : EVENT_CATS;
    body = (
      <div className="field"><span className="lbl">{tt('Санат', 'Категория')}</span>
        <div className="chips wrap">
          {[['all', tt('Барлығы', 'Все')] as [string, string], ...Object.entries(src).map(([k, c]): [string, string] => [k, L(c)])]
            .map(([k, l]) => <Chip key={k} pressed={ui.evCat === k} onClick={() => set({ evCat: k })}>{l}</Chip>)}
        </div>
      </div>
    );
  } else if (cat === 'courses') {
    body = (
      <div className="field"><span className="lbl">{tt('Курстар', 'Курсы')}</span>
        <div className="chips wrap">
          <Chip pressed={!ui.courseMine} onClick={() => set({ courseMine: false })}>{tt('Барлық курстар', 'Все курсы')}</Chip>
          <Chip pressed={ui.courseMine} onClick={() => set({ courseMine: true })}><Icon n="target" s={15} /> {tt('Мамандығыма арналған', 'Для моей профессии')}</Chip>
        </div>
      </div>
    );
  } else if (cat === 'deals') {
    const cats: [string, string][] = [['all', tt('Барлығы', 'Все')], ...Object.entries(DEAL_CATS).filter(([k]) => content.discounts.some(d => d.cat === k)).map(([k, c]): [string, string] => [k, L(c)])];
    body = (
      <div className="field"><span className="lbl">{tt('Санат', 'Категория')}</span>
        <div className="chips wrap">{cats.map(([k, l]) => <Chip key={k} pressed={ui.dealCat === k} onClick={() => set({ dealCat: k })}>{l}</Chip>)}</div>
      </div>
    );
  } else {
    body = (
      <>
        <p className="small muted">{tt('Бұл санатта қосымша сүзгі жоқ. Санатты таңдасаңыз, оның сүзгілері шығады.', 'В этой категории дополнительных фильтров нет. Выберите категорию — появятся её фильтры.')}</p>
        <CatChips cur={cat} onPick={k => { if (k !== cat) onPick(k); }} />
      </>
    );
  }
  return (
    <>
      <h2>{tt('Сүзгілер', 'Фильтры')} · {tt(...SEARCH_LABELS[cat])}</h2>
      {body}
      <div className="btn-row">
        {any ? <button className="btn ghost" type="button" onClick={() => setUi(clearAll(cat))}>{tt('Тазалау', 'Сбросить')}</button> : null}
        <button className="btn" type="button" onClick={closeSheet}>{tt('Көрсету', 'Показать')}</button>
      </div>
    </>
  );
}

/* ---------- result lists ---------- */
function NoResults({ icon, msg, cat }: { icon: 'search' | 'globe' | 'calendar' | 'book' | 'tag'; msg: string; cat: SearchCat }) {
  const { ui, tt, setUi } = useApp();
  const q = ui.sq.trim();
  const filtered = activeFilters(cat, ui).length > 0;
  return (
    <Empty icon={icon} action={q || filtered ? (
      <div className="btn-row" style={{ justifyContent: 'center' }}>
        {q ? <button className="btn ghost" type="button" onClick={() => setUi({ sq: '' })}>{tt('Іздеуді тазалау', 'Очистить поиск')}</button> : null}
        {filtered ? <button className="btn ghost" type="button" onClick={() => setUi(clearAll(cat))}>{tt('Сүзгілерді тазалау', 'Сбросить фильтры')}</button> : null}
      </div>
    ) : null}>{msg}</Empty>
  );
}

const Count = ({ n }: { n: number }) => {
  const { tt } = useApp();
  return <p className="tiny muted" role="status">{tt(`Табылды: ${n}`, `Найдено: ${n}`)}</p>;
};

/** DB events (when the switch is on) and the legacy demo ones, filtered by category and query */
function useEventItems(on: boolean, cat: string): { items: EventListItem[]; loading: boolean } {
  const { ui, lang } = useApp();
  const eventsOn = useFeature('events');
  const acc = useAccount();
  const { data, loading } = useLoad<{ events: DbEvent[] }>(on && eventsOn ? '/api/events?scope=upcoming' : null);
  const items = eventListItems(eventsOn ? data?.events ?? null : null, EVENTS, { signedIn: !!acc.user, cat, q: ui.sq, lang });
  return { items, loading: eventsOn && on && loading && !data };
}

function JobsResults() {
  const { ui, L, tt, content } = useApp();
  const f = ui.jf;
  let list = content.jobs.filter(j => (!f.today || j.today) && ['noexp', 'daily', 'remote', 'near'].every(k => !f[k] || j.tags.includes(k))
    && (ui.city === 'all' || j.city === ui.city) && matches(ui.sq, L(j.title), L(j.org), L(j.desc), L(CITIES[j.city] ?? { kk: '', ru: '' })));
  if (ui.sortPay) list = list.slice().sort((a, b) => hourly(b) - hourly(a));
  return (
    <>
      <Count n={list.length} />
      {list.length ? <div className="group">{list.map(j => <JobRow key={j.id} j={j} heart />)}</div>
        : <NoResults icon="search" cat="jobs" msg={tt('Сәйкес вакансия жоқ. Сүзгіні азайтып көріңіз.', 'Подходящих вакансий нет. Попробуйте убрать фильтры.')} />}
      <SuggestLink />
    </>
  );
}

function OppResults({ cat }: { cat: SearchCat }) {
  const { ui, L, tt, content } = useApp();
  const list = content.opportunities
    .filter(o => oppKind(o) === cat && (cat !== 'intern' || !INTERN_TYPES.includes(ui.oppType) || o.type === ui.oppType) && matches(ui.sq, L(o.title), L(o.org), L(o.desc)))
    .sort((a, b) => oppLeft(a) - oppLeft(b));
  return (
    <>
      <Count n={list.length} />
      {list.length ? <div className="list">{list.map(o => <OppCard key={o.id} o={o} />)}</div>
        : <NoResults icon="globe" cat={cat} msg={tt('Бағдарлама табылмады.', 'Программы не найдены.')} />}
      <SuggestLink />
    </>
  );
}

function EventsResults() {
  const { ui, tt, go } = useApp();
  const acc = useAccount();
  const { items, loading } = useEventItems(true, ui.evCat);
  if (loading) return <Loading />;
  return (
    <>
      {canCreateEvents(acc.user?.role) ? <button className="btn ghost block" type="button" onClick={() => go('organize')}><Icon n="calendar" s={18} /> {tt('Ұйымдастырушы беті', 'Страница организатора')}</button> : null}
      <Count n={items.length} />
      {items.length
        ? <div className="list">{items.map(it => it.kind === 'db' ? <DbEventCard key={'db' + it.e.id} e={it.e} /> : <EventCard key={it.e.id} e={it.e} demo />)}</div>
        : <NoResults icon="calendar" cat="events" msg={tt('Іс-шара табылмады.', 'Событий не найдено.')} />}
    </>
  );
}

const CERT_LBL = (tt: App['tt']): Record<string, [string, string]> => ({
  free: [tt('Сертификат тегін', 'Сертификат бесплатно'), 'b-ok'], badge: [tt('Тегін бейдж', 'Бесплатный бейдж'), 'b-ok'], paid: [tt('Сертификат ақылы', 'Сертификат платный'), 'b-line'],
});
const weeksTxt = (n: number, tt: App['tt']) => tt(`${n} апта`, `${n} ${plural(n, ['неделя', 'недели', 'недель'])}`);

function CoursesResults() {
  const { S, ui, L, tt, openSheet } = useApp();
  const list = COURSES.filter(c => (!ui.courseMine || c.spec.includes(S.profile.spec)) && matches(ui.sq, L(c.title), c.platform));
  const certLbl = CERT_LBL(tt);
  return (
    <>
      <Count n={list.length} />
      {list.length ? (
        <div className="group">
          {list.map(c => (
            <Row key={c.id} onClick={() => openSheet(<CourseSheet id={c.id} />)} icon={S.done[c.id] ? 'award' : 'book'} tone={S.done[c.id] ? 'ok' : ''} title={L(c.title)}
              sub={<>{c.platform}, {weeksTxt(c.weeks, tt)}<span className="badges">{S.done[c.id] ? <span className="badge b-ok"><Icon n="check" s={12} /> {tt('Аяқталды', 'Пройден')}</span> : null}<span className={`badge ${certLbl[c.cert][1]}`}>{certLbl[c.cert][0]}</span></span></>} />
          ))}
        </div>
      ) : <NoResults icon="book" cat="courses" msg={tt('Курс табылмады.', 'Курсы не найдены.')} />}
      <p className="note">{tt('Оқудың өзі тегін. Сертификат құнын курс бетінен тексеріңіз.', 'Само обучение бесплатное. Стоимость сертификата уточняйте на странице курса.')}</p>
    </>
  );
}

/** a discount tile of the list (opens its sheet); also the admin editor's preview */
export function DealCard({ d }: { d: Discount }) {
  const { L, openSheet } = useApp();
  return (
    <div className="fav-card">
      <button className="card" type="button" onClick={() => openSheet(<DealSheet id={d.id} />)}>
        <span className="badges" style={{ alignSelf: 'flex-start' }}><span className="badge b-line">{L(DEAL_CATS[d.cat])}</span><DemoBadge show={d.is_demo} /></span>
        <span className="deal-pct">{d.pct}</span>
        <span className="row-title">{L(d.name)}</span>
        <span className="row-sub">{L(d.what)}</span>
      </button>
      <Heart id={d.id} opp={false} />
    </div>
  );
}

function DealsResults() {
  const { ui, L, tt, content } = useApp();
  const list = content.discounts.filter(d => (ui.dealCat === 'all' || d.cat === ui.dealCat) && matches(ui.sq, L(d.name), L(d.what), L(DEAL_CATS[d.cat]), L(d.cond)));
  return (
    <>
      <StudentCardHero />
      <Count n={list.length} />
      {list.length ? (
        <div className="deals">{list.map(d => <DealCard key={d.id} d={d} />)}</div>
      ) : <NoResults icon="tag" cat="deals" msg={tt('Жеңілдік табылмады.', 'Скидки не найдены.')} />}
      <BonusTiers />
    </>
  );
}

function AllResults() {
  const { S, I, ui, tt, L, lang, content, monS } = useApp();
  const marketOn = useFeature('market');
  const { items: evs } = useEventItems(true, 'all');
  const base = searchItems(S, I, content);
  const evItems: SearchItem[] = evs.map(it => {
    if (it.kind === 'db') {
      const d = new Date(new Date(it.e.starts_at).getTime() + 5 * 3600_000);
      return {
        k: 'events', id: 'ev' + it.e.id, go: 'event:' + it.e.id, org: evTitle(it.e, lang), noFav: true, fav: false, demo: false, badge: null,
        title: `${d.getUTCDate()} ${monS[d.getUTCMonth()]}${it.e.place_text ? ', ' + it.e.place_text : ''}`, meta: `${L(ALL_EVENT_CATS[it.e.cat] ?? ALL_EVENT_CATS.other)} · ${tt('Іс-шара', 'Событие')}`,
      };
    }
    const d = dOff(it.e.day);
    return {
      k: 'events', id: it.e.id, go: 'event:' + it.e.id, org: L(it.e.title), noFav: true, fav: false, demo: true, badge: null,
      title: `${d.getDate()} ${monS[d.getMonth()]}, ${L(it.e.place)}`, meta: `${L(EVENT_CATS[it.e.cat])} · ${tt('Іс-шара', 'Событие')}`,
    };
  });
  const courseItems: SearchItem[] = COURSES.map(c => ({
    k: 'courses', id: c.id, act: 'course', org: L(c.title), title: `${c.platform}, ${weeksTxt(c.weeks, tt)}`, meta: tt('Курс', 'Курс'),
    badge: c.cert === 'paid' ? null : [tt('Тегін', 'Бесплатно'), 'new'], fav: false, demo: false, noFav: true,
  }));
  const deals = base.filter(it => it.k === 'deals');
  const list = [...base.filter(it => it.k !== 'deals'), ...evItems, ...courseItems, ...deals].filter(it => matches(ui.sq, it.org, it.title, it.meta));
  return (
    <>
      {marketOn ? <MarketSearchResults q={ui.sq} /> : null}
      <Count n={list.length} />
      {list.length ? <div className="group">{list.map(it => <SearchRow key={it.k + it.id} it={it} />)}</div>
        : <NoResults icon="search" cat="all" msg={tt('Ештеңе табылмады. Басқа сөзбен іздеп көріңіз.', 'Ничего не нашлось. Попробуйте другое слово.')} />}
    </>
  );
}

function MarketResults() {
  const { ui, tt, go } = useApp();
  const on = useFeature('market');
  if (!on) return <NoResults icon="search" cat="market" msg={tt('Ештеңе табылмады. Басқа сөзбен іздеп көріңіз.', 'Ничего не нашлось. Попробуйте другое слово.')} />;
  return (
    <>
      <MarketSearchResults q={ui.sq} />
      {ui.sq.trim() ? null : <p className="note">{tt('Затты атауымен іздеңіз.', 'Ищите вещь по названию.')}</p>}
      <button className="btn ghost block" type="button" onClick={() => go('market')}><Icon n="gift" s={18} /> {tt('Барлық тегін заттар', 'Все вещи даром')}</button>
    </>
  );
}

/** short enough to show whole in the box at 320 px */
const PLACEHOLDER: Record<SearchCat, [string, string]> = {
  all: ['Жұмыс, грант, курс…', 'Работа, грант, курс…'],
  jobs: ['Вакансия, қала…', 'Вакансия, город…'],
  intern: ['Бағдарлама, ұйым…', 'Программа, вуз…'],
  grant: ['Грант, байқау…', 'Грант, конкурс…'],
  mobility: ['Университет, ел…', 'Вуз, страна…'],
  events: ['Іс-шара, орын…', 'Событие, место…'],
  courses: ['Курс, платформа…', 'Курс, платформа…'],
  deals: ['Серіктес, жеңілдік…', 'Партнёр, скидка…'],
  market: ['Зат атауы…', 'Название вещи…'],
};

/** `fixed` = the category the URL itself names (/jobs -> 'jobs'); on /search it comes from ?k= or stays «Барлығы» */
export function Search({ fixed }: { fixed?: SearchCat }) {
  const app = useApp();
  const { ui, tt, go, setUi, openSheet } = app;
  const marketOn = useFeature('market');
  const inp = useRef<HTMLInputElement>(null);

  // the URL decides the category when the screen opens (before the first paint, so no other category flashes)
  useLayoutEffect(() => {
    if (fixed) { setUi({ sk: fixed }); return; }
    let k: string | null = null;
    try { k = new URLSearchParams(window.location.search).get('k'); } catch { /* ignore */ }
    setUi({ sk: isSearchCat(k) ? k : 'all' });
    let q: string | null = null;
    try { q = new URLSearchParams(window.location.search).get('q'); } catch { /* ignore */ }
    if (q) setUi({ sq: q.slice(0, 100) });
  }, [fixed, setUi]);

  // focus (and the caret at the end) when the home search box sent the student here
  useEffect(() => {
    if (!ui.sqFocus) return;
    setUi({ sqFocus: false });
    const el = inp.current;
    if (!el) return;
    el.focus();
    try { el.setSelectionRange(el.value.length, el.value.length); } catch { /* type=search in some engines */ }
  }, [ui.sqFocus, setUi]);

  const raw = fixed ?? (isSearchCat(ui.sk) ? ui.sk : 'all');
  const cat: SearchCat = raw === 'market' && !marketOn ? 'all' : raw;
  const pick = (k: SearchCat) => { if (k !== cat) go('search:' + k, { replace: true }); };
  const active = activeFilters(cat, ui);
  const ph = PLACEHOLDER[cat];

  let results: ReactNode;
  switch (cat) {
    case 'jobs': results = <JobsResults />; break;
    case 'intern': case 'grant': case 'mobility': results = <OppResults cat={cat} />; break;
    case 'events': results = <EventsResults />; break;
    case 'courses': results = <CoursesResults />; break;
    case 'deals': results = <DealsResults />; break;
    case 'market': results = <MarketResults />; break;
    default: results = <AllResults />;
  }

  return (
    <>
      <div className="search-top">
        <div className="search-bar" role="search">
          <div className="search"><Icon n="search" s={18} />
            <input ref={inp} className="input" id="sq" type="search" enterKeyHint="search" autoComplete="off" placeholder={tt(...ph)} value={ui.sq}
              onChange={e => setUi({ sq: e.target.value })} aria-label={tt('Іздеу', 'Поиск')} />
          </div>
          <button className="icon-btn filter-btn" type="button" onClick={() => openSheet(<FilterSheet cat={cat} onPick={pick} />)}
            aria-label={active.length ? tt(`Сүзгілер: ${active.length} қосулы`, `Фильтры: включено ${active.length}`) : tt('Сүзгілер', 'Фильтры')}>
            <Icon n="filter" s={22} />
            {active.length ? <span className="count">{active.length}</span> : null}
          </button>
        </div>
      </div>
      <CatChips cur={cat} onPick={pick} />
      {active.length ? (
        <div className="chips wrap active-filters" aria-label={tt('Қосулы сүзгілер', 'Включённые фильтры')}>
          {active.map(f => {
            const l = filterLabel(f.key, app, cat);
            return (
              <button key={f.key} className="chip" type="button" aria-pressed="true" onClick={() => setUi(f.clear)} aria-label={tt(`Сүзгіні алып тастау: ${l}`, `Убрать фильтр: ${l}`)}>
                {l} <Icon n="x" s={14} />
              </button>
            );
          })}
          {active.length > 1 ? <button className="link-btn" type="button" onClick={() => setUi(clearAll(cat))}>{tt('Барлығын тазалау', 'Сбросить все')}</button> : null}
        </div>
      ) : null}
      <div id="sList" className="section">{results}</div>
    </>
  );
}
