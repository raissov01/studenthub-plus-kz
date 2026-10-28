'use client';
/* Events from the database (prompt 05): list cards, the event page with registration and e-ticket, QR check-in
   for students (/checkin), and the organizer screens (/organize, /organize/new, /organize/<id>) with the live
   rotating QR, a ticket scanner, the fallback list, teams, finish, CSV and feedback. */
import { useEffect, useMemo, useRef, useState } from 'react';
import { useApp } from '@/components/app';
import { Chip, Empty, Icon, Qr, Row, SectionHead, ShareRow } from '@/components/ui';
import {
  AdminOnly, almatyWall, ErrorLine, errCode, errText, Feature, fromLocalInput, hasAccount, hhmm, Loading, NotFound, SignInCard,
  toLocalInput, useFeature, useLoad, useNeedAccount,
} from '@/components/views/common';
import { api, apiDownload, useAccount } from '@/lib/account';
import { keepPagesOffline, useOnline } from '@/lib/pwa';
import { rememberEvent, rememberMine, savedTicket, savedTickets, withMy } from '@/lib/tickets';
import { BUILDINGS } from '@/lib/data/campus';
import {
  ALL_EVENT_CATS, canCreateEvents, isPast, CHECKIN_KEY, checkinToken, CLAIM_KEY, evDesc, evTitle,
  type Attendance, type CheckinResult, type DbEvent, type FeedbackSummary, type LiveView, type RegRow, type RegsResp,
} from '@/lib/events';

/* ---------- list ---------- */
export function DbEventCard({ e }: { e: DbEvent }) {
  const { tt, lang, L, monS, wd, go } = useApp();
  const d = almatyWall(e.starts_at);
  return (
    <button className="card" type="button" onClick={() => go('event:' + e.id)}>
      <div className="card-row">
        <div className="date"><small>{monS[d.getMonth()]}</small><b>{d.getDate()}</b></div>
        <div className="row-main">
          <span className="row-title">{evTitle(e, lang)}</span>
          <span className="row-sub">{wd[d.getDay()]}, {hhmm(e.starts_at)}{e.place_text ? `. ${e.place_text}` : ''}</span>
          <span className="badges" style={{ marginTop: 6 }}>
            <span className="badge b-line">{L(ALL_EVENT_CATS[e.cat])}</span>
            {e.gives_cert ? <span className="badge b-sun">{tt('Сертификат', 'Сертификат')}</span> : null}
            {e.status === 'draft' ? <span className="badge b-line">{tt('Черновик', 'Черновик')}</span> : null}
            {e.status === 'cancelled' ? <span className="badge b-bad">{tt('Өтпейді', 'Отменено')}</span> : null}
            {e.my?.status === 'registered' ? <span className="badge b-ok"><Icon n="check" s={12} /> {tt('Тіркелдіңіз', 'Вы записаны')}</span> : null}
            {e.my?.status === 'waitlist' ? <span className="badge b-line">{tt('Күту тізімінде', 'В листе ожидания')}</span> : null}
            {!e.my && e.spots_left === 0 ? <span className="badge b-line">{tt('Орын жоқ', 'Мест нет')}</span> : null}
          </span>
        </div>
      </div>
    </button>
  );
}

/* ---------- event page ---------- */
interface AttendeesResp { count: number; attendees?: { name: string; faculty: string | null }[] }

export function DbEventView({ id }: { id: string }) {
  return <Feature name="events"><DbEvent_ id={id} /></Feature>;
}

