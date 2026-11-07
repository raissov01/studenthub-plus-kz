'use client';
/* «Рөл беру»: the one dialog an admin uses to give or take any right, in plain words. Behind it are the existing
   audited endpoints (users.role, the moderator right, café / print staff, club admins, university staff); the
   person giving the role never needs to know which is which. Also the readable badges of a person's rights and the
   «invite by email» sheet for someone who has not signed in yet. Every endpoint checks the admin role again. */
import { useState, type ReactNode } from 'react';
import { useApp } from '@/components/app';
import { ErrorLine, errText, Loading, useFeature, useLoad } from '@/components/views/common';
import { api, ApiError, useAccount, type Role } from '@/lib/account';
import { reasonOk, who } from '@/lib/console';

type TT = (kk: string, ru: string) => string;

/** the rights a person can have, as the dialog lists them */
export type Access = 'student' | 'admin' | 'moderator' | 'cafe' | 'print' | 'club' | 'ambassador' | 'university_staff';

/** one readable label for a role, a moderator right or a place code of the user list (cafe_manager, club_admin, …) */
export function accessLabel(code: string, tt: TT): string {
  switch (code) {
    case 'admin': return tt('Әкімші', 'Администратор');
    case 'moderator': return tt('Модератор', 'Модератор');
    case 'ambassador': return tt('Амбассадор', 'Амбассадор');
    case 'university_staff': return tt('Университет қызметкері', 'Сотрудник университета');
    case 'employer': return tt('Жұмыс беруші', 'Работодатель');
    case 'cafe_manager': return tt('Кофехана менеджері', 'Менеджер кофейни');
    case 'cafe_barista': return tt('Кофехана баристасы', 'Бариста кофейни');
    case 'print_manager': return tt('Полиграфия менеджері', 'Менеджер полиграфии');
    case 'print_operator': return tt('Полиграфия операторы', 'Оператор полиграфии');
    case 'club_admin': return tt('Клуб әкімшісі', 'Админ клуба');
    default: return tt('Студент', 'Студент');
  }
}

/** every right of a person as badges; a plain student gets one «Студент» badge */
export function AccessBadges({ role, scopes = [], places = [] }: { role: Role | string; scopes?: string[] | null; places?: string[] | null }) {
  const { tt } = useApp();
  const codes = [...(role !== 'student' ? [role] : []), ...(scopes || []), ...(places || []).filter(p => p !== role)];
  if (!codes.length) return <span className="badge b-line">{accessLabel('student', tt)}</span>;
  return <>{codes.map(c => <span key={c} className={`badge ${c === 'admin' ? 'b-bad' : 'b-sky'}`}>{accessLabel(c, tt)}</span>)}</>;
}

interface Place { id: number; nameKk?: string; nameRu?: string | null; name_kk?: string; name_ru?: string | null }
interface Detail {
  user: { id: number; name: string | null; username: string | null; email: string | null; role: Role; university: string | null };
  scopes: { scope: string }[];
  memberships: {
    cafes: { id: number; nameKk: string; nameRu: string; role: string }[]; printShops: { id: number; nameKk: string; nameRu: string; role: string }[];
    clubs: { id: number; nameKk: string; nameRu: string | null }[]; university: string | null;
  };
}

