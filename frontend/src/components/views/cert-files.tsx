'use client';
/* «Сертификаттар» on the portfolio screen: certificate files (PDF, JPG, PNG) next to the course / event / manual
   certificates the app already knows. Signed in (and the portfolio switch on): files go to the server, each can be made
   public for the public portfolio page /p/<slug>. Signed out: the file stays on this device (IndexedDB) with a note that
   signing in uploads it and makes sharing possible. Cards: thumbnail (photo, or a PDF tile with the size), open in the
   in-app viewer (PDF in an iframe with open / download fallbacks, photo in a lightbox), edit, delete, public toggle. */
import './cert-files.css';
import { useEffect, useRef, useState, useSyncExternalStore, type ReactNode } from 'react';
import { useApp } from '@/components/app';
import { ConfirmSheet, Empty, Icon, Row, SectionHead } from '@/components/ui';
import { ErrorLine, errText, hasAccount, Loading, useFeature, useNeedAccount } from '@/components/views/common';
import { useAccount } from '@/lib/account';
import {
  CERT_ACCEPT, CERT_CATEGORIES, CERT_LIMITS, categoryLabel, cleanCertMeta, downloadName, fmtBytes, problemText,
  type CertKind, type CertList, type CertMeta, type FileProblem, type ServerCert,
} from '@/lib/certfiles';
import { certsApi, deviceCerts, prepareFile, useDeviceCerts, type DeviceCert, type PickedFile } from '@/lib/certstore';
import { certificates } from '@/lib/domain';
import { parseYmd } from '@/lib/format';

/* ---------- the signed-in list, shared by the section and the sheets ---------- */
let cache: { uid: number; data: CertList | null; err: string } | null = null;
let loading = 0;
const subs = new Set<() => void>();
const emit = () => subs.forEach(l => l());
function fetchList(uid: number, lang: 'kk' | 'ru' | 'en') {
  if (loading === uid) return;
  loading = uid;
  certsApi.list(lang).then(
    d => { cache = { uid, data: d, err: '' }; },
    e => { cache = { uid, data: cache?.uid === uid ? cache.data : null, err: errText(e) }; },
  ).finally(() => { loading = 0; emit(); });
}
function patchList(uid: number, fn: (d: CertList) => CertList) {
  if (cache?.uid === uid && cache.data) { cache = { ...cache, data: fn(cache.data) }; emit(); }
}
const withUsage = (d: CertList, list: ServerCert[]): CertList => ({
  ...d, certificates: list, usage: { ...d.usage, files: list.length, bytes: list.reduce((n, c) => n + c.bytes, 0) },
});

/** where new certificates go: the account (signed in + switch on) or this device */
function useCertMode() {
  const acc = useAccount();
  const on = useFeature('portfolio');
  const uid = on && hasAccount(acc) ? acc.user!.id : 0;
  return { uid, loaded: acc.loaded, signedIn: hasAccount(acc), on };
}

function useServerCerts(uid: number) {
  const { lang } = useApp();
  const snap = useSyncExternalStore(l => { subs.add(l); return () => { subs.delete(l); }; }, () => cache, () => null);
  useEffect(() => { if (uid && cache?.uid !== uid) fetchList(uid, lang); }, [uid, lang]);
  const cur = uid && snap?.uid === uid ? snap : null;
  return { data: cur?.data ?? null, err: cur?.err ?? '', reload: () => { if (uid) fetchList(uid, lang); } };
}

/** an object URL for a blob, loaded lazily and revoked when the card goes away */
function useBlobUrl(load: (() => Promise<Blob>) | null, key: string): { url: string; err: boolean } {
  const [state, setState] = useState<{ key: string; url: string; err: boolean }>({ key: '', url: '', err: false });
  useEffect(() => {
    if (!load) return;
    let url = '';
    let alive = true;
    load().then(b => {
      url = URL.createObjectURL(b);
      if (alive) setState({ key, url, err: false }); else URL.revokeObjectURL(url);
    }, () => { if (alive) setState({ key, url: '', err: true }); });
    return () => { alive = false; if (url) URL.revokeObjectURL(url); };
    // eslint-disable-next-line react-hooks/exhaustive-deps -- reload only when the file changes (key)
  }, [key]);
  return state.key === key ? { url: state.url, err: state.err } : { url: '', err: false };
}

