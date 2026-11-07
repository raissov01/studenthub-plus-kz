'use client';
/* /admin/payments — «Төлемдер»: the money of Кофехана orders and print jobs in one list (kurs payments, adapted): filters,
   the totals of the filter, revenue by day, and «Ақша қайтарылды» for a paid order Kaspi could not refund by itself.
   The super admin sees every shop; a shop manager only their own (the server decides and answers 403 otherwise).
   /admin/payments?cafe=3 or ?print=2 opens one shop. */
import { useState } from 'react';
import { useApp } from '@/components/app';
import { ChartCard, ColumnChart, compact, DataTable, KpiStrip, type Point } from '@/components/charts';
import { ErrorLine, errText, Loading, NotFound, useLoad, useQueryParam, ymd } from '@/components/views/common';
import { orderStatus, payLabel } from '@/components/views/admin-users';
import { parseQuery, SavedViews } from '@/components/views/admin-views';
import { api, apiDownload, useAccount } from '@/lib/account';
import { money } from '@/lib/cafe';
import { query } from '@/lib/console';

interface Row {
  kind: 'cafe' | 'print'; id: number; shopId: number; shopKk: string; shopRu: string; customer: string | null; customerId: number | null;
  total: number; paid: number | null; status: string; method: string; payment: string; refundManual: boolean; createdAt: string; closedAt: string | null;
}
interface Resp {
  scope: 'all' | 'managed'; page: number; limit: number; rows: Row[]; days: { day: string; orders: number; revenue: number }[];
  shops: { kind: 'cafe' | 'print'; id: number; nameKk: string; nameRu: string }[];
  totals: { count: number; revenue: number; paidKaspi: number; refunded: number; toReturn: number; toReturnSum: number };
}

const EMPTY = { kind: '', shop: '', payment: '', status: '', method: '', from: '', to: '' };
type Filter = typeof EMPTY;

