'use client';
/* The super admin's console screens next to /admin: the audit log (/admin/audit), roles and rights
   (/admin/roles), CSV exports (/admin/exports) and University voice across universities (/admin/voice); and the
   moderator's workspace (/admin/moderation). Numbers are counts from the API; empty states say so plainly. */
import { useState, type ReactNode } from 'react';
import { useApp } from '@/components/app';
import { Row, SectionHead } from '@/components/ui';
import { AdminOnly, ErrorLine, errText, Loading, ModeratorOnly, ReasonSheet, useFeature, useLoad, useQueryParam, ymd } from '@/components/views/common';
import { actionLabel, AuditList, type AuditEntry } from '@/components/views/admin-users';
import { Stat } from '@/components/views/admin';
import { api, apiDownload, useAccount } from '@/lib/account';
import { query, who } from '@/lib/console';

/* ---------- audit log ---------- */

export function AdminAudit() { return <AdminOnly><Audit /></AdminOnly>; }

function Audit() {
  const { tt, lang, toast } = useApp();
  const target = useQueryParam('target') ?? '';
  const [f, setF] = useState({ action: '', actor: '', target, from: '', to: '' });
  const [before, setBefore] = useState<number[]>([]);
  const cur = before[before.length - 1];
  const r = useLoad<{ entries: AuditEntry[]; actions: { action: string; count: number }[]; next: number | null }>('/api/admin/audit' + query({ ...f, before: cur ?? '' }));
  const set = (k: keyof typeof f, v: string) => { setBefore([]); setF(x => ({ ...x, [k]: v })); };
  const idOk = (s: string) => s === '' || /^\d+$/.test(s);
  return (
    <div className="stack">
      <p className="small muted">{tt('Рөл, құқық, шектеу, өшіру, экспорт және модерация шешімдерінің бәрі осында: кім, кімге, қашан, қандай себеппен.',
        'Здесь все роли, права, ограничения, удаления, экспорты и решения модерации: кто, кому, когда и почему.')}</p>
      <div className="card adm-filters">
        <div className="field"><label htmlFor="af_action">{tt('Әрекет', 'Действие')}</label>
          <select className="input" id="af_action" value={f.action} onChange={e => set('action', e.target.value)}>
            <option value="">{tt('Барлығы', 'Все')}</option>
            {(r.data?.actions ?? []).map(a => <option key={a.action} value={a.action}>{actionLabel(a.action, tt)} ({a.count})</option>)}
            {f.action && !(r.data?.actions ?? []).some(a => a.action === f.action) ? <option value={f.action}>{actionLabel(f.action, tt)}</option> : null}
          </select></div>
        <div className="field"><label htmlFor="af_actor">{tt('Кім жасады (id)', 'Кто сделал (id)')}</label>
          <input className="input num" id="af_actor" inputMode="numeric" value={f.actor} onChange={e => idOk(e.target.value) && set('actor', e.target.value)} /></div>
        <div className="field"><label htmlFor="af_target">{tt('Кімге (id)', 'Кому (id)')}</label>
          <input className="input num" id="af_target" inputMode="numeric" value={f.target} onChange={e => idOk(e.target.value) && set('target', e.target.value)} /></div>
        <div className="field"><label htmlFor="af_from">{tt('Бастап', 'С')}</label>
          <input className="input" id="af_from" type="date" value={f.from} onChange={e => set('from', e.target.value)} /></div>
        <div className="field"><label htmlFor="af_to">{tt('Дейін', 'По')}</label>
          <input className="input" id="af_to" type="date" value={f.to} onChange={e => set('to', e.target.value)} /></div>
      </div>
      <div className="section-head">
        <span className="small muted">{before.length ? tt(`${before.length + 1}-бет`, `Стр. ${before.length + 1}`) : tt('Ең жаңасы', 'Самые новые')}</span>
        <button className="link-btn" type="button" onClick={() => apiDownload('/api/admin/export/audit.csv' + query(f), 'studenthub-audit.csv', lang)
          .catch(e => toast(errText(e, tt('Файлды жүктеу мүмкін болмады', 'Не удалось скачать файл'))))}>CSV</button>
      </div>
      <ErrorLine err={r.err} />
      {!r.data ? (r.err ? null : <Loading />) : <AuditList entries={r.data.entries} />}
      {before.length || r.data?.next ? (
        <nav className="adm-pager" aria-label={tt('Беттер', 'Страницы')}>
          <button className="btn ghost" type="button" disabled={!before.length} onClick={() => setBefore(b => b.slice(0, -1))}>{tt('Жаңалары', 'Новее')}</button>
          <button className="btn ghost" type="button" disabled={!r.data?.next} onClick={() => r.data?.next && setBefore(b => [...b, r.data!.next!])}>{tt('Ескілері', 'Старее')}</button>
        </nav>
      ) : null}
    </div>
  );
}

