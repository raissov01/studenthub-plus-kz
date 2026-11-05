'use client';
/* The student's own proof before paying (docs/research/print-competitors.md: «online proof»; «the pages came out
   flipped»): small pictures of the chosen pages, drawn on the student's device from the file they just picked —
   nothing is sent anywhere and the server does no rendering (the API container has a 150 MB cap). pdf.js is loaded
   only when the student taps «Беттерді көру», and runs on the page's own thread (no worker file to serve). */
import { useEffect, useRef, useState } from 'react';
import { useApp } from '@/components/app';
import { rangePages } from '@/lib/print';

const MAX_THUMBS = 8;
const WIDTH = 104; // CSS px

type PdfJs = typeof import('pdfjs-dist/legacy/build/pdf.mjs');
let lib: Promise<PdfJs> | null = null;

function loadPdfJs(): Promise<PdfJs> {
  lib ??= (async () => {
    // the worker's code in this thread: no separate worker script, the CSP stays as it is
    const worker = await import('pdfjs-dist/legacy/build/pdf.worker.mjs');
    (globalThis as unknown as { pdfjsWorker: unknown }).pdfjsWorker = worker;
    return import('pdfjs-dist/legacy/build/pdf.mjs');
  })();
  return lib;
}

interface Thumb { n: number; url: string; landscape: boolean }

/** thumbnails of the chosen pages of a local PDF (or the picture itself) */
export function PagePreview({ file, kind, range }: { file: File; kind: string; range: string }) {
  const { tt } = useApp();
  const [open, setOpen] = useState(false);
  const [thumbs, setThumbs] = useState<Thumb[]>([]);
  const [total, setTotal] = useState(0);
  const [err, setErr] = useState('');
  const urls = useRef<string[]>([]);
  useEffect(() => () => { urls.current.forEach(u => URL.revokeObjectURL(u)); }, []);
  useEffect(() => {
    if (!open) return;
    let live = true;
    const run = async () => {
      if (kind === 'jpg' || kind === 'png') {
        const url = URL.createObjectURL(file);
        urls.current.push(url);
        const img = new Image();
        img.onload = () => { if (live) { setThumbs([{ n: 1, url, landscape: img.naturalWidth > img.naturalHeight }]); setTotal(1); } };
        img.src = url;
        return;
      }
      try {
        const pdfjs = await loadPdfJs();
        const doc = await pdfjs.getDocument({ data: new Uint8Array(await file.arrayBuffer()), isEvalSupported: false }).promise;
        if (!live) return;
        const idx = rangePages(range, doc.numPages) ?? [];
        setTotal(idx.length);
        const out: Thumb[] = [];
        for (const i of idx.slice(0, MAX_THUMBS)) {
          const page = await doc.getPage(i + 1);
          const base = page.getViewport({ scale: 1 });
          const scale = (WIDTH * 2) / base.width;
          const vp = page.getViewport({ scale });
          const canvas = document.createElement('canvas');
          canvas.width = Math.ceil(vp.width); canvas.height = Math.ceil(vp.height);
          const ctx = canvas.getContext('2d');
          if (!ctx) break;
          await page.render({ canvasContext: ctx, viewport: vp }).promise;
          const blob = await new Promise<Blob | null>(res => canvas.toBlob(res, 'image/png'));
          if (!blob || !live) break;
          const url = URL.createObjectURL(blob);
          urls.current.push(url);
          out.push({ n: i + 1, url, landscape: base.width > base.height });
          setThumbs([...out]);
        }
        void doc.destroy();
      } catch {
        if (live) setErr(tt('Бұл файлды алдын ала көрсету мүмкін емес (оператор ашады).', 'Этот файл нельзя показать заранее (оператор откроет).'));
      }
    };
    void run();
    return () => { live = false; };
  }, [open, file, kind, range, tt]);
  if (!open) {
    return <button className="link-btn small print-preview-btn" type="button" onClick={() => setOpen(true)}>{tt('Беттерді көру', 'Посмотреть страницы')}</button>;
  }
  const landscape = thumbs.filter(t => t.landscape).length;
  return (
    <div className="print-preview" aria-label={tt('Беттердің алдын ала көрінісі', 'Предпросмотр страниц')}>
      {err ? <p className="small muted">{err}</p> : !thumbs.length ? <p className="small muted">{tt('Беттер дайындалып жатыр…', 'Готовим страницы…')}</p> : null}
      <ul className="print-thumbs">
        {thumbs.map(t => (
          <li key={t.n} className={t.landscape ? 'is-land' : ''}>
            {/* eslint-disable-next-line @next/next/no-img-element -- a local blob of the student's own page */}
            <img src={t.url} alt={tt(`${t.n}-бет`, `Стр. ${t.n}`)} width={WIDTH} />
            <span className="tiny num">{t.n}</span>
          </li>
        ))}
      </ul>
      {total > thumbs.length && thumbs.length >= MAX_THUMBS ? <p className="tiny muted">{tt(`Тағы ${total - thumbs.length} бет`, `Ещё страниц: ${total - thumbs.length}`)}</p> : null}
      {landscape ? <p className="tiny print-land">{tt(`${landscape} бет көлденең — оператор солай басады. Қате болса, ескертпеге жазыңыз.`, `Альбомных страниц: ${landscape} — оператор так и напечатает. Если это ошибка, напишите в комментарии.`)}</p> : null}
    </div>
  );
}