function DbEvent_({ id }: { id: string }) {
  const { tt, lang, L, I, wd, toast, go, setUi, openSheet } = useApp();
  const acc = useAccount();
  const need = useNeedAccount();
  const ev = useLoad<{ event: DbEvent }>(`/api/events/${id}${acc.user ? '?u=' + acc.user.id : ''}`);
  const att = useLoad<AttendeesResp>(`/api/events/${id}/attendees${acc.user ? '?u=' + acc.user.id : ''}`);
  const [busy, setBusy] = useState(false);
  const [prevTeam, setPrevTeam] = useState(false);
  const online = useOnline();
  // keep what the API said about my registration, so the ticket opens without internet (lib/tickets.ts)
  const live = ev.data?.event;
  const userId = acc.user?.id;
  useEffect(() => {
    if (!live) return;
    rememberEvent(live, userId);
    if (live.my) keepPagesOffline([`/event/${live.id}`]);
  }, [live, userId]);
  if (ev.code === 'not_found' || ev.code === 'http_404') return <NotFound />;
  // no network: the saved copy of my ticket, if there is one
  const saved = !live && (ev.code === 'offline' || ev.code === 'network') ? savedTicket(id) : null;
  if (ev.err && !saved) return <ErrorLine err={ev.err} />;
  const e = live ?? saved?.event;
  if (!e) return <Loading />;
  const d = almatyWall(e.starts_at);
  const b = BUILDINGS.find(x => x.id === e.building_id);
  const my = e.my;
  const reload = () => { ev.reload(); att.reload(); };
  const register = async () => {
    if (!need()) return;
    setBusy(true);
    try {
      const r = await api<{ status: string; already: boolean }>(`/api/events/${e.id}/register`, { method: 'POST', body: e.teams_enabled ? { with_previous_team: prevTeam } : {}, lang });
      toast(r.status === 'waitlist' ? tt('Орын жоқ — күту тізіміндесіз', 'Мест нет — вы в листе ожидания') : tt('Сіз тіркелдіңіз', 'Вы записаны'));
      reload();
    } catch (err) { toast(errText(err)); } finally { setBusy(false); }
  };
  const unregister = async () => {
    setBusy(true);
    try { await api(`/api/events/${e.id}/register`, { method: 'DELETE', lang }); toast(tt('Тіркеу жойылды', 'Запись отменена')); reload(); }
    catch (err) { toast(errText(err)); } finally { setBusy(false); }
  };
  const open = e.status === 'published' && !isPast(e.ends_at);
  const started = isPast(e.starts_at);
  return (
    <>
      <div className="card-row">
        <div className="date" style={{ width: 64 }}><small>{I.monS[d.getMonth()]}</small><b style={{ fontSize: '1.6rem' }}>{d.getDate()}</b></div>
        <div className="detail-head" style={{ minWidth: 0 }}><h1>{evTitle(e, lang)}</h1>
          <div className="badges">
            <span className="badge b-line">{L(ALL_EVENT_CATS[e.cat])}</span>
            {e.gives_cert ? <span className="badge b-sun"><Icon n="award" s={12} /> {tt('Сертификат беріледі', 'Выдаётся сертификат')}</span> : null}
            {e.status === 'draft' ? <span className="badge b-line">{tt('Черновик', 'Черновик')}</span> : null}
            {e.status === 'finished' ? <span className="badge b-line">{tt('Өтті', 'Прошло')}</span> : null}
          </div></div>
      </div>
      {saved ? <p className="small muted" role="status">{tt(`Сақталған көшірме (${I.fDate(new Date(saved.savedAt))}). Интернет қосылғанда жаңарады.`, `Сохранённая копия (${I.fDate(new Date(saved.savedAt))}). Обновится, когда появится интернет.`)}</p> : null}
      {e.status === 'cancelled' ? <p className="draft-banner">{tt('Іс-шара өтпейді (ұйымдастырушы тоқтатты)', 'Мероприятие отменено организатором')}</p> : null}
      <div className="group">
        <Row icon="clock" title={`${wd[d.getDay()]}, ${I.fDate(d)}`} sub={`${hhmm(e.starts_at)}–${hhmm(e.ends_at)} · ${e.university}`} chev={false} />
        {b ? <Row onClick={() => { setUi({ mapSel: b.id, mapQ: '' }); go('map'); }} icon="pin" title={e.place_text || L(b.name)} sub={tt('Кампус картасынан көру', 'Посмотреть на карте кампуса')} />
          : e.place_text ? <Row icon="pin" title={e.place_text} chev={false} /> : null}
        <Row icon="user" title={tt(`Кім барады: ${att.data?.count ?? e.registered}`, `Кто идёт: ${att.data?.count ?? e.registered}`)}
          sub={e.capacity == null ? tt('Орын шектелмеген', 'Без ограничения мест') : e.spots_left ? tt(`Бос орын: ${e.spots_left} / ${e.capacity}`, `Свободно: ${e.spots_left} из ${e.capacity}`) : tt(`Орын толды (${e.capacity}). Күту тізімі бар`, `Мест нет (${e.capacity}). Есть лист ожидания`)}
          onClick={att.data?.attendees ? () => openSheet(<AttendeesSheet list={att.data?.attendees ?? []} />) : undefined} chev={!!att.data?.attendees} />
      </div>
      {!att.data?.attendees ? <p className="tiny muted">{tt('Қатысушылардың аты тек тіркелгендерге көрінеді.', 'Имена участников видны только записавшимся.')}</p> : null}
      {evDesc(e, lang) ? <p style={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>{evDesc(e, lang)}</p> : null}
      {e.can_manage ? <button className="btn secondary block" type="button" onClick={() => go('organize:' + e.id)}><Icon n="gear" s={18} /> {tt('Іс-шараны басқару', 'Управлять мероприятием')}</button> : null}
      {my?.status === 'registered' ? (
        <>
          <div className="card" style={{ alignItems: 'center', textAlign: 'center' }}>
            <span className="eyebrow">{tt('Электрондық билет', 'Электронный билет')}</span>
            {my.checked_in_at
              ? <><span className="badge b-ok"><Icon n="check" s={12} /> {tt('Келгеніңіз расталды', 'Ваше присутствие подтверждено')}</span>
                {my.team_no ? <span className="big-num">{tt(`${my.team_no}-команда`, `Команда ${my.team_no}`)}</span> : null}</>
              : <><Qr text={my.ticket} /><span className="code">{my.ticket.slice(0, 6).toUpperCase()}</span>
                <span className="tiny muted">{tt('Кіреберісте ұйымдастырушының QR-ын сканерлеңіз. Интернет болмаса — осы билетті көрсетіңіз.', 'На входе отсканируйте QR организатора. Нет интернета — покажите этот билет.')}</span></>}
          </div>
          <IcsLink id={e.id} />
          {!started && open ? <button className="btn danger block" type="button" disabled={busy || !online} onClick={unregister}>{tt('Тіркеуден бас тарту', 'Отменить запись')}</button> : null}
          {e.status === 'finished' && my.checked_in_at && my.feedback_score == null && online ? <FeedbackForm id={e.id} onDone={reload} /> : null}
          {my.feedback_score != null ? <p className="tiny muted">{tt('Пікіріңіз үшін рақмет!', 'Спасибо за отзыв!')}</p> : null}
        </>
      ) : my?.status === 'waitlist' ? (
        <>
          <div className="card"><b>{tt('Орын жоқ — күту тізіміндесіз', 'Мест нет — вы в листе ожидания')}</b>
            <span className="small muted">{tt('Біреу бас тартса, сізді автоматты тіркеп, Telegram-да хабарлаймыз.', 'Если кто-то откажется, запишем вас автоматически и сообщим в Telegram.')}</span></div>
          {open && !started ? <IcsLink id={e.id} /> : null}
          {open && !started ? <button className="btn danger block" type="button" disabled={busy || !online} onClick={unregister}>{tt('Күту тізімінен шығу', 'Выйти из листа ожидания')}</button> : null}
        </>
      ) : open ? (
        <>
          {e.teams_enabled ? <label className="check"><input type="checkbox" checked={prevTeam} onChange={x => setPrevTeam(x.target.checked)} /><span className="small">{tt('Бұрынғы командаммен', 'С моей прошлой командой')}</span></label> : null}
          <div className="sticky-cta">
            <button className="btn block" type="button" disabled={busy || !online} onClick={register}><Icon n="check" s={18} /> {e.spots_left === 0 ? tt('Күту тізіміне жазылу', 'В лист ожидания') : tt('Тіркелу', 'Записаться')}</button>
          </div>
          <IcsLink id={e.id} />
        </>
      ) : null}
      {e.gives_cert ? <p className="tiny muted">{tt('Келгеніңіз QR арқылы расталса, сертификат портфолиоңызға өзі қосылады.', 'Если ваше присутствие подтверждено по QR, сертификат сам появится в портфолио.')}</p> : null}
      {e.status === 'published' ? <ShareRow path={`/event/${e.id}`} title={evTitle(e, lang)} kind="event" id={String(e.id)} /> : null}
      {!online ? <p className="tiny muted">{tt('Тіркелу мен бас тартуға интернет керек.', 'Для записи и отмены нужен интернет.')}</p> : null}
      {!acc.user && online ? <SignInCard text={tt('Тіркелу үшін Telegram арқылы кіріңіз — билет пен еске салу аккаунтқа байланады.', 'Чтобы записаться, войдите через Telegram — билет и напоминание привязываются к аккаунту.')} /> : null}
    </>
  );
}

function AttendeesSheet({ list }: { list: { name: string; faculty: string | null }[] }) {
  const { tt, closeSheet } = useApp();
  return (
    <>
      <h2>{tt('Кім барады', 'Кто идёт')}</h2>
      <div className="group">{list.length ? list.map((a, i) => <Row key={i} icon="user" title={a.name || '—'} sub={a.faculty || ''} chev={false} />) : <div className="empty">{tt('Әзірге ешкім', 'Пока никого')}</div>}</div>
      <button className="btn ghost block" type="button" onClick={closeSheet}>{tt('Жабу', 'Закрыть')}</button>
    </>
  );
}

function FeedbackForm({ id, onDone }: { id: number; onDone: () => void }) {
  const { tt, lang, toast } = useApp();
  const [score, setScore] = useState(0);
  const [text, setText] = useState('');
  const [err, setErr] = useState('');
  return (
    <form className="card form" onSubmit={async e => {
      e.preventDefault();
      if (!score) return;
      try { await api(`/api/events/${id}/feedback`, { method: 'POST', body: { score, text: text.trim() }, lang }); toast(tt('Рақмет!', 'Спасибо!')); onDone(); }
      catch (e2) { setErr(errText(e2)); }
    }}>
      <h3 id="evFbQ">{tt('Іс-шара қалай өтті?', 'Как прошло мероприятие?')}</h3>
      <div className="chips fb-score" role="radiogroup" aria-labelledby="evFbQ">{[1, 2, 3, 4, 5].map(n => <Chip key={n} pressed={score === n} onClick={() => setScore(n)}>{n}</Chip>)}</div>
      <div className="field"><label htmlFor="evFbText">{tt('Не ұнады / не жетіспеді? (міндетті емес)', 'Что понравилось / чего не хватило? (необязательно)')}</label>
        <textarea className="input" id="evFbText" maxLength={500} value={text} onChange={e => setText(e.target.value)} /></div>
      <ErrorLine err={err} />
      <button className="btn" type="submit" disabled={!score}>{tt('Жіберу', 'Отправить')}</button>
    </form>
  );
}

/* ---------- student check-in by QR (/checkin, Mini App start_param ci_…) ---------- */
function readStored(k: string, store: 'local' | 'session'): string {
  try { return (store === 'local' ? window.localStorage : window.sessionStorage).getItem(k) || ''; } catch { return ''; }
}
function writeStored(k: string, v: string | null, store: 'local' | 'session') {
  try { const s = store === 'local' ? window.localStorage : window.sessionStorage; if (v == null) s.removeItem(k); else s.setItem(k, v); } catch { /* ignore */ }
}

export function Checkin() {
  return <Feature name="events"><Checkin_ /></Feature>;
}

type CiState = { st: 'idle' | 'busy' | 'done' | 'login' | 'error'; res?: CheckinResult; err?: string };
function Checkin_() {
  const { tt, lang, go } = useApp();
  const acc = useAccount();
  const [state, setState] = useState<CiState>({ st: 'idle' });
  const sent = useRef('');
  useEffect(() => {
    if (!acc.loaded) return;
    const fromUrl = checkinToken(window.location.search);
    if (fromUrl) { writeStored(CHECKIN_KEY, fromUrl, 'session'); window.history.replaceState(window.history.state, '', window.location.pathname); }
    const token = fromUrl || readStored(CHECKIN_KEY, 'session');
    let claim: { claim: string; expires_at: string } | null = null;
    try { claim = JSON.parse(readStored(CLAIM_KEY, 'local') || 'null'); } catch { claim = null; }
    if (claim && new Date(claim.expires_at).getTime() < Date.now()) { writeStored(CLAIM_KEY, null, 'local'); claim = null; }
    const ready = hasAccount(acc);
    const job = claim && ready ? 'claim:' + claim.claim : token ? 'token:' + token + ':' + ready : claim ? 'wait' : '';
    if (!job || job === sent.current) return;
    sent.current = job;
    if (job === 'wait') { setTimeout(() => setState({ st: 'login' }), 0); return; }
    const req = claim && ready
      ? api<CheckinResult>('/api/events/checkin/claim', { method: 'POST', body: { claim: claim.claim }, lang })
      : api<CheckinResult & { claim?: string; expires_at?: string }>('/api/events/checkin', { method: 'POST', body: { token }, lang });
    setTimeout(() => setState({ st: 'busy' }), 0);
    req.then(r => {
      writeStored(CHECKIN_KEY, null, 'session');
      if ((r as { status: string }).status === 'login_required') {
        const c = r as unknown as { claim: string; expires_at: string };
        writeStored(CLAIM_KEY, JSON.stringify({ claim: c.claim, expires_at: c.expires_at }), 'local');
        setState({ st: 'login' });
        return;
      }
      writeStored(CLAIM_KEY, null, 'local');
      setState({ st: 'done', res: r });
    }, e => {
      writeStored(CHECKIN_KEY, null, 'session');
      if (errCode(e) === 'claim_expired') writeStored(CLAIM_KEY, null, 'local');
      setState({ st: 'error', err: errText(e) });
    });
  }, [acc, lang]);
  if (!acc.loaded || state.st === 'busy') return <Loading />;
  if (state.st === 'done' && state.res) {
    return (
      <div className="card" style={{ alignItems: 'center', textAlign: 'center' }} role="status">
        <span className="ico ok" style={{ width: 56, height: 56 }}><Icon n="check" s={30} /></span>
        <h1>{state.res.status === 'already' ? tt('Сіз бұрын тіркелгенсіз', 'Вы уже отмечены') : tt('Қош келдің!', 'Добро пожаловать!')}</h1>
        {state.res.team_no ? <><span className="eyebrow">{tt('Сіздің командаңыз', 'Ваша команда')}</span><span className="big-num">{state.res.team_no}</span></> : null}
        <button className="btn secondary" type="button" onClick={() => go('home')}>{tt('Басты бетке', 'На главную')}</button>
      </div>
    );
  }
  if (state.st === 'login') {
    return (
      <>
        <div className="card"><h2>{tt('Келгеніңізді растау үшін кіріңіз', 'Войдите, чтобы подтвердить присутствие')}</h2>
          <p className="small muted">{tt('QR қабылданды. 10 минут ішінде Telegram арқылы кірсеңіз, келгеніңіз өзі расталады.', 'QR принят. Войдите через Telegram в течение 10 минут — и присутствие подтвердится само.')}</p></div>
        <SignInCard text={tt('Аккаунт керек: келу сізге ғана жазылады.', 'Нужен аккаунт: отметка записывается только на вас.')} />
      </>
    );
  }
  if (state.st === 'error') {
    return (
      <Empty icon="qr" action={<button className="btn secondary" type="button" onClick={() => go('opps:events')}>{tt('Іс-шаралар', 'Мероприятия')}</button>}>
        <span role="alert">{state.err}</span>
      </Empty>
    );
  }
  return <Empty icon="qr">{tt('Іс-шарадағы QR-ды телефон камерасымен сканерлеңіз.', 'Отсканируйте QR на мероприятии камерой телефона.')}</Empty>;
}

/* ---------- portfolio: verified attendance ---------- */
export function VerifiedAttendance() {
  const { tt, lang, I } = useApp();
  const acc = useAccount();
  const on = useFeature('events');
  const { data } = useLoad<{ attendance: Attendance[] }>(on && acc.user ? '/api/events/my/attendance' : null);
  if (!on || !acc.user || !data?.attendance.length) return null;
  return (
    <div className="group">
      {data.attendance.map(a => (
        <Row key={a.event_id} go={'event:' + a.event_id} icon="calendar" title={evTitle(a, lang)} sub={I.fDateY(almatyWall(a.starts_at))}
          end={<span className="badge b-ok"><Icon n="check" s={12} /> {tt('Расталған', 'Подтверждено')}</span>} />
      ))}
    </div>
  );
}

/** «Менің тізімім» → my DB registrations */
export function MyDbEvents() {
  const acc = useAccount();
  const on = useFeature('events');
  const online = useOnline();
  const { data, code } = useLoad<{ registrations: Parameters<typeof withMy>[0][] }>(on && acc.user ? '/api/events/my' : null);
  const userId = acc.user?.id;
  const fetched = useMemo(() => data ? data.registrations.map(withMy) : null, [data]);
  useEffect(() => {
    if (!fetched || userId == null) return;
    rememberMine(fetched, userId);
    keepPagesOffline(fetched.filter(e => e.my?.status === 'registered').map(e => `/event/${e.id}`));
  }, [fetched, userId]);
  // without network (or before /api/me answers offline): the tickets kept on this device
  const fromCache = !fetched && on && (!online || code === 'offline' || code === 'network');
  const list = (fetched ?? (fromCache ? savedTickets().map(t => t.event) : [])).filter(e => e.status !== 'finished' && !isPast(e.ends_at));
  if (!list.length) return null;
  return (
    <div className="list">
      {list.map(e => (
        <div key={e.id} className="list my-ev">
          <DbEventCard e={e} />
          {e.status === 'published' ? <IcsLink id={e.id} small /> : null}
        </div>
      ))}
    </div>
  );
}

/** «Күнтізбеге қосу»: the server's .ics of a DB event (GET /api/events/{id}/ics, in the UI language) */
export function IcsLink({ id, small }: { id: number; small?: boolean }) {
  const { tt, lang } = useApp();
  const online = useOnline();
  if (!online) return null; // the file comes from the API
  return (
    <a className={small ? 'link-btn ics-link' : 'btn ghost block'} href={`/api/events/${id}/ics?lang=${lang}`} download={`event-${id}.ics`}>
      <Icon n="calendar" s={small ? 16 : 18} /> {tt('Күнтізбеге қосу', 'Добавить в календарь')}
    </a>
  );
}

/* ---------- organizer ---------- */
export function Organize({ prm }: { prm: string }) {
  return (
    <Feature name="events">
      {prm === 'new' ? <OrganizerGate create><EventForm /></OrganizerGate>
        : /^\d+$/.test(prm) ? <OrganizerGate><ManageEvent id={prm} /></OrganizerGate>
          : prm ? <NotFound /> : <OrganizerGate><OrganizerList /></OrganizerGate>}
    </Feature>
  );
}

function OrganizerGate({ children, create }: { children: React.ReactNode; create?: boolean }) {
  const acc = useAccount();
  if (!acc.loaded) return <Loading />;
  if (!acc.user) return <NotFound />;
  if (create && !canCreateEvents(acc.user.role)) return <NotFound />;
  return <>{children}</>;
}

function OrganizerList() {
  const { tt, go } = useApp();
  const acc = useAccount();
  const { data, err } = useLoad<{ events: DbEvent[] }>('/api/events/manage');
  if (err) return <ErrorLine err={err} />;
  if (!data) return <Loading />;
  const can = canCreateEvents(acc.user?.role);
  if (!can && !data.events.length) return <NotFound />;
  return (
    <>
      {can ? <button className="btn block" type="button" onClick={() => go('organize:new')}><Icon n="plus" s={18} /> {tt('Жаңа іс-шара', 'Новое мероприятие')}</button> : null}
      {data.events.length
        ? <div className="list">{data.events.map(e => <OrgCard key={e.id} e={e} />)}</div>
        : <Empty icon="calendar">{tt('Әзірге іс-шара жоқ. Біріншісін құрыңыз.', 'Мероприятий пока нет. Создайте первое.')}</Empty>}
      <p className="note">{tt('Іс-шара тек өз университетіңізге жарияланады. Модерация жоқ — бұл сіздің тікелей рөліңіз.', 'Мероприятие публикуется только для вашего университета. Без модерации — это ваша прямая роль.')}</p>
    </>
  );
}

function OrgCard({ e }: { e: DbEvent }) {
  const { tt, lang, I, go } = useApp();
  const st: Record<string, string> = { draft: tt('Черновик', 'Черновик'), published: tt('Жарияланған', 'Опубликовано'), finished: tt('Аяқталды', 'Завершено'), cancelled: tt('Тоқтатылды', 'Отменено') };
  return (
    <Row onClick={() => go('organize:' + e.id)} icon="calendar" title={evTitle(e, lang)}
      sub={<>{I.fDate(almatyWall(e.starts_at))}, {hhmm(e.starts_at)} · {st[e.status]}<span className="badges"><span className="badge b-line num">{tt(`Тіркелген ${e.registered}`, `Записались ${e.registered}`)}</span><span className="badge b-line num">{tt(`Келген ${e.checked_in}`, `Пришли ${e.checked_in}`)}</span></span></>} />
  );
}

interface FormState {
  title_kk: string; title_ru: string; desc_kk: string; desc_ru: string; cat: string; starts: string; ends: string; building_id: string;
  place_text: string; capacity: string; walk_in_allowed: boolean; gives_cert: boolean; teams_enabled: boolean; university: string;
}
function initForm(e?: DbEvent): FormState {
  const now = new Date(Date.now() + 86400_000);
  now.setUTCMinutes(0, 0, 0);
  const s = e?.starts_at ?? now.toISOString(), en = e?.ends_at ?? new Date(now.getTime() + 2 * 3600_000).toISOString();
  return {
    title_kk: e?.title_kk ?? '', title_ru: e?.title_ru ?? '', desc_kk: e?.desc_kk ?? '', desc_ru: e?.desc_ru ?? '', cat: e?.cat ?? 'career',
    starts: toLocalInput(s), ends: toLocalInput(en), building_id: e?.building_id ?? '', place_text: e?.place_text ?? '',
    capacity: e?.capacity == null ? '' : String(e.capacity), walk_in_allowed: e?.walk_in_allowed ?? false, gives_cert: e?.gives_cert ?? false,
    teams_enabled: e?.teams_enabled ?? false, university: e?.university ?? '',
  };
}

function EventForm({ edit, onSaved }: { edit?: DbEvent; onSaved?: () => void }) {
  const { tt, lang, L, toast, go } = useApp();
  const acc = useAccount();
  const [f, setF] = useState<FormState>(() => ({ ...initForm(edit), university: edit?.university ?? acc.user?.university ?? '' }));
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  const set = <K extends keyof FormState>(k: K, v: FormState[K]) => setF(x => ({ ...x, [k]: v }));
  const admin = acc.user?.role === 'admin';
  const titleOk = f.title_kk.trim().length >= 3 && (!admin || !!edit || f.university.trim().length >= 2);
  const save = async (status?: 'draft' | 'published') => {
    if (!titleOk || busy) return;
    const cap = f.capacity.trim();
    const body: Record<string, unknown> = {
      title_kk: f.title_kk.trim(), title_ru: f.title_ru.trim() || null, desc_kk: f.desc_kk.trim() || null, desc_ru: f.desc_ru.trim() || null,
      cat: f.cat, starts_at: fromLocalInput(f.starts), ends_at: fromLocalInput(f.ends), building_id: f.building_id || null,
      place_text: f.place_text.trim() || null, capacity: cap ? Number(cap) : null, walk_in_allowed: f.walk_in_allowed,
      gives_cert: f.gives_cert, teams_enabled: f.teams_enabled,
    };
    if (status) body.status = status;
    if (!edit && admin && f.university.trim()) body.university = f.university.trim();
    setBusy(true); setErr('');
    try {
      if (edit) {
        await api(`/api/events/${edit.id}`, { method: 'PATCH', body, lang });
        toast(tt('Сақталды', 'Сохранено'));
        onSaved?.();
      } else {
        const r = await api<{ event: DbEvent }>('/api/events/create', { method: 'POST', body, lang });
        toast(status === 'published' ? tt('Жарияланды', 'Опубликовано') : tt('Черновик сақталды', 'Черновик сохранён'));
        go('organize:' + r.event.id, { replace: true });
      }
    } catch (e) { setErr(errText(e)); } finally { setBusy(false); }
  };
  return (
    <form className="form" onSubmit={e => { e.preventDefault(); void save(edit ? undefined : 'published'); }}>
      <div className="field"><label htmlFor="efTitleKk">{tt('Атауы (қазақша, 3–140)', 'Название (на казахском, 3–140)')}</label>
        <input className="input" id="efTitleKk" maxLength={140} value={f.title_kk} onChange={e => set('title_kk', e.target.value)} required /></div>
      <div className="field"><label htmlFor="efTitleRu">{tt('Атауы (орысша, міндетті емес)', 'Название (на русском, необязательно)')}</label>
        <input className="input" id="efTitleRu" maxLength={140} value={f.title_ru} onChange={e => set('title_ru', e.target.value)} /></div>
      <div className="field"><label htmlFor="efCat">{tt('Санаты', 'Категория')}</label>
        <select className="input" id="efCat" value={f.cat} onChange={e => set('cat', e.target.value)}>
          {Object.entries(ALL_EVENT_CATS).map(([k, c]) => <option key={k} value={k}>{L(c)}</option>)}
        </select></div>
      <div className="grid-2">
        <div className="field"><label htmlFor="efStart">{tt('Басталуы', 'Начало')}</label>
          <input className="input" id="efStart" type="datetime-local" value={f.starts} onChange={e => set('starts', e.target.value)} required /></div>
        <div className="field"><label htmlFor="efEnd">{tt('Аяқталуы', 'Окончание')}</label>
          <input className="input" id="efEnd" type="datetime-local" value={f.ends} onChange={e => set('ends', e.target.value)} required /></div>
      </div>
      <p className="tiny muted">{tt('Уақыт — Астана уақыты.', 'Время — по Астане.')}</p>
      <div className="field"><label htmlFor="efBuilding">{tt('Корпус (картада көрсету үшін)', 'Корпус (для карты кампуса)')}</label>
        <select className="input" id="efBuilding" value={f.building_id} onChange={e => set('building_id', e.target.value)}>
          <option value="">{tt('Көрсетілмеген', 'Не указан')}</option>
          {BUILDINGS.map(b => <option key={b.id} value={b.id}>{b.n}. {L(b.name)}</option>)}
        </select></div>
      <div className="field"><label htmlFor="efPlace">{tt('Орны (мысалы «Акт залы» не «онлайн»)', 'Место (например «Актовый зал» или «онлайн»)')}</label>
        <input className="input" id="efPlace" maxLength={200} value={f.place_text} onChange={e => set('place_text', e.target.value)} /></div>
      <div className="field"><label htmlFor="efCap">{tt('Орын саны (бос = шектелмеген)', 'Количество мест (пусто = без ограничения)')}</label>
        <input className="input" id="efCap" type="number" inputMode="numeric" min={1} max={100000} value={f.capacity} onChange={e => set('capacity', e.target.value)} /></div>
      {!edit && admin ? <div className="field"><label htmlFor="efUni">{tt('Университет', 'Университет')}</label>
        <input className="input" id="efUni" maxLength={120} value={f.university} onChange={e => set('university', e.target.value)} required /></div> : null}
      <label className="check"><input type="checkbox" checked={f.walk_in_allowed} onChange={e => set('walk_in_allowed', e.target.checked)} /><span className="small">{tt('Тіркелмегендер де сол жерде келе алады', 'Можно прийти без записи')}</span></label>
      <label className="check"><input type="checkbox" checked={f.gives_cert} onChange={e => set('gives_cert', e.target.checked)} /><span className="small">{tt('Келгендерге сертификат беріледі', 'Пришедшим выдаётся сертификат')}</span></label>
      <label className="check"><input type="checkbox" checked={f.teams_enabled} onChange={e => set('teams_enabled', e.target.checked)} /><span className="small">{tt('Командаларға бөлу (4–6 адам, әртүрлі факультет)', 'Делить на команды (4–6 человек, разные факультеты)')}</span></label>
      <div className="field"><label htmlFor="efDescKk">{tt('Сипаттама (қазақша)', 'Описание (на казахском)')}</label>
        <textarea className="input" id="efDescKk" maxLength={3000} value={f.desc_kk} onChange={e => set('desc_kk', e.target.value)} /></div>
      <div className="field"><label htmlFor="efDescRu">{tt('Сипаттама (орысша)', 'Описание (на русском)')}</label>
        <textarea className="input" id="efDescRu" maxLength={3000} value={f.desc_ru} onChange={e => set('desc_ru', e.target.value)} /></div>
      <ErrorLine err={err} />
      {edit
        ? <button className="btn block" type="submit" disabled={!titleOk || busy}>{tt('Сақтау', 'Сохранить')}</button>
        : <div className="btn-row">
          <button className="btn ghost" type="button" disabled={!titleOk || busy} onClick={() => void save('draft')}>{tt('Черновик ретінде', 'Как черновик')}</button>
          <button className="btn" type="submit" disabled={!titleOk || busy}>{tt('Жариялау', 'Опубликовать')}</button>
        </div>}
    </form>
  );
}

function ManageEvent({ id }: { id: string }) {
  const { tt, lang, I, toast, openSheet, closeSheet, go } = useApp();
  const ev = useLoad<{ event: DbEvent }>(`/api/events/${id}`);
  const e = ev.data?.event;
  const regs = useLoad<RegsResp>(e?.can_manage ? `/api/events/${id}/regs` : null);
  const fb = useLoad<FeedbackSummary>(e?.can_manage && e.status === 'finished' ? `/api/events/${id}/feedback` : null);
  const [editing, setEditing] = useState(false);
  const [live, setLive] = useState(false);
  const [scan, setScan] = useState(false);
  const [busy, setBusy] = useState('');
  const [q, setQ] = useState('');
  if (ev.err) return ev.code === 'not_found' ? <NotFound /> : <ErrorLine err={ev.err} />;
  if (!e) return <Loading />;
  if (!e.can_manage) return <NotFound />;
  const reload = () => { ev.reload(); regs.reload(); fb.reload(); };
  const run = async (key: string, fn: () => Promise<unknown>, done: string) => {
    if (busy) return;
    setBusy(key);
    try { await fn(); if (done) toast(done); reload(); } catch (err) { toast(errText(err)); } finally { setBusy(''); }
  };
  const confirm = (msg: string, yes: string, fn: () => void) => openSheet(
    <>
      <h2>{msg}</h2>
      <div className="btn-row">
        <button className="btn ghost" type="button" onClick={closeSheet}>{tt('Жоқ', 'Нет')}</button>
        <button className="btn" type="button" onClick={() => { closeSheet(); fn(); }}>{yes}</button>
      </div>
    </>,
  );
  const patchStatus = (status: string, done: string) => run('st', () => api(`/api/events/${e.id}`, { method: 'PATCH', body: { status }, lang }), done);
  const startLive = () => run('live', async () => { await api(`/api/events/${e.id}/checkin/start`, { method: 'POST', lang }); setLive(true); }, '');
  const manual = (body: { user_id?: number; ticket?: string }) => api<CheckinResult>(`/api/events/${e.id}/checkin/manual`, { method: 'POST', body, lang });
  /** a scanned e-ticket (full code) or the 6 characters printed under it, matched against the list */
  const byCode = async (code: string) => {
    const c = code.trim();
    if (c.length > 8) return manual({ ticket: c });
    const hit = (regs.data?.regs ?? []).filter(r => r.ticket_short.toLowerCase() === c.toLowerCase());
    if (hit.length !== 1) throw new Error(tt('Бұл код тізімде жоқ', 'Такого кода нет в списке'));
    return manual({ user_id: hit[0].user_id });
  };
  const active = e.status === 'published';
  const list = (regs.data?.regs ?? []).filter(r => !q.trim() || [r.name, r.username, r.faculty, r.ticket_short].join(' ').toLowerCase().includes(q.trim().toLowerCase()));
  const csvName = `event-${e.id}-attendees.csv`;
  return (
    <>
      <div className="detail-head"><h1>{evTitle(e, lang)}</h1>
        <span className="small muted">{I.fDate(almatyWall(e.starts_at))}, {hhmm(e.starts_at)}–{hhmm(e.ends_at)} · {e.university}</span></div>
      <div className="stat-grid">
        <div className="stat"><b>{regs.data?.registered ?? e.registered}</b><span>{tt('Тіркелген', 'Записались')}</span></div>
        <div className="stat"><b>{regs.data?.checked_in ?? e.checked_in}</b><span>{tt('Келген', 'Пришли')}</span></div>
        <div className="stat"><b>{regs.data?.waitlist ?? e.waitlist}</b><span>{tt('Күту тізімі', 'Лист ожидания')}</span></div>
      </div>
      {e.status === 'draft' ? <div className="card"><p className="small">{tt('Бұл — черновик: студенттер әлі көрмейді.', 'Это черновик: студенты его пока не видят.')}</p>
        <button className="btn" type="button" disabled={!!busy} onClick={() => patchStatus('published', tt('Жарияланды', 'Опубликовано'))}>{tt('Жариялау', 'Опубликовать')}</button></div> : null}
      {e.status === 'cancelled' ? <p className="draft-banner">{tt('Іс-шара тоқтатылды', 'Мероприятие отменено')}</p> : null}
      {active ? (
        <section className="section"><SectionHead title={tt('Келгендерді тіркеу', 'Отметка пришедших')} />
          <button className="btn block" type="button" disabled={!!busy} onClick={startLive}><Icon n="qr" s={18} /> {tt('Тіркеуді бастау', 'Начать отметку')}</button>
          <button className="btn ghost block" type="button" onClick={() => setScan(true)}><Icon n="search" s={18} /> {tt('Студент билетін сканерлеу', 'Сканировать билет студента')}</button>
          <p className="tiny muted">{tt('«Тіркеуді бастау» әр басылғанда жаңа кілт жасалады — бұрынғы QR жұмыс істемейді.', 'Каждое «Начать отметку» создаёт новый ключ — старый QR перестаёт работать.')}</p>
        </section>
      ) : null}
      <section className="section">
        <SectionHead title={tt('Тізім', 'Список')} more={<button className="link-btn" type="button" onClick={() => run('csv', () => apiDownload(`/api/events/${e.id}/attendees.csv`, csvName, lang), '')}>CSV</button>} />
        <div className="search"><Icon n="search" s={18} /><input className="input" type="search" aria-label={tt('Тізімнен іздеу', 'Поиск по списку')} placeholder={tt('Аты, @username не билет коды', 'Имя, @username или код билета')} value={q} onChange={x => setQ(x.target.value)} /></div>
        <div className="group">
          {regs.err ? <div className="empty">{regs.err}</div> : !regs.data ? <div className="empty">{tt('Жүктеліп жатыр…', 'Загрузка…')}</div> : list.length ? list.map(r => (
            <RegLine key={r.user_id} r={r} active={active} busy={!!busy}
              onIn={() => run('m' + r.user_id, () => manual({ user_id: r.user_id }), tt('Келді деп белгіленді', 'Отмечен как пришедший'))}
              onRevoke={() => confirm(tt('Бұл тіркелуді растамау керек пе?', 'Отменить эту отметку?'), tt('Растамау', 'Не подтверждать'),
                () => void run('r' + r.user_id, () => api(`/api/events/${e.id}/checkin/revoke`, { method: 'POST', body: { user_id: r.user_id }, lang }), tt('Тіркелу алынып тасталды', 'Отметка снята')))} />
          )) : <div className="empty">{tt('Әзірге ешкім тіркелмеген', 'Пока никто не записался')}</div>}
        </div>
        <p className="tiny muted">{tt('Интернет нашар болса: студенттің билетіндегі кодты тізімнен тауып, «Келді» басыңыз.', 'Если плохой интернет: найдите код с билета студента в списке и нажмите «Пришёл».')}</p>
      </section>
      {e.teams_enabled && e.status !== 'cancelled' ? (
        <button className="btn ghost block" type="button" disabled={!!busy} onClick={() => run('teams', async () => {
          const r = await api<{ teams: number }>(`/api/events/${e.id}/teams`, { method: 'POST', lang });
          toast(tt(`Командалар: ${r.teams}`, `Команд: ${r.teams}`));
        }, '')}><Icon n="user" s={18} /> {tt('Командаларға қайта бөлу', 'Перераспределить по командам')}</button>
      ) : null}
      {e.status === 'finished' && fb.data ? (
        <section className="section"><SectionHead title={tt('Пікірлер', 'Отзывы')} more={<span className="tiny muted num">{fb.data.count ? `${(fb.data.average ?? 0).toFixed(1)} / 5 · ${fb.data.count}` : ''}</span>} />
          <div className="group">{fb.data.comments.length ? fb.data.comments.map((c, i) => <Row key={i} icon="chat" title={`${c.score} / 5`} sub={c.text} chev={false} />)
            : <div className="empty">{tt('Әзірге пікір жоқ', 'Отзывов пока нет')}</div>}</div></section>
      ) : null}
      {active || e.status === 'draft' ? (
        <section className="section"><SectionHead title={tt('Баптаулар', 'Настройки')} />
          {editing ? <EventForm edit={e} onSaved={() => { setEditing(false); reload(); }} />
            : <button className="btn ghost block" type="button" onClick={() => setEditing(true)}><Icon n="edit" s={18} /> {tt('Өзгерту', 'Изменить')}</button>}
          {active ? <button className="btn secondary block" type="button" disabled={!!busy} onClick={() => confirm(tt('Іс-шараны аяқтау керек пе? Кейін QR жұмыс істемейді.', 'Завершить мероприятие? После этого QR перестанет работать.'), tt('Аяқтау', 'Завершить'),
            () => void run('fin', async () => {
              const r = await api<{ certificates: number }>(`/api/events/${e.id}/finish`, { method: 'POST', lang });
              toast(r.certificates ? tt(`Аяқталды. Сертификат: ${r.certificates}`, `Завершено. Сертификатов: ${r.certificates}`) : tt('Аяқталды', 'Завершено'));
            }, ''))}>{tt('Аяқтау', 'Завершить')}</button> : null}
          <button className="btn danger block" type="button" disabled={!!busy} onClick={() => confirm(tt('Іс-шараны тоқтату керек пе? Тіркелгендердің бәріне хабар кетеді.', 'Отменить мероприятие? Все записавшиеся получат сообщение.'), tt('Тоқтату', 'Отменить'),
            () => void patchStatus('cancelled', tt('Тоқтатылды', 'Отменено')))}>{tt('Іс-шараны тоқтату', 'Отменить мероприятие')}</button>
        </section>
      ) : null}
      <button className="link-btn" type="button" style={{ alignSelf: 'flex-start' }} onClick={() => go('event:' + e.id)}>{tt('Студент көретін бет', 'Страница для студентов')}</button>
      {live ? <LiveOverlay id={e.id} onClose={() => { setLive(false); reload(); }} /> : null}
      {scan ? <ScanOverlay onTicket={byCode} onClose={() => { setScan(false); reload(); }} /> : null}
    </>
  );
}

function RegLine({ r, active, busy, onIn, onRevoke }: { r: RegRow; active: boolean; busy: boolean; onIn: () => void; onRevoke: () => void }) {
  const { tt } = useApp();
  const status = r.checked_in_at && !r.checkin_revoked ? <span className="badge b-ok"><Icon n="check" s={12} /> {tt('Келді', 'Пришёл')}{r.team_no ? ` · ${r.team_no}` : ''}</span>
    : r.checkin_revoked ? <span className="badge b-bad">{tt('Расталмады', 'Не подтверждено')}</span>
      : r.status === 'waitlist' ? <span className="badge b-line">{tt('Күту', 'Ожидание')}</span> : null;
  return (
    <div className="row">
      <span className="row-main">
        <span className="row-title">{r.name || (r.username ? '@' + r.username : '#' + r.user_id)}</span>
        <span className="row-sub">{[r.faculty, r.course ? tt(`${r.course}-курс`, `${r.course} курс`) : '', r.ticket_short.toUpperCase()].filter(Boolean).join(' · ')}</span>
        {status ? <span className="badges">{status}</span> : null}
      </span>
      {active && r.status === 'registered' ? (
        r.checked_in_at && !r.checkin_revoked
          ? <button className="btn ghost reg-btn" type="button" disabled={busy} onClick={onRevoke}>{tt('Растамау', 'Не подтв.')}</button>
          : !r.checkin_revoked ? <button className="btn secondary reg-btn" type="button" disabled={busy} onClick={onIn}>{tt('Келді', 'Пришёл')}</button> : null
      ) : null}
    </div>
  );
}

/** full-screen rotating QR; polls /checkin/live every 4 s and redraws when the token changes */
function LiveOverlay({ id, onClose }: { id: number; onClose: () => void }) {
  const { tt, lang } = useApp();
  const [v, setV] = useState<LiveView | null>(null);
  const [err, setErr] = useState('');
  const [left, setLeft] = useState(0);
  useEffect(() => {
    let live = true;
    const poll = () => api<LiveView>(`/api/events/${id}/checkin/live`, { lang }).then(r => { if (live) { setV(r); setLeft(r.rotates_in_sec); setErr(''); } }, e => { if (live) setErr(errText(e)); });
    void poll();
    const t = setInterval(poll, 4000);
    const c = setInterval(() => setLeft(x => Math.max(0, x - 1)), 1000);
    return () => { live = false; clearInterval(t); clearInterval(c); };
  }, [id, lang]);
  useEffect(() => {
    const on = (e: KeyboardEvent) => { if (e.key === 'Escape') onClose(); };
    window.addEventListener('keydown', on);
    return () => window.removeEventListener('keydown', on);
  }, [onClose]);
  return (
    <div className="live" role="dialog" aria-modal="true" aria-label={tt('Тірі QR', 'Живой QR')}>
      <div className="live-in">
        <div className="live-counts num"><span><b>{v?.checked_in ?? '–'}</b> {tt('келді', 'пришли')}</span><span><b>{v?.registered ?? '–'}</b> {tt('тіркелген', 'записались')}</span></div>
        {v ? <Qr big text={v.deep_link || v.web_link} /> : <div className="qr qr-big" />}
        <p className="small">{tt('Телефон камерасымен сканерлеңіз', 'Отсканируйте камерой телефона')} · <span className="num">{tt(`QR ${left} с кейін жаңарады`, `QR обновится через ${left} с`)}</span></p>
        <ErrorLine err={err} />
        <p className="tiny muted">{v ? (lang === 'ru' ? v.notice_ru : v.notice_kk) : ''}</p>
        <button className="btn block" type="button" onClick={onClose}>{tt('Жабу', 'Закрыть')}</button>
      </div>
    </div>
  );
}

interface Detector { detect(src: CanvasImageSource): Promise<{ rawValue: string }[]> }
declare global { interface Window { BarcodeDetector?: new (o: { formats: string[] }) => Detector } }

/** camera ticket scanner (BarcodeDetector where available) with manual code entry as the fallback */
function ScanOverlay({ onTicket, onClose }: { onTicket: (t: string) => Promise<CheckinResult>; onClose: () => void }) {
  const { tt } = useApp();
  const video = useRef<HTMLVideoElement>(null);
  const [camErr, setCamErr] = useState(() => (typeof window !== 'undefined' && !window.BarcodeDetector ? 'unsupported' : ''));
  const [code, setCode] = useState('');
  const [last, setLast] = useState<{ ok: boolean; text: string } | null>(null);
  const busy = useRef(false);
  const seen = useRef<Map<string, number>>(new Map());
  const submit = async (t: string) => {
    const ticket = t.trim();
    if (!ticket || busy.current) return;
    busy.current = true;
    try {
      const r = await onTicket(ticket);
      setLast({ ok: true, text: r.status === 'already' ? tt('Бұрын тіркелген', 'Уже отмечен') : tt('Келді', 'Пришёл') + (r.team_no ? ` · ${tt('команда', 'команда')} ${r.team_no}` : '') });
      setCode('');
    } catch (e) { setLast({ ok: false, text: errText(e) }); } finally { busy.current = false; }
  };
  const submitRef = useRef(submit);
  useEffect(() => { submitRef.current = submit; });
  useEffect(() => {
    if (!window.BarcodeDetector || !navigator.mediaDevices?.getUserMedia) return;
    let stream: MediaStream | null = null;
    let stop = false;
    const det = new window.BarcodeDetector({ formats: ['qr_code'] });
    navigator.mediaDevices.getUserMedia({ video: { facingMode: 'environment' }, audio: false }).then(s => {
      if (stop) { s.getTracks().forEach(t => t.stop()); return; }
      stream = s;
      const v = video.current;
      if (!v) return;
      v.srcObject = s;
      void v.play().catch(() => { /* autoplay blocked: the frame loop still runs once it starts */ });
      const tick = async () => {
        if (stop) return;
        try {
          if (v.readyState >= 2) {
            const found = await det.detect(v);
            const raw = found[0]?.rawValue?.trim();
            const now = Date.now();
            if (raw && (seen.current.get(raw) ?? 0) < now - 5000) { seen.current.set(raw, now); await submitRef.current(raw); }
          }
        } catch { /* a frame that failed to decode */ }
        setTimeout(tick, 350);
      };
      void tick();
    }, () => setCamErr('denied'));
    return () => { stop = true; stream?.getTracks().forEach(t => t.stop()); };
  }, []);
  return (
    <div className="live" role="dialog" aria-modal="true" aria-label={tt('Билет сканері', 'Сканер билетов')}>
      <div className="live-in">
        <h2>{tt('Студент билетін сканерлеу', 'Сканирование билета студента')}</h2>
        {camErr ? <p className="note">{camErr === 'unsupported'
          ? tt('Бұл браузерде камерамен сканерлеу жоқ. Билеттегі кодты қолмен енгізіңіз.', 'В этом браузере нет сканера камеры. Введите код с билета вручную.')
          : tt('Камераға рұқсат берілмеді. Кодты қолмен енгізіңіз.', 'Нет доступа к камере. Введите код вручную.')}</p>
          : <video ref={video} className="scan-video" muted playsInline />}
        <form className="form" onSubmit={e => { e.preventDefault(); void submit(code); }}>
          <div className="field"><label htmlFor="scanCode">{tt('Билеттегі код (6 таңба не толық)', 'Код с билета (6 символов или полный)')}</label>
            <input className="input" id="scanCode" autoComplete="off" autoCapitalize="none" spellCheck={false} value={code} onChange={e => setCode(e.target.value)} /></div>
          <button className="btn" type="submit" disabled={!code.trim()}>{tt('Келді деп белгілеу', 'Отметить пришедшим')}</button>
        </form>
        {last ? <p className={`small ${last.ok ? '' : 'urgent'}`} role="status">{last.text}</p> : null}
        <button className="btn ghost block" type="button" onClick={onClose}>{tt('Жабу', 'Закрыть')}</button>
      </div>
    </div>
  );
}

/** admin shortcut: every event of every university is managed from /organize as well */
export function AdminEventsLink() {
  const { tt } = useApp();
  return <AdminOnly><Row go="organize" icon="calendar" title={tt('Іс-шаралар (ұйымдастыру)', 'Мероприятия (организация)')} /></AdminOnly>;
}
