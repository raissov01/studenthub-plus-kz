'use client';
/* «Пәтерлес» (FEATURE_ROOMMATES): /roommates listings (all / «Маған сай»), ?id=new my listing, ?id=requests requests,
   ?id=<n> one listing. The Bagdar rules: no author identity or contact in a listing, no contacts in free text, one
   active listing, «тек өз жынысым» strict (gender kept apart from the profile), 10 requests a day, contacts open
   only when the owner accepts, 30-day auto-close, complaints. Reading needs an account. */
import { useState } from 'react';
import { useApp } from '@/components/app';
import { Chip, ConfirmSheet, Empty, Icon, Row, SectionHead, Seg } from '@/components/ui';
import { ErrorLine, errText, Feature, hasAccount, Loading, NotFound, SignInCard, useLoad, ymd } from '@/components/views/common';
import { ContactCard, ReportLink, RuleWarning } from '@/components/views/community-ui';
import { useQueryId } from '@/components/views/subjects';
import { api, useAccount } from '@/lib/account';
import {
  addDays, almatyToday, RM_GENDER, RM_HABITS, RM_HOUSING, RM_KINDS, RM_LIMITS, rmProblem, tenge,
  type RMKind, type RMListing, type RMMe, type RMRequest,
} from '@/lib/community';

export function RoommatesScreen() {
  const prm = useQueryId() ?? '';
  const { tt } = useApp();
  const acc = useAccount();
  let body;
  if (!acc.loaded) body = <Loading />;
  else if (!hasAccount(acc)) body = <SignInCard text={tt('«Пәтерлес» тек кірген студенттерге ашық: хабарландыруда аты-жөн мен байланыс жоқ, олар екі жақ келіскенде ғана ашылады.',
    '«Соседи» доступны только вошедшим студентам: в объявлениях нет имён и контактов, они открываются только по согласию обеих сторон.')} />;
  else body = !prm ? <RMHome /> : prm === 'new' ? <RMNew /> : prm === 'requests' ? <RMRequests /> : /^\d+$/.test(prm) ? <RMListingView id={prm} /> : <NotFound />;
  return <Feature name="roommates">{body}</Feature>;
}

function ListingCard({ l, onRequest }: { l: RMListing; onRequest?: () => void }) {
  const { tt, L } = useApp();
  return (
    <article className="card cm-listing" aria-label={L(RM_KINDS[l.kind])}>
      <div className="badges">
        <span className={`badge ${l.kind === 'have_room' ? 'b-ok' : 'b-sun'}`}>{L(RM_KINDS[l.kind])}</span>
        <span className="badge b-line">{L(RM_HOUSING[l.housing])}</span>
        {l.sameGender && l.gender ? <span className="badge b-line">{L(RM_GENDER[l.gender])}</span> : null}
        {l.common != null && l.common > 0 ? <span className="badge b-ok">{tt(`${l.common} ортақ әдет`, `общих привычек: ${l.common}`)}</span> : null}
      </div>
      <span className="row-title">{l.district}</span>
      <span className="cm-meta">
        <span className="num">{tenge(l.budgetMin)} – {tenge(l.budgetMax)} / {tt('ай', 'мес')}</span>
        <span>{tt(`Көшу: ${ymd(l.moveIn)}`, `Заезд: ${ymd(l.moveIn)}`)}</span>
      </span>
      {l.habits.length ? <div className="badges">{l.habits.map(h => <span key={h} className="badge b-line">{L(RM_HABITS[h] ?? { kk: h, ru: h })}</span>)}</div> : null}
      {l.about ? <p className="cm-body small">{l.about}</p> : null}
      {l.mine ? <span className="tiny muted">{tt(`Сіздің хабарландыруыңыз · ${ymd(l.expiresAt)} дейін`, `Ваше объявление · до ${ymd(l.expiresAt)}`)}</span>
        : l.myRequest ? <span className="badge b-line">{l.myRequest === 'accepted' ? tt('Келісті — «Сұраулар» бөлімін қараңыз', 'Согласие — см. «Запросы»')
          : l.myRequest === 'declined' ? tt('Бұл жолы келіспеді', 'В этот раз не сложилось') : tt('Сұрау жіберілді', 'Запрос отправлен')}</span>
          : onRequest ? <button className="btn" type="button" onClick={onRequest}><Icon n="heart" s={18} /> {tt('Қызығамын', 'Интересно')}</button> : null}
      {!l.mine ? <ReportLink kind="roommate_listing" id={l.id} /> : null}
    </article>
  );
}