const OPTIONS: { k: Access; t: [string, string]; d: [string, string]; feature?: 'cafe' | 'print' | 'clubs' | 'b2b' }[] = [
  { k: 'student', t: ['Студент', 'Студент'], d: ['Қосымша құқығы жоқ. Әкімші не модератор құқығын алып тастайды.', 'Без дополнительных прав. Снимает права администратора и модератора.'] },
  { k: 'moderator', t: ['Модератор', 'Модератор'], d: ['Шағымдар мен жариялауды қарайды. Қолданушылар мен ақшаны көрмейді.', 'Разбирает жалобы и публикации. Не видит пользователей и деньги.'] },
  { k: 'admin', t: ['Әкімші', 'Администратор'], d: ['Бәрін басқарады, басқаларға рөл береді.', 'Управляет всем, выдаёт роли другим.'] },
  { k: 'cafe', t: ['Кофехана қызметкері', 'Сотрудник кофейни'], d: ['Тек өз кофеханасының тапсырыстары. Қайсысын таңдаңыз.', 'Только заказы своей кофейни. Выберите какой.'], feature: 'cafe' },
  { k: 'print', t: ['Полиграфия қызметкері', 'Сотрудник полиграфии'], d: ['Тек өз полиграфиясының кезегі. Қайсысын таңдаңыз.', 'Только очередь своей полиграфии. Выберите какую.'], feature: 'print' },
  { k: 'club', t: ['Клуб әкімшісі', 'Админ клуба'], d: ['Тек өз клубының беті, мүшелері мен іс-шаралары.', 'Только страница, участники и мероприятия своего клуба.'], feature: 'clubs' },
  { k: 'ambassador', t: ['Амбассадор', 'Амбассадор'], d: ['Өз университетінің іс-шараларын құрады.', 'Создаёт мероприятия своего университета.'] },
  { k: 'university_staff', t: ['Университет қызметкері', 'Сотрудник университета'], d: ['Өз университетінің сауалнамалары мен өтініштері.', 'Опросы и обращения своего университета.'], feature: 'b2b' },
];

const placeName = (p: Place, lang: string) => (lang !== 'kk' ? p.nameRu || p.name_ru : null) || p.nameKk || p.name_kk || `#${p.id}`;

