'use client';
/* Spec 10 career-fair booths (FEATURE_FAIRS, backend/internal/fairs): the companies on a career event page, the
   organizer's booth list with printable QR codes, the booth scan screen (/booth?b=<token>), «Мен барған стендтер» and
   the employer cabinet's fairs. A scan made without internet waits on the device and is sent when the connection
   is back, if that is within two hours. */
import { useCallback, useEffect, useRef, useState } from 'react';
import { useApp } from '@/components/app';
import { Empty, Icon, Qr, Row, SectionHead } from '@/components/ui';
import { ErrorLine, errCode, errText, Feature, hasAccount, hhmm, Loading, SignInCard, useFeature, useLoad } from '@/components/views/common';
import { api, useAccount } from '@/lib/account';

export interface Exhibitor { employerId: number; name: string; booth: string; openJobs: number; token?: string; url?: string; checkins?: number }
export interface MyBooth { employerId: number; company: string; booth: string; openJobs: number; at: string; shared: string[] }
interface BoothResult { status: 'ok' | 'already'; eventId: number; eventTitle: string; employerId: number; company: string; booth: string; openJobs: number; shared: string[] }
interface EmployerFair { eventId: number; title: string; titleRu: string | null; startsAt: string; endsAt: string; place: string | null; booth: string; token: string; url: string; checkins: number }
interface Visitor { at: string; name?: string; course?: number; faculty?: string; telegram?: string }

const FIELDS: [string, [string, string]][] = [
  ['name', ['Аты-жөнім', 'Имя']], ['course', ['Курс', 'Курс']], ['faculty', ['Факультет', 'Факультет']], ['telegram', ['Telegram', 'Telegram']],
];

/* ---------- offline queue: a scan in a hall without signal ---------- */
const QUEUE_KEY = 'studenthub.booths.v1';
const QUEUE_TTL = 2 * 3600_000;
type Queued = { token: string; at: number };
function readQueue(): Queued[] {
  try {
    const v = JSON.parse(localStorage.getItem(QUEUE_KEY) || '[]') as Queued[];
    return Array.isArray(v) ? v.filter(x => typeof x?.token === 'string' && typeof x.at === 'number' && Date.now() - x.at < QUEUE_TTL) : [];
  } catch { return []; }
}
function writeQueue(q: Queued[]) { try { if (q.length) localStorage.setItem(QUEUE_KEY, JSON.stringify(q)); else localStorage.removeItem(QUEUE_KEY); } catch { /* ignore */ } }
export function queueBooth(token: string) { const q = readQueue().filter(x => x.token !== token); q.push({ token, at: Date.now() }); writeQueue(q); }
/** sends what waited offline; returns how many went through */
export async function flushBooths(lang: 'kk' | 'ru'): Promise<number> {
  let sent = 0;
  for (const x of readQueue()) {
    try {
      await api<BoothResult>('/api/events/booth-checkin', { method: 'POST', body: { token: x.token }, lang });
      sent++;
      writeQueue(readQueue().filter(y => y.token !== x.token));
    } catch (e) {
      const c = errCode(e);
      if (c === 'network' || c === 'offline') break;
      writeQueue(readQueue().filter(y => y.token !== x.token)); // refused: nothing to retry
    }
  }
  return sent;
}

const offline = (e: unknown) => errCode(e) === 'network' || errCode(e) === 'offline' || (typeof navigator !== 'undefined' && !navigator.onLine);

/* ---------- event page (C1, C3) and the organizer's booths ---------- */
export function FairSection({ id, cat, manage }: { id: number; cat: string; manage: boolean }) {
  const on = useFeature('fairs');
  if (!on || cat !== 'career') return null;
  return <FairSection_ id={id} manage={manage} />;
}

