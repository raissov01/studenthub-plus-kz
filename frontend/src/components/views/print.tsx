'use client';
/* «Полиграфия» (docs/print.md): shop list → the shop's price list and the job builder (upload files → options per file →
   the server's price → goods → pickup time → pay with Kaspi straight to the shop's own account or at the counter) →
   the job screen (queue position, status, a new price to approve, pickup code); history; the operator's queue board
   (/print/staff, made for a tablet) with the price list. Every total shown comes from the server. Flat: solid fills,
   1px borders. */
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useApp } from '@/components/app';
import { Empty, Icon, Row, SectionHead, Seg } from '@/components/ui';
import { ErrorLine, Feature, Loading, NotFound, ReasonSheet, SignInCard, errCode, errText, hasAccount, hhmm, useLoad, useNeedAccount } from '@/components/views/common';
import { KaspiPay, beep } from '@/components/views/shop-common';
import { useQueryId } from '@/components/views/subjects';
import { api, apiUpload, useAccount } from '@/lib/account';
import { hoursLabel, tengeToTiyn } from '@/lib/cafe';
import { newKey } from '@/lib/points';
import {
  acceptAttr, blocker, bytesLabel, checkFile, colorAt, defaultOpts, duplexAt, fileError, fixOpts, isActive, isImage, lineDetail, money,
  newArrivals, paymentLabel, priceList, priceStateLabel, quoteBody, rangeOK, shouldPoll, sizesOf, statusLabel, stepIndex, workable,
  type Catalog, type FileOpts, type Goods, type Job, type PaymentMethod, type Quote, type Service, type Shop, type Upload,
} from '@/lib/print';

const nameOf = (x: { nameKk: string; nameRu: string }, lang: string) => (lang !== 'kk' ? x.nameRu : x.nameKk);
const placeOf = (x: { building?: string; placeKk: string; placeRu: string }, lang: string) => [x.building, lang !== 'kk' ? x.placeRu : x.placeKk].filter(Boolean).join(', ');

/** /print, /print?id=<shop>, /print/jobs, /print/jobs?id=<job>, /print/staff */
export function PrintScreen({ prm }: { prm: string }) {
  return <Feature name="print"><PrintRoute prm={prm} /></Feature>;
}

function PrintRoute({ prm }: { prm: string }) {
  const id = Number(useQueryId() || 0);
  if (prm === 'staff') return <PrintBoard />;
  if (prm === 'jobs') return id > 0 ? <JobView id={id} /> : <JobHistory />;
  if (prm) return <NotFound />;
  return id > 0 ? <ShopView id={id} /> : <ShopList />;
}

function JobBadge({ j }: { j: Pick<Job, 'status'> }) {
  const { tt } = useApp();
  const cls = j.status === 'ready' ? 'b-ok' : j.status === 'cancelled' || j.status === 'no_show' ? 'b-bad' : 'b-line';
  return <span className={`badge ${cls}`}>{statusLabel(j.status, tt)}</span>;
}

/* ---------- shop list ---------- */

function ShopList() {
  const { tt, lang, go } = useApp();
  const acc = useAccount();
  const list = useLoad<{ shops: Shop[] }>('/api/print/shops');
  const mine = useLoad<{ jobs: Job[] }>(hasAccount(acc) ? '/api/print/jobs/mine' : null);
  const active = (mine.data?.jobs ?? []).filter(j => isActive(j.status));
  return (
    <div className="cafe print">
      {active.length ? (
        <section className="section">
          <SectionHead title={tt('Менің тапсырыстарым', 'Мои заказы')} />
          <div className="group">
            {active.map(j => (
              <Row key={j.id} go={`print:j${j.id}`} icon="printer" title={<span className="cafe-code-inline num">№{j.pickupCode}</span>}
                sub={`${nameOf({ nameKk: j.shopNameKk, nameRu: j.shopNameRu }, lang)} · ${money(j.total)}`} end={<JobBadge j={j} />} />
            ))}
          </div>
        </section>
      ) : null}
      <section className="section">
        <SectionHead title={tt('Полиграфиялар', 'Полиграфии')}
          more={hasAccount(acc) ? <button className="link-btn" type="button" onClick={() => go('print:jobs')}>{tt('Тарих', 'История')}</button> : undefined} />
        <ErrorLine err={list.err} />
        {!list.data ? <Loading /> : !list.data.shops.length ? <Empty icon="printer">{tt('Әзірге полиграфия қосылмаған.', 'Полиграфии пока не подключены.')}</Empty> : (
          <div className="group">
            {list.data.shops.map(s => (
              <Row key={s.id} go={`print:${s.id}`} icon="printer" tone={s.openNow ? 'nico-2' : ''} title={nameOf(s, lang)}
                sub={`${placeOf(s, lang)}${placeOf(s, lang) ? ' · ' : ''}${hoursLabel(s, lang)}`}
                end={<span className={`badge ${s.openNow ? 'b-ok' : 'b-line'}`}>{s.openNow ? tt('Ашық', 'Открыто') : tt('Жабық', 'Закрыто')}</span>} />
            ))}
          </div>
        )}
      </section>
      <p className="note">{tt('Файлдарды жүктеп, бағасын көріп, кезекке тұрыңыз — дайын болғанда хабар келеді. Төлем полиграфияның өз Kaspi шотына не кассада.',
        'Загрузите файлы, узнайте цену и встаньте в очередь — когда будет готово, придёт уведомление. Оплата — на Kaspi-счёт полиграфии или на кассе.')}</p>
    </div>
  );
}

/* ---------- the shop: price list and the job builder ---------- */

function ShopView({ id }: { id: number }) {
  const { tt, lang } = useApp();
  const c = useLoad<Catalog>(`/api/print/shops/${id}`);
  if (c.code === 'not_found') return <NotFound />;
  if (!c.data) return <><ErrorLine err={c.err} />{c.err ? null : <Loading />}</>;
  const { shop } = c.data;
  const note = lang !== 'kk' ? shop.noteRu : shop.noteKk;
  return (
    <div className="cafe print">
      <div className="card">
        <h2>{nameOf(shop, lang)}</h2>
        {placeOf(shop, lang) ? <p className="small muted">{placeOf(shop, lang)}</p> : null}
        <p className="small"><Icon n="clock" s={16} /> {hoursLabel(shop, lang)}{shop.openNow ? '' : ' · ' + tt('қазір жабық: тапсырыс келесі жұмыс уақытында басылады', 'сейчас закрыто: заказ напечатают в рабочее время')}</p>
        {note ? <p className="small">{note}</p> : null}
      </div>
      <JobBuilder cat={c.data} />
      <PriceList services={c.data.services} goods={c.data.goods} minOrder={shop.minOrder} />
    </div>
  );
}

