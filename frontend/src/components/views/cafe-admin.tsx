'use client';
/* Admin of «Кофе / Кофейня» (/admin/cafe, docs/cafe.md): cafés (hours, slots, payment mode), staff, the café's Kaspi
   cashier login (the café added our cashier number in Kaspi Pay; SMS code step), the menu, and the order audit trail. */
import { useState } from 'react';
import { useApp } from '@/components/app';
import { Row, SectionHead, Seg } from '@/components/ui';
import { AdminOnly, ErrorLine, Feature, Loading, errText, hhmm, useLoad } from '@/components/views/common';
import { StaffMenu } from '@/components/views/cafe';
import { AuditTrail, KaspiPanel, StaffPanel } from '@/components/views/shop-common';
import { useQueryId } from '@/components/views/subjects';
import { api } from '@/lib/account';
import { hoursLabel, money, paymentLabel, statusLabel, type Cafe, type Order } from '@/lib/cafe';

export function AdminCafe() {
  return <AdminOnly><Feature name="cafe"><AdminCafeBody /></Feature></AdminOnly>;
}

function AdminCafeBody() {
  const { tt } = useApp();
  const [tab, setTab] = useState('cafes');
  return (
    <div className="cafe">
      <p className="note">{tt('Ақша әрқашан кофейняның өз Kaspi Pay шотына түседі. Біз тек кассир сессиясымен QR жасап, төлем күйін оқимыз.',
        'Деньги всегда идут на собственный Kaspi Pay-счёт кофейни. Мы только создаём QR через сессию кассира и читаем статус оплаты.')}</p>
      <Seg items={[['cafes', tt('Кофейнялар', 'Кофейни')], ['orders', tt('Тапсырыстар', 'Заказы')]]} cur={tab} onPick={setTab} />
      {tab === 'cafes' ? <Cafes /> : <Audit />}
    </div>
  );
}

const emptyCafe = { nameKk: '', nameRu: '', building: '', placeKk: '', placeRu: '', openTime: '08:00', closeTime: '18:00', openDays: [1, 2, 3, 4, 5],
  slotMinutes: 10, slotCapacity: 6, leadMinutes: 10, active: false, paymentMode: 'pickup', kaspiMerchantRef: '', kaspiOrgId: null as number | null };

function Cafes() {
  const { tt, lang, openSheet } = useApp();
  const q = useLoad<{ cafes: Cafe[]; kaspiConfigured: boolean }>('/api/admin/cafes');
  if (!q.data) return <><ErrorLine err={q.err} />{q.err ? null : <Loading />}</>;
  return (
    <section className="section">
      {!q.data.kaspiConfigured ? <p className="small muted">{tt('Kaspi байланысы бапталмаған (CAFE_KASPI_*): тек кассада төлеу жұмыс істейді.', 'Kaspi не настроен (CAFE_KASPI_*): работает только оплата на кассе.')}</p> : null}
      <button className="btn secondary" type="button" onClick={() => openSheet(<CafeForm onDone={q.reload} />)}>{tt('Жаңа кофейня', 'Новая кофейня')}</button>
      {q.data.cafes.map(c => (
        <article className="card" key={c.id}>
          <h3>{lang === 'ru' ? c.nameRu : c.nameKk} <span className={`badge ${c.active ? 'b-ok' : 'b-line'}`}>{c.active ? tt('Белсенді', 'Активна') : tt('Өшірулі', 'Выключена')}</span></h3>
          <p className="small muted">{hoursLabel(c, lang)} · {tt(`слот ${c.slotMinutes} мин, ${c.slotCapacity} тапсырыс`, `слот ${c.slotMinutes} мин, ${c.slotCapacity} заказов`)} · {tt('төлем', 'оплата')}: {c.paymentMode}</p>
          <KaspiPanel kaspi={c.kaspi} base={`/api/admin/cafes/${c.id}`} onDone={q.reload} />
          <StaffPanel staff={c.staff} base={`/api/admin/cafes/${c.id}`} roles={[['barista', tt('бариста', 'бариста')], ['manager', tt('менеджер', 'менеджер')]]} onDone={q.reload} />
          <div className="btn-row">
            <button className="btn ghost" type="button" onClick={() => openSheet(<CafeForm c={c} onDone={q.reload} />)}>{tt('Баптау', 'Настроить')}</button>
            <button className="btn ghost" type="button" onClick={() => openSheet(<><h2>{tt('Мәзір', 'Меню')}</h2><StaffMenu cafeId={c.id} role="admin" /></>)}>{tt('Мәзір', 'Меню')}</button>
          </div>
        </article>
      ))}
    </section>
  );
}

