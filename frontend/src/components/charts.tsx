'use client';
/* Flat, hand-made SVG charts for the admin screens (ported from the kurs admin dashboard): one series per chart, so no
   legend; thin marks, a hairline grid, the last value labelled, a tooltip on hover and on the arrow keys, and a
   «Кесте» (table) view with the same numbers. Zero is drawn as a tick on the baseline, a missing value as a gap. */
import { useEffect, useRef, useState, type KeyboardEvent, type PointerEvent, type ReactNode } from 'react';
import { useApp } from '@/components/app';

export type Point = { label: string; long: string; value: number | null; text: string };

function useWidth<T extends HTMLElement>() {
  const ref = useRef<T>(null);
  const [w, setW] = useState(0);
  useEffect(() => {
    const el = ref.current; if (!el) return;
    const ro = new ResizeObserver(([e]) => setW(Math.floor(e.contentRect.width)));
    ro.observe(el); setW(Math.floor(el.getBoundingClientRect().width));
    return () => ro.disconnect();
  }, []);
  return [ref, w] as const;
}

export function niceMax(v: number) {
  if (v <= 0) return 1;
  const p = 10 ** Math.floor(Math.log10(v)); const n = v / p;
  return (n <= 1 ? 1 : n <= 2 ? 2 : n <= 2.5 ? 2.5 : n <= 5 ? 5 : 10) * p;
}

const H = 200, TOP = 18, BOTTOM = 26;

/** card with a title, a one-line «so what», and a chart/table switch */
export function ChartCard({ title, subtitle, table, children, testId }: { title: string; subtitle: string; table: ReactNode; children: ReactNode; testId?: string }) {
  const { tt } = useApp();
  const [asTable, setAsTable] = useState(false);
  return (
    <section className="card chart-card" data-testid={testId}>
      <div className="chart-head">
        <div className="chart-titles">
          <h3>{title}</h3>
          <p className="small muted">{subtitle}</p>
        </div>
        <div className="chart-switch" role="group" aria-label={title}>
          {[false, true].map(v => (
            <button key={String(v)} type="button" aria-pressed={asTable === v} onClick={() => setAsTable(v)}>
              {v ? tt('Кесте', 'Таблица') : tt('График', 'График')}
            </button>
          ))}
        </div>
      </div>
      {asTable ? table : children}
    </section>
  );
}

export function DataTable({ head, rows }: { head: [string, string]; rows: [string, string][] }) {
  return (
    <div className="chart-table">
      <table>
        <thead><tr><th scope="col">{head[0]}</th><th scope="col" className="num">{head[1]}</th></tr></thead>
        <tbody>{rows.map(([a, b], i) => <tr key={i}><td>{a}</td><td className="num">{b}</td></tr>)}</tbody>
      </table>
    </div>
  );
}

function useActive(n: number) {
  const [active, setActive] = useState<number | null>(null);
  const onKey = (e: KeyboardEvent) => {
    if (e.key === 'ArrowRight') { e.preventDefault(); setActive(a => (a === null ? 0 : Math.min(n - 1, a + 1))); }
    else if (e.key === 'ArrowLeft') { e.preventDefault(); setActive(a => (a === null ? n - 1 : Math.max(0, a - 1))); }
    else if (e.key === 'Escape') setActive(null);
  };
  return { active, setActive, onKey };
}

function Tip({ x, y, w, children }: { x: number; y: number; w: number; children: ReactNode }) {
  const left = Math.max(4, Math.min(w - 164, x - 80));
  return <div role="status" className="chart-tip" style={{ left, top: Math.max(0, y - 58) }}>{children}</div>;
}

