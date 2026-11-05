'use client';
/* «Іздеу» and the sections.
   - «Іздеу» (/search): one search box over everything with the filter button; the sheet narrows it to one section
     (the scope). No category chips: the sections have their own sub-tabs instead.
   - A section (/jobs, /internships, /events, /courses, /discounts): its own sub-tabs (lib/sections.ts), sticky under
     the header together with the search box; the sub-tab rides in ?t= so a link or «back» lands on the same tab.
   Old /search?k=<category> links land on the section that now holds the category. */
import { Suspense, useEffect, useLayoutEffect, useRef, useState, type KeyboardEvent, type ReactNode } from 'react';
import { useSearchParams } from 'next/navigation';
import { useApp } from '@/components/app';
import { Chip, Empty, Heart, Icon, Row } from '@/components/ui';
import { PinButton } from '@/components/views/calendar';
import { useFeature } from '@/components/views/common';
import { SearchRow } from '@/components/views/home';
import { MarketGrid, MarketSearchResults } from '@/components/views/market';
import { JobRow, OppCard, SuggestLink } from '@/components/views/opps';
import { StudentCardHero } from '@/components/views/profile';
import { NotifyToggle, type Cat as NotifyCat } from '@/components/views/notify';
import { CourseSheet, DealSheet } from '@/components/views/sheets';
import { useAccount } from '@/lib/account';
import { useConfig } from '@/lib/config';
import { useDesktop } from '@/lib/desktop';
import { CITIES, COURSES, DEAL_CATS, EVENT_CATS, JOB_TAGS } from '@/lib/data/static';
import { hourly, oppLeft, searchItems, type SearchItem } from '@/lib/domain';
import { ALL_EVENT_CATS, canCreateEvents, evTitle, evDesc } from '@/lib/events';
import { plural } from '@/lib/format';
import { pathOf } from '@/lib/routes';
import { activeFilters, clearAll, isScope, JOB_FLAGS, matches, SCOPES, scopeOfKind, type FilterCat, type Scope } from '@/lib/search';
import { eventInTab, jobTab, oppPlace, pickTab, SECTIONS, sectionView, viewOfSearchCat, visibleTabs, type SectionId, type SectionTab } from '@/lib/sections';
import type { App } from '@/components/app';
import type { UiState } from '@/lib/defaults';
import type { Discount } from '@/lib/types';

/** label of one active filter chip */
function filterLabel(key: string, app: App): string {
  const { ui, tt, tp, L } = app;
  if (key.startsWith('jf:')) {
    const k = key.slice(3);
    return k === 'today' ? tt('Бүгінгі', 'На сегодня') : tp(JOB_TAGS[k]);
  }
  if (key === 'scope') return isScope(ui.sk) && ui.sk !== 'all' ? tt(...SECTIONS[ui.sk].label) : ui.sk;
  if (key === 'city') return L(CITIES[ui.city] ?? { kk: ui.city, ru: ui.city });
  if (key === 'sortPay') return tt('Төлем бойынша', 'По оплате');
  if (key === 'evCat') return L(ALL_EVENT_CATS[ui.evCat] ?? { kk: ui.evCat, ru: ui.evCat });
  if (key === 'courseMine') return tt('Мамандығыма', 'Для моей профессии');
  if (key === 'dealCat') return L(DEAL_CATS[ui.dealCat] ?? { kk: ui.dealCat, ru: ui.dealCat });
  return key;
}

