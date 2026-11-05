// pdf.js ships no types for its worker module; components/views/print-preview.tsx loads it into the page's own thread
declare module 'pdfjs-dist/legacy/build/pdf.worker.mjs';
