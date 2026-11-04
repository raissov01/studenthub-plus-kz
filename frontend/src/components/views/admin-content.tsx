'use client';
/* /admin/content (docs/api/admin-content.md): opportunities, jobs and discounts an admin publishes from the site.
   The list (kind, status, search, counts, expired marker; edit / hide / unhide / delete with a typed confirmation) and
   the editor /admin/content/new?kind=… and /admin/content/adm-<n>: every field of the data/*.json record, the rules
   of the docs table on the client, the server's own check (POST /api/admin/content/validate, debounced) shown at the
   field its `path` names, and a preview drawn by the real card and page components. Admin only, like /admin. */
import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { useApp } from '@/components/app';
import { Chip, Empty, Icon, Seg } from '@/components/ui';
import { AdminOnly, ErrorLine, errText, Loading, NotFound, useLoad, ymd } from '@/components/views/common';
import { JobBody, OppBody } from '@/components/views/detail';
import { JobRow, OppCard } from '@/components/views/opps';
import { DealCard } from '@/components/views/search';
import { DealBody } from '@/components/views/sheets';
import { api, ApiError } from '@/lib/account';
import {
  bodyOf, check, CITY_CODES, DEAL_CAT_CODES, emptyForm, EN_LEVELS, formOfItem, isKind, JOB_PER, JOB_TAG_CODES, KINDS, MESSAGES,
  OPP_TYPE_CODES, previewOf, slotOf, SPEC_CODES, type Bi, type Errors, type Form, type Kind, type Msg,
} from '@/lib/admincontent';
import { CITIES, DEAL_CATS, JOB_TAGS, OPP_TYPES, SKILLS, SPECS } from '@/lib/data/static';
import type { Discount, Job, Opportunity } from '@/lib/types';

interface Item {
  id: string; kind: Kind; status: 'published' | 'hidden'; no_deadline: boolean; expired: boolean;
  record: Record<string, unknown>; created_at: string; updated_at: string;
}
type Counts = Record<Kind, { published: number; hidden: number }>;

const KIND_LABEL: Record<Kind, [string, string]> = {
  opportunities: ['Мүмкіндіктер', 'Возможности'], jobs: ['Жұмыстар', 'Работа'], discounts: ['Жеңілдіктер', 'Скидки'],
};
const kindOfUrl = (): Kind | null => {
  try { const k = new URLSearchParams(window.location.search).get('kind'); return isKind(k) ? k : null; } catch { return null; }
};

export function AdminContent({ prm }: { prm: string }) {
  const sub = prm.replace(/\?.*$/, '');
  let screen: ReactNode;
  if (sub === 'content') screen = <ContentList />;
  else if (sub === 'content/new') screen = <NewItem />;
  else if (/^content\/adm-\d+$/.test(sub)) screen = <EditItem id={sub.slice('content/'.length)} />;
  else screen = <NotFound />;
  return <AdminOnly>{screen}</AdminOnly>;
}