function FairSection_({ id, manage }: { id: number; manage: boolean }) {
  const { tt, go, setUi } = useApp();
  const acc = useAccount();
  const ex = useLoad<{ exhibitors: Exhibitor[] }>(`/api/events/${id}/exhibitors`);
  useBoothQueue();
  const [fresh, setFresh] = useState<Exhibitor[] | null>(null);
  const list = fresh ?? ex.data?.exhibitors ?? null;
  const jobsOf = (name: string) => { setUi({ sq: name }); go('opps:jobs'); };
  return (
    <>
      <section className="section" aria-labelledby="fairCompanies">
        <SectionHead title={<span id="fairCompanies">{tt('Қатысушы компаниялар', 'Компании-участники')}</span>} more={list ? <span className="tiny muted num">{list.length}</span> : null} />
        {!list ? (ex.err ? <ErrorLine err={ex.err} /> : <Loading />) : list.length ? (
          <div className="group" id="fairList">{list.map(x => (
            <Row key={x.employerId} icon="briefcase" title={x.name}
              sub={[x.booth ? tt(`Стенд ${x.booth}`, `Стенд ${x.booth}`) : '', x.openJobs ? tt(`${x.openJobs} вакансия`, `вакансий: ${x.openJobs}`) : tt('ашық вакансия жоқ', 'нет открытых вакансий')].filter(Boolean).join(' · ')}
              onClick={x.openJobs ? () => jobsOf(x.name) : undefined} chev={!!x.openJobs} />
          ))}</div>
        ) : <p className="small muted">{tt('Компаниялар тізімі әлі жарияланбады.', 'Список компаний ещё не опубликован.')}</p>}
        <p className="tiny muted">{tt('Стендтегі QR-ды сканерлесеңіз, компания «Мен барған стендтер» тізіміне түседі. Келесі күні таңертең қорытынды келеді.', 'Отсканируйте QR на стенде — компания попадёт в «Стенды, где я был». Утром следующего дня придёт сводка.')}</p>
      </section>
      {hasAccount(acc) ? <MyBooths id={id} /> : null}
      {manage ? <OrganizerBooths id={id} list={list ?? []} onChange={setFresh} /> : null}
    </>
  );
}

function MyBooths({ id }: { id: number }) {
  const { tt, go, setUi } = useApp();
  const r = useLoad<{ booths: MyBooth[] }>(`/api/events/${id}/my-booths`);
  if (!r.data?.booths.length) return null;
  return (
    <section className="section" aria-labelledby="fairMine">
      <SectionHead title={<span id="fairMine">{tt('Мен барған стендтер', 'Стенды, где я был')}</span>} />
      <div className="group" id="myBooths">{r.data.booths.map(b => (
        <Row key={b.employerId} icon="check" tone="ok" title={b.company}
          sub={`${hhmm(b.at)}${b.openJobs ? ' · ' + tt(`${b.openJobs} вакансия`, `вакансий: ${b.openJobs}`) : ''}`}
          onClick={b.openJobs ? () => { setUi({ sq: b.company }); go('opps:jobs'); } : undefined} chev={!!b.openJobs} />
      ))}</div>
    </section>
  );
}

function OrganizerBooths({ id, list, onChange }: { id: number; list: Exhibitor[]; onChange: (l: Exhibitor[]) => void }) {
  const { tt, lang, openSheet } = useApp();
  const [bin, setBin] = useState('');
  const [booth, setBooth] = useState('');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  const send = async (method: 'POST' | 'DELETE', path: string, body?: object) => {
    setBusy(true); setErr('');
    try { const r = await api<{ exhibitors: Exhibitor[] }>(path, { method, body, lang }); onChange(r.exhibitors); return true; } catch (e) { setErr(errText(e)); return false; } finally { setBusy(false); }
  };
  return (
    <section className="section" aria-labelledby="fairOrg">
      <SectionHead title={<span id="fairOrg">{tt('Стендтер (ұйымдастырушы)', 'Стенды (организатор)')}</span>} />
      {list.length ? <div className="group">{list.map(x => (
        <div key={x.employerId} className="row" style={{ gap: 8 }}>
          <span className="row-main"><span className="row-title">{x.name}{x.booth ? ` · ${x.booth}` : ''}</span><span className="row-sub num">{tt(`Тіркелді: ${x.checkins ?? 0}`, `Отметились: ${x.checkins ?? 0}`)}</span></span>
          {x.url ? <button className="btn ghost" type="button" onClick={() => openSheet(<BoothQrSheet title={x.name} booth={x.booth} url={x.url!} />)}><Icon n="qr" s={18} /> QR</button> : null}
          <button className="icon-btn" type="button" aria-label={tt(`${x.name} — алып тастау`, `Убрать ${x.name}`)} disabled={busy} onClick={() => void send('DELETE', `/api/events/${id}/exhibitors/${x.employerId}`)}><Icon n="x" s={18} /></button>
        </div>
      ))}</div> : null}
      <form className="card form" onSubmit={async e => { e.preventDefault(); if (await send('POST', `/api/events/${id}/exhibitors`, { bin: bin.trim(), booth })) { setBin(''); setBooth(''); } }}>
        <div className="grid-2">
          <div className="field"><label htmlFor="fairBin">{tt('Компания БСН-і', 'БИН компании')}</label>
            <input className="input num" id="fairBin" inputMode="numeric" maxLength={12} value={bin} onChange={e => setBin(e.target.value.replace(/\D/g, ''))} /></div>
          <div className="field"><label htmlFor="fairBooth">{tt('Стенд', 'Стенд')}</label>
            <input className="input" id="fairBooth" maxLength={40} value={booth} onChange={e => setBooth(e.target.value)} placeholder="A1" /></div>
        </div>
        <span className="tiny muted">{tt('Тек тексерілген компанияны қосуға болады.', 'Добавить можно только проверенную компанию.')}</span>
        <button className="btn secondary" type="submit" disabled={busy || bin.length !== 12}><Icon n="plus" s={18} /> {tt('Компания қосу', 'Добавить компанию')}</button>
      </form>
      <ErrorLine err={err} />
    </section>
  );
}