/* ---------- roles and rights ---------- */

interface Person { userId: number; name: string | null; username: string | null }
interface Grants {
  scopes: string[];
  grants: (Person & { university: string | null; role: string; scope: string; reason: string; createdAt: string; grantedBy: { id: number; name: string | null } | null })[];
  admins: Person[];
  cafes: (Person & { shopId: number; nameKk: string; nameRu: string; role: string })[];
  printShops: (Person & { shopId: number; nameKk: string; nameRu: string; role: string })[];
  clubs: (Person & { clubId: number; nameKk: string; nameRu: string | null })[];
  universityStaff: (Person & { university: string })[];
  feedbackStaff: (Person & { recipientId: number; university: string; nameKk: string; nameRu: string })[];
}

export function AdminRoles() { return <AdminOnly><Roles /></AdminOnly>; }

function Roles() {
  const { tt, lang, go, openSheet, closeSheet, toast } = useApp();
  const cafe = useFeature('cafe'), print = useFeature('print'), clubs = useFeature('clubs'), b2b = useFeature('b2b'), feedback = useFeature('feedback');
  const r = useLoad<Grants>('/api/admin/grants');
  const [uid, setUid] = useState('');
  if (!r.data) return <><ErrorLine err={r.err} />{r.err ? null : <Loading />}</>;
  const g = r.data;
  const nm = (o: { nameKk: string; nameRu: string | null }) => (lang !== 'kk' && o.nameRu ? o.nameRu : o.nameKk);
  const person = (p: Person, extra = '') => (
    <Row key={`${p.userId}-${extra}`} icon="user" title={who(p)} sub={extra} onClick={() => go('admin:users?id=' + p.userId)} />
  );
  const revoke = (userId: number, name: string) => openSheet(
    <ReasonSheet title={tt(`${name}: модератор құқығын алу`, `${name}: снять права модератора`)} label={tt('Себебі (тарихта сақталады)', 'Причина (сохранится в истории)')} action={tt('Алу', 'Снять')} danger
      onSubmit={async reason => { await api('/api/admin/grants/revoke', { method: 'POST', body: { userId, scope: 'moderator', reason }, lang }); closeSheet(); toast(tt('Құқық алынды', 'Права сняты')); r.reload(); }} />,
  );
  const grant = () => openSheet(
    <ReasonSheet title={tt(`#${uid}: модератор тағайындау`, `#${uid}: назначить модератором`)} label={tt('Себебі (тарихта сақталады)', 'Причина (сохранится в истории)')} action={tt('Тағайындау', 'Назначить')}
      onSubmit={async reason => { await api('/api/admin/grants', { method: 'POST', body: { userId: Number(uid), scope: 'moderator', reason }, lang }); closeSheet(); setUid(''); toast(tt('Модератор тағайындалды', 'Назначен модератор')); r.reload(); }} />,
  );
  const section = (title: string, manage: string | null, rows: ReactNode[], empty: string) => (
    <section className="section" key={title}>
      <SectionHead title={title} more={manage ? <button className="link-btn" type="button" onClick={() => go(manage)}>{tt('Басқару', 'Управлять')}</button> : undefined} />
      <div className="group">{rows.length ? rows : <div className="empty">{empty}</div>}</div>
    </section>
  );
  return (
    <div className="stack">
      <p className="small muted">{tt('Кімнің қандай құқығы бар — бір жерде. Әр беру мен алу себебімен тарихта сақталады.', 'Кто чем управляет — в одном месте. Каждая выдача и снятие сохраняется в истории с причиной.')}</p>
      <section className="section">
        <SectionHead title={tt('Модераторлар', 'Модераторы')} />
        <p className="tiny muted">{tt('Модератор кезекті, шағымдарды, тегін заттарды, ағын мен контентті қарайды. Қолданушылар, экспорт, ақша және рөл беретін өтінімдер оған жабық.',
          'Модератор разбирает очередь, жалобы, «Отдам даром», ленту и контент. Пользователи, экспорт, деньги и заявки, выдающие роли, ему закрыты.')}</p>
        <div className="group">
          {g.grants.length ? g.grants.map(x => (
            <Row key={x.userId + x.scope} icon="shield" title={who(x)} onClick={() => revoke(x.userId, who(x))}
              sub={`${ymd(x.createdAt)} · ${x.grantedBy ? who(x.grantedBy) : '—'} · «${x.reason}»`} end={<span className="badge b-line">{tt('алу', 'снять')}</span>} />
          )) : <div className="empty">{tt('Модератор әлі жоқ', 'Модераторов пока нет')}</div>}
        </div>
        <form className="pts-inline" onSubmit={e => { e.preventDefault(); if (/^\d+$/.test(uid)) grant(); }}>
          <input className="input num" inputMode="numeric" aria-label={tt('Пайдаланушы id', 'id пользователя')} placeholder="user id" value={uid} onChange={e => setUid(e.target.value.trim())} />
          <button className="btn secondary" type="submit" disabled={!/^\d+$/.test(uid)}>{tt('Модератор ету', 'Сделать модератором')}</button>
        </form>
        <p className="tiny muted">{tt('id-ді «Қолданушылар» бетінен табыңыз.', 'id можно найти на странице «Пользователи».')}</p>
      </section>
      {section(tt('Әкімшілер', 'Администраторы'), 'admin:users', g.admins.map(p => person(p, tt('әкімші', 'администратор'))), tt('Әкімші жоқ', 'Администраторов нет'))}
      {cafe ? section(tt('Кофехана қызметкерлері', 'Сотрудники кофеен'), 'admin:cafe', g.cafes.map(p => person(p, `${nm(p)} · ${p.role}`)), tt('Қызметкер жоқ', 'Сотрудников нет')) : null}
      {print ? section(tt('Полиграфия қызметкерлері', 'Сотрудники полиграфий'), 'admin:print', g.printShops.map(p => person(p, `${nm(p)} · ${p.role}`)), tt('Қызметкер жоқ', 'Сотрудников нет')) : null}
      {clubs ? section(tt('Клуб әкімшілері', 'Админы клубов'), 'admin:clubs', g.clubs.map(p => person(p, nm(p))), tt('Клуб әкімшісі жоқ', 'Админов клубов нет')) : null}
      {b2b ? section(tt('Университет қызметкерлері', 'Сотрудники университетов'), 'admin:staff', g.universityStaff.map(p => person(p, p.university)), tt('Қызметкер жоқ', 'Сотрудников нет')) : null}
      {feedback ? section(tt('Өтініш алушылар', 'Получатели обращений'), 'voice', g.feedbackStaff.map(p => person(p, `${p.university} · ${lang !== 'kk' ? p.nameRu : p.nameKk}`)), tt('Ешкім тағайындалмаған', 'Никто не назначен')) : null}
    </div>
  );
}

