'use client';
/* From the competitor research (docs/research/print-competitors.md): the price before any upload, and the
   operator's day in numbers. Every sum comes from the server. Flat: solid fills, 1px borders. */
import { useEffect, useState } from 'react';
import { useApp } from '@/components/app';
import { ErrorLine, Loading, errText, useLoad } from '@/components/views/common';
import { api } from '@/lib/account';
import { colorAt, duplexAt, money, sizesOf, type Quote, type Service } from '@/lib/print';

const nameOf = (x: { nameKk: string; nameRu: string }, lang: string) => (lang !== 'kk' ? x.nameRu : x.nameKk);

/** «how much would it be?» — pages typed by hand, the shop's own prices, the server's sum; nothing is ordered */
export function PriceCalc({ shopId, services }: { shopId: number; services: Service[] }) {
  const { tt, lang } = useApp();
  const prints = services.filter(s => s.kind === 'print' && s.online);
  const bindings = services.filter(s => s.kind === 'binding' && s.online);
  const [svcId, setSvcId] = useState(prints[0]?.id ?? 0);
  const svc = prints.find(s => s.id === svcId) ?? prints[0];
  const sizes = sizesOf(svc);
  const [size, setSize] = useState(sizes.includes('A4') ? 'A4' : sizes[0] ?? 'A4');
  const [color, setColor] = useState(false);
  const [duplex, setDuplex] = useState(false);
  const [pages, setPages] = useState('');
  const [copies, setCopies] = useState('1');
  const [binding, setBinding] = useState(0);
  const [quote, setQuote] = useState<Quote | null>(null);
  const [err, setErr] = useState('');
  const sz = sizes.includes(size) ? size : sizes[0] ?? size;
  const col = color && colorAt(svc, sz);
  const dup = duplex && duplexAt(svc, sz, col);
  const p = Math.round(Number(pages)), c = Math.round(Number(copies));
  const ok = !!svc && p >= 1 && p <= 2000 && c >= 1 && c <= 100;
  const body = JSON.stringify({ shopId, items: [{ serviceId: svc?.id ?? 0, size: sz, color: col, duplex: dup, copies: c, pages: p, bindingId: binding }] });
  useEffect(() => {
    if (!ok) return;
    let live = true;
    const t = setTimeout(() => {
      api<{ quote: Quote }>('/api/print/estimate', { method: 'POST', body: JSON.parse(body), lang })
        .then(r => { if (live) { setQuote(r.quote); setErr(''); } }, e => { if (live) { setQuote(null); setErr(errText(e)); } });
    }, 400);
    return () => { live = false; clearTimeout(t); };
  }, [body, ok, lang]);
  if (!svc) return null;
  return (
    <details className="card print-calc" id="printCalc">
      <summary className="print-calc-sum">{tt('Файлсыз бағаны есептеу', 'Посчитать цену без файла')}</summary>
      {prints.length > 1 ? (
        <div className="field"><label htmlFor="pcSvc">{tt('Қызмет', 'Услуга')}</label>
          <select className="input" id="pcSvc" value={svc.id} onChange={e => setSvcId(Number(e.target.value))}>
            {prints.map(s => <option key={s.id} value={s.id}>{nameOf(s, lang)}</option>)}
          </select></div>
      ) : null}
      <div className="print-2col">
        <div className="field"><label htmlFor="pcPages">{tt('Бет саны', 'Страниц')}</label>
          <input className="input num" id="pcPages" type="number" inputMode="numeric" min={1} max={2000} value={pages} onChange={e => setPages(e.target.value)} /></div>
        <div className="field"><label htmlFor="pcCopies">{tt('Дана', 'Копий')}</label>
          <input className="input num" id="pcCopies" type="number" inputMode="numeric" min={1} max={100} value={copies} onChange={e => setCopies(e.target.value)} /></div>
      </div>
      <fieldset className="cafe-opt"><legend className="lbl">{tt('Қағаз өлшемі', 'Формат бумаги')}</legend>
        <div className="cafe-choices">{sizes.map(s => <button key={s} type="button" className="chip" aria-pressed={sz === s} onClick={() => setSize(s)}>{s}</button>)}</div>
      </fieldset>
      <div className="cafe-choices">
        <button type="button" className="chip" aria-pressed={!col} onClick={() => setColor(false)}>{tt('Ақ-қара', 'Ч/б')}</button>
        <button type="button" className="chip" aria-pressed={col} disabled={!colorAt(svc, sz)} onClick={() => setColor(true)}>{tt('Түрлі түсті', 'Цветная')}</button>
      </div>
      {duplexAt(svc, sz, col) ? <label className="cafe-switch"><input type="checkbox" checked={dup} onChange={e => setDuplex(e.target.checked)} /><span>{tt('Екі жақты', 'Двусторонняя')}</span></label> : null}
      {bindings.length ? (
        <div className="field"><label htmlFor="pcBind">{tt('Түптеу', 'Переплёт')}</label>
          <select className="input" id="pcBind" value={binding} onChange={e => setBinding(Number(e.target.value))}>
            <option value={0}>{tt('Жоқ', 'Нет')}</option>
            {bindings.map(s => <option key={s.id} value={s.id}>{nameOf(s, lang)}</option>)}
          </select></div>
      ) : null}
      <div aria-live="polite">
        {!ok ? <p className="small muted">{tt('Бет санын жазыңыз.', 'Укажите число страниц.')}</p>
          : err ? <ErrorLine err={err} />
            : quote ? <p className="cafe-total"><span>{tt('Шамамен', 'Примерно')}</span><b className="num" id="printCalcTotal">{money(quote.total)}</b></p>
              : <p className="small muted">{tt('Баға есептелуде…', 'Считаем цену…')}</p>}
      </div>
      <p className="tiny muted">{tt('Нақты баға файлды жүктегенде есептеледі (бет санын сервер өзі санайды).', 'Точная цена — после загрузки файла (страницы считает сервер).')}</p>
    </details>
  );
}