function CafeForm({ c, onDone }: { c?: Cafe; onDone: () => void }) {
  const { tt, lang, closeSheet, toast } = useApp();
  const [f, setF] = useState(() => (c ? { ...emptyCafe, ...c, kaspiMerchantRef: c.kaspiMerchantRef ?? '', kaspiOrgId: c.kaspiOrgId ?? null } : emptyCafe));
  const [err, setErr] = useState('');
  const text = (k: keyof typeof f, label: string, max = 120) => (
    <div className="field"><label htmlFor={`cf_${k}`}>{label}</label>
      <input className="input" id={`cf_${k}`} maxLength={max} value={String(f[k] ?? '')} onChange={e => setF(x => ({ ...x, [k]: e.target.value }))} /></div>
  );
  const num = (k: 'slotMinutes' | 'slotCapacity' | 'leadMinutes', label: string) => (
    <div className="field"><label htmlFor={`cf_${k}`}>{label}</label>
      <input className="input num" id={`cf_${k}`} type="number" value={f[k]} onChange={e => setF(x => ({ ...x, [k]: Number(e.target.value) }))} /></div>
  );
  const days = lang === 'ru' ? ['Пн', 'Вт', 'Ср', 'Чт', 'Пт', 'Сб', 'Вс'] : ['Дс', 'Сс', 'Ср', 'Бс', 'Жм', 'Сб', 'Жс'];
  return (
    <form className="form" onSubmit={async e => {
      e.preventDefault();
      const body = { nameKk: f.nameKk.trim(), nameRu: f.nameRu.trim(), building: f.building.trim(), placeKk: f.placeKk.trim(), placeRu: f.placeRu.trim(),
        openTime: f.openTime, closeTime: f.closeTime, openDays: f.openDays, slotMinutes: f.slotMinutes, slotCapacity: f.slotCapacity, leadMinutes: f.leadMinutes,
        active: f.active, paymentMode: f.paymentMode, kaspiMerchantRef: (f.kaspiMerchantRef || '').trim(), kaspiOrgId: f.kaspiOrgId };
      try {
        await api(c ? `/api/admin/cafes/${c.id}` : '/api/admin/cafes', { method: c ? 'PUT' : 'POST', body, lang });
        closeSheet(); toast(tt('Сақталды', 'Сохранено')); onDone();
      } catch (x) { setErr(errText(x)); }
    }}>
      <h2>{c ? tt('Кофейняны баптау', 'Настройка кофейни') : tt('Жаңа кофейня', 'Новая кофейня')}</h2>
      {text('nameKk', tt('Атауы (қаз.)', 'Название (каз.)'), 80)}
      {text('nameRu', tt('Атауы (орыс.)', 'Название (рус.)'), 80)}
      {text('building', tt('Ғимарат', 'Корпус'))}
      {text('placeKk', tt('Орны (қаз.): қабат, фойе', 'Место (каз.): этаж, фойе'), 200)}
      {text('placeRu', tt('Орны (орыс.)', 'Место (рус.)'), 200)}
      <div className="cafe-2col">
        <div className="field"><label htmlFor="cf_open">{tt('Ашылады', 'Открывается')}</label><input className="input" id="cf_open" type="time" value={f.openTime} onChange={e => setF(x => ({ ...x, openTime: e.target.value }))} /></div>
        <div className="field"><label htmlFor="cf_close">{tt('Жабылады', 'Закрывается')}</label><input className="input" id="cf_close" type="time" value={f.closeTime} onChange={e => setF(x => ({ ...x, closeTime: e.target.value }))} /></div>
      </div>
      <fieldset className="field"><legend className="lbl">{tt('Жұмыс күндері', 'Рабочие дни')}</legend>
        <div className="cafe-choices">{days.map((d, i) => {
          const on = f.openDays.includes(i + 1);
          return <button key={d} type="button" className="chip" aria-pressed={on} onClick={() => setF(x => ({ ...x, openDays: on ? x.openDays.filter(v => v !== i + 1) : [...x.openDays, i + 1].sort() }))}>{d}</button>;
        })}</div></fieldset>
      <div className="cafe-2col">
        {num('slotMinutes', tt('Слот, мин', 'Слот, мин'))}
        {num('slotCapacity', tt('Слотқа тапсырыс', 'Заказов на слот'))}
      </div>
      {num('leadMinutes', tt('Ең ерте алу: қазір + мин', 'Самое раннее: сейчас + мин'))}
      <div className="field"><label htmlFor="cf_pay">{tt('Төлем', 'Оплата')}</label>
        <select className="input" id="cf_pay" value={f.paymentMode} onChange={e => setF(x => ({ ...x, paymentMode: e.target.value as Cafe['paymentMode'] }))}>
          <option value="pickup">{tt('Тек кассада (әдепкі)', 'Только на кассе (по умолчанию)')}</option>
          <option value="both">{tt('Kaspi не касса', 'Kaspi или касса')}</option>
          <option value="kaspi">{tt('Тек Kaspi (алдын ала)', 'Только Kaspi (предоплата)')}</option>
        </select></div>
      {text('kaspiMerchantRef', tt('Kaspi Pay-дегі атауы / БСН (құпия емес)', 'Название / БИН в Kaspi Pay (не секрет)'))}
      <div className="field"><label htmlFor="cf_org">{tt('OrganizationId (бір нөмір бірнеше бизнесте болса ғана)', 'OrganizationId (только если один номер в нескольких бизнесах)')}</label>
        <input className="input num" id="cf_org" inputMode="numeric" value={f.kaspiOrgId ?? ''} onChange={e => setF(x => ({ ...x, kaspiOrgId: e.target.value ? Number(e.target.value) : null }))} /></div>
      <label className="cafe-switch"><input type="checkbox" checked={f.active} onChange={e => setF(x => ({ ...x, active: e.target.checked }))} /><span>{tt('Белсенді (студенттерге көрінеді)', 'Активна (видна студентам)')}</span></label>
      <ErrorLine err={err} />
      <button className="btn block" type="submit">{tt('Сақтау', 'Сохранить')}</button>
    </form>
  );
}