/** «Рөл беру»: pick one right (radio list with one line each), a place when it needs one, an optional reason */
export function GiveRoleSheet({ userId, onDone }: { userId: number; onDone: () => void }) {
  const { tt, lang, toast, closeSheet } = useApp();
  const me = useAccount().user;
  const f = { cafe: useFeature('cafe'), print: useFeature('print'), clubs: useFeature('clubs'), b2b: useFeature('b2b') };
  const d = useLoad<Detail>(`/api/admin/users/${userId}`);
  const [pick, setPick] = useState<Access | ''>('');
  const [place, setPlace] = useState('');
  const [shopRole, setShopRole] = useState<'manager' | 'worker'>('worker');
  const [univ, setUniv] = useState<string | null>(null);
  const [reason, setReason] = useState(tt('Командаға қосылды', 'Добавлен в команду'));
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  const placesPath = pick === 'cafe' ? '/api/admin/cafes' : pick === 'print' ? '/api/admin/print/shops' : pick === 'club' ? '/api/admin/clubs' : null;
  const places = useLoad<{ cafes?: Place[]; shops?: Place[]; clubs?: Place[] }>(placesPath);
  if (!d.data) return <div className="form">{d.err ? <ErrorLine err={d.err} /> : <Loading />}</div>;
  const u = d.data.user, m = d.data.memberships;
  const self = me?.id === u.id;
  const isMod = d.data.scopes.some(s => s.scope === 'moderator');
  const list = places.data ? (places.data.cafes || places.data.shops || places.data.clubs || []) : [];
  const university = univ ?? u.university ?? '';
  const needsPlace = pick === 'cafe' || pick === 'print' || pick === 'club';
  const r = reason.trim();
  const ok = !!pick && (!needsPlace || !!place) && (pick !== 'university_staff' || university.trim().length >= 2) && (r === '' || reasonOk(r));
  const why = r || tt('Командаға қосылды', 'Добавлен в команду');

  // what the person has now, each with its own «Алу» (take away)
  const current: { key: string; label: string; remove: () => Promise<unknown> }[] = [
    ...(u.role !== 'student' && !(u.role === 'university_staff' && m.university) ? [{ key: 'role', label: accessLabel(u.role, tt),
      remove: () => api('/api/admin/role', { method: 'POST', body: { userId: u.id, role: 'student', reason: tt('Рөл алынды', 'Роль снята') }, lang }) }] : []),
    ...(isMod ? [{ key: 'mod', label: accessLabel('moderator', tt),
      remove: () => api('/api/admin/grants/revoke', { method: 'POST', body: { userId: u.id, scope: 'moderator', reason: tt('Рөл алынды', 'Роль снята') }, lang }) }] : []),
    ...m.cafes.map(c => ({ key: 'c' + c.id, label: `${accessLabel('cafe_' + c.role, tt)}: ${placeName(c, lang)}`,
      remove: () => api(`/api/admin/cafes/${c.id}/staff/${u.id}`, { method: 'DELETE', lang }) })),
    ...m.printShops.map(c => ({ key: 'p' + c.id, label: `${accessLabel('print_' + c.role, tt)}: ${placeName(c, lang)}`,
      remove: () => api(`/api/admin/print/shops/${c.id}/staff/${u.id}`, { method: 'DELETE', lang }) })),
    ...m.clubs.map(c => ({ key: 'k' + c.id, label: `${accessLabel('club_admin', tt)}: ${placeName(c, lang)}`,
      remove: () => api(`/api/admin/clubs/${c.id}/admins/${u.id}`, { method: 'DELETE', lang }) })),
    ...(m.university ? [{ key: 'univ', label: `${accessLabel('university_staff', tt)}: ${m.university}`,
      remove: () => api(`/api/admin/university-staff/${u.id}`, { method: 'DELETE', lang }) }] : []),
  ];

  const run = async (fn: () => Promise<unknown>, done: string) => {
    setBusy(true); setErr('');
    try { await fn(); toast(done); onDone(); d.reload(); return true; } catch (e) { setErr(errText(e, tt('Болмады', 'Не получилось'))); return false; } finally { setBusy(false); }
  };
  const give = async () => {
    if (!ok || busy) return;
    const role = (to: Role) => api('/api/admin/role', { method: 'POST', body: { userId: u.id, role: to, reason: why }, lang });
    const done = await run(async () => {
      switch (pick) {
        case 'student':
          if (u.role !== 'student' && !(u.role === 'university_staff' && m.university)) await role('student');
          if (isMod) await api('/api/admin/grants/revoke', { method: 'POST', body: { userId: u.id, scope: 'moderator', reason: why }, lang });
          return;
        case 'admin': case 'ambassador': return role(pick);
        case 'moderator': return api('/api/admin/grants', { method: 'POST', body: { userId: u.id, scope: 'moderator', reason: why }, lang });
        case 'cafe': return api(`/api/admin/cafes/${place}/staff`, { method: 'POST', body: { userId: u.id, role: shopRole === 'manager' ? 'manager' : 'barista', reason: why }, lang });
        case 'print': return api(`/api/admin/print/shops/${place}/staff`, { method: 'POST', body: { userId: u.id, role: shopRole === 'manager' ? 'manager' : 'operator', reason: why }, lang });
        case 'club': return api(`/api/admin/clubs/${place}/admins`, { method: 'POST', body: { userId: u.id }, lang });
        case 'university_staff': return api('/api/admin/university-staff', { method: 'POST', body: { userId: u.id, university: university.trim() }, lang });
      }
    }, pick === 'student' ? tt('Құқықтар алынды', 'Права сняты') : tt('Рөл берілді', 'Роль выдана'));
    if (done) closeSheet();
  };

  return (
    <form className="form give-role" onSubmit={e => { e.preventDefault(); void give(); }}>
      <h2>{tt('Рөл беру', 'Выдать роль')}</h2>
      <p className="small"><b>{who(u)}</b>{u.email ? <span className="muted"> · {u.email}</span> : null}</p>
      {self ? <p className="small muted" role="note">{tt('Өзіңізге рөл бере не ала алмайсыз: мұны басқа әкімші жасайды.', 'Себе роль выдать или снять нельзя: это делает другой администратор.')}</p> : null}
      <div className="give-now">
        <span className="small muted">{tt('Қазір', 'Сейчас')}:</span>
        {current.length ? (
          <ul className="give-now-list">
            {current.map(c => (
              <li key={c.key}><span className="badge b-sky">{c.label}</span>
                {self ? null : <button className="link-btn" type="button" disabled={busy}
                  onClick={() => void run(c.remove, tt('Құқық алынды', 'Право снято'))}>{tt('Алу', 'Снять')}</button>}</li>
            ))}
          </ul>
        ) : <span className="badge b-line">{accessLabel('student', tt)}</span>}
      </div>
      {self ? null : <>
        <fieldset className="give-options">
          <legend className="small"><b>{tt('Қандай рөл берілсін?', 'Какую роль выдать?')}</b></legend>
          {OPTIONS.filter(o => !o.feature || f[o.feature]).map(o => (
            <label key={o.k} className={`give-option${pick === o.k ? ' is-picked' : ''}`}>
              <input type="radio" name="giveRole" value={o.k} checked={pick === o.k} onChange={() => { setPick(o.k); setPlace(''); }} />
              <span><b>{tt(...o.t)}</b><span className="small muted">{tt(...o.d)}</span></span>
            </label>
          ))}
        </fieldset>
        {needsPlace ? (
          <div className="field">
            <label htmlFor="givePlace">{pick === 'club' ? tt('Қай клуб?', 'Какой клуб?') : pick === 'cafe' ? tt('Қай кофехана?', 'Какая кофейня?') : tt('Қай полиграфия?', 'Какая полиграфия?')}</label>
            {places.err ? <ErrorLine err={places.err} /> : !places.data ? <Loading /> : list.length ? (
              <select className="input" id="givePlace" value={place} onChange={e => setPlace(e.target.value)}>
                <option value="">{tt('Таңдаңыз', 'Выберите')}</option>
                {list.map(p => <option key={p.id} value={p.id}>{placeName(p, lang)}</option>)}
              </select>
            ) : <p className="small muted">{tt('Әзірге бірде-біреуі қосылмаған.', 'Пока ни одного не добавлено.')}</p>}
          </div>
        ) : null}
        {pick === 'cafe' || pick === 'print' ? (
          <div className="field"><span className="small"><b>{tt('Не істейді?', 'Что делает?')}</b></span>
            <label className="check small"><input type="radio" name="giveShopRole" checked={shopRole === 'worker'} onChange={() => setShopRole('worker')} />
              {pick === 'cafe' ? tt('Бариста — тапсырыстарды қабылдап, береді', 'Бариста — принимает и выдаёт заказы') : tt('Оператор — тапсырыстарды басып шығарады', 'Оператор — печатает заказы')}</label>
            <label className="check small"><input type="radio" name="giveShopRole" checked={shopRole === 'manager'} onChange={() => setShopRole('manager')} />
              {tt('Менеджер — бәрін қоса, табыс пен команданы көреді', 'Менеджер — всё это плюс выручка и команда')}</label>
          </div>
        ) : null}
        {pick === 'university_staff' ? (
          <div className="field"><label htmlFor="giveUniv">{tt('Қай университет?', 'Какой университет?')}</label>
            <input className="input" id="giveUniv" maxLength={160} value={university} onChange={e => setUniv(e.target.value)} /></div>
        ) : null}
        <div className="field">
          <label htmlFor="giveReason">{tt('Неге? (қысқаша, тарихта сақталады)', 'Почему? (коротко, сохранится в истории)')}</label>
          <input className="input" id="giveReason" maxLength={500} value={reason} onChange={e => setReason(e.target.value)} />
        </div>
      </>}
      <ErrorLine err={err} />
      <div className="btn-row">
        <button className="btn ghost" type="button" onClick={closeSheet}>{tt('Жабу', 'Закрыть')}</button>
        {self ? null : <button className="btn" type="submit" disabled={!ok || busy}>{pick === 'student' ? tt('Құқықтарды алу', 'Снять права') : tt('Рөл беру', 'Выдать роль')}</button>}
      </div>
    </form>
  );
}

