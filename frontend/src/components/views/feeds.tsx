'use client';
/* Opportunity feed (docs/feeds.md): the «ИИ» badge and the source block on feed opportunities, the admin page
   /admin/feeds (sources, pending, published, AI usage) and the editor of a pending feed item in the queue. */
import { useEffect, useState } from 'react';
import { useApp } from '@/components/app';
import { Chip, Icon, Row, Seg, SectionHead } from '@/components/ui';
import { ErrorLine, ModeratorOnly, errText, Feature, Loading, ReasonSheet, useLoad, ymd } from '@/components/views/common';
import { api, ApiError, useAccount } from '@/lib/account';
import type { FeedMeta, LText, Opportunity } from '@/lib/types';

export const FEED_CATS: Record<string, LText> = {
  hackathon: { kk: 'Хакатон', ru: 'Хакатон' }, startup: { kk: 'Стартап', ru: 'Стартап' }, grant: { kk: 'Грант', ru: 'Грант' },
  scholarship: { kk: 'Стипендия', ru: 'Стипендия' }, internship: { kk: 'Тағылымдама', ru: 'Стажировка' },
  mobility: { kk: 'Академиялық ұтқырлық', ru: 'Академическая мобильность' }, exchange: { kk: 'Алмасу бағдарламасы', ru: 'Программа обмена' },
  competition: { kk: 'Байқау', ru: 'Конкурс' }, conference: { kk: 'Конференция', ru: 'Конференция' }, volunteer: { kk: 'Еріктілік', ru: 'Волонтёрство' },
  course: { kk: 'Курс', ru: 'Курс' }, other: { kk: 'Басқа', ru: 'Другое' },
};
const FUNDING: Record<string, LText> = {
  fully: { kk: 'Толық қаржыландырылады', ru: 'Полностью финансируется' }, partly: { kk: 'Ішінара қаржыландырылады', ru: 'Частично финансируется' },
  none: { kk: 'Қаржыландыру жоқ', ru: 'Без финансирования' }, unknown: { kk: 'Постта айтылмаған', ru: 'В посте не указано' },
};

const hostOf = (url: string) => { try { return new URL(url).hostname.replace(/^www\./, ''); } catch { return url; } };
export const isFeed = (o: Opportunity): o is Opportunity & { feed: FeedMeta } => !!o.feed && o.source === 'feed';

/** «ИИ · <source>» on cards: the item was found and summarised automatically */
export function FeedBadge({ o }: { o: Opportunity }) {
  const { tt } = useApp();
  if (!isFeed(o)) return null;
  return <span className="badge b-line feed-badge" title={tt('ИИ қорытындысы, түпнұсқасын тексеріңіз', 'Краткое изложение ИИ, проверьте оригинал')}>
    <Icon n="spark" s={12} /> {tt('ИИ', 'ИИ')} · {o.feed.source_name}</span>;
}

/** the source block of a feed opportunity: AI note, original post, application link and the facts the post states */
export function FeedNote({ o }: { o: Opportunity }) {
  const { tt, L, I } = useApp();
  if (!isFeed(o)) return null;
  const f = o.feed;
  return (
    <section className="card feed-note" aria-label={tt('Дереккөз', 'Источник')}>
      <p className="small"><span className="badge b-sun"><Icon n="spark" s={12} /> {L(f.note)}</span></p>
      <dl className="kv">
        <dt>{tt('Түпнұсқа', 'Оригинал')}</dt>
        <dd><a href={f.original_url} target="_blank" rel="noopener noreferrer">{f.source_name}</a></dd>
        <dt>{tt('Санаты', 'Категория')}</dt><dd>{L(FEED_CATS[f.category] ?? FEED_CATS.other)}</dd>
        {f.apply_url ? <><dt>{tt('Өтінім', 'Заявка')}</dt><dd><a href={f.apply_url} target="_blank" rel="noopener noreferrer">{hostOf(f.apply_url)}</a></dd></> : null}
        {f.event_date ? <><dt>{tt('Өтетін күні', 'Дата проведения')}</dt><dd>{I.fDate(new Date(f.event_date + 'T00:00:00'))}</dd></> : null}
        {f.eligibility ? <><dt>{tt('Кімге', 'Для кого')}</dt><dd>{f.eligibility}</dd></> : null}
        <dt>{tt('Қаржыландыру', 'Финансирование')}</dt><dd>{L(FUNDING[f.funding] ?? FUNDING.unknown)}</dd>
        {f.online ? <><dt>{tt('Формат', 'Формат')}</dt><dd>{tt('Онлайн', 'Онлайн')}</dd></> : null}
        {f.extra_sources.length ? <><dt>{tt('Басқа дереккөздер', 'Другие источники')}</dt>
          <dd>{f.extra_sources.map((e, i) => <span key={e.url}>{i ? ', ' : ''}<a href={e.url} target="_blank" rel="noopener noreferrer">{e.name || hostOf(e.url)}</a></span>)}</dd></> : null}
      </dl>
      <p className="tiny muted">{f.reviewed
        ? tt('Әкімші қарап шықты. Мерзім мен шарттарды түпнұсқадан тексеріңіз.', 'Проверено администратором. Сроки и условия сверяйте с оригиналом.')
        : tt('Сенімді дереккөзден автоматты түрде жарияланды.', 'Опубликовано автоматически из надёжного источника.')}</p>
      <a className="btn ghost" href={f.original_url} target="_blank" rel="noopener noreferrer"><Icon n="ext" s={18} /> {tt('Түпнұсқаны ашу', 'Открыть оригинал')}</a>
    </section>
  );
}

