'use client';
/* Admin of «Полиграфия» (/admin/print, docs/print.md): shops (hours, formats, minimum order, file retention, payment
   mode), staff, the shop's own Kaspi cashier login (the café flow), the price list, and the job audit trail. */
import { useState } from 'react';
import { useApp } from '@/components/app';
import { Row, SectionHead, Seg } from '@/components/ui';
import { AdminOnly, ErrorLine, Feature, Loading, errText, useLoad } from '@/components/views/common';
import { PrintCatalog } from '@/components/views/print';
import { AuditTrail, KaspiPanel, StaffPanel } from '@/components/views/shop-common';
import { api } from '@/lib/account';
import { hoursLabel, tengeToTiyn } from '@/lib/cafe';
import { KINDS, money, paymentLabel, priceStateLabel, statusLabel, type FileKind, type Job, type Shop } from '@/lib/print';

export function AdminPrint() {
  return <AdminOnly><Feature name="print"><AdminPrintBody /></Feature></AdminOnly>;
}

function AdminPrintBody() {
  const { tt } = useApp();
  const [tab, setTab] = useState('shops');
  return (
    <div className="cafe print">
      <p className="note">{tt('Ақша әрқашан полиграфияның өз Kaspi Pay шотына түседі. Файлдар жабық, сақтау мерзімінен соң өшіріледі.',
        'Деньги всегда идут на собственный Kaspi Pay-счёт полиграфии. Файлы закрыты и удаляются после срока хранения.')}</p>
      <Seg items={[['shops', tt('Полиграфиялар', 'Полиграфии')], ['jobs', tt('Тапсырыстар', 'Заказы')]]} cur={tab} onPick={setTab} />
      {tab === 'shops' ? <Shops /> : <Audit />}
    </div>
  );
}

const emptyShop = {
  nameKk: '', nameRu: '', building: '', placeKk: '', placeRu: '', openTime: '08:30', closeTime: '18:00', openDays: [1, 2, 3, 4, 5],
  slotMinutes: 15, leadMinutes: 20, minOrder: 0, accepts: [...KINDS] as FileKind[], retentionDays: 3, active: false, paymentMode: 'pickup' as Shop['paymentMode'],
  noteKk: '', noteRu: '', kaspiMerchantRef: '', kaspiOrgId: null as number | null,
};

function Shops() {
  const { tt, lang, openSheet } = useApp();
  const q = useLoad<{ shops: Shop[]; kaspiConfigured: boolean }>('/api/admin/print/shops');
  if (!q.data) return <><ErrorLine err={q.err} />{q.err ? null : <Loading />}</>;
  return (
    <section className="section">
      {!q.data.kaspiConfigured ? <p className="small muted">{tt('Kaspi байланысы бапталмаған (CAFE_KASPI_*): тек кассада төлеу жұмыс істейді.', 'Kaspi не настроен (CAFE_KASPI_*): работает только оплата на кассе.')}</p> : null}
      <button className="btn secondary" type="button" onClick={() => openSheet(<ShopForm onDone={q.reload} />)}>{tt('Жаңа полиграфия', 'Новая полиграфия')}</button>
      {q.data.shops.map(s => (
        <article className="card" key={s.id}>
          <h3>{lang !== 'kk' ? s.nameRu : s.nameKk} <span className={`badge ${s.active ? 'b-ok' : 'b-line'}`}>{s.active ? tt('Белсенді', 'Активна') : tt('Өшірулі', 'Выключена')}</span></h3>
          <p className="small muted">{hoursLabel(s, lang)} · {s.accepts.join(', ')} · {tt(`файлдар ${s.retentionDays} күн`, `файлы ${s.retentionDays} дн.`)} · {tt('төлем', 'оплата')}: {s.paymentMode}</p>
          <KaspiPanel kaspi={s.kaspi} base={`/api/admin/print/shops/${s.id}`} onDone={q.reload} />
          <StaffPanel staff={s.staff} base={`/api/admin/print/shops/${s.id}`} roles={[['operator', tt('оператор', 'оператор')], ['manager', tt('менеджер', 'менеджер')]]} onDone={q.reload} />
          <div className="btn-row">
            <button className="btn ghost" type="button" onClick={() => openSheet(<ShopForm s={s} onDone={q.reload} />)}>{tt('Баптау', 'Настроить')}</button>
            <button className="btn ghost" type="button" onClick={() => openSheet(<><h2>{tt('Прайс', 'Прайс')}</h2><PrintCatalog shopId={s.id} role="admin" /></>)}>{tt('Прайс', 'Прайс')}</button>
          </div>
        </article>
      ))}
    </section>
  );
}

