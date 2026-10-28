'use client';
/* Campus map: illustrated plan with pins, entrances, nearby places, room search, GPS «Мен қайдамын?», pinch/wheel zoom. */
import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react';
import { useApp } from '@/components/app';
import { Chip, Icon, Note, Seg } from '@/components/ui';
import { BUILDINGS, DIR_L, ENTR, GEO, KIND_L, NEAR_CATS, NEARBY, PINS, UNI_SITES } from '@/lib/data/campus';
import { distTxt, entrPx, geoToPx, inPlan, mapSearch, metresBetween, nearOnMapCount, onCampus, walkMin, type Me } from '@/lib/campus';
import { IC } from '@/lib/icons';
import type { Building, NearPlace } from '@/lib/types';

/* viewBox lives outside React so pinch/drag can move it every frame without re-rendering the plan */
let vb: [number, number, number, number] | null = null;
const mapVb = (): [number, number, number, number] => vb || [0, 0, GEO.w, GEO.h];
const vbStr = () => mapVb().map(n => n.toFixed(1)).join(' ');
function setMapVb(box: HTMLElement, v: number[]) {
  const w = Math.min(GEO.w, Math.max(GEO.w / 6, v[2])), h = w * GEO.h / GEO.w;
  const x = Math.min(GEO.w - w, Math.max(0, v[0])), y = Math.min(GEO.h - h, Math.max(0, v[1]));
  vb = w >= GEO.w - 0.5 ? null : [x, y, w, h];
  box.querySelector('svg')?.setAttribute('viewBox', vbStr());
  box.classList.toggle('zoomed', !!vb);
}
function zoomMapAt(box: HTMLElement, cx: number, cy: number, f: number) {
  const svg = box.querySelector('svg');
  if (!svg) return;
  const r = svg.getBoundingClientRect(), v = mapVb();
  const sx = v[0] + (cx - r.left) / r.width * v[2], sy = v[1] + (cy - r.top) / r.height * v[3];
  const w = Math.min(GEO.w, Math.max(GEO.w / 6, v[2] / f)), k = w / v[2];
  setMapVb(box, [sx - (sx - v[0]) * k, sy - (sy - v[1]) * k, w, w * GEO.h / GEO.w]);
}
function bindMapZoom(box: HTMLElement) {
  const pts = new Map<number, { x: number; y: number }>();
  let pan: null | { x: number; y: number; vb: number[] } = null, pinch: number | null = null, moved = false;
  const down = (e: PointerEvent) => {
    pts.set(e.pointerId, { x: e.clientX, y: e.clientY });
    if (pts.size === 1) { pan = { x: e.clientX, y: e.clientY, vb: mapVb().slice() }; moved = false; }
    if (pts.size === 2) { const [a, b] = [...pts.values()]; pinch = Math.hypot(a.x - b.x, a.y - b.y); pan = null; }
  };
  const move = (e: PointerEvent) => {
    if (!pts.has(e.pointerId)) return;
    pts.set(e.pointerId, { x: e.clientX, y: e.clientY });
    if (pts.size === 2 && pinch) {
      const [a, b] = [...pts.values()], d = Math.hypot(a.x - b.x, a.y - b.y);
      zoomMapAt(box, (a.x + b.x) / 2, (a.y + b.y) / 2, d / pinch); pinch = d; moved = true; e.preventDefault();
    } else if (pan && vb) {
      const svg = box.querySelector('svg');
      if (!svg) return;
      const r = svg.getBoundingClientRect(), dx = e.clientX - pan.x, dy = e.clientY - pan.y;
      if (Math.abs(dx) + Math.abs(dy) > 6) moved = true;
      setMapVb(box, [pan.vb[0] - dx / r.width * pan.vb[2], pan.vb[1] - dy / r.height * pan.vb[3], pan.vb[2], pan.vb[3]]);
    }
  };
  const end = (e: PointerEvent) => { pts.delete(e.pointerId); if (pts.size < 2) pinch = null; if (!pts.size) pan = null; };
  const click = (e: MouseEvent) => { if (moved) { e.stopPropagation(); e.preventDefault(); moved = false; } };
  const wheel = (e: WheelEvent) => { e.preventDefault(); zoomMapAt(box, e.clientX, e.clientY, e.deltaY < 0 ? 1.25 : 0.8); };
  const dbl = (e: MouseEvent) => { e.preventDefault(); zoomMapAt(box, e.clientX, e.clientY, 2); };
  box.addEventListener('pointerdown', down);
  box.addEventListener('pointermove', move);
  (['pointerup', 'pointercancel', 'pointerleave'] as const).forEach(t => box.addEventListener(t, end));
  box.addEventListener('click', click, true);
  box.addEventListener('wheel', wheel, { passive: false });
  box.addEventListener('dblclick', dbl);
  return () => {
    box.removeEventListener('pointerdown', down);
    box.removeEventListener('pointermove', move);
    (['pointerup', 'pointercancel', 'pointerleave'] as const).forEach(t => box.removeEventListener(t, end));
    box.removeEventListener('click', click, true);
    box.removeEventListener('wheel', wheel);
    box.removeEventListener('dblclick', dbl);
  };
}