/* ---------- admin: /admin/feeds ---------- */
interface SourceRow {
  id: string; kind: string; handle: string; url: string; name: string; category: string; scope: string; trust: string; enabled: boolean;
  last_run_at: string | null; last_ok_at: string | null; last_error: string | null; last_new: number; posts: number; published: number;
}
interface Overview {
  sources: SourceRow[]; counts: Record<string, number>;
  usage: { calls: number; tokens: number; failures: number; limit: number; autopublish_min_confidence: number; channel: boolean; ai: boolean };
}
interface ItemRow {
  id: number; status: string; category: string; source: string; source_id: string; confidence: number; auto: boolean; submission_id: number | null;
  deadline: string | null; created_at: string; published_at: string | null;
  data: { title_kk: string; title_ru: string; summary_kk: string; source_url: string; apply_url: string; validation_notes?: string[] };
  extra_sources: { source_id: string; url: string }[];
}

export function AdminFeeds() {
  return <ModeratorOnly><Feature name="feeds"><Feeds /></Feature></ModeratorOnly>;
}

function Feeds() {
  const { tt } = useApp();
  const [tab, setTab] = useState('pending');
  const o = useLoad<Overview>('/api/admin/feeds');
  const c = o.data?.counts ?? {};
  return (
    <>
      <ErrorLine err={o.err} />
      {!o.data ? <Loading /> : <Usage o={o.data} />}
      <Seg items={[['pending', tt(`Күтуде · ${c.pending ?? 0}`, `Ожидают · ${c.pending ?? 0}`)], ['published', tt(`Жарияланған · ${c.published ?? 0}`, `Опубликовано · ${c.published ?? 0}`)],
        ['sources', tt('Дереккөздер', 'Источники')]]} cur={tab} onPick={setTab} />
      {tab === 'sources' ? (o.data ? <Sources list={o.data.sources} onDone={o.reload} /> : null) : <Items status={tab} onDone={o.reload} />}
    </>
  );
}

function Usage({ o }: { o: Overview }) {
  const { tt } = useApp();
  const u = o.usage;
  return (
    <section className="section">
      <SectionHead title={tt('ИИ бүгін', 'ИИ сегодня')} />
      <div className="stat-grid">
        <div className="stat"><b className="num">{u.calls}</b><span className="tiny muted">{tt('сұраныс', 'запросов')}{u.failures ? ` · ${u.failures} ${tt('қате', 'ошибок')}` : ''}</span></div>
        <div className="stat"><b className="num">{u.tokens.toLocaleString('ru-RU')}</b><span className="tiny muted">{tt('токен', 'токенов')} / {u.limit.toLocaleString('ru-RU')}</span></div>
      </div>
      <p className="tiny muted">
        {u.ai ? '' : tt('OPENAI_API_KEY қойылмаған: тек жинау. ', 'OPENAI_API_KEY не задан: только сбор. ')}
        {u.autopublish_min_confidence > 0
          ? tt(`Сенімді дереккөз + сенімділік ≥ ${u.autopublish_min_confidence} автоматты жарияланады.`, `Надёжный источник + уверенность ≥ ${u.autopublish_min_confidence} публикуется автоматически.`)
          : tt('Автожариялау өшірулі: бәрі кезекке түседі.', 'Автопубликация выключена: всё идёт в очередь.')}
        {u.channel ? tt(' Жарияланғандар Telegram арнасына жіберіледі.', ' Опубликованное уходит в Telegram-канал.') : ''}
      </p>
    </section>
  );
}