/* ---------- list ---------- */
function ContentList() {
  const { tt, L, lang, go, toast, openSheet } = useApp();
  const [kind, setKind] = useState<Kind>(() => kindOfUrl() ?? 'opportunities');
  const [status, setStatus] = useState('all');
  const [q, setQ] = useState('');
  const [dq, setDq] = useState('');
  useEffect(() => { const id = setTimeout(() => setDq(q.trim()), 300); return () => clearTimeout(id); }, [q]);
  const list = useLoad<{ items: Item[]; counts: Counts }>(`/api/admin/content?kind=${kind}&status=${status}&limit=200${dq ? '&q=' + encodeURIComponent(dq) : ''}`);
  const [busy, setBusy] = useState('');
  const pickKind = (k: string) => {
    if (!isKind(k)) return;
    setKind(k);
    try { window.history.replaceState(window.history.state, '', '/admin/content?kind=' + k); } catch { /* ignore */ }
  };
  const counts = list.data?.counts;
  const act = async (it: Item, verb: 'hide' | 'unhide') => {
    setBusy(it.id);
    try {
      await api(`/api/admin/content/${it.id}/${verb}`, { method: 'POST', lang });
      toast(verb === 'hide' ? tt('Жасырылды. Сайттан 10 минут ішінде кетеді', 'Скрыто. Уйдёт с сайта в течение 10 минут')
        : tt('Қайта көрсетілді. Сайтта 10 минут ішінде шығады', 'Снова показано. Появится на сайте в течение 10 минут'));
      list.reload();
    } catch (e) { toast(errText(e)); } finally { setBusy(''); }
  };
  return (
    <>
      <div className="chips wrap" role="group" aria-label={tt('Түрі', 'Тип')}>
        {KINDS.map(k => (
          <Chip key={k} pressed={kind === k} onClick={() => pickKind(k)}>
            {tt(...KIND_LABEL[k])}{counts ? <span className="num ac-count">{counts[k].published} · {counts[k].hidden}</span> : null}
          </Chip>
        ))}
      </div>
      <p className="tiny muted">{tt('Сандар: жарияланған · жасырылған', 'Числа: опубликовано · скрыто')}</p>
      <div className="chips wrap" role="group" aria-label={tt('Күйі', 'Статус')}>
        {([['all', tt('Барлығы', 'Все')], ['published', tt('Жарияланған', 'Опубликованные')], ['hidden', tt('Жасырылған', 'Скрытые')]] as [string, string][])
          .map(([k, l]) => <Chip key={k} pressed={status === k} onClick={() => setStatus(k)}>{l}</Chip>)}
      </div>
      <div className="search" role="search"><Icon n="search" s={18} />
        <input className="input" type="search" value={q} onChange={e => setQ(e.target.value)} maxLength={100}
          placeholder={tt('Атауы не ұйым…', 'Название или организация…')} aria-label={tt('Жазбаны іздеу', 'Поиск записи')} />
      </div>
      <button className="btn block" type="button" onClick={() => go('admin:content/new?kind=' + kind)}><Icon n="plus" s={18} /> {tt('Қосу', 'Добавить')}</button>
      <ErrorLine err={list.err} />
      {!list.data ? (list.err ? null : <Loading />) : list.data.items.length ? (
        <div className="list">{list.data.items.map(it => {
          const r = it.record;
          const title = L((it.kind === 'discounts' ? r.name : r.title) as { kk: string; ru: string } ?? { kk: '', ru: '' }) || it.id;
          const city = typeof r.city === 'string' && CITIES[r.city] ? L(CITIES[r.city]) : '';
          const dl = typeof r.deadline === 'string' && r.deadline ? r.deadline : '';
          return (
            <div key={it.id} className="card ac-item">
              <div className="badges">
                <span className={`badge ${it.status === 'published' ? 'b-ok' : 'b-line'}`}>{it.status === 'published' ? tt('Жарияланған', 'Опубликовано') : tt('Жасырылған', 'Скрыто')}</span>
                {it.expired ? <span className="badge b-bad">{tt('Мерзімі өтті', 'Срок истёк')}</span> : null}
                <span className="badge b-line num">{it.id}</span>
              </div>
              <b className="ac-title">{title}</b>
              <span className="small muted">{[city, dl ? tt(`Мерзімі: ${dl}`, `Срок: ${dl}`) : tt('Мерзімсіз', 'Без срока')].filter(Boolean).join(' · ')}</span>
              <span className="tiny muted">{tt('Жаңартылды', 'Обновлено')}: {ymd(it.updated_at)}</span>
              <div className="btn-row">
                <button className="btn ghost" type="button" onClick={() => go('admin:content/' + it.id)}><Icon n="edit" s={18} /> {tt('Өңдеу', 'Изменить')}</button>
                {it.status === 'published'
                  ? <button className="btn secondary" type="button" disabled={busy === it.id} onClick={() => act(it, 'hide')}>{tt('Жасыру', 'Скрыть')}</button>
                  : <button className="btn secondary" type="button" disabled={busy === it.id} onClick={() => act(it, 'unhide')}>{tt('Қайта көрсету', 'Показать')}</button>}
                <button className="btn danger" type="button" onClick={() => openSheet(<DeleteSheet it={it} title={title} onDone={list.reload} />)}><Icon n="trash" s={18} /> {tt('Өшіру', 'Удалить')}</button>
              </div>
            </div>
          );
        })}</div>
      ) : <Empty icon="file">{dq || status !== 'all' ? tt('Ештеңе табылмады', 'Ничего не найдено') : tt('Әзірге жазба жоқ', 'Пока нет записей')}</Empty>}
    </>
  );
}

/** «Біржола өшіресіз бе?» — the admin types the record's id to confirm */
function DeleteSheet({ it, title, onDone }: { it: Item; title: string; onDone: () => void }) {
  const { tt, lang, toast, closeSheet } = useApp();
  const [typed, setTyped] = useState('');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  const match = typed.trim() === it.id;
  return (
    <form className="form" onSubmit={async e => {
      e.preventDefault();
      if (!match || busy) return;
      setBusy(true); setErr('');
      try { await api(`/api/admin/content/${it.id}`, { method: 'DELETE', lang }); closeSheet(); toast(tt('Өшірілді', 'Удалено')); onDone(); }
      catch (e2) { setErr(errText(e2)); } finally { setBusy(false); }
    }}>
      <h2>{tt('Біржола өшіресіз бе?', 'Удалить навсегда?')}</h2>
      <p className="small ac-title">{title}</p>
      <p className="small muted">{tt('Жазба сайттан да, тізімнен де кетеді. Көшірмесі тек журналда қалады.', 'Запись исчезнет и с сайта, и из списка. Копия останется только в журнале.')}</p>
      <div className="field"><label htmlFor="acDel">{tt(`Растау үшін жазыңыз: ${it.id}`, `Для подтверждения введите: ${it.id}`)}</label>
        <input className="input" id="acDel" autoComplete="off" autoCapitalize="off" spellCheck={false} value={typed} onChange={e => setTyped(e.target.value)} /></div>
      <ErrorLine err={err} />
      <div className="btn-row">
        <button className="btn ghost" type="button" onClick={closeSheet}>{tt('Бас тарту', 'Отмена')}</button>
        <button className="btn danger" type="submit" disabled={!match || busy}>{tt('Өшіру', 'Удалить')}</button>
      </div>
    </form>
  );
}