const twoGis = (q: string) => `https://2gis.kz/astana/search/${encodeURIComponent(q)}`;

/* ---------- nearby tab ---------- */
function NearRow({ p }: { p: NearPlace }) {
  const { tt, tp } = useApp();
  const c = NEAR_CATS[p.c], approx = p.src === '2gis' ? '~' : '';
  const where = p.a ? `${p.a}, ` : '';
  const sub = `${where}${approx}${p.d} ${tt('м', 'м')}, ${tt(`${approx}${walkMin(p.d)} мин жаяу`, `${approx}${walkMin(p.d)} мин пешком`)}, ${tp(DIR_L[p.dir])}`;
  const href = p.lat != null ? `${twoGis(p.n)}?m=${p.lon}%2C${p.lat}%2F18` : twoGis(p.n + ' ' + (p.a || ''));
  return (
    <a className="row" href={href} target="_blank" rel="noopener">
      <span className={`nico nico-${Object.keys(NEAR_CATS).indexOf(p.c) % 5}`}><Icon n={c[2]} s={20} /></span>
      <span className="row-main"><span className="row-title">{p.n}</span><span className="row-sub">{sub}</span>
        {p.h || p.note ? <span className="tiny muted">{p.h === '24/7' ? tt('Тәулік бойы', 'Круглосуточно') : p.h || p.note}</span> : null}</span>
      <span className="chev"><Icon n="ext" s={18} /></span>
    </a>
  );
}
function Near() {
  const { ui, tt, L, setUi } = useApp();
  const cats: [string, string][] = [['all', tt('Барлығы', 'Все')], ...Object.entries(NEAR_CATS).map(([k, c]): [string, string] => [k, tt(c[0], c[1])])];
  const list = NEARBY.filter(p => ui.nearCat === 'all' || p.c === ui.nearCat);
  return (
    <>
      <div className="chips">{cats.map(([k, l]) => <Chip key={k} pressed={ui.nearCat === k} onClick={() => setUi({ nearCat: k })}>{l}</Chip>)}</div>
      <div id="nearList" className="section"><div className="group">{list.map((p, i) => <NearRow key={i} p={p} />)}</div></div>
      <Note>{tt('Қашықтық кампус ортасынан түзу сызықпен есептелген. Басқанда 2GIS ашылады. Дереккөз: OpenStreetMap және 2GIS.', 'Расстояние — по прямой от центра кампуса. Нажмите, чтобы открыть 2GIS. Источники: OpenStreetMap и 2GIS.')}</Note>
      <section className="section"><h2>{tt('Университеттің басқа ғимараттары', 'Другие здания университета')}</h2>
        <div className="group">{UNI_SITES.map((u, i) => (
          <a key={i} className="row" href={twoGis('Сейфуллин ' + L(u.a))} target="_blank" rel="noopener"><span className="bnum bnum-other"><Icon n="pin" s={16} /></span><span className="row-main"><span className="row-title">{L(u.n)}</span><span className="row-sub">{L(u.a)}</span></span><span className="chev"><Icon n="ext" s={18} /></span></a>
        ))}</div></section>
    </>
  );
}