function PriceList({ services, goods, minOrder }: { services: Service[]; goods: Goods[]; minOrder: number }) {
  const { tt, lang } = useApp();
  return (
    <section className="section" aria-labelledby="printPrices">
      <SectionHead title={<span id="printPrices">{tt('Прайс', 'Прайс')}</span>} />
      <div className="group">
        {services.map(s => (
          <div className="row" key={s.id}>
            <span className="row-main">
              <span className="row-title">{nameOf(s, lang)} {!s.online ? <span className="badge b-line">{tt('кассада', 'на кассе')}</span> : null}</span>
              {(lang !== 'kk' ? s.descRu : s.descKk) ? <span className="row-sub">{lang !== 'kk' ? s.descRu : s.descKk}</span> : null}
              {priceList(s, tt, lang).map(l => <span className="row-sub num" key={l}>{l}</span>)}
            </span>
          </div>
        ))}
        {goods.map(g => (
          <div className="row" key={`g${g.id}`}>
            <span className="row-main"><span className="row-title">{nameOf(g, lang)}</span><span className="row-sub num">{money(g.price)}</span></span>
            {!g.inStock ? <span className="badge b-line">{tt('Жоқ', 'Нет')}</span> : null}
          </div>
        ))}
      </div>
      {minOrder > 0 ? <p className="tiny muted">{tt(`Ең аз тапсырыс: ${money(minOrder)}`, `Минимальный заказ: ${money(minOrder)}`)}</p> : null}
    </section>
  );
}

interface Picked { upload: Upload; opts: FileOpts }

function Stepper({ value, set, min = 0, max, label }: { value: number; set: (n: number) => void; min?: number; max: number; label: string }) {
  const { tt } = useApp();
  return (
    <span className="cafe-qty small" role="group" aria-label={label}>
      <button className="btn ghost" type="button" onClick={() => set(Math.max(min, value - 1))} aria-label={tt('Азайту', 'Меньше')} disabled={value <= min}>−</button>
      <b className="num" aria-live="polite">{value}</b>
      <button className="btn ghost" type="button" onClick={() => set(Math.min(max, value + 1))} aria-label={tt('Көбейту', 'Больше')} disabled={value >= max}>+</button>
    </span>
  );
}

function JobBuilder({ cat }: { cat: Catalog }) {
  const { tt, lang, go, toast } = useApp();
  const need = useNeedAccount();
  const { shop, services, goods } = cat;
  const [files, setFiles] = useState<Picked[]>([]);
  const [uploading, setUploading] = useState(0);
  const [ferr, setFerr] = useState('');
  const [gq, setGq] = useState<Record<number, number>>({});
  const [uq, setUq] = useState<Record<number, number>>({});
  const [quote, setQuote] = useState<Quote | null>(null);
  const [qerr, setQerr] = useState('');
  const [pickup, setPickup] = useState('');
  const [pay, setPay] = useState<PaymentMethod>(shop.payments.includes('kaspi') ? 'kaspi' : 'pickup');
  const [note, setNote] = useState('');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  const key = useRef(newKey());
  const seq = useRef(0);
  const input = useRef<HTMLInputElement>(null);
  const units = services.filter(s => s.kind === 'unit' && s.online);
  const bindings = services.filter(s => s.kind === 'binding' && s.online);
  const lams = services.filter(s => s.kind === 'lamination' && s.online);
  const printSvcs = services.filter(s => s.kind === 'print' && s.online);
  const stock = goods.filter(g => g.inStock);
  const body = useMemo(() => quoteBody(shop.id, files, gq, uq), [shop.id, files, gq, uq]);
  const ready = (body.files.length + body.goods.length + body.services.length) > 0 &&
    files.every(f => rangeOK(f.opts.range) && (f.upload.pages != null || isImage(f.upload.kind) || f.opts.pages > 0));
  const bodyKey = JSON.stringify(body);
  // the server prices every change (after a short pause); the device never shows its own sum
  useEffect(() => {
    if (!ready) return;
    const n = ++seq.current;
    const t = setTimeout(() => {
      void (async () => {
        try {
          const r = await api<{ quote: Quote }>('/api/print/quote', { method: 'POST', body: JSON.parse(bodyKey), lang });
          if (n === seq.current) { setQuote(r.quote); setQerr(''); }
        } catch (e) { if (n === seq.current) { setQuote(null); setQerr(errText(e)); } }
      })();
    }, 350);
    return () => clearTimeout(t);
  }, [bodyKey, ready, lang]);
  const shown = ready ? quote : null;
  const pick = async (list: FileList | null) => {
    if (!list || !need()) return;
    setFerr('');
    let have = files.length;
    for (const f of Array.from(list)) {
      const bad = checkFile(f, shop.accepts, have, cat.limits.maxFiles);
      if (bad) { setFerr(`${f.name}: ${fileError(bad, tt)}`); continue; }
      have++;
      setUploading(n => n + 1);
      try {
        const form = new FormData();
        form.append('file', f, f.name);
        const r = await apiUpload<{ upload: Upload }>('/api/print/uploads', form, lang);
        setFiles(cur => [...cur, { upload: r.upload, opts: fixOpts(defaultOpts(r.upload, services), services, r.upload.kind) }]);
      } catch (e) { setFerr(`${f.name}: ${errText(e)}`); have--; }
      finally { setUploading(n => n - 1); }
    }
    if (input.current) input.current.value = '';
  };
  const setOpts = (i: number, patch: Partial<FileOpts>) =>
    setFiles(cur => cur.map((p, j) => j === i ? { ...p, opts: fixOpts({ ...p.opts, ...patch }, services, p.upload.kind) } : p));
  const remove = async (i: number) => {
    const p = files[i];
    setFiles(cur => cur.filter((_, j) => j !== i));
    try { await api(`/api/print/uploads/${p.upload.id}`, { method: 'DELETE', lang }); } catch { /* removed later by retention */ }
  };
  const submit = async () => {
    if (busy || !need() || !shown) return;
    setBusy(true); setErr('');
    try {
      const r = await api<{ job: Job }>('/api/print/jobs', { method: 'POST', lang, body: { ...body, pickupAt: pickup || null, payment: pay, note: note.trim(), key: key.current } });
      toast(tt('Тапсырыс кезекке қосылды', 'Заказ поставлен в очередь'));
      go(`print:j${r.job.id}`);
    } catch (e) {
      setErr(errText(e));
      if (errCode(e) !== 'network') key.current = newKey();
    } finally { setBusy(false); }
  };
  const nothing = !printSvcs.length && !units.length && !stock.length;
  if (nothing) return null;
  return (
    <section className="section print-build" aria-labelledby="printBuild">
      <SectionHead title={<span id="printBuild">{tt('Тапсырыс беру', 'Оформить заказ')}</span>} />
      {printSvcs.length ? (
        <div className="card print-drop">
          <input ref={input} id="printFile" className="sr-only" type="file" multiple accept={acceptAttr(shop.accepts)} onChange={e => void pick(e.target.files)} />
          <label htmlFor="printFile" className="btn secondary block" aria-disabled={files.length >= cat.limits.maxFiles}>
            <Icon n="plus" s={18} /> {tt('Файл қосу', 'Добавить файл')}
          </label>
          <p className="tiny muted">{tt(`${shop.accepts.map(x => x.toUpperCase()).join(', ')} · 20 МБ-қа дейін · ең көбі ${cat.limits.maxFiles} файл. Файлдар жабық сақталады және ${shop.retentionDays} күннен соң өшіріледі.`,
            `${shop.accepts.map(x => x.toUpperCase()).join(', ')} · до 20 МБ · не больше ${cat.limits.maxFiles} файлов. Файлы хранятся закрыто и удаляются через ${shop.retentionDays} дн.`)}</p>
          {uploading ? <p className="small muted" role="status">{tt('Жүктеліп жатыр…', 'Загружаем…')}</p> : null}
          <ErrorLine err={ferr} />
        </div>
      ) : null}
      {files.map((p, i) => (
        <FileCard key={p.upload.id} p={p} services={printSvcs} bindings={bindings} lams={lams}
          set={patch => setOpts(i, patch)} remove={() => void remove(i)} />
      ))}
      {stock.length || units.length ? (
        <div className="group">
          {[...stock.map(g => ({ id: g.id, name: nameOf(g, lang), sub: money(g.price), map: gq, set: setGq, max: 50, unit: false })),
            ...units.map(s => ({ id: s.id, name: nameOf(s, lang), sub: priceList(s, tt, lang)[0], map: uq, set: setUq, max: 500, unit: true }))].map(x => (
            <div className="row" key={`${x.unit ? 'u' : 'g'}${x.id}`}>
              <span className="row-main"><span className="row-title">{x.name}</span><span className="row-sub num">{x.sub}</span></span>
              <Stepper value={x.map[x.id] ?? 0} max={x.max} label={x.name} set={n => x.set(m => ({ ...m, [x.id]: n }))} />
            </div>
          ))}
        </div>
      ) : null}
      <div className="card print-quote" aria-live="polite" id="printQuote">
        {!ready ? <p className="small muted">{tt('Файл не тауар қосыңыз — бағасын сервер есептейді.', 'Добавьте файл или товар — цену посчитает сервер.')}</p>
          : !shown ? (qerr ? <ErrorLine err={qerr} /> : <p className="small muted">{tt('Баға есептелуде…', 'Считаем цену…')}</p>) : (
            <>
              <div className="group cafe-lines">
                {shown.lines.map((l, i) => (
                  <div className="row" key={i}>
                    <span className="row-main"><span className="row-title">{nameOf(l, lang)}</span>
                      {lineDetail(l, tt) ? <span className="row-sub">{lineDetail(l, tt)}</span> : null}
                      {(l.parts ?? []).length > 1 ? <span className="row-sub">{l.parts!.map(x => `${nameOf(x, lang)} ${money(x.amount)}`).join(' · ')}</span> : null}</span>
                    <span className="row-end num">{money(l.total)}</span>
                  </div>
                ))}
              </div>
              <p className="cafe-total"><span>{tt('Барлығы', 'Итого')}</span><b className="num" id="printTotal">{money(shown.total)}</b></p>
              {shown.review ? <p className="small print-review">{tt('Бет санын өзіңіз жаздыңыз: оператор файлды ашып тексереді. Баға өзгерсе, сізден растау сұралады.',
                'Число страниц указано вами: оператор откроет файл и проверит. Если цена изменится, мы спросим подтверждение.')}</p> : null}
            </>
          )}
      </div>
      <div className="field">
        <label htmlFor="printPickup">{tt('Қашан аласыз', 'Когда заберёте')}</label>
        <select className="input" id="printPickup" value={pickup} onChange={e => setPickup(e.target.value)}>
          <option value="">{tt('Мүмкіндігінше тез', 'Как можно скорее')}</option>
          {cat.slots.map(s => <option key={s.at} value={s.at}>{hhmm(s.at)}</option>)}
        </select>
      </div>
      <fieldset className="field cafe-pay">
        <legend className="lbl">{tt('Төлем', 'Оплата')}</legend>
        {shop.payments.includes('kaspi') ? (
          <label className="cafe-radio"><input type="radio" name="printPay" value="kaspi" checked={pay === 'kaspi'} onChange={() => setPay('kaspi')} />
            <span><b>{tt('Kaspi арқылы', 'Через Kaspi')}</b><span className="tiny muted">{tt('Ақша тікелей полиграфияға түседі', 'Деньги сразу уходят полиграфии')}</span></span></label>
        ) : null}
        {shop.payments.includes('pickup') ? (
          <label className="cafe-radio"><input type="radio" name="printPay" value="pickup" checked={pay === 'pickup'} onChange={() => setPay('pickup')} />
            <span><b>{tt('Алған кезде кассада', 'На кассе при получении')}</b><span className="tiny muted">{tt('Қолма-қол не Kaspi QR', 'Наличными или Kaspi QR')}</span></span></label>
        ) : null}
        {!shop.payments.length ? <p className="small urgent">{tt('Төлем уақытша қолжетімсіз.', 'Оплата временно недоступна.')}</p> : null}
      </fieldset>
      <div className="field">
        <label htmlFor="printNote">{tt('Ескертпе (міндетті емес)', 'Комментарий (необязательно)')}</label>
        <input className="input" id="printNote" maxLength={300} value={note} onChange={e => setNote(e.target.value)} placeholder={tt('Мысалы: степлермен бекіту', 'Например: скрепить степлером')} />
      </div>
      <ErrorLine err={err} />
      <button className="btn block" type="button" id="printSubmit" onClick={() => void submit()} disabled={busy || !shown || !shop.payments.length || uploading > 0}>
        {busy ? tt('Жіберілуде…', 'Отправляем…') : tt('Кезекке тұру', 'Встать в очередь')}{shown ? <> · <span className="num">{money(shown.total)}</span></> : null}
      </button>
    </section>
  );
}