const fmtIssued = (iso: string | null, fDateY: (d: Date) => string) => (iso ? fDateY(parseYmd(iso)) : '');

/* ---------- the section ---------- */

export function CertificatesSection() {
  const { S, I, tt, openSheet, update } = useApp();
  const mode = useCertMode();
  const device = useDeviceCerts();
  const server = useServerCerts(mode.uid);
  const known = certificates(S, I);
  const files = mode.uid ? server.data?.certificates ?? [] : [];
  const add = () => openSheet(<CertFormSheet />);
  const waiting = mode.uid ? !server.data && !server.err : !device.ready;
  const nothing = !files.length && !device.list.length && !known.length;
  return (
    <section className="section" aria-labelledby="certsHead">
      <SectionHead title={<span id="certsHead">{tt('Сертификаттар', 'Сертификаты')}</span>}
        more={<button className="link-btn" type="button" onClick={add}>+ {tt('Қосу', 'Добавить')}</button>} />
      {mode.uid && server.err ? <ErrorLine err={server.err} /> : null}
      {waiting && !known.length ? <Loading /> : null}
      {!waiting && nothing ? (
        <div className="group">
          <Empty icon="file" action={<button className="btn secondary" type="button" onClick={add}><Icon n="plus" s={18} /> {tt('Сертификат қосу', 'Добавить сертификат')}</button>}>
            {tt('Сертификатыңызды PDF не сурет ретінде қосыңыз', 'Добавьте сертификат как PDF или фото')}
          </Empty>
        </div>
      ) : null}
      {files.length || device.list.length ? (
        <ul className="cf-list" aria-label={tt('Сертификат файлдары', 'Файлы сертификатов')}>
          {files.map(c => <ServerCard key={'s' + c.id} c={c} uid={mode.uid} pageVisible={!!server.data?.pageVisible} />)}
          {device.list.map(c => <DeviceCard key={'d' + c.id} c={c} />)}
        </ul>
      ) : null}
      {mode.uid && device.list.length ? <UploadDeviceCard list={device.list} uid={mode.uid} /> : null}
      {!mode.uid && mode.loaded ? <DeviceNote signedIn={mode.signedIn} on={mode.on} durable={device.durable} /> : null}
      {mode.uid && server.data ? (
        <p className="tiny muted num">{tt(`Орын: ${fmtBytes(server.data.usage.bytes)} / ${fmtBytes(server.data.usage.maxBytes)} · файл: ${server.data.usage.files} / ${server.data.usage.maxFiles}`,
          `Место: ${fmtBytes(server.data.usage.bytes)} / ${fmtBytes(server.data.usage.maxBytes)} · файлов: ${server.data.usage.files} / ${server.data.usage.maxFiles}`)}</p>
      ) : null}
      {known.length ? (
        <div className="group">{known.map((c, k) => (
          <Row key={k} icon="award" tone="sun" title={c.title} sub={`${c.org} · ${I.fDateY(c.date)}`} chev={false}
            end={c.src === 'manual' ? <button className="icon-btn" type="button" style={{ color: 'var(--muted)' }} aria-label={tt('Жою', 'Удалить')} onClick={() => update(s => { s.certs.splice(c.i ?? -1, 1); })}><Icon n="trash" s={18} /></button> : null} />
        ))}</div>
      ) : null}
    </section>
  );
}

function DeviceNote({ signedIn, on, durable }: { signedIn: boolean; on: boolean; durable: boolean }) {
  const { tt } = useApp();
  const need = useNeedAccount();
  return (
    <div className="cf-note" role="note">
      <Icon n="shield" s={18} />
      <div>
        <p className="small">{tt('Файлдар тек осы құрылғыда сақталады, ешқайда жіберілмейді.', 'Файлы хранятся только на этом устройстве и никуда не отправляются.')}</p>
        {on && !signedIn ? (
          <p className="small">{tt('Аккаунтқа кірсеңіз, оларды аккаунтқа жүктеп, жария портфолиода бөлісе аласыз.', 'Войдите в аккаунт — сможете загрузить их в аккаунт и делиться ими в публичном портфолио.')}{' '}
            <button className="link-btn" type="button" onClick={need}>{tt('Кіру', 'Войти')}</button></p>
        ) : null}
        {!durable ? <p className="tiny urgent">{tt('Бұл браузерде файлдарды сақтау мүмкін емес (жеке режим?).', 'Этот браузер не даёт сохранять файлы (приватный режим?).')}</p> : null}
      </div>
    </div>
  );
}