/* ---------- editor ---------- */
function NewItem() {
  const [kind] = useState<Kind>(() => kindOfUrl() ?? 'opportunities');
  return <Editor initial={emptyForm(kind)} />;
}

function EditItem({ id }: { id: string }) {
  const r = useLoad<{ item: Item }>(`/api/admin/content/${id}`);
  if (r.code === 'not_found') return <NotFound />;
  if (r.err) return <ErrorLine err={r.err} />;
  if (!r.data) return <Loading />;
  const it = r.data.item;
  return <Editor key={it.id + it.updated_at} id={it.id} expired={it.expired} initial={formOfItem(it.kind, it.status, it.no_deadline, it.record)} />;
}

interface ServerCheck { key: string; ok: boolean; path: string; message: string }

/** the server's check of the current body: debounced, at most one call every ~2 s (writes are rate limited) */
function useServerCheck(key: string, body: unknown, enabled: boolean): [ServerCheck | null, (c: ServerCheck) => void] {
  const { lang } = useApp();
  const [res, setRes] = useState<ServerCheck | null>(null);
  const last = useRef(0);
  const bodyRef = useRef(body);
  useEffect(() => { bodyRef.current = body; }, [body]);
  useEffect(() => {
    if (!enabled || res?.key === key) return;
    const wait = Math.max(700, 2100 - (Date.now() - last.current));
    let live = true;
    const id = setTimeout(() => {
      last.current = Date.now();
      api('/api/admin/content/validate', { method: 'POST', body: bodyRef.current, lang }).then(
        () => { if (live) setRes({ key, ok: true, path: '', message: '' }); },
        e => {
          if (!live) return;
          if (e instanceof ApiError && e.code === 'invalid_record') setRes({ key, ok: false, path: String(e.detail.path ?? ''), message: e.message });
          // rate limit or network: no verdict, the save runs the check again
        },
      );
    }, wait);
    return () => { live = false; clearTimeout(id); };
  }, [key, enabled, lang, res?.key]);
  return [res, setRes];
}

/** slots the form draws; a server path outside them is shown above the buttons */
const SLOTS = new Set(['title.kk', 'title.ru', 'org.kk', 'org.ru', 'desc.kk', 'desc.ru', 'sched.kk', 'sched.ru', 'what.kk', 'what.ru', 'cond.kk', 'cond.ru',
  'type', 'cat', 'pct', 'city', 'source_url', 'deadline', 'posted_at', 'verified_at', 'verified_by', 'contact.phone', 'contact.email', 'contact.site',
  'req.course', 'req.gpa', 'req.en', 'docs[]', 'steps[]', 'pay', 'per', 'hours', 'req[]']);