function FileCard({ p, services, bindings, lams, set, remove }: {
  p: Picked; services: Service[]; bindings: Service[]; lams: Service[]; set: (patch: Partial<FileOpts>) => void; remove: () => void;
}) {
  const { tt, lang } = useApp();
  const { upload: u, opts: o } = p;
  const svc = services.find(s => s.id === o.serviceId);
  const sizes = sizesOf(svc);
  const img = isImage(u.kind);
  const fid = `pf${u.id}`;
  const lamsHere = lams.filter(l => l.pricing.sheet?.[o.size]);
  return (
    <article className="card print-file" aria-label={u.name}>
      <div className="print-file-top">
        <span className="row-main"><b className="row-title print-fname">{u.name}</b>
          <span className="row-sub">{u.kind.toUpperCase()} · {bytesLabel(u.bytes, lang)}{u.pages != null ? ' · ' + tt(`${u.pages} бет`, `${u.pages} стр.`) : ''}</span></span>
        <button className="icon-btn" type="button" onClick={remove} aria-label={tt(`${u.name}: алып тастау`, `${u.name}: убрать`)}><Icon n="trash" s={18} /></button>
      </div>
      {u.pages == null && !img ? (
        <div className="field">
          <label htmlFor={`${fid}pages`}>{tt('Файлдағы бет саны', 'Страниц в файле')}</label>
          <input className="input num" id={`${fid}pages`} type="number" min={1} max={2000} inputMode="numeric" value={o.pages || ''} required
            onChange={e => set({ pages: Math.max(0, Math.min(2000, Number(e.target.value) || 0)) })} />
          <span className="tiny muted">{u.pagesHint ? tt(`Файлдың өз дерегі бойынша: ${u.pagesHint}. `, `По данным файла: ${u.pagesHint}. `) : ''}{tt('Оператор тексереді.', 'Оператор проверит.')}</span>
        </div>
      ) : null}
      {services.length > 1 ? (
        <div className="field"><label htmlFor={`${fid}svc`}>{tt('Қызмет', 'Услуга')}</label>
          <select className="input" id={`${fid}svc`} value={o.serviceId} onChange={e => set({ serviceId: Number(e.target.value) })}>
            {services.map(s => <option key={s.id} value={s.id}>{nameOf(s, lang)}</option>)}
          </select></div>
      ) : null}
      <fieldset className="cafe-opt"><legend className="lbl">{tt('Қағаз өлшемі', 'Формат бумаги')}</legend>
        <div className="cafe-choices">{sizes.map(s => <button key={s} type="button" className="chip" aria-pressed={o.size === s} onClick={() => set({ size: s })}>{s}</button>)}</div>
      </fieldset>
      <fieldset className="cafe-opt"><legend className="lbl">{tt('Түсі', 'Цвет')}</legend>
        <div className="cafe-choices">
          <button type="button" className="chip" aria-pressed={!o.color} onClick={() => set({ color: false })}>{tt('Ақ-қара', 'Ч/б')}</button>
          <button type="button" className="chip" aria-pressed={o.color} disabled={!colorAt(svc, o.size)} onClick={() => set({ color: true })}>{tt('Түрлі түсті', 'Цветная')}</button>
        </div>
      </fieldset>
      {!img && duplexAt(svc, o.size, o.color) ? (
        <label className="cafe-switch"><input type="checkbox" checked={o.duplex} onChange={e => set({ duplex: e.target.checked })} /><span>{tt('Екі жақты', 'Двусторонняя')}</span></label>
      ) : null}
      <div className="print-2col">
        <div className="field"><span className="lbl" id={`${fid}cl`}>{tt('Дана', 'Копий')}</span>
          <Stepper value={o.copies} min={1} max={100} label={tt('Дана саны', 'Количество копий')} set={n => set({ copies: n })} /></div>
        {!img ? (
          <div className="field"><label htmlFor={`${fid}range`}>{tt('Беттер (бос — барлығы)', 'Страницы (пусто — все)')}</label>
            <input className="input num" id={`${fid}range`} value={o.range} placeholder="1-5, 8" maxLength={200} aria-invalid={!rangeOK(o.range)}
              onChange={e => set({ range: e.target.value })} /></div>
        ) : null}
      </div>
      {bindings.length && !img ? (
        <div className="field"><label htmlFor={`${fid}bind`}>{tt('Түптеу', 'Переплёт')}</label>
          <select className="input" id={`${fid}bind`} value={o.bindingId} onChange={e => set({ bindingId: Number(e.target.value) })}>
            <option value={0}>{tt('Жоқ', 'Нет')}</option>
            {bindings.map(s => <option key={s.id} value={s.id}>{nameOf(s, lang)}</option>)}
          </select></div>
      ) : null}
      {lamsHere.length ? (
        <div className="field"><label htmlFor={`${fid}lam`}>{tt('Ламинация', 'Ламинация')}</label>
          <select className="input" id={`${fid}lam`} value={o.laminationId} onChange={e => set({ laminationId: Number(e.target.value) })}>
            <option value={0}>{tt('Жоқ', 'Нет')}</option>
            {lamsHere.map(s => <option key={s.id} value={s.id}>{nameOf(s, lang)}</option>)}
          </select></div>
      ) : null}
    </article>
  );
}