function RequestSheet({ l, done }: { l: RMListing; done: () => void }) {
  const { tt, lang, toast, closeSheet } = useApp();
  const [err, setErr] = useState('');
  const [busy, setBusy] = useState(false);
  return (
    <div className="form">
      <h2>{tt('Сұрау жіберу', 'Отправить запрос')}</h2>
      <p className="small">{tt('Иесіне сіздің курсыңыз, университетіңіз және хабарландыруыңыздың қысқаша түйіні көрсетіледі. Атыңыз бен байланысыңыз — ол келіссе ғана.',
        'Владельцу покажем ваш курс, университет и кратко ваше объявление. Имя и контакты — только если он согласится.')}</p>
      <ErrorLine err={err} />
      <div className="btn-row">
        <button className="btn ghost" type="button" onClick={closeSheet}>{tt('Бас тарту', 'Отмена')}</button>
        <button className="btn" type="button" disabled={busy} onClick={async () => {
          setBusy(true); setErr('');
          try { await api(`/api/roommates/listings/${l.id}/requests`, { method: 'POST', lang }); closeSheet(); done(); toast(tt('Сұрау жіберілді', 'Запрос отправлен')); }
          catch (e) { setErr(errText(e)); } finally { setBusy(false); }
        }}>{tt('Жіберу', 'Отправить')}</button>
      </div>
    </div>
  );
}

function GenderCard({ me, reload }: { me: RMMe; reload: () => void }) {
  const { tt, lang, toast } = useApp();
  const pick = async (g: string) => {
    try { await api('/api/roommates/gender', { method: 'POST', body: { gender: g === 'none' ? null : g }, lang }); reload(); } catch (e) { toast(errText(e)); }
  };
  return (
    <div className="card">
      <span className="row-title" id="rmGenderLbl">{tt('Жынысыңыз', 'Ваш пол')}</span>
      <span className="tiny muted">{tt('Тек «тек өз жынысым» шарты үшін керек. Профильде де, хабарландыруда да көрсетілмейді.', 'Нужен только для условия «только мой пол». Не показывается ни в профиле, ни в объявлении.')}</span>
      <Seg items={[['f', tt('Қыз', 'Девушка')], ['m', tt('Ұл', 'Парень')], ['none', tt('Көрсетпеу', 'Не указывать')]]} cur={me.gender ?? 'none'} onPick={pick} />
    </div>
  );
}

