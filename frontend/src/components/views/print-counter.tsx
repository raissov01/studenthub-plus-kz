'use client';
/* The print shop's counter (docs/print.md «Беру» and «Басу реті»): a tiny room, hundreds of jobs a day, one operator.
   - Search: type the code the student says, or scan the QR on their screen (camera via BarcodeDetector where the
     browser has it; a USB scanner types into the same box) → the job, the first name to ask, pages and the first
     file to check against the top of the stack → «Берілді» (the server checks the code / QR matches).
   - Print plan: the ticked jobs in queue order = the order they come out of the printer. Each job (each run of the
     same paper/colour/duplex) is its own print-ready PDF, so the printer's «Offset / job separation» shifts every
     job's stack at no paper cost; optional slips (four per A4) or full covers, by the shop's setting.
   Flat: solid fills, 1px borders. */
import { useEffect, useRef, useState } from 'react';
import { useApp } from '@/components/app';
import { Icon, Qr } from '@/components/ui';
import { ErrorLine, errText, hhmm } from '@/components/views/common';
import { api } from '@/lib/account';
import {
  batchOrder, lineDetail, manualKind, money, parseCounterInput, paymentLabel, segmentLabel, segments, slipSheets, stackSummary, statusLabel,
  workable, type Job, type Separator,
} from '@/lib/print';

const nameOf = (x: { nameKk: string; nameRu: string }, lang: string) => (lang !== 'kk' ? x.nameRu : x.nameKk);

/* ---------- the camera scanner ---------- */

interface Detector { detect(src: CanvasImageSource): Promise<{ rawValue: string }[]> }
type DetectorCtor = new (o: { formats: string[] }) => Detector;
const detectorOf = () => (typeof window !== 'undefined' ? (window as unknown as { BarcodeDetector?: DetectorCtor }).BarcodeDetector : undefined);
export const canScan = () => !!detectorOf() && typeof navigator !== 'undefined' && !!navigator.mediaDevices?.getUserMedia;

function Scanner({ onCode, onClose }: { onCode: (raw: string) => void; onClose: () => void }) {
  const { tt } = useApp();
  const video = useRef<HTMLVideoElement>(null);
  const [err, setErr] = useState('');
  const done = useRef(onCode);
  useEffect(() => { done.current = onCode; });
  useEffect(() => {
    const Ctor = detectorOf();
    if (!Ctor || !navigator.mediaDevices?.getUserMedia) return;
    let stream: MediaStream | null = null;
    let stop = false;
    const det = new Ctor({ formats: ['qr_code'] });
    navigator.mediaDevices.getUserMedia({ video: { facingMode: 'environment' }, audio: false }).then(s => {
      if (stop) { s.getTracks().forEach(t => t.stop()); return; }
      stream = s;
      const v = video.current;
      if (!v) return;
      v.srcObject = s;
      void v.play().catch(() => { /* the frame loop still runs once it starts */ });
      const tick = async () => {
        if (stop) return;
        try {
          if (v.readyState >= 2) {
            const raw = (await det.detect(v))[0]?.rawValue?.trim();
            if (raw && parseCounterInput(raw)) { stop = true; done.current(raw); return; }
          }
        } catch { /* a frame that failed to decode */ }
        setTimeout(tick, 300);
      };
      void tick();
    }, () => setErr(tt('Камераға рұқсат берілмеді. Кодты қолмен жазыңыз.', 'Нет доступа к камере. Введите код вручную.')));
    return () => { stop = true; stream?.getTracks().forEach(t => t.stop()); };
  }, [tt]);
  return (
    <div className="print-scan">
      {err ? <p className="small urgent">{err}</p> : <video ref={video} className="scan-video" muted playsInline aria-label={tt('Камера', 'Камера')} />}
      <button className="btn ghost" type="button" onClick={onClose}>{tt('Камераны жабу', 'Закрыть камеру')}</button>
    </div>
  );
}

/* ---------- the counter search ---------- */

