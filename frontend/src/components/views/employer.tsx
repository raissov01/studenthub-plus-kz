'use client';
/* Employer cabinet (prompt 10): /employer (company card and status, response stats, jobs with application counts,
   members, invites, transfer / leave / close), /employer/apply (verification request), /employer/join/<code>,
   /employer/jobs/new and /employer/jobs/<id>/edit (rules screen once, then the job form), /employer/jobs/<id>
   (applications: only what the student chose to share, status buttons). Plus the admin screens /admin/employers and
   /admin/staff (university staff binding). Everything sits behind the b2b switch; other people get the neutral
   not-found screen or a sign-in prompt. */
import { useState, type ReactNode } from 'react';
import { copyText, useApp } from '@/components/app';
import { Chip, ConfirmSheet, Empty, Icon, Row, SectionHead } from '@/components/ui';
import { AdminOnly, ErrorLine, errText, Feature, hasAccount, Loading, NotFound, ReasonSheet, SignInCard, useLoad, ymd } from '@/components/views/common';
import { AppStatusBadge, SharedList } from '@/components/views/jobs-b2b';
import { roleLabel } from '@/components/views/account';
import { api, useAccount, type Role } from '@/lib/account';
import {
  EMPLOYER_STATUS, JOB_RULES, JOB_STATUS, jobStatusTone, PER_LABEL,
  type AdminEmployer, type AppStatus, type EmployerApplication, type EmployerJob, type EmployerMe, type Member, type ResponseStats, type StaffRow,
} from '@/lib/b2b';
import { CITIES, HOME_CITIES, JOB_TAGS, SKILLS, SPECS } from '@/lib/data/static';
import { money } from '@/lib/format';
import type { LText } from '@/lib/types';

export function EmployerCabinet({ prm }: { prm: string }) {
  return <Feature name="b2b"><EmployerRoute prm={prm} /></Feature>;
}

function EmployerRoute({ prm }: { prm: string }) {
  let m: RegExpExecArray | null;
  if (!prm) return <Gate><Cabinet /></Gate>;
  if (prm === 'apply') return <Gate><ApplyCompany /></Gate>;
  if ((m = /^join\/([A-Za-z0-9_-]{4,64})$/.exec(prm))) return <Gate><Join code={m[1]} /></Gate>;
  if (prm === 'jobs/new') return <Gate><JobFormPage /></Gate>;
  if ((m = /^jobs\/(\d{1,12})$/.exec(prm))) return <Gate><JobApplications id={Number(m[1])} /></Gate>;
  if ((m = /^jobs\/(\d{1,12})\/edit$/.exec(prm))) return <Gate><JobFormPage id={Number(m[1])} /></Gate>;
  return <NotFound />;
}

function Gate({ children }: { children: ReactNode }) {
  const { tt } = useApp();
  const acc = useAccount();
  if (!acc.loaded) return <Loading />;
  if (!hasAccount(acc)) return <SignInCard text={tt('Жұмыс беруші кабинеті Telegram аккаунтымен ашылады.', 'Кабинет работодателя открывается через аккаунт Telegram.')} />;
  return <>{children}</>;
}

const ErrorOrLoading = ({ err }: { err: string }) => (err ? <ErrorLine err={err} /> : <Loading />);

function ResponseLine({ r }: { r: ResponseStats | null | undefined }) {
  const { tt } = useApp();
  if (!r) return <p className="tiny muted">{tt('Жауап беру көрсеткіші кемінде 5 өтінім жиналғанда шығады. Ол студенттерге вакансия бетінде көрсетіледі.', 'Показатель ответов появится от 5 откликов. Студенты видят его на странице вакансии.')}</p>;
  return (
    <div className="stat-grid">
      <div className="stat"><b className="num">{r.applications}</b><span>{tt('Өтінім', 'Откликов')}</span></div>
      <div className="stat"><b className="num">{r.rate != null ? r.rate + '%' : '—'}</b><span>{tt('Жауап берілгені', 'С ответом')}</span></div>
      <div className="stat"><b className="num">{r.days != null ? r.days : '—'}</b><span>{tt('Әдетте неше күнде жауап', 'Дней до ответа обычно')}</span></div>
    </div>
  );
}

/* ---------- /employer ---------- */
function Cabinet() {
  const me = useLoad<EmployerMe>('/api/employer/me');
  if (!me.data) return <ErrorOrLoading err={me.err} />;
  const e = me.data.employer;
  if (!e) return <Intro meaning={me.data.verificationMeaning} />;
  if (e.status !== 'verified') return <CompanyStatus me={me.data} />;
  return <Verified me={me.data} reload={me.reload} />;
}

function Meaning({ m }: { m: LText }) {
  const { L } = useApp();
  return <p className="tiny muted"><Icon n="shield" s={13} /> {L(m)}</p>;
}