/* ---------- the filter sheet ---------- */
/** the filters: a bottom sheet on a phone, a side panel next to the list on a laptop (`panel`) */
function FilterSheet({ cat, panel }: { cat: FilterCat; panel?: boolean }) {
  const app = useApp();
  const { ui, tt, tp, L, setUi, closeSheet, content } = app;
  const eventsOn = useFeature('events');
  const set = (p: Partial<UiState>) => setUi(p);
  const any = activeFilters(cat, ui).length > 0;
  let body: ReactNode;
  if (cat === 'all') {
    const cur = isScope(ui.sk) ? ui.sk : 'all';
    body = (
      <div className="field"><span className="lbl">{tt('Қай бөлімнен іздеу', 'Где искать')}</span>
        <div className="chips wrap">
          {SCOPES.map(k => <Chip key={k} pressed={cur === k} onClick={() => set({ sk: k })}>{k === 'all' ? tt('Барлық бөлім', 'Все разделы') : tt(...SECTIONS[k].label)}</Chip>)}
        </div>
      </div>
    );
  } else if (cat === 'jobs') {
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
    body = <p className="small muted">{tt('Бұл бөлімде қосымша сүзгі жоқ. Түрін жоғарыдағы қойындылардан таңдаңыз.', 'В этом разделе дополнительных фильтров нет. Тип выберите во вкладках вверху.')}</p>;
  }
  const title = cat === 'all' ? tt('Іздеу', 'Поиск') : cat === 'market' ? tt('Тегін беремін', 'Отдам даром') : tt(...SECTIONS[cat].label);
  return (
    <>
      <h2>{tt('Сүзгілер', 'Фильтры')} · {title}</h2>
      {body}
      {panel ? (any ? <button className="btn ghost" type="button" onClick={() => setUi(clearAll(cat))}>{tt('Тазалау', 'Сбросить')}</button> : null) : (
        <div className="btn-row">
          {any ? <button className="btn ghost" type="button" onClick={() => setUi(clearAll(cat))}>{tt('Тазалау', 'Сбросить')}</button> : null}
          <button className="btn" type="button" onClick={closeSheet}>{tt('Көрсету', 'Показать')}</button>
        </div>
      )}
    </>
  );
}