/** the big search box at the top of the board: code or QR → the job → «Берілді» */
export function CounterSearch({ shopId, onChanged }: { shopId: number; onChanged: () => void }) {
  const { tt, lang } = useApp();
  const [q, setQ] = useState('');
  const [found, setFound] = useState<{ jobs: Job[]; proof: { token: string } | { code: string } } | null>(null);
  const [err, setErr] = useState('');
  const [scan, setScan] = useState(false);
  const [busy, setBusy] = useState(false);
  const [camera, setCamera] = useState(false);
  useEffect(() => { const t = setTimeout(() => setCamera(canScan()), 0); return () => clearTimeout(t); }, []);
  const input = useRef<HTMLInputElement>(null);
  const find = async (raw: string) => {
    const proof = parseCounterInput(raw);
    setErr(''); setFound(null);
    if (!proof) { setErr(tt('Кодты (3–4 сан) жазыңыз не QR-ды сканерлеңіз.', 'Введите код (3–4 цифры) или отсканируйте QR.')); return; }
    setBusy(true);
    try {
      const v = 'token' in proof ? proof.token : proof.code;
      const r = await api<{ jobs: Job[] }>(`/api/print/staff/lookup?shop=${shopId}&q=${encodeURIComponent(v)}`, { lang });
      if (!r.jobs.length) setErr(tt(`№${'code' in proof ? proof.code : ''} табылмады. Кодты қайта сұраңыз.`, `№${'code' in proof ? proof.code : ''} не найден. Спросите код ещё раз.`));
      setFound({ jobs: r.jobs, proof });
    } catch (e) { setErr(errText(e)); } finally { setBusy(false); }
  };
  const clear = () => { setQ(''); setFound(null); setErr(''); input.current?.focus(); };
  return (
    <section className="card print-counter" aria-labelledby="pcSearchH">
      <h3 id="pcSearchH" className="cafe-col-h">{tt('Беру: код не QR', 'Выдача: код или QR')}</h3>
      <form className="print-counter-form" onSubmit={e => { e.preventDefault(); void find(q); }}>
        <label className="sr-only" htmlFor="pcSearch">{tt('Алу коды', 'Код получения')}</label>
        <input ref={input} className="input num print-counter-input" id="pcSearch" inputMode="numeric" autoComplete="off" autoCapitalize="none" spellCheck={false}
          placeholder="№ 4821" value={q} maxLength={80} onChange={e => setQ(e.target.value)} />
        <button className="btn" type="submit" disabled={busy || !q.trim()}>{tt('Табу', 'Найти')}</button>
        {camera ? <button className="btn ghost" type="button" aria-pressed={scan} onClick={() => setScan(!scan)}><Icon n="qr" s={18} /> {tt('QR', 'QR')}</button> : null}
      </form>
      {scan ? <Scanner onClose={() => setScan(false)} onCode={raw => { setScan(false); setQ(raw); void find(raw); }} /> : null}
      <ErrorLine err={err} />
      {found?.jobs.map(j => <Handover key={j.id} j={j} proof={found.proof} onDone={() => { clear(); onChanged(); }} />)}
    </section>
  );
}