/** single-series columns; `fmtTick` formats the y axis (compact numbers) */
export function ColumnChart({ data, label, fmtTick }: { data: Point[]; label: string; fmtTick: (v: number) => string }) {
  const [ref, w] = useWidth<HTMLDivElement>();
  const { active, setActive, onKey } = useActive(data.length);
  const left = 52, right = 8;
  const max = niceMax(Math.max(0, ...data.map(d => d.value ?? 0)));
  const ticks = [0, 0.5, 1].map(f => f * max);
  const plotW = Math.max(0, w - left - right), plotH = H - TOP - BOTTOM;
  const band = data.length ? plotW / data.length : 0;
  const barW = Math.max(2, Math.min(18, band * 0.6));
  const y = (v: number) => TOP + plotH - (v / max) * plotH;
  const last = data.length - 1;
  const every = Math.max(1, Math.ceil(data.length / Math.max(1, Math.floor(plotW / 44))));
  return (
    <div ref={ref} className="chart" tabIndex={0} role="img" aria-label={label}
      onKeyDown={onKey} onBlur={() => setActive(null)} onPointerLeave={() => setActive(null)}>
      {w > 0 && (
        <svg width={w} height={H} aria-hidden className="chart-svg">
          {ticks.map(v => (
            <g key={v}>
              <line x1={left} x2={w - right} y1={y(v)} y2={y(v)} stroke="var(--line)" strokeWidth={1} />
              <text x={left - 8} y={y(v)} dy="0.32em" textAnchor="end" fontSize={12} fill="var(--muted)">{fmtTick(v)}</text>
            </g>
          ))}
          {data.map((d, i) => {
            const c = left + band * i + band / 2;
            const v = d.value ?? 0;
            const top = y(v), h = TOP + plotH - top;
            const r = Math.min(4, h, barW / 2);
            const x0 = c - barW / 2;
            return (
              <g key={i}>
                {h > 0 && (
                  <path d={`M${x0},${TOP + plotH} V${top + r} Q${x0},${top} ${x0 + r},${top} H${x0 + barW - r} Q${x0 + barW},${top} ${x0 + barW},${top + r} V${TOP + plotH} Z`}
                    fill="var(--primary)" opacity={active === null || active === i ? 1 : 0.5} />
                )}
                {d.value === 0 && <rect x={x0} y={TOP + plotH - 1} width={barW} height={2} fill="var(--primary)" />}
                {i === last && v > 0 && <text x={Math.min(c, w - right - 14)} y={top - 6} textAnchor="middle" fontSize={12} fontWeight={700} fill="var(--text)">{fmtTick(v)}</text>}
                {(last - i) % every === 0 && <text x={c} y={H - 8} textAnchor="middle" fontSize={12} fill="var(--muted)">{d.label}</text>}
                <rect x={left + band * i} y={TOP} width={band} height={plotH} fill="transparent" onPointerEnter={() => setActive(i)} onPointerMove={() => setActive(i)} />
              </g>
            );
          })}
        </svg>
      )}
      {active !== null && data[active] && (
        <Tip x={left + band * active + band / 2} y={y(data[active].value ?? 0)} w={w}>
          <span className="block muted">{data[active].long}</span>
          <b className="num">{data[active].text}</b>
        </Tip>
      )}
    </div>
  );
}