/* ---------- campus tab ---------- */
function BCard({ b }: { b: Building }) {
  const { tt, tp, L } = useApp();
  return (
    <div className="card bcard">
      <div className="card-row" style={{ alignItems: 'center' }}><span className={`bnum bnum-${b.kind}`}>{b.n}</span>
        <div className="row-main"><span className="row-title">{L(b.name)}</span><span className="row-sub">{tp(KIND_L[b.kind])}</span></div></div>
      <p className="small">{L(b.has)}</p>
      {b.a ? <a className="small" href={twoGis('Сейфуллин ' + b.a)} target="_blank" rel="noopener"><Icon n="pin" s={14} /> {b.a}</a> : null}
      {ENTR[b.id] ? <p className="tiny"><span className="ent-key"></span> {tt('Кіреберіс картада жасыл көрсеткімен белгіленген (2GIS бойынша).', 'Вход отмечен на карте зелёной стрелкой (по 2GIS).')}</p> : null}
      {b.corp ? <p className="tiny muted">{tt(`Аудитория нөмірі ${b.corp}-ден басталады, мысалы ${b.corp}-214.`, `Номера аудиторий начинаются с ${b.corp}, например ${b.corp}-214.`)}</p> : null}
    </div>
  );
}

export function MapView({ tab }: { tab: 'campus' | 'near' }) {
  const app = useApp();
  const { ui, I, tt, tp, L, setUi, go, toast } = app;
  const tabs = <Seg items={[['campus', tt('Кампус', 'Кампус')], ['near', tt('Жақын маңда', 'Рядом')]]} cur={tab} onPick={v => go('map:' + v, { replace: true })} />;

  const boxRef = useRef<HTMLDivElement>(null);
  const watch = useRef<number | null>(null);
  const [me, setMe] = useState<Me | null>(null);
  const [geoState, setGeoState] = useState<'off' | 'wait' | 'on'>('off');

  const stopGeo = useCallback(() => {
    if (watch.current != null) { try { navigator.geolocation.clearWatch(watch.current); } catch { /* ignore */ } }
    watch.current = null;
    setMe(null);
    setGeoState('off');
  }, []);
  const startGeo = useCallback(() => {
    if (!('geolocation' in navigator)) { toast(tt('Бұл құрылғыда геолокация жоқ', 'На этом устройстве нет геолокации')); return; }
    if (watch.current != null) return;
    setGeoState('wait');
    watch.current = navigator.geolocation.watchPosition(pos => {
      const { latitude: lat, longitude: lon, accuracy } = pos.coords;
      const [x, y] = geoToPx(lat, lon);
      setMe({ lat, lon, x, y, acc: accuracy });
      setGeoState('on');
    }, err => {
      stopGeo();
      toast(err.code === 1 ? tt('Орныңызды көрсету үшін браузерде геолокацияға рұқсат беріңіз', 'Разрешите геолокацию в браузере, чтобы показать ваше место')
        : tt('Орныңызды анықтау мүмкін болмады. Ашық жерде қайталап көріңіз.', 'Не удалось определить место. Попробуйте на открытом месте.'));
    }, { enableHighAccuracy: true, maximumAge: 10000, timeout: 20000 });
  }, [stopGeo, toast, tt]);

  // leaving the map stops GPS, as in the legacy router
  useEffect(() => () => { if (watch.current != null) navigator.geolocation.clearWatch(watch.current); }, []);
  useEffect(() => {
    if (tab !== 'campus') return;
    const box = boxRef.current;
    if (!box) return;
    const off = bindMapZoom(box);
    if (ui.geoAuto) { setUi({ geoAuto: false }); startGeo(); }
    return off;
    // bind once per mount of the campus tab
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [tab]);

  if (tab === 'near') return <>{tabs}<Near /></>;

  const r = mapSearch(ui.mapQ || '', I);
  const hl = r && r.room ? (r.b ? [r.b.id] : []) : r ? r.list.map(b => b.id) : ui.mapSel ? [ui.mapSel] : [];
  const target: Building | null | undefined = r && r.room && r.b ? r.b : r && !r.room && r.list.length === 1 ? r.list[0] : !r && ui.mapSel ? BUILDINGS.find(b => b.id === ui.mapSel) : null;

  const pick = (id: string) => {
    setUi({ mapSel: id, mapQ: '' });
    boxRef.current?.scrollIntoView({ block: 'start', behavior: 'smooth' });
  };

  /* map layers */
  const nearLayer = ui.mapNear ? NEARBY.filter(p => p.lat != null && p.lon != null).map((p, i) => {
    const [x, y] = geoToPx(p.lat!, p.lon!);
    if (!inPlan([x, y])) return null;
    const c = NEAR_CATS[p.c];
    return (
      <a key={i} className={`np np-${p.c}`} href={`${twoGis(p.n)}?m=${p.lon}%2C${p.lat}%2F18`} target="_blank" rel="noopener" aria-label={`${p.n}, ${tt(c[0], c[1])}`}>
        <title>{p.n}</title>
        <circle cx={x.toFixed(0)} cy={y.toFixed(0)} r="21" />
        <svg x={(x - 11).toFixed(0)} y={(y - 11).toFixed(0)} viewBox="0 0 24 24" width="22" height="22" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" dangerouslySetInnerHTML={{ __html: IC[c[2]] }} />
      </a>
    );
  }) : null;
  const entrLayer = Object.entries(ENTR).map(([id, e]) => {
    const [x, y] = geoToPx(e[0], e[1]), [fx, fy] = geoToPx(e[2], e[3]);
    let dx = x - fx, dy = y - fy;
    const L0 = Math.hypot(dx, dy) || 1;
    dx /= L0; dy /= L0;
    const on = hl.includes(id), len = on ? 46 : 34, sx = x - dx * len, sy = y - dy * len;
    const hx = x - dx * 14, hy = y - dy * 14, px = -dy * 10, py = dx * 10;
    return (
      <g key={id} className={`ent${on ? ' on' : ''}${hl.length && !on ? ' dim' : ''}`} aria-hidden="true">
        <line x1={sx.toFixed(0)} y1={sy.toFixed(0)} x2={hx.toFixed(0)} y2={hy.toFixed(0)} />
        <polygon points={`${x.toFixed(0)},${y.toFixed(0)} ${(hx + px).toFixed(0)},${(hy + py).toFixed(0)} ${(hx - px).toFixed(0)},${(hy - py).toFixed(0)}`} />
      </g>
    );
  });
  const pins = BUILDINGS.map(b => {
    const [x, y] = PINS[b.id], on = hl.includes(b.id);
    return (
      <g key={b.id} className={`pin pin-${b.kind}${on ? ' on' : ''}${hl.length && !on ? ' dim' : ''}`} role="button" tabIndex={0} aria-label={`${b.n}. ${L(b.name)}`}
        onClick={() => pick(b.id)} onKeyDown={e => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); pick(b.id); } }}>
        {on ? <circle className="pin-halo" cx={x} cy={y} r="52" /> : null}<circle cx={x} cy={y} r="32" /><text x={x} y={y + 11}>{b.n}</text>
      </g>
    );
  });
  let geoLayer: ReactNode = null;
  if (me && onCampus(me)) {
    const tp2 = target && (entrPx(target.id) || PINS[target.id]);
    geoLayer = (
      <>
        {tp2 ? <line className="me-route" x1={me.x.toFixed(0)} y1={me.y.toFixed(0)} x2={tp2[0]} y2={tp2[1]} /> : null}
        <circle className="me-acc" cx={me.x.toFixed(0)} cy={me.y.toFixed(0)} r={Math.min(400, Math.max(24, me.acc * GEO.pxPerM)).toFixed(0)} />
        <circle className="me-dot" cx={me.x.toFixed(0)} cy={me.y.toFixed(0)} r="17" />
      </>
    );
  }

  /* status line under the tools */
  let status: ReactNode = null;
  if (geoState === 'wait') status = tt('Орныңыз анықталып жатыр…', 'Определяем ваше место…');
  else if (me && !onCampus(me)) {
    const d = distTxt(metresBetween(me.lat, me.lon, 51.18692, 71.41178));
    status = <>{tt(`Сіз кампустан ${d} қашықтықтасыз.`, `Вы в ${d} от кампуса.`)} <a href={twoGis('Сейфуллин университет')} target="_blank" rel="noopener">{tt('2GIS-те бағыт', 'Маршрут в 2GIS')}</a></>;
  } else if (me && target) {
    const tp2 = entrPx(target.id) || PINS[target.id], m = Math.hypot(tp2[0] - me.x, tp2[1] - me.y) / GEO.pxPerM;
    const door = ENTR[target.id] ? tt(' кіреберісіне', ' (до входа)') : '';
    const walk = tt(`~${walkMin(m)} мин жаяу`, `~${walkMin(m)} мин пешком`);
    status = m < 25 ? tt(`Сіз ${L(target.name)} жанындасыз.`, `Вы рядом: ${L(target.name)}.`)
      : tt(`${L(target.name)}${door} дейін ${distTxt(m)}, ${walk}.`, `До: ${L(target.name)}${door} — ${distTxt(m)}, ${walk}.`);
  } else if (me) {
    let best = BUILDINGS[0], bd = 1e9;
    BUILDINGS.forEach(b => { const d = Math.hypot(PINS[b.id][0] - me.x, PINS[b.id][1] - me.y); if (d < bd) { bd = d; best = b; } });
    status = tt(`Ең жақын нысан: ${best.n}. ${L(best.name)}, ${distTxt(bd / GEO.pxPerM)}. Баратын корпусты таңдаңыз.`, `Ближе всего: ${best.n}. ${L(best.name)}, ${distTxt(bd / GEO.pxPerM)}. Выберите, куда идти.`);
  }

  /* results under the plan */
  let results: ReactNode;
  if (r && r.room) {
    results = r.b ? (
      <div className="card">
        <h2>{tt(`${r.num} аудитория`, `Аудитория ${r.num}`)}</h2>
        <dl className="kv"><dt>{tt('Корпус', 'Корпус')}</dt><dd>{r.b.n}. {L(r.b.name)}</dd><dt>{tt('Қабат', 'Этаж')}</dt><dd>{r.floor}</dd></dl>
        <ol className="steps">
          <li><span>{tt(`Картадағы ${r.b.n}-нысанды табыңыз: ${L(r.b.has).toLowerCase()}`, `Найдите на схеме здание ${r.b.n}: ${L(r.b.has).toLowerCase()}`)}</span></li>
          {r.floor > 1 ? <li><span>{tt(`${r.floor}-қабатқа көтеріліңіз`, `Поднимитесь на ${r.floor} этаж`)}</span></li> : null}
          <li><span>{tt('Дәліздегі көрсеткіштер бойынша аудиторияны табыңыз', 'Найдите аудиторию по указателям в коридоре')}</span></li>
        </ol>
      </div>
    ) : <div className="empty">{tt(`${r.corp}-корпустың нөмірі картаға әлі енгізілмеген. Әзірге белгілісі: 4-корпус (техникалық факультеттің жаңа корпусы).`, `Номер ${r.corp}-го корпуса ещё не внесён. Пока известен 4-й корпус (новый корпус технического факультета).`)}</div>;
  } else if (r) {
    results = r.list.length ? <div className="list">{r.list.map(b => <BCard key={b.id} b={b} />)}</div> : <div className="empty">{tt('Ештеңе табылмады.', 'Ничего не найдено.')}</div>;
  } else {
    const sel = BUILDINGS.find(b => b.id === ui.mapSel);
    results = (
      <>
        {sel ? <BCard b={sel} /> : null}
        <section className="section"><h2>{tt('Барлық нысандар', 'Все здания')}</h2><div className="group">{BUILDINGS.map(b => (
          <button key={b.id} className="row" type="button" onClick={() => pick(b.id)}><span className={`bnum bnum-${b.kind}`}>{b.n}</span><span className="row-main"><span className="row-title">{L(b.name)}</span><span className="row-sub">{L(b.has)}</span></span></button>
        ))}</div></section>
      </>
    );
  }

  const zoomBy = (f: number) => { const box = boxRef.current; if (!box) return; const b = box.getBoundingClientRect(); zoomMapAt(box, b.left + b.width / 2, b.top + b.height / 2, f); };
  return (
    <>
      {tabs}
      <div className="search"><Icon n="search" s={18} /><input className="input" id="mapQ" type="search" placeholder={tt('Аудитория (4-214) не нысан атауы', 'Аудитория (4-214) или название здания')} value={ui.mapQ} onChange={e => setUi({ mapQ: e.target.value, mapSel: null })} autoComplete="off" /></div>
      <div className="chips">{([['study', tt('Оқу корпустары', 'Учебные корпуса')], ['dorm', tt('Жатақханалар', 'Общежития')], ['other', tt('Басқа', 'Другое')]] as const).map(([k, l]) => (
        <button key={k} className={`chip legend lg-${k}`} type="button" onClick={() => setUi({ mapQ: tp(KIND_L[k]), mapSel: null })}><i></i>{l}</button>
      ))}</div>
      <div className="map-tools">
        <button className="btn secondary" type="button" id="geoBtn" aria-pressed={geoState !== 'off'} onClick={() => (geoState !== 'off' ? stopGeo() : startGeo())}>
          <Icon n="pin" s={18} /> {geoState !== 'off' ? tt('Орнымды жасыру', 'Скрыть моё место') : tt('Мен қайдамын?', 'Где я?')}
        </button>
        <button className="chip" type="button" aria-pressed={!!ui.mapNear} onClick={() => setUi({ mapNear: !ui.mapNear })}>{tt(`Жақын орындар (${nearOnMapCount()})`, `Места рядом (${nearOnMapCount()})`)}</button>
      </div>
      <p className="geo-status" id="geoStatus" hidden={!status}>{status}</p>
      <div className="plan-wrap">
        <div className={`plan-box${vb ? ' zoomed' : ''}`} id="planBox" ref={boxRef}>
          <svg className="plan" viewBox={vbStr()} role="group" aria-label={tt('Кампус картасы', 'Карта кампуса')}>
            <image href="/campus-map.jpg" x="0" y="0" width="1672" height="941" />
            <text className="pl-street" x="44" y="470" transform="rotate(-90 44 470)">{tt('Жеңіс даңғылы', 'пр. Победы')}</text>
            <text className="pl-street" x="1628" y="470" transform="rotate(90 1628 470)">{tt('Бейбітшілік көшесі', 'ул. Бейбитшилик')}</text>
            <text className="pl-street" x="836" y="913">{tt('Әлия Молдағұлова көшесі', 'ул. Алии Молдагуловой')}</text>
            {nearLayer}
            {entrLayer}
            {pins}
            <g id="geoLayer">{geoLayer}</g>
          </svg>
        </div>
        <div className="map-ctl">
          <button type="button" aria-label={tt('Үлкейту', 'Приблизить')} onClick={() => zoomBy(1.6)}><Icon n="plus" s={20} /></button>
          <button type="button" aria-label={tt('Кішірейту', 'Отдалить')} onClick={() => zoomBy(1 / 1.6)}><svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round"><path d="M5 12h14" /></svg></button>
          <button type="button" aria-label={tt('Бастапқы көрініс', 'Исходный вид')} onClick={() => { if (boxRef.current) setMapVb(boxRef.current, [0, 0, GEO.w, GEO.h]); }}><svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M4 9V4h5M20 9V4h-5M4 15v5h5M20 15v5h-5" /></svg></button>
        </div>
      </div>
      <p className="tiny muted">{tt('Екі саусақпен үлкейтіп, жылжытуға болады.', 'Карту можно приближать двумя пальцами и двигать.')}</p>
      <div id="mapRes" className="section">{results}</div>
      <Note>{tt('Сызба университеттің жоспары бойынша шамамен салынған, атаулары мен мекенжайлары 2GIS бойынша. Аудитория нөмірінің бірінші саны — корпус, екіншісі — қабат.', 'Схема нарисована по плану университета приблизительно, названия и адреса — по 2GIS. Первая цифра аудитории — корпус, вторая — этаж.')}</Note>
      <a className="btn ghost" href="https://www.google.com/maps/search/?api=1&query=Seifullin+University+Astana" target="_blank" rel="noopener"><Icon n="ext" s={18} /> {tt('Google картасынан ашу', 'Открыть в Google Картах')}</a>
    </>
  );
}