/** signed in with files still on the device: upload them (then they leave the device) */
function UploadDeviceCard({ list, uid }: { list: DeviceCert[]; uid: number }) {
  const { tt, lang, toast } = useApp();
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  const go = async () => {
    setBusy(true); setErr('');
    let done = 0;
    for (const c of [...list].reverse()) {
      try {
        const r = await certsApi.upload(metaOf(c), c.blob, downloadName(c.title, c.kind, c.mime), lang);
        await deviceCerts.remove(c.id);
        patchList(uid, d => withUsage(d, [r.certificate, ...d.certificates]));
        done++;
      } catch (e) { setErr(errText(e)); break; }
    }
    setBusy(false);
    if (done) toast(tt(`Аккаунтқа жүктелді: ${done}`, `Загружено в аккаунт: ${done}`));
  };
  return (
    <div className="card">
      <p className="small">{tt(`Осы құрылғыда ${list.length} сертификат файлы бар. Аккаунтқа жүктесеңіз, басқа құрылғыдан да көресіз және жария портфолиода бөлісе аласыз.`,
        `На этом устройстве ${list.length} файл(ов) сертификатов. Загрузите в аккаунт — они будут видны с других устройств, ими можно поделиться в публичном портфолио.`)}</p>
      <ErrorLine err={err} />
      <button className="btn secondary" type="button" disabled={busy} onClick={go}><Icon n="send" s={18} /> {busy ? tt('Жүктелуде…', 'Загрузка…') : tt('Аккаунтқа жүктеу', 'Загрузить в аккаунт')}</button>
    </div>
  );
}

const metaOf = (c: CertMeta): CertMeta => ({ title: c.title, issuer: c.issuer, issuedOn: c.issuedOn, credentialId: c.credentialId, credentialUrl: c.credentialUrl, category: c.category });

/* ---------- cards ---------- */

function Thumb({ kind, url, bytes, onOpen, title }: { kind: CertKind; url: string; bytes: number; onOpen: () => void; title: string }) {
  const { tt } = useApp();
  return (
    <button className={`cf-thumb${kind === 'pdf' ? ' cf-thumb-pdf' : ''}`} type="button" onClick={onOpen} aria-label={tt(`Ашу: ${title}`, `Открыть: ${title}`)}>
      {kind === 'pdf' ? (
        <><Icon n="file" s={26} /><span className="cf-ext">PDF</span><span className="cf-size num">{fmtBytes(bytes)}</span></>
      ) : url ? (
        // eslint-disable-next-line @next/next/no-img-element -- a blob: URL, nothing for next/image to optimise
        <img src={url} alt="" />
      ) : <Icon n="award" s={24} />}
    </button>
  );
}

function CardBody({ c, extra, actions }: { c: CertMeta & { kind: CertKind; bytes: number }; extra?: ReactNode; actions: ReactNode }) {
  const { tt, L, fDateY } = useApp();
  const date = fmtIssued(c.issuedOn, fDateY);
  return (
    <div className="cf-body">
      <span className="cf-title">{c.title}</span>
      <span className="cf-meta">{[c.issuer, date].filter(Boolean).join(' · ')}</span>
      <span className="cf-tags">
        <span className="badge b-line">{L(categoryLabel(c.category))}</span>
        {c.kind === 'image' ? <span className="badge b-line num">{tt('Сурет', 'Фото')} · {fmtBytes(c.bytes)}</span> : null}
        {extra}
      </span>
      {c.credentialId ? <span className="tiny muted cf-cred">ID: {c.credentialId}</span> : null}
      {c.credentialUrl ? <a className="tiny cf-cred" href={c.credentialUrl} target="_blank" rel="noopener nofollow">{tt('Тексеру сілтемесі', 'Ссылка для проверки')}</a> : null}
      <div className="cf-actions">{actions}</div>
    </div>
  );
}