function Intro({ meaning }: { meaning: LText }) {
  const { tt, go } = useApp();
  const [code, setCode] = useState('');
  const c = code.trim().replace(/^.*\/employer\/join\//, '');
  return (
    <>
      <div className="card">
        <h2>{tt('Жұмыс берушілерге', 'Для работодателей')}</h2>
        <p className="small">{tt('Тексерілген компания вакансияны өзі жариялап, студенттердің өтінімдеріне кабинетте жауап береді. Студенттерге бәрі тегін.', 'Проверенная компания сама публикует вакансии и отвечает на отклики студентов в кабинете. Для студентов всё бесплатно.')}</p>
        <Meaning m={meaning} />
        <button className="btn" type="button" onClick={() => go('employer:apply')}><Icon n="briefcase" s={18} /> {tt('Компанияны тіркеу', 'Зарегистрировать компанию')}</button>
      </div>
      <form className="card" onSubmit={e => { e.preventDefault(); if (/^[A-Za-z0-9_-]{4,64}$/.test(c)) go('employer:join/' + c); }}>
        <h3>{tt('Әріптесіңіз шақырды ма?', 'Вас пригласил коллега?')}</h3>
        <div className="field"><label htmlFor="emCode">{tt('Шақыру коды не сілтемесі', 'Код или ссылка приглашения')}</label>
          <input className="input" id="emCode" autoComplete="off" value={code} onChange={e => setCode(e.target.value)} /></div>
        <button className="btn secondary" type="submit" disabled={!/^[A-Za-z0-9_-]{4,64}$/.test(c)}>{tt('Қосылу', 'Вступить')}</button>
      </form>
    </>
  );
}

function CompanyStatus({ me }: { me: EmployerMe }) {
  const { tt, L, go } = useApp();
  const e = me.employer!;
  return (
    <div className="card">
      <div className="badges"><span className={`badge ${e.status === 'pending' ? 'b-sky' : 'b-bad'}`}>{L(EMPLOYER_STATUS[e.status])}</span></div>
      <h2 style={{ overflowWrap: 'anywhere' }}>{e.name}</h2>
      {e.status === 'pending' ? <p className="small">{tt('Өтінім әкімшіде. Ол БСН-ді ресми тізілімнен, сайтты не 2GIS бетін тексереді. Шешім Telegram-ға келеді.', 'Заявка у администратора: он проверит БИН по официальному реестру и сайт или страницу 2GIS. Решение придёт в Telegram.')}</p> : null}
      {e.status === 'rejected' ? <p className="small">{tt('Өтінім қайтарылды. Түзетіп, қайта жіберуге болады.', 'Заявка отклонена. Можно исправить и отправить снова.')}</p> : null}
      {e.status === 'suspended' ? <p className="small">{tt('Компания тоқтатылды: вакансиялар студенттерге көрінбейді. Сұрақ болса, әкімшіге жазыңыз.', 'Компания приостановлена: вакансии скрыты от студентов. По вопросам пишите администратору.')}</p> : null}
      {e.status === 'closed' ? <p className="small">{tt('Компания жабылған. Жаңа өтінім беруге болады.', 'Компания закрыта. Можно подать новую заявку.')}</p> : null}
      {e.statusReason ? <p className="small"><b>{tt('Себебі', 'Причина')}:</b> <span style={{ overflowWrap: 'anywhere' }}>{e.statusReason}</span></p> : null}
      <Meaning m={me.verificationMeaning} />
      {e.status === 'rejected' || e.status === 'closed' ? <button className="btn" type="button" onClick={() => go('employer:apply')}>{tt('Қайта өтінім беру', 'Подать заявку снова')}</button> : null}
    </div>
  );
}

function Verified({ me, reload }: { me: EmployerMe; reload: () => void }) {
  const { tt, L, go, openSheet } = useApp();
  const e = me.employer!;
  const jobs = useLoad<{ jobs: EmployerJob[] }>('/api/employer/jobs');
  return (
    <>
      <div className="card">
        <div className="badges"><span className="badge b-ok"><Icon n="shield" s={12} /> {L(EMPLOYER_STATUS.verified)}</span><span className="badge b-line">{me.role === 'owner' ? tt('Иесі', 'Владелец') : tt('Мүше', 'Участник')}</span></div>
        <h2 style={{ overflowWrap: 'anywhere' }}>{e.name}</h2>
        <dl className="kv">
          <dt>{tt('БСН', 'БИН')}</dt><dd className="num">{e.bin}</dd>
          <dt>{tt('Сайт', 'Сайт')}</dt><dd><a href={e.website} target="_blank" rel="noopener nofollow">{e.website.replace(/^https:\/\//, '')}</a></dd>
          <dt>{tt('Байланыс', 'Контакт')}</dt><dd>{[e.contactName, e.contactPhone].filter(Boolean).join(', ')}</dd>
          {e.address ? <><dt>{tt('Мекенжай', 'Адрес')}</dt><dd>{e.address}</dd></> : null}
        </dl>
        {e.about ? <p className="small muted" style={{ whiteSpace: 'pre-line', overflowWrap: 'anywhere' }}>{e.about}</p> : null}
        <Meaning m={me.verificationMeaning} />
        <button className="btn ghost" type="button" onClick={() => openSheet(<ContactSheet me={me} onDone={reload} />)}><Icon n="edit" s={18} /> {tt('Байланысты өзгерту', 'Изменить контакты')}</button>
      </div>
      <section className="section"><SectionHead title={tt('Жауап беру көрсеткіші', 'Показатель ответов')} /><ResponseLine r={me.response} /></section>
      <section className="section">
        <SectionHead title={tt('Вакансиялар', 'Вакансии')} more={<button className="link-btn" type="button" onClick={() => go('employer:jobs/new')}>+ {tt('Жаңа вакансия', 'Новая вакансия')}</button>} />
        {!jobs.data ? <ErrorOrLoading err={jobs.err} /> : jobs.data.jobs.length ? (
          <div className="list">{jobs.data.jobs.map(j => <JobCard key={j.id} j={j} />)}</div>
        ) : <Empty icon="briefcase" action={<button className="btn" type="button" onClick={() => go('employer:jobs/new')}>{tt('Алғашқы вакансияны жариялау', 'Опубликовать первую вакансию')}</button>}>
          {tt('Әзірге вакансия жоқ. Алғашқы 3 вакансияны әкімші тексереді.', 'Вакансий пока нет. Первые 3 вакансии проверяет администратор.')}</Empty>}
      </section>
      <Members me={me} onLeft={reload} />
    </>
  );
}

function ContactSheet({ me, onDone }: { me: EmployerMe; onDone: () => void }) {
  const { tt, lang, toast, closeSheet } = useApp();
  const e = me.employer!;
  const [f, setF] = useState({ website: e.website, contactName: e.contactName, contactPhone: e.contactPhone, address: e.address, about: e.about });
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  const set = (k: keyof typeof f) => (ev: { target: { value: string } }) => setF(x => ({ ...x, [k]: ev.target.value }));
  return (
    <form className="form" onSubmit={async ev => {
      ev.preventDefault(); setBusy(true); setErr('');
      try {
        await api('/api/employer/me', { method: 'PATCH', body: { website: f.website.trim(), contactName: f.contactName.trim(), contactPhone: f.contactPhone.trim(), address: f.address.trim(), about: f.about.trim() }, lang });
        closeSheet(); toast(tt('Сақталды', 'Сохранено')); onDone();
      } catch (e2) { setErr(errText(e2)); } finally { setBusy(false); }
    }}>
      <h2>{tt('Байланысты өзгерту', 'Изменить контакты')}</h2>
      <p className="tiny muted">{tt('Атауы мен БСН тексеруден кейін өзгермейді.', 'Название и БИН после проверки не меняются.')}</p>
      <div className="field"><label htmlFor="ecSite">{tt('Сайт не 2GIS сілтемесі (https://)', 'Сайт или ссылка 2GIS (https://)')}</label><input className="input" id="ecSite" type="url" required maxLength={300} value={f.website} onChange={set('website')} /></div>
      <div className="field"><label htmlFor="ecName">{tt('Байланыс адамы', 'Контактное лицо')}</label><input className="input" id="ecName" required minLength={2} maxLength={80} value={f.contactName} onChange={set('contactName')} /></div>
      <div className="field"><label htmlFor="ecPhone">{tt('Телефон', 'Телефон')}</label><input className="input" id="ecPhone" type="tel" maxLength={30} value={f.contactPhone} onChange={set('contactPhone')} /></div>
      <div className="field"><label htmlFor="ecAddr">{tt('Мекенжай', 'Адрес')}</label><input className="input" id="ecAddr" maxLength={200} value={f.address} onChange={set('address')} /></div>
      <div className="field"><label htmlFor="ecAbout">{tt('Компания туралы', 'О компании')}</label><textarea className="input" id="ecAbout" maxLength={1000} value={f.about} onChange={set('about')} /></div>
      <ErrorLine err={err} />
      <div className="btn-row">
        <button className="btn ghost" type="button" onClick={closeSheet}>{tt('Бас тарту', 'Отмена')}</button>
        <button className="btn" type="submit" disabled={busy}>{tt('Сақтау', 'Сохранить')}</button>
      </div>
    </form>
  );
}

function JobCard({ j }: { j: EmployerJob }) {
  const { tt, L, go } = useApp();
  const a = j.applications || {};
  const counts: [AppStatus, string][] = [['sent', tt('жаңа', 'новых')], ['viewed', tt('қаралды', 'просмотрено')], ['invited', tt('шақырылды', 'приглашено')], ['rejected', tt('бас тартылды', 'отказ')], ['no_response', tt('жауапсыз', 'без ответа')]];
  const shown = counts.filter(([k]) => a[k]);
  return (
    <button className="card" type="button" onClick={() => go('employer:jobs/' + j.id)}>
      <div className="badges">
        <span className={`badge ${jobStatusTone(j.status)}`}>{L(JOB_STATUS[j.status])}</span>
        {j.status === 'hidden' && j.hiddenReason === 'reports' ? <span className="badge b-line">{tt('шағымдар бойынша', 'по жалобам')}</span> : null}
        {!j.minorOk ? <span className="badge b-line">18+</span> : null}
      </div>
      <h3 style={{ overflowWrap: 'anywhere' }}>{L({ kk: j.titleKk, ru: j.titleRu })}</h3>
      <span className="small muted">{money(j.pay)} · {L(PER_LABEL[j.per]).toLowerCase()}{j.expiresAt && (j.status === 'published' || j.status === 'expired') ? ` · ${tt('мерзімі', 'до')} ${ymd(j.expiresAt)}` : ''}</span>
      <span className="small">{shown.length ? shown.map(([k, l]) => `${a[k]} ${l}`).join(' · ') : tt('Өтінім әлі жоқ', 'Откликов пока нет')}</span>
    </button>
  );
}

function Members({ me, onLeft }: { me: EmployerMe; onLeft: () => void }) {
  const { tt, lang, toast, openSheet, closeSheet } = useApp();
  const list = useLoad<{ members: Member[] }>('/api/employer/members');
  const [invite, setInvite] = useState<{ link: string; expiresAt: string } | null>(null);
  const owner = me.role === 'owner';
  const run = async (fn: () => Promise<unknown>, done: string, after?: () => void) => {
    try { await fn(); toast(done); (after ?? list.reload)(); } catch (e) { toast(errText(e)); }
  };
  const name = (m: Member) => m.name || (m.username ? '@' + m.username : `#${m.userId}`);
  return (
    <section className="section">
      <SectionHead title={tt('Компания мүшелері', 'Участники компании')} />
      {!list.data ? <ErrorOrLoading err={list.err} /> : (
        <div className="group">
          {list.data.members.map(m => (
            <Row key={m.userId} icon="user" chev={false} title={name(m) + (m.me ? tt(' (сіз)', ' (вы)') : '')}
              sub={[m.username ? '@' + m.username : '', m.role === 'owner' ? tt('иесі', 'владелец') : tt('мүше', 'участник'), ymd(m.joinedAt)].filter(Boolean).join(' · ')}
              end={owner && !m.me ? (
                <span className="btn-row" style={{ flexWrap: 'nowrap', gap: 6 }}>
                  <button className="link-btn" type="button" onClick={() => openSheet(<ConfirmSheet msg={tt(`Компанияны ${name(m)} мүшесіне беру керек пе? Сіз қарапайым мүше боласыз.`, `Передать компанию участнику ${name(m)}? Вы станете обычным участником.`)}
                    yesLabel={tt('Иә, беру', 'Да, передать')} yes={() => run(() => api('/api/employer/transfer', { method: 'POST', body: { userId: m.userId }, lang }), tt('Компания берілді', 'Компания передана'), onLeft)} />)}>{tt('Беру', 'Передать')}</button>
                  <button className="link-btn" type="button" style={{ color: 'var(--danger)' }} onClick={() => openSheet(<ConfirmSheet msg={tt(`${name(m)} компаниядан шығарылсын ба?`, `Удалить ${name(m)} из компании?`)}
                    yesLabel={tt('Иә, шығару', 'Да, удалить')} yes={() => run(() => api(`/api/employer/members/${m.userId}`, { method: 'DELETE', lang }), tt('Шығарылды', 'Удалён'))} />)}>{tt('Шығару', 'Удалить')}</button>
                </span>
              ) : undefined} />
          ))}
        </div>
      )}
      {owner ? (
        <div className="card">
          <p className="small">{tt('Әріптесті шақыру: бір реттік сілтеме 7 күн жарамды.', 'Пригласить коллегу: одноразовая ссылка действует 7 дней.')}</p>
          {invite ? (
            <>
              <div className="pre" style={{ overflowWrap: 'anywhere' }}>{invite.link}</div>
              <span className="tiny muted">{tt('Жарамды', 'Действует до')}: {ymd(invite.expiresAt)}</span>
              <button className="btn secondary" type="button" onClick={() => copyText(invite.link, toast, tt)}><Icon n="copy" s={18} /> {tt('Көшіру', 'Скопировать')}</button>
            </>
          ) : (
            <button className="btn secondary" type="button" onClick={() => run(async () => setInvite(await api<{ link: string; expiresAt: string }>('/api/employer/invites', { method: 'POST', lang })), tt('Сілтеме жасалды', 'Ссылка создана'))}>
              <Icon n="plus" s={18} /> {tt('Шақыру сілтемесін жасау', 'Создать ссылку-приглашение')}
            </button>
          )}
        </div>
      ) : null}
      <div className="btn-row">
        {owner ? null : (
          <button className="btn ghost" type="button" onClick={() => openSheet(<ConfirmSheet msg={tt('Компаниядан шығасыз ба?', 'Выйти из компании?')}
            yesLabel={tt('Иә, шығу', 'Да, выйти')} yes={() => run(() => api('/api/employer/leave', { method: 'POST', lang }), tt('Компаниядан шықтыңыз', 'Вы вышли из компании'), onLeft)} />)}>{tt('Компаниядан шығу', 'Выйти из компании')}</button>
        )}
        {owner ? (
          <button className="btn danger" type="button" onClick={() => openSheet(<CloseSheet onDone={() => { closeSheet(); toast(tt('Компания жабылды', 'Компания закрыта')); onLeft(); }} />)}>{tt('Компанияны жабу', 'Закрыть компанию')}</button>
        ) : null}
      </div>
      {owner ? <p className="tiny muted">{tt('Иесі кетер алдында компанияны басқа мүшеге беруі керек. Жабу: барлық вакансия жабылып, мүшелер шығарылады.', 'Перед уходом владелец передаёт компанию другому участнику. Закрытие: все вакансии закрываются, участники удаляются.')}</p> : null}
    </section>
  );
}

function CloseSheet({ onDone }: { onDone: () => void }) {
  const { tt, lang, closeSheet } = useApp();
  const [v, setV] = useState('');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  return (
    <form className="form" onSubmit={async e => {
      e.preventDefault(); if (v !== 'CLOSE') return;
      setBusy(true); setErr('');
      try { await api('/api/employer/close', { method: 'POST', body: { confirm: 'CLOSE' }, lang }); onDone(); } catch (e2) { setErr(errText(e2)); } finally { setBusy(false); }
    }}>
      <h2>{tt('Компанияны жабу', 'Закрыть компанию')}</h2>
      <p className="small">{tt('Барлық вакансия жабылады, мүшелер шығарылады. Растау үшін CLOSE деп жазыңыз.', 'Все вакансии закроются, участники будут удалены. Для подтверждения введите CLOSE.')}</p>
      <div className="field"><label htmlFor="ecClose">CLOSE</label><input className="input" id="ecClose" autoComplete="off" value={v} onChange={e => setV(e.target.value)} /></div>
      <ErrorLine err={err} />
      <div className="btn-row">
        <button className="btn ghost" type="button" onClick={closeSheet}>{tt('Бас тарту', 'Отмена')}</button>
        <button className="btn danger" type="submit" disabled={v !== 'CLOSE' || busy}>{tt('Жабу', 'Закрыть')}</button>
      </div>
    </form>
  );
}

/* ---------- /employer/apply ---------- */
function ApplyCompany() {
  const { tt, lang, toast, go } = useApp();
  const me = useLoad<EmployerMe>('/api/employer/me');
  const [f, setF] = useState({ name: '', bin: '', website: '', contactName: '', contactPhone: '', address: '', about: '' });
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  const set = (k: keyof typeof f) => (ev: { target: { value: string } }) => setF(x => ({ ...x, [k]: ev.target.value }));
  if (!me.data) return <ErrorOrLoading err={me.err} />;
  const cur = me.data.employer;
  if (cur && cur.status !== 'rejected' && cur.status !== 'closed') return <CompanyStatus me={me.data} />;
  const bin = f.bin.replace(/\s/g, '');
  const site = f.website.trim();
  const okSite = /^https:\/\/[^\s<>"'`]{2,}$/i.test(site);
  const ok = f.name.trim().length >= 2 && /^\d{12}$/.test(bin) && okSite && f.contactName.trim().length >= 2;
  return (
    <form className="form" onSubmit={async e => {
      e.preventDefault(); if (!ok || busy) return;
      setBusy(true); setErr('');
      try {
        await api('/api/employer/apply', { method: 'POST', lang, body: { name: f.name.trim(), bin, website: site, contactName: f.contactName.trim(), contactPhone: f.contactPhone.trim(), address: f.address.trim(), about: f.about.trim() } });
        toast(tt('Өтінім жіберілді. Әкімші тексереді', 'Заявка отправлена. Её проверит администратор'));
        go('employer', { replace: true });
      } catch (e2) { setErr(errText(e2)); } finally { setBusy(false); }
    }}>
      <p className="small">{tt('Әкімші БСН-ді ресми тізілімнен, сайтты не 2GIS бетін тексереді. Тексерілген компания вакансияларды өзі жариялайды.', 'Администратор проверит БИН по официальному реестру и сайт или страницу 2GIS. Проверенная компания сама публикует вакансии.')}</p>
      <Meaning m={me.data.verificationMeaning} />
      {cur?.statusReason ? <p className="small"><b>{tt('Алдыңғы өтінім қайтарылды', 'Прошлая заявка отклонена')}:</b> <span style={{ overflowWrap: 'anywhere' }}>{cur.statusReason}</span></p> : null}
      <div className="field"><label htmlFor="eaName">{tt('Компания атауы', 'Название компании')}</label><input className="input" id="eaName" required minLength={2} maxLength={120} value={f.name} onChange={set('name')} /></div>
      <div className="field"><label htmlFor="eaBin">{tt('БСН (12 сан)', 'БИН (12 цифр)')}</label><input className="input num" id="eaBin" inputMode="numeric" required maxLength={14} value={f.bin} onChange={set('bin')} />
        {f.bin && !/^\d{12}$/.test(bin) ? <span className="tiny urgent">{tt('12 сан болуы керек', 'Нужно 12 цифр')}</span> : null}</div>
      <div className="field"><label htmlFor="eaSite">{tt('Сайт не 2GIS сілтемесі (https://)', 'Сайт или ссылка 2GIS (https://)')}</label><input className="input" id="eaSite" type="url" required maxLength={300} placeholder="https://" value={f.website} onChange={set('website')} />
        {site && !okSite ? <span className="tiny urgent">{tt('Сілтеме https:// деп басталуы керек', 'Ссылка должна начинаться с https://')}</span> : null}</div>
      <div className="field"><label htmlFor="eaContact">{tt('Байланыс адамы', 'Контактное лицо')}</label><input className="input" id="eaContact" required minLength={2} maxLength={80} value={f.contactName} onChange={set('contactName')} /></div>
      <div className="field"><label htmlFor="eaPhone">{tt('Телефон (міндетті емес)', 'Телефон (необязательно)')}</label><input className="input" id="eaPhone" type="tel" maxLength={30} value={f.contactPhone} onChange={set('contactPhone')} /></div>
      <div className="field"><label htmlFor="eaAddr">{tt('Мекенжай (міндетті емес)', 'Адрес (необязательно)')}</label><input className="input" id="eaAddr" maxLength={200} value={f.address} onChange={set('address')} /></div>
      <div className="field"><label htmlFor="eaAbout">{tt('Компания туралы (міндетті емес)', 'О компании (необязательно)')}</label><textarea className="input" id="eaAbout" maxLength={1000} value={f.about} onChange={set('about')} /></div>
      <ErrorLine err={err} />
      <button className="btn block" type="submit" disabled={!ok || busy}>{tt('Тексеруге жіберу', 'Отправить на проверку')}</button>
    </form>
  );
}

/* ---------- /employer/join/<code> ---------- */
function Join({ code }: { code: string }) {
  const { tt, lang, toast, go } = useApp();
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  return (
    <div className="card">
      <h2>{tt('Компанияға қосылу', 'Вступить в компанию')}</h2>
      <p className="small">{tt('Шақыруды қабылдасаңыз, компания кабинеті, вакансиялары мен өтінімдері сізге ашылады. Бір аккаунт тек бір компанияда бола алады.', 'Приняв приглашение, вы получите доступ к кабинету компании, её вакансиям и откликам. Один аккаунт — одна компания.')}</p>
      <ErrorLine err={err} />
      <button className="btn" type="button" disabled={busy} onClick={async () => {
        setBusy(true); setErr('');
        try {
          const r = await api<{ company: string }>('/api/employer/join', { method: 'POST', body: { code }, lang });
          toast(tt(`«${r.company}» компаниясына қосылдыңыз`, `Вы в компании «${r.company}»`));
          go('employer', { replace: true });
        } catch (e) { setErr(errText(e)); } finally { setBusy(false); }
      }}>{tt('Шақыруды қабылдау', 'Принять приглашение')}</button>
    </div>
  );
}

/* ---------- /employer/jobs/new, /employer/jobs/<id>/edit ---------- */
interface JobForm {
  titleKk: string; titleRu: string; descKk: string; descRu: string; city: string; remote: boolean; address: string; pay: string;
  per: 'shift' | 'hour' | 'month'; hours: string; schedKk: string; schedRu: string; tags: string[]; skills: string[]; spec: string[];
  req: LText[]; applyKk: string; applyRu: string; minorOk: '' | 'yes' | 'no';
}
const emptyForm = (): JobForm => ({
  titleKk: '', titleRu: '', descKk: '', descRu: '', city: 'astana', remote: false, address: '', pay: '', per: 'shift', hours: '', schedKk: '', schedRu: '',
  tags: [], skills: [], spec: [], req: [{ kk: '', ru: '' }], applyKk: '', applyRu: '', minorOk: '',
});
const formOf = (j: EmployerJob): JobForm => ({
  titleKk: j.titleKk, titleRu: j.titleRu, descKk: j.descKk, descRu: j.descRu, city: j.city === 'remote' ? 'astana' : j.city, remote: j.remote, address: j.address,
  pay: String(j.pay), per: j.per, hours: j.hours != null ? String(j.hours) : '', schedKk: j.schedKk, schedRu: j.schedRu, tags: j.tags.filter(t => t !== 'remote'),
  skills: j.skills, spec: j.spec.filter(s => s !== 'all'), req: j.req.length ? j.req : [{ kk: '', ru: '' }], applyKk: j.applyKk, applyRu: j.applyRu, minorOk: j.minorOk ? 'yes' : 'no',
});
const len = (s: string) => s.trim().length;

function JobFormPage({ id }: { id?: number }) {
  const me = useLoad<EmployerMe>('/api/employer/me');
  const jobs = useLoad<{ jobs: EmployerJob[] }>(id ? '/api/employer/jobs' : null);
  const [rulesOk, setRulesOk] = useState(false);
  if (!me.data) return <ErrorOrLoading err={me.err} />;
  if (!me.data.employer || me.data.employer.status !== 'verified') return <CompanyGate me={me.data} />;
  if (id) {
    if (!jobs.data) return <ErrorOrLoading err={jobs.err} />;
    const j = jobs.data.jobs.find(x => x.id === id);
    if (!j) return <NotFound />;
    return <JobEditor job={j} />;
  }
  if (!me.data.rulesAccepted && !rulesOk) return <Rules onAccept={() => setRulesOk(true)} />;
  return <JobEditor />;
}

function CompanyGate({ me }: { me: EmployerMe }) {
  const { tt, go } = useApp();
  if (me.employer) return <CompanyStatus me={me} />;
  return <Empty icon="briefcase" action={<button className="btn" type="button" onClick={() => go('employer:apply')}>{tt('Компанияны тіркеу', 'Зарегистрировать компанию')}</button>}>
    {tt('Вакансия жариялау үшін алдымен компанияны тіркеп, тексеруден өтіңіз.', 'Чтобы публиковать вакансии, сначала зарегистрируйте компанию и пройдите проверку.')}</Empty>;
}

function Rules({ onAccept }: { onAccept: () => void }) {
  const { tt, L, lang, toast } = useApp();
  const [agree, setAgree] = useState(false);
  const [busy, setBusy] = useState(false);
  return (
    <div className="card">
      <h2>{tt('Вакансия жариялау ережелері', 'Правила публикации вакансий')}</h2>
      <ul className="bullets small">{JOB_RULES.map((r, i) => <li key={i}>{L(r)}</li>)}</ul>
      <p className="tiny muted">{tt('Ереже бұзылса, студенттер шағым жасайды; әкімші вакансияны не компанияны тоқтатады.', 'При нарушениях студенты жалуются, администратор скрывает вакансию или приостанавливает компанию.')}</p>
      <label className="check"><input type="checkbox" checked={agree} onChange={e => setAgree(e.target.checked)} /><span className="small">{tt('Ережелерді оқыдым және сақтаймын', 'Я прочитал(а) правила и соблюдаю их')}</span></label>
      <button className="btn" type="button" disabled={!agree || busy} onClick={async () => {
        setBusy(true);
        try { await api('/api/employer/rules/accept', { method: 'POST', lang }); onAccept(); } catch (e) { toast(errText(e)); } finally { setBusy(false); }
      }}>{tt('Жалғастыру', 'Продолжить')}</button>
    </div>
  );
}

function ChipSet({ id, label, options, value, onChange, max }: { id: string; label: string; options: [string, string][]; value: string[]; onChange: (v: string[]) => void; max?: number }) {
  return (
    <div className="field" role="group" aria-labelledby={id}>
      <span className="lbl" id={id}>{label}</span>
      <div className="chips wrap">{options.map(([k, l]) => (
        <Chip key={k} pressed={value.includes(k)} onClick={() => onChange(value.includes(k) ? value.filter(x => x !== k) : max && value.length >= max ? value : [...value, k])}>{l}</Chip>
      ))}</div>
    </div>
  );
}

function JobEditor({ job }: { job?: EmployerJob }) {
  const { tt, tp, L, lang, toast, go } = useApp();
  const [f, setF] = useState<JobForm>(() => (job ? formOf(job) : emptyForm()));
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  const set = <K extends keyof JobForm>(k: K, v: JobForm[K]) => setF(x => ({ ...x, [k]: v }));
  const txt = (k: 'titleKk' | 'titleRu' | 'descKk' | 'descRu' | 'address' | 'schedKk' | 'schedRu' | 'applyKk' | 'applyRu') => (ev: { target: { value: string } }) => set(k, ev.target.value);
  const pay = Number(f.pay), hours = Number(f.hours);
  const problems = [
    len(f.titleKk) < 3 && tt('Атауы (қазақша) — кемінде 3 таңба', 'Название на казахском — от 3 символов'),
    len(f.descKk) < 20 && tt('Сипаттама (қазақша) — кемінде 20 таңба', 'Описание на казахском — от 20 символов'),
    !(pay > 0 && Number.isInteger(pay)) && tt('Төлем мөлшері — бүтін сан', 'Размер оплаты — целое число'),
    f.per === 'shift' && !(hours > 0 && hours <= 168) && tt('Ауысым ұзақтығы (сағат)', 'Длительность смены (часы)'),
    len(f.schedKk) < 3 && tt('Кесте (қазақша)', 'График на казахском'),
    !f.remote && len(f.address) < 3 && tt('Нақты мекенжай не «қашықтан»', 'Точный адрес или «удалённо»'),
    len(f.applyKk) < 3 && tt('«Қалай өтінім беріледі» (қазақша)', '«Как откликнуться» на казахском'),
    !f.minorOk && tt('«18-ге толмағандарға сай ма?» — жауап беріңіз', 'Ответьте на «Подходит ли несовершеннолетним?»'),
  ].filter(Boolean) as string[];
  const body = () => ({
    titleKk: f.titleKk.trim(), titleRu: f.titleRu.trim(), descKk: f.descKk.trim(), descRu: f.descRu.trim(), city: f.remote ? 'remote' : f.city, remote: f.remote,
    address: f.address.trim(), pay, per: f.per, hours: f.hours.trim() && hours > 0 ? hours : null, schedKk: f.schedKk.trim(), schedRu: f.schedRu.trim(),
    tags: f.tags, skills: f.skills, spec: f.spec.length ? f.spec : ['all'], req: f.req.map(r => ({ kk: r.kk.trim(), ru: r.ru.trim() })).filter(r => r.kk),
    applyKk: f.applyKk.trim(), applyRu: f.applyRu.trim(), minorOk: f.minorOk === 'yes',
  });
  const input = (k: 'titleKk' | 'titleRu' | 'schedKk' | 'schedRu' | 'address', label: string, max: number) => (
    <div className="field"><label htmlFor={'jf_' + k}>{label}</label><input className="input" id={'jf_' + k} maxLength={max} value={f[k]} onChange={txt(k)} /></div>
  );
  const area = (k: 'descKk' | 'descRu' | 'applyKk' | 'applyRu', label: string, max: number) => (
    <div className="field"><label htmlFor={'jf_' + k}>{label}</label><textarea className="input" id={'jf_' + k} maxLength={max} value={f[k]} onChange={txt(k)} /></div>
  );
  return (
    <form className="form" onSubmit={async e => {
      e.preventDefault();
      if (problems.length || busy) return;
      setBusy(true); setErr('');
      try {
        if (job) {
          await api(`/api/employer/jobs/${job.id}`, { method: 'PATCH', body: body(), lang });
          toast(tt('Вакансия сақталды', 'Вакансия сохранена'));
          go('employer:jobs/' + job.id, { replace: true });
        } else {
          const r = await api<{ job: EmployerJob; pending: boolean }>('/api/employer/jobs', { method: 'POST', body: { ...body(), acceptRules: true }, lang });
          toast(r.pending ? tt('Вакансия тексеруге жіберілді', 'Вакансия отправлена на проверку') : tt('Вакансия жарияланды', 'Вакансия опубликована'));
          go('employer', { replace: true });
        }
      } catch (e2) { setErr(errText(e2)); } finally { setBusy(false); }
    }}>
      {job ? null : <p className="small muted">{tt('Алғашқы 3 вакансияны әкімші тексереді, кейінгілері бірден жарияланады. Вакансия 30 күннен кейін өздігінен жабылады (ұзартуға болады).', 'Первые 3 вакансии проверяет администратор, следующие публикуются сразу. Вакансия закрывается сама через 30 дней (можно продлить).')}</p>}
      {input('titleKk', tt('Атауы (қазақша)', 'Название (на казахском)'), 120)}
      {input('titleRu', tt('Атауы (орысша, міндетті емес)', 'Название (на русском, необязательно)'), 120)}
      {area('descKk', tt('Не істеу керек (қазақша, 20–3000)', 'Что нужно делать (на казахском, 20–3000)'), 3000)}
      {area('descRu', tt('Не істеу керек (орысша)', 'Что нужно делать (на русском)'), 3000)}
      <div className="grid-2">
        <div className="field"><label htmlFor="jf_pay">{tt('Төлем, ₸', 'Оплата, ₸')}</label><input className="input num" id="jf_pay" inputMode="numeric" value={f.pay} onChange={e => set('pay', e.target.value.replace(/[^\d]/g, ''))} /></div>
        <div className="field"><label htmlFor="jf_per">{tt('Төлем түрі', 'Вид оплаты')}</label>
          <select className="input" id="jf_per" value={f.per} onChange={e => set('per', e.target.value as JobForm['per'])}>
            {(['shift', 'hour', 'month'] as const).map(k => <option key={k} value={k}>{L(PER_LABEL[k])}</option>)}
          </select></div>
      </div>
      <div className="field"><label htmlFor="jf_hours">{f.per === 'shift' ? tt('Ауысым ұзақтығы, сағат', 'Длительность смены, часов') : tt('Күніне сағат (міндетті емес)', 'Часов в день (необязательно)')}</label>
        <input className="input num" id="jf_hours" inputMode="decimal" value={f.hours} onChange={e => set('hours', e.target.value.replace(/[^\d.]/g, ''))} /></div>
      {input('schedKk', tt('Кесте (қазақша)', 'График (на казахском)'), 200)}
      {input('schedRu', tt('Кесте (орысша)', 'График (на русском)'), 200)}
      <label className="check"><input type="checkbox" checked={f.remote} onChange={e => set('remote', e.target.checked)} /><span className="small">{tt('Қашықтан жұмыс', 'Удалённая работа')}</span></label>
      {f.remote ? null : (
        <div className="field"><label htmlFor="jf_city">{tt('Қала', 'Город')}</label>
          <select className="input" id="jf_city" value={f.city} onChange={e => set('city', e.target.value)}>{HOME_CITIES.map(k => <option key={k} value={k}>{L(CITIES[k])}</option>)}</select></div>
      )}
      {input('address', f.remote ? tt('Мекенжай (міндетті емес)', 'Адрес (необязательно)') : tt('Нақты мекенжай', 'Точный адрес'), 200)}
      <ChipSet id="jfTags" label={tt('Белгілер', 'Метки')} options={Object.entries(JOB_TAGS).filter(([k]) => k !== 'remote').map(([k, l]) => [k, tp(l)])} value={f.tags} onChange={v => set('tags', v)} />
      <ChipSet id="jfSkills" label={tt('Керек дағдылар (10-ға дейін)', 'Нужные навыки (до 10)')} options={Object.entries(SKILLS).map(([k, l]) => [k, L(l)])} value={f.skills} onChange={v => set('skills', v)} max={10} />
      <ChipSet id="jfSpec" label={tt('Мамандықтар (бос болса — барлығы)', 'Специальности (пусто — все)')} options={Object.entries(SPECS).filter(([k]) => k !== 'all').map(([k, l]) => [k, L(l)])} value={f.spec} onChange={v => set('spec', v)} max={10} />
      <div className="field" role="group" aria-labelledby="jfReq">
        <span className="lbl" id="jfReq">{tt('Талаптар', 'Требования')}</span>
        {f.req.map((r, i) => (
          <div key={i} className="grid-2">
            <input className="input" aria-label={tt(`Талап ${i + 1} (қазақша)`, `Требование ${i + 1} (на казахском)`)} placeholder={tt('қазақша', 'на казахском')} maxLength={200} value={r.kk} onChange={e => set('req', f.req.map((x, k) => (k === i ? { ...x, kk: e.target.value } : x)))} />
            <input className="input" aria-label={tt(`Талап ${i + 1} (орысша)`, `Требование ${i + 1} (на русском)`)} placeholder={tt('орысша', 'на русском')} maxLength={200} value={r.ru} onChange={e => set('req', f.req.map((x, k) => (k === i ? { ...x, ru: e.target.value } : x)))} />
          </div>
        ))}
        {f.req.length < 10 ? <button className="link-btn" type="button" style={{ alignSelf: 'flex-start' }} onClick={() => set('req', [...f.req, { kk: '', ru: '' }])}>+ {tt('Талап қосу', 'Добавить требование')}</button> : null}
      </div>
      {area('applyKk', tt('Қалай өтінім беріледі (қазақша)', 'Как откликнуться (на казахском)'), 500)}
      {area('applyRu', tt('Қалай өтінім беріледі (орысша)', 'Как откликнуться (на русском)'), 500)}
      <fieldset className="consent-age">
        <legend>{tt('18-ге толмағандарға сай ма?', 'Подходит ли несовершеннолетним?')}</legend>
        <label className="check"><input type="radio" name="jfMinor" checked={f.minorOk === 'yes'} onChange={() => set('minorOk', 'yes')} /><span className="small">{tt('Иә, сай', 'Да, подходит')}</span></label>
        <label className="check"><input type="radio" name="jfMinor" checked={f.minorOk === 'no'} onChange={() => set('minorOk', 'no')} /><span className="small">{tt('Жоқ (түнгі ауысым, алкоголь сатылатын орын, ауыр жұмыс)', 'Нет (ночные смены, продажа алкоголя, тяжёлый труд)')}</span></label>
      </fieldset>
      <p className="tiny muted">{tt('Алдын ала ақы, депозит, «курс сатып алу» туралы талап жазылса, вакансия қабылданбайды.', 'Вакансия не примется, если в ней требуют предоплату, депозит или «покупку курса».')}</p>
      {problems.length ? <ul className="bullets tiny muted">{problems.map(p => <li key={p}>{p}</li>)}</ul> : null}
      <ErrorLine err={err} />
      <button className="btn block" type="submit" disabled={!!problems.length || busy}>{job ? tt('Сақтау', 'Сохранить') : tt('Жариялау', 'Опубликовать')}</button>
    </form>
  );
}

/* ---------- /employer/jobs/<id> ---------- */
function JobApplications({ id }: { id: number }) {
  const { tt, L, lang, toast, go, openSheet } = useApp();
  const jobs = useLoad<{ jobs: EmployerJob[] }>('/api/employer/jobs');
  const j = jobs.data?.jobs.find(x => x.id === id);
  const apps = useLoad<{ applications: EmployerApplication[] }>(j && j.status !== 'pending' && j.status !== 'rejected' ? `/api/employer/jobs/${id}/applications` : null);
  const [busy, setBusy] = useState(false);
  if (!jobs.data) return <ErrorOrLoading err={jobs.err} />;
  if (!j) return <NotFound />;
  const act = async (path: string, done: string) => {
    setBusy(true);
    try { await api(path, { method: 'POST', lang }); toast(done); jobs.reload(); } catch (e) { toast(errText(e)); } finally { setBusy(false); }
  };
  const canEdit = j.status === 'published' || j.status === 'closed' || j.status === 'expired';
  return (
    <>
      <div className="card">
        <div className="badges">
          <span className={`badge ${jobStatusTone(j.status)}`}>{L(JOB_STATUS[j.status])}</span>
          {!j.minorOk ? <span className="badge b-line">18+</span> : null}
        </div>
        <h2 style={{ overflowWrap: 'anywhere' }}>{L({ kk: j.titleKk, ru: j.titleRu })}</h2>
        <span className="small muted">{money(j.pay)} · {L(PER_LABEL[j.per]).toLowerCase()}</span>
        {j.status === 'pending' ? <p className="small">{tt('Әкімші тексеріп жатыр. Бекітілгенде студенттерге көрінеді.', 'Вакансию проверяет администратор. После одобрения её увидят студенты.')}</p> : null}
        {j.status === 'hidden' ? <p className="small">{j.hiddenReason === 'reports' ? tt('Шағымдар бойынша әкімші тексергенше жасырылды.', 'Скрыта по жалобам до проверки администратором.') : tt('Әкімші жасырды.', 'Скрыта администратором.')}</p> : null}
        {j.statusReason ? <p className="small"><b>{tt('Себебі', 'Причина')}:</b> <span style={{ overflowWrap: 'anywhere' }}>{j.statusReason}</span></p> : null}
        <dl className="kv">
          {j.publishedAt ? <><dt>{tt('Жарияланды', 'Опубликована')}</dt><dd>{ymd(j.publishedAt)}</dd></> : null}
          {j.expiresAt ? <><dt>{j.status === 'expired' ? tt('Мерзімі өтті', 'Срок истёк') : tt('Жабылады', 'Закроется')}</dt><dd>{ymd(j.expiresAt)}</dd></> : null}
        </dl>
        <div className="btn-row">
          {j.status === 'published' ? <button className="btn secondary" type="button" onClick={() => go('job:job-' + j.id)}>{tt('Студент көретін бет', 'Как видят студенты')}</button> : null}
          {canEdit ? <button className="btn ghost" type="button" onClick={() => go(`employer:jobs/${j.id}/edit`)}><Icon n="edit" s={18} /> {tt('Өзгерту', 'Изменить')}</button> : null}
          {canEdit ? <button className="btn ghost" type="button" disabled={busy} onClick={() => act(`/api/employer/jobs/${j.id}/extend`, tt('30 күнге ұзартылды', 'Продлено на 30 дней'))}>{tt('+30 күнге ұзарту', 'Продлить на 30 дней')}</button> : null}
          {j.status === 'published' ? <button className="btn danger" type="button" disabled={busy} onClick={() => openSheet(<ConfirmSheet msg={tt('Вакансияны жабасыз ба? Студенттер оны енді көрмейді.', 'Закрыть вакансию? Студенты её больше не увидят.')}
            yesLabel={tt('Иә, жабу', 'Да, закрыть')} yes={() => act(`/api/employer/jobs/${j.id}/close`, tt('Вакансия жабылды', 'Вакансия закрыта'))} />)}>{tt('Жабу', 'Закрыть')}</button> : null}
        </div>
      </div>
      {j.status === 'pending' || j.status === 'rejected' ? null : (
        <section className="section">
          <SectionHead title={tt('Өтінімдер', 'Отклики')} more={<span className="tiny muted num">{apps.data?.applications.length ?? ''}</span>} />
          <p className="tiny muted">{tt('Тек студент өзі таңдап жіберген деректер көрінеді. 14 күн жауапсыз қалған өтінім «жауап болмады» болады.', 'Видны только данные, которые студент сам выбрал. Отклик без ответа 14 дней получает статус «нет ответа».')}</p>
          {!apps.data ? <ErrorOrLoading err={apps.err} /> : apps.data.applications.length ? (
            <div className="list">{apps.data.applications.map(a => <ApplicationCard key={a.id} a={a} onDone={apps.reload} />)}</div>
          ) : <div className="group"><div className="empty">{tt('Әзірге өтінім жоқ', 'Откликов пока нет')}</div></div>}
        </section>
      )}
    </>
  );
}

function ApplicationCard({ a, onDone }: { a: EmployerApplication; onDone: () => void }) {
  const { tt, lang, toast, openSheet, closeSheet } = useApp();
  const [busy, setBusy] = useState(false);
  const status = async (s: 'viewed' | 'invited' | 'rejected', message = '') => {
    await api(`/api/employer/applications/${a.id}/status`, { method: 'POST', body: { status: s, message }, lang });
    onDone();
  };
  const open = a.status === 'sent' || a.status === 'viewed' || a.status === 'invited';
  const waiting = a.status === 'sent' || a.status === 'viewed';
  return (
    <div className="card">
      <div className="card-row" style={{ justifyContent: 'space-between', alignItems: 'center', flexWrap: 'wrap', gap: 8 }}>
        <AppStatusBadge s={a.status} />
        <span className="tiny muted">{ymd(a.createdAt)}{waiting ? ' · ' + tt(`${a.daysWaiting} күн күтіп тұр`, `ждёт ${a.daysWaiting} дн.`) : ''}</span>
      </div>
      <SharedList s={a.sharedFields} />
      {a.inviteMessage ? <p className="small muted" style={{ whiteSpace: 'pre-line', overflowWrap: 'anywhere' }}><b>{tt('Сіздің хабарламаңыз', 'Ваше сообщение')}:</b> {a.inviteMessage}</p> : null}
      {open ? (
        <div className="btn-row">
          {a.status === 'sent' ? <button className="btn ghost" type="button" disabled={busy} onClick={async () => {
            setBusy(true); try { await status('viewed'); toast(tt('Қаралды деп белгіленді', 'Отмечено как просмотренное')); } catch (e) { toast(errText(e)); } finally { setBusy(false); }
          }}>{tt('Қаралды', 'Просмотрено')}</button> : null}
          {a.status !== 'invited' ? <button className="btn" type="button" onClick={() => openSheet(<ReasonSheet title={tt('Студентті шақыру', 'Пригласить студента')}
            label={tt('Хабарлама: қашан, қайда келу, кімге хабарласу (міндетті емес)', 'Сообщение: когда и куда прийти, с кем связаться (необязательно)')} required={false} min={0} max={500}
            action={tt('Шақыру', 'Пригласить')} onSubmit={async m => { await status('invited', m); closeSheet(); toast(tt('Шақыру жіберілді', 'Приглашение отправлено')); }} />)}>{tt('Шақыру', 'Пригласить')}</button> : null}
          <button className="btn danger" type="button" onClick={() => openSheet(<ReasonSheet title={tt('Бас тарту', 'Отказать')}
            label={tt('Студентке хабарлама (міндетті емес)', 'Сообщение студенту (необязательно)')} required={false} min={0} max={500} danger
            action={tt('Бас тарту', 'Отказать')} onSubmit={async m => { await status('rejected', m); closeSheet(); toast(tt('Студентке жауап жіберілді', 'Ответ отправлен студенту')); }} />)}>{tt('Бас тарту', 'Отказать')}</button>
        </div>
      ) : null}
    </div>
  );
}

/* ---------- admin: /admin/employers ---------- */
export function AdminEmployers() {
  return <Feature name="b2b"><AdminOnly><Employers /></AdminOnly></Feature>;
}

function Employers() {
  const { tt, L, lang, toast, go, openSheet, closeSheet } = useApp();
  const [st, setSt] = useState('');
  const list = useLoad<{ employers: AdminEmployer[] }>(`/api/admin/employers?status=${st}`);
  const decide = (e: AdminEmployer, verb: 'suspend' | 'restore') => openSheet(
    <ReasonSheet title={verb === 'suspend' ? tt(`«${e.name}» тоқтату`, `Приостановить «${e.name}»`) : tt(`«${e.name}» қайта қосу`, `Восстановить «${e.name}»`)}
      label={tt('Себебі (міндетті, журналға жазылады)', 'Причина (обязательно, пишется в журнал)')} danger={verb === 'suspend'}
      action={verb === 'suspend' ? tt('Тоқтату', 'Приостановить') : tt('Қайта қосу', 'Восстановить')}
      onSubmit={async reason => {
        await api(`/api/admin/employers/${e.id}/${verb}`, { method: 'POST', body: { reason }, lang });
        closeSheet(); toast(verb === 'suspend' ? tt('Компания тоқтатылды, вакансиялары жасырылды', 'Компания приостановлена, вакансии скрыты') : tt('Компания қайта қосылды', 'Компания восстановлена'));
        list.reload();
      }} />,
  );
  const tabs: [string, string][] = [['', tt('Барлығы', 'Все')], ...(['pending', 'verified', 'suspended', 'rejected', 'closed'] as const).map((k): [string, string] => [k, L(EMPLOYER_STATUS[k])])];
  return (
    <>
      <div className="chips">{tabs.map(([k, l]) => <Chip key={k || 'all'} pressed={st === k} onClick={() => setSt(k)}>{l}</Chip>)}</div>
      <button className="btn ghost block" type="button" onClick={() => go('admin:queue')}>{tt('Тексеру кезегі (компаниялар, вакансиялар, шағымдар)', 'Очередь проверки (компании, вакансии, жалобы)')}</button>
      <p className="note">{tt('Тексеру: БСН-ді stat.gov.kz не egov.kz тізілімінен қолмен қарау, сайт не 2GIS беті ашылатынын тексеру, күмән болса телефон соғу. «Тексерілген» — жұмысқа кепілдік емес.', 'Проверка: вручную найти БИН в реестре stat.gov.kz или egov.kz, открыть сайт или 2GIS, при сомнениях позвонить. «Проверено» — не гарантия трудоустройства.')}</p>
      {!list.data ? <ErrorOrLoading err={list.err} /> : list.data.employers.length ? (
        <div className="list">{list.data.employers.map(e => (
          <div key={e.id} className="card">
            <div className="badges">
              <span className={`badge ${e.status === 'verified' ? 'b-ok' : e.status === 'pending' ? 'b-sky' : 'b-bad'}`}>{L(EMPLOYER_STATUS[e.status])}</span>
              {e.lowRate ? <span className="badge b-bad">{tt('Жауап < 30%', 'Ответов < 30%')}</span> : null}
            </div>
            <b style={{ overflowWrap: 'anywhere' }}>{e.name}</b>
            <dl className="kv payload">
              <div className="kv-row"><dt>{tt('БСН', 'БИН')}</dt><dd className="num">{e.bin}</dd></div>
              <div className="kv-row"><dt>{tt('Сайт', 'Сайт')}</dt><dd><a href={e.website} target="_blank" rel="noopener noreferrer">{e.website}</a></dd></div>
              <div className="kv-row"><dt>{tt('Байланыс', 'Контакт')}</dt><dd>{[e.contactName, e.contactPhone].filter(Boolean).join(', ')}</dd></div>
              <div className="kv-row"><dt>{tt('Мүше / вакансия', 'Участников / вакансий')}</dt><dd className="num">{e.members} / {e.jobs}</dd></div>
              <div className="kv-row"><dt>{tt('Жауап', 'Ответы')}</dt><dd>{e.response ? `${e.response.applications} · ${e.response.rate ?? '—'}% · ${e.response.days ?? '—'} ${tt('күн', 'дн.')}` : tt('5 өтінімнен аз', 'меньше 5 откликов')}</dd></div>
              <div className="kv-row"><dt>{tt('Тіркелді', 'Создана')}</dt><dd>{ymd(e.createdAt)}{e.verifiedAt ? ` · ${tt('тексерілді', 'проверена')} ${ymd(e.verifiedAt)}` : ''}</dd></div>
              {e.statusReason ? <div className="kv-row"><dt>{tt('Себебі', 'Причина')}</dt><dd>{e.statusReason}</dd></div> : null}
            </dl>
            {e.status === 'verified' ? <button className="btn danger" type="button" onClick={() => decide(e, 'suspend')}>{tt('Тоқтату', 'Приостановить')}</button> : null}
            {e.status === 'suspended' ? <button className="btn secondary" type="button" onClick={() => decide(e, 'restore')}>{tt('Қайта қосу', 'Восстановить')}</button> : null}
          </div>
        ))}</div>
      ) : <div className="group"><div className="empty">{tt('Компания жоқ', 'Компаний нет')}</div></div>}
    </>
  );
}

/* ---------- admin: /admin/staff ---------- */
interface UserRow { id: number; username: string | null; name: string | null; role: Role; university: string | null; consentAt: string | null }

export function AdminStaff() {
  return <Feature name="b2b"><AdminOnly><Staff /></AdminOnly></Feature>;
}

function Staff() {
  const { tt, lang, toast, openSheet } = useApp();
  const list = useLoad<{ staff: StaffRow[] }>('/api/admin/university-staff');
  const [q, setQ] = useState('');
  const [query, setQuery] = useState<string | null>(null);
  const users = useLoad<{ users: UserRow[] }>(query != null ? '/api/admin/users?q=' + encodeURIComponent(query) : null);
  const name = (x: { name: string | null; username: string | null }, id: number) => x.name || (x.username ? '@' + x.username : `#${id}`);
  return (
    <>
      <p className="note">{tt('Адам алдымен Telegram арқылы кіріп, келісім береді. Содан кейін оны осында университетке байлаңыз — тек рөл беру жеткіліксіз. Қызметкер тек жиынтық сандарды көреді.', 'Человек сначала входит через Telegram и даёт согласие. Затем привяжите его к университету здесь — одной роли недостаточно. Сотрудник видит только агрегированные числа.')}</p>
      <section className="section">
        <SectionHead title={tt('Байланған қызметкерлер', 'Привязанные сотрудники')} />
        {!list.data ? <ErrorOrLoading err={list.err} /> : list.data.staff.length ? (
          <div className="group">{list.data.staff.map(s => (
            <Row key={s.userId} icon="cap" chev={false} title={name(s, s.userId)} sub={[s.university, s.username ? '@' + s.username : '', roleLabel(s.role, tt), ymd(s.createdAt)].filter(Boolean).join(' · ')}
              end={<button className="link-btn" type="button" style={{ color: 'var(--danger)' }} onClick={() => openSheet(<ConfirmSheet msg={tt(`${name(s, s.userId)} университеттен ажыратылсын ба? Рөлі студентке қайтады.`, `Отвязать ${name(s, s.userId)}? Роль вернётся к «студент».`)}
                yesLabel={tt('Иә, ажырату', 'Да, отвязать')} yes={async () => { try { await api(`/api/admin/university-staff/${s.userId}`, { method: 'DELETE', lang }); toast(tt('Ажыратылды', 'Отвязан')); list.reload(); } catch (e) { toast(errText(e)); } }} />)}>{tt('Ажырату', 'Отвязать')}</button>} />
          ))}</div>
        ) : <div className="group"><div className="empty">{tt('Әзірге ешкім байланбаған', 'Пока никто не привязан')}</div></div>}
      </section>
      <section className="section">
        <SectionHead title={tt('Қызметкерді байлау', 'Привязать сотрудника')} />
        <form className="search" role="search" onSubmit={e => { e.preventDefault(); setQuery(q.trim()); }}>
          <Icon n="search" s={18} />
          <input className="input" type="search" aria-label={tt('Қолданушыны іздеу', 'Поиск пользователя')} placeholder={tt('Аты, @username не Telegram id', 'Имя, @username или Telegram id')} value={q} onChange={e => setQ(e.target.value)} />
        </form>
        {query == null ? null : !users.data ? <ErrorOrLoading err={users.err} /> : users.data.users.length ? (
          <div className="group">{users.data.users.map(u => (
            <Row key={u.id} icon="user" title={name(u, u.id)} sub={[u.username ? '@' + u.username : '', roleLabel(u.role, tt), u.university || '', u.consentAt ? '' : tt('келісімсіз', 'без согласия')].filter(Boolean).join(' · ')}
              onClick={() => openSheet(<BindSheet u={u} onDone={list.reload} />)} />
          ))}</div>
        ) : <div className="group"><div className="empty">{tt('Ешкім табылмады', 'Никого не найдено')}</div></div>}
      </section>
    </>
  );
}

function BindSheet({ u, onDone }: { u: UserRow; onDone: () => void }) {
  const { tt, lang, toast, closeSheet } = useApp();
  const [univ, setUniv] = useState(u.university || '');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  const ok = univ.trim().length >= 2;
  return (
    <form className="form" onSubmit={async e => {
      e.preventDefault(); if (!ok || busy) return;
      setBusy(true); setErr('');
      try {
        await api('/api/admin/university-staff', { method: 'POST', body: { userId: u.id, university: univ.trim() }, lang });
        closeSheet(); toast(tt('Байланды: рөл «Университет қызметкері»', 'Привязан: роль «Сотрудник университета»')); onDone();
      } catch (e2) { setErr(errText(e2)); } finally { setBusy(false); }
    }}>
      <h2 style={{ overflowWrap: 'anywhere' }}>{u.name || (u.username ? '@' + u.username : `#${u.id}`)}</h2>
      {!u.consentAt ? <p className="small urgent">{tt('Бұл адам әлі келісім бермеген.', 'Этот человек ещё не дал согласие.')}</p> : null}
      <div className="field"><label htmlFor="stUniv">{tt('Университет (профильдер мен іс-шаралардағы атаумен дәл)', 'Университет (точно как в профилях и мероприятиях)')}</label>
        <input className="input" id="stUniv" maxLength={120} value={univ} onChange={e => setUniv(e.target.value)} /></div>
      <ErrorLine err={err} />
      <div className="btn-row">
        <button className="btn ghost" type="button" onClick={closeSheet}>{tt('Бас тарту', 'Отмена')}</button>
        <button className="btn" type="submit" disabled={!ok || busy}>{tt('Байлау', 'Привязать')}</button>
      </div>
    </form>
  );
}