function ShopForm({ s, onDone }: { s?: Shop; onDone: () => void }) {
  const { tt, lang, closeSheet, toast } = useApp();
  const [f, setF] = useState(() => (s ? { ...emptyShop, ...s, kaspiMerchantRef: s.kaspiMerchantRef ?? '', kaspiOrgId: s.kaspiOrgId ?? null } : emptyShop));
  const [minOrder, setMinOrder] = useState(() => String((s?.minOrder ?? 0) / 100));
  const [err, setErr] = useState('');
  const text = (k: 'nameKk' | 'nameRu' | 'building' | 'placeKk' | 'placeRu' | 'noteKk' | 'noteRu' | 'kaspiMerchantRef', label: string, max = 120) => (
    <div className="field"><label htmlFor={`ps_${k}`}>{label}</label>
      <input className="input" id={`ps_${k}`} maxLength={max} value={String(f[k] ?? '')} onChange={e => setF(x => ({ ...x, [k]: e.target.value }))} /></div>
  );
  const num = (k: 'slotMinutes' | 'leadMinutes' | 'retentionDays', label: string) => (
    <div className="field"><label htmlFor={`ps_${k}`}>{label}</label>
      <input className="input num" id={`ps_${k}`} type="number" value={f[k]} onChange={e => setF(x => ({ ...x, [k]: Number(e.target.value) }))} /></div>
  );
  const days = lang === 'en' ? ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun'] : lang !== 'kk' ? ['Пн', 'Вт', 'Ср', 'Чт', 'Пт', 'Сб', 'Вс'] : ['Дс', 'Сс', 'Ср', 'Бс', 'Жм', 'Сб', 'Жс'];
  return (
    <form className="form" onSubmit={async e => {
      e.preventDefault();
      const mo = minOrder.trim() === '' || minOrder.trim() === '0' ? 0 : tengeToTiyn(minOrder);
      if (mo === null) { setErr(tt('Ең аз соманы бүтін теңгемен жазыңыз.', 'Минимальную сумму — в целых тенге.')); return; }
      const body = {
        nameKk: f.nameKk.trim(), nameRu: f.nameRu.trim(), building: f.building.trim(), placeKk: f.placeKk.trim(), placeRu: f.placeRu.trim(),
        openTime: f.openTime, closeTime: f.closeTime, openDays: f.openDays, slotMinutes: f.slotMinutes, leadMinutes: f.leadMinutes, minOrder: mo,
        accepts: f.accepts, retentionDays: f.retentionDays, active: f.active, paymentMode: f.paymentMode, noteKk: f.noteKk.trim(), noteRu: f.noteRu.trim(),
        kaspiMerchantRef: (f.kaspiMerchantRef || '').trim(), kaspiOrgId: f.kaspiOrgId,
      };
      try {
        await api(s ? `/api/admin/print/shops/${s.id}` : '/api/admin/print/shops', { method: s ? 'PUT' : 'POST', body, lang });
        closeSheet(); toast(tt('Сақталды', 'Сохранено')); onDone();
      } catch (x) { setErr(errText(x)); }
    }}>
      <h2>{s ? tt('Полиграфияны баптау', 'Настройка полиграфии') : tt('Жаңа полиграфия', 'Новая полиграфия')}</h2>
      {text('nameKk', tt('Атауы (қаз.)', 'Название (каз.)'), 80)}
      {text('nameRu', tt('Атауы (орыс.)', 'Название (рус.)'), 80)}
      {text('building', tt('Ғимарат', 'Корпус'))}
      {text('placeKk', tt('Орны (қаз.): қабат, кабинет', 'Место (каз.): этаж, кабинет'), 200)}
      {text('placeRu', tt('Орны (орыс.)', 'Место (рус.)'), 200)}
      <div className="cafe-2col">
        <div className="field"><label htmlFor="ps_open">{tt('Ашылады', 'Открывается')}</label><input className="input" id="ps_open" type="time" value={f.openTime} onChange={e => setF(x => ({ ...x, openTime: e.target.value }))} /></div>
        <div className="field"><label htmlFor="ps_close">{tt('Жабылады', 'Закрывается')}</label><input className="input" id="ps_close" type="time" value={f.closeTime} onChange={e => setF(x => ({ ...x, closeTime: e.target.value }))} /></div>
      </div>
      <fieldset className="field"><legend className="lbl">{tt('Жұмыс күндері', 'Рабочие дни')}</legend>
        <div className="cafe-choices">{days.map((d, i) => {
          const on = f.openDays.includes(i + 1);
          return <button key={d} type="button" className="chip" aria-pressed={on} onClick={() => setF(x => ({ ...x, openDays: on ? x.openDays.filter(v => v !== i + 1) : [...x.openDays, i + 1].sort() }))}>{d}</button>;
        })}</div></fieldset>
      <fieldset className="field"><legend className="lbl">{tt('Қабылдайтын файлдар', 'Принимаемые файлы')}</legend>
        <div className="cafe-choices">{KINDS.map(k => {
          const on = f.accepts.includes(k);
          return <button key={k} type="button" className="chip" aria-pressed={on} onClick={() => setF(x => ({ ...x, accepts: on ? x.accepts.filter(v => v !== k) : [...x.accepts, k] }))}>{k.toUpperCase()}</button>;
        })}</div></fieldset>
      <div className="cafe-2col">
        {num('slotMinutes', tt('Алу уақыты қадамы, мин', 'Шаг времени выдачи, мин'))}
        {num('leadMinutes', tt('Ең ерте: қазір + мин', 'Самое раннее: сейчас + мин'))}
      </div>
      <div className="cafe-2col">
        {num('retentionDays', tt('Файлды сақтау, күн (1–30)', 'Хранить файлы, дней (1–30)'))}
        <div className="field"><label htmlFor="ps_min">{tt('Ең аз тапсырыс, ₸', 'Мин. заказ, ₸')}</label><input className="input num" id="ps_min" inputMode="numeric" value={minOrder} onChange={e => setMinOrder(e.target.value)} /></div>
      </div>
      <div className="field"><label htmlFor="ps_pay">{tt('Төлем', 'Оплата')}</label>
        <select className="input" id="ps_pay" value={f.paymentMode} onChange={e => setF(x => ({ ...x, paymentMode: e.target.value as Shop['paymentMode'] }))}>
          <option value="pickup">{tt('Тек кассада (әдепкі)', 'Только на кассе (по умолчанию)')}</option>
          <option value="both">{tt('Kaspi не касса', 'Kaspi или касса')}</option>
          <option value="kaspi">{tt('Тек Kaspi (алдын ала)', 'Только Kaspi (предоплата)')}</option>
        </select></div>
      {text('noteKk', tt('Студенттерге ескертпе (қаз.)', 'Заметка для студентов (каз.)'), 300)}
      {text('noteRu', tt('Студенттерге ескертпе (орыс.)', 'Заметка для студентов (рус.)'), 300)}
      {text('kaspiMerchantRef', tt('Kaspi Pay-дегі атауы / БСН (құпия емес)', 'Название / БИН в Kaspi Pay (не секрет)'))}
      <div className="field"><label htmlFor="ps_org">{tt('OrganizationId (бір нөмір бірнеше бизнесте болса ғана)', 'OrganizationId (только если один номер в нескольких бизнесах)')}</label>
        <input className="input num" id="ps_org" inputMode="numeric" value={f.kaspiOrgId ?? ''} onChange={e => setF(x => ({ ...x, kaspiOrgId: e.target.value ? Number(e.target.value) : null }))} /></div>
      <label className="cafe-switch"><input type="checkbox" checked={f.active} onChange={e => setF(x => ({ ...x, active: e.target.checked }))} /><span>{tt('Белсенді (студенттерге көрінеді)', 'Активна (видна студентам)')}</span></label>
      <ErrorLine err={err} />
      <button className="btn block" type="submit">{tt('Сақтау', 'Сохранить')}</button>
    </form>
  );
}

