'use client';
/* eslint-disable @next/next/no-img-element -- photos come from our API (unattached ones only with the session) */
/* «Табылған заттар» (FEATURE_LOSTFOUND): /lostfound list, /lostfound?id=new form, ?id=mine, ?id=<n> a post.
   A post never names its author. A claim is a private message to the poster; contacts open only after the poster
   confirms; the return is marked by both. Documents and cards: no photo, no number, a found one goes to the dean's
   office or security. */
import { useRef, useState } from 'react';
import { useApp } from '@/components/app';
import { DeskDetail, DeskList } from '@/components/desk';
import { useDesktop } from '@/lib/desktop';
import { Chip, Empty, Icon, Row, SectionHead, Seg } from '@/components/ui';
import { ErrorLine, errText, Feature, hasAccount, Loading, NotFound, SignInCard, useLoad, useNeedAccount, ymd } from '@/components/views/common';
import { ContactCard, ReportLink, RuleWarning } from '@/components/views/community-ui';
import { useQueryId } from '@/components/views/subjects';
import { api, apiUpload, useAccount } from '@/lib/account';
import {
  almatyToday, isSensitive, LF_CATS, LF_HANDED, LF_LIMITS, lfProblem, type LFClaim, type LFDetail, type LFKind, type LFList, type LFPost,
} from '@/lib/community';
import { BUILDINGS } from '@/lib/data/campus';
import { shrinkPhoto } from '@/lib/market';

export function LostFoundScreen() {
  const prm = useQueryId() ?? '';
  return (
    <Feature name="lostfound">
      {!prm ? <LFListView /> : prm === 'new' || prm.startsWith('new:') ? <LFNew kind={prm === 'new:found' ? 'found' : 'lost'} />
        : prm === 'mine' ? <LFMine /> : /^\d+$/.test(prm) ? <LFPostView id={prm} /> : <NotFound />}
    </Feature>
  );
}

function kindLabel(k: LFKind, tt: (kk: string, ru: string) => string) {
  return k === 'lost' ? tt('Жоғалды', 'Потеряно') : tt('Табылды', 'Найдено');
}

function buildingName(id: string, L: (t: { kk: string; ru: string }) => string) {
  const b = BUILDINGS.find(x => x.id === id);
  return b ? `${b.n}-корпус · ${L(b.name)}` : '';
}

function LFCard({ p }: { p: LFPost }) {
  const { tt, L } = useApp();
  const where = [buildingName(p.building, L), p.room].filter(Boolean).join(', ');
  return (
    <Row go={'lostfound:' + p.id} icon={p.kind === 'lost' ? 'search' : 'gift'} tone={p.kind === 'lost' ? 'nico-2' : 'nico-1'}
      title={p.title}
      sub={[kindLabel(p.kind, tt), L(LF_CATS[p.category] ?? LF_CATS.other), where, ymd(p.happenedOn)].filter(Boolean).join(' · ')}
      end={p.handedTo ? <span className="badge b-ok">{L(LF_HANDED[p.handedTo])}</span> : undefined} />
  );
}