/* ---------- the job ---------- */

function useJobPoll(id: number) {
  const { lang } = useApp();
  const [j, setJ] = useState<Job | null>(null);
  const [err, setErr] = useState('');
  const [code, setCode] = useState('');
  const load = useCallback(async () => {
    try { const r = await api<{ job: Job }>(`/api/print/jobs/${id}`, { lang }); setJ(r.job); setErr(''); }
    catch (e) { setErr(errText(e)); setCode(errCode(e)); }
  }, [id, lang]);
  const live = !j || shouldPoll(j);
  useEffect(() => {
    const first = setTimeout(() => { void load(); }, 0);
    const t = live ? setInterval(() => { if (document.visibilityState === 'visible') void load(); }, 4000) : undefined;
    return () => { clearTimeout(first); clearInterval(t); };
  }, [live, load]);
  return { j, setJ, err, code, reload: load };
}

function JobView({ id }: { id: number }) {
  const { tt, lang, openSheet, closeSheet, toast, go } = useApp();
  const acc = useAccount();
  const { j, setJ, err, code, reload } = useJobPoll(id);
  const [busy, setBusy] = useState(false);
  const [perr, setPerr] = useState('');
  const asked = useRef(false);
  const call = useCallback(async (path: string, body: unknown) => {
    setBusy(true); setPerr('');
    try { const r = await api<{ job: Job }>(`/api/print/jobs/${id}/${path}`, { method: 'POST', body, lang }); setJ(r.job); }
    catch (e) { setPerr(errText(e)); } finally { setBusy(false); }
  }, [id, lang, setJ]);
  // an agreed Kaspi job asks for its QR at once
  useEffect(() => {
    if (!j || asked.current) return;
    if (j.paymentMethod === 'kaspi' && j.status === 'new' && j.priceState === 'final' && j.paymentStatus === 'unpaid' && !j.pay) {
      asked.current = true;
      const t = setTimeout(() => { void call('pay', {}); }, 0);
      return () => clearTimeout(t);
    }
  }, [j, call]);
  if (!acc.loaded) return <Loading />;
  if (!hasAccount(acc)) return <SignInCard text={tt('Тапсырысты көру үшін аккаунтқа кіріңіз.', 'Войдите, чтобы увидеть заказ.')} />;
  if (code === 'not_found') return <NotFound />;
  if (!j) return <><ErrorLine err={err} />{err ? null : <Loading />}</>;
  const step = stepIndex(j.status);
  const cancel = () => openSheet(
    <ReasonSheet title={tt('Тапсырысты болдырмау?', 'Отменить заказ?')} label={tt('Себебі (міндетті емес)', 'Причина (необязательно)')} required={false} danger
      action={tt('Болдырмау', 'Отменить')} onSubmit={async () => {
        const r = await api<{ job: Job }>(`/api/print/jobs/${id}/cancel`, { method: 'POST', lang });
        setJ(r.job); closeSheet(); toast(tt('Тапсырыс болдырылмады', 'Заказ отменён'));
      }} />,
  );
  const needsPay = j.paymentMethod === 'kaspi' && j.status === 'new' && j.priceState === 'final' && j.paymentStatus !== 'paid' && j.paymentStatus !== 'refunded';
  return (
    <div className="cafe print">
      <div className={`card cafe-codecard${j.status === 'ready' ? ' is-ready' : ''}`} role="status" aria-live="polite">
        <span className="tiny muted">{tt('Алу коды', 'Код получения')}</span>
        <b className="cafe-code num" id="printCode">{j.pickupCode}</b>
        <span className="cafe-status"><JobBadge j={j} /></span>
        <span className="small">{nameOf({ nameKk: j.shopNameKk, nameRu: j.shopNameRu }, lang)}{(lang !== 'kk' ? j.placeRu : j.placeKk) ? ', ' + (lang !== 'kk' ? j.placeRu : j.placeKk) : ''} · <b className="num">{j.pickupAt ? hhmm(j.pickupAt) : tt('мүмкіндігінше тез', 'как можно скорее')}</b></span>
        {j.ahead != null && j.status === 'new' ? <span className="small" id="printAhead">{j.ahead === 0 ? tt('Сіз кезекте біріншісіз', 'Вы первый в очереди') : tt(`Алдыңызда ${j.ahead} тапсырыс`, `Перед вами заказов: ${j.ahead}`)}</span> : null}
      </div>
      {step >= 0 ? (
        <ol className="cafe-steps" aria-label={tt('Тапсырыс барысы', 'Ход заказа')}>
          {[tt('Жаңа', 'Новый'), tt('Басып жатыр', 'Печатается'), tt('Дайын', 'Готово'), tt('Алынды', 'Получен')].map((s, i) => (
            <li key={s} className={i <= step ? 'on' : ''} aria-current={i === step ? 'step' : undefined}>{s}</li>
          ))}
        </ol>
      ) : null}
      {j.status === 'new' && j.priceState === 'review' ? (
        <div className="card print-review" role="note"><b>{priceStateLabel('review', tt)}</b>
          <span className="small">{tt('Бет санын өзіңіз жаздыңыз: оператор файлды ашып, бағаны растайды. Баға өзгерсе, осында сұраймыз.', 'Число страниц указано вами: оператор откроет файл и подтвердит цену. Если цена изменится, спросим здесь.')}</span></div>
      ) : null}
      {j.status === 'new' && j.priceState === 'proposed' && j.proposedTotal != null ? (
        <div className="card print-proposal" role="alert" id="printProposal">
          <h2>{tt('Жаңа баға', 'Новая цена')}: <span className="num">{money(j.proposedTotal)}</span></h2>
          <p className="small">{tt(`Бұрын: ${money(j.total)}.`, `Было: ${money(j.total)}.`)}{j.adjustReason ? ' ' + tt('Оператор:', 'Оператор:') + ' ' + j.adjustReason : ''}</p>
          <ErrorLine err={perr} />
          <div className="btn-row">
            <button className="btn" type="button" id="printAccept" disabled={busy} onClick={() => void call('price', { accept: true })}>{tt('Жаңа бағамен келісемін', 'Согласен с новой ценой')}</button>
            <button className="btn ghost" type="button" disabled={busy} onClick={() => void call('price', { accept: false })}>{tt('Бас тарту (болдырмау)', 'Отказаться (отменить)')}</button>
          </div>
        </div>
      ) : null}
      {needsPay ? (
        <div className="card cafe-paycard">
          <h2>{tt('Kaspi арқылы төлеу', 'Оплата через Kaspi')}</h2>
          {j.paymentStatus === 'failed' ? <p className="small urgent">{tt('Төлем өтпеді не уақыты бітті. Қайта көріңіз.', 'Оплата не прошла или истекла. Попробуйте снова.')}</p> : null}
          {j.pay ? <KaspiPay pay={j.pay} /> : <p className="small muted">{busy ? tt('Kaspi сілтемесі жасалып жатыр…', 'Создаём ссылку Kaspi…') : ''}</p>}
          <ErrorLine err={perr} />
          <div className="btn-row">
            {!j.pay || j.paymentStatus === 'failed' ? <button className="btn" type="button" disabled={busy} onClick={() => void call('pay', {})}>{tt('Kaspi QR алу', 'Получить Kaspi QR')}</button> : null}
            <button className="btn ghost" type="button" disabled={busy} onClick={() => void call('pay', { method: 'pickup' })}>{tt('Кассада төлеймін', 'Оплачу на кассе')}</button>
          </div>
          <p className="tiny muted">{tt('Төлем полиграфияның Kaspi шотына тікелей түседі. Бұл бет өзі жаңарады.', 'Оплата идёт прямо на Kaspi-счёт полиграфии. Страница обновится сама.')}</p>
        </div>
      ) : null}
      <div className="card">
        <div className="group cafe-lines">
          {j.lines.map((l, i) => (
            <div className="row" key={i}>
              <span className="row-main"><span className="row-title">{nameOf(l, lang)}</span>
                {lineDetail(l, tt) ? <span className="row-sub">{lineDetail(l, tt)}</span> : null}</span>
              <span className="row-end num">{money(l.total)}</span>
            </div>
          ))}
        </div>
        <p className="cafe-total"><span>{tt('Барлығы', 'Итого')}</span><b className="num">{money(j.total)}</b></p>
        <p className="small muted">{paymentLabel(j, tt)}{j.note ? ' · ' + j.note : ''}</p>
        {j.cancelReason && j.cancelReason !== 'student' ? <p className="small">{j.cancelReason === 'unpaid' ? tt('Төленбегендіктен болдырылмады.', 'Отменён: не оплачен.')
          : j.cancelReason === 'price_rejected' ? tt('Жаңа бағадан бас тарттыңыз.', 'Вы отказались от новой цены.') : j.cancelReason}</p> : null}
        {j.files.length ? (
          <ul className="print-files">
            {j.files.map(f => <li key={f.id} className="small">{f.url && !f.deleted ? <a href={f.url} rel="noopener">{f.name}</a> : <span className="muted">{f.name}{f.deleted ? tt(' — өшірілді', ' — удалён') : ''}</span>}</li>)}
          </ul>
        ) : null}
      </div>
      <ErrorLine err={err} />
      <div className="btn-row">
        {j.status === 'new' ? <button className="btn danger" type="button" onClick={cancel}>{tt('Болдырмау', 'Отменить')}</button> : null}
        <button className="btn ghost" type="button" onClick={() => { void reload(); }}>{tt('Жаңарту', 'Обновить')}</button>
        {!isActive(j.status) ? <button className="btn secondary" type="button" onClick={() => go(`print:${j.shopId}`)}>{tt('Тағы тапсырыс', 'Новый заказ')}</button> : null}
      </div>
    </div>
  );
}