function Editor({ initial, id, expired }: { initial: Form; id?: string; expired?: boolean }) {
  const { tt, lang, go, toast } = useApp();
  const [f, setF] = useState<Form>(initial);
  const [dirty, setDirty] = useState(false);
  const [touched, setTouched] = useState<Record<string, boolean>>({});
  const [tried, setTried] = useState(false);
  const [tab, setTab] = useState('form');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  const kind = f.kind;
  const set = (p: Partial<Form>) => { setF(x => ({ ...x, ...p })); setDirty(true); };
  const errs: Errors = useMemo(() => check(f), [f]);
  const body = useMemo(() => bodyOf(f), [f]);
  const key = JSON.stringify(body);
  const [server, setServer] = useServerCheck(key, body, dirty);
  const srv = server && server.key === key && !server.ok ? server : null;
  const srvSlot = srv ? slotOf(srv.path) : '';

  /** what a slot shows: the form's own rule once the field was left (or a save was tried), else the server's verdict */
  const show = (slot: string): { msg: Msg | null; server: string } => {
    const mine = (touched[slot] || tried) ? errs[slot] ?? null : null;
    if (srv && srvSlot === slot) return { msg: mine ?? MESSAGES[srv.path] ?? MESSAGES[slot] ?? null, server: srv.message };
    return { msg: mine, server: '' };
  };
  const touch = (slot: string) => setTouched(t => (t[slot] ? t : { ...t, [slot]: true }));
  const fp: FieldCtx = { show, touch };

  const save = async (status: 'published' | 'hidden') => {
    setTried(true); setErr('');
    if (Object.keys(errs).length) {
      toast(tt('Белгіленген өрістерді түзетіңіз', 'Исправьте отмеченные поля'));
      setTab('form');
      setTimeout(() => document.querySelector('.field-err')?.closest('.field, fieldset')?.scrollIntoView({ block: 'center' }), 50);
      return;
    }
    setBusy(true);
    const b = bodyOf(f, status);
    try {
      await api('/api/admin/content/validate', { method: 'POST', body: b, lang });
      if (id) await api(`/api/admin/content/${id}`, { method: 'PUT', body: b, lang });
      else await api('/api/admin/content', { method: 'POST', body: b, lang });
      toast(status === 'hidden' ? tt('Жасырын сақталды', 'Сохранено скрытым') : tt('Сақталды. Сайтта 10 минут ішінде шығады', 'Сохранено. Появится на сайте в течение 10 минут'));
      go('admin:content?kind=' + kind, { replace: true });
    } catch (e) {
      if (e instanceof ApiError && e.code === 'invalid_record') {
        setServer({ key, ok: false, path: String(e.detail.path ?? ''), message: e.message });
        setTab('form');
        setTimeout(() => document.querySelector('.field-err')?.closest('.field, fieldset')?.scrollIntoView({ block: 'center' }), 50);
      } else setErr(errText(e));
    } finally { setBusy(false); }
  };

  const otherErr = srv && !SLOTS.has(srvSlot) ? srv : null;
  const nErr = Object.keys(errs).length;
  return (
    <>
      <div className="card">
        <span className="eyebrow">{tt(...KIND_LABEL[kind])}{id ? ` · ${id}` : ''}</span>
        <div className="badges">
          {id ? <span className={`badge ${initial.status === 'published' ? 'b-ok' : 'b-line'}`}>{initial.status === 'published' ? tt('Жарияланған', 'Опубликовано') : tt('Жасырылған', 'Скрыто')}</span> : <span className="badge b-line">{tt('Жаңа жазба', 'Новая запись')}</span>}
          {expired ? <span className="badge b-bad">{tt('Мерзімі өтті', 'Срок истёк')}</span> : null}
        </div>
        <span className="tiny muted" role="status">{!dirty ? tt('Өрістерді толтырыңыз. Тексеру жазған сайын жүреді.', 'Заполните поля. Проверка идёт по ходу ввода.')
          : nErr ? tt(`Түзететін өріс: ${nErr}`, `Полей с ошибками: ${nErr}`)
            : srv ? tt('Сервер қате тапты — белгіленген өрісті қараңыз', 'Сервер нашёл ошибку — смотрите отмеченное поле')
              : server?.key === key && server.ok ? tt('Сервер тексерді: бәрі дұрыс', 'Сервер проверил: всё верно') : tt('Тексеріліп жатыр…', 'Проверяется…')}</span>
      </div>
      <Seg items={[['form', tt('Форма', 'Форма')], ['preview', tt('Алдын ала қарау', 'Предпросмотр')]]} cur={tab} onPick={setTab} />
      {tab === 'preview' ? <Preview f={f} /> : (
        <form className="form ac-form" noValidate onSubmit={e => { e.preventDefault(); save('published'); }}>
          <section className="section">
            <h2>{tt('Негізгі', 'Основное')}</h2>
            {kind === 'opportunities' ? (
              <Sel ctx={fp} slot="type" label={tt('Түрі *', 'Тип *')} value={f.type} onChange={v => set({ type: v })}
                options={OPP_TYPE_CODES.map(k => [k, OPP_TYPES[k]] as [string, [string, string]])} />
            ) : null}
            {kind === 'discounts' ? (
              <Sel ctx={fp} slot="cat" label={tt('Санаты *', 'Категория *')} value={f.cat} onChange={v => set({ cat: v })}
                options={DEAL_CAT_CODES.map(k => [k, [DEAL_CATS[k].kk, DEAL_CATS[k].ru]] as [string, [string, string]])} />
            ) : null}
            <BiField ctx={fp} name="title" label={kind === 'discounts' ? tt('Серіктес атауы *', 'Название партнёра *') : tt('Атауы *', 'Название *')} value={f.title} onChange={v => set({ title: v })} max={200} />
            {kind !== 'discounts' ? <BiField ctx={fp} name="org" label={kind === 'jobs' ? tt('Жұмыс беруші', 'Работодатель') : tt('Ұйым', 'Организация')} value={f.org} onChange={v => set({ org: v })} max={200} /> : null}
            {kind === 'discounts' ? <Text ctx={fp} slot="pct" label={tt('Жеңілдік мөлшері * (−15%, 1 500 ₸)', 'Размер скидки * (−15%, 1 500 ₸)')} value={f.pct} onChange={v => set({ pct: v })} max={40} /> : null}
            {kind === 'opportunities' ? <Check label={tt('Қаржыландырылады', 'Финансируется')} value={f.funded} onChange={v => set({ funded: v })} /> : null}
            {kind === 'jobs' ? <Check label={tt('Бүгінгі жұмыс', 'Работа на сегодня')} value={f.today} onChange={v => set({ today: v })} /> : null}
          </section>

          {kind === 'jobs' ? (
            <section className="section">
              <h2>{tt('Төлем және кесте', 'Оплата и график')}</h2>
              <div className="grid-2">
                <Text ctx={fp} slot="pay" label={tt('Төлем, ₸ *', 'Оплата, ₸ *')} value={f.pay} onChange={v => set({ pay: v })} inputMode="decimal" max={12} />
                <Sel ctx={fp} slot="per" label={tt('Кезеңі *', 'Период *')} value={f.per} onChange={v => set({ per: v })}
                  options={JOB_PER.map(k => [k, PER_LABEL[k]] as [string, [string, string]])} />
              </div>
              <Text ctx={fp} slot="hours" label={f.per === 'shift' ? tt('Ауысым сағаты *', 'Часов в смене *') : tt('Сағат (ауысымда не күніне)', 'Часов (в смену или в день)')}
                value={f.hours} onChange={v => set({ hours: v })} inputMode="decimal" max={6} />
              <BiField ctx={fp} name="sched" label={tt('Кесте', 'График')} value={f.sched} onChange={v => set({ sched: v })} max={200} />
              <Codes label={tt('Белгілер', 'Метки')} codes={JOB_TAG_CODES} labels={JOB_TAGS} value={f.tags} onChange={v => set({ tags: v })} />
            </section>
          ) : null}

          <section className="section">
            <h2>{tt('Сипаттама', 'Описание')}</h2>
            {kind === 'discounts' ? (
              <>
                <BiField ctx={fp} name="what" label={tt('Не беріледі', 'Что даёт')} value={f.what} onChange={v => set({ what: v })} max={500} area />
                <BiField ctx={fp} name="cond" label={tt('Шарттары', 'Условия')} value={f.cond} onChange={v => set({ cond: v })} max={500} area />
              </>
            ) : <BiField ctx={fp} name="desc" label={kind === 'jobs' ? tt('Не істеу керек', 'Что нужно делать') : tt('Сипаттама', 'Описание')} value={f.desc} onChange={v => set({ desc: v })} max={4000} area />}
          </section>

          {kind === 'opportunities' ? (
            <section className="section">
              <h2>{tt('Талаптар', 'Требования')}</h2>
              <div className="grid-2">
                <Text ctx={fp} slot="req.course" label={tt('Курс (1–7)', 'Курс (1–7)')} value={f.course} onChange={v => set({ course: v })} inputMode="numeric" max={2} />
                <Text ctx={fp} slot="req.gpa" label={tt('GPA (0–4)', 'GPA (0–4)')} value={f.gpa} onChange={v => set({ gpa: v })} inputMode="decimal" max={5} />
              </div>
              <Sel ctx={fp} slot="req.en" label={tt('Ағылшын тілі', 'Английский')} value={f.en} onChange={v => set({ en: v })} empty={tt('Талап жоқ', 'Не требуется')}
                options={EN_LEVELS.map(k => [k, [k, k]] as [string, [string, string]])} />
              <Codes label={tt('Дағдылар', 'Навыки')} codes={Object.keys(SKILLS)} labels={SKILLS} value={f.reqSkills} onChange={v => set({ reqSkills: v })} />
              <Codes label={tt('Мамандықтар (бос — барлығына)', 'Специальности (пусто — для всех)')} codes={SPEC_CODES.filter(k => k !== 'all')} labels={SPECS} value={f.spec} onChange={v => set({ spec: v })} />
            </section>
          ) : null}
          {kind === 'jobs' ? (
            <section className="section">
              <h2>{tt('Талаптар', 'Требования')}</h2>
              <Codes label={tt('Дағдылар', 'Навыки')} codes={Object.keys(SKILLS)} labels={SKILLS} value={f.skills} onChange={v => set({ skills: v })} />
              <Codes label={tt('Мамандықтар (бос — барлығына)', 'Специальности (пусто — для всех)')} codes={SPEC_CODES.filter(k => k !== 'all')} labels={SPECS} value={f.spec} onChange={v => set({ spec: v })} />
              <Rows ctx={fp} name="req" label={tt('Талаптар тізімі', 'Список требований')} add={tt('Талап қосу', 'Добавить требование')} value={f.req} onChange={v => set({ req: v })} />
            </section>
          ) : null}
          {kind === 'opportunities' ? (
            <section className="section">
              <h2>{tt('Құжаттар мен қадамдар', 'Документы и шаги')}</h2>
              <Rows ctx={fp} name="docs" label={tt('Құжаттар', 'Документы')} add={tt('Құжат қосу', 'Добавить документ')} value={f.docs} onChange={v => set({ docs: v })} />
              <Rows ctx={fp} name="steps" label={tt('Қалай рәсімдеу керек (қадамдар)', 'Как оформить (шаги)')} add={tt('Қадам қосу', 'Добавить шаг')} value={f.steps} onChange={v => set({ steps: v })} />
            </section>
          ) : null}

          <section className="section">
            <h2>{tt('Мерзім және қала', 'Срок и город')}</h2>
            <Sel ctx={fp} slot="city" label={tt('Қала *', 'Город *')} value={f.city} onChange={v => set({ city: v })}
              options={CITY_CODES.map(k => [k, [CITIES[k].kk, CITIES[k].ru]] as [string, [string, string]])} />
            <Field ctx={fp} slot="deadline" id="ac_deadline" label={tt('Соңғы күн *', 'Дедлайн *')}>
              {a => (
                <>
                  <input className="input" id="ac_deadline" type="date" value={f.deadline} disabled={f.noDeadline} {...a} onChange={e => set({ deadline: e.target.value })} />
                  <label className="check"><input type="checkbox" checked={f.noDeadline} onChange={e => { set({ noDeadline: e.target.checked, deadline: e.target.checked ? '' : f.deadline }); touch('deadline'); }} />
                    <span className="small">{tt('Мерзімсіз', 'Без срока')}</span></label>
                </>
              )}
            </Field>
          </section>

          <section className="section">
            <h2>{tt('Дереккөз және тексеру', 'Источник и проверка')}</h2>
            <Text ctx={fp} slot="source_url" label={tt('Ресми дереккөз сілтемесі * (https://)', 'Ссылка на официальный источник * (https://)')} value={f.source_url} onChange={v => set({ source_url: v })} type="url" max={500} placeholder="https://" />
            <div className="grid-2">
              <Text ctx={fp} slot="posted_at" label={tt('Жарияланған күні', 'Дата публикации')} value={f.posted_at} onChange={v => set({ posted_at: v })} type="date" />
              <Text ctx={fp} slot="verified_at" label={tt('Тексерілген күні', 'Дата проверки')} value={f.verified_at} onChange={v => set({ verified_at: v })} type="date" />
            </div>
            <p className="tiny muted">{tt('Жарияланған күні бос болса — бүгінгі күн қойылады.', 'Если дата публикации пустая — ставится сегодняшняя.')}</p>
            <Text ctx={fp} slot="verified_by" label={tt('Кім тексерді', 'Кто проверил')} value={f.verified_by} onChange={v => set({ verified_by: v })} max={120} />
            <h3 className="ac-sub">{tt('Байланыс', 'Контакты')}</h3>
            <Text ctx={fp} slot="contact.phone" label={tt('Телефон', 'Телефон')} value={f.phone} onChange={v => set({ phone: v })} type="tel" max={40} />
            <Text ctx={fp} slot="contact.email" label="Email" value={f.email} onChange={v => set({ email: v })} type="email" max={120} />
            <Text ctx={fp} slot="contact.site" label={tt('Сайт (https://)', 'Сайт (https://)')} value={f.site} onChange={v => set({ site: v })} type="url" max={500} placeholder="https://" />
          </section>

          {otherErr ? <p className="small urgent" role="alert">{otherErr.path === 'list' ? tt(...MESSAGES.list) : otherErr.message}{otherErr.path === 'list' ? <span className="tiny muted ac-srv">{otherErr.message}</span> : null}</p> : null}
          <ErrorLine err={err} />
          <div className="btn-row">
            <button className="btn" type="submit" disabled={busy}>{tt('Жариялау', 'Опубликовать')}</button>
            <button className="btn secondary" type="button" disabled={busy} onClick={() => save('hidden')}>{tt('Жасырын сақтау', 'Сохранить скрытым')}</button>
          </div>
          <button className="btn ghost block" type="button" onClick={() => go('admin:content?kind=' + kind, { replace: true })}>{tt('Бас тарту', 'Отмена')}</button>
        </form>
      )}
    </>
  );
}