/** a booth QR big enough to print on A5 */
function BoothQrSheet({ title, booth, url }: { title: string; booth: string; url: string }) {
  const { tt, closeSheet } = useApp();
  return (
    <div className="booth-print" style={{ display: 'grid', gap: 12, justifyItems: 'center', textAlign: 'center' }}>
      <h2 style={{ overflowWrap: 'anywhere' }}>{title}{booth ? ` · ${booth}` : ''}</h2>
      <Qr text={url} big />
      <p className="small">{tt('Студенттер: стендке тіркелу үшін сканерлеңіз', 'Студентам: отсканируйте, чтобы отметиться у стенда')}</p>
      <div className="btn-row">
        <button className="btn" type="button" onClick={() => window.print()}><Icon n="file" s={18} /> {tt('Басып шығару', 'Печать')}</button>
        <button className="btn ghost" type="button" onClick={closeSheet}>{tt('Жабу', 'Закрыть')}</button>
      </div>
    </div>
  );
}

/* ---------- /booth?b=<token> (C2) ---------- */
const PENDING_KEY = 'studenthub.booth.pending';

export function BoothScreen() {
  return <Feature name="fairs"><Booth_ /></Feature>;
}

type St = { st: 'idle' | 'busy' | 'done' | 'queued' | 'error' | 'none'; res?: BoothResult; token?: string; err?: string };
function Booth_() {
  const { tt, lang, go } = useApp();
  const acc = useAccount();
  const [state, setState] = useState<St>({ st: 'idle' });
  const sent = useRef('');
  useBoothQueue();
  const signedIn = hasAccount(acc);
  useEffect(() => {
    if (!acc.loaded) return;
    const fromUrl = new URLSearchParams(window.location.search).get('b');
    if (fromUrl) {
      try { sessionStorage.setItem(PENDING_KEY, fromUrl); } catch { /* ignore */ }
      window.history.replaceState(window.history.state, '', window.location.pathname);
    }
    let token = fromUrl;
    if (!token) { try { token = sessionStorage.getItem(PENDING_KEY); } catch { token = null; } }
    if (!token) { if (sent.current === '') { sent.current = 'none'; setTimeout(() => setState({ st: 'none' }), 0); } return; }
    if (!signedIn || sent.current === token) return;
    sent.current = token;
    const tok = token;
    setTimeout(() => setState({ st: 'busy' }), 0);
    api<BoothResult>('/api/events/booth-checkin', { method: 'POST', body: { token: tok }, lang }).then(r => {
      try { sessionStorage.removeItem(PENDING_KEY); } catch { /* ignore */ }
      setState({ st: 'done', res: r, token: tok });
    }, e => {
      try { sessionStorage.removeItem(PENDING_KEY); } catch { /* ignore */ }
      if (offline(e)) { queueBooth(tok); setState({ st: 'queued' }); return; }
      setState({ st: 'error', err: errText(e) });
    });
  }, [acc.loaded, signedIn, lang]);
  if (!acc.loaded || state.st === 'busy') return <Loading />;
  if (!signedIn && state.st !== 'none') {
    return <SignInCard text={tt('QR қабылданды. Стендке тіркелу үшін Telegram арқылы кіріңіз — кейін осы бетке ораласыз.', 'QR принят. Войдите через Telegram, чтобы отметиться у стенда, — после входа вернётесь сюда.')} />;
  }
  if (state.st === 'none') return <Empty icon="qr">{tt('Стендтегі QR-ды телефон камерасымен сканерлеңіз.', 'Отсканируйте QR на стенде камерой телефона.')}</Empty>;
  if (state.st === 'queued') {
    return (
      <div className="card" role="status">
        <h1>{tt('Интернет жоқ — сақтап қойдық', 'Нет интернета — сохранили')}</h1>
        <p className="small">{tt('Байланыс 2 сағат ішінде қалпына келсе, тіркеу өзі жіберіледі.', 'Если связь появится в течение 2 часов, отметка отправится сама.')}</p>
      </div>
    );
  }
  if (state.st === 'error') {
    return (
      <div className="card" role="alert">
        <h1>{tt('Тіркелмеді', 'Не отмечено')}</h1>
        <ErrorLine err={state.err ?? ''} />
        <button className="btn secondary" type="button" onClick={() => go('home')}>{tt('Басты бетке', 'На главную')}</button>
      </div>
    );
  }
  if (state.st === 'done' && state.res) return <BoothDone res={state.res} token={state.token ?? ''} />;
  return <Loading />;
}