function JobHistory() {
  const { tt, lang } = useApp();
  const acc = useAccount();
  const q = useLoad<{ jobs: Job[] }>(hasAccount(acc) ? '/api/print/jobs/mine' : null);
  if (!acc.loaded) return <Loading />;
  if (!hasAccount(acc)) return <SignInCard text={tt('Тапсырыстар тарихы аккаунтта сақталады.', 'История заказов хранится в аккаунте.')} />;
  return (
    <div className="cafe print">
      <ErrorLine err={q.err} />
      {!q.data ? <Loading /> : !q.data.jobs.length ? <Empty icon="printer">{tt('Әзірге тапсырыс жоқ.', 'Заказов пока нет.')}</Empty> : (
        <div className="group">
          {q.data.jobs.map(j => (
            <Row key={j.id} go={`print:j${j.id}`} icon="printer" title={<span className="num">№{j.pickupCode} · {money(j.total)}</span>}
              sub={`${nameOf({ nameKk: j.shopNameKk, nameRu: j.shopNameRu }, lang)} · ${j.createdAt.slice(0, 10)}`} end={<JobBadge j={j} />} />
          ))}
        </div>
      )}
    </div>
  );
}

/* ---------- the operator's queue board (tablet) ---------- */

interface Board { shopId: number; shops: Shop[]; jobs: Job[] }