const PER_LABEL: Record<string, [string, string]> = { shift: ['Ауысымға', 'За смену'], month: ['Айына', 'В месяц'], hour: ['Сағатына', 'В час'] };

/* ---------- form pieces ---------- */
interface FieldCtx { show: (slot: string) => { msg: Msg | null; server: string }; touch: (slot: string) => void }
type A11y = { 'aria-invalid'?: true; 'aria-describedby'?: string };

function ErrMsg({ id, msg, server }: { id: string; msg: Msg | null; server: string }) {
  const { tt } = useApp();
  if (!msg && !server) return null;
  return (
    <span className="field-err" id={id} role="alert">
      {msg ? tt(...msg) : null}
      {server ? <span className="tiny muted ac-srv">{tt('Сервер', 'Сервер')}: {server}</span> : null}
    </span>
  );
}

/** label + control + the slot's message; `children` gets the aria attributes for its control */
function Field({ ctx, slot, id, label, children }: { ctx: FieldCtx; slot: string; id: string; label: string; children: (a: A11y) => ReactNode }) {
  const { msg, server } = ctx.show(slot);
  const bad = !!(msg || server);
  const eid = id + '_err';
  return (
    <div className="field" onBlur={() => ctx.touch(slot)}>
      <label htmlFor={id}>{label}</label>
      {children(bad ? { 'aria-invalid': true, 'aria-describedby': eid } : {})}
      <ErrMsg id={eid} msg={msg} server={server} />
    </div>
  );
}