interface Report {
  day: string; received: number; pickedUp: number; noShow: number; cancelled: number; open: number;
  revenueKaspi: number; revenueCounter: number; refunded: number; refundsManual: number;
  pagesBw: number; pagesColor: number; sheets: number; waitMedianMin: number | null;
}

/** the operator's day: what came in, what went out, the money by way of payment */
export function DayReport({ shopId }: { shopId: number }) {
  const { tt } = useApp();
  const [day, setDay] = useState('');
  const q = useLoad<{ report: Report }>(`/api/print/staff/report?shop=${shopId}${day ? `&day=${day}` : ''}`);
  const r = q.data?.report;
  const rows: [string, string][] = r ? [
    [tt('Келді', 'Поступило'), String(r.received)],
    [tt('Берілді', 'Выдано'), String(r.pickedUp)],
    [tt('Алынбады', 'Не забрали'), String(r.noShow)],
    [tt('Болдырылмады', 'Отменено'), String(r.cancelled)],
    [tt('Қазір ашық', 'Сейчас открыто'), String(r.open)],
    [tt('Kaspi арқылы', 'Через Kaspi'), money(r.revenueKaspi)],
    [tt('Кассада', 'На кассе'), money(r.revenueCounter)],
    [tt('Қайтарылды', 'Возвращено'), money(r.refunded)],
    [tt('Ақ-қара беттер', 'Ч/б страниц'), String(r.pagesBw)],
    [tt('Түрлі түсті беттер', 'Цветных страниц'), String(r.pagesColor)],
    [tt('Парақ', 'Листов'), String(r.sheets)],
    [tt('Кезектен дайынға дейін (медиана)', 'От очереди до готовности (медиана)'), r.waitMedianMin == null ? '—' : tt(`${r.waitMedianMin} мин`, `${r.waitMedianMin} мин`)],
  ] : [];
  return (
    <section className="section print-report" aria-labelledby="prH">
      <div className="field"><label htmlFor="prDay" id="prH">{tt('Күн', 'День')}</label>
        <input className="input num" id="prDay" type="date" value={day || r?.day || ''} onChange={e => setDay(e.target.value)} /></div>
      <ErrorLine err={q.err} />
      {!r ? (q.err ? null : <Loading />) : (
        <div className="group">
          {rows.map(([k, v]) => <div className="row" key={k}><span className="row-main"><span className="row-title">{k}</span></span><span className="row-end num">{v}</span></div>)}
        </div>
      )}
      {r && r.refundsManual > 0 ? <p className="small urgent">{tt(`Қолмен қайтару күтілуде: ${r.refundsManual}`, `Ждут ручного возврата: ${r.refundsManual}`)}</p> : null}
      <p className="tiny muted">{tt('Сомалар — берілген тапсырыстар бойынша; Kaspi ақшасы полиграфияның өз шотына түседі.', 'Суммы — по выданным заказам; деньги Kaspi приходят на счёт самой полиграфии.')}</p>
    </section>
  );
}