/* ---------- exports ---------- */

export function AdminExports() { return <AdminOnly><Exports /></AdminOnly>; }

function Exports() {
  const { tt, lang } = useApp();
  const cafe = useFeature('cafe'), print = useFeature('print'), feedback = useFeature('feedback'), surveys = useFeature('surveys');
  const [from, setFrom] = useState('');
  const [to, setTo] = useState('');
  const [busy, setBusy] = useState('');
  const [err, setErr] = useState('');
  const kinds: [string, string, string, boolean][] = ([
    ['users', tt('Қолданушылар', 'Пользователи'), tt('Аты, байланыс, рөл, университет, белсенділік. Сүзгілер — «Қолданушылар» бетінде.', 'Имя, контакты, роль, вуз, активность. Фильтры — на странице «Пользователи».'), false],
    ['audit', tt('Өзгерістер тарихы', 'История изменений'), tt('Кезең бойынша барлық жазба.', 'Все записи за период.'), true],
    ...(cafe ? [['cafe-orders', tt('Кофе тапсырыстары', 'Заказы кофе'), tt('Сома, төлем, күй; студенттің тек id-і.', 'Сумма, оплата, статус; от студента только id.'), true]] : []),
    ...(print ? [['print-jobs', tt('Басып шығару тапсырыстары', 'Заказы печати'), tt('Сома, төлем, күй; файлдарсыз.', 'Сумма, оплата, статус; без файлов.'), true]] : []),
    ...(feedback ? [['feedback', tt('Өтініштер', 'Обращения'), tt('Тек метадеректер: мәтінсіз, авторсыз.', 'Только метаданные: без текста и автора.'), true]] : []),
    ...(surveys ? [['surveys', tt('Сауалнамалар', 'Опросы'), tt('Тізім мен қатысушылар саны. Жауаптар — әр сауалнаманың өз CSV-інде (5-тен аз топ жасырын).', 'Список и число участников. Ответы — в CSV каждого опроса (группы меньше 5 скрыты).'), true]] : []),
  ] as [string, string, string, boolean][]);
  const run = async (k: string, period: boolean) => {
    setBusy(k); setErr('');
    try { await apiDownload(`/api/admin/export/${k}.csv` + (period ? query({ from, to }) : ''), `studenthub-${k}.csv`, lang); }
    catch (e) { setErr(errText(e, tt('Файлды жүктеу мүмкін болмады', 'Не удалось скачать файл'))); } finally { setBusy(''); }
  };
  return (
    <div className="stack">
      <p className="small muted">{tt('Файлдар Excel-де бірден ашылады (UTF-8). Әр жүктеу тарихта сақталады. Жеке деректерді тек қажет жерде қолданыңыз.',
        'Файлы сразу открываются в Excel (UTF-8). Каждая выгрузка сохраняется в истории. Используйте личные данные только там, где это нужно.')}</p>
      <div className="card adm-filters">
        <div className="field"><label htmlFor="ex_from">{tt('Кезең басы', 'Начало периода')}</label>
          <input className="input" id="ex_from" type="date" value={from} onChange={e => setFrom(e.target.value)} /></div>
        <div className="field"><label htmlFor="ex_to">{tt('Кезең соңы', 'Конец периода')}</label>
          <input className="input" id="ex_to" type="date" value={to} onChange={e => setTo(e.target.value)} /></div>
      </div>
      <ErrorLine err={err} />
      <div className="group">
        {kinds.map(([k, title, sub, period]) => (
          <Row key={k} icon="doc" title={title} sub={sub} onClick={() => { if (!busy) void run(k, period); }}
            end={<span className="badge b-sky">{busy === k ? tt('жүктелуде…', 'загрузка…') : 'CSV'}</span>} />
        ))}
      </div>
    </div>
  );
}