const idOf = (slot: string) => 'ac_' + slot.replace(/[^a-z]+/gi, '_');

function Text({ ctx, slot, label, value, onChange, type = 'text', max, inputMode, placeholder }: {
  ctx: FieldCtx; slot: string; label: string; value: string; onChange: (v: string) => void; type?: string; max?: number;
  inputMode?: 'numeric' | 'decimal'; placeholder?: string;
}) {
  const id = idOf(slot);
  return (
    <Field ctx={ctx} slot={slot} id={id} label={label}>
      {a => <input className="input" id={id} type={type} value={value} maxLength={max} inputMode={inputMode} placeholder={placeholder} {...a} onChange={e => onChange(e.target.value)} />}
    </Field>
  );
}

function Sel({ ctx, slot, label, value, onChange, options, empty }: {
  ctx: FieldCtx; slot: string; label: string; value: string; onChange: (v: string) => void; options: [string, [string, string]][]; empty?: string;
}) {
  const { tt, tp } = useApp();
  const id = idOf(slot);
  return (
    <Field ctx={ctx} slot={slot} id={id} label={label}>
      {a => (
        <select className="input" id={id} value={value} {...a} onChange={e => { onChange(e.target.value); ctx.touch(slot); }}>
          <option value="">{empty ?? tt('— таңдаңыз —', '— выберите —')}</option>
          {options.map(([k, l]) => <option key={k} value={k}>{tp(l)}</option>)}
        </select>
      )}
    </Field>
  );
}