function Audit() {
  const { tt, lang, openSheet } = useApp();
  const [status, setStatus] = useState('');
  const q = useLoad<{ jobs: Job[] }>(`/api/admin/print/jobs${status ? `?status=${status}` : ''}`);
  const open = async (j: Job) => {
    const r = await api<{ job: Job }>(`/api/admin/print/jobs/${j.id}`, { lang });
    openSheet(<AuditSheet j={r.job} />);
  };
  return (
    <section className="section">
      <div className="field"><label htmlFor="paStatus">{tt('Күйі', 'Статус')}</label>
        <select className="input" id="paStatus" value={status} onChange={e => setStatus(e.target.value)}>
          <option value="">{tt('Барлығы', 'Все')}</option>
          {['new', 'printing', 'ready', 'picked_up', 'cancelled', 'no_show', 'review', 'refund_manual'].map(x => <option key={x} value={x}>{x}</option>)}
        </select></div>
      <ErrorLine err={q.err} />
      {!q.data ? <Loading /> : (
        <div className="group">
          {q.data.jobs.map(j => (
            <Row key={j.id} onClick={() => void open(j)} title={<span className="num">#{j.id} · №{j.pickupCode} · {money(j.total)}</span>}
              sub={`${lang !== 'kk' ? j.shopNameRu : j.shopNameKk} · ${j.customer || '—'} · ${j.createdAt.slice(0, 10)} · ${paymentLabel(j, tt)}`}
              end={<span className="badge b-line">{statusLabel(j.status, tt)}</span>} />
          ))}
        </div>
      )}
    </section>
  );
}

function AuditSheet({ j }: { j: Job }) {
  const { tt } = useApp();
  return (
    <>
      <h2 className="num">#{j.id} · №{j.pickupCode}</h2>
      <p className="small">{statusLabel(j.status, tt)} · {priceStateLabel(j.priceState, tt)} · {paymentLabel(j, tt)} · {money(j.total)}</p>
      <p className="small muted">{j.files.map(f => `${f.name} (${f.kind}${f.deleted ? ', ' + tt('өшірілді', 'удалён') : ''})`).join(' · ')}</p>
      <SectionHead title={tt('Журнал', 'Журнал')} />
      <AuditTrail events={j.events} />
    </>
  );
}