/** the sign-in page to send someone */
export const signInLink = () => (typeof window !== 'undefined' ? window.location.origin : '') + '/profile';

/** reserve «Әкімші» or «Модератор» for an email that has not signed in yet; applied on their first sign-in */
export function InviteSheet({ email: initial, onDone }: { email: string; onDone: () => void }) {
  const { tt, lang, toast, closeSheet } = useApp();
  const [email, setEmail] = useState(initial);
  const [grant, setGrant] = useState<'moderator' | 'admin'>('moderator');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  const ok = /^\S+@\S+\.\S+$/.test(email.trim());
  return (
    <form className="form" onSubmit={async e => {
      e.preventDefault();
      if (!ok || busy) return;
      setBusy(true); setErr('');
      try {
        await api('/api/admin/invites', { method: 'POST', body: { email: email.trim(), grant, reason: tt('Командаға шақырылды', 'Приглашён в команду') }, lang });
        closeSheet(); toast(tt('Сақталды: кірген кезде рөл өзі қосылады', 'Сохранено: роль добавится при входе')); onDone();
      } catch (x) { setErr(x instanceof ApiError ? x.message : errText(x)); } finally { setBusy(false); }
    }}>
      <h2>{tt('Email бойынша шақыру', 'Пригласить по email')}</h2>
      <ol className="steps small">
        <li>{tt('Төмендегі сілтемені адамға жіберіңіз.', 'Отправьте человеку ссылку ниже.')}</li>
        <li>{tt('Ол осы email-мен тіркеліп, поштасын растайды.', 'Он регистрируется с этим email и подтверждает почту.')}</li>
        <li>{tt('Рөл өзі қосылады — сізге ештеңе істеудің керегі жоқ.', 'Роль добавится сама — вам больше ничего делать не нужно.')}</li>
      </ol>
      <CopyLink />
      <div className="field"><label htmlFor="invEmail">Email</label>
        <input className="input" id="invEmail" type="email" inputMode="email" autoComplete="off" value={email} onChange={e => setEmail(e.target.value)} /></div>
      <fieldset className="give-options">
        <legend className="small"><b>{tt('Қандай рөл?', 'Какая роль?')}</b></legend>
        {(['moderator', 'admin'] as const).map(k => (
          <label key={k} className={`give-option${grant === k ? ' is-picked' : ''}`}>
            <input type="radio" name="invGrant" checked={grant === k} onChange={() => setGrant(k)} />
            <span><b>{accessLabel(k, tt)}</b><span className="small muted">{tt(...OPTIONS.find(o => o.k === k)!.d)}</span></span>
          </label>
        ))}
      </fieldset>
      <p className="tiny muted">{tt('Кофехана, полиграфия не клуб құқығын адам кіргеннен кейін «Рөл беру» арқылы беріңіз.', 'Права кофейни, полиграфии или клуба выдайте через «Выдать роль» после входа.')}</p>
      <ErrorLine err={err} />
      <div className="btn-row">
        <button className="btn ghost" type="button" onClick={closeSheet}>{tt('Бас тарту', 'Отмена')}</button>
        <button className="btn" type="submit" disabled={!ok || busy}>{tt('Рөлді алдын ала беру', 'Назначить заранее')}</button>
      </div>
    </form>
  );
}