/** one found job: what to check against the stack, and the handover */
function Handover({ j, proof, onDone }: { j: Job; proof: { token: string } | { code: string }; onDone: () => void }) {
  const { tt, lang, toast } = useApp();
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  const s = stackSummary(j);
  const goods = j.lines.filter(l => l.kind === 'goods' || l.kind === 'service');
  const owes = j.paymentMethod === 'pickup' && j.paymentStatus !== 'paid';
  const give = async () => {
    setBusy(true); setErr('');
    try {
      await api(`/api/print/staff/jobs/${j.id}/picked-up`, { method: 'POST', body: proof, lang });
      toast(tt(`№${j.pickupCode} берілді`, `№${j.pickupCode} выдан`));
      onDone();
    } catch (e) { setErr(errText(e)); } finally { setBusy(false); }
  };
  return (
    <article className="print-handover" data-status={j.status} aria-label={`№${j.pickupCode}`}>
      <div className="print-handover-top">
        <b className="cafe-ticket-code num">№{j.pickupCode}</b>
        <span className={`badge ${j.status === 'ready' ? 'b-ok' : 'b-line'}`}>{statusLabel(j.status, tt)}</span>
      </div>
      <p className="print-handover-name">{j.customer || tt('Аты жоқ', 'Без имени')}</p>
      <p className="small">{tt('Атын сұраңыз да, стектің үстіңгі бетімен салыстырыңыз:', 'Спросите имя и сверьте с верхним листом стопки:')}</p>
      <ul className="cafe-ticket-items">
        {s.files ? <li><b className="num">{tt(`${s.pages} бет`, `${s.pages} стр.`)}</b>{' · '}{tt(`${s.files} файл`, `файлов: ${s.files}`)}{s.first ? <> · {tt('бірінші:', 'первый:')} <b>{s.first}</b></> : null}</li> : null}
        {goods.map((l, i) => <li key={i}><b className="num">{l.qty}×</b> {nameOf(l, lang)}</li>)}
      </ul>
      {j.note ? <p className="small cafe-note">{j.note}</p> : null}
      <p className={`small ${owes ? 'print-owes' : ''}`}>{owes ? tt(`Алу кезінде төлейді: ${money(j.total)}`, `Оплата при получении: ${money(j.total)}`) : paymentLabel(j, tt)}</p>
      <ErrorLine err={err} />
      {j.status === 'ready' ? <button className="btn block" type="button" disabled={busy} onClick={() => void give()}>{owes ? tt('Төледі — берілді', 'Оплатил — выдан') : tt('Берілді', 'Выдан')}</button>
        : j.status === 'picked_up' ? <p className="small muted">{tt('Бұрын берілген', 'Уже выдан')}{j.closedAt ? ' · ' + hhmm(j.closedAt) : ''}</p>
          : j.status === 'new' || j.status === 'printing' ? <p className="small muted">{tt('Әлі дайын емес — кезекте.', 'Ещё не готов — в очереди.')}</p> : null}
    </article>
  );
}

/** «Берілді» from a ticket: the operator types the code the student says (or scans), the server checks the match */
export function HandoverSheet({ j, onDone }: { j: Job; onDone: () => void }) {
  const { tt, lang, closeSheet } = useApp();
  const [code, setCode] = useState('');
  const [err, setErr] = useState('');
  const [busy, setBusy] = useState(false);
  const [scan, setScan] = useState(false);
  const [camera, setCamera] = useState(false);
  useEffect(() => { const t = setTimeout(() => setCamera(canScan()), 0); return () => clearTimeout(t); }, []);
  const s = stackSummary(j);
  const send = async (raw: string) => {
    const proof = parseCounterInput(raw);
    if (!proof) { setErr(tt('Студент айтқан кодты жазыңыз.', 'Введите код, который назвал студент.')); return; }
    setBusy(true); setErr('');
    try { await api(`/api/print/staff/jobs/${j.id}/picked-up`, { method: 'POST', body: proof, lang }); closeSheet(); onDone(); }
    catch (e) { setErr(errText(e)); } finally { setBusy(false); }
  };
  return (
    <form className="form" onSubmit={e => { e.preventDefault(); void send(code); }}>
      <h2>{tt(`№${j.pickupCode} беру`, `Выдача №${j.pickupCode}`)}</h2>
      <p className="print-handover-name">{j.customer || tt('Аты жоқ', 'Без имени')}</p>
      <p className="small muted">{tt(`${s.pages} бет · бірінші: ${s.first}`, `${s.pages} стр. · первый: ${s.first}`)}</p>
      <div className="field"><label htmlFor="hsCode">{tt('Студент айтқан код', 'Код, который назвал студент')}</label>
        <input className="input num print-counter-input" id="hsCode" inputMode="numeric" autoComplete="off" value={code} maxLength={80} onChange={e => setCode(e.target.value)} autoFocus /></div>
      {scan ? <Scanner onClose={() => setScan(false)} onCode={raw => { setScan(false); void send(raw); }} /> : null}
      <ErrorLine err={err} />
      <div className="btn-row">
        <button className="btn" type="submit" disabled={busy || !code.trim()}>{tt('Берілді', 'Выдан')}</button>
        {camera && !scan ? <button className="btn ghost" type="button" onClick={() => setScan(true)}><Icon n="qr" s={18} /> {tt('QR сканерлеу', 'Сканировать QR')}</button> : null}
      </div>
    </form>
  );
}

/* ---------- the print plan ---------- */

/** a built PDF, saved by its own tap and printed from the computer's PDF viewer (a blob tab would be blocked as a
    popup after the build, and the site's CSP keeps the browser's own PDF plugin off) */