function Sources({ list, onDone }: { list: SourceRow[]; onDone: () => void }) {
  const { tt, lang, toast } = useApp();
  // moderators see the sources; switching one on or off stays with admins (the server checks it too)
  const admin = useAccount().user?.role === 'admin';
  const [busy, setBusy] = useState('');
  const toggle = async (s: SourceRow) => {
    setBusy(s.id);
    try {
      await api(`/api/admin/feeds/sources/${encodeURIComponent(s.id)}`, { method: 'POST', body: { enabled: !s.enabled }, lang });
      toast(!s.enabled ? tt('Қосылды', 'Включено') : tt('Өшірілді', 'Выключено'));
      onDone();
    } catch (e) { toast(errText(e)); } finally { setBusy(''); }
  };
  if (!list.length) return <div className="group"><div className="empty">{tt('Дереккөздер алғашқы жинаудан кейін шығады.', 'Источники появятся после первого сбора.')}</div></div>;
  return (
    <div className="list">{list.map(s => (
      <div key={s.id} className="card">
        <div className="badges">
          <span className={`badge ${s.enabled ? 'b-ok' : 'b-line'}`}>{s.enabled ? tt('қосулы', 'включён') : tt('өшірулі', 'выключен')}</span>
          <span className="badge b-line">{s.kind}</span><span className="badge b-line">{s.trust}</span><span className="badge b-line">{s.scope}</span>
        </div>
        <h3>{s.name}</h3>
        <a className="tiny" href={s.url} target="_blank" rel="noopener noreferrer">{s.handle ? '@' + s.handle : hostOf(s.url)}</a>
        <span className="small muted num">{tt('Соңғы жинау', 'Последний сбор')}: {s.last_run_at ? ymd(s.last_run_at) + ' ' + s.last_run_at.slice(11, 16) : '—'}
          {' · '}{tt('жаңа', 'новых')} {s.last_new} · {tt('барлығы', 'всего')} {s.posts} · {tt('жарияланған', 'опубликовано')} {s.published}</span>
        {s.last_error ? <p className="small urgent" role="alert">{s.last_error}</p> : null}
        {admin ? <button className="btn ghost" type="button" disabled={busy === s.id} onClick={() => toggle(s)}>
          {s.enabled ? tt('Өшіру', 'Выключить') : tt('Қосу', 'Включить')}</button> : null}
      </div>
    ))}</div>
  );
}