function Audit() {
  const { tt, lang, openSheet } = useApp();
  const id = Number(useQueryId() || 0);
  const [status, setStatus] = useState('');
  const q = useLoad<{ orders: Order[] }>(`/api/admin/cafe/orders${status ? `?status=${status}` : ''}`);
  const open = async (o: Order) => {
    const r = await api<{ order: Order }>(`/api/admin/cafe/orders/${o.id}`, { lang });
    openSheet(<AuditSheet o={r.order} />);
  };
  return (
    <section className="section">
      <div className="field"><label htmlFor="auStatus">{tt('Күйі', 'Статус')}</label>
        <select className="input" id="auStatus" value={status} onChange={e => setStatus(e.target.value)}>
          <option value="">{tt('Барлығы', 'Все')}</option>
          {['new', 'accepted', 'ready', 'picked_up', 'cancelled', 'no_show', 'refund_manual'].map(s => <option key={s} value={s}>{s}</option>)}
        </select></div>
      <ErrorLine err={q.err} />
      {!q.data ? <Loading /> : (
        <div className="group">
          {q.data.orders.map(o => (
            <Row key={o.id} onClick={() => void open(o)} title={<span className="num">#{o.id} · №{o.pickupCode} · {money(o.total)}{o.id === id ? ' ●' : ''}</span>}
              sub={`${lang === 'ru' ? o.cafeNameRu : o.cafeNameKk} · ${o.customer || '—'} · ${o.pickupAt.slice(0, 10)} ${hhmm(o.pickupAt)} · ${paymentLabel(o, tt)}`}
              end={<span className="badge b-line">{statusLabel(o.status, tt)}</span>} />
          ))}
        </div>
      )}
    </section>
  );
}

function AuditSheet({ o }: { o: Order }) {
  const { tt } = useApp();
  return (
    <>
      <h2 className="num">#{o.id} · №{o.pickupCode}</h2>
      <p className="small">{statusLabel(o.status, tt)} · {paymentLabel(o, tt)} · {money(o.total)}</p>
      <SectionHead title={tt('Журнал', 'Журнал')} />
      <AuditTrail events={o.events} />
    </>
  );
}