function OpenPdf({ url, pages, name, id }: { url: string; pages: number; name: string; id?: string }) {
  const { tt } = useApp();
  return <a className="btn" href={url} download={name} id={id}>{tt(`PDF жүктеу · ${pages} бет`, `Скачать PDF · ${pages} стр.`)}</a>;
}

/** one segment's button: builds that print-ready PDF in the browser and opens it */
function SegmentButton({ j, index, cover }: { j: Job; index: number; cover: boolean }) {
  const { tt } = useApp();
  const seg = segments(j)[index];
  const [busy, setBusy] = useState(false);
  const [note, setNote] = useState<string[]>([]);
  const [err, setErr] = useState('');
  const [built, setBuilt] = useState<{ url: string; pages: number } | null>(null);
  const onlyManual = seg.lines.every(x => manualKind(x.file?.kind));
  const run = async () => {
    setBusy(true); setErr(''); setNote([]); setBuilt(null);
    try {
      const { buildSegment } = await import('@/lib/print-pdf');
      const b = await buildSegment(j, seg, cover, tt);
      setNote([...b.manual.map(m => tt('Бөлек басыңыз: ', 'Печатать отдельно: ') + m), ...b.failed.map(f => tt(`${f}: браузер аша алмады — түпнұсқаны жүктеп басыңыз`, `${f}: браузер не открыл — скачайте оригинал и печатайте`))]);
      if (b.url) setBuilt({ url: b.url, pages: b.pages });
    } catch (e) { setErr(errText(e, tt('PDF жасалмады', 'PDF не собран'))); } finally { setBusy(false); }
  };
  return (
    <div className="print-seg">
      <button className="btn secondary" type="button" disabled={busy} onClick={() => void run()}>
        <Icon n="file" s={16} /> {busy ? tt('Жиналып жатыр…', 'Собираем…') : onlyManual && !cover ? tt('Тізім', 'Список') : 'PDF'} · {segmentLabel(seg, tt)}
      </button>
      {built ? <OpenPdf url={built.url} pages={built.pages} name={`${j.pickupCode}-${index + 1}-${seg.size}.pdf`} /> : null}
      {seg.lines.filter(x => manualKind(x.file?.kind)).map(x => (
        <span key={x.line.fileId} className="tiny print-manual">{tt('Office файлы — өз бағдарламасынан басыңыз:', 'Файл Office — печатайте из своей программы:')} {x.file?.url && !x.file.deleted ? <a href={x.file.url} rel="noopener" download>{x.file.name}</a> : x.file?.name} · {lineDetail(x.line, tt)}</span>
      ))}
      {note.map(n => <span key={n} className="tiny print-manual">{n}</span>)}
      <ErrorLine err={err} />
    </div>
  );
}