function ServerCard({ c, uid, pageVisible }: { c: ServerCert; uid: number; pageVisible: boolean }) {
  const { tt, lang, toast, openSheet } = useApp();
  const thumb = useBlobUrl(c.kind === 'image' ? () => certsApi.file(c, lang) : null, c.url);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  const toggleId = `cfPub${c.id}`;
  const open = () => openSheet(<CertViewer title={c.title} kind={c.kind} mime={c.mime} load={() => certsApi.file(c, lang)} />);
  const setPublic = async (v: boolean) => {
    setBusy(true); setErr('');
    const put = (cert: ServerCert) => patchList(uid, d => ({ ...d, certificates: d.certificates.map(x => (x.id === c.id ? cert : x)) }));
    put({ ...c, public: v }); // optimistic: the switch moves at once, and back if the server refuses
    try {
      put((await certsApi.patch(c.id, { public: v }, lang)).certificate);
      toast(v ? tt('Жария бетте көрсетіледі', 'Показывается на публичной странице') : tt('Енді тек сізге көрінеді', 'Теперь виден только вам'));
    } catch (e) { put(c); setErr(errText(e)); } finally { setBusy(false); }
  };
  const remove = () => openSheet(<ConfirmSheet msg={tt('Сертификатты және оның файлын өшіреміз бе?', 'Удалить сертификат и его файл?')} yes={async () => {
    try {
      await certsApi.remove(c.id, lang);
      patchList(uid, d => withUsage(d, d.certificates.filter(x => x.id !== c.id)));
      toast(tt('Өшірілді', 'Удалено'));
    } catch (e) { toast(errText(e)); }
  }} />);
  return (
    <li className="cf-card">
      <Thumb kind={c.kind} url={thumb.url} bytes={c.bytes} onOpen={open} title={c.title} />
      <CardBody c={c}
        extra={<span className={`badge ${c.public ? 'b-ok' : 'b-line'}`}>{c.public ? tt('Жария', 'Публичный') : tt('Жеке', 'Личный')}</span>}
        actions={<>
          <button className="chip" type="button" onClick={open}><Icon n="ext" s={16} /> {tt('Ашу', 'Открыть')}</button>
          <button className="chip" type="button" onClick={() => openSheet(<CertFormSheet edit={{ where: 'server', cert: c }} />)}><Icon n="edit" s={16} /> {tt('Өңдеу', 'Изменить')}</button>
          <button className="chip" type="button" onClick={remove}><Icon n="trash" s={16} /> {tt('Жою', 'Удалить')}</button>
        </>} />
      <div className="cf-public">
        <label className="check" htmlFor={toggleId}>
          <input id={toggleId} type="checkbox" checked={c.public} disabled={busy} onChange={e => void setPublic(e.target.checked)} />
          <span className="small">{tt('Жария портфолиода көрсету', 'Показывать в публичном портфолио')}</span>
        </label>
        {c.public && !pageVisible ? <p className="tiny muted">{tt('Жария бет (не «Сертификаттар» блогы) өшірулі: файлды әзірге ешкім көрмейді.', 'Публичная страница (или блок «Сертификаты») выключена: файл пока никто не видит.')}</p> : null}
        <ErrorLine err={err} />
      </div>
    </li>
  );
}

function DeviceCard({ c }: { c: DeviceCert }) {
  const { tt, toast, openSheet } = useApp();
  const thumb = useBlobUrl(c.kind === 'image' ? () => Promise.resolve(c.blob) : null, c.id);
  const open = () => openSheet(<CertViewer title={c.title} kind={c.kind} mime={c.mime} load={() => Promise.resolve(c.blob)} />);
  const remove = () => openSheet(<ConfirmSheet msg={tt('Сертификатты осы құрылғыдан өшіреміз бе?', 'Удалить сертификат с этого устройства?')} yes={async () => {
    try { await deviceCerts.remove(c.id); toast(tt('Өшірілді', 'Удалено')); } catch (e) { toast(errText(e)); }
  }} />);
  return (
    <li className="cf-card">
      <Thumb kind={c.kind} url={thumb.url} bytes={c.bytes} onOpen={open} title={c.title} />
      <CardBody c={c} extra={<span className="badge b-line">{tt('Құрылғыда', 'На устройстве')}</span>}
        actions={<>
          <button className="chip" type="button" onClick={open}><Icon n="ext" s={16} /> {tt('Ашу', 'Открыть')}</button>
          <button className="chip" type="button" onClick={() => openSheet(<CertFormSheet edit={{ where: 'device', cert: c }} />)}><Icon n="edit" s={16} /> {tt('Өңдеу', 'Изменить')}</button>
          <button className="chip" type="button" onClick={remove}><Icon n="trash" s={16} /> {tt('Жою', 'Удалить')}</button>
        </>} />
    </li>
  );
}