/** single-series line on a 0–100% scale; null values leave a gap */
export function LineChart({ data, label }: { data: Point[]; label: string }) {
  const [ref, w] = useWidth<HTMLDivElement>();
  const { active, setActive, onKey } = useActive(data.length);
  const left = 44, right = 40;
  const plotW = Math.max(0, w - left - right), plotH = H - TOP - BOTTOM;
  const step = data.length > 1 ? plotW / (data.length - 1) : 0;
  const x = (i: number) => left + step * i;
  const y = (v: number) => TOP + plotH - (v / 100) * plotH;
  const segs: string[] = [];
  let cur = '';
  data.forEach((d, i) => {
    if (d.value === null) { if (cur) segs.push(cur); cur = ''; return; }
    cur += `${cur ? 'L' : 'M'}${x(i)},${y(d.value)}`;
  });
  if (cur) segs.push(cur);
  const lastIdx = data.map(d => d.value !== null).lastIndexOf(true);
  const every = Math.max(1, Math.ceil(data.length / Math.max(1, Math.floor(plotW / 52))));
  const onMove = (e: PointerEvent<SVGRectElement>) => {
    const r = (e.currentTarget.ownerSVGElement as SVGSVGElement).getBoundingClientRect();
    setActive(Math.max(0, Math.min(data.length - 1, Math.round((e.clientX - r.left - left) / (step || 1)))));
  };
  return (
    <div ref={ref} className="chart" tabIndex={0} role="img" aria-label={label}
      onKeyDown={onKey} onBlur={() => setActive(null)} onPointerLeave={() => setActive(null)}>
      {w > 0 && (
        <svg width={w} height={H} aria-hidden className="chart-svg">
          {[0, 50, 100].map(v => (
            <g key={v}>
              <line x1={left} x2={w - right} y1={y(v)} y2={y(v)} stroke="var(--line)" strokeWidth={1} />
              <text x={left - 8} y={y(v)} dy="0.32em" textAnchor="end" fontSize={12} fill="var(--muted)">{v}%</text>
            </g>
          ))}
          {data.map((d, i) => (data.length - 1 - i) % every === 0 && (
            <text key={i} x={x(i)} y={H - 8} textAnchor="middle" fontSize={12} fill="var(--muted)">{d.label}</text>
          ))}
          {active !== null && <line x1={x(active)} x2={x(active)} y1={TOP} y2={TOP + plotH} stroke="var(--line-strong)" strokeWidth={1} />}
          {segs.map((d, i) => <path key={i} d={d} fill="none" stroke="var(--primary)" strokeWidth={2} strokeLinejoin="round" strokeLinecap="round" />)}
          {data.map((d, i) => d.value !== null && (
            <circle key={i} cx={x(i)} cy={y(d.value)} r={active === i ? 5 : 4} fill="var(--primary)" stroke="var(--surface)" strokeWidth={2} />
          ))}
          {lastIdx >= 0 && <text x={x(lastIdx) + 10} y={y(data[lastIdx].value!)} dy="0.32em" fontSize={12} fontWeight={700} fill="var(--text)">{data[lastIdx].value}%</text>}
          <rect x={left - step / 2} y={TOP} width={plotW + step} height={plotH} fill="transparent" onPointerMove={onMove} onPointerEnter={onMove} />
        </svg>
      )}
      {active !== null && data[active] && (
        <Tip x={x(active)} y={data[active].value === null ? TOP + plotH : y(data[active].value!)} w={w}>
          <span className="block muted">{data[active].long}</span>
          <b className="num">{data[active].text}</b>
        </Tip>
      )}
    </div>
  );
}

/** one ruled strip of figures; a figure with `go` opens the list behind it */
export function KpiStrip({ items }: { items: { label: string; value: string; go?: () => void; tone?: 'warn' }[] }) {
  return (
    <ul className="kpi-strip" data-testid="kpi-strip">
      {items.map(x => (
        <li key={x.label} className={x.tone === 'warn' ? 'is-warn' : ''}>
          {x.go
            ? <button type="button" onClick={x.go}><b className="num">{x.value}</b><span>{x.label}</span></button>
            : <div><b className="num">{x.value}</b><span>{x.label}</span></div>}
        </li>
      ))}
    </ul>
  );
}

/** compact number for an axis: 1200 -> «1,2 мың» / «1,2 тыс.» */
export function compact(v: number, tt: (kk: string, ru: string) => string): string {
  const d = (n: number) => String(+n.toFixed(1)).replace('.', ',');
  if (v >= 1_000_000) return `${d(v / 1_000_000)} ${tt('млн', 'млн')}`;
  if (v >= 1000) return `${d(v / 1000)} ${tt('мың', 'тыс.')}`;
  return String(Math.round(v));
}