/** a kk/ru pair: side by side where there is room (one column on narrow phones), stacked for long texts */
function BiField({ ctx, name, label, value, onChange, max, area }: { ctx: FieldCtx; name: string; label: string; value: Bi; onChange: (v: Bi) => void; max: number; area?: boolean }) {
  const { tt } = useApp();
  const kk = ctx.show(name + '.kk'), ru = ctx.show(name + '.ru');
  const ids = { kk: idOf(name + '_kk'), ru: idOf(name + '_ru') };
  const ctl = (lang: 'kk' | 'ru', s: { msg: Msg | null; server: string }) => {
    const bad = !!(s.msg || s.server);
    const p = { className: 'input', id: ids[lang], value: value[lang], maxLength: max, 'aria-invalid': bad || undefined, 'aria-describedby': bad ? ids[lang] + '_err' : undefined,
      onChange: (e: { target: { value: string } }) => onChange({ ...value, [lang]: e.target.value }) };
    return (
      <div className="field" onBlur={() => ctx.touch(`${name}.${lang}`)}>
        <label htmlFor={ids[lang]} className="ac-lang">{lang === 'kk' ? tt('Қазақша', 'На казахском') : tt('Орысша (бос болса — қазақшасы)', 'На русском (пусто — как на казахском)')}</label>
        {area ? <textarea {...p} /> : <input {...p} />}
        <ErrMsg id={ids[lang] + '_err'} msg={s.msg} server={s.server} />
      </div>
    );
  };
  return (
    <fieldset className="field ac-bi">
      <legend className="lbl">{label}</legend>
      <div className={area ? 'ac-stack' : 'grid-2'}>{ctl('kk', kk)}{ctl('ru', ru)}</div>
    </fieldset>
  );
}

function Check({ label, value, onChange }: { label: string; value: boolean; onChange: (v: boolean) => void }) {
  return <label className="check"><input type="checkbox" checked={value} onChange={e => onChange(e.target.checked)} /><span className="small">{label}</span></label>;
}