function PrintBoard() {
  const { tt, lang, openSheet, closeSheet, toast } = useApp();
  const acc = useAccount();
  const [shop, setShop] = useState(0);
  const [board, setBoard] = useState<Board | null>(null);
  const [err, setErr] = useState('');
  const [code, setCode] = useState('');
  const [tab, setTab] = useState('queue');
  const [col, setCol] = useState('new');
  const [sound, setSound] = useState(false);
  const seen = useRef<Set<number> | null>(null);
  const soundRef = useRef(sound);
  useEffect(() => { soundRef.current = sound; }, [sound]);
  const load = useCallback(async () => {
    try {
      const b = await api<Board>(`/api/print/staff/jobs${shop ? `?shop=${shop}` : ''}`, { lang });
      const fresh = newArrivals(seen.current, b.jobs);
      if (fresh.length) {
        if (soundRef.current) beep();
        try { navigator.vibrate?.(200); } catch { /* not supported */ }
      }
      seen.current = new Set(b.jobs.map(j => j.id));
      setBoard(b); setErr(''); setCode('');
    } catch (e) { setErr(errText(e)); setCode(errCode(e)); }
  }, [shop, lang]);
  const signed = hasAccount(acc);
  useEffect(() => {
    if (!signed) return;
    const first = setTimeout(() => { void load(); }, 0);
    const t = setInterval(() => { if (document.visibilityState === 'visible') void load(); }, 5000);
    return () => { clearTimeout(first); clearInterval(t); };
  }, [signed, load]);
  if (!acc.loaded) return <Loading />;
  if (!signed) return <SignInCard text={tt('Полиграфия қызметкері ретінде кіріңіз.', 'Войдите как сотрудник полиграфии.')} />;
  if (code === 'not_staff' || code === 'forbidden') return <NotFound />;
  if (!board) return <><ErrorLine err={err} />{err ? null : <Loading />}</>;
  const me = board.shops.find(s => s.id === board.shopId);
  const act = async (j: Job, action: string, body?: unknown) => {
    try {
      await api(`/api/print/staff/jobs/${j.id}/${action}`, { method: 'POST', body: body ?? {}, lang });
      await load();
    } catch (e) { toast(errText(e)); }
  };
  const decline = (j: Job) => openSheet(
    <ReasonSheet title={tt(`№${j.pickupCode} бас тарту`, `Отклонить №${j.pickupCode}`)} label={tt('Себебі (студент көреді)', 'Причина (студент увидит)')} required={false} danger
      action={tt('Бас тарту', 'Отклонить')} onSubmit={async reason => { await act(j, 'decline', { reason }); closeSheet(); }} />,
  );
  const reprice = (j: Job) => openSheet(<PriceSheet j={j} onDone={() => { closeSheet(); void load(); }} />);
  const cols: [string, string, Job[]][] = [
    ['new', tt('Жаңа', 'Новые'), board.jobs.filter(j => j.status === 'new')],
    ['printing', tt('Басып жатыр', 'Печатаются'), board.jobs.filter(j => j.status === 'printing')],
    ['ready', tt('Дайын', 'Готовы'), board.jobs.filter(j => j.status === 'ready')],
  ];
  const manual = board.jobs.filter(j => j.refundManual);
  return (
    <div className="cafe cafe-staff print">
      <div className="cafe-staff-head">
        {board.shops.length > 1 ? (
          <select className="input" aria-label={tt('Полиграфия', 'Полиграфия')} value={board.shopId} onChange={e => { seen.current = null; setShop(Number(e.target.value)); }}>
            {board.shops.map(s => <option key={s.id} value={s.id}>{nameOf(s, lang)}</option>)}
          </select>
        ) : <h2>{me ? nameOf(me, lang) : ''}</h2>}
        <button className="btn ghost" type="button" aria-pressed={sound} onClick={() => { if (!sound) beep(); setSound(!sound); }}>
          <Icon n="bell" s={18} /> {sound ? tt('Дыбыс қосулы', 'Звук включён') : tt('Дыбысты қосу', 'Включить звук')}
        </button>
      </div>
      <Seg items={[['queue', tt('Кезек', 'Очередь')], ['prices', tt('Прайс', 'Прайс')]]} cur={tab} onPick={setTab} />
      <ErrorLine err={err} />
      {tab === 'prices' ? <PrintCatalog shopId={board.shopId} role={me?.myRole ?? ''} /> : (
        <>
          {manual.length ? (
            <div className="card cafe-alert" role="alert">
              <b>{tt('Ақшаны қолмен қайтару керек', 'Нужен ручной возврат')}</b>
              {manual.map(j => (
                <div className="btn-row" key={j.id}>
                  <span className="small num">№{j.pickupCode} · {money(j.total)}</span>
                  {me?.myRole === 'manager' ? <button className="btn secondary" type="button" onClick={() => void act(j, 'refunded')}>{tt('Қайтарылды', 'Возвращено')}</button> : null}
                </div>
              ))}
            </div>
          ) : null}
          <div className="cafe-colpick"><Seg items={cols.map(([k, l, list]) => [k, `${l} ${list.length}`])} cur={col} onPick={setCol} /></div>
          <div className="cafe-board">
            {cols.map(([k, label, list]) => (
              <section key={k} className={`cafe-col${col === k ? ' cur' : ''}`} aria-label={label}>
                <h3 className="cafe-col-h">{label} <span className="badge b-line num">{list.length}</span></h3>
                {!list.length ? <p className="small muted">{tt('Бос', 'Пусто')}</p> : list.map(j => (
                  <Ticket key={j.id} j={j} act={act} decline={decline} reprice={reprice} />
                ))}
              </section>
            ))}
          </div>
        </>
      )}
    </div>
  );
}

function Ticket({ j, act, decline, reprice }: { j: Job; act: (j: Job, a: string, body?: unknown) => Promise<void>; decline: (j: Job) => void; reprice: (j: Job) => void }) {
  const { tt, lang } = useApp();
  const ok = workable(j);
  const why = blocker(j, tt);
  return (
    <article className="card cafe-ticket print-ticket" data-status={j.status} data-price={j.priceState}>
      <div className="cafe-ticket-top">
        <b className="cafe-ticket-code num">№{j.pickupCode}</b>
        <span className="num cafe-ticket-time"><Icon n="clock" s={16} /> {j.pickupAt ? hhmm(j.pickupAt) : tt('тез', 'скорее')}</span>
      </div>
      {j.customer ? <span className="small muted">{j.customer}</span> : null}
      <ul className="cafe-ticket-items">
        {j.lines.filter(l => l.kind !== 'minimum').map((l, i) => {
          const f = l.kind === 'file' ? j.files.find(x => x.id === l.fileId) : undefined;
          return (
            <li key={i}>
              {l.kind === 'file' ? (f?.url && !f.deleted ? <a className="print-dl" href={f.url} rel="noopener" download><Icon n="file" s={16} /> {f.name}</a> : <span className="muted">{nameOf(l, lang)}</span>)
                : <><b className="num">{l.qty}×</b> {nameOf(l, lang)}</>}
              {l.kind === 'file' ? <span className="muted"> · {lineDetail(l, tt)}{(l.parts ?? []).slice(1).map(x => ' · ' + nameOf(x, lang)).join('')}</span> : null}
            </li>
          );
        })}
      </ul>
      {j.note ? <p className="small cafe-note">{j.note}</p> : null}
      <span className={`badge ${j.paymentStatus === 'paid' ? 'b-ok' : 'b-line'}`}>{paymentLabel(j, tt)} · {money(j.total)}{j.proposedTotal != null ? ` → ${money(j.proposedTotal)}` : ''}</span>
      {why ? <p className="small print-why">{why}</p> : null}
      <div className="btn-row cafe-ticket-btns">
        {j.status === 'new' && j.priceState === 'review' ? <>
          <button className="btn" type="button" onClick={() => void act(j, 'price', { total: j.total })}>{tt('Бағаны растау', 'Подтвердить цену')}</button>
          <button className="btn ghost" type="button" onClick={() => reprice(j)}>{tt('Бағаны өзгерту', 'Изменить цену')}</button>
        </> : null}
        {j.status === 'new' && j.priceState === 'proposed' ? <button className="btn ghost" type="button" onClick={() => reprice(j)}>{tt('Бағаны өзгерту', 'Изменить цену')}</button> : null}
        {j.status === 'new' && ok ? <>
          <button className="btn" type="button" onClick={() => void act(j, 'printing')}>{tt('Басып жатырмын', 'Печатаю')}</button>
          {j.paymentMethod === 'pickup' ? <button className="btn ghost" type="button" onClick={() => reprice(j)}>{tt('Баға', 'Цена')}</button> : null}
        </> : null}
        {j.status === 'new' ? <button className="btn ghost" type="button" onClick={() => decline(j)}>{tt('Бас тарту', 'Отклонить')}</button> : null}
        {j.status === 'printing' ? <button className="btn" type="button" onClick={() => void act(j, 'ready')}>{tt('Дайын', 'Готово')}</button> : null}
        {j.status === 'ready' ? <>
          <button className="btn" type="button" onClick={() => void act(j, 'picked-up')}>{tt('Берілді', 'Выдан')}</button>
          <button className="btn ghost" type="button" onClick={() => void act(j, 'no-show')}>{tt('Келмеді', 'Не пришёл')}</button>
        </> : null}
      </div>
    </article>
  );
}

