'use client';
/* Student side of the employer cabinets (prompt 10), as small pieces the job screens mount: the verified-employer
   badge and response rate on job rows, the employer block / apply flow / report on a real job's page, «Менің
   өтінімдерім» with real statuses and withdraw, the «Университеттен ресми» badge and the announcements list.
   Demo jobs keep the old local «Өтінім жіберу» (S.applied); only database jobs (source = "employer") use the API. */
import { useCallback, useEffect, useState, useSyncExternalStore, type ReactNode } from 'react';
import { useApp } from '@/components/app';
import { Chip, ConfirmSheet, Empty, Icon, Row, SectionHead } from '@/components/ui';
import { ErrorLine, errCode, errText, Feature, Loading, NotFound, useFeature, useLoad, useNeedAccount, ymd } from '@/components/views/common';
import { api, useAccount } from '@/lib/account';
import {
  APP_STATUS, APPLY_LIMITS, appStatusTone, isEmployerJob, isOfficial, jobDbId, JOB_REPORT_REASONS,
  type Announcement, type AnyJob, type AppStatus, type MyApplication, type SharedFields,
} from '@/lib/b2b';
import { SKILLS } from '@/lib/data/static';
import { parseYmd } from '@/lib/format';
import type { Job, Opportunity } from '@/lib/types';

/* ---------- my applications: one shared copy for the job rows, the job page and «Менің тізімім» ---------- */
let mine: { uid: number; list: MyApplication[] } | null = null;
let loadingUid = 0;
const subs = new Set<() => void>();
const subscribe = (l: () => void) => { subs.add(l); return () => { subs.delete(l); }; };
function loadMine(uid: number) {
  if (loadingUid === uid) return;
  loadingUid = uid;
  api<{ applications: MyApplication[] }>('/api/applications/my').then(
    r => { mine = { uid, list: r.applications }; },
    () => { mine = { uid, list: mine?.uid === uid ? mine.list : [] }; },
  ).finally(() => { loadingUid = 0; subs.forEach(l => l()); });
}

/** my real applications (null while loading, or when signed out / the switch is off) */
export function useMyApplications(): { list: MyApplication[] | null; reload: () => void } {
  const acc = useAccount();
  const on = useFeature('b2b');
  const uid = on && acc.user ? acc.user.id : 0;
  const snap = useSyncExternalStore(subscribe, () => mine, () => null);
  useEffect(() => { if (uid && mine?.uid !== uid) loadMine(uid); }, [uid]);
  const reload = useCallback(() => { if (uid) loadMine(uid); }, [uid]);
  return { list: uid && snap?.uid === uid ? snap.list : null, reload };
}
const myAppFor = (list: MyApplication[] | null, jobId: string) => list?.find(a => a.jobId === jobId) ?? null;

export function AppStatusBadge({ s }: { s: AppStatus }) {
  const { L } = useApp();
  return <span className={`badge ${appStatusTone(s)}`}>{s === 'invited' ? <Icon n="check" s={12} /> : null} {L(APP_STATUS[s])}</span>;
}

/* ---------- job rows ---------- */
/** extra line on a job row for employer jobs: verified employer, response rate, my application status */
export function EmployerJobBits({ j }: { j: Job }) {
  const { tt } = useApp();
  const { list } = useMyApplications();
  if (!isEmployerJob(j)) return null;
  const r = (j as AnyJob).response;
  const my = myAppFor(list, j.id);
  return (
    <span className="badges">
      <span className="badge b-ok"><Icon n="shield" s={12} /> {tt('Тексерілген жұмыс беруші', 'Проверенный работодатель')}</span>
      {r && r.rate != null ? <span className="badge b-line">{tt(`${r.rate}% өтінімге жауап береді`, `Отвечает на ${r.rate}% откликов`)}</span> : null}
      {my ? <AppStatusBadge s={my.status} /> : null}
    </span>
  );
}