function LFListView() {
  const { tt, L, go } = useApp();
  const desktop = useDesktop();
  const acc = useAccount();
  const [kind, setKind] = useState<'all' | LFKind>('all');
  const [cat, setCat] = useState('all');
  const [uni, setUni] = useState<'own' | 'all'>('own');
  const own = !!acc.user?.university;
  const qs = new URLSearchParams();
  if (kind !== 'all') qs.set('kind', kind);
  if (cat !== 'all') qs.set('category', cat);
  if (!own || uni === 'all') qs.set('university', 'all');
  const { data, err } = useLoad<LFList>(acc.loaded ? '/api/lostfound/posts?' + qs.toString() : null);
  const filters = (
    <>
      <div className="btn-row">
        <button className="btn" type="button" onClick={() => go('lostfound:new:lost')}><Icon n="search" s={18} /> {tt('Жоғалттым', 'Потерял')}</button>
        <button className="btn secondary" type="button" onClick={() => go('lostfound:new:found')}><Icon n="gift" s={18} /> {tt('Таптым', 'Нашёл')}</button>
      </div>
      {acc.user ? <button className="link-btn" type="button" style={{ alignSelf: 'flex-start' }} onClick={() => go('lostfound:mine')}>{tt('Менің жазбаларым', 'Мои записи')}</button> : null}
      <Seg items={[['all', tt('Барлығы', 'Все')], ['lost', tt('Жоғалғандар', 'Потерянное')], ['found', tt('Табылғандар', 'Найденное')]]} cur={kind} onPick={k => setKind(k as typeof kind)} />
      {own ? <div className="chips">
        <Chip pressed={uni === 'own'} onClick={() => setUni('own')}>{tt('Менің университетім', 'Мой университет')}</Chip>
        <Chip pressed={uni === 'all'} onClick={() => setUni('all')}>{tt('Барлық университет', 'Все университеты')}</Chip>
      </div> : null}
      <div className="field"><label htmlFor="lfCat">{tt('Санат', 'Категория')}</label>
        <select className="input" id="lfCat" value={cat} onChange={e => setCat(e.target.value)}>
          <option value="all">{tt('Кез келген', 'Любая')}</option>
          {Object.entries(LF_CATS).map(([k, c]) => <option key={k} value={k}>{L(c)}</option>)}
        </select></div>
    </>
  );
  const results = (
    <>
      <ErrorLine err={err} />
      {!data && !err ? <Loading /> : data?.posts.length ? (
        <div className={`group${desktop ? ' desk-cards' : ''}`} aria-label={tt('Хабарландырулар', 'Объявления')}>{data.posts.map(p => <LFCard key={p.id} p={p} />)}</div>
      ) : data ? (
        <Empty icon="lostfound">{tt('Әзірге ештеңе жоқ. Бір нәрсе жоғалтсаңыз не тапсаңыз — бірінші болып жазыңыз.', 'Пока ничего нет. Если что-то потеряли или нашли — напишите первым.')}</Empty>
      ) : null}
      <p className="note">{tt('Құжат пен картаны тапсаңыз: фотосын салмаңыз, нөмірін жазбаңыз, деканатқа не күзетке тапсырыңыз. Иесі өзі белгісін айтады.',
        'Нашли документ или карту: не выкладывайте фото и номер, сдайте в деканат или охрану. Владелец сам назовёт примету.')}</p>
    </>
  );
  if (desktop) return <DeskList label={tt('Сүзгілер', 'Фильтры')} filters={filters}>{results}</DeskList>;
  return <>{filters}{results}</>;
}

function ClaimSheet({ post, done }: { post: LFPost; done: () => void }) {
  const { tt, lang, toast, closeSheet } = useApp();
  const [msg, setMsg] = useState('');
  const [err, setErr] = useState('');
  const [busy, setBusy] = useState(false);
  const n = Array.from(msg.trim()).length;
  const found = post.kind === 'found';
  return (
    <form className="form" onSubmit={async e => {
      e.preventDefault();
      if (busy || n < 10) return;
      setBusy(true); setErr('');
      try {
        await api(`/api/lostfound/posts/${post.id}/claims`, { method: 'POST', body: { message: msg.trim() }, lang });
        closeSheet(); done();
        toast(tt('Жіберілді. Жариялаушы растаса, байланыстар ашылады.', 'Отправлено. Если автор подтвердит, контакты откроются.'));
      } catch (e2) { setErr(errText(e2)); } finally { setBusy(false); }
    }}>
      <h2>{found ? tt('Бұл менікі', 'Это моё') : tt('Мен таптым', 'Я нашёл')}</h2>
      <p className="small">{found
        ? tt('Тек иесі білетін белгіні жазыңыз (мысалы, брелок, ішіндегі зат, сызат). Бұл хабарламаны тек тапқан адам оқиды.', 'Назовите примету, которую знает только владелец (брелок, что внутри, царапина). Сообщение прочитает только нашедший.')
        : tt('Не тапқаныңызды және қай жерде екенін жазыңыз. Хабарламаны тек иесі оқиды.', 'Напишите, что и где вы нашли. Сообщение прочитает только владелец.')}</p>
      <div className="field"><label htmlFor="lfMsg">{tt('Хабарлама', 'Сообщение')}</label>
        <textarea className="input" id="lfMsg" maxLength={LF_LIMITS.message} value={msg} onChange={e => setMsg(e.target.value)} />
        <span className="tiny muted num">{n} / {LF_LIMITS.message}</span></div>
      <p className="tiny muted">{tt('Телефон мен @username жазбаңыз: байланыс растаудан кейін өзі ашылады.', 'Не пишите телефон и @username: контакты откроются после подтверждения.')}</p>
      <ErrorLine err={err} />
      <div className="btn-row">
        <button className="btn ghost" type="button" onClick={closeSheet}>{tt('Бас тарту', 'Отмена')}</button>
        <button className="btn" type="submit" disabled={busy || n < 10}>{tt('Жіберу', 'Отправить')}</button>
      </div>
    </form>
  );
}

