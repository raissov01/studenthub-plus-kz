'use client';
/* «Клубтар» (FEATURE_CLUBS): the catalogue (/clubs) with direction / building / search filters, the club page
   (/club?id=<id>, query id so the one saved page opens offline), joining (request or the club's Telegram group),
   the club admin tools (page editor, join requests, members, moderated news, club events through the events flow),
   the «Клуб жетекшісімін» application and the site admin list (/admin/clubs). API: docs/clubs.md. */
import { useState } from 'react';
import { useApp } from '@/components/app';
import { DeskDetail, DeskList } from '@/components/desk';
import { useDesktop } from '@/lib/desktop';
import { Chip, Empty, Icon, Row, SectionHead } from '@/components/ui';
import { AdminOnly, ErrorLine, errText, Feature, Loading, NotFound, SignInCard, useFeature, useLoad, useNeedAccount, ymd } from '@/components/views/common';
import { useQueryId } from '@/components/views/subjects';
import { DbEventCard, EventForm } from '@/components/views/events';
import { api, useAccount } from '@/lib/account';
import { BUILDINGS } from '@/lib/data/campus';
import { useOnline } from '@/lib/pwa';
import type { DbEvent } from '@/lib/events';
import {
  clubAbout, clubLeader, clubName, clubSchedule, clubSummary, directionOf, DIRECTIONS, filterClubs, NO_FILTER, postBody, safeSocial,
  usedBuildings, usedDirections, type AdminClub, type Club, type ClubDetail, type ClubFilter, type ClubMember,
} from '@/lib/clubs';

const EMPTY_KK = 'Клубтар әлі қосылмаған — клуб жетекшісі болсаңыз, өтінім беріңіз';
const EMPTY_RU = 'Клубы ещё не добавлены — если вы руководитель клуба, подайте заявку';

function DirBadge({ d }: { d: string }) {
  const { L } = useApp();
  const x = directionOf(d);
  return <span className={`badge dir-${x.tone}`}>{L(x.label)}</span>;
}

function RecruitBadge({ on }: { on: boolean }) {
  const { tt } = useApp();
  return on ? <span className="badge b-ok"><Icon n="check" s={12} /> {tt('Мүше қабылдап жатыр', 'Идёт набор')}</span> : null;
}

const buildingName = (id: string | null, L: (t: { kk: string; ru: string }) => string) => {
  const b = BUILDINGS.find(x => x.id === id);
  return b ? L(b.name) : '';
};

/* ---------- catalogue ---------- */
export function ClubsScreen() {
  return <Feature name="clubs"><Catalogue /></Feature>;
}

function Catalogue() {
  const { tt, lang, L, openSheet } = useApp();
  const desktop = useDesktop();
  const acc = useAccount();
  const q = useLoad<{ clubs: Club[] }>(`/api/clubs${acc.user ? '?u=' + acc.user.id : ''}`);
  const [f, setF] = useState<ClubFilter>(NO_FILTER);
  const need = useNeedAccount();
  const apply = () => { if (need()) openSheet(<LeaderApplySheet />); };
  if (q.err && !q.data) return <ErrorLine err={q.err} />;
  if (!q.data) return <Loading />;
  const all = q.data.clubs;
  const applyBtn = <button className="btn ghost" type="button" id="clubLeaderApply" onClick={apply}><Icon n="award" s={18} /> {tt('Клуб жетекшісімін', 'Я руководитель клуба')}</button>;
  if (!all.length) return <Empty icon="users" action={applyBtn}>{tt(EMPTY_KK, EMPTY_RU)}</Empty>;
  const list = filterClubs(all, f, lang);
  const mine = all.filter(c => c.my?.admin || c.my?.status === 'member');
  const dirs = usedDirections(all);
  const bs = usedBuildings(all);
  const mineEl = mine.length ? (
    <section className="section">
      <SectionHead title={tt('Менің клубтарым', 'Мои клубы')} />
      <div className="group">{mine.map(c => <Row key={c.id} go={'club:' + c.id} icon="users" title={clubName(c, lang)}
        sub={c.my?.admin ? tt('Сіз жетекшісіз', 'Вы руководитель') : tt('Мүшесіз', 'Вы участник')} />)}</div>
    </section>
  ) : null;
  const filters = (
    <>
      <div className="search" role="search">
        <Icon n="search" s={18} />
        <input className="input" id="clubQ" type="search" autoComplete="off" value={f.q} onChange={e => setF({ ...f, q: e.target.value })}
          placeholder={tt('Клуб атауы не тақырып', 'Название клуба или тема')} aria-label={tt('Клуб іздеу', 'Поиск клуба')} />
      </div>
      <div className="chips club-dirs" role="group" aria-label={tt('Бағыты', 'Направление')}>
        <Chip pressed={f.direction === 'all'} onClick={() => setF({ ...f, direction: 'all' })}>{tt('Барлығы', 'Все')}</Chip>
        {dirs.map(d => <Chip key={d.id} pressed={f.direction === d.id} onClick={() => setF({ ...f, direction: d.id })}>{L(d.label)}</Chip>)}
        <Chip pressed={!!f.recruiting} onClick={() => setF({ ...f, recruiting: !f.recruiting })}>{tt('Мүше қабылдап жатыр', 'Идёт набор')}</Chip>
      </div>
      {bs.length ? (
        <div className="field">
          <label htmlFor="clubBuilding">{tt('Корпус', 'Корпус')}</label>
          <select className="input" id="clubBuilding" value={f.building} onChange={e => setF({ ...f, building: e.target.value })}>
            <option value="all">{tt('Кез келген корпус', 'Любой корпус')}</option>
            {bs.map(b => <option key={b} value={b}>{buildingName(b, L)}</option>)}
          </select>
        </div>
      ) : null}
    </>
  );
  const results = (
    <>
      <p className="tiny muted num" role="status">{tt(`Табылды: ${list.length}`, `Найдено: ${list.length}`)}</p>
      {list.length
        ? <div className="list" id="clubList">{list.map(c => <ClubCard key={c.id} c={c} />)}</div>
        : <Empty icon="search">{tt('Бұл сүзгі бойынша клуб жоқ.', 'По этому фильтру клубов нет.')}</Empty>}
    </>
  );
  const foot = (
    <>
      <p className="note">{tt('Клубты басқарасыз ба? Өтінім беріңіз — модератор тексерген соң клуб бетін өзіңіз жүргізесіз.', 'Руководите клубом? Подайте заявку — после проверки модератором вы будете вести страницу клуба.')}</p>
      {applyBtn}
    </>
  );
  // a laptop: search and filters in a left panel (with «I lead a club»), my clubs and the catalogue beside them
  if (desktop) return <DeskList label={tt('Сүзгілер', 'Фильтры')} filters={<>{filters}{foot}</>}>{mineEl}{results}</DeskList>;
  return <>{mineEl}{filters}{results}{foot}</>;
}