function Items({ status, onDone }: { status: string; onDone: () => void }) {
  const { tt, L, lang, toast, openSheet, closeSheet } = useApp();
  const q = useLoad<{ items: ItemRow[] }>(`/api/admin/feeds/items?status=${status}`);
  const [minConf, setMinConf] = useState(0);
  const [src, setSrc] = useState('');
  const [cat, setCat] = useState('');
  const [picked, setPicked] = useState<Set<number>>(() => new Set());
  const done = () => { q.reload(); onDone(); };
  const approve = async (it: ItemRow) => {
    try {
      await api(`/api/admin/queue/${it.submission_id}/approve`, { method: 'POST', body: {}, lang });
      toast(tt('Жарияланды', 'Опубликовано')); done();
    } catch (e) { toast(errText(e)); }
  };
  const reject = (it: ItemRow) => openSheet(<ReasonSheet title={tt('Қабылдамау', 'Отклонить')} label={tt('Себебі', 'Причина')} action={tt('Қабылдамау', 'Отклонить')} danger
    onSubmit={async reason => { await api(`/api/admin/queue/${it.submission_id}/reject`, { method: 'POST', body: { reason }, lang }); closeSheet(); done(); }} />);
  const unpublish = (it: ItemRow) => openSheet(<ReasonSheet title={tt('Сайттан алу', 'Снять с сайта')} label={tt('Ескертпе (міндетті емес)', 'Комментарий (необязательно)')} required={false} min={0}
    action={tt('Алу', 'Снять')} danger onSubmit={async () => { await api(`/api/admin/feeds/items/${it.id}/unpublish`, { method: 'POST', body: {}, lang }); closeSheet(); done(); }} />);
  const pending = status === 'pending';
  const all = q.data?.items ?? [];
  const shown = pending ? all.filter(it => it.confidence >= minConf && (!src || it.source_id === src) && (!cat || it.category === cat)) : all;
  const sources = [...new Map(all.map(it => [it.source_id, it.source])).entries()];
  const cats = [...new Set(all.map(it => it.category))];
  const chosen = shown.filter(it => picked.has(it.id));
  const bulk = (action: 'approve' | 'reject', filter: BulkFilter) => openSheet(<BulkSheet action={action} filter={filter} onDone={() => { setPicked(new Set()); done(); }} />);
  const toggle = (id: number) => setPicked(p => { const n = new Set(p); if (n.has(id)) n.delete(id); else n.add(id); return n; });
  return (
    <>
      <ErrorLine err={q.err} />
      {pending && all.length ? (
        <div className="card bulk-filters">
          <div className="field"><span className="small muted" id="bfConf">{tt('ИИ сенімділігі кемінде', 'Уверенность ИИ не ниже')}</span>
            <div className="chips wrap" role="group" aria-labelledby="bfConf">
              {[0, 0.6, 0.8, 0.9].map(v => <Chip key={v} pressed={minConf === v} onClick={() => setMinConf(v)}>{v ? `${Math.round(v * 100)}%` : tt('Кез келген', 'Любая')}</Chip>)}
            </div></div>
          <div className="adm-filters">
            <div className="field"><label htmlFor="bfSrc">{tt('Дереккөз', 'Источник')}</label>
              <select className="input" id="bfSrc" value={src} onChange={e => setSrc(e.target.value)}>
                <option value="">{tt('Барлығы', 'Все')}</option>{sources.map(([id, name]) => <option key={id} value={id}>{name}</option>)}
              </select></div>
            <div className="field"><label htmlFor="bfCat">{tt('Санаты', 'Категория')}</label>
              <select className="input" id="bfCat" value={cat} onChange={e => setCat(e.target.value)}>
                <option value="">{tt('Барлығы', 'Все')}</option>{cats.map(c => <option key={c} value={c}>{L(FEED_CATS[c] ?? FEED_CATS.other)}</option>)}
              </select></div>
          </div>
          <div className="btn-row">
            <button className="btn secondary" type="button" disabled={!shown.length} onClick={() => bulk('approve', { all: true, minConfidence: minConf, source: src, category: cat })}>
              {tt('Бәрін жариялау', 'Опубликовать всё')}</button>
            <button className="link-btn" type="button" disabled={!shown.length} onClick={() => setPicked(chosen.length === shown.length ? new Set() : new Set(shown.map(it => it.id)))}>
              {chosen.length === shown.length && shown.length ? tt('Белгіні алу', 'Снять выделение') : tt('Көрініп тұрғанның бәрін белгілеу', 'Выделить все видимые')}</button>
          </div>
          <p className="tiny muted">{tt('«Бәрін жариялау» сүзгіге сай барлық күтудегі жазбаны алады; алдымен нақты саны мен тізімі көрсетіледі. Мерзімі өткендері өткізіліп жіберіледі.',
            '«Опубликовать всё» берёт все ожидающие записи под фильтр; сначала покажем точное число и список. Просроченные пропускаются.')}</p>
        </div>
      ) : null}
      {!q.data ? <Loading /> : shown.length ? (
        <div className="list">{shown.map(it => (
          <div key={it.id} className={`card${picked.has(it.id) ? ' is-picked' : ''}`}>
            {pending && it.submission_id ? (
              <label className="check small"><input type="checkbox" checked={picked.has(it.id)} onChange={() => toggle(it.id)} /> {tt('Таңдау', 'Выбрать')}</label>
            ) : null}
            <div className="badges">
              <span className="badge b-sky">{L(FEED_CATS[it.category] ?? FEED_CATS.other)}</span>
              <span className="badge b-line">{it.source}</span>
              <span className={`badge ${it.confidence >= 0.8 ? 'b-ok' : 'b-line'} num`}>{Math.round(it.confidence * 100)}%</span>
              {it.auto ? <span className="badge b-line">{tt('авто', 'авто')}</span> : null}
              {it.extra_sources.length ? <span className="badge b-line">+{it.extra_sources.length} {tt('дереккөз', 'источн.')}</span> : null}
            </div>
            <h3>{lang !== 'kk' ? it.data.title_ru || it.data.title_kk : it.data.title_kk}</h3>
            <p className="small">{it.data.summary_kk}</p>
            <span className="small muted">{tt('Мерзімі', 'Срок')}: {it.deadline || tt('көрсетілмеген', 'не указан')}</span>
            {it.data.validation_notes?.length ? <ul className="bullets tiny muted">{it.data.validation_notes.map(n => <li key={n}>{n}</li>)}</ul> : null}
            <div className="btn-row">
              <a className="btn ghost" href={it.data.source_url} target="_blank" rel="noopener noreferrer"><Icon n="ext" s={18} /> {tt('Түпнұсқа', 'Оригинал')}</a>
              {status === 'pending' && it.submission_id ? <>
                <button className="btn ghost" type="button" onClick={() => openSheet(<FeedEditSheet id={it.submission_id!} payload={it.data as unknown as Record<string, unknown>} onDone={done} />)}><Icon n="edit" s={18} /> {tt('Түзету', 'Исправить')}</button>
                <button className="btn danger" type="button" onClick={() => reject(it)}>{tt('Қабылдамау', 'Отклонить')}</button>
                <button className="btn" type="button" onClick={() => approve(it)}>{tt('Жариялау', 'Опубликовать')}</button>
              </> : null}
              {status === 'published' ? <button className="btn danger" type="button" onClick={() => unpublish(it)}>{tt('Сайттан алу', 'Снять с сайта')}</button> : null}
            </div>
          </div>
        ))}</div>
      ) : <div className="group"><div className="empty">{all.length ? tt('Бұл сүзгіге сай жазба жоқ', 'Под этот фильтр записей нет') : tt('Бос', 'Пусто')}</div></div>}
      {pending && chosen.length ? (
        <div className="bulk-bar" role="region" aria-label={tt('Таңдалғандар', 'Выбранные')}>
          <span className="small num">{tt(`Таңдалды: ${chosen.length}`, `Выбрано: ${chosen.length}`)}</span>
          <button className="btn danger" type="button" onClick={() => bulk('reject', { ids: chosen.map(it => it.id) })}>{tt('Таңдалғанды қабылдамау', 'Отклонить выбранные')}</button>
          <button className="btn" type="button" onClick={() => bulk('approve', { ids: chosen.map(it => it.id) })}>{tt('Таңдалғанды жариялау', 'Опубликовать выбранные')}</button>
        </div>
      ) : null}
    </>
  );
}