/** «Университеттен ресми» on official opportunities (source = "university") */
export function OfficialBadge({ o }: { o: Opportunity }) {
  const { tt } = useApp();
  if (!isOfficial(o)) return null;
  return <span className="badge b-ok"><Icon n="cap" s={12} /> {tt('Университеттен ресми', 'Официально от университета')}</span>;
}

/* ---------- a real job's page ---------- */
/** loads a database job that is not in /api/content (yet, or filtered for age) from GET /api/jobs/{id}: 404 = neutral not found */
export function LiveEmployerJob({ id, render }: { id: string; render: (j: Job) => ReactNode }) {
  return <Feature name="b2b"><LiveJob id={id} render={render} /></Feature>;
}
function LiveJob({ id, render }: { id: string; render: (j: Job) => ReactNode }) {
  const n = jobDbId(id);
  const r = useLoad<{ job: Job }>(n ? `/api/jobs/${n}` : null);
  if (!n) return <NotFound />;
  if (r.err) return r.code === 'job_not_found' || r.code === 'not_found' ? <NotFound /> : <ErrorLine err={r.err} />;
  if (!r.data) return <Loading />;
  return <>{render(r.data.job)}</>;
}

function MeaningSheet({ j }: { j: AnyJob }) {
  const { tt, L, closeSheet } = useApp();
  const e = j.employer!;
  return (
    <>
      <h2>{tt('Тексерілген жұмыс беруші', 'Проверенный работодатель')}</h2>
      <p className="small">{L(e.verification_meaning)}</p>
      <dl className="kv">
        <dt>{tt('Компания', 'Компания')}</dt><dd>{e.name}</dd>
        {e.verified_at ? <><dt>{tt('Тексерілген күні', 'Дата проверки')}</dt><dd>{e.verified_at}</dd></> : null}
        {e.website ? <><dt>{tt('Сайт', 'Сайт')}</dt><dd><a href={e.website} target="_blank" rel="noopener nofollow">{e.website.replace(/^https:\/\//, '')}</a></dd></> : null}
      </dl>
      {e.about ? <p className="small muted" style={{ whiteSpace: 'pre-line', overflowWrap: 'anywhere' }}>{e.about}</p> : null}
      <button className="btn ghost block" type="button" onClick={closeSheet}>{tt('Жабу', 'Закрыть')}</button>
    </>
  );
}

/** badge + address + response numbers + «қалай өтінім беріледі» + report, placed into the job page */
export function EmployerJobInfo({ j }: { j: Job }) {
  const { tt, L, openSheet } = useApp();
  if (!isEmployerJob(j)) return null;
  const x = j as AnyJob, r = x.response;
  return (
    <>
      <div className="card">
        <button type="button" className="badge b-ok verified" style={{ alignSelf: 'flex-start' }} aria-haspopup="dialog" onClick={() => openSheet(<MeaningSheet j={x} />)}>
          <Icon n="shield" s={12} /> {tt('Тексерілген жұмыс беруші', 'Проверенный работодатель')}
        </button>
        <p className="tiny muted">{tt('Тексерілген: компания тіркелген, байланысы нақты. Бұл жұмысқа кепілдік емес.', 'Проверено: компания зарегистрирована, контакты настоящие. Это не гарантия трудоустройства.')}</p>
        <dl className="kv">
          <dt>{tt('Мекенжай', 'Адрес')}</dt><dd>{x.remote ? tt('Қашықтан', 'Удалённо') : x.address || '—'}</dd>
          {r?.days != null ? <><dt>{tt('Жауап', 'Ответ')}</dt><dd>{tt(`Әдетте ${r.days} күнде жауап береді`, `Обычно отвечает за ${r.days} дн.`)}</dd></> : null}
          {r?.rate != null ? <><dt>{tt('Жауап беру', 'Отвечает')}</dt><dd>{tt(`Өтінімдердің ${r.rate}%-ына жауап берді`, `Ответила на ${r.rate}% откликов`)}</dd></> : null}
        </dl>
        {!r ? <p className="tiny muted">{tt('Жауап беру көрсеткіші кемінде 5 өтінім жиналғанда шығады.', 'Показатель ответов появится, когда наберётся хотя бы 5 откликов.')}</p> : null}
      </div>
      {x.apply && L(x.apply) ? (
        <section className="section"><h2>{tt('Қалай өтінім беріледі', 'Как откликнуться')}</h2><p style={{ whiteSpace: 'pre-line', overflowWrap: 'anywhere' }}>{L(x.apply)}</p></section>
      ) : null}
    </>
  );
}

/** «Шағым» on a real job: reason picker → POST /api/jobs/{id}/report */
export function JobReportButton({ j }: { j: Job }) {
  const { tt, openSheet } = useApp();
  const need = useNeedAccount();
  return (
    <button className="btn ghost" type="button" onClick={() => { if (need()) openSheet(<JobReportSheet j={j} />); }}>
      <Icon n="bell" s={18} /> {tt('Вакансияға шағым', 'Пожаловаться на вакансию')}
    </button>
  );
}

function JobReportSheet({ j }: { j: Job }) {
  const { tt, L, lang, toast, closeSheet } = useApp();
  const [reason, setReason] = useState('');
  const [text, setText] = useState('');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  return (
    <form className="form" onSubmit={async e => {
      e.preventDefault();
      if (!reason || busy) return;
      setBusy(true); setErr('');
      try {
        const r = await api<{ alreadyReported: boolean; hidden: boolean }>(`/api/jobs/${jobDbId(j.id)}/report`, { method: 'POST', body: { reason, text: text.trim() }, lang });
        closeSheet();
        toast(r.alreadyReported ? tt('Сіз бұған дейін шағым жібергенсіз', 'Вы уже отправляли жалобу') : r.hidden ? tt('Шағым қабылданды, вакансия тексеруге дейін жасырылды', 'Жалоба принята, вакансия скрыта до проверки') : tt('Шағым жіберілді. Рақмет!', 'Жалоба отправлена. Спасибо!'));
      } catch (e2) { setErr(errText(e2)); } finally { setBusy(false); }
    }}>
      <h2>{tt('Вакансияға шағым', 'Жалоба на вакансию')}</h2>
      <div className="chips wrap" role="group" aria-label={tt('Себебі', 'Причина')}>
        {Object.entries(JOB_REPORT_REASONS).map(([k, l]) => <Chip key={k} pressed={reason === k} onClick={() => setReason(k)}>{L(l)}</Chip>)}
      </div>
      <div className="field"><label htmlFor="jrText">{tt('Толығырақ (міндетті емес, 300 таңбаға дейін)', 'Подробнее (необязательно, до 300 символов)')}</label>
        <textarea className="input" id="jrText" maxLength={300} value={text} onChange={e => setText(e.target.value)} /></div>
      <p className="tiny muted">{tt('Шағымды әкімші тексереді. Бірнеше шағым түссе, вакансия тексеруге дейін жасырылады.', 'Жалобу проверит администратор. При нескольких жалобах вакансия скрывается до проверки.')}</p>
      <ErrorLine err={err} />
      <div className="btn-row">
        <button className="btn ghost" type="button" onClick={closeSheet}>{tt('Бас тарту', 'Отмена')}</button>
        <button className="btn" type="submit" disabled={!reason || busy}>{tt('Жіберу', 'Отправить')}</button>
      </div>
    </form>
  );
}

/** the sticky call-to-action of a real job: apply (sign-in → choose fields → confirm) or my status + withdraw */
export function EmployerApplyCta({ j, children }: { j: Job; children?: ReactNode }) {
  const { tt, openSheet } = useApp();
  const acc = useAccount();
  const need = useNeedAccount();
  const { list, reload } = useMyApplications();
  const my = myAppFor(list, j.id);
  const minorBlocked = !!acc.user && acc.user.isAdult !== true && !!(j as AnyJob).adult_only;
  return (
    <>
      {my ? (
        <div className="card">
          <div className="card-row" style={{ justifyContent: 'space-between', alignItems: 'center', flexWrap: 'wrap' }}>
            <span className="row-title">{tt('Менің өтінімім', 'Мой отклик')}</span><AppStatusBadge s={my.status} />
          </div>
          <MyAppDetails a={my} onChange={reload} />
        </div>
      ) : null}
      <div className="sticky-cta">
        {my ? null : minorBlocked
          ? <button className="btn" type="button" disabled>{tt('18 жастан бастап', 'С 18 лет')}</button>
          : <button className="btn" type="button" onClick={() => { if (need()) openSheet(<ApplySheet j={j} onDone={reload} />); }}><Icon n="send" s={18} /> {tt('Өтінім жіберу', 'Откликнуться')}</button>}
        {children}
      </div>
    </>
  );
}

const cut = (s: string, n: number) => (s.length > n ? s.slice(0, n).trimEnd() : s);

function ApplySheet({ j, onDone }: { j: Job; onDone: () => void }) {
  const { S, tt, L, lang, toast, closeSheet, go } = useApp();
  const acc = useAccount();
  const u = acc.user;
  const skills = S.profile.skills.filter(k => SKILLS[k]).map(k => cut(L(SKILLS[k]), 40)).slice(0, APPLY_LIMITS.skills);
  const exps = (S.profile.exp || '').split('\n').map(s => s.trim()).filter(Boolean).slice(0, APPLY_LIMITS.experience).map(t => ({ title: cut(t, 120), org: '', period: '' }));
  const courseText = [u?.course ? tt(`${u.course}-курс`, `${u.course} курс`) : '', u?.faculty || ''].filter(Boolean).join(', ');
  const [f, setF] = useState({ name: !!u?.name, course: !!courseText, telegram: !!u?.username, skills: skills.length > 0, exp: exps.length > 0, phone: false });
  const [phone, setPhone] = useState(S.profile.phone || '');
  const [letter, setLetter] = useState('');
  const [step, setStep] = useState<'pick' | 'confirm'>('pick');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  if (!u) return null;
  const phoneOk = !f.phone || !phone.trim() || /^\+?[0-9 ()-]{7,20}$/.test(phone.trim());
  const any = f.name || f.course || f.telegram || f.skills || f.exp || (f.phone && phone.trim()) || letter.trim();
  const toggle = (k: keyof typeof f) => setF(x => ({ ...x, [k]: !x[k] }));
  const opt = (k: keyof typeof f, label: string, value: ReactNode, disabled?: boolean, hint?: string) => (
    <label className={`row check${disabled ? ' muted' : ''}`}>
      <input type="checkbox" checked={f[k] && !disabled} disabled={disabled} onChange={() => toggle(k)} />
      <span className="row-main"><span className="row-title">{label}</span><span className="row-sub" style={{ overflowWrap: 'anywhere' }}>{disabled ? hint : value}</span></span>
    </label>
  );
  const share = {
    name: f.name && !!u.name, course: f.course && !!courseText, telegram: f.telegram && !!u.username,
    skills: f.skills ? skills : [], experience: f.exp ? exps : [], phone: f.phone ? phone.trim() : '',
  };
  const sendList = [
    share.name ? `${tt('Аты', 'Имя')}: ${u.name}` : '', share.course ? `${tt('Курс, мамандық', 'Курс, специальность')}: ${courseText}` : '',
    share.telegram ? `Telegram: @${u.username}` : '', share.skills.length ? `${tt('Дағдылар', 'Навыки')}: ${share.skills.join(', ')}` : '',
    share.experience.length ? `${tt('Тәжірибе', 'Опыт')}: ${share.experience.map(e => e.title).join('; ')}` : '', share.phone ? `${tt('Телефон', 'Телефон')}: ${share.phone}` : '',
    letter.trim() ? `${tt('Хат', 'Письмо')}: ${letter.trim()}` : '',
  ].filter(Boolean);
  const company = L(j.org);
  const send = async () => {
    setBusy(true); setErr('');
    try {
      await api(`/api/jobs/${jobDbId(j.id)}/apply`, { method: 'POST', body: { share, letter: letter.trim(), confirm: true }, lang });
      onDone(); closeSheet();
      toast(tt(`Өтінім «${company}» компаниясына жіберілді`, `Отклик отправлен в «${company}»`));
    } catch (e) {
      if (errCode(e) === 'already_applied') onDone();
      setErr(errText(e)); setStep('pick');
    } finally { setBusy(false); }
  };
  if (step === 'confirm') {
    return (
      <>
        <h2>{tt('Жіберуді растаңыз', 'Подтвердите отправку')}</h2>
        <p className="small"><b>{tt(`Осы деректер «${company}» компаниясына жіберіледі:`, `Эти данные будут отправлены компании «${company}»:`)}</b></p>
        <ul className="bullets small">{sendList.map((s, i) => <li key={i} style={{ overflowWrap: 'anywhere' }}>{s}</li>)}</ul>
        <p className="tiny muted">{tt('Басқа ешнәрсе жіберілмейді. Өтінімді кері қайтарсаңыз, компаниядағы көшірмесі өшеді.', 'Больше ничего не передаётся. Если отозвать отклик, его копия у компании удалится.')}</p>
        <ErrorLine err={err} />
        <div className="btn-row">
          <button className="btn ghost" type="button" onClick={() => setStep('pick')}>{tt('Артқа', 'Назад')}</button>
          <button className="btn" type="button" disabled={busy} onClick={send}><Icon n="send" s={18} /> {tt('Жіберу', 'Отправить')}</button>
        </div>
      </>
    );
  }
  const syncHint = tt('Аккаунтта жоқ — Параметрлер → «Аккаунтпен синхрондау»', 'Нет в аккаунте — Параметры → «Синхронизировать с аккаунтом»');
  return (
    <form className="form" onSubmit={e => { e.preventDefault(); if (any && phoneOk) setStep('confirm'); }}>
      <h2>{tt('Өтінім жіберу', 'Отклик на вакансию')}</h2>
      <p className="small muted">{tt(`Компанияға нені жіберетініңізді таңдаңыз: «${company}» тек белгіленгенді көреді.`, `Выберите, что отправить: «${company}» увидит только отмеченное.`)}</p>
      <div className="group">
        {opt('name', tt('Аты', 'Имя'), u.name || '', !u.name, syncHint)}
        {opt('course', tt('Курс және мамандық', 'Курс и специальность'), courseText, !courseText, syncHint)}
        {opt('telegram', 'Telegram username', u.username ? '@' + u.username : '', !u.username, tt('Telegram-да username жоқ', 'В Telegram нет username'))}
        {opt('skills', tt('Дағдылар', 'Навыки'), skills.join(', '), !skills.length, tt('Профильде дағды жоқ', 'В профиле нет навыков'))}
        {opt('exp', tt('Тәжірибе', 'Опыт'), exps.map(e => e.title).join('; '), !exps.length, tt('Профильде тәжірибе жоқ', 'В профиле нет опыта'))}
        {opt('phone', tt('Телефон (міндетті емес)', 'Телефон (необязательно)'), tt('Тек осы өтінімге жазылады', 'Попадёт только в этот отклик'))}
      </div>
      {f.phone ? (
        <div className="field"><label htmlFor="apPhone">{tt('Телефон', 'Телефон')}</label>
          <input className="input" id="apPhone" type="tel" inputMode="tel" autoComplete="tel" maxLength={APPLY_LIMITS.phone} value={phone} onChange={e => setPhone(e.target.value)} />
          {!phoneOk ? <span className="tiny urgent">{tt('Нөмірді тексеріңіз', 'Проверьте номер')}</span> : null}</div>
      ) : null}
      <div className="field"><label htmlFor="apLetter">{tt('Қысқа хат (міндетті емес)', 'Короткое письмо (необязательно)')}</label>
        <textarea className="input" id="apLetter" maxLength={APPLY_LIMITS.letter} value={letter} onChange={e => setLetter(e.target.value)} />
        <span className="tiny muted num">{letter.length} / {APPLY_LIMITS.letter}</span></div>
      <p className="tiny muted">{tt('GPA мен email ешқашан жіберілмейді.', 'GPA и email никогда не передаются.')}</p>
      <ErrorLine err={err} />
      <div className="btn-row">
        <button className="btn ghost" type="button" onClick={() => { closeSheet(); }}>{tt('Бас тарту', 'Отмена')}</button>
        <button className="btn" type="submit" disabled={!any || !phoneOk}>{tt('Жалғастыру', 'Далее')}</button>
      </div>
      {!u.name || !courseText ? <button className="link-btn" type="button" style={{ alignSelf: 'flex-start' }} onClick={() => go('settings')}>{tt('Параметрлерге өту', 'Перейти в параметры')}</button> : null}
    </form>
  );
}

/* ---------- «Менің өтінімдерім» ---------- */
export function SharedList({ s }: { s: SharedFields }) {
  const { tt } = useApp();
  const rows: [string, ReactNode][] = [];
  if (s.name) rows.push([tt('Аты', 'Имя'), s.name]);
  if (s.course != null || s.specialty) rows.push([tt('Курс, мамандық', 'Курс, специальность'), [s.course != null ? tt(`${s.course}-курс`, `${s.course} курс`) : '', s.specialty || ''].filter(Boolean).join(', ')]);
  if (s.telegram) rows.push(['Telegram', s.telegramUrl ? <a href={s.telegramUrl} target="_blank" rel="noopener">{s.telegram}</a> : s.telegram]);
  if (s.phone) rows.push([tt('Телефон', 'Телефон'), <a key="ph" href={`tel:${s.phone.replace(/[^\d+]/g, '')}`}>{s.phone}</a>]);
  if (s.skills?.length) rows.push([tt('Дағдылар', 'Навыки'), s.skills.join(', ')]);
  if (s.experience?.length) rows.push([tt('Тәжірибе', 'Опыт'), s.experience.map(e => [e.title, e.org, e.period].filter(Boolean).join(', ')).join('; ')]);
  if (s.letter) rows.push([tt('Хат', 'Письмо'), <span key="l" style={{ whiteSpace: 'pre-line' }}>{s.letter}</span>]);
  if (!rows.length) return <p className="tiny muted">{tt('Деректер жоқ (өтінім кері қайтарылған).', 'Данных нет (отклик отозван).')}</p>;
  return <dl className="kv payload">{rows.map(([k, v]) => <div key={k} className="kv-row"><dt>{k}</dt><dd>{v}</dd></div>)}</dl>;
}

function MyAppDetails({ a, onChange }: { a: MyApplication; onChange: () => void }) {
  const { tt, lang, toast, openSheet } = useApp();
  const [busy, setBusy] = useState(false);
  const withdraw = () => openSheet(<ConfirmSheet msg={tt('Өтінімді кері қайтарасыз ба? Компаниядағы көшірмесі өшеді, қайта жіберу мүмкін болмайды.', 'Отозвать отклик? Его копия у компании удалится, отправить снова будет нельзя.')} yesLabel={tt('Иә, қайтару', 'Да, отозвать')} yes={async () => {
    setBusy(true);
    try { await api(`/api/applications/${a.id}/withdraw`, { method: 'POST', lang }); toast(tt('Өтінім кері қайтарылды', 'Отклик отозван')); onChange(); } catch (e) { toast(errText(e)); } finally { setBusy(false); }
  }} />);
  return (
    <>
      <span className="tiny muted">{tt('Жіберілді', 'Отправлен')}: {ymd(a.createdAt)}{a.updatedAt && ymd(a.updatedAt) !== ymd(a.createdAt) ? ` · ${tt('жаңартылды', 'обновлён')}: ${ymd(a.updatedAt)}` : ''}</span>
      {a.status === 'invited' && a.inviteMessage ? <p className="small banner card" style={{ whiteSpace: 'pre-line', overflowWrap: 'anywhere' }}>{a.inviteMessage}</p> : null}
      {a.status === 'rejected' && a.inviteMessage ? <p className="small muted" style={{ whiteSpace: 'pre-line', overflowWrap: 'anywhere' }}>{a.inviteMessage}</p> : null}
      {a.status === 'no_response' ? <p className="small">{tt('Компания 14 күнде жауап бермеді. Басқа вакансияларды қара.', 'Компания не ответила за 14 дней. Посмотрите другие вакансии.')}</p> : null}
      {a.status !== 'withdrawn' ? <details><summary className="tiny muted">{tt('Компанияға не жіберілді', 'Что получила компания')}</summary><SharedList s={a.sharedFields} /></details> : null}
      {a.status === 'sent' || a.status === 'viewed' || a.status === 'invited'
        ? <button className="btn ghost" type="button" disabled={busy} onClick={withdraw}>{tt('Кері қайтару', 'Отозвать')}</button> : null}
    </>
  );
}

/** «Менің тізімім» → real applications to employers */
export function MyApplications() {
  const { tt, L, go } = useApp();
  const acc = useAccount();
  const on = useFeature('b2b');
  const { list, reload } = useMyApplications();
  if (!on || !acc.user) return null;
  return (
    <section className="section">
      <SectionHead title={tt('Жұмыс берушілерге өтінімдерім', 'Мои отклики работодателям')} more={<span className="tiny muted num">{list ? list.length : ''}</span>} />
      {!list ? <Loading /> : list.length ? (
        <div className="list">{list.map(a => (
          <div key={a.id} className="card">
            <div className="card-row" style={{ justifyContent: 'space-between', alignItems: 'flex-start', gap: 8, flexWrap: 'wrap' }}>
              <button className="link-btn" type="button" style={{ textAlign: 'left', padding: 0, minHeight: 0, overflowWrap: 'anywhere' }} onClick={() => go('job:' + a.jobId)}>{L({ kk: a.title, ru: a.titleRu })}</button>
              <AppStatusBadge s={a.status} />
            </div>
            <span className="small muted" style={{ overflowWrap: 'anywhere' }}>{a.company}</span>
            <MyAppDetails a={a} onChange={reload} />
          </div>
        ))}</div>
      ) : <div className="group"><div className="empty">{tt('Тексерілген жұмыс берушілерге әлі өтінім жібермедіңіз.', 'Вы ещё не откликались проверенным работодателям.')}</div></div>}
    </section>
  );
}

/* ---------- «Университеттен ресми» announcements ---------- */
export function AnnouncementsEntry() {
  const { tt } = useApp();
  const on = useFeature('b2b');
  if (!on) return null;
  return <Row go="announcements" icon="cap" title={tt('Университеттен ресми', 'Официально от университета')} sub={tt('Университет қызметкерлерінің хабарландырулары', 'Объявления сотрудников университетов')} />;
}

export function Announcements() {
  return <Feature name="b2b"><AnnouncementList /></Feature>;
}
function AnnouncementList() {
  const { tt, L, I } = useApp();
  const r = useLoad<{ announcements: Announcement[] }>('/api/announcements');
  if (r.err) return <ErrorLine err={r.err} />;
  if (!r.data) return <Loading />;
  if (!r.data.announcements.length) return <Empty icon="cap">{tt('Әзірге ресми хабарландыру жоқ.', 'Официальных объявлений пока нет.')}</Empty>;
  return (
    <div className="list">
      {r.data.announcements.map(a => (
        <article key={a.id} className="card">
          <div className="badges"><span className="badge b-ok"><Icon n="cap" s={12} /> {tt('Университеттен ресми', 'Официально от университета')}</span><span className="badge b-line">{a.university}</span></div>
          <h3 style={{ overflowWrap: 'anywhere' }}>{L(a.title)}</h3>
          <p className="small" style={{ whiteSpace: 'pre-line', overflowWrap: 'anywhere' }}>{L(a.body)}</p>
          <span className="tiny muted">{I.fDateY(parseYmd(ymd(a.publishedAt)))}{a.until ? ` · ${tt('дейін', 'до')} ${I.fDateY(parseYmd(a.until))}` : ''}</span>
          {a.url ? <a className="btn ghost" href={a.url} target="_blank" rel="noopener"><Icon n="ext" s={18} /> {tt('Толығырақ', 'Подробнее')}</a> : null}
        </article>
      ))}
    </div>
  );
}