function ClubCard({ c }: { c: Club }) {
  const { tt, lang, L, go } = useApp();
  const where = [buildingName(c.building_id, L), c.room].filter(Boolean).join(', ');
  const when = clubSchedule(c, lang);
  return (
    <button className="card club-card" type="button" onClick={() => go('club:' + c.id)}>
      <span className="row-title">{clubName(c, lang)}</span>
      <span className="badges"><DirBadge d={c.direction} /><RecruitBadge on={c.recruiting} />
        {c.my?.status === 'pending' ? <span className="badge b-line">{tt('Өтінім жіберілді', 'Заявка отправлена')}</span> : null}</span>
      {clubSummary(c, lang) ? <span className="small">{clubSummary(c, lang)}</span> : null}
      {where || when ? <span className="row-sub">{[where, when].filter(Boolean).join(' · ')}</span> : null}
      {(() => {
        const foot = [clubLeader(c, lang), c.members ? tt(`${c.members} мүше`, `Участников: ${c.members}`) : '',
          safeSocial(c.telegram_url) ? 'Telegram' : '', safeSocial(c.instagram_url) ? 'Instagram' : ''].filter(Boolean).join(' · ');
        return foot ? <span className="tiny muted">{foot}</span> : null;
      })()}
    </button>
  );
}

/* ---------- club page ---------- */
export function ClubScreen() {
  const id = useQueryId();
  return <Feature name="clubs">{id && /^\d+$/.test(id) ? <ClubPage id={id} /> : id ? <NotFound /> : <Catalogue />}</Feature>;
}