function BoothDone({ res, token }: { res: BoothResult; token: string }) {
  const { tt, lang, go, toast } = useApp();
  const [share, setShare] = useState<string[]>(res.shared);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  // the same scan again with what to share: idempotent, only the fields change
  const save = async () => {
    setBusy(true); setErr('');
    try {
      await api<BoothResult>('/api/events/booth-checkin', { method: 'POST', body: { token, share }, lang });
      toast(tt('Сақталды', 'Сохранено'));
    } catch (e) { setErr(errText(e)); } finally { setBusy(false); }
  };
  return (
    <>
      <div className="card" style={{ alignItems: 'center', textAlign: 'center' }} role="status">
        <span className="ico ok" style={{ width: 56, height: 56 }}><Icon n="check" s={30} /></span>
        <h1 style={{ overflowWrap: 'anywhere' }}>{res.status === 'already' ? tt(`${res.company}: бұрын тіркелгенсіз`, `${res.company}: вы уже отмечены`) : tt(`Стенд тіркелді: ${res.company}`, `Стенд отмечен: ${res.company}`)}</h1>
        <span className="small muted" style={{ overflowWrap: 'anywhere' }}>{res.eventTitle}{res.booth ? ` · ${res.booth}` : ''}</span>
      </div>
      <fieldset className="card form" id="boothShare">
        <legend className="row-title">{tt('Компания не көрсін?', 'Что видит компания?')}</legend>
        <p className="tiny muted">{tt('Ештеңе белгілемесеңіз, компания тек келушілер санын көреді.', 'Если ничего не отметить, компания увидит только число посетителей.')}</p>
        {FIELDS.map(([k, l]) => (
          <label key={k} className="check"><input type="checkbox" checked={share.includes(k)} onChange={e => setShare(s => e.target.checked ? [...s, k] : s.filter(x => x !== k))} /><span className="small">{tt(...l)}</span></label>
        ))}
        <button className="btn secondary" type="button" disabled={busy || !token} onClick={save}>{tt('Сақтау', 'Сохранить')}</button>
        <ErrorLine err={err} />
      </fieldset>
      <div className="btn-row">
        {res.openJobs ? <button className="btn" type="button" onClick={() => go('opps:jobs')}><Icon n="briefcase" s={18} /> {tt(`Вакансиялар (${res.openJobs})`, `Вакансии (${res.openJobs})`)}</button> : null}
        <button className="btn ghost" type="button" onClick={() => go('event:' + res.eventId)}>{tt('Жәрмеңке беті', 'Страница ярмарки')}</button>
      </div>
    </>
  );
}