/* ---------- viewer ---------- */

export function CertViewer({ title, kind, mime, load }: { title: string; kind: CertKind; mime: string; load: () => Promise<Blob> }) {
  const { tt, closeSheet } = useApp();
  const { url, err } = useBlobUrl(load, title + kind + mime);
  const [zoom, setZoom] = useState(false);
  const name = downloadName(title, kind, mime);
  return (
    <>
      <h2 className="cf-view-title">{title}</h2>
      {err ? <p className="small urgent" role="alert">{tt('Файл ашылмады. Интернетті тексеріп, қайталаңыз.', 'Файл не открылся. Проверьте интернет и повторите.')}</p> : null}
      {!url && !err ? <Loading /> : null}
      {url && kind === 'pdf' ? (
        <iframe className="cf-pdf" src={url} title={tt(`PDF: ${title}`, `PDF: ${title}`)} />
      ) : null}
      {url && kind === 'image' ? (
        <button type="button" className={`cf-lightbox${zoom ? ' zoom' : ''}`} onClick={() => setZoom(z => !z)}
          aria-label={zoom ? tt('Кішірейту', 'Уменьшить') : tt('Үлкейту', 'Увеличить')} aria-pressed={zoom}>
          {/* eslint-disable-next-line @next/next/no-img-element -- a blob: URL */}
          <img src={url} alt={title} />
        </button>
      ) : null}
      {url ? (
        <>
          {kind === 'pdf' ? <p className="tiny muted">{tt('PDF көрінбесе, жаңа бетте ашыңыз не жүктеп алыңыз.', 'Если PDF не отображается, откройте его в новой вкладке или скачайте.')}</p> : null}
          <div className="btn-row">
            <a className="btn ghost" href={url} target="_blank" rel="noopener"><Icon n="ext" s={18} /> {tt('Жаңа бетте ашу', 'Открыть в новой вкладке')}</a>
            <a className="btn secondary" href={url} download={name}><Icon n="file" s={18} /> {tt('Жүктеп алу', 'Скачать')}</a>
          </div>
        </>
      ) : null}
      <button className="btn ghost block" type="button" onClick={closeSheet}>{tt('Жабу', 'Закрыть')}</button>
    </>
  );
}

/* ---------- add / edit ---------- */

type Edit = { where: 'server'; cert: ServerCert } | { where: 'device'; cert: DeviceCert };

const EMPTY: CertMeta = { title: '', issuer: '', issuedOn: null, credentialId: '', credentialUrl: '', category: 'course' };