/* ---------- result lists ---------- */
function NoResults({ icon, msg, cat }: { icon: 'search' | 'globe' | 'calendar' | 'book' | 'tag'; msg: string; cat: FilterCat }) {
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

/** Public events follow the server's demo filter; management APIs are used only by detail screens. */
function useEventItems(cat: string, tab = 'all') {
  const { ui, lang, content } = useApp();
  return (content.events ?? []).filter(e => (cat === 'all' || e.cat === cat) && eventInTab(e.cat, tab)
    && matches(ui.sq, evTitle(e, lang), evDesc(e, lang), e.place_text));
}

/** a category with nothing published at all (not a filter miss): say so and offer to be told when items appear */
function NothingYet({ icon, msg, notify }: { icon: 'briefcase' | 'globe' | 'calendar' | 'tag'; msg: string; notify?: [NotifyCat, [string, string]] }) {
  return (
    <div className="nothing-yet" data-empty="true">
      <Empty icon={icon} action={notify ? <NotifyToggle cat={notify[0]} label={notify[1]} /> : null}>{msg}</Empty>
    </div>
  );
}

function JobsResults({ tab }: { tab: string }) {
  const { ui, L, tt, content } = useApp();
  const f = ui.jf;
  let list = content.jobs.filter(j => jobTab(j) === tab && (!f.today || j.today) && ['noexp', 'daily', 'remote', 'near'].every(k => !f[k] || j.tags.includes(k))
    && (ui.city === 'all' || j.city === ui.city) && matches(ui.sq, L(j.title), L(j.org), L(j.desc), L(CITIES[j.city] ?? { kk: '', ru: '' })));
  if (ui.sortPay) list = list.slice().sort((a, b) => hourly(b) - hourly(a));
  if (!content.jobs.length) {
    return (
      <>
        <NothingYet icon="briefcase" msg={tt('Әзірге вакансия жоқ — жаңалары жарияланғанда хабарлаймыз.', 'Вакансий пока нет — сообщим, когда появятся новые.')}
          notify={['jobs', ['Жаңа вакансиялар туралы хабарлау', 'Сообщать о новых вакансиях']]} />
        <SuggestLink />
      </>
    );
  }
  const msg = tab === 'youth'
    ? tt('Жастар практикасы бойынша орын әзірге жоқ. Жаңалары осында шығады.', 'Мест по молодёжной практике пока нет. Новые появятся здесь.')
    : tt('Сәйкес вакансия жоқ. Сүзгіні азайтып көріңіз.', 'Подходящих вакансий нет. Попробуйте убрать фильтры.');
  return (
    <>
      <Count n={list.length} />
      {list.length ? <div className="group">{list.map(j => <JobRow key={j.id} j={j} heart />)}</div>
        : <NoResults icon="search" cat="jobs" msg={msg} />}
      <SuggestLink />
    </>
  );
}

/** the opportunities of one section sub-tab (lib/sections.ts oppPlace) */
function useOpps(section: 'intern' | 'events', tab: string) {
  const { ui, L, content } = useApp();
  return content.opportunities
    .filter(o => { const p = oppPlace(o); return p.section === section && (tab === 'all' || p.tab === tab) && matches(ui.sq, L(o.title), L(o.org), L(o.desc)); })
    .sort((a, b) => oppLeft(a) - oppLeft(b));
}

function OppResults({ tab }: { tab: string }) {
  const { tt } = useApp();
  const list = useOpps('intern', tab);
  const any = useOpps('intern', 'all').length > 0;
  if (!any) {
    return (
      <>
        <NothingYet icon="globe" msg={tt('Әзірге бағдарлама жоқ — жаңалары жарияланғанда хабарлаймыз.', 'Программ пока нет — сообщим, когда появятся новые.')}
          notify={['digest', ['Апталық жаңа мүмкіндіктер топтамасы', 'Подборка новых возможностей раз в неделю']]} />
        <SuggestLink />
      </>
    );
  }
  return (
    <>
      <Count n={list.length} />
      {list.length ? <div className="list">{list.map(o => <OppCard key={o.id} o={o} />)}</div>
        : <NoResults icon="globe" cat="intern" msg={tt('Бұл бөлімде бағдарлама әзірге жоқ.', 'В этом разделе программ пока нет.')} />}
      <SuggestLink />
    </>
  );
}

function ClubsPanel() {
  const { tt, go } = useApp();
  return (
    <div className="card">
      <span className="row-title">{tt('Университет клубтары', 'Клубы университета')}</span>
      <span className="row-sub">{tt('Клубтың беті, жаңалықтары мен іс-шаралары, қосылу өтінімі.', 'Страница клуба, новости и мероприятия, заявка на вступление.')}</span>
      <button className="btn" type="button" onClick={() => go('clubs')}>{tt('Клубтар тізімін ашу', 'Открыть список клубов')}</button>
    </div>
  );
}

function EventsResults({ tab }: { tab: string }) {
  const { ui, tt, go, lang, I } = useApp();
  const acc = useAccount();
  const items = useEventItems(ui.evCat, tab);
  // competitions stored as opportunities (subtype «competition») sit with the contests
  const contests = useOpps('events', tab === 'all' || tab === 'contests' ? 'all' : '-');
  if (tab === 'clubs') return <ClubsPanel />;
  const n = items.length + contests.length;
  return (
    <>
      {canCreateEvents(acc.user?.role) ? <button className="btn ghost block" type="button" onClick={() => go('organize')}><Icon n="calendar" s={18} /> {tt('Ұйымдастырушы беті', 'Страница организатора')}</button> : null}
      <Count n={n} />
      {items.length
        ? <div className="group">{items.map(e => (
          <div className="sr" key={e.id}>
            <Row go={'event:' + e.id} icon="calendar" title={evTitle(e, lang)} sub={`${I.fDate(new Date(e.starts_at))}${e.place_text ? ', ' + e.place_text : ''}`} />
            <span className="sr-end"><PinButton item={'event:' + e.id} /></span>
          </div>
        ))}</div> : null}
      {contests.length ? <div className="list">{contests.map(o => <OppCard key={o.id} o={o} />)}</div> : null}
      {n ? null : tab === 'all'
        ? <NothingYet icon="calendar" msg={tt('Әзірге іс-шара жоқ — жаңалары жарияланғанда хабарлаймыз.', 'Мероприятий пока нет — сообщим, когда появятся новые.')}
          notify={['digest', ['Апталық жаңа мүмкіндіктер топтамасы', 'Подборка новых возможностей раз в неделю']]} />
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
  const { L, tt, openSheet } = useApp();
  return (
    <div className="fav-card">
      <button className="card" type="button" onClick={() => openSheet(<DealSheet id={d.id} />)}>
        <span className="badges" style={{ alignSelf: 'flex-start' }}><span className="badge b-line">{L(DEAL_CATS[d.cat])}</span></span>
        <span className="deal-pct">{d.pct}</span>
        <span className="row-title">{L(d.name)}</span>
        <span className="row-sub">{L(d.what)}</span>
        <span className="row-sub card-detail card-detail-truncate" title={L(d.cond)}>{tt("Шарт: ", "Условие: ")}{L(d.cond)}</span>
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
      {!content.discounts.length ? <NothingYet icon="tag" msg={tt('Әзірге серіктес жеңілдігі жоқ. Келісімдер жасалғанда осында шығады.', 'Скидок партнёров пока нет. Появятся здесь, когда будут заключены соглашения.')} /> : <Count n={list.length} />}
      {!content.discounts.length ? null : list.length ? (
        <div className="deals">{list.map(d => <DealCard key={d.id} d={d} />)}</div>
      ) : <NoResults icon="tag" cat="deals" msg={tt('Жеңілдік табылмады.', 'Скидки не найдены.')} />}
    </>
  );
}

/** «Тегін беремін»: the market's own list; a query searches it by name */
function MarketResults() {
  const { ui } = useApp();
  return (
    <>
      <MarketSearchResults q={ui.sq} />
      <MarketGrid />
    </>
  );
}

function AllResults() {
  const { S, I, ui, tt, L, lang, content, monS } = useApp();
  const marketOn = useFeature('market');
  const evs = useEventItems('all');
  const scope: Scope = isScope(ui.sk) ? ui.sk : 'all';
  const base = searchItems(S, I, content);
  const evItems: SearchItem[] = evs.map(e => {
    const d = new Date(new Date(e.starts_at).getTime() + 5 * 3600_000);
    return {
      k: 'events', id: 'ev' + e.id, go: 'event:' + e.id, org: evTitle(e, lang), noFav: true, fav: false, demo: !!e.is_demo, badge: null,
      title: `${d.getUTCDate()} ${monS[d.getUTCMonth()]}${e.place_text ? ', ' + e.place_text : ''}`, meta: `${L(ALL_EVENT_CATS[e.cat] ?? ALL_EVENT_CATS.other)} · ${tt('Іс-шара', 'Событие')}`,
    };
  });
  const courseItems: SearchItem[] = COURSES.map(c => ({
    k: 'courses', id: c.id, act: 'course', org: L(c.title), title: `${c.platform}, ${weeksTxt(c.weeks, tt)}`, meta: tt('Курс', 'Курс'),
    badge: c.cert === 'paid' ? null : [tt('Тегін', 'Бесплатно'), 'new'], fav: false, demo: false, noFav: true,
  }));
  const deals = base.filter(it => it.k === 'deals');
  const list = [...base.filter(it => it.k !== 'deals'), ...evItems, ...courseItems, ...deals]
    .filter(it => (scope === 'all' || scopeOfKind(it.k) === scope) && matches(ui.sq, it.org, it.title, it.meta));
  return (
    <>
      {marketOn && (scope === 'all' || scope === 'deals') ? <MarketSearchResults q={ui.sq} /> : null}
      <Count n={list.length} />
      {list.length ? <div className="group">{list.map(it => <SearchRow key={it.k + it.id} it={it} />)}</div>
        : <NoResults icon="search" cat="all" msg={tt('Ештеңе табылмады. Басқа сөзбен іздеп көріңіз.', 'Ничего не нашлось. Попробуйте другое слово.')} />}
    </>
  );
}

/** short enough to show whole in the box at 320 px */
const PLACEHOLDER: Record<FilterCat, [string, string]> = {
  all: ['Жұмыс, грант, курс…', 'Работа, грант, курс…'],
  jobs: ['Вакансия, қала…', 'Вакансия, город…'],
  intern: ['Бағдарлама, ұйым…', 'Программа, вуз…'],
  events: ['Іс-шара, орын…', 'Событие, место…'],
  courses: ['Курс, платформа…', 'Курс, платформа…'],
  deals: ['Серіктес, жеңілдік…', 'Партнёр, скидка…'],
  market: ['Зат атауы…', 'Название вещи…'],
};

/** the search box with the filter button, and the removable chips of the active filters */
function SearchBox({ cat, top, panel }: { cat: FilterCat; top?: ReactNode; panel?: boolean }) {
  const app = useApp();
  const { ui, tt, setUi, openSheet } = app;
  const inp = useRef<HTMLInputElement>(null);
  const active = activeFilters(cat, ui);

  // focus (and the caret at the end) when the home search box sent the student here
  useEffect(() => {
    if (!ui.sqFocus) return;
    setUi({ sqFocus: false });
    const el = inp.current;
    if (!el) return;
    el.focus();
    try { el.setSelectionRange(el.value.length, el.value.length); } catch { /* type=search in some engines */ }
  }, [ui.sqFocus, setUi]);

  return (
    <>
      <div className="search-top">
        {top}
        <div className="search-bar" role="search">
          <div className="search"><Icon n="search" s={18} />
            <input ref={inp} className="input" id="sq" type="search" enterKeyHint="search" autoComplete="off" placeholder={tt(...PLACEHOLDER[cat])} value={ui.sq}
              onChange={e => setUi({ sq: e.target.value })} aria-label={tt('Іздеу', 'Поиск')} />
          </div>
          {panel ? null : <button className="icon-btn filter-btn" type="button" onClick={() => openSheet(<FilterSheet cat={cat} />)}
            aria-label={active.length ? tt(`Сүзгілер: ${active.length} қосулы`, `Фильтры: включено ${active.length}`) : tt('Сүзгілер', 'Фильтры')}>
            <Icon n="filter" s={22} />
            {active.length ? <span className="count">{active.length}</span> : null}
          </button>}
        </div>
      </div>
      {active.length ? (
        <div className="chips wrap active-filters" aria-label={tt('Қосулы сүзгілер', 'Включённые фильтры')}>
          {active.map(f => {
            const l = filterLabel(f.key, app);
            return (
              <button key={f.key} className="chip" type="button" aria-pressed="true" onClick={() => setUi(f.clear)} aria-label={tt(`Сүзгіні алып тастау: ${l}`, `Убрать фильтр: ${l}`)}>
                {l} <Icon n="x" s={14} />
              </button>
            );
          })}
          {active.length > 1 ? <button className="link-btn" type="button" onClick={() => setUi(clearAll(cat))}>{tt('Барлығын тазалау', 'Сбросить все')}</button> : null}
        </div>
      ) : null}
    </>
  );
}

/** «Іздеу»: everything at once; old /search?k=<category> links go on to the section that holds the category */
export function Search() {
  const { go, setUi } = useApp();
  useLayoutEffect(() => {
    let sp: URLSearchParams | null = null;
    try { sp = new URLSearchParams(window.location.search); } catch { /* ignore */ }
    const to = viewOfSearchCat(sp?.get('k'));
    const q = sp?.get('q');
    if (q) setUi({ sq: q.slice(0, 100) });
    if (to) go(to, { replace: true });
    else if (sp?.get('k') === 'all') setUi({ sk: 'all' });
  }, [go, setUi]);
  return (
    <Filtered cat="all">
      {panel => <>
        <SearchBox cat="all" panel={panel} />
        <div id="sList" className="section"><AllResults /></div>
      </>}
    </Filtered>
  );
}

/** a laptop shows the filters in a panel beside the list (lib/desktop.ts); a phone keeps the filter button and sheet */
function Filtered({ cat, children }: { cat: FilterCat; children: (panel: boolean) => ReactNode }) {
  const { tt } = useApp();
  const desktop = useDesktop();
  if (!desktop) return <>{children(false)}</>;
  // «Тағылымдама» and «Тегін беремін» have no filters beyond their tabs: no empty panel
  if (cat === 'intern' || cat === 'market') return <>{children(true)}</>;
  return (
    <div className="search-layout">
      <aside className="search-side" aria-label={tt('Сүзгілер', 'Фильтры')}><FilterSheet cat={cat} panel /></aside>
      <div className="search-main">{children(true)}</div>
    </div>
  );
}

/* ---------- a section with its sub-tabs ---------- */

/** an accessible tablist: arrow keys move between tabs (and select them), Home/End jump to the ends */
function SubTabs({ section, tabs, cur, onPick }: { section: SectionId; tabs: SectionTab[]; cur: string; onPick: (id: string) => void }) {
  const { tt } = useApp();
  const box = useRef<HTMLDivElement>(null);
  // the selected tab scrolls into view (a deep link to the last tab at 360 px)
  useEffect(() => {
    const el = box.current?.querySelector<HTMLElement>('[aria-selected="true"]');
    el?.scrollIntoView?.({ block: 'nearest', inline: 'nearest' });
  }, [cur]);
  const onKey = (e: KeyboardEvent<HTMLDivElement>) => {
    // count from the focused tab, not from `cur`: a second key press can arrive before the re-render
    const focused = (e.target as HTMLElement).id.replace(`st-${section}-`, '');
    const i = Math.max(0, tabs.findIndex(t => t.id === (tabs.some(x => x.id === focused) ? focused : cur)));
    let n = -1;
    if (e.key === 'ArrowRight') n = (i + 1) % tabs.length;
    else if (e.key === 'ArrowLeft') n = (i - 1 + tabs.length) % tabs.length;
    else if (e.key === 'Home') n = 0;
    else if (e.key === 'End') n = tabs.length - 1;
    if (n < 0) return;
    e.preventDefault();
    onPick(tabs[n].id);
    box.current?.querySelector<HTMLElement>(`#st-${section}-${tabs[n].id}`)?.focus();
  };
  return (
    <div className="subtabs" role="tablist" aria-label={tt(...SECTIONS[section].label)} ref={box} onKeyDown={onKey}>
      {tabs.map(t => {
        const on = t.id === cur;
        return (
          <button key={t.id} id={`st-${section}-${t.id}`} className="subtab" type="button" role="tab" aria-selected={on}
            aria-controls="sList" tabIndex={on ? 0 : -1} onClick={() => onPick(t.id)}>
            {tt(...t.label)}
          </button>
        );
      })}
    </div>
  );
}

function SectionBody({ section }: { section: SectionId }) {
  const { content } = useApp();
  const cfg = useConfig();
  const sp = useSearchParams();
  const clubs = !!(cfg.features as Partial<Record<string, boolean>>).clubs;
  const tabs = visibleTabs(section, { jobs: content.jobs, clubs, market: !!cfg.features.market });
  // The tab lives in state, updated at once on a click or key. useSearchParams catches up with
  // history.replaceState only after a deferred router update, so reading the tab straight from it lets a quick second key
  // see the previous tab. A URL change is adopted when it is the live URL: an outside navigation (menu, «back») or our
  // own write catching up; a stale in-between value (two quick keys) is skipped.
  const urlT = sp.get('t');
  const [st, setSt] = useState({ urlT, tab: urlT });
  if (st.urlT !== urlT) {
    const live = new URLSearchParams(window.location.search).get('t');
    setSt(live === urlT ? { urlT, tab: urlT } : { ...st, urlT });
  }
  const tab = pickTab(tabs, st.tab);
  const pick = (id: string) => {
    if (id === tab) return;
    setSt(s => ({ ...s, tab: id }));
    // the tab rides in the URL without a navigation: the page stays static, «back» from an item returns here
    try { window.history.replaceState(null, '', pathOf(sectionView(section, id))); } catch { /* ignore */ }
  };
  const fcat: FilterCat = section === 'deals' && tab === 'free' ? 'market' : section;

  let results: ReactNode;
  switch (section) {
    case 'jobs': results = <JobsResults tab={tab} />; break;
    case 'intern': results = <OppResults tab={tab} />; break;
    case 'events': results = <EventsResults tab={tab} />; break;
    case 'courses': results = <CoursesResults />; break;
    default: results = tab === 'free' ? <MarketResults /> : <DealsResults />;
  }
  const many = tabs.length > 1;
  return (
    <Filtered cat={fcat}>
      {panel => <>
        <SearchBox cat={fcat} panel={panel} top={many ? <SubTabs section={section} tabs={tabs} cur={tab} onPick={pick} /> : null} />
        <div id="sList" className="section" {...(many ? { role: 'tabpanel', 'aria-labelledby': `st-${section}-${tab}` } : {})}>{results}</div>
      </>}
    </Filtered>
  );
}

/** /jobs, /internships, /events, /courses, /discounts */
export function SectionScreen({ section }: { section: SectionId }) {
  return <Suspense fallback={null}><SectionBody section={section} /></Suspense>;
}