/** multi-select of codes as chips */
function Codes({ label, codes, labels, value, onChange }: {
  label: string; codes: string[]; labels: Record<string, { kk: string; ru: string } | [string, string]>; value: string[]; onChange: (v: string[]) => void;
}) {
  const { tt } = useApp();
  const name = (k: string) => { const l = labels[k]; return !l ? k : Array.isArray(l) ? tt(l[0], l[1]) : tt(l.kk, l.ru); };
  return (
    <fieldset className="field ac-bi">
      <legend className="lbl">{label}</legend>
      <div className="chips wrap">{codes.map(k => (
        <Chip key={k} pressed={value.includes(k)} onClick={() => onChange(value.includes(k) ? value.filter(x => x !== k) : [...value, k])}>{name(k)}</Chip>
      ))}</div>
    </fieldset>
  );
}

/** a list of kk/ru rows (docs, steps, job requirements): add, remove, move */
function Rows({ ctx, name, label, add, value, onChange }: { ctx: FieldCtx; name: 'docs' | 'steps' | 'req'; label: string; add: string; value: Bi[]; onChange: (v: Bi[]) => void }) {
  const { tt } = useApp();
  const slot = name + '[]';
  const { msg, server } = ctx.show(slot);
  const eid = idOf(slot) + '_err';
  const move = (i: number, d: -1 | 1) => { const n = value.slice(); [n[i], n[i + d]] = [n[i + d], n[i]]; onChange(n); };
  return (
    <fieldset className="field ac-bi" onBlur={() => ctx.touch(slot)}>
      <legend className="lbl">{label}</legend>
      {value.length ? <ol className="ac-rows">{value.map((r, i) => (
        <li key={i} className="ac-row">
          <span className="ac-n num" aria-hidden="true">{i + 1}</span>
          <div className="ac-row-in">
            <div className="field"><label htmlFor={`${idOf(slot)}${i}_kk`} className="ac-lang">{tt('Қазақша', 'На казахском')}</label>
              <input className="input" id={`${idOf(slot)}${i}_kk`} value={r.kk} maxLength={300} aria-invalid={msg && !r.kk.trim() && r.ru.trim() ? true : undefined}
                onChange={e => onChange(value.map((x, j) => (j === i ? { ...x, kk: e.target.value } : x)))} /></div>
            <div className="field"><label htmlFor={`${idOf(slot)}${i}_ru`} className="ac-lang">{tt('Орысша', 'На русском')}</label>
              <input className="input" id={`${idOf(slot)}${i}_ru`} value={r.ru} maxLength={300}
                onChange={e => onChange(value.map((x, j) => (j === i ? { ...x, ru: e.target.value } : x)))} /></div>
          </div>
          <div className="ac-row-act">
            <button className="icon-btn" type="button" disabled={i === 0} onClick={() => move(i, -1)} aria-label={tt(`${i + 1}-жолды жоғары`, `Строку ${i + 1} выше`)}><Icon n="chev" s={18} className="ac-up" /></button>
            <button className="icon-btn" type="button" disabled={i === value.length - 1} onClick={() => move(i, 1)} aria-label={tt(`${i + 1}-жолды төмен`, `Строку ${i + 1} ниже`)}><Icon n="chev" s={18} className="ac-down" /></button>
            <button className="icon-btn" type="button" onClick={() => onChange(value.filter((_, j) => j !== i))} aria-label={tt(`${i + 1}-жолды өшіру`, `Удалить строку ${i + 1}`)}><Icon n="trash" s={18} /></button>
          </div>
        </li>
      ))}</ol> : <p className="tiny muted">{tt('Әзірге бос', 'Пока пусто')}</p>}
      <ErrMsg id={eid} msg={msg} server={server} />
      <button className="btn ghost" type="button" onClick={() => onChange([...value, { kk: '', ru: '' }])}><Icon n="plus" s={18} /> {add}</button>
    </fieldset>
  );
}

/* ---------- preview: the real list card and page ---------- */
function Preview({ f }: { f: Form }) {
  const { tt } = useApp();
  const rec = previewOf(f);
  let card: ReactNode, page: ReactNode;
  if (f.kind === 'opportunities') { card = <OppCard o={rec as Opportunity} />; page = <OppBody o={rec as Opportunity} />; }
  else if (f.kind === 'jobs') { card = <div className="group"><JobRow j={rec as Job} heart /></div>; page = <JobBody j={rec as Job} />; }
  else { card = <div className="deals"><DealCard d={rec as Discount} /></div>; page = <DealBody d={rec as Discount} />; }
  return (
    <>
      <p className="note">{tt('Студент осылай көреді (интерфейс тілінде). Батырмалар мұнда басылмайды.', 'Так увидит студент (на языке интерфейса). Кнопки здесь не нажимаются.')}</p>
      <section className="section">
        <h2>{tt('Тізімдегі карточка', 'Карточка в списке')}</h2>
        <div className="ac-preview" inert>{card}</div>
      </section>
      <section className="section">
        <h2>{f.kind === 'discounts' ? tt('Ашылған терезе', 'Открытое окно') : tt('Толық беті', 'Страница')}</h2>
        <div className="ac-preview ac-page" inert>{page}</div>
      </section>
    </>
  );
}