export function CertFormSheet({ edit }: { edit?: Edit }) {
  const { tt, L, lang, toast, closeSheet } = useApp();
  const mode = useCertMode();
  const [f, setF] = useState<CertMeta>(edit ? metaOf(edit.cert) : EMPTY);
  const [picked, setPicked] = useState<PickedFile | null>(null);
  const [preparing, setPreparing] = useState(false);
  const [fileErr, setFileErr] = useState('');
  const [err, setErr] = useState('');
  const [busy, setBusy] = useState(false);
  const fileRef = useRef<HTMLInputElement>(null);
  const camRef = useRef<HTMLInputElement>(null);
  const [touch] = useState(() => typeof window !== 'undefined' && window.matchMedia?.('(pointer: coarse)').matches);
  const preview = useBlobUrl(picked && picked.kind === 'image' ? () => Promise.resolve(picked.blob) : null, picked ? picked.name + picked.bytes : '');
  const toServer = edit ? edit.where === 'server' : !!mode.uid;
  const set = <K extends keyof CertMeta>(k: K, v: CertMeta[K]) => setF(x => ({ ...x, [k]: v }));

  const pick = async (list: FileList | null) => {
    const file = list?.[0];
    if (!file) return;
    setFileErr(''); setPreparing(true);
    try {
      const p = await prepareFile(file);
      setPicked(p);
      if (!f.title.trim()) set('title', file.name.replace(/\.[a-z0-9]{2,5}$/i, '').replace(/[_-]+/g, ' ').trim().slice(0, CERT_LIMITS.title));
    } catch (e) {
      setPicked(null);
      setFileErr(problemText(((e as { problem?: FileProblem }).problem) || 'type', tt));
    } finally {
      setPreparing(false);
      if (fileRef.current) fileRef.current.value = '';
      if (camRef.current) camRef.current.value = '';
    }
  };

  const submit = async () => {
    setErr('');
    const { meta, problem } = cleanCertMeta(f);
    if (problem) {
      setErr({
        title: tt('Атауын жазыңыз (120 таңбаға дейін).', 'Укажите название (до 120 символов).'),
        issuer: tt('Ұйым атауы тым ұзын.', 'Слишком длинное название организации.'),
        credentialId: tt('ID тым ұзын.', 'Слишком длинный ID.'),
        credentialUrl: tt('Сілтеме https:// деп басталуы керек.', 'Ссылка должна начинаться с https://.'),
        issuedOn: tt('Күні дұрыс емес.', 'Неверная дата.'),
      }[problem]);
      return;
    }
    if (!edit && !picked) { setFileErr(tt('Сертификат файлын таңдаңыз.', 'Выберите файл сертификата.')); fileRef.current?.focus(); return; }
    setBusy(true);
    try {
      if (edit?.where === 'server') {
        const r = await certsApi.patch(edit.cert.id, meta, lang);
        patchList(mode.uid, d => ({ ...d, certificates: d.certificates.map(x => (x.id === r.certificate.id ? r.certificate : x)) }));
      } else if (edit?.where === 'device') {
        await deviceCerts.update(edit.cert.id, meta);
      } else if (toServer && picked) {
        const r = await certsApi.upload(meta, picked.blob, downloadName(meta.title, picked.kind, picked.mime), lang);
        patchList(mode.uid, d => withUsage(d, [r.certificate, ...d.certificates]));
      } else if (picked) {
        await deviceCerts.add(meta, picked);
      }
      toast(edit ? tt('Сақталды', 'Сохранено') : tt('Сертификат қосылды', 'Сертификат добавлен'));
      closeSheet();
    } catch (e) {
      setErr(errText(e, tt('Сақтау мүмкін болмады', 'Не удалось сохранить')));
    } finally { setBusy(false); }
  };

  return (
    <>
      <h2>{edit ? tt('Сертификатты өңдеу', 'Изменить сертификат') : tt('Сертификат қосу', 'Добавить сертификат')}</h2>
      <form className="form" noValidate onSubmit={e => { e.preventDefault(); if (!busy && !preparing) void submit(); }}>
        {!edit ? (
          <div className="field">
            <span className="lbl" id="cfFileLbl">{tt('Файл (PDF не сурет)', 'Файл (PDF или фото)')}</span>
            <div className="cf-pick">
              <label className="cf-drop" htmlFor="cfFile">
                <Icon n="file" s={22} />
                <span>{preparing ? tt('Дайындалуда…', 'Подготовка…') : picked ? tt('Басқа файл таңдау', 'Выбрать другой файл') : tt('Файл таңдау', 'Выбрать файл')}</span>
                <input ref={fileRef} id="cfFile" type="file" accept={CERT_ACCEPT} className="cf-file-input"
                  aria-describedby="cfFileHint cfFileErr" onChange={e => void pick(e.target.files)} />
              </label>
              {touch ? (
                <label className="cf-drop" htmlFor="cfCam">
                  <Icon n="plus" s={22} />
                  <span>{tt('Камерамен түсіру', 'Снять камерой')}</span>
                  <input ref={camRef} id="cfCam" type="file" accept="image/*" capture="environment" className="cf-file-input" onChange={e => void pick(e.target.files)} />
                </label>
              ) : null}
            </div>
            {picked ? (
              <div className="cf-picked" aria-live="polite">
                {picked.kind === 'image' && preview.url
                  // eslint-disable-next-line @next/next/no-img-element -- a blob: URL
                  ? <img src={preview.url} alt="" />
                  : <span className="cf-thumb cf-thumb-pdf cf-mini"><Icon n="file" s={20} /><span className="cf-ext">PDF</span></span>}
                <span className="small"><b>{picked.name}</b><br /><span className="muted num">{picked.kind === 'pdf' ? 'PDF' : tt('Сурет', 'Фото')} · {fmtBytes(picked.bytes)}{picked.width ? ` · ${picked.width}×${picked.height}` : ''}</span></span>
              </div>
            ) : null}
            <span className="tiny muted" id="cfFileHint">{tt('PDF — 10 МБ-қа дейін, JPG / PNG — 8 МБ-қа дейін. Сурет 1600 px-ке дейін кішірейтіледі, GPS деректері өшіріледі.', 'PDF — до 10 МБ, JPG / PNG — до 8 МБ. Фото уменьшается до 1600 px, данные GPS удаляются.')}</span>
            <span className="field-err" id="cfFileErr" role={fileErr ? 'alert' : undefined}>{fileErr}</span>
          </div>
        ) : null}
        <div className="field"><label htmlFor="cfTitle">{tt('Атауы', 'Название')}</label>
          <input className="input" id="cfTitle" maxLength={CERT_LIMITS.title} value={f.title} onChange={e => set('title', e.target.value)} required
            placeholder={tt('мысалы, IELTS Academic 7.0', 'например, IELTS Academic 7.0')} /></div>
        <div className="field"><label htmlFor="cfIssuer">{tt('Берген ұйым', 'Кем выдан')}</label>
          <input className="input" id="cfIssuer" maxLength={CERT_LIMITS.issuer} value={f.issuer} onChange={e => set('issuer', e.target.value)} /></div>
        <div className="grid-2">
          <div className="field"><label htmlFor="cfDate">{tt('Берілген күні', 'Дата выдачи')}</label>
            <input className="input" id="cfDate" type="date" value={f.issuedOn ?? ''} onChange={e => set('issuedOn', e.target.value || null)} /></div>
          <div className="field"><label htmlFor="cfCat">{tt('Санаты', 'Категория')}</label>
            <select className="input" id="cfCat" value={f.category} onChange={e => set('category', e.target.value as CertMeta['category'])}>
              {CERT_CATEGORIES.map(([k, l]) => <option key={k} value={k}>{L(l)}</option>)}
            </select></div>
        </div>
        <div className="field"><label htmlFor="cfCredId">{tt('Сертификат ID (міндетті емес)', 'ID сертификата (необязательно)')}</label>
          <input className="input" id="cfCredId" maxLength={CERT_LIMITS.credentialId} value={f.credentialId} onChange={e => set('credentialId', e.target.value)} /></div>
        <div className="field"><label htmlFor="cfCredUrl">{tt('Тексеру сілтемесі (міндетті емес)', 'Ссылка для проверки (необязательно)')}</label>
          <input className="input" id="cfCredUrl" type="url" inputMode="url" maxLength={CERT_LIMITS.credentialUrl} value={f.credentialUrl} onChange={e => set('credentialUrl', e.target.value)} placeholder="https://" /></div>
        <p className="tiny muted">{toServer
          ? tt('Файл аккаунтыңызға жүктеледі және әдепкіде тек сізге көрінеді. Жария портфолиода көрсетуді кейін өзіңіз қосасыз.', 'Файл загрузится в аккаунт и по умолчанию виден только вам. Показ в публичном портфолио включаете вы сами.')
          : tt('Файл тек осы құрылғыда сақталады. Аккаунтқа кірсеңіз, оны жүктеп, жария портфолиода бөлісе аласыз.', 'Файл сохранится только на этом устройстве. Войдите в аккаунт, чтобы загрузить его и делиться в публичном портфолио.')}</p>
        <ErrorLine err={err} />
        <button className="btn" type="submit" disabled={busy || preparing}>{busy ? tt('Сақталуда…', 'Сохранение…') : edit ? tt('Сақтау', 'Сохранить') : tt('Қосу', 'Добавить')}</button>
      </form>
    </>
  );
}