type BulkFilter = { ids?: number[]; all?: boolean; minConfidence?: number; source?: string; category?: string };
interface BulkPreview { count: number; max: number; items: { id: number; title: string; confidence: number; source: string; deadline: string | null }[] }
interface BulkResult { published: number; rejected: number; skipped: { id: number; reason: string }[]; failed: { id: number; reason: string }[] }

/** confirm a bulk decision: the server's exact count and list first, then the result (expired items are skipped) */
function BulkSheet({ action, filter, onDone }: { action: 'approve' | 'reject'; filter: BulkFilter; onDone: () => void }) {
  const { tt, lang, closeSheet, toast } = useApp();
  const pre = useLoadPost<BulkPreview>('/api/admin/feeds/items/bulk', { action: 'preview', filter });
  const [reason, setReason] = useState('');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  const [res, setRes] = useState<BulkResult | null>(null);
  const ok = action === 'approve' || (reason.trim().length >= 3 && reason.trim().length <= 500);
  const today = new Date().toISOString().slice(0, 10);
  if (res) {
    const expired = res.skipped.filter(x => x.reason === 'expired').length;
    return (
      <div className="form">
        <h2>{tt('Дайын', 'Готово')}</h2>
        <div className="stat-grid">
          <div className="stat"><b className="num">{action === 'approve' ? res.published : res.rejected}</b><span>{action === 'approve' ? tt('жарияланды', 'опубликовано') : tt('қабылданбады', 'отклонено')}</span></div>
          <div className="stat"><b className="num">{expired}</b><span>{tt('мерзімі өткен, өткізілді', 'просрочены, пропущены')}</span></div>
          <div className="stat"><b className="num">{res.skipped.length - expired}</b><span>{tt('бұрын шешілген', 'уже рассмотрены')}</span></div>
          <div className={`stat${res.failed.length ? ' stat-warn' : ''}`}><b className="num">{res.failed.length}</b><span>{tt('қате', 'ошибок')}</span></div>
        </div>
        {res.failed.length ? <p className="small urgent">{res.failed.map(f => `#${f.id}: ${f.reason}`).join(', ')}</p> : null}
        <button className="btn" type="button" onClick={closeSheet}>{tt('Жабу', 'Закрыть')}</button>
      </div>
    );
  }
  return (
    <form className="form" onSubmit={async e => {
      e.preventDefault();
      if (!ok || busy || !pre.data?.count) return;
      setBusy(true); setErr('');
      try {
        const r = await api<BulkResult>('/api/admin/feeds/items/bulk', { method: 'POST', body: { action, filter, reason: reason.trim() }, lang });
        setRes(r); onDone();
        toast(action === 'approve' ? tt(`Жарияланды: ${r.published}`, `Опубликовано: ${r.published}`) : tt(`Қабылданбады: ${r.rejected}`, `Отклонено: ${r.rejected}`));
      } catch (x) { setErr(errText(x)); } finally { setBusy(false); }
    }}>
      <h2>{action === 'approve' ? tt('Жаппай жариялау', 'Массовая публикация') : tt('Жаппай қабылдамау', 'Массовое отклонение')}</h2>
      <ErrorLine err={pre.err} />
      {!pre.data ? (pre.err ? null : <Loading />) : (
        <>
          <p className="small"><b className="num">{pre.data.count}</b> {tt('жазба', 'записей')}{pre.data.count >= pre.data.max ? tt(` (бір ретте ең көбі ${pre.data.max})`, ` (за раз не больше ${pre.data.max})`) : ''}</p>
          <ol className="group adm-log bulk-preview">
            {pre.data.items.slice(0, 30).map(it => (
              <li key={it.id}><span className="small"><b>{it.title || '#' + it.id}</b></span>
                <span className="tiny muted num">{it.source} · {Math.round(it.confidence * 100)}% · {it.deadline ? (it.deadline < today ? tt(`мерзімі өткен ${it.deadline}`, `просрочено ${it.deadline}`) : it.deadline) : tt('мерзімсіз', 'без срока')}</span></li>
            ))}
          </ol>
          {pre.data.count > 30 ? <p className="tiny muted">{tt(`және тағы ${pre.data.count - 30}`, `и ещё ${pre.data.count - 30}`)}</p> : null}
        </>
      )}
      {action === 'reject' ? (
        <div className="field"><label htmlFor="bkReason">{tt('Себебі (міндетті, журналға жазылады)', 'Причина (обязательно, пишется в журнал)')}</label>
          <textarea className="input" id="bkReason" maxLength={500} value={reason} onChange={e => setReason(e.target.value)} /></div>
      ) : <p className="tiny muted">{tt('Әр жазба жеке жариялаудағыдай тексеріледі; мерзімі өткендер жарияланбайды.', 'Каждая запись проверяется как при обычной публикации; просроченные не публикуются.')}</p>}
      <ErrorLine err={err} />
      <div className="btn-row">
        <button className="btn ghost" type="button" onClick={closeSheet}>{tt('Бас тарту', 'Отмена')}</button>
        <button className={`btn${action === 'reject' ? ' danger' : ''}`} type="submit" disabled={!ok || busy || !pre.data?.count}>
          {action === 'approve' ? tt(`Жариялау (${pre.data?.count ?? 0})`, `Опубликовать (${pre.data?.count ?? 0})`) : tt(`Қабылдамау (${pre.data?.count ?? 0})`, `Отклонить (${pre.data?.count ?? 0})`)}</button>
      </div>
    </form>
  );
}