function RMHome() {
  const { tt, go, openSheet, lang, toast } = useApp();
  const me = useLoad<RMMe>('/api/roommates/me');
  const [tab, setTab] = useState<'all' | 'match'>('all');
  const [kind, setKind] = useState<'all' | RMKind>('all');
  const [housing, setHousing] = useState<'all' | 'dorm' | 'flat'>('all');
  const qs = new URLSearchParams();
  if (tab === 'match') qs.set('match', '1');
  else {
    if (kind !== 'all') qs.set('kind', kind);
    if (housing !== 'all') qs.set('housing', housing);
  }
  const list = useLoad<{ listings: RMListing[]; needListing?: boolean }>('/api/roommates/listings?' + qs.toString());
  const reload = () => { me.reload(); list.reload(); };
  const mine = me.data?.listing;
  return (
    <>
      <div className="btn-row">
        {!mine ? <button className="btn" type="button" onClick={() => go('roommates:new')}><Icon n="plus" s={18} /> {tt('Хабарландыру беру', 'Разместить объявление')}</button> : null}
        <button className="btn ghost" type="button" onClick={() => go('roommates:requests')}>{tt('Сұраулар', 'Запросы')}</button>
      </div>
      {me.data ? <GenderCard me={me.data} reload={reload} /> : null}
      {mine ? (
        <>
          <SectionHead title={tt('Менің хабарландыруым', 'Моё объявление')} />
          <ListingCard l={mine} />
          <button className="btn ghost" type="button" onClick={() => openSheet(<ConfirmSheet msg={tt('Хабарландыруды жабу керек пе?', 'Закрыть объявление?')} yesLabel={tt('Иә, жабу', 'Да, закрыть')}
            yes={async () => { try { await api(`/api/roommates/listings/${mine.id}/close`, { method: 'POST', lang }); reload(); } catch (e) { toast(errText(e)); } }} />)}>
            {tt('Хабарландыруды жабу', 'Закрыть объявление')}</button>
        </>
      ) : null}
      <Seg items={[['all', tt('Барлығы', 'Все')], ['match', tt('Маған сай', 'Подходят мне')]]} cur={tab} onPick={k => setTab(k as typeof tab)} />
      {tab === 'all' ? (
        <div className="chips">
          <Chip pressed={kind === 'all'} onClick={() => setKind('all')}>{tt('Кез келген', 'Любые')}</Chip>
          <Chip pressed={kind === 'have_room'} onClick={() => setKind('have_room')}>{tt('Бөлмесі бар', 'Есть комната')}</Chip>
          <Chip pressed={kind === 'looking'} onClick={() => setKind('looking')}>{tt('Бірге іздейді', 'Ищут вместе')}</Chip>
          <Chip pressed={housing === 'dorm'} onClick={() => setHousing(h => (h === 'dorm' ? 'all' : 'dorm'))}>{tt('Жатақхана', 'Общежитие')}</Chip>
          <Chip pressed={housing === 'flat'} onClick={() => setHousing(h => (h === 'flat' ? 'all' : 'flat'))}>{tt('Пәтер', 'Квартира')}</Chip>
        </div>
      ) : null}
      <ErrorLine err={list.err || me.err} />
      {!list.data && !list.err ? <Loading /> : list.data?.needListing ? (
        <Empty icon="home" action={<button className="btn secondary" type="button" onClick={() => go('roommates:new')}>{tt('Хабарландыру беру', 'Разместить объявление')}</button>}>
          {tt('«Маған сай» тізімі сіздің хабарландыруыңызға қарап құралады.', 'Список «Подходят мне» строится по вашему объявлению.')}</Empty>
      ) : list.data?.listings.filter(l => !l.mine).length ? (
        list.data.listings.filter(l => !l.mine).map(l => (
          <ListingCard key={l.id} l={l} onRequest={() => openSheet(<RequestSheet l={l} done={reload} />)} />
        ))
      ) : list.data ? (
        <Empty icon="home">{tt('Әзірге сәйкес хабарландыру жоқ. Өзіңіз жариялаңыз — басқалар сізді табады.', 'Подходящих объявлений пока нет. Разместите своё — вас найдут.')}</Empty>
      ) : null}
      {me.data ? <p className="tiny muted">{tt(`Бүгін ${me.data.requestsToday}/${me.data.dailyLimit} сұрау.`, `Сегодня запросов: ${me.data.requestsToday}/${me.data.dailyLimit}.`)}</p> : null}
      <p className="note">{tt('Пәтерді көрмей ақша аудармаңыз. Алғашқы кездесу — адам көп жерде. Ақша сұраса не күдікті болса — шағымданыңыз.',
        'Не переводите деньги, не увидев жильё. Первая встреча — в людном месте. Если просят деньги или что-то подозрительно — пожалуйтесь.')}</p>
    </>
  );
}

function RMListingView({ id }: { id: string }) {
  const { openSheet } = useApp();
  const { data, err, code, reload } = useLoad<{ listing: RMListing }>(`/api/roommates/listings/${id}`);
  if (code === 'not_found') return <NotFound />;
  if (err) return <ErrorLine err={err} />;
  if (!data) return <Loading />;
  return <ListingCard l={data.listing} onRequest={data.listing.status === 'active' ? () => openSheet(<RequestSheet l={data.listing} done={reload} />) : undefined} />;
}