function ClubPage({ id }: { id: string }) {
  const { tt, lang, L, toast, openSheet, go, setUi } = useApp();
  const acc = useAccount();
  const need = useNeedAccount();
  const online = useOnline();
  const eventsOn = useFeature('events');
  const d = useLoad<ClubDetail>(`/api/clubs/${id}${acc.user ? '?u=' + acc.user.id : ''}`);
  const evs = useLoad<{ events: DbEvent[] }>(eventsOn ? `/api/events?club=${id}${acc.user ? '&u=' + acc.user.id : ''}` : null);
  const [busy, setBusy] = useState(false);
  const desktop = useDesktop();
  if (d.code === 'not_found' || d.code === 'http_404') return <NotFound />;
  if (d.err && !d.data) return <ErrorLine err={d.err} />;
  if (!d.data) return <Loading />;
  const { club: c, posts, can_manage: manage } = d.data;
  const my = c.my;
  const reload = () => { d.reload(); evs.reload(); };
  const b = BUILDINGS.find(x => x.id === c.building_id);
  const tg = safeSocial(c.telegram_url), ig = safeSocial(c.instagram_url), joinLink = safeSocial(c.join_link);
  const leaveClub = async () => {
    setBusy(true);
    try {
      await api(`/api/clubs/${c.id}/join`, { method: 'DELETE', lang });
      toast(my?.status === 'member' ? tt('Клубтан шықтыңыз', 'Вы вышли из клуба') : tt('Өтінім қайтарылды', 'Заявка отозвана'));
      reload();
    } catch (e) { toast(errText(e)); } finally { setBusy(false); }
  };
  const head = (
    <div className="detail-head"><h1>{clubName(c, lang)}</h1>
      <div className="badges"><DirBadge d={c.direction} /><RecruitBadge on={c.recruiting} />
        {c.status === 'archived' ? <span className="badge b-line">{tt('Архивте', 'В архиве')}</span> : null}
        {my?.admin ? <span className="badge b-sun">{tt('Сіз жетекшісіз', 'Вы руководитель')}</span> : null}</div>
    </div>
  );
  const summary = clubSummary(c, lang) ? <p>{clubSummary(c, lang)}</p> : null;
  const about = clubAbout(c, lang) ? <p style={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>{clubAbout(c, lang)}</p> : null;
  const facts = (
    <div className="group">
      {clubSchedule(c, lang) ? <Row icon="clock" title={clubSchedule(c, lang)} sub={tt('Кездесу кестесі', 'Расписание встреч')} chev={false} /> : null}
      {b ? <Row icon="pin" title={[L(b.name), c.room].filter(Boolean).join(', ')} sub={tt('Кампус картасынан көру', 'Посмотреть на карте кампуса')}
        onClick={() => { setUi({ mapSel: b.id, mapQ: '' }); go('map'); }} />
        : c.room ? <Row icon="pin" title={c.room} chev={false} /> : null}
      {clubLeader(c, lang) || c.leader_contact ? <Row icon="user" title={clubLeader(c, lang) || tt('Жетекші', 'Руководитель')}
        sub={c.leader_contact ? c.leader_contact + (manage && !c.leader_contact_public ? ' · ' + tt('тек сізге көрінеді', 'видно только вам') : '') : undefined} chev={false} /> : null}
      <Row icon="users" title={tt(`Мүшелер: ${c.members}`, `Участников: ${c.members}`)} chev={false} />
      {tg ? <Row icon="send" title="Telegram" sub={tg.replace('https://', '')} href={tg} /> : null}
      {ig ? <Row icon="camera" title="Instagram" sub={ig.replace(/^https:\/\/(www\.)?/, '')} href={ig} /> : null}
    </div>
  );
  const join = c.status === 'active' ? (
    c.join_mode === 'link' && joinLink ? (
      <a className="btn block" id="clubJoin" href={joinLink} target="_blank" rel="noopener noreferrer"><Icon n="send" s={18} /> {tt('Telegram тобына қосылу', 'Вступить в Telegram-группу')}</a>
    ) : my?.status === 'member' ? (
      <div className="card"><span className="badge b-ok"><Icon n="check" s={12} /> {tt('Сіз клуб мүшесісіз', 'Вы участник клуба')}</span>
        <button className="btn ghost" type="button" disabled={busy || !online} onClick={leaveClub}>{tt('Клубтан шығу', 'Выйти из клуба')}</button></div>
    ) : my?.status === 'pending' ? (
      <div className="card" id="clubPending"><b>{tt('Өтінім жіберілді', 'Заявка отправлена')}</b>
        <span className="small muted">{tt('Жетекші қараған соң хабарлама келеді.', 'Когда руководитель рассмотрит, придёт уведомление.')}</span>
        <button className="btn ghost" type="button" disabled={busy || !online} onClick={leaveClub}>{tt('Өтінімді қайтарып алу', 'Отозвать заявку')}</button></div>
    ) : !my?.admin ? (
      <div className="sticky-cta"><button className="btn block" type="button" id="clubJoin" disabled={!online}
        onClick={() => { if (need()) openSheet(<JoinSheet c={c} onDone={reload} />); }}><Icon n="plus" s={18} /> {tt('Қосылу', 'Вступить')}</button></div>
    ) : null
  ) : null;
  const signIn = !acc.user && online ? <SignInCard text={tt('Клубқа қосылу үшін аккаунтқа кіріңіз.', 'Чтобы вступить в клуб, войдите в аккаунт.')} /> : null;
  const managePanel = manage ? <ManagePanel d={d.data} onChange={reload} /> : null;
  const events = eventsOn ? (
    <section className="section">
      <SectionHead title={tt('Алдағы іс-шаралар', 'Ближайшие мероприятия')} />
      {evs.data?.events?.length ? <div className="list" id="clubEvents">{evs.data.events.map(e => <DbEventCard key={e.id} e={e} />)}</div>
        : evs.data ? <p className="small muted">{tt('Әзірге жоспарланған іс-шара жоқ.', 'Запланированных мероприятий пока нет.')}</p> : null}
    </section>
  ) : null;
  const news = (
    <section className="section">
      <SectionHead title={tt('Клуб жаңалықтары', 'Новости клуба')} />
      {posts.length ? <div className="list" id="clubNews">{posts.map(p => (
        <div className="card" key={p.id}>
          <span className="tiny muted">{ymd(p.published_at || p.created_at)}{p.status === 'pending' ? ' · ' + tt('Модерацияда', 'На модерации') : ''}</span>
          <span style={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>{postBody(p, lang)}</span>
          {p.link_url && /^https:\/\//.test(p.link_url) ? <a className="small" href={p.link_url} target="_blank" rel="noopener noreferrer">{tt('Толығырақ', 'Подробнее')}</a> : null}
        </div>
      ))}</div> : <p className="small muted">{tt('Жаңалық әзірге жоқ.', 'Новостей пока нет.')}</p>}
    </section>
  );
  const source = c.source_url && /^https:\/\//.test(c.source_url) ? (
    <p className="tiny muted">{tt('Дереккөз', 'Источник')}: <a href={c.source_url} target="_blank" rel="noopener noreferrer">{c.source_url.replace('https://', '').slice(0, 60)}</a>
      {c.verified_at ? ` · ${tt('тексерілді', 'проверено')} ${c.verified_at}` : ''}</p>
  ) : null;
  const leaderApply = acc.user && !my?.admin && c.status === 'active' ? (
    <button className="link-btn" type="button" onClick={() => { if (need()) openSheet(<LeaderApplySheet club={c} />); }}>{tt('Мен осы клубтың жетекшісімін', 'Я руководитель этого клуба')}</button>
  ) : null;
  if (desktop) {
    // a laptop: a cover card with the name and the summary; about, events and news in the column; joining,
    // the meeting time and place, the leader and the contacts in the rail
    return (
      <>
        <div className={`card club-cover club-dir-${c.direction}`} id="clubCover">{head}{summary}</div>
        <DeskDetail label={tt('Клуб туралы', 'О клубе')} main={<>
          {managePanel}
          {about ? <section className="section"><SectionHead title={tt('Клуб туралы', 'О клубе')} />{about}</section> : null}
          {events}
          {news}
          {source}
        </>} rail={<>
          {join}
          {signIn}
          {facts}
          {leaderApply}
        </>} />
      </>
    );
  }
  return (
    <>
      {head}
      {summary}
      {about}
      {facts}
      {join}
      {signIn}
      {managePanel}
      {events}
      {news}
      {source}
      {leaderApply}
    </>
  );
}

function JoinSheet({ c, onDone }: { c: Club; onDone: () => void }) {
  const { tt, lang, toast, closeSheet } = useApp();
  const [msg, setMsg] = useState('');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  return (
    <form className="form" onSubmit={async e => {
      e.preventDefault();
      setBusy(true); setErr('');
      try {
        await api(`/api/clubs/${c.id}/join`, { method: 'POST', body: { message: msg.trim() }, lang });
        closeSheet(); toast(tt('Өтінім жіберілді', 'Заявка отправлена')); onDone();
      } catch (e2) { setErr(errText(e2)); } finally { setBusy(false); }
    }}>
      <h2>{tt('Клубқа өтінім', 'Заявка в клуб')}</h2>
      <p className="small muted">{tt('Жетекші сіздің атыңызды, факультетіңізді және курсыңызды көреді. Шешім хабарлама болып келеді.', 'Руководитель увидит ваше имя, факультет и курс. Решение придёт уведомлением.')}</p>
      <div className="field"><label htmlFor="joinMsg">{tt('Өзіңіз туралы бір-екі сөз (міндетті емес)', 'Пара слов о себе (необязательно)')}</label>
        <textarea className="input" id="joinMsg" maxLength={300} value={msg} onChange={e => setMsg(e.target.value)} /></div>
      <ErrorLine err={err} />
      <div className="btn-row">
        <button className="btn ghost" type="button" onClick={closeSheet}>{tt('Бас тарту', 'Отмена')}</button>
        <button className="btn" type="submit" disabled={busy}>{tt('Өтінім жіберу', 'Отправить заявку')}</button>
      </div>
    </form>
  );
}

/* ---------- club admin ---------- */
function ManagePanel({ d, onChange }: { d: ClubDetail; onChange: () => void }) {
  const { tt, openSheet } = useApp();
  const eventsOn = useFeature('events');
  const [eventForm, setEventForm] = useState(false);
  const c = d.club;
  return (
    <section className="section" aria-label={tt('Клубты басқару', 'Управление клубом')}>
      <SectionHead title={tt('Клубты басқару', 'Управление клубом')} />
      <div className="group">
        <Row icon="users" title={tt('Өтінімдер мен мүшелер', 'Заявки и участники')} end={d.pending_requests ? <b className="num" id="clubPendingCount">{d.pending_requests}</b> : undefined}
          onClick={() => openSheet(<MembersSheet clubId={c.id} onChange={onChange} />)} />
        <Row icon="edit" title={tt('Клуб бетін өзгерту', 'Изменить страницу клуба')} onClick={() => openSheet(<ClubEditSheet c={c} onDone={onChange} />)} />
        <Row icon="chat" title={tt('Жаңалық жариялау', 'Опубликовать новость')} sub={tt('Модерациядан кейін шығады', 'Появится после модерации')}
          onClick={() => openSheet(<PostSheet clubId={c.id} onDone={onChange} />)} />
        {eventsOn ? <Row icon="calendar" title={tt('Іс-шара қосу', 'Добавить мероприятие')} sub={tt('QR арқылы келу, күнтізбе, ұпайлар', 'Отметка по QR, календарь, баллы')}
          onClick={() => setEventForm(v => !v)} /> : null}
      </div>
      {eventForm ? <div className="card"><EventForm clubId={c.id} /></div> : null}
      <p className="tiny muted">{tt('Мүшелер тізімін тек клуб жетекшілері көреді.', 'Список участников видят только руководители клуба.')}</p>
    </section>
  );
}

function MembersSheet({ clubId, onChange }: { clubId: number; onChange: () => void }) {
  const { tt, lang, toast } = useApp();
  const q = useLoad<{ members: ClubMember[] }>(`/api/clubs/${clubId}/members`);
  const [busy, setBusy] = useState(0);
  const act = async (m: ClubMember, how: 'approve' | 'decline' | 'remove') => {
    setBusy(m.user_id);
    try {
      if (how === 'remove') await api(`/api/clubs/${clubId}/members/${m.user_id}`, { method: 'DELETE', lang });
      else await api(`/api/clubs/${clubId}/requests/${m.user_id}`, { method: 'POST', body: { decision: how }, lang });
      toast(how === 'approve' ? tt('Қабылданды. Студентке хабар кетті', 'Принят. Студент уведомлён') : how === 'decline' ? tt('Өтінім қабылданбады', 'Заявка отклонена') : tt('Мүше шығарылды', 'Участник удалён'));
      q.reload(); onChange();
    } catch (e) { toast(errText(e)); } finally { setBusy(0); }
  };
  const who = (m: ClubMember) => [m.name || tt('Атсыз', 'Без имени'), m.username ? '@' + m.username : ''].filter(Boolean).join(' ');
  const info = (m: ClubMember) => [m.faculty, m.course ? tt(`${m.course}-курс`, `${m.course} курс`) : ''].filter(Boolean).join(', ');
  if (q.err) return <ErrorLine err={q.err} />;
  if (!q.data) return <Loading />;
  const pending = q.data.members.filter(m => m.status === 'pending'), members = q.data.members.filter(m => m.status === 'member');
  return (
    <>
      <h2>{tt('Өтінімдер', 'Заявки')} ({pending.length})</h2>
      {pending.length ? <div className="list" id="clubRequests">{pending.map(m => (
        <div className="card" key={m.user_id}>
          <b>{who(m)}</b>{info(m) ? <span className="small muted">{info(m)}</span> : null}
          {m.message ? <span className="small" style={{ overflowWrap: 'anywhere' }}>{m.message}</span> : null}
          <div className="btn-row">
            <button className="btn ghost" type="button" disabled={!!busy} onClick={() => act(m, 'decline')}>{tt('Бас тарту', 'Отклонить')}</button>
            <button className="btn" type="button" disabled={!!busy} onClick={() => act(m, 'approve')}>{tt('Қабылдау', 'Принять')}</button>
          </div>
        </div>
      ))}</div> : <p className="small muted">{tt('Жаңа өтінім жоқ.', 'Новых заявок нет.')}</p>}
      <h2>{tt('Мүшелер', 'Участники')} ({members.length})</h2>
      {members.length ? <div className="group">{members.map(m => (
        <Row key={m.user_id} icon="user" title={who(m)} sub={info(m) || undefined} chev={false}
          end={<button className="link-btn" type="button" disabled={!!busy} onClick={() => act(m, 'remove')}>{tt('Шығару', 'Удалить')}</button>} />
      ))}</div> : <p className="small muted">{tt('Әзірге мүше жоқ.', 'Участников пока нет.')}</p>}
    </>
  );
}

const EDIT_KEYS = ['summary_kk', 'summary_ru', 'about_kk', 'about_ru', 'building_id', 'room', 'schedule_kk', 'schedule_ru', 'leader_title_kk',
  'leader_title_ru', 'leader_contact', 'telegram_url', 'instagram_url', 'join_link'] as const;
type EditKey = typeof EDIT_KEYS[number];

function ClubEditSheet({ c, onDone }: { c: Club; onDone: () => void }) {
  const { tt, lang, L, toast, closeSheet } = useApp();
  const [f, setF] = useState<Record<EditKey, string>>(() => Object.fromEntries(EDIT_KEYS.map(k => [k, (c[k] as string | null) ?? ''])) as Record<EditKey, string>);
  const [mode, setMode] = useState(c.join_mode);
  const [recruiting, setRecruiting] = useState(c.recruiting);
  const [pub, setPub] = useState(c.leader_contact_public);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  const text = (k: EditKey, label: string, max: number, area = false) => (
    <div className="field"><label htmlFor={'ce-' + k}>{label}</label>
      {area ? <textarea className="input" id={'ce-' + k} maxLength={max} value={f[k]} onChange={e => setF({ ...f, [k]: e.target.value })} />
        : <input className="input" id={'ce-' + k} maxLength={max} value={f[k]} onChange={e => setF({ ...f, [k]: e.target.value })} />}</div>
  );
  return (
    <form className="form" onSubmit={async e => {
      e.preventDefault();
      setBusy(true); setErr('');
      try {
        const body: Record<string, unknown> = { join_mode: mode, recruiting, leader_contact_public: pub };
        for (const k of EDIT_KEYS) body[k] = f[k].trim() || null;
        await api(`/api/clubs/${c.id}`, { method: 'PATCH', body, lang });
        closeSheet(); toast(tt('Сақталды', 'Сохранено')); onDone();
      } catch (e2) { setErr(errText(e2)); } finally { setBusy(false); }
    }}>
      <h2>{tt('Клуб беті', 'Страница клуба')}</h2>
      <label className="check"><input type="checkbox" id="ce-recruiting" checked={recruiting} onChange={e => setRecruiting(e.target.checked)} /><span className="small">{tt('Мүше қабылдап жатырмыз', 'Идёт набор участников')}</span></label>
      {text('summary_kk', tt('Қысқаша (қазақша, карточкада)', 'Кратко (на казахском, в карточке)'), 300, true)}
      {text('summary_ru', tt('Қысқаша (орысша)', 'Кратко (на русском)'), 300, true)}
      {text('about_kk', tt('Біз туралы (қазақша)', 'О клубе (на казахском)'), 4000, true)}
      {text('about_ru', tt('Біз туралы (орысша)', 'О клубе (на русском)'), 4000, true)}
      <div className="field"><label htmlFor="ce-building_id">{tt('Корпус', 'Корпус')}</label>
        <select className="input" id="ce-building_id" value={f.building_id} onChange={e => setF({ ...f, building_id: e.target.value })}>
          <option value="">{tt('Көрсетілмеген', 'Не указан')}</option>
          {BUILDINGS.map(b => <option key={b.id} value={b.id}>{b.n}. {L(b.name)}</option>)}
        </select></div>
      {text('room', tt('Аудитория', 'Аудитория'), 60)}
      {text('schedule_kk', tt('Кесте (мысалы «Сейсенбі, 18:00»)', 'Расписание (на казахском)'), 200)}
      {text('schedule_ru', tt('Кесте (орысша)', 'Расписание (например «Вторник, 18:00»)'), 200)}
      {text('leader_title_kk', tt('Жетекшінің рөлі (аты емес, мысалы «Төраға»)', 'Роль руководителя (на казахском)'), 80)}
      {text('leader_title_ru', tt('Жетекшінің рөлі (орысша)', 'Роль руководителя (не имя, например «Председатель»)'), 80)}
      {text('leader_contact', tt('Байланыс (мысалы @username)', 'Контакт (например @username)'), 120)}
      <label className="check"><input type="checkbox" checked={pub} onChange={e => setPub(e.target.checked)} /><span className="small">{tt('Байланысымды барлығына көрсету', 'Показывать мой контакт всем')}</span></label>
      {text('telegram_url', 'Telegram (https://t.me/…)', 120)}
      {text('instagram_url', 'Instagram (https://instagram.com/…)', 120)}
      <div className="field"><label htmlFor="ce-join_mode">{tt('Қалай қосылады', 'Как вступают')}</label>
        <select className="input" id="ce-join_mode" value={mode} onChange={e => setMode(e.target.value as Club['join_mode'])}>
          <option value="request">{tt('Өтінім арқылы (мен бекітемін)', 'По заявке (я одобряю)')}</option>
          <option value="link">{tt('Telegram тобына тікелей сілтеме', 'Прямая ссылка на Telegram-группу')}</option>
        </select></div>
      {mode === 'link' ? text('join_link', tt('Топқа сілтеме (https://t.me/…)', 'Ссылка на группу (https://t.me/…)'), 120) : null}
      <ErrorLine err={err} />
      <div className="btn-row">
        <button className="btn ghost" type="button" onClick={closeSheet}>{tt('Бас тарту', 'Отмена')}</button>
        <button className="btn" type="submit" disabled={busy}>{tt('Сақтау', 'Сохранить')}</button>
      </div>
    </form>
  );
}

function PostSheet({ clubId, onDone }: { clubId: number; onDone: () => void }) {
  const { tt, lang, toast, closeSheet } = useApp();
  const [kk, setKk] = useState(''), [ru, setRu] = useState(''), [link, setLink] = useState('');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  return (
    <form className="form" onSubmit={async e => {
      e.preventDefault();
      setBusy(true); setErr('');
      try {
        await api(`/api/clubs/${clubId}/posts`, { method: 'POST', body: { body_kk: kk.trim(), body_ru: ru.trim(), link_url: link.trim() }, lang });
        closeSheet(); toast(tt('Модерацияға жіберілді', 'Отправлено на модерацию')); onDone();
      } catch (e2) { setErr(errText(e2)); } finally { setBusy(false); }
    }}>
      <h2>{tt('Клуб жаңалығы', 'Новость клуба')}</h2>
      <div className="field"><label htmlFor="cpKk">{tt('Мәтін (қазақша)', 'Текст (на казахском)')}</label>
        <textarea className="input" id="cpKk" maxLength={2000} value={kk} onChange={e => setKk(e.target.value)} required /></div>
      <div className="field"><label htmlFor="cpRu">{tt('Мәтін (орысша, міндетті емес)', 'Текст (на русском, необязательно)')}</label>
        <textarea className="input" id="cpRu" maxLength={2000} value={ru} onChange={e => setRu(e.target.value)} /></div>
      <div className="field"><label htmlFor="cpLink">{tt('Сілтеме (https://, міндетті емес)', 'Ссылка (https://, необязательно)')}</label>
        <input className="input" id="cpLink" type="url" maxLength={500} value={link} onChange={e => setLink(e.target.value)} /></div>
      <ErrorLine err={err} />
      <div className="btn-row">
        <button className="btn ghost" type="button" onClick={closeSheet}>{tt('Бас тарту', 'Отмена')}</button>
        <button className="btn" type="submit" disabled={busy || kk.trim().length < 3}>{tt('Жіберу', 'Отправить')}</button>
      </div>
    </form>
  );
}

/* ---------- «Клуб жетекшісімін» ---------- */
export function LeaderApplySheet({ club }: { club?: Club }) {
  const { tt, lang, L, toast, closeSheet } = useApp();
  const [name, setName] = useState(''), [dir, setDir] = useState('other'), [proof, setProof] = useState(''), [url, setUrl] = useState('');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  const ok = proof.trim().length >= 20 && /^https:\/\/\S+$/.test(url.trim()) && (!!club || name.trim().length >= 2);
  return (
    <form className="form" onSubmit={async e => {
      e.preventDefault();
      if (!ok) return;
      setBusy(true); setErr('');
      try {
        const body = club ? { club_id: club.id, proof_text: proof.trim(), proof_url: url.trim() } : { name_kk: name.trim(), direction: dir, proof_text: proof.trim(), proof_url: url.trim() };
        await api('/api/clubs/leader-applications', { method: 'POST', body, lang });
        closeSheet(); toast(tt('Өтінім модерацияға жіберілді', 'Заявка отправлена на модерацию'));
      } catch (e2) { setErr(errText(e2)); } finally { setBusy(false); }
    }}>
      <h2>{club ? tt(`«${clubName(club, lang)}» жетекшісімін`, `Я руководитель «${clubName(club, lang)}»`) : tt('Клуб жетекшісімін', 'Я руководитель клуба')}</h2>
      <p className="small muted">{tt('Модератор дәлелді тексереді. Бекітілсе, клуб бетін, жаңалықтарын, іс-шараларын және өтінімдерді өзіңіз басқарасыз.', 'Модератор проверит подтверждение. После одобрения вы будете вести страницу клуба, новости, мероприятия и заявки.')}</p>
      {!club ? <>
        <div className="field"><label htmlFor="laName">{tt('Клуб атауы', 'Название клуба')}</label>
          <input className="input" id="laName" maxLength={120} value={name} onChange={e => setName(e.target.value)} required /></div>
        <div className="field"><label htmlFor="laDir">{tt('Бағыты', 'Направление')}</label>
          <select className="input" id="laDir" value={dir} onChange={e => setDir(e.target.value)}>
            {DIRECTIONS.map(d => <option key={d.id} value={d.id}>{L(d.label)}</option>)}
          </select></div>
      </> : null}
      <div className="field"><label htmlFor="laProof">{tt('Дәлел: кім екеніңіз, клубта не істейсіз (20–1000 таңба)', 'Подтверждение: кто вы и что делаете в клубе (20–1000 символов)')}</label>
        <textarea className="input" id="laProof" maxLength={1000} value={proof} onChange={e => setProof(e.target.value)} /></div>
      <div className="field"><label htmlFor="laUrl">{tt('Сілтеме (клуб арнасы, университет беті, т.б.)', 'Ссылка (канал клуба, страница университета и т. п.)')}</label>
        <input className="input" id="laUrl" type="url" maxLength={500} placeholder="https://" value={url} onChange={e => setUrl(e.target.value)} /></div>
      <ErrorLine err={err} />
      <div className="btn-row">
        <button className="btn ghost" type="button" onClick={closeSheet}>{tt('Бас тарту', 'Отмена')}</button>
        <button className="btn" type="submit" disabled={!ok || busy}>{tt('Жіберу', 'Отправить')}</button>
      </div>
    </form>
  );
}

/* ---------- site admin ---------- */
export function AdminClubs() {
  return <Feature name="clubs"><AdminOnly><AdminClubsList /></AdminOnly></Feature>;
}

function AdminClubsList() {
  const { tt, lang, L, toast, openSheet } = useApp();
  const q = useLoad<{ clubs: AdminClub[] }>('/api/admin/clubs');
  const [name, setName] = useState(''), [dir, setDir] = useState('other'), [src, setSrc] = useState('');
  const [err, setErr] = useState('');
  const call = async (path: string, method: string, body?: unknown) => {
    try { await api(path, { method, body, lang }); toast(tt('Сақталды', 'Сохранено')); q.reload(); } catch (e) { toast(errText(e)); }
  };
  if (q.err) return <ErrorLine err={q.err} />;
  if (!q.data) return <Loading />;
  return (
    <>
      <form className="form card" onSubmit={async e => {
        e.preventDefault(); setErr('');
        try {
          await api('/api/admin/clubs', { method: 'POST', body: { name_kk: name.trim(), direction: dir, ...(src.trim() ? { source_url: src.trim(), verified_at: new Date().toISOString().slice(0, 10) } : {}) }, lang });
          setName(''); setSrc(''); q.reload();
        } catch (e2) { setErr(errText(e2)); }
      }}>
        <h2>{tt('Клуб қосу', 'Добавить клуб')}</h2>
        <div className="field"><label htmlFor="acName">{tt('Атауы (қазақша)', 'Название (на казахском)')}</label>
          <input className="input" id="acName" maxLength={120} value={name} onChange={e => setName(e.target.value)} required /></div>
        <div className="field"><label htmlFor="acDir">{tt('Бағыты', 'Направление')}</label>
          <select className="input" id="acDir" value={dir} onChange={e => setDir(e.target.value)}>{DIRECTIONS.map(d => <option key={d.id} value={d.id}>{L(d.label)}</option>)}</select></div>
        <div className="field"><label htmlFor="acSrc">{tt('Ресми дереккөз (https://)', 'Официальный источник (https://)')}</label>
          <input className="input" id="acSrc" type="url" value={src} onChange={e => setSrc(e.target.value)} /></div>
        <ErrorLine err={err} />
        <button className="btn" type="submit" disabled={name.trim().length < 2}>{tt('Қосу', 'Добавить')}</button>
      </form>
      {q.data.clubs.length ? <div className="list">{q.data.clubs.map(c => (
        <div className="card" key={c.id}>
          <b>{clubName(c, lang)}</b>
          <span className="badges"><DirBadge d={c.direction} />{c.status === 'archived' ? <span className="badge b-line">{tt('Архивте', 'В архиве')}</span> : null}
            {c.pending_requests ? <span className="badge b-line num">{tt(`Өтінім ${c.pending_requests}`, `Заявок ${c.pending_requests}`)}</span> : null}</span>
          <span className="small muted">{tt('Жетекшілер', 'Руководители')}: {c.admins.length ? c.admins.map(a => (
            <button key={a.user_id} className="link-btn" type="button" onClick={() => call(`/api/admin/clubs/${c.id}/admins/${a.user_id}`, 'DELETE')}
              aria-label={tt(`${a.name ?? a.user_id} жетекшілігін алу`, `Снять руководителя ${a.name ?? a.user_id}`)}>{a.name ?? '#' + a.user_id} ✕</button>
          )) : '—'}</span>
          <div className="btn-row">
            <button className="btn ghost" type="button" onClick={() => openSheet(<GrantSheet onGrant={uid => call(`/api/admin/clubs/${c.id}/admins`, 'POST', { userId: uid })} />)}>{tt('Жетекші тағайындау', 'Назначить руководителя')}</button>
            <button className="btn ghost" type="button" onClick={() => call(`/api/admin/clubs/${c.id}`, 'PATCH', { status: c.status === 'active' ? 'archived' : 'active' })}>
              {c.status === 'active' ? tt('Архивке', 'В архив') : tt('Қалпына келтіру', 'Восстановить')}</button>
          </div>
        </div>
      ))}</div> : <Empty icon="users">{tt(EMPTY_KK, EMPTY_RU)}</Empty>}
    </>
  );
}

function GrantSheet({ onGrant }: { onGrant: (uid: number) => Promise<void> }) {
  const { tt, closeSheet } = useApp();
  const [v, setV] = useState('');
  const n = Number(v);
  return (
    <form className="form" onSubmit={async e => { e.preventDefault(); if (n > 0) { await onGrant(n); closeSheet(); } }}>
      <h2>{tt('Жетекші тағайындау', 'Назначить руководителя')}</h2>
      <div className="field"><label htmlFor="grUid">{tt('Қолданушы ID (Әкімші → Қолданушылар іздеуінен)', 'ID пользователя (Админ → поиск пользователей)')}</label>
        <input className="input" id="grUid" inputMode="numeric" value={v} onChange={e => setV(e.target.value.replace(/\D/g, ''))} /></div>
      <button className="btn" type="submit" disabled={!(n > 0)}>{tt('Тағайындау', 'Назначить')}</button>
    </form>
  );
}