/** one POST whose answer the sheet shows (the bulk preview) */
function useLoadPost<T>(path: string, body: unknown): { data: T | null; err: string } {
  const { lang } = useApp();
  const [state, setState] = useState<{ data: T | null; err: string }>({ data: null, err: '' });
  const key = JSON.stringify(body);
  useEffect(() => {
    let live = true;
    api<T>(path, { method: 'POST', body: JSON.parse(key), lang }).then(d => { if (live) setState({ data: d, err: '' }); }, e => { if (live) setState({ data: null, err: errText(e) }); });
    return () => { live = false; };
  }, [path, key, lang]);
  return state;
}

const EDIT_FIELDS: [string, LText, 'text' | 'area' | 'date' | 'cat'][] = [
  ['title_kk', { kk: 'Атауы (қаз.)', ru: 'Название (каз.)' }, 'text'], ['title_ru', { kk: 'Атауы (орыс.)', ru: 'Название (рус.)' }, 'text'],
  ['summary_kk', { kk: 'Қысқаша (қаз.)', ru: 'Кратко (каз.)' }, 'area'], ['summary_ru', { kk: 'Қысқаша (орыс.)', ru: 'Кратко (рус.)' }, 'area'],
  ['category', { kk: 'Санаты', ru: 'Категория' }, 'cat'], ['organizer', { kk: 'Ұйымдастырушы', ru: 'Организатор' }, 'text'],
  ['deadline', { kk: 'Өтінім мерзімі', ru: 'Срок заявки' }, 'date'], ['event_date', { kk: 'Өтетін күні', ru: 'Дата проведения' }, 'date'],
  ['eligibility', { kk: 'Кімге', ru: 'Для кого' }, 'text'], ['apply_url', { kk: 'Өтінім сілтемесі', ru: 'Ссылка на заявку' }, 'text'],
];