/** the batch: the ticked jobs in the order they will come out of the printer, one print job each */
export function PrintPlan({ jobs, picked, separator, onBulk }: { jobs: Job[]; picked: Set<number>; separator: Separator; onBulk: (action: 'printing' | 'ready', ids: number[]) => Promise<void> }) {
  const { tt } = useApp();
  const list = batchOrder(jobs, picked);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  const [slipPdf, setSlipPdf] = useState<{ url: string; pages: number } | null>(null);
  const slips = async () => {
    setBusy(true); setErr(''); setSlipPdf(null);
    try {
      const { buildSlips } = await import('@/lib/print-pdf');
      const b = await buildSlips(list, tt);
      setSlipPdf({ url: b.url, pages: b.pages });
    } catch (e) { setErr(errText(e, tt('PDF жасалмады', 'PDF не собран'))); } finally { setBusy(false); }
  };
  const bulk = async (a: 'printing' | 'ready') => {
    setBusy(true); setErr('');
    try { await onBulk(a, list.map(j => j.id)); } catch (e) { setErr(errText(e)); } finally { setBusy(false); }
  };
  return (
    <div className="form print-plan">
      <h2>{tt(`Басу реті: ${list.length} тапсырыс`, `Порядок печати: заказов ${list.length}`)}</h2>
      <p className="small muted">{tt('Жоғарыдан төмен басыңыз — науадағы стек осы тізіммен бірдей болады. Әр тапсырыс — бөлек басып шығару тапсырмасы.',
        'Печатайте сверху вниз — стопка в лотке совпадёт со списком. Каждый заказ — отдельное задание печати.')}</p>
      {separator === 'slip' ? (
        <button className="btn ghost" type="button" disabled={busy || !list.length} onClick={() => void slips()}>
          {tt(`Кесетін парақшалар (${slipSheets(list.length)} A4 парақ, 4-тен)`, `Вкладыши для разрезания (${slipSheets(list.length)} листа A4, по 4)`)}
        </button>
      ) : null}
      {slipPdf ? <OpenPdf url={slipPdf.url} pages={slipPdf.pages} name={`slips-${list.map(j => j.pickupCode).join("-").slice(0, 60)}.pdf`} id="printSlipsPdf" /> : null}
      <ol className="print-plan-list">
        {list.map(j => (
          <li key={j.id} className="print-plan-item">
            <span className="print-plan-head"><b className="num">№{j.pickupCode}</b> {j.customer ? <span className="small">{j.customer}</span> : null}
              <span className="small muted num"> · {tt(`${stackSummary(j).pages} бет`, `${stackSummary(j).pages} стр.`)}</span></span>
            {!workable(j) ? <span className="tiny urgent">{tt('Басуға әлі болмайды (баға/төлем)', 'Печатать ещё нельзя (цена/оплата)')}</span> : null}
            {segments(j).map((_, i) => <SegmentButton key={i} j={j} index={i} cover={separator === 'full' && i === 0} />)}
            {!segments(j).length ? <span className="tiny muted">{tt('Файл жоқ: тек тауар', 'Файлов нет: только товары')}</span> : null}
          </li>
        ))}
      </ol>
      <ErrorLine err={err} />
      <div className="btn-row">
        <button className="btn" type="button" disabled={busy || !list.length} onClick={() => void bulk('printing')}>{tt('Барлығы: басып жатырмын', 'Все: печатаю')}</button>
        <button className="btn secondary" type="button" disabled={busy || !list.length} onClick={() => void bulk('ready')}>{tt('Барлығы: дайын', 'Все: готово')}</button>
      </div>
    </div>
  );
}

/* ---------- the zero-paper tip ---------- */

const TIP_KEY = 'print:offsetTip:v1';

/** shown once on the board: the printer's own job separation divides the stacks without spending paper */
export function OffsetTip() {
  const { tt } = useApp();
  const [shown, setShown] = useState(false);
  useEffect(() => {
    const t = setTimeout(() => { try { setShown(localStorage.getItem(TIP_KEY) !== '1'); } catch { setShown(false); } }, 0);
    return () => clearTimeout(t);
  }, []);
  if (!shown) return null;
  return (
    <div className="card print-tip" role="note">
      <b>{tt('Стектерді қағазсыз бөлу', 'Разделять стопки без бумаги')}</b>
      <p className="small">{tt('Принтер драйверінде «Offset / Сдвиг / Job separation» (Konica Minolta: «Сдвиг», «Offset») қосыңыз: әр басып шығару тапсырмасы науада сәл ығысып шығады. Тапсырыстарды «Басу реті» бойынша бір-бірлеп басыңыз — стек тізіммен бірдей болады.',
        'Включите в драйвере принтера «Offset / Сдвиг / Job separation» (Konica Minolta: «Сдвиг», «Offset»): каждое задание выйдет в лоток со сдвигом. Печатайте заказы по одному в «Порядке печати» — стопка совпадёт со списком.')}</p>
      <button className="btn ghost" type="button" onClick={() => { try { localStorage.setItem(TIP_KEY, '1'); } catch { /* private mode */ } setShown(false); }}>{tt('Түсінікті', 'Понятно')}</button>
    </div>
  );
}

/* ---------- the student's counter QR ---------- */

/** on the student's job screen: the QR the operator scans (it holds only the job and code, signed) */
export function PickupQr({ token }: { token: string }) {
  const { tt } = useApp();
  return (
    <div className="print-qr" id="printQr">
      <Qr text={token} />
      <span className="tiny muted">{tt('Кассада осы QR-ды көрсетіңіз не кодты айтыңыз', 'Покажите этот QR на кассе или назовите код')}</span>
    </div>
  );
}