const CLAIM_STATUS: Record<LFClaim['status'], [string, string]> = {
  pending: ['Жауап күтілуде', 'Ждёт ответа'], confirmed: ['Расталды', 'Подтверждено'], declined: ['Расталмады', 'Не подтверждено'],
  returned: ['Қайтарылды', 'Возвращено'], withdrawn: ['Қайтарып алынды', 'Отозвано'],
};

function ClaimBlock({ c, mine, reload }: { c: LFClaim; mine: boolean; reload: () => void }) {
  const { tt, lang, toast } = useApp();
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  const act = async (path: string, body?: unknown) => {
    setBusy(true); setErr('');
    try {
      const r = await api<{ returned?: boolean; award?: { xp: number; coins: number } | null }>(path, { method: 'POST', body, lang });
      if (r.award && (r.award.xp || r.award.coins)) toast(tt(`Рақмет! +${r.award.xp} XP`, `Спасибо! +${r.award.xp} XP`));
      reload();
    } catch (e) { setErr(errText(e)); } finally { setBusy(false); }
  };
  return (
    <div className="card cm-claim">
      <span className="badges"><span className={`badge ${c.status === 'confirmed' || c.status === 'returned' ? 'b-ok' : 'b-line'}`}>{tt(...CLAIM_STATUS[c.status])}</span></span>
      <p style={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere', margin: 0 }}>{c.message}</p>
      {mine && c.status === 'pending' ? (
        <div className="btn-row">
          <button className="btn ghost" type="button" disabled={busy} onClick={() => act(`/api/lostfound/claims/${c.id}/decide`, { accept: false })}>{tt('Сәйкес емес', 'Не совпадает')}</button>
          <button className="btn" type="button" disabled={busy} onClick={() => act(`/api/lostfound/claims/${c.id}/decide`, { accept: true })}>{tt('Растау', 'Подтвердить')}</button>
        </div>
      ) : null}
      {c.contact ? <ContactCard c={c.contact} title={tt('Байланыс', 'Контакт')} /> : null}
      {c.status === 'confirmed' ? (
        c.iReturned
          ? <p className="small muted">{tt('Сіз белгіледіңіз. Екінші жақтың растауы күтілуде.', 'Вы отметили. Ждём подтверждения второй стороны.')}</p>
          : <button className="btn secondary block" type="button" disabled={busy} onClick={() => act(`/api/lostfound/claims/${c.id}/returned`)}>
            <Icon n="check" s={18} /> {tt('Зат иесіне қайтты', 'Вещь вернулась к владельцу')}</button>
      ) : null}
      <ErrorLine err={err} />
    </div>
  );
}

function LFPostView({ id }: { id: string }) {
  const { tt, L, go, openSheet, lang, toast } = useApp();
  const desktop = useDesktop();
  const acc = useAccount();
  const need = useNeedAccount();
  const { data, err, code, reload } = useLoad<LFDetail>(acc.loaded ? `/api/lostfound/posts/${id}${acc.user ? '?u=' + acc.user.id : ''}` : null);
  if (code === 'not_found') return <NotFound />;
  if (err) return <ErrorLine err={err} />;
  if (!data) return <Loading />;
  const p = data.post;
  const where = [buildingName(p.building, L), p.room].filter(Boolean).join(', ');
  const close = async () => {
    try { await api(`/api/lostfound/posts/${p.id}/close`, { method: 'POST', lang }); toast(tt('Жабылды', 'Закрыто')); reload(); } catch (e) { toast(errText(e)); }
  };
  const photo = p.photo ? <div className="mk-photos"><img src={p.photo} alt={p.title} /></div> : null;
  const head = (
    <div className="detail-head"><h1>{p.title}</h1>
      <div className="badges">
        <span className={`badge ${p.kind === 'lost' ? 'b-sun' : 'b-ok'}`}>{kindLabel(p.kind, tt)}</span>
        <span className="badge b-line">{L(LF_CATS[p.category] ?? LF_CATS.other)}</span>
        {p.handedTo ? <span className="badge b-ok">{L(LF_HANDED[p.handedTo])}</span> : null}
        {p.status !== 'active' ? <span className="badge b-line">{p.status === 'resolved' ? tt('Иесі табылды', 'Владелец найден') : tt('Жабық', 'Закрыто')}</span> : null}
      </div></div>
  );
  const desc = p.desc ? <p style={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>{p.desc}</p> : null;
  const facts = (
    <div className="group">
      {where ? <Row icon="pin" title={where} sub={p.university} chev={false} /> : <Row icon="pin" title={p.university} chev={false} />}
      <Row icon="calendar" title={ymd(p.happenedOn)} sub={tt(`${ymd(p.expiresAt)} дейін көрінеді`, `видно до ${ymd(p.expiresAt)}`)} chev={false} />
    </div>
  );
  const handed = p.handedTo ? <p className="note">{tt('Құжатты алу үшін жеке куәлігіңізбен көрсетілген жерге барыңыз.', 'Чтобы забрать, приходите с удостоверением личности в указанное место.')}</p> : null;
  const claims = p.mine ? (
    <section className="section" aria-label={tt('Хабарламалар', 'Сообщения')}>
      <SectionHead title={tt('Хабарламалар', 'Сообщения')} />
      {data.claims?.length ? data.claims.map(c => <ClaimBlock key={c.id} c={c} mine reload={reload} />)
        : <p className="small muted">{tt('Әзірге ешкім жазбады. Хабарлама келсе, Telegram-ға және хабарламаларға хабар келеді.', 'Пока никто не написал. О новом сообщении придёт уведомление.')}</p>}
      {p.status === 'active' ? <button className="btn ghost" type="button" onClick={close}>{tt('Жариялауды жабу', 'Снять с публикации')}</button> : null}
    </section>
  ) : data.myClaim ? (
    <section className="section" aria-label={tt('Менің хабарламам', 'Моё сообщение')}>
      <SectionHead title={tt('Менің хабарламам', 'Моё сообщение')} />
      <ClaimBlock c={data.myClaim} mine={false} reload={reload} />
    </section>
  ) : null;
  const act = !p.mine && !data.myClaim && p.status === 'active' ? (
    hasAccount(acc)
      ? <button className="btn block" type="button" onClick={() => openSheet(<ClaimSheet post={p} done={reload} />)}>
        {p.kind === 'found' ? tt('Бұл менікі', 'Это моё') : tt('Мен таптым', 'Я нашёл')}</button>
      : <SignInCard text={tt('Жариялаушыға жеке хабарлама жіберу үшін кіріңіз. Байланыстар екі жақ келіскенде ғана ашылады.', 'Чтобы написать автору, войдите. Контакты открываются только по согласию обеих сторон.')} />
  ) : null;
  const report = !p.mine ? <ReportLink kind="lostfound_post" id={p.id} /> : null;
  const mineLink = !acc.user ? null : <button className="link-btn" type="button" style={{ alignSelf: 'flex-start' }} onClick={() => { if (need()) go('lostfound:mine'); }}>{tt('Менің жазбаларым', 'Мои записи')}</button>;
  if (desktop) return <DeskDetail label={tt('Қайда және қашан', 'Где и когда')} main={<>{head}{photo}{desc}{claims}</>} rail={<>{facts}{act}{handed}{report}{mineLink}</>} />;
  return <>{photo}{head}{desc}{facts}{handed}{claims}{act}{report}{mineLink}</>;
}

function LFNew({ kind: initial }: { kind: LFKind }) {
  const { tt, L, lang, go } = useApp();
  const acc = useAccount();
  const [f, setF] = useState({ kind: initial, category: 'keys', title: '', desc: '', building: '', room: '', handedTo: '', happenedOn: almatyToday() });
  const [photo, setPhoto] = useState<{ name: string; url: string } | null>(null);
  const [up, setUp] = useState(false);
  const [err, setErr] = useState('');
  const [busy, setBusy] = useState(false);
  const [tried, setTried] = useState(false);
  const fileRef = useRef<HTMLInputElement>(null);
  if (!acc.loaded) return <Loading />;
  if (!hasAccount(acc)) return <SignInCard text={tt('Жариялау үшін кіріңіз. Атыңыз бен байланысыңыз хабарландыруда көрсетілмейді.', 'Чтобы опубликовать, войдите. Ваше имя и контакты в объявлении не показываются.')} />;
  const sensitive = isSensitive(f.category);
  const problem = lfProblem({ ...f, kind: f.kind, photo: photo?.name ?? '' });
  const set = (k: keyof typeof f, v: string) => setF(x => ({ ...x, [k]: v }));
  const pick = async (files: FileList | null) => {
    const file = files?.[0];
    if (!file) return;
    setUp(true); setErr('');
    try {
      const blob = await shrinkPhoto(file);
      const form = new FormData();
      form.append('photo', blob, 'photo.jpg');
      setPhoto(await apiUpload<{ name: string; url: string }>('/api/lostfound/photos', form, lang));
    } catch (e) { setErr(errText(e, tt('Фото жүктелмеді', 'Фото не загрузилось'))); } finally { setUp(false); if (fileRef.current) fileRef.current.value = ''; }
  };
  return (
    <form className="form" noValidate onSubmit={async e => {
      e.preventDefault();
      setTried(true);
      if (problem || busy) return;
      setBusy(true); setErr('');
      try {
        const r = await api<{ post: LFPost }>('/api/lostfound/posts', {
          method: 'POST', lang,
          body: { ...f, title: f.title.trim(), desc: f.desc.trim(), room: f.room.trim(), photo: sensitive ? '' : photo?.name ?? '', handedTo: f.handedTo },
        });
        go('lostfound:' + r.post.id, { replace: true });
      } catch (e2) { setErr(errText(e2)); } finally { setBusy(false); }
    }}>
      <Seg items={[['lost', tt('Жоғалттым', 'Потерял')], ['found', tt('Таптым', 'Нашёл')]]} cur={f.kind} onPick={k => set('kind', k)} />
      <div className="field"><label htmlFor="lfCatNew">{tt('Санат', 'Категория')}</label>
        <select className="input" id="lfCatNew" value={f.category} onChange={e => { set('category', e.target.value); if (isSensitive(e.target.value)) setPhoto(null); }}>
          {Object.entries(LF_CATS).map(([k, c]) => <option key={k} value={k}>{L(c)}</option>)}
        </select></div>
      {sensitive ? (
        <div className="card banner cm-warn" role="note">
          <b>{tt('Құжат пен карта — жеке дерек', 'Документы и карты — личные данные')}</b>
          <span className="small">{tt('Фотосын салмаңыз, нөмірін, атын-жөнін, ЖСН-ін жазбаңыз. Жалпы атауы жеткілікті: «Студенттік билет». Иесі өзі белгісін айтады.',
            'Не выкладывайте фото, номер, ФИО, ИИН. Достаточно общего названия: «Студенческий билет». Владелец сам назовёт примету.')}</span>
          {f.kind === 'found' ? (
            <fieldset className="cm-fieldset">
              <legend className="small">{tt('Қайда тапсырдыңыз?', 'Куда вы сдали?')}</legend>
              {(['dean', 'security'] as const).map(k => (
                <label key={k} className="check"><input type="radio" name="lfHanded" checked={f.handedTo === k} onChange={() => set('handedTo', k)} /> {L(LF_HANDED[k])}</label>
              ))}
            </fieldset>
          ) : null}
        </div>
      ) : null}
      <div className="field"><label htmlFor="lfTitle">{tt('Не?', 'Что?')}</label>
        <input className="input" id="lfTitle" maxLength={LF_LIMITS.title} value={f.title} onChange={e => set('title', e.target.value)}
          placeholder={sensitive ? tt('Студенттік билет', 'Студенческий билет') : tt('Қара рюкзак', 'Чёрный рюкзак')} /></div>
      <div className="field"><label htmlFor="lfDesc">{tt('Сипаттама (міндетті емес)', 'Описание (необязательно)')}</label>
        <textarea className="input" id="lfDesc" maxLength={LF_LIMITS.desc} value={f.desc} onChange={e => set('desc', e.target.value)} />
        <span className="tiny muted">{f.kind === 'found'
          ? tt('Бәрін айтпаңыз: бір белгіні жасырын қалдырыңыз — иесі оны өзі айтсын.', 'Не рассказывайте всё: оставьте одну примету в секрете — пусть владелец назовёт её сам.')
          : tt('Байланысыңызды жазбаңыз: тапқан адам сайт арқылы жазады.', 'Не пишите свои контакты: нашедший напишет через сайт.')}</span></div>
      <div className="field"><label htmlFor="lfBuilding">{tt('Корпус', 'Корпус')}</label>
        <select className="input" id="lfBuilding" value={f.building} onChange={e => set('building', e.target.value)}>
          <option value="">{tt('Көрсетпеу', 'Не указывать')}</option>
          {BUILDINGS.map(b => <option key={b.id} value={b.id}>{b.n}-корпус · {L(b.name)}</option>)}
        </select></div>
      <div className="field"><label htmlFor="lfRoom">{tt('Аудитория не орын', 'Аудитория или место')}</label>
        <input className="input" id="lfRoom" maxLength={LF_LIMITS.room} value={f.room} onChange={e => set('room', e.target.value)} placeholder="1-305" /></div>
      <div className="field"><label htmlFor="lfDate">{tt('Күні', 'Дата')}</label>
        <input className="input" id="lfDate" type="date" value={f.happenedOn} max={almatyToday()} onChange={e => set('happenedOn', e.target.value)} /></div>
      {!sensitive ? (
        <div className="field"><span className="lbl">{tt('Фото (міндетті емес)', 'Фото (необязательно)')}</span>
          <span className="tiny muted">{tt('Фотода құжат, карта, адамның беті болмасын. Орналасу деректері (GPS) өшіріледі.', 'На фото не должно быть документов, карт и лиц. Геоданные (GPS) удаляются.')}</span>
          <div className="mk-thumbs">
            {photo ? <span className="mk-thumb"><img src={photo.url} alt={tt('Жүктелген фото', 'Загруженное фото')} />
              <button className="mk-x" type="button" onClick={() => setPhoto(null)} aria-label={tt('Фотоны алып тастау', 'Убрать фото')}><Icon n="x" s={16} /></button></span>
              : <label className="mk-thumb mk-add"><Icon n="plus" s={22} /><span className="tiny">{up ? tt('Жүктелуде…', 'Загрузка…') : tt('Фото', 'Фото')}</span>
                <input ref={fileRef} type="file" accept="image/jpeg,image/png,image/webp" className="visually-hidden" onChange={e => pick(e.target.files)} disabled={up} /></label>}
          </div></div>
      ) : null}
      {tried || (problem && problem !== 'bad_text' && problem !== 'handed_required') ? <RuleWarning code={problem} /> : null}
      <ErrorLine err={err} />
      <button className="btn block" type="submit" disabled={busy || up}>{tt('Жариялау', 'Опубликовать')}</button>
      <p className="tiny muted">{tt('Хабарландыру 30 күннен соң өзі жабылады. Атыңыз көрсетілмейді.', 'Объявление закроется само через 30 дней. Ваше имя не показывается.')}</p>
    </form>
  );
}

function LFMine() {
  const { tt } = useApp();
  const acc = useAccount();
  const { data, err } = useLoad<{ posts: LFPost[]; claims: LFClaim[] }>(hasAccount(acc) ? '/api/lostfound/mine' : null);
  if (!acc.loaded) return <Loading />;
  if (!hasAccount(acc)) return <SignInCard text={tt('Өз жазбаларыңызды көру үшін кіріңіз.', 'Войдите, чтобы увидеть свои записи.')} />;
  if (err) return <ErrorLine err={err} />;
  if (!data) return <Loading />;
  return (
    <>
      <SectionHead title={tt('Менің хабарландыруларым', 'Мои объявления')} />
      {data.posts.length ? <div className="group">{data.posts.map(p => (
        <Row key={p.id} go={'lostfound:' + p.id} icon={p.kind === 'lost' ? 'search' : 'gift'} title={p.title}
          sub={[kindLabel(p.kind, tt), p.status === 'active' ? tt('Белсенді', 'Активно') : tt('Жабық', 'Закрыто')].join(' · ')}
          end={p.pendingClaims ? <span className="badge b-ok">{p.pendingClaims}</span> : undefined} />
      ))}</div> : <Empty icon="lostfound">{tt('Хабарландыру жоқ.', 'Объявлений нет.')}</Empty>}
      <SectionHead title={tt('Менің хабарламаларым', 'Мои сообщения')} />
      {data.claims.length ? <div className="group">{data.claims.map(c => (
        <Row key={c.id} go={'lostfound:' + c.postId} icon="chat" title={c.postTitle ?? tt('Жабылған хабарландыру', 'Закрытое объявление')}
          sub={tt(...CLAIM_STATUS[c.status])} />
      ))}</div> : <p className="small muted">{tt('Әзірге ешкімге жазбадыңыз.', 'Вы пока никому не писали.')}</p>}
    </>
  );
}