/** sends scans that waited offline, once the device is back online (mounted on the booth and event screens) */
export function useBoothQueue() {
  const { lang, toast, tt } = useApp();
  const run = useCallback(() => {
    if (!readQueue().length || !navigator.onLine) return;
    void flushBooths(lang).then(n => { if (n) toast(tt(`Желісіз сақталған ${n} стенд жіберілді`, `Отправлено стендов, сохранённых офлайн: ${n}`)); });
  }, [lang, toast, tt]);
  useEffect(() => {
    run();
    window.addEventListener('online', run);
    return () => window.removeEventListener('online', run);
  }, [run]);
}

/* ---------- employer cabinet (C4) ---------- */
export function EmployerFairs() {
  const on = useFeature('fairs');
  const events = useFeature('events');
  if (!on || !events) return null;
  return <EmployerFairs_ />;
}
function EmployerFairs_() {
  const { tt, lang, openSheet, I } = useApp();
  const r = useLoad<{ fairs: EmployerFair[] }>('/api/employer/fairs');
  if (!r.data) return r.err ? <ErrorLine err={r.err} /> : null;
  if (!r.data.fairs.length) return null;
  return (
    <section className="section" aria-labelledby="empFairs">
      <SectionHead title={<span id="empFairs">{tt('Мансап жәрмеңкелері', 'Ярмарки вакансий')}</span>} />
      <div className="list">{r.data.fairs.map(f => (
        <div key={f.eventId} className="card">
          <b style={{ overflowWrap: 'anywhere' }}>{lang === 'ru' && f.titleRu ? f.titleRu : f.title}</b>
          <span className="small muted">{I.fDate(new Date(f.startsAt))}, {hhmm(f.startsAt)}–{hhmm(f.endsAt)}{f.booth ? ` · ${tt('стенд', 'стенд')} ${f.booth}` : ''}</span>
          <span className="small num">{tt(`Тіркелген студенттер: ${f.checkins}`, `Отметились студентов: ${f.checkins}`)}</span>
          <div className="btn-row">
            <button className="btn secondary" type="button" onClick={() => openSheet(<BoothQrSheet title={lang === 'ru' && f.titleRu ? f.titleRu : f.title} booth={f.booth} url={f.url} />)}><Icon n="qr" s={18} /> {tt('Стенд QR-ы', 'QR стенда')}</button>
            <button className="btn ghost" type="button" onClick={() => openSheet(<VisitorsSheet id={f.eventId} />)}><Icon n="user" s={18} /> {tt('Келгендер', 'Посетители')}</button>
          </div>
        </div>
      ))}</div>
    </section>
  );
}

function VisitorsSheet({ id }: { id: number }) {
  const { tt, closeSheet } = useApp();
  const r = useLoad<{ total: number; anonymous: number; visitors: Visitor[] }>(`/api/employer/fairs/${id}/checkins`);
  return (
    <>
      <h2>{tt('Стендке келгендер', 'Посетители стенда')}</h2>
      {!r.data ? (r.err ? <ErrorLine err={r.err} /> : <Loading />) : (
        <>
          <p className="small num">{tt(`Барлығы: ${r.data.total}. Байланысын бөліспегендер: ${r.data.anonymous}.`, `Всего: ${r.data.total}. Не поделились контактами: ${r.data.anonymous}.`)}</p>
          <div className="group">{r.data.visitors.length ? r.data.visitors.map((v, i) => (
            <Row key={i} icon="user" chev={false} title={v.name || tt('Аты көрсетілмеген', 'Имя не указано')}
              sub={[hhmm(v.at), v.course ? tt(`${v.course}-курс`, `${v.course} курс`) : '', v.faculty ?? '', v.telegram ? '@' + v.telegram : ''].filter(Boolean).join(' · ')} />
          )) : <div className="empty">{tt('Әзірге ешкім байланысын бөліспеді.', 'Пока никто не поделился контактами.')}</div>}</div>
        </>
      )}
      <button className="btn ghost block" type="button" onClick={closeSheet}>{tt('Жабу', 'Закрыть')}</button>
    </>
  );
}
