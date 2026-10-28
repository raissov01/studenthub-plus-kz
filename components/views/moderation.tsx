'use client';
/* eslint-disable @next/next/no-img-element -- market photos come from our API (some only with the session); next/image would proxy them */
/* Admin moderation (prompt 06 and the kinds 07/08 add): /admin/queue (one queue for every kind, oldest first,
   payload view, opportunity checklist, approve / reject with a required reason), /admin/reports (counted reports
   and «негізсіз»), /admin/ambassadors and /admin/market. All admin-only, like /admin. */
import { useState } from 'react';
import { useApp } from '@/components/app';
import { Chip, Icon, Row, Seg, SectionHead } from '@/components/ui';
import { AdminOnly, ErrorLine, errText, Feature, Loading, ReasonSheet, useFeature, useLoad, ymd } from '@/components/views/common';
import { api } from '@/lib/account';
import { MARKET_CATS, MARKET_CONDITIONS, type AdminMarketItem } from '@/lib/market';

interface QueueItem {
  id: number; kind: string; payload: Record<string, unknown>; submitted_by: number | null; university: string | null;
  status: 'pending' | 'approved' | 'rejected'; reason: string | null; created_at: string; reviewed_at: string | null; reviewed_by: number | null;
  age_hours: number; author: { id: number; name: string | null; username: string | null; role: string; university: string | null } | null;
  checklist?: { hasOfficialSource: boolean; noFee: boolean; deadlineOk: boolean };
}

export function kindLabel(k: string, tt: (kk: string, ru: string) => string): string {
  const m: Record<string, [string, string]> = {
    opportunity: ['Мүмкіндік', 'Возможность'], place: ['Пайдалы орын', 'Полезное место'],
    ambassador_application: ['Амбассадор өтінімі', 'Заявка амбассадора'], ambassador_renewal: ['Амбассадор мерзімін ұзарту', 'Продление амбассадора'],
    mentor_application: ['Ментор өтінімі', 'Заявка ментора'], mentor_report: ['Ментор жұбына шағым', 'Жалоба на пару ментора'],
    market_item: ['Тегін зат (жаңа аккаунт)', 'Вещь (новый аккаунт)'], market_report: ['Тегін затқа шағым', 'Жалоба на вещь'],
    employer_job: ['Жұмыс берушінің вакансиясы', 'Вакансия работодателя'],
  };
  return m[k] ? tt(m[k][0], m[k][1]) : k;
}

export function AdminQueue() {
  return <AdminOnly><Queue /></AdminOnly>;
}

function Queue() {
  const { tt } = useApp();
  const [kind, setKind] = useState('');
  const [status, setStatus] = useState('pending');
  const [after, setAfter] = useState<number[]>([]);
  const cur = after[after.length - 1];
  const q = useLoad<{ items: QueueItem[]; pendingByKind: Record<string, number> }>(`/api/admin/queue?status=${status}&limit=30${kind ? '&kind=' + kind : ''}${cur ? '&after=' + cur : ''}`);
  const kinds = Object.keys(q.data?.pendingByKind ?? {});
  if (kind && !kinds.includes(kind)) kinds.push(kind);
  const pick = (f: () => void) => { setAfter([]); f(); };
  return (
    <>
      <Seg items={[['pending', tt('Күтуде', 'Ожидают')], ['approved', tt('Бекітілген', 'Одобрено')], ['rejected', tt('Қайтарылған', 'Отклонено')]]} cur={status} onPick={v => pick(() => setStatus(v))} />
      <div className="chips wrap">
        <Chip pressed={!kind} onClick={() => pick(() => setKind(''))}>{tt('Барлығы', 'Все')}</Chip>
        {kinds.map(k => <Chip key={k} pressed={kind === k} onClick={() => pick(() => setKind(k))}>{kindLabel(k, tt)}{q.data?.pendingByKind[k] ? ` · ${q.data.pendingByKind[k]}` : ''}</Chip>)}
      </div>
      <p className="tiny muted">{status === 'pending' ? tt('Ескісі бірінші.', 'Сначала старые.') : tt('Жаңасы бірінші.', 'Сначала новые.')}</p>
      <ErrorLine err={q.err} />
      {!q.data ? <Loading /> : q.data.items.length ? (
        <div className="list">{q.data.items.map(it => <QueueCard key={it.id} it={it} onDone={q.reload} />)}</div>
      ) : <div className="group"><div className="empty">{tt('Кезек бос', 'Очередь пуста')}</div></div>}
      <div className="btn-row">
        {after.length ? <button className="btn ghost" type="button" onClick={() => setAfter(a => a.slice(0, -1))}>{tt('Алдыңғы бет', 'Предыдущая')}</button> : null}
        {q.data && q.data.items.length >= 30 ? <button className="btn ghost" type="button" onClick={() => setAfter(a => [...a, q.data!.items[q.data!.items.length - 1].id])}>{tt('Келесі бет', 'Следующая')}</button> : null}
      </div>
    </>
  );
}