function PriceSheet({ j, onDone }: { j: Job; onDone: () => void }) {
  const { tt, lang } = useApp();
  const [total, setTotal] = useState(String(Math.round((j.proposedTotal ?? j.total) / 100)));
  const [reason, setReason] = useState(j.adjustReason ?? '');
  const [err, setErr] = useState('');
  const [busy, setBusy] = useState(false);
  return (
    <form className="form" onSubmit={async e => {
      e.preventDefault();
      const t = tengeToTiyn(total);
      if (!t) { setErr(tt('Соманы бүтін теңгемен жазыңыз.', 'Укажите сумму в целых тенге.')); return; }
      setBusy(true); setErr('');
      try { await api(`/api/print/staff/jobs/${j.id}/price`, { method: 'POST', body: { total: t, reason: reason.trim() }, lang }); onDone(); }
      catch (x) { setErr(errText(x)); } finally { setBusy(false); }
    }}>
      <h2>{tt(`№${j.pickupCode}: баға`, `№${j.pickupCode}: цена`)}</h2>
      <p className="small muted">{tt(`Қазір: ${money(j.total)}. Басқа сома студенттің растауын күтеді.`, `Сейчас: ${money(j.total)}. Другая сумма ждёт подтверждения студента.`)}</p>
      <div className="field"><label htmlFor="ppTotal">{tt('Жаңа сома, ₸', 'Новая сумма, ₸')}</label>
        <input className="input num" id="ppTotal" inputMode="numeric" value={total} onChange={e => setTotal(e.target.value)} required /></div>
      <div className="field"><label htmlFor="ppReason">{tt('Себебі (студент көреді)', 'Причина (студент увидит)')}</label>
        <textarea className="input" id="ppReason" rows={3} maxLength={300} value={reason} onChange={e => setReason(e.target.value)} placeholder={tt('Мысалы: файлда 14 бет', 'Например: в файле 14 страниц')} /></div>
      <ErrorLine err={err} />
      <button className="btn block" type="submit" disabled={busy}>{tt('Жіберу', 'Отправить')}</button>
    </form>
  );
}

/* ---------- price list at the counter: stock for everyone, editing for managers (and admins) ---------- */

export function PrintCatalog({ shopId, role }: { shopId: number; role: string }) {
  const { tt, lang, openSheet, toast } = useApp();
  const q = useLoad<{ services: Service[]; goods: Goods[]; role: string }>(`/api/print/staff/catalog?shop=${shopId}`);
  const r = q.data?.role || role;
  const canEdit = r === 'manager' || r === 'admin';
  const [local, setLocal] = useState<Record<number, boolean>>({});
  const isOn = (g: Goods) => local[g.id] ?? g.inStock;
  const toggle = async (g: Goods) => {
    const next = !isOn(g);
    setLocal(m => ({ ...m, [g.id]: next }));
    try { await api(`/api/print/staff/goods/${g.id}/stock`, { method: 'POST', body: { inStock: next }, lang }); }
    catch (e) { setLocal(m => ({ ...m, [g.id]: !next })); toast(errText(e)); }
  };
  if (!q.data) return <><ErrorLine err={q.err} />{q.err ? null : <Loading />}</>;
  return (
    <section className="section">
      {canEdit ? (
        <div className="btn-row">
          <button className="btn secondary" type="button" onClick={() => openSheet(<ServiceForm shopId={shopId} onDone={q.reload} />)}><Icon n="plus" s={18} /> {tt('Қызмет', 'Услуга')}</button>
          <button className="btn ghost" type="button" onClick={() => openSheet(<GoodsForm shopId={shopId} onDone={q.reload} />)}><Icon n="plus" s={18} /> {tt('Тауар', 'Товар')}</button>
        </div>
      ) : null}
      <div className="group">
        {q.data.services.map(s => (
          <div className="row" key={`s${s.id}`}>
            <span className="row-main"><span className="row-title">{nameOf(s, lang)} {!s.active ? <span className="badge b-line">{tt('өшірулі', 'выкл.')}</span> : !s.online ? <span className="badge b-line">{tt('кассада', 'на кассе')}</span> : null}</span>
              {priceList(s, tt, lang).map(l => <span className="row-sub num" key={l}>{l}</span>)}</span>
            {canEdit ? <button className="icon-btn" type="button" aria-label={tt(`${nameOf(s, lang)}: өзгерту`, `${nameOf(s, lang)}: изменить`)} onClick={() => openSheet(<ServiceForm shopId={shopId} s={s} onDone={q.reload} />)}><Icon n="edit" s={18} /></button> : null}
          </div>
        ))}
        {q.data.goods.filter(g => !g.archived).map(g => (
          <div className="row" key={`g${g.id}`}>
            <span className="row-main"><span className="row-title">{nameOf(g, lang)}</span><span className="row-sub num">{money(g.price)}</span></span>
            {canEdit ? <button className="icon-btn" type="button" aria-label={tt(`${nameOf(g, lang)}: өзгерту`, `${nameOf(g, lang)}: изменить`)} onClick={() => openSheet(<GoodsForm shopId={shopId} g={g} onDone={q.reload} />)}><Icon n="edit" s={18} /></button> : null}
            <label className="cafe-switch">
              <input type="checkbox" checked={isOn(g)} onChange={() => void toggle(g)} aria-label={tt(`${nameOf(g, lang)}: бар`, `${nameOf(g, lang)}: в наличии`)} />
              <span aria-hidden="true">{isOn(g) ? tt('Бар', 'Есть') : tt('Жоқ', 'Нет')}</span>
            </label>
          </div>
        ))}
      </div>
    </section>
  );
}

const PRICING_EXAMPLE: Record<string, string> = {
  print: '{"sizes":{"A4":{"bw":2000,"color":10000,"bwDuplex":1500,"colorDuplex":9000}}}',
  binding: '{"tiers":[{"maxSheets":50,"price":50000},{"maxSheets":150,"price":80000}]}',
  lamination: '{"sheet":{"A4":15000,"A3":25000}}',
  unit: '{"price":5000,"unitKk":"бет","unitRu":"стр."}',
};