/* ---------- University voice across universities ---------- */

interface VoiceOverview {
  universities: { university: string; surveys: { draft: number; published: number; closed: number; respondents: number };
    feedback: { held: number; open: number; overdue: number; answered: number; recipients: number; unstaffed: number }; wellbeing: number; staff: number }[];
  surveys: { id: number; university: string; title: string; status: string; anonymous: boolean; respondents: number; createdAt: string; closesAt: string | null }[];
}

export function AdminVoice() { return <AdminOnly><Voice /></AdminOnly>; }

function Voice() {
  const { tt, lang, go, toast } = useApp();
  const r = useLoad<VoiceOverview>('/api/admin/voice');
  if (!r.data) return <><ErrorLine err={r.err} />{r.err ? null : <Loading />}</>;
  const v = r.data;
  const status = (s: string) => s === 'published' ? tt('ашық', 'открыт') : s === 'closed' ? tt('жабық', 'закрыт') : tt('жоба', 'черновик');
  return (
    <div className="stack">
      <p className="small muted">{tt('Тек сандар: өтініш мәтіндері мен сауалнама жауаптары «Студент дауысы» бетінде, 5-тен аз топ ешқашан көрсетілмейді.',
        'Только числа: тексты обращений и ответы опросов — на странице «Голос студентов», группы меньше 5 никогда не показываются.')}</p>
      <Row go="voice" icon="chat" title={tt('Студент дауысы: басқару', 'Голос студентов: управление')} sub={tt('Сауалнама жасау, өтініштерге жауап, алушылар мен байланыстар', 'Создать опрос, ответить на обращения, получатели и контакты')} />
      {v.universities.length ? v.universities.map(u => (
        <article key={u.university} className="card">
          <h3 style={{ margin: 0 }}>{u.university}</h3>
          <div className="stat-grid">
            <Stat value={u.surveys.published} label={tt(`ашық сауалнама (${u.surveys.draft} жоба, ${u.surveys.closed} жабық)`, `открытых опросов (${u.surveys.draft} черн., ${u.surveys.closed} закр.)`)} />
            <Stat value={u.surveys.respondents} label={tt('сауалнамаға қатысқан', 'ответов на опросы')} />
            <Stat value={u.feedback.open} label={tt('ашық өтініш', 'открытых обращений')} />
            <Stat value={u.feedback.overdue} label={tt('мерзімі өткен', 'просрочено')} tone={u.feedback.overdue ? 'warn' : undefined} />
            <Stat value={u.feedback.held} label={tt('белгіленген (әкімшіге)', 'помечено (админу)')} tone={u.feedback.held ? 'warn' : undefined} />
            <Stat value={u.wellbeing} label={tt('психолог байланыстары', 'контакты психологов')} />
          </div>
          <p className="tiny muted">{tt(`${u.feedback.recipients} алушы, оның ${u.feedback.unstaffed} қызметкерсіз · ${u.staff} университет қызметкері · ${u.feedback.answered} жауап берілген`,
            `${u.feedback.recipients} получателей, без сотрудника ${u.feedback.unstaffed} · ${u.staff} сотрудников вуза · ${u.feedback.answered} с ответом`)}</p>
        </article>
      )) : <div className="group"><div className="empty">{tt('Әзірге бірде-бір университет бапталмаған', 'Пока ни один университет не настроен')}</div></div>}
      <section className="section">
        <SectionHead title={tt('Соңғы сауалнамалар', 'Последние опросы')} />
        <div className="group">
          {v.surveys.length ? v.surveys.map(s => (
            <Row key={s.id} icon="doc" title={s.title} chev={false}
              sub={`${s.university} · ${status(s.status)} · ${tt(`${s.respondents} қатысушы`, `${s.respondents} участн.`)} · ${ymd(s.createdAt)}`}
              end={s.respondents ? <button className="link-btn" type="button" onClick={() => apiDownload(`/api/staff/surveys/${s.id}/results.csv`, `survey-${s.id}.csv`, lang)
                .catch(e => toast(errText(e)))}>CSV</button> : undefined} />
          )) : <div className="empty">{tt('Сауалнама әлі жоқ', 'Опросов пока нет')}</div>}
        </div>
        <button className="btn ghost" type="button" onClick={() => go('admin:exports')}>{tt('Барлық экспорт', 'Все экспорты')}</button>
      </section>
    </div>
  );
}