/** «Шақыру сілтемесін көшіру»: the sign-in page link, with a fallback text field when the clipboard is closed */
export function CopyLink({ children }: { children?: ReactNode }) {
  const { tt, toast } = useApp();
  const link = signInLink();
  return (
    <div className="copy-link">
      <input className="input" readOnly aria-label={tt('Кіру сілтемесі', 'Ссылка для входа')} value={link} onFocus={e => e.currentTarget.select()} />
      <button className="btn secondary" type="button" onClick={() => navigator.clipboard?.writeText(link).then(() => toast(tt('Сілтеме көшірілді', 'Ссылка скопирована')),
        () => toast(tt('Көшіру мүмкін болмады: сілтемені қолмен белгілеңіз', 'Не удалось скопировать: выделите ссылку вручную')))}>
        {children ?? tt('Шақыру сілтемесін көшіру', 'Скопировать ссылку-приглашение')}</button>
    </div>
  );
}

export interface InviteRow { id: number; email: string; grant: string; reason: string; createdAt: string; by: { id: number; name: string | null } | null }

/** the emails waiting for their first sign-in, each with «Болдырмау» (cancel) */
export function PendingInvites({ list, onChange }: { list: InviteRow[]; onChange: () => void }) {
  const { tt, lang, toast } = useApp();
  if (!list.length) return null;
  return (
    <section className="card stack" aria-label={tt('Кіруді күтетін шақырулар', 'Приглашения ждут входа')}>
      <b>{tt('Кіруді күтетін шақырулар', 'Приглашения ждут входа')}</b>
      <ul className="give-now-list">
        {list.map(i => (
          <li key={i.id}><span>{i.email}</span> <span className="badge b-sky">{accessLabel(i.grant, tt)}</span>
            <button className="link-btn" type="button" onClick={async () => {
              try { await api(`/api/admin/invites/${i.id}`, { method: 'DELETE', lang }); toast(tt('Шақыру болдырылмады', 'Приглашение отменено')); onChange(); } catch (e) { toast(errText(e)); }
            }}>{tt('Болдырмау', 'Отменить')}</button></li>
        ))}
      </ul>
    </section>
  );
}