export function AdminPayments() {
  const { tt, lang, monS, toast, go } = useApp();
  const acc = useAccount();
  const cafe = useQueryParam('cafe'), print = useQueryParam('print');
  const [f, setF] = useState<Filter>(() => ({ ...EMPTY, ...(cafe ? { kind: 'cafe', shop: cafe } : print ? { kind: 'print', shop: print } : {}) }));
  const [page, setPage] = useState(1);
  const qs = query({ ...f, page: page > 1 ? page : '' });
  const r = useLoad<Resp>(acc.user ? '/api/admin/payments' + qs : null);
  const set = (k: keyof Filter, v: string) => { setPage(1); setF(x => ({ ...x, [k]: v })); };
  if (!acc.loaded) return <Loading />;
  if (!acc.user || r.code === 'forbidden' || r.code === 'not_found') return <NotFound />;
  const d = r.data;
  const nm = (s: { nameKk?: string; nameRu?: string; shopKk?: string; shopRu?: string }) => (lang !== 'kk' ? s.nameRu || s.shopRu : s.nameKk || s.shopKk) || '';
  const days: Point[] = (d?.days ?? []).map(x => {
    const [, m, dd] = x.day.split('-').map(Number);
    return { label: String(dd), long: `${dd} ${monS[m - 1]}`, value: Math.round(x.revenue / 100), text: tt(`${money(x.revenue)} · ${x.orders} тапсырыс`, `${money(x.revenue)} · заказов ${x.orders}`) };
  });
  const markReturned = async (row: Row) => {
    try {
      await api(row.kind === 'cafe' ? `/api/cafe/staff/orders/${row.id}/refunded` : `/api/print/staff/jobs/${row.id}/refunded`, { method: 'POST', lang });
      toast(tt('Белгіленді: ақша қайтарылды', 'Отмечено: деньги возвращены')); r.reload();
    } catch (e) { toast(errText(e)); }
  };
  const pages = d ? Math.max(1, Math.ceil(d.totals.count / d.limit)) : 1;
  const sel = (k: keyof Filter, label: string, opts: [string, string][]) => (
    <div className="field">
      <label htmlFor={'pf_' + k}>{label}</label>
      <select className="input" id={'pf_' + k} value={f[k]} onChange={e => set(k, e.target.value)}>{opts.map(([v, l]) => <option key={v} value={v}>{l}</option>)}</select>
    </div>
  );
  const shopValue = f.kind && f.shop ? `${f.kind}:${f.shop}` : f.kind;
  return (
    <div className="stack">
      <p className="small muted">{d?.scope === 'managed'
        ? tt('Сіз басқаратын нүктелердің тапсырыстары мен ақшасы.', 'Заказы и деньги точек, которыми вы управляете.')
        : tt('Барлық кофехана мен полиграфияның тапсырыстары мен ақшасы. Ақша нүктенің өз шотына түседі.', 'Заказы и деньги всех кофеен и полиграфий. Деньги идут на счёт самой точки.')}</p>
      <div className="adm-filters card">
        <div className="field">
          <label htmlFor="pf_shop">{tt('Нүкте', 'Точка')}</label>
          <select className="input" id="pf_shop" value={shopValue} onChange={e => {
            const [k, id] = e.target.value.split(':'); setPage(1); setF(x => ({ ...x, kind: k || '', shop: id || '' }));
          }}>
            <option value="">{tt('Барлық нүкте', 'Все точки')}</option>
            <option value="cafe">{tt('Барлық кофехана', 'Все кофейни')}</option>
            <option value="print">{tt('Барлық полиграфия', 'Все полиграфии')}</option>
            {(d?.shops ?? []).map(s => <option key={s.kind + s.id} value={`${s.kind}:${s.id}`}>{nm(s)}</option>)}
          </select>
        </div>
        {sel('payment', tt('Төлем', 'Оплата'), [['', tt('Кез келген', 'Любая')], ['paid', tt('Kaspi арқылы төленді', 'Оплачено через Kaspi')], ['unpaid', tt('Кассада / төленбеген', 'На кассе / не оплачено')],
          ['refund_manual', tt('Қолмен қайтару керек', 'Вернуть вручную')], ['refunded', tt('Қайтарылды', 'Возвращено')], ['failed', tt('Өтпеді', 'Не прошла')]])}
        {sel('status', tt('Тапсырыс', 'Заказ'), [['', tt('Кез келген', 'Любой')], ['picked_up', tt('Берілді', 'Выдан')], ['cancelled', tt('Тоқтатылды', 'Отменён')], ['no_show', tt('Алып кетпеді', 'Не забрали')],
          ['new', tt('Жаңа', 'Новый')], ['ready', tt('Дайын', 'Готов')]])}
        <div className="field"><label htmlFor="pf_from">{tt('Бастап', 'С')}</label><input className="input" id="pf_from" type="date" value={f.from} onChange={e => set('from', e.target.value)} /></div>
        <div className="field"><label htmlFor="pf_to">{tt('Дейін', 'По')}</label><input className="input" id="pf_to" type="date" value={f.to} onChange={e => set('to', e.target.value)} /></div>
      </div>
      <SavedViews screen="payments" query={query(f)} onApply={v => { setF(parseQuery(v, EMPTY)); setPage(1); }} />
      <ErrorLine err={r.err} />
      {!d ? (r.err ? null : <Loading />) : <>
        <KpiStrip items={[
          { label: tt('Табыс (берілгендер)', 'Выручка (выданные)'), value: money(d.totals.revenue) },
          { label: tt('Тапсырыс', 'Заказов'), value: String(d.totals.count) },
          { label: tt('Kaspi арқылы төленді', 'Оплачено через Kaspi'), value: money(d.totals.paidKaspi) },
          { label: tt('Қайтарылды', 'Возвращено'), value: money(d.totals.refunded) },
          { label: tt(`Қолмен қайтару керек (${d.totals.toReturn})`, `Вернуть вручную (${d.totals.toReturn})`), value: money(d.totals.toReturnSum),
            tone: d.totals.toReturn ? 'warn' : undefined, go: d.totals.toReturn ? () => set('payment', 'refund_manual') : undefined },
        ]} />
        <ChartCard testId="chart-pay" title={tt('Табыс күн сайын', 'Выручка по дням')} subtitle={tt('Берілген тапсырыстар, ₸ (қайтарылғандарсыз)', 'Выданные заказы, ₸ (без возвратов)')}
          table={<DataTable head={[tt('Күн', 'День'), tt('Сома', 'Сумма')]} rows={days.map(p => [p.long, p.text])} />}>
          <ColumnChart data={days} fmtTick={v => compact(v, tt)} label={`${tt('Табыс', 'Выручка')}: ${days.map(p => `${p.long} ${p.text}`).join('; ')}`} />
        </ChartCard>
        <div className="section-head">
          <span className="small muted num" role="status">{tt(`Табылды: ${d.totals.count}`, `Найдено: ${d.totals.count}`)}</span>
          {d.scope === 'all' ? <button className="link-btn" type="button" onClick={() => apiDownload('/api/admin/export/cafe-orders.csv', 'studenthub-cafe-orders.csv', lang).catch(e => toast(errText(e)))}>{tt('Excel-ге жүктеу', 'Скачать для Excel')}</button> : null}
        </div>
        {d.rows.length ? (
          <div className="group pay-list" data-testid="pay-list">
            {d.rows.map(x => (
              <div key={x.kind + x.id} className={`pay-row${x.refundManual ? ' is-owed' : ''}`}>
                <span className="pay-main">
                  <b>{money(x.paid ?? x.total)} <span className="small muted">· {nm(x)}</span></b>
                  <span className="small muted">{[`#${x.id}`, ymd(x.createdAt), x.customer || tt('өшірілген аккаунт', 'удалённый аккаунт'), orderStatus(x.status, tt), payLabel(x.payment, x.method, tt)].join(' · ')}</span>
                  {x.refundManual ? <span className="small urgent">{tt('Тапсырыс тоқтатылды, ақшаны Kaspi Pay-де қолмен қайтару керек', 'Заказ отменён, деньги нужно вернуть вручную в Kaspi Pay')}</span> : null}
                </span>
                <span className="pay-act">
                  {x.refundManual ? <button className="btn secondary" type="button" onClick={() => void markReturned(x)}>{tt('Ақша қайтарылды', 'Деньги возвращены')}</button> : null}
                  {d.scope === 'all' && x.customerId ? <button className="link-btn" type="button" onClick={() => go('admin:users?id=' + x.customerId)}>{tt('Студент', 'Студент')}</button> : null}
                </span>
              </div>
            ))}
          </div>
        ) : <div className="group"><div className="empty">{tt('Бұл сүзгімен тапсырыс жоқ', 'По этому фильтру заказов нет')}</div></div>}
        {pages > 1 ? (
          <nav className="adm-pager" aria-label={tt('Беттер', 'Страницы')}>
            <button className="btn ghost" type="button" disabled={page <= 1} onClick={() => setPage(p => p - 1)}>{tt('Алдыңғы', 'Назад')}</button>
            <span className="small muted num">{tt(`${page}-бет / ${pages}`, `Стр. ${page} из ${pages}`)}</span>
            <button className="btn ghost" type="button" disabled={page >= pages} onClick={() => setPage(p => p + 1)}>{tt('Келесі', 'Далее')}</button>
          </nav>
        ) : null}
      </>}
    </div>
  );
}