/* ---------- the moderator's workspace ---------- */

export function AdminModeration() { return <ModeratorOnly><Moderation /></ModeratorOnly>; }

function Moderation() {
  const { tt } = useApp();
  const admin = useAccount().user?.role === 'admin';
  const market = useFeature('market'), feeds = useFeature('feeds');
  const q = useLoad<{ pendingByKind: Record<string, number> }>('/api/admin/queue?status=pending&limit=1');
  const rep = useLoad<{ reports: unknown[] }>('/api/admin/reports');
  const pending = Object.values(q.data?.pendingByKind ?? {}).reduce((a, b) => a + b, 0);
  return (
    <div className="stack">
      <p className="small muted">{admin
        ? tt('Модератордың жұмыс орны осылай көрінеді. Сіз әкімші ретінде рөл беретін өтінімдерді де көресіз.', 'Так выглядит рабочее место модератора. Как администратор вы видите и заявки, выдающие роли.')
        : tt('Кезек, шағымдар және контент. Рөл беретін өтінімдер (амбассадор, ментор, компания, клуб жетекшісі) — әкімшіде.', 'Очередь, жалобы и контент. Заявки, выдающие роли (амбассадор, ментор, компания, руководитель клуба), — у администратора.')}</p>
      <ErrorLine err={q.err || rep.err} />
      <div className="stat-grid">
        <Stat value={q.data ? pending : '…'} label={tt('Кезекте күтуде', 'Ждут в очереди')} tone={pending ? 'warn' : undefined} />
        <Stat value={rep.data ? rep.data.reports.length : '…'} label={tt('Шағым жасалған жазба', 'Записей с жалобами')} />
      </div>
      <div className="group">
        <Row go="admin:queue" icon="shield" title={tt('Тексеруді күтетіндер', 'Ждут проверки')} sub={tt('Ескісі бірінші; қайтарғанда себеп міндетті', 'Сначала старые; при отказе причина обязательна')} end={pending ? <b className="num">{pending}</b> : undefined} />
        <Row go="admin:reports" icon="bell" title={tt('Шағымдар', 'Жалобы')} sub={tt('Негізсіз шағымды жабу не жазбаны жасыру', 'Закрыть необоснованную или скрыть запись')} />
        <Row go="admin:content" icon="file" title={tt('Жазбалар', 'Записи')} sub={tt('Мүмкіндіктер, жұмыстар, жеңілдіктер', 'Возможности, работа, скидки')} />
        {market ? <Row go="admin:market" icon="gift" title={tt('Тегін заттар', 'Отдам даром')} sub={tt('Жасыру, қайтару, жариялауды шектеу', 'Скрыть, вернуть, ограничить публикации')} /> : null}
        {feeds ? <Row go="admin:feeds" icon="spark" title={tt('Мүмкіндіктер ағыны', 'Лента возможностей')} sub={tt('ЖИ тапқан жазбаларды тексеру', 'Проверка записей, найденных ИИ')} /> : null}
      </div>
    </div>
  );
}