const show = (v: unknown): string => {
  if (v == null || v === '') return '—';
  if (Array.isArray(v)) return v.map(show).join(', ');
  if (typeof v === 'boolean') return v ? '+' : '—';
  if (typeof v === 'object') return JSON.stringify(v);
  return String(v);
};

function Payload({ it }: { it: QueueItem }) {
  const { tt } = useApp();
  const p = it.payload || {};
  const photos = it.kind === 'market_item' && Array.isArray(p.photos) ? (p.photos as string[]) : [];
  const entries = Object.entries(p).filter(([k]) => k !== 'photos' || !photos.length);
  return (
    <>
      {photos.length ? <div className="mk-thumbs">{photos.map(n => <span key={n} className="mk-thumb"><img src={'/api/market/photos/' + n} alt="" /></span>)}</div> : null}
      <dl className="kv payload">
        {entries.map(([k, v]) => (
          <div key={k} className="kv-row"><dt>{k}</dt><dd>{typeof v === 'string' && /^https:\/\//.test(v) ? <a href={v} target="_blank" rel="noopener noreferrer">{v}</a> : show(v)}</dd></div>
        ))}
      </dl>
      {it.kind === 'market_item' && typeof p.itemId === 'number' ? <a className="tiny" href={`/market/${p.itemId}`} target="_blank" rel="noopener">{tt('Затты ашу', 'Открыть вещь')}</a> : null}
    </>
  );
}

function QueueCard({ it, onDone }: { it: QueueItem; onDone: () => void }) {
  const { tt, lang, toast, openSheet, closeSheet } = useApp();
  const a = it.author;
  const decide = (verb: 'approve' | 'reject') => openSheet(
    <ReasonSheet
      title={verb === 'approve' ? tt('Бекіту', 'Одобрить') : tt('Қайтару', 'Отклонить')}
      label={verb === 'approve' ? tt('Ескертпе авторға (міндетті емес)', 'Комментарий автору (необязательно)') : tt('Себебі (міндетті, авторға жіберіледі)', 'Причина (обязательно, уйдёт автору)')}
      required={verb === 'reject'} min={verb === 'approve' ? 0 : 3}
      action={verb === 'approve' ? tt('Бекіту', 'Одобрить') : tt('Қайтару', 'Отклонить')} danger={verb === 'reject'}
      onSubmit={async reason => {
        const r = await api<{ notified: boolean }>(`/api/admin/queue/${it.id}/${verb}`, { method: 'POST', body: reason ? { reason } : {}, lang });
        closeSheet();
        toast((verb === 'approve' ? tt('Бекітілді', 'Одобрено') : tt('Қайтарылды', 'Отклонено')) + (r.notified ? tt('. Авторға хабар кетті', '. Автор уведомлён') : ''));
        onDone();
      }} />,
  );
  const ck = it.checklist;
  return (
    <div className="card">
      <div className="badges"><span className="badge b-sky">{kindLabel(it.kind, tt)}</span>
        {it.university ? <span className="badge b-line">{it.university}</span> : null}
        <span className="badge b-line num">{it.age_hours < 48 ? tt(`${Math.round(it.age_hours)} сағ бұрын`, `${Math.round(it.age_hours)} ч назад`) : tt(`${Math.round(it.age_hours / 24)} күн бұрын`, `${Math.round(it.age_hours / 24)} дн. назад`)}</span></div>
      <span className="small">{a ? `${a.name || ''}${a.username ? ' @' + a.username : ''} · ${a.role}${a.university ? ' · ' + a.university : ''}` : tt('Автор аккаунтын өшірген', 'Автор удалил аккаунт')}</span>
      <Payload it={it} />
      {ck ? (
        <div className="checklist">
          <span className="eyebrow">{tt('Бекітер алдында тексеріңіз', 'Перед одобрением проверьте')}</span>
          {([[ck.hasOfficialSource, tt('Ресми дереккөз сілтемесі бар', 'Есть ссылка на официальный источник')], [ck.noFee, tt('Ақы сұрамайды', 'Не требует оплаты')], [ck.deadlineOk, tt('Мерзімі дұрыс', 'Срок корректный')]] as [boolean, string][])
            .map(([ok, l]) => <span key={l} className={`small elig ${ok ? '' : 'urgent'}`}><Icon n={ok ? 'check' : 'x'} s={16} /> {l}</span>)}
        </div>
      ) : null}
      {it.status === 'pending' ? (
        <div className="btn-row">
          <button className="btn danger" type="button" onClick={() => decide('reject')}>{tt('Қайтару', 'Отклонить')}</button>
          <button className="btn" type="button" onClick={() => decide('approve')}>{tt('Бекіту', 'Одобрить')}</button>
        </div>
      ) : <span className="small muted">{it.status === 'approved' ? tt('Бекітілді', 'Одобрено') : tt('Қайтарылды', 'Отклонено')} · {ymd(it.reviewed_at)}{it.reason ? ' · ' + it.reason : ''}</span>}
    </div>
  );
}

/* ---------- reports ---------- */
interface ReportRow { kind: string; id: string; total: number; counted: number; first_at: string; reasons: string[]; hide_after: number; hidden: boolean }

export function AdminReports() {
  return <AdminOnly><Reports /></AdminOnly>;
}
function Reports() {
  const { tt, lang, toast, openSheet, closeSheet, go } = useApp();
  const marketOn = useFeature('market');
  const r = useLoad<{ reports: ReportRow[] }>('/api/admin/reports');
  return (
    <>
      <p className="note">{tt('Тек әртүрлі, кемінде 7 күндік аккаунттардың шағымы саналады. Шекке жетсе жазба жасырылады; «Негізсіз» басылса — қайта көрінеді.', 'Считаются только жалобы разных аккаунтов старше 7 дней. При пороге запись скрывается; «Необоснованно» — снова видна.')}</p>
      <ErrorLine err={r.err} />
      {!r.data ? <Loading /> : (
        <div className="list">{r.data.reports.length ? r.data.reports.map(x => (
          <div key={x.kind + x.id} className="card">
            <div className="badges"><span className="badge b-sky">{kindLabel(x.kind, tt)}</span><span className="badge b-line">{x.id}</span>
              {x.hidden ? <span className="badge b-bad">{tt('Жасырылған', 'Скрыто')}</span> : null}</div>
            <span className="small num">{tt(`Шағым: ${x.total}, саналған: ${x.counted} / ${x.hide_after}`, `Жалоб: ${x.total}, засчитано: ${x.counted} из ${x.hide_after}`)} · {ymd(x.first_at)}</span>
            <ul className="bullets small">{x.reasons.map((s, i) => <li key={i} style={{ overflowWrap: 'anywhere' }}>{s}</li>)}</ul>
            <button className="btn ghost" type="button" onClick={() => openSheet(<ReasonSheet title={tt('Шағымдар негізсіз', 'Жалобы необоснованны')} label={tt('Ескертпе (міндетті емес)', 'Комментарий (необязательно)')} required={false} min={0}
              action={tt('Негізсіз', 'Необоснованно')} onSubmit={async reason => {
                await api('/api/admin/reports/dismiss', { method: 'POST', body: { kind: x.kind, id: x.id, reason }, lang });
                closeSheet(); toast(tt('Шағымдар алынып тасталды', 'Жалобы сняты')); r.reload();
              }} />)}>{tt('Негізсіз', 'Необоснованно')}</button>
          </div>
        )) : <div className="group"><div className="empty">{tt('Шағым жоқ', 'Жалоб нет')}</div></div>}</div>
      )}
      {marketOn ? <button className="btn secondary block" type="button" onClick={() => go('admin:market')}>{tt('Тегін заттарға шағымдар', 'Жалобы на вещи «Отдам даром»')}</button> : null}
      <button className="btn ghost block" type="button" onClick={() => go('admin:queue')}>{tt('Модерация кезегі', 'Очередь модерации')}</button>
    </>
  );
}

/* ---------- ambassadors ---------- */
interface AdminTerm {
  id: number; user_id: number; university: string; role_in_team: 'lead' | 'member'; term_start: string; term_end: string; hours_per_week: number;
  ref_code: string; status: string; paused_until: string | null; cert_code: string | null; name: string | null; username: string | null; joined: number;
}

export function AdminAmbassadors() {
  return <Feature name="ambassador"><AdminOnly><Ambassadors /></AdminOnly></Feature>;
}
function Ambassadors() {
  const { tt, lang, toast, openSheet, closeSheet, go } = useApp();
  const [st, setSt] = useState('active');
  const list = useLoad<{ terms: AdminTerm[] }>(`/api/admin/ambassadors?status=${st}`);
  const run = async (fn: () => Promise<unknown>, done: string) => { try { await fn(); toast(done); list.reload(); } catch (e) { toast(errText(e)); } };
  return (
    <>
      <Seg items={[['active', tt('Белсенді', 'Активные')], ['ended', tt('Аяқталған', 'Завершённые')], ['all', tt('Барлығы', 'Все')]]} cur={st} onPick={setSt} />
      <button className="btn ghost block" type="button" onClick={() => go('admin:queue')}>{tt('Өтінімдер кезегі', 'Очередь заявок')}</button>
      <ErrorLine err={list.err} />
      {!list.data ? <Loading /> : (
        <div className="list">{list.data.terms.length ? list.data.terms.map(t => (
          <div key={t.id} className="card">
            <b>{t.name || '—'}{t.username ? ` · @${t.username}` : ''}</b>
            <span className="small muted">{t.university} · {t.role_in_team === 'lead' ? tt('жетекші', 'лидер') : tt('мүше', 'участник')} · {tt(`${t.hours_per_week} сағ/апта`, `${t.hours_per_week} ч/нед`)}</span>
            <span className="small muted">{ymd(t.term_start)} — {ymd(t.term_end)} · {tt(`шақырғандары: ${t.joined}`, `пригласил(а): ${t.joined}`)} · {t.status}</span>
            <div className="btn-row">
              {t.status === 'active' ? <button className="btn ghost" type="button" onClick={() => openSheet(<TermEditSheet t={t} onDone={list.reload} />)}>{tt('Түзету', 'Изменить')}</button> : null}
              <button className="btn secondary" type="button" onClick={() => run(async () => {
                const r = await api<{ cert_code: string }>(`/api/admin/ambassadors/${t.id}/certificate`, { method: 'POST', lang });
                go('certificate:' + r.cert_code);
              }, tt('Сертификат берілді', 'Сертификат выдан'))}>{t.cert_code ? tt('Сертификат', 'Сертификат') : tt('Сертификат беру', 'Выдать сертификат')}</button>
              {t.status === 'active' ? <button className="btn danger" type="button" onClick={() => openSheet(<ReasonSheet title={tt('Мерзімді аяқтау', 'Завершить срок')} label={tt('Себебі (міндетті)', 'Причина (обязательно)')} action={tt('Аяқтау', 'Завершить')} danger
                onSubmit={async reason => { await api(`/api/admin/ambassadors/${t.id}/end`, { method: 'POST', body: { reason }, lang }); closeSheet(); toast(tt('Аяқталды, рөл студентке қайтты', 'Завершено, роль снята')); list.reload(); }} />)}>{tt('Аяқтау', 'Завершить')}</button> : null}
            </div>
          </div>
        )) : <div className="group"><div className="empty">{tt('Амбассадор жоқ', 'Амбассадоров нет')}</div></div>}</div>
      )}
      <button className="link-btn" type="button" style={{ alignSelf: 'flex-start' }} onClick={() => run(async () => {
        const r = await api<{ ended: number }>('/api/admin/ambassadors/expire', { method: 'POST', lang });
        toast(tt(`Мерзімі біткендер: ${r.ended}`, `Истёкших: ${r.ended}`));
      }, '')}>{tt('Мерзімі біткендерді қазір жабу', 'Закрыть истёкшие сроки сейчас')}</button>
    </>
  );
}

function TermEditSheet({ t, onDone }: { t: AdminTerm; onDone: () => void }) {
  const { tt, lang, toast, closeSheet } = useApp();
  const [end, setEnd] = useState(ymd(t.term_end));
  const [role, setRole] = useState(t.role_in_team);
  const [hours, setHours] = useState(t.hours_per_week);
  const [err, setErr] = useState('');
  return (
    <form className="form" onSubmit={async e => {
      e.preventDefault();
      try { await api(`/api/admin/ambassadors/${t.id}`, { method: 'PATCH', body: { term_end: end, role_in_team: role, hours_per_week: hours }, lang }); closeSheet(); toast(tt('Сақталды', 'Сохранено')); onDone(); }
      catch (e2) { setErr(errText(e2)); }
    }}>
      <h2>{t.name || t.university}</h2>
      <div className="field"><label htmlFor="teEnd">{tt('Мерзімі', 'Срок до')}</label><input className="input" id="teEnd" type="date" value={end} onChange={e => setEnd(e.target.value)} /></div>
      <div className="field"><span className="lbl">{tt('Командадағы рөлі', 'Роль в команде')}</span>
        <Seg items={[['lead', tt('Жетекші', 'Лидер')], ['member', tt('Мүше', 'Участник')]]} cur={role} onPick={v => setRole(v as 'lead' | 'member')} /></div>
      <div className="field"><label htmlFor="teHours">{tt('Сағат / апта (1–10)', 'Часов в неделю (1–10)')}</label>
        <input className="input" id="teHours" type="number" min={1} max={10} value={hours} onChange={e => setHours(+e.target.value)} /></div>
      <ErrorLine err={err} />
      <div className="btn-row">
        <button className="btn ghost" type="button" onClick={closeSheet}>{tt('Бас тарту', 'Отмена')}</button>
        <button className="btn" type="submit">{tt('Сақтау', 'Сохранить')}</button>
      </div>
    </form>
  );
}

/* ---------- market moderation ---------- */
export function AdminMarket() {
  return <Feature name="market"><AdminOnly><MarketAdmin /></AdminOnly></Feature>;
}
function MarketAdmin() {
  const { tt, L, lang, toast, openSheet, closeSheet } = useApp();
  const [st, setSt] = useState('reported');
  const list = useLoad<{ items: AdminMarketItem[] }>(`/api/admin/market/items?status=${st}`);
  const act = (it: AdminMarketItem, verb: 'hide' | 'restore') => openSheet(
    <ReasonSheet title={verb === 'hide' ? tt('Біржола жасыру', 'Скрыть навсегда') : tt('Қайта көрсету (шағым негізсіз)', 'Вернуть (жалобы необоснованны)')}
      label={tt('Себебі (міндетті, журналға жазылады)', 'Причина (обязательно, пишется в журнал)')} action={verb === 'hide' ? tt('Жасыру', 'Скрыть') : tt('Қайта көрсету', 'Вернуть')} danger={verb === 'hide'}
      onSubmit={async reason => { await api(`/api/admin/market/items/${it.id}/${verb}`, { method: 'POST', body: { reason }, lang }); closeSheet(); toast(tt('Сақталды', 'Сохранено')); list.reload(); }} />,
  );
  return (
    <>
      <div className="chips">{([['reported', tt('Шағым бар', 'С жалобами')], ['hidden', tt('Жасырылған', 'Скрытые')], ['pending', tt('Тексеруде', 'На проверке')], ['active', tt('Белсенді', 'Активные')]] as [string, string][])
        .map(([k, l]) => <Chip key={k} pressed={st === k} onClick={() => setSt(k)}>{l}</Chip>)}</div>
      <ErrorLine err={list.err} />
      {!list.data ? <Loading /> : list.data.items.length ? (
        <div className="list">{list.data.items.map(it => (
          <div key={it.id} className="card">
            <div className="card-row">
              <span className="mk-mini">{it.photos[0] ? <img src={it.photos[0]} alt="" /> : <Icon n="gift" s={22} />}</span>
              <div className="row-main"><span className="row-title">{it.title}</span>
                <span className="tiny muted">{L(MARKET_CATS[it.category])} · {L(MARKET_CONDITIONS[it.condition])} · {it.university} · {it.status}{it.hiddenReason ? ` (${it.hiddenReason})` : ''}</span>
                <span className="tiny muted">{it.author ? `${it.author.name}${it.author.username ? ' @' + it.author.username : ''}` : ''} · id {it.ownerId}</span></div>
            </div>
            {it.desc ? <p className="small" style={{ overflowWrap: 'anywhere' }}>{it.desc}</p> : null}
            {it.reports ? <span className="small num">{tt(`Шағым: ${it.reports}, саналған: ${it.countedReports}`, `Жалоб: ${it.reports}, засчитано: ${it.countedReports}`)}</span> : null}
            {it.reportReasons.length ? <ul className="bullets small">{it.reportReasons.map((r, i) => <li key={i} style={{ overflowWrap: 'anywhere' }}>{r}</li>)}</ul> : null}
            <div className="btn-row">
              {it.status !== 'hidden' || it.hiddenReason === 'reports' ? <button className="btn danger" type="button" onClick={() => act(it, 'hide')}>{tt('Жасыру', 'Скрыть')}</button> : null}
              {it.reports || it.hiddenReason === 'reports' ? <button className="btn ghost" type="button" onClick={() => act(it, 'restore')}>{tt('Негізсіз — қайта көрсету', 'Необоснованно — вернуть')}</button> : null}
              <button className="btn ghost" type="button" onClick={() => openSheet(<BanSheet userId={it.ownerId} name={it.author?.name ?? ''} />)}>{tt('Жариялауды тоқтату', 'Запретить публикации')}</button>
            </div>
          </div>
        ))}</div>
      ) : <div className="group"><div className="empty">{tt('Бос', 'Пусто')}</div></div>}
      <SectionHead title={tt('Тыйымды алу', 'Снять запрет')} />
      <UnbanForm />
    </>
  );
}

function BanSheet({ userId, name }: { userId: number; name: string }) {
  const { tt, lang, toast, closeSheet } = useApp();
  const [days, setDays] = useState(7);
  return (
    <ReasonSheet title={tt(`Жариялауды уақытша тоқтату: ${name || '#' + userId}`, `Временный запрет публикаций: ${name || '#' + userId}`)} label={tt('Себебі (міндетті)', 'Причина (обязательно)')} action={tt('Тоқтату', 'Запретить')} danger
      extra={<div className="field"><label htmlFor="banDays">{tt('Неше күнге (1–365)', 'На сколько дней (1–365)')}</label><input className="input" id="banDays" type="number" min={1} max={365} value={days} onChange={e => setDays(+e.target.value)} /></div>}
      onSubmit={async reason => {
        const r = await api<{ until: string }>('/api/admin/market/bans', { method: 'POST', body: { userId, days, reason }, lang });
        closeSheet(); toast(tt(`${ymd(r.until)} дейін тоқтатылды`, `Запрет до ${ymd(r.until)}`));
      }} />
  );
}

function UnbanForm() {
  const { tt, lang, toast } = useApp();
  const [uid, setUid] = useState('');
  const [reason, setReason] = useState('');
  const [err, setErr] = useState('');
  return (
    <form className="form" onSubmit={async e => {
      e.preventDefault(); setErr('');
      try { await api(`/api/admin/market/bans/${Number(uid)}`, { method: 'DELETE', body: { reason: reason.trim() }, lang }); toast(tt('Тыйым алынды', 'Запрет снят')); setUid(''); setReason(''); }
      catch (e2) { setErr(errText(e2)); }
    }}>
      <div className="grid-2">
        <div className="field"><label htmlFor="ubId">{tt('Қолданушы id', 'id пользователя')}</label><input className="input" id="ubId" inputMode="numeric" value={uid} onChange={e => setUid(e.target.value.replace(/\D/g, ''))} /></div>
        <div className="field"><label htmlFor="ubReason">{tt('Себебі', 'Причина')}</label><input className="input" id="ubReason" maxLength={500} value={reason} onChange={e => setReason(e.target.value)} /></div>
      </div>
      <ErrorLine err={err} />
      <button className="btn ghost" type="submit" disabled={!uid || reason.trim().length < 3}>{tt('Тыйымды алу', 'Снять запрет')}</button>
    </form>
  );
}

/** «Модерация» links on the /admin dashboard; each feature's link shows only while its switch is on */
export function ModerationLinks() {
  const { tt } = useApp();
  const amb = useFeature('ambassador'), mentors = useFeature('mentors'), market = useFeature('market'), events = useFeature('events');
  const q = useLoad<{ pendingByKind: Record<string, number> }>('/api/admin/queue?status=pending&limit=1');
  const pending = Object.values(q.data?.pendingByKind ?? {}).reduce((a, b) => a + b, 0);
  return (
    <section className="section">
      <SectionHead title={tt('Модерация', 'Модерация')} />
      <div className="group">
        <Row go="admin:queue" icon="shield" title={tt('Модерация кезегі', 'Очередь модерации')} end={pending ? <b className="num">{pending}</b> : undefined} />
        <Row go="admin:reports" icon="bell" title={tt('Шағымдар', 'Жалобы')} />
        {amb ? <Row go="admin:ambassadors" icon="award" title={tt('Амбассадорлар', 'Амбассадоры')} /> : null}
        {mentors ? <Row go="admin:mentors" icon="cap" title={tt('Менторларды жұптау', 'Подбор менторов')} /> : null}
        {market ? <Row go="admin:market" icon="gift" title={tt('Тегін заттар', 'Отдам даром')} /> : null}
        {events ? <Row go="organize" icon="calendar" title={tt('Іс-шаралар', 'Мероприятия')} /> : null}
      </div>
    </section>
  );
}