/** corrects a pending feed item before approval: PATCH /api/admin/queue/{id} (validated by the server, written to the audit log) */
export function FeedEditSheet({ id, payload, onDone }: { id: number; payload: Record<string, unknown>; onDone: () => void }) {
  const { tt, L, lang, toast, closeSheet } = useApp();
  const [f, setF] = useState<Record<string, string>>(() => Object.fromEntries(EDIT_FIELDS.map(([k]) => [k, typeof payload[k] === 'string' ? payload[k] as string : ''])));
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  return (
    <form className="form" noValidate onSubmit={async e => {
      e.preventDefault();
      if (busy) return;
      setBusy(true); setErr('');
      try {
        await api(`/api/admin/queue/${id}`, { method: 'PATCH', body: { payload: f }, lang });
        closeSheet(); toast(tt('Түзетілді. Енді жариялауға болады', 'Исправлено. Теперь можно опубликовать')); onDone();
      } catch (e2) {
        setErr(e2 instanceof ApiError && e2.code === 'not_pending' ? tt('Бұл жазба бұрын шешілген', 'Запись уже рассмотрена') : errText(e2));
      } finally { setBusy(false); }
    }}>
      <h2>{tt('Жазбаны түзету', 'Исправить запись')}</h2>
      <p className="tiny muted">{tt('Тек түпнұсқада жазылған деректі енгізіңіз.', 'Вносите только то, что написано в оригинале.')}</p>
      {EDIT_FIELDS.map(([k, label, kind]) => (
        <div key={k} className="field">
          <label htmlFor={'fe_' + k}>{L(label)}</label>
          {kind === 'area' ? <textarea className="input" id={'fe_' + k} maxLength={700} value={f[k]} onChange={e => setF({ ...f, [k]: e.target.value })} />
            : kind === 'cat' ? <div className="chips wrap" id={'fe_' + k}>{Object.keys(FEED_CATS).map(c => <Chip key={c} pressed={f[k] === c} onClick={() => setF({ ...f, [k]: c })}>{L(FEED_CATS[c])}</Chip>)}</div>
              : <input className="input" id={'fe_' + k} type={kind === 'date' ? 'date' : 'text'} maxLength={kind === 'text' ? 400 : undefined} value={f[k]} onChange={e => setF({ ...f, [k]: e.target.value })} />}
        </div>
      ))}
      <ErrorLine err={err} />
      <div className="btn-row">
        <button className="btn ghost" type="button" onClick={closeSheet}>{tt('Бас тарту', 'Отмена')}</button>
        <button className="btn" type="submit" disabled={busy}>{tt('Сақтау', 'Сохранить')}</button>
      </div>
    </form>
  );
}

/** the admin menu row */
export function FeedsAdminRow() {
  const { tt } = useApp();
  return <Row go="admin:feeds" icon="spark" title={tt('Мүмкіндіктер ағыны (ИИ)', 'Лента возможностей (ИИ)')} sub={tt('Telegram мен сайттардан, ИИ сүзгісі', 'Из Telegram и сайтов, фильтр ИИ')} />;
}