function RMNew() {
  const { tt, L, lang, go } = useApp();
  const me = useLoad<RMMe>('/api/roommates/me');
  const today = almatyToday();
  const [f, setF] = useState({ kind: 'looking' as RMKind, housing: 'flat', district: '', budgetMin: '30000', budgetMax: '70000', moveIn: addDays(today, 14),
    habits: [] as string[], about: '', sameGender: false });
  const [err, setErr] = useState('');
  const [busy, setBusy] = useState(false);
  const [tried, setTried] = useState(false);
  if (me.err) return <ErrorLine err={me.err} />;
  if (!me.data) return <Loading />;
  if (me.data.listing) return <Empty icon="home" action={<button className="btn secondary" type="button" onClick={() => go('roommates')}>{tt('Хабарландыруым', 'Моё объявление')}</button>}>
    {tt('Бір адамға бір белсенді хабарландыру. Жаңасын беру үшін бұрынғысын жабыңыз.', 'Одно активное объявление на человека. Чтобы создать новое, закройте прежнее.')}</Empty>;
  const draft = { ...f, budgetMin: Number(f.budgetMin), budgetMax: Number(f.budgetMax) };
  const problem = rmProblem(draft, me.data.gender, today);
  const toggle = (h: string) => setF(x => ({ ...x, habits: x.habits.includes(h) ? x.habits.filter(y => y !== h) : x.habits.length >= RM_LIMITS.habits ? x.habits : [...x.habits, h] }));
  return (
    <form className="form" noValidate onSubmit={async e => {
      e.preventDefault();
      setTried(true);
      if (problem || busy) return;
      setBusy(true); setErr('');
      try {
        await api('/api/roommates/listings', { method: 'POST', lang, body: { ...draft, district: f.district.trim(), about: f.about.trim() } });
        go('roommates', { replace: true });
      } catch (e2) { setErr(errText(e2)); } finally { setBusy(false); }
    }}>
      <Seg items={[['looking', L(RM_KINDS.looking)], ['have_room', L(RM_KINDS.have_room)]]} cur={f.kind} onPick={k => setF(x => ({ ...x, kind: k as RMKind }))} />
      <Seg items={[['flat', L(RM_HOUSING.flat)], ['dorm', L(RM_HOUSING.dorm)]]} cur={f.housing} onPick={k => setF(x => ({ ...x, housing: k }))} />
      <div className="field"><label htmlFor="rmDistrict">{tt('Аудан не жатақхана', 'Район или общежитие')}</label>
        <input className="input" id="rmDistrict" maxLength={RM_LIMITS.district} value={f.district} onChange={e => setF(x => ({ ...x, district: e.target.value }))}
          placeholder={tt('Сарыарқа ауданы', 'Район Сарыарка')} /></div>
      <div className="card-row">
        <div className="field" style={{ flex: 1 }}><label htmlFor="rmMin">{tt('Бюджет, ₸ — бастап', 'Бюджет, ₸ — от')}</label>
          <input className="input num" id="rmMin" type="number" inputMode="numeric" min={0} step={1000} value={f.budgetMin} onChange={e => setF(x => ({ ...x, budgetMin: e.target.value }))} /></div>
        <div className="field" style={{ flex: 1 }}><label htmlFor="rmMax">{tt('дейін', 'до')}</label>
          <input className="input num" id="rmMax" type="number" inputMode="numeric" min={5000} step={1000} value={f.budgetMax} onChange={e => setF(x => ({ ...x, budgetMax: e.target.value }))} /></div>
      </div>
      <div className="field"><label htmlFor="rmMove">{tt('Көшу күні', 'Дата заезда')}</label>
        <input className="input" id="rmMove" type="date" min={today} value={f.moveIn} onChange={e => setF(x => ({ ...x, moveIn: e.target.value }))} /></div>
      <fieldset className="cm-fieldset">
        <legend className="small">{tt(`Әдеттер (${f.habits.length}/${RM_LIMITS.habits})`, `Привычки (${f.habits.length}/${RM_LIMITS.habits})`)}</legend>
        <div className="chips wrap">{Object.entries(RM_HABITS).map(([k, h]) => <Chip key={k} pressed={f.habits.includes(k)} onClick={() => toggle(k)}>{L(h)}</Chip>)}</div>
      </fieldset>
      <div className="field"><label htmlFor="rmAbout">{tt('Өзім туралы (міндетті емес)', 'О себе (необязательно)')}</label>
        <textarea className="input" id="rmAbout" maxLength={RM_LIMITS.about} value={f.about} onChange={e => setF(x => ({ ...x, about: e.target.value }))} />
        <span className="tiny muted">{tt('Телефон, @username, сілтеме жазбаңыз — байланыс келісімнен кейін ашылады.', 'Не пишите телефон, @username, ссылки — контакты откроются после согласия.')}</span></div>
      <label className="check"><input type="checkbox" checked={f.sameGender} onChange={e => setF(x => ({ ...x, sameGender: e.target.checked }))} />
        <span>{tt('Тек өз жынысым: басқаларға хабарландыру мүлде көрінбейді', 'Только мой пол: другим объявление не видно вовсе')}</span></label>
      {f.sameGender && !me.data.gender ? <GenderCard me={me.data} reload={me.reload} /> : null}
      {tried || problem === 'has_contact' ? <RuleWarning code={problem} /> : null}
      <ErrorLine err={err} />
      <button className="btn block" type="submit" disabled={busy}>{tt('Жариялау', 'Опубликовать')}</button>
      <p className="tiny muted">{tt('Хабарландыру 30 күннен соң өзі жабылады.', 'Объявление закроется само через 30 дней.')}</p>
    </form>
  );
}