function ServiceForm({ shopId, s, onDone }: { shopId: number; s?: Service; onDone: () => void }) {
  const { tt, lang, closeSheet } = useApp();
  const [f, setF] = useState(() => ({
    kind: s?.kind ?? 'print', nameKk: s?.nameKk ?? '', nameRu: s?.nameRu ?? '', descKk: s?.descKk ?? '', descRu: s?.descRu ?? '',
    pricing: s ? JSON.stringify(s.pricing) : PRICING_EXAMPLE.print, online: s?.online ?? true, active: s?.active ?? true,
  }));
  const [err, setErr] = useState('');
  const set = (k: keyof typeof f) => (e: { target: { value: string } }) => setF(x => ({ ...x, [k]: e.target.value }));
  const ok = useMemo(() => { try { const v = JSON.parse(f.pricing); return v && typeof v === 'object' && !Array.isArray(v); } catch { return false; } }, [f.pricing]);
  return (
    <form className="form" onSubmit={async e => {
      e.preventDefault();
      if (!ok) { setErr(tt('Баға ережесі — JSON нысаны (тиынмен).', 'Правила цены — JSON-объект (в тиынах).')); return; }
      const body = { shopId, kind: f.kind, nameKk: f.nameKk.trim(), nameRu: f.nameRu.trim(), descKk: f.descKk.trim(), descRu: f.descRu.trim(),
        pricing: JSON.parse(f.pricing), online: f.online, active: f.active, sort: s?.sort ?? 0 };
      try { await api(s ? `/api/print/staff/services/${s.id}` : '/api/print/staff/services', { method: s ? 'PUT' : 'POST', body, lang }); closeSheet(); onDone(); }
      catch (x) { setErr(errText(x)); }
    }}>
      <h2>{s ? tt('Қызметті өзгерту', 'Изменить услугу') : tt('Жаңа қызмет', 'Новая услуга')}</h2>
      <div className="field"><label htmlFor="psKind">{tt('Түрі', 'Тип')}</label>
        <select className="input" id="psKind" value={f.kind} onChange={e => setF(x => ({ ...x, kind: e.target.value as Service['kind'], pricing: s && s.kind === e.target.value ? x.pricing : PRICING_EXAMPLE[e.target.value] }))}>
          <option value="print">{tt('Басып шығару (бет бойынша)', 'Печать (за страницу)')}</option>
          <option value="binding">{tt('Түптеу (парақ санына қарай)', 'Переплёт (по числу листов)')}</option>
          <option value="lamination">{tt('Ламинация (парақ)', 'Ламинация (за лист)')}</option>
          <option value="unit">{tt('Бірлік бойынша: көшіру, скан, визитка…', 'За единицу: копия, скан, визитки…')}</option>
        </select></div>
      <div className="field"><label htmlFor="psKk">{tt('Атауы (қаз.)', 'Название (каз.)')}</label><input className="input" id="psKk" value={f.nameKk} onChange={set('nameKk')} required maxLength={80} /></div>
      <div className="field"><label htmlFor="psRu">{tt('Атауы (орыс.)', 'Название (рус.)')}</label><input className="input" id="psRu" value={f.nameRu} onChange={set('nameRu')} required maxLength={80} /></div>
      <div className="field"><label htmlFor="psDk">{tt('Сипаттама (қаз.)', 'Описание (каз.)')}</label><input className="input" id="psDk" value={f.descKk} onChange={set('descKk')} maxLength={300} /></div>
      <div className="field"><label htmlFor="psDr">{tt('Сипаттама (орыс.)', 'Описание (рус.)')}</label><input className="input" id="psDr" value={f.descRu} onChange={set('descRu')} maxLength={300} /></div>
      <div className="field"><label htmlFor="psPricing">{tt('Баға ережесі (JSON, тиынмен: 2000 = 20 ₸)', 'Правила цены (JSON, в тиынах: 2000 = 20 ₸)')}</label>
        <textarea className="input mono" id="psPricing" rows={5} value={f.pricing} onChange={set('pricing')} aria-invalid={!ok} /></div>
      <label className="cafe-switch"><input type="checkbox" checked={f.online} onChange={e => setF(x => ({ ...x, online: e.target.checked }))} /><span>{tt('Онлайн тапсырысқа (әйтпесе тек прайста)', 'В онлайн-заказе (иначе только в прайсе)')}</span></label>
      <label className="cafe-switch"><input type="checkbox" checked={f.active} onChange={e => setF(x => ({ ...x, active: e.target.checked }))} /><span>{tt('Белсенді', 'Активна')}</span></label>
      <ErrorLine err={err} />
      <button className="btn block" type="submit">{tt('Сақтау', 'Сохранить')}</button>
    </form>
  );
}

function GoodsForm({ shopId, g, onDone }: { shopId: number; g?: Goods; onDone: () => void }) {
  const { tt, lang, closeSheet } = useApp();
  const [f, setF] = useState(() => ({ nameKk: g?.nameKk ?? '', nameRu: g?.nameRu ?? '', price: g ? String(g.price / 100) : '', archived: !!g?.archived }));
  const [err, setErr] = useState('');
  return (
    <form className="form" onSubmit={async e => {
      e.preventDefault();
      const price = tengeToTiyn(f.price);
      if (!price) { setErr(tt('Баға бүтін теңгемен.', 'Цена — целые тенге.')); return; }
      const body = { shopId, nameKk: f.nameKk.trim(), nameRu: f.nameRu.trim(), price, inStock: g?.inStock ?? true, archived: f.archived, sort: g?.sort ?? 0 };
      try { await api(g ? `/api/print/staff/goods/${g.id}` : '/api/print/staff/goods', { method: g ? 'PUT' : 'POST', body, lang }); closeSheet(); onDone(); }
      catch (x) { setErr(errText(x)); }
    }}>
      <h2>{g ? tt('Тауарды өзгерту', 'Изменить товар') : tt('Жаңа тауар', 'Новый товар')}</h2>
      <div className="field"><label htmlFor="pgKk">{tt('Атауы (қаз.)', 'Название (каз.)')}</label><input className="input" id="pgKk" value={f.nameKk} onChange={e => setF(x => ({ ...x, nameKk: e.target.value }))} required maxLength={80} /></div>
      <div className="field"><label htmlFor="pgRu">{tt('Атауы (орыс.)', 'Название (рус.)')}</label><input className="input" id="pgRu" value={f.nameRu} onChange={e => setF(x => ({ ...x, nameRu: e.target.value }))} required maxLength={80} /></div>
      <div className="field"><label htmlFor="pgPrice">{tt('Бағасы, ₸', 'Цена, ₸')}</label><input className="input num" id="pgPrice" inputMode="numeric" value={f.price} onChange={e => setF(x => ({ ...x, price: e.target.value }))} required /></div>
      {g ? <label className="cafe-switch"><input type="checkbox" checked={f.archived} onChange={e => setF(x => ({ ...x, archived: e.target.checked }))} /><span>{tt('Прайстан алып тастау', 'Убрать из прайса')}</span></label> : null}
      <ErrorLine err={err} />
      <button className="btn block" type="submit">{tt('Сақтау', 'Сохранить')}</button>
    </form>
  );
}