function RMRequests() {
  const { tt, lang, toast } = useApp();
  const { data, err, reload } = useLoad<{ requests: RMRequest[] }>('/api/roommates/requests');
  if (err) return <ErrorLine err={err} />;
  if (!data) return <Loading />;
  const incoming = data.requests.filter(r => r.incoming), outgoing = data.requests.filter(r => !r.incoming);
  const decide = async (id: number, accept: boolean) => {
    try { await api(`/api/roommates/requests/${id}/decide`, { method: 'POST', body: { accept }, lang }); reload(); } catch (e) { toast(errText(e)); }
  };
  const status = (s: RMRequest['status']) => s === 'accepted' ? tt('Келісті', 'Согласие') : s === 'declined' ? tt('Келіспеді', 'Отказ') : tt('Жауап күтілуде', 'Ждёт ответа');
  return (
    <>
      <SectionHead title={tt('Маған келген', 'Входящие')} />
      {incoming.length ? incoming.map(r => (
        <div key={r.id} className="card">
          <span className="row-title">{[r.course ? tt(`${r.course}-курс`, `${r.course} курс`) : '', r.university].filter(Boolean).join(' · ') || tt('Студент', 'Студент')}</span>
          {r.listing ? <ListingCard l={r.listing} /> : <span className="small muted">{tt('Хабарландыруы жоқ', 'Без объявления')}</span>}
          <span className="badges"><span className="badge b-line">{status(r.status)}</span></span>
          {r.status === 'pending' ? (
            <div className="btn-row">
              <button className="btn ghost" type="button" onClick={() => decide(r.id, false)}>{tt('Жоқ', 'Нет')}</button>
              <button className="btn" type="button" onClick={() => decide(r.id, true)}>{tt('Келісемін', 'Согласен')}</button>
            </div>
          ) : null}
          {r.contact ? <ContactCard c={r.contact} title={tt('Байланыс', 'Контакт')} /> : null}
        </div>
      )) : <p className="small muted">{tt('Әзірге сұрау жоқ.', 'Запросов пока нет.')}</p>}
      <SectionHead title={tt('Мен жібергендер', 'Отправленные')} />
      {outgoing.length ? outgoing.map(r => (
        <div key={r.id} className="card">
          {r.listing ? <Row go={'roommates:' + r.listingId} icon="home" title={r.listing.district} sub={status(r.status)} /> : null}
          {r.contact ? <ContactCard c={r.contact} title={tt('Байланыс', 'Контакт')} /> : null}
        </div>
      )) : <p className="small muted">{tt('Сіз әлі сұрау жібермедіңіз.', 'Вы ещё не отправляли запросов.')}</p>}
    </>
  );
}
