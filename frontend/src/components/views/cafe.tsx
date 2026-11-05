'use client';
/* «Кофе / Кофейня» (docs/cafe.md): café list → menu with options → cart → pickup slot → pay now with Kaspi (straight to
   the café's own Kaspi account) or at the counter → order status with a big pickup code; order history; the counter's
   order board (/cafe/staff, made for a tablet); the compact home shortcut. Every price shown here is a preview: the
   server recomputes the order from its own menu. Flat: solid fills, 1px borders. */
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useApp } from '@/components/app';
import { Empty, Icon, Row, SectionHead, Seg } from '@/components/ui';
import { ErrorLine, Feature, Loading, NotFound, ReasonSheet, SignInCard, errCode, errText, hasAccount, hhmm, useFeature, useLoad, useNeedAccount } from '@/components/views/common';
import { KaspiPay, beep } from '@/components/views/shop-common';
import { useQueryId } from '@/components/views/subjects';
import { api, useAccount } from '@/lib/account';
import {
  addToCart, cartTotal, defaultOptions, hoursLabel, isActive, loadCart, missingOptions, money, newArrivals, paymentLabel, pickChoice,
  saveCart, setQty, shouldPoll, statusLabel, stepIndex, unitPrice,
  type Cafe as CafeT, type CartLine, type Item, type Menu, type Order, type PaymentMethod,
} from '@/lib/cafe';
import { newKey } from '@/lib/points';

const nameOf = (x: { nameKk: string; nameRu: string }, lang: string) => (lang === 'ru' ? x.nameRu : x.nameKk);

/** /cafe, /cafe?id=<café>, /cafe/orders, /cafe/orders?id=<order>, /cafe/staff */
export function CafeScreen({ prm }: { prm: string }) {
  return <Feature name="cafe"><CafeRoute prm={prm} /></Feature>;
}

function CafeRoute({ prm }: { prm: string }) {
  const id = Number(useQueryId() || 0);
  if (prm === 'staff') return <StaffBoard />;
  if (prm === 'orders') return id > 0 ? <OrderView id={id} /> : <OrderHistory />;
  if (prm) return <NotFound />;
  return id > 0 ? <MenuView id={id} /> : <CafeList />;
}

function OrderBadge({ o }: { o: Pick<Order, 'status'> }) {
  const { tt } = useApp();
  const cls = o.status === 'ready' ? 'b-ok' : o.status === 'cancelled' || o.status === 'no_show' ? 'b-bad' : 'b-line';
  return <span className={`badge ${cls}`}>{statusLabel(o.status, tt)}</span>;
}

/* ---------- home shortcut ---------- */

/** one row on the home screen while FEATURE_CAFE is on; shows the code of an order that is ready */
export function CafeShortcut() {
  const { tt, lang } = useApp();
  const on = useFeature('cafe');
  const acc = useAccount();
  const signed = on && hasAccount(acc);
  const mine = useLoad<{ orders: Order[] }>(signed ? '/api/cafe/orders/mine' : null);
  if (!on) return null;
  const active = mine.data?.orders.find(o => isActive(o.status));
  const sub = active
    ? tt(`№${active.pickupCode} · ${statusLabel(active.status, tt)} · ${hhmm(active.pickupAt)}`, `№${active.pickupCode} · ${statusLabel(active.status, tt)} · ${hhmm(active.pickupAt)}`)
    : tt('Алдын ала тапсырыс беріп, кезексіз алыңыз', 'Закажите заранее и заберите без очереди');
  return (
    <div className="group cafe-short" id="cafeShortcut">
      <Row go={active ? `cafe:o${active.id}` : 'cafe'} icon="cup" tone="nico-3" title={active ? nameOf({ nameKk: active.cafeNameKk, nameRu: active.cafeNameRu }, lang) : tt('Кофе алдын ала', 'Кофе заранее')} sub={sub} />
    </div>
  );
}

/* ---------- café list ---------- */

function CafeList() {
  const { tt, lang, go } = useApp();
  const acc = useAccount();
  const list = useLoad<{ cafes: CafeT[] }>('/api/cafes');
  const mine = useLoad<{ orders: Order[] }>(hasAccount(acc) ? '/api/cafe/orders/mine' : null);
  const active = (mine.data?.orders ?? []).filter(o => isActive(o.status));
  return (
    <div className="cafe">
      {active.length ? (
        <section className="section">
          <SectionHead title={tt('Менің тапсырыстарым', 'Мои заказы')} />
          <div className="group">
            {active.map(o => (
              <Row key={o.id} go={`cafe:o${o.id}`} icon="cup" title={<span className="cafe-code-inline num">№{o.pickupCode}</span>}
                sub={`${nameOf({ nameKk: o.cafeNameKk, nameRu: o.cafeNameRu }, lang)} · ${hhmm(o.pickupAt)}`} end={<OrderBadge o={o} />} />
            ))}
          </div>
        </section>
      ) : null}
      <section className="section">
        <SectionHead title={tt('Кофейнялар', 'Кофейни')}
          more={hasAccount(acc) ? <button className="link-btn" type="button" onClick={() => go('cafe:orders')}>{tt('Тарих', 'История')}</button> : undefined} />
        <ErrorLine err={list.err} />
        {!list.data ? <Loading /> : !list.data.cafes.length ? <Empty icon="cup">{tt('Әзірге кофейня қосылмаған.', 'Кофейни пока не подключены.')}</Empty> : (
          <div className="group">
            {list.data.cafes.map(c => (
              <Row key={c.id} go={`cafe:${c.id}`} icon="cup" tone={c.openNow ? 'nico-2' : ''}
                title={<>{nameOf(c, lang)} {c.isDemo ? <span className="badge b-line b-demo">{tt('үлгі', 'пример')}</span> : null}</>}
                sub={`${[c.building, lang === 'ru' ? c.placeRu : c.placeKk].filter(Boolean).join(', ')}${c.building || c.placeKk ? ' · ' : ''}${hoursLabel(c, lang)}`}
                end={<span className={`badge ${c.openNow ? 'b-ok' : 'b-line'}`}>{c.openNow ? tt('Қабылдап жатыр', 'Принимает') : tt('Жабық', 'Закрыто')}</span>} />
            ))}
          </div>
        )}
      </section>
      <p className="note">{tt('Төлем тікелей кофейняның Kaspi шотына түседі не кассада төлейсіз. Student Hub ақшаны ұстамайды.',
        'Оплата идёт прямо на Kaspi-счёт кофейни или на кассе. Student Hub деньги не держит.')}</p>
    </div>
  );
}

/* ---------- menu, cart, checkout ---------- */

function MenuView({ id }: { id: number }) {
  const { tt, lang, openSheet, toast } = useApp();
  const m = useLoad<Menu>(`/api/cafes/${id}/menu`);
  const [cart, setCart] = useState<CartLine[]>(() => (typeof window === 'undefined' ? [] : loadCart(id)));
  useEffect(() => { saveCart(id, cart); }, [id, cart]);
  if (m.code === 'not_found') return <NotFound />;
  if (!m.data) return <><ErrorLine err={m.err} />{m.err ? null : <Loading />}</>;
  const { cafe, categories, items } = m.data;
  const sum = cartTotal(cart, items);
  const groups: [string, Item[]][] = [
    ...categories.map(c => [nameOf(c, lang), items.filter(i => i.categoryId === c.id)] as [string, Item[]]),
    [tt('Басқа', 'Другое'), items.filter(i => !categories.some(c => c.id === i.categoryId))],
  ].filter(([, list]) => list.length > 0) as [string, Item[]][];
  const add = (it: Item, options: Record<string, string[]>, qty: number) => {
    setCart(c => addToCart(c, it.id, options, qty));
    toast(tt('Себетке қосылды', 'Добавлено в корзину'));
  };
  const quickAdd = (it: Item) => {
    if (!it.options.length) add(it, {}, 1);
    else openSheet(<ItemSheet item={it} onAdd={add} />);
  };
  return (
    <div className="cafe cafe-menu">
      <div className="card">
        <h2>{nameOf(cafe, lang)} {cafe.isDemo ? <span className="badge b-line b-demo">{tt('үлгі', 'пример')}</span> : null}</h2>
        <p className="small muted">{[cafe.building, lang === 'ru' ? cafe.placeRu : cafe.placeKk].filter(Boolean).join(', ')}</p>
        <p className="small"><Icon n="clock" s={16} /> {hoursLabel(cafe, lang)}{cafe.openNow ? '' : ' · ' + tt('қазір тапсырыс қабылдамайды', 'сейчас заказы не принимает')}</p>
      </div>
      {groups.map(([title, list]) => (
        <section className="section" key={title}>
          <SectionHead title={title} />
          <div className="group">
            {list.map(it => (
              <div className="row cafe-item" key={it.id}>
                <span className="row-main">
                  <span className="row-title">{nameOf(it, lang)}</span>
                  {(lang === 'ru' ? it.descRu : it.descKk) ? <span className="row-sub">{lang === 'ru' ? it.descRu : it.descKk}</span> : null}
                  <span className="row-sub num">{it.options.length ? tt('бастап ', 'от ') : ''}{money(it.price)}</span>
                </span>
                {it.available ? (
                  <button className="btn secondary cafe-add" type="button" onClick={() => quickAdd(it)} aria-label={tt(`${nameOf(it, lang)}: себетке қосу`, `${nameOf(it, lang)}: в корзину`)}>
                    <Icon n="plus" s={18} />
                  </button>
                ) : <span className="badge b-line">{tt('Таусылды', 'Закончилось')}</span>}
              </div>
            ))}
          </div>
        </section>
      ))}
      {sum.count > 0 ? (
        <div className="cafe-cartbar">
          <button className="btn block" type="button" id="cafeCart" onClick={() => openSheet(<CheckoutSheet menu={m.data!} cart={cart} setCart={setCart} />)}>
            <Icon n="cup" s={18} /> {tt(`Себет · ${sum.count}`, `Корзина · ${sum.count}`)} · <span className="num">{money(sum.total)}</span>
          </button>
        </div>
      ) : null}
    </div>
  );
}

function ItemSheet({ item, onAdd }: { item: Item; onAdd: (it: Item, options: Record<string, string[]>, qty: number) => void }) {
  const { tt, lang, closeSheet } = useApp();
  const [opts, setOpts] = useState(() => defaultOptions(item));
  const [qty, setQ] = useState(1);
  const missing = missingOptions(item, opts);
  return (
    <form className="form" onSubmit={e => { e.preventDefault(); if (missing.length) return; onAdd(item, opts, qty); closeSheet(); }}>
      <h2>{nameOf(item, lang)}</h2>
      {item.options.map(o => (
        <fieldset className="cafe-opt" key={o.id}>
          <legend className="lbl">{nameOf(o, lang)}{o.required ? '' : tt(' (міндетті емес)', ' (необязательно)')}</legend>
          <div className="cafe-choices">
            {o.choices.map(c => {
              const on = (opts[o.id] ?? []).includes(c.id);
              return (
                <button key={c.id} type="button" className="chip" aria-pressed={on} onClick={() => setOpts(cur => pickChoice(cur, o, c.id))}>
                  {nameOf(c, lang)}{c.delta ? <span className="num"> +{money(c.delta)}</span> : null}
                </button>
              );
            })}
          </div>
        </fieldset>
      ))}
      <div className="cafe-qty">
        <span className="lbl">{tt('Саны', 'Количество')}</span>
        <button className="btn ghost" type="button" onClick={() => setQ(q => Math.max(1, q - 1))} aria-label={tt('Азайту', 'Меньше')} disabled={qty <= 1}>−</button>
        <b className="num" aria-live="polite">{qty}</b>
        <button className="btn ghost" type="button" onClick={() => setQ(q => Math.min(10, q + 1))} aria-label={tt('Көбейту', 'Больше')} disabled={qty >= 10}>+</button>
      </div>
      <button className="btn block" type="submit" disabled={missing.length > 0}>
        {tt('Себетке қосу', 'В корзину')} · <span className="num">{money(unitPrice(item, opts) * qty)}</span>
      </button>
    </form>
  );
}

function CheckoutSheet({ menu, cart, setCart }: { menu: Menu; cart: CartLine[]; setCart: (f: (c: CartLine[]) => CartLine[]) => void }) {
  const { tt, lang, go, toast } = useApp();
  const need = useNeedAccount();
  const [lines, setLines] = useState(cart);
  const free = menu.slots.filter(s => !s.full);
  const [slot, setSlot] = useState(free[0]?.at ?? '');
  const [pay, setPay] = useState<PaymentMethod>(menu.cafe.payments.includes('kaspi') ? 'kaspi' : 'pickup');
  const [note, setNote] = useState('');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  const key = useRef(newKey());
  const sum = cartTotal(lines, menu.items);
  const update = (f: (c: CartLine[]) => CartLine[]) => { setLines(f); setCart(f); };
  const submit = async () => {
    if (busy || !need()) return;
    setBusy(true); setErr('');
    try {
      const r = await api<{ order: Order }>('/api/cafe/orders', {
        method: 'POST', lang,
        body: { cafeId: menu.cafe.id, items: lines.filter(l => !sum.gone.includes(l)).map(l => ({ itemId: l.itemId, qty: l.qty, options: l.options })), pickupAt: slot, payment: pay, note: note.trim(), key: key.current },
      });
      setCart(() => []);
      toast(tt('Тапсырыс жіберілді', 'Заказ отправлен'));
      // go() replaces the sheet's history entry with the order screen
      go(`cafe:o${r.order.id}`);
    } catch (e) {
      setErr(errText(e));
      // a refused order may be retried with other choices: a new intent, a new key
      if (errCode(e) !== 'network') key.current = newKey();
    } finally { setBusy(false); }
  };
  if (!lines.length) return <><h2>{tt('Себет бос', 'Корзина пуста')}</h2></>;
  return (
    <form className="form" onSubmit={e => { e.preventDefault(); void submit(); }}>
      <h2>{tt('Себет', 'Корзина')}</h2>
      <div className="group">
        {lines.map(l => {
          const it = menu.items.find(i => i.id === l.itemId);
          const gone = sum.gone.includes(l);
          const opts = it ? it.options.flatMap(o => (l.options[o.id] ?? []).map(c => nameOf(o.choices.find(x => x.id === c) ?? { nameKk: c, nameRu: c }, lang))) : [];
          return (
            <div className="row" key={l.key}>
              <span className="row-main">
                <span className="row-title">{it ? nameOf(it, lang) : '—'}</span>
                <span className="row-sub">{gone ? tt('Қазір жоқ — алып тасталады', 'Сейчас нет — будет убрано') : [...opts, money(it ? unitPrice(it, l.options) : 0)].join(' · ')}</span>
              </span>
              <span className="cafe-qty small">
                <button className="btn ghost" type="button" onClick={() => update(c => setQty(c, l.key, l.qty - 1))} aria-label={tt('Азайту', 'Меньше')}>−</button>
                <b className="num">{l.qty}</b>
                <button className="btn ghost" type="button" onClick={() => update(c => setQty(c, l.key, l.qty + 1))} aria-label={tt('Көбейту', 'Больше')} disabled={l.qty >= 10}>+</button>
              </span>
            </div>
          );
        })}
      </div>
      <div className="field">
        <label htmlFor="cafeSlot">{tt('Қашан аласыз', 'Когда заберёте')}</label>
        {free.length ? (
          <select className="input" id="cafeSlot" value={slot} onChange={e => setSlot(e.target.value)}>
            {menu.slots.map(s => <option key={s.at} value={s.at} disabled={s.full}>{hhmm(s.at)}{s.full ? tt(' — толды', ' — занято') : ''}</option>)}
          </select>
        ) : <p className="small urgent">{tt('Бүгін бос уақыт қалмады.', 'Свободного времени на сегодня нет.')}</p>}
      </div>
      <fieldset className="field cafe-pay">
        <legend className="lbl">{tt('Төлем', 'Оплата')}</legend>
        {menu.cafe.payments.includes('kaspi') ? (
          <label className="cafe-radio"><input type="radio" name="cafePay" value="kaspi" checked={pay === 'kaspi'} onChange={() => setPay('kaspi')} />
            <span><b>{tt('Қазір Kaspi арқылы', 'Сейчас через Kaspi')}</b><span className="tiny muted">{tt('Ақша тікелей кофейняға түседі', 'Деньги сразу уходят кофейне')}</span></span></label>
        ) : null}
        {menu.cafe.payments.includes('pickup') ? (
          <label className="cafe-radio"><input type="radio" name="cafePay" value="pickup" checked={pay === 'pickup'} onChange={() => setPay('pickup')} />
            <span><b>{tt('Алған кезде кассада', 'На кассе при получении')}</b><span className="tiny muted">{tt('Қолма-қол не Kaspi QR', 'Наличными или Kaspi QR')}</span></span></label>
        ) : null}
        {!menu.cafe.payments.length ? <p className="small urgent">{tt('Төлем уақытша қолжетімсіз.', 'Оплата временно недоступна.')}</p> : null}
      </fieldset>
      <div className="field">
        <label htmlFor="cafeNote">{tt('Ескертпе (міндетті емес)', 'Комментарий (необязательно)')}</label>
        <input className="input" id="cafeNote" maxLength={140} value={note} onChange={e => setNote(e.target.value)} placeholder={tt('Мысалы: қантсыз', 'Например: без сахара')} />
      </div>
      <ErrorLine err={err} />
      <button className="btn block" type="submit" id="cafeSubmit" disabled={busy || !free.length || !menu.cafe.payments.length || sum.count === 0}>
        {busy ? tt('Жіберілуде…', 'Отправляем…') : tt('Тапсырыс беру', 'Заказать')} · <span className="num">{money(sum.total)}</span>
      </button>
      <p className="tiny muted">{tt('Соңғы сома серверде мәзір бойынша есептеледі.', 'Итоговую сумму сервер считает по меню.')}</p>
    </form>
  );
}

/* ---------- order status ---------- */

function useOrderPoll(id: number) {
  const { lang } = useApp();
  const [o, setO] = useState<Order | null>(null);
  const [err, setErr] = useState('');
  const [code, setCode] = useState('');
  const load = useCallback(async () => {
    try { const r = await api<{ order: Order }>(`/api/cafe/orders/${id}`, { lang }); setO(r.order); setErr(''); }
    catch (e) { setErr(errText(e)); setCode(errCode(e)); }
  }, [id, lang]);
  const live = !o || shouldPoll(o);
  useEffect(() => {
    const first = setTimeout(() => { void load(); }, 0);
    const t = live ? setInterval(() => { if (document.visibilityState === 'visible') void load(); }, 4000) : undefined;
    return () => { clearTimeout(first); clearInterval(t); };
  }, [live, load]);
  return { o, setO, err, code, reload: load };
}

function OrderView({ id }: { id: number }) {
  const { tt, lang, openSheet, closeSheet, toast, go } = useApp();
  const acc = useAccount();
  const { o, setO, err, code, reload } = useOrderPoll(id);
  const [busy, setBusy] = useState(false);
  const [perr, setPerr] = useState('');
  const asked = useRef(false);
  const pay = useCallback(async (method?: 'pickup') => {
    setBusy(true); setPerr('');
    try {
      const r = await api<{ order: Order }>(`/api/cafe/orders/${id}/pay`, { method: 'POST', body: method ? { method } : {}, lang });
      setO(r.order);
    } catch (e) { setPerr(errText(e)); } finally { setBusy(false); }
  }, [id, lang, setO]);
  // a fresh Kaspi order asks for its QR at once
  useEffect(() => {
    if (!o || asked.current) return;
    if (o.paymentMethod === 'kaspi' && o.status === 'new' && o.paymentStatus === 'unpaid' && !o.pay) {
      asked.current = true;
      const t = setTimeout(() => { void pay(); }, 0);
      return () => clearTimeout(t);
    }
  }, [o, pay]);
  if (!acc.loaded) return <Loading />;
  if (!hasAccount(acc)) return <SignInCard text={tt('Тапсырысты көру үшін аккаунтқа кіріңіз.', 'Войдите, чтобы увидеть заказ.')} />;
  if (code === 'not_found') return <NotFound />;
  if (!o) return <><ErrorLine err={err} />{err ? null : <Loading />}</>;
  const step = stepIndex(o.status);
  const cancel = () => openSheet(
    <ReasonSheet title={tt('Тапсырысты болдырмау?', 'Отменить заказ?')} label={tt('Себебі (міндетті емес)', 'Причина (необязательно)')} required={false} danger
      action={tt('Болдырмау', 'Отменить')} onSubmit={async () => {
        const r = await api<{ order: Order }>(`/api/cafe/orders/${id}/cancel`, { method: 'POST', lang });
        setO(r.order); closeSheet(); toast(tt('Тапсырыс болдырылмады', 'Заказ отменён'));
      }} />,
  );
  const needsPay = o.paymentMethod === 'kaspi' && o.status === 'new' && o.paymentStatus !== 'paid' && o.paymentStatus !== 'refunded';
  return (
    <div className="cafe cafe-order">
      <div className={`card cafe-codecard${o.status === 'ready' ? ' is-ready' : ''}`} role="status" aria-live="polite">
        <span className="tiny muted">{tt('Алу коды', 'Код получения')}</span>
        <b className="cafe-code num" id="cafeCode">{o.pickupCode}</b>
        <span className="cafe-status"><OrderBadge o={o} /></span>
        <span className="small">{nameOf({ nameKk: o.cafeNameKk, nameRu: o.cafeNameRu }, lang)}{(lang === 'ru' ? o.placeRu : o.placeKk) ? ', ' + (lang === 'ru' ? o.placeRu : o.placeKk) : ''} · <b className="num">{hhmm(o.pickupAt)}</b></span>
      </div>
      {step >= 0 ? (
        <ol className="cafe-steps" aria-label={tt('Тапсырыс барысы', 'Ход заказа')}>
          {[tt('Жіберілді', 'Отправлен'), tt('Қабылданды', 'Принят'), tt('Дайын', 'Готов'), tt('Алынды', 'Выдан')].map((s, i) => (
            <li key={s} className={i <= step ? 'on' : ''} aria-current={i === step ? 'step' : undefined}>{s}</li>
          ))}
        </ol>
      ) : null}
      {needsPay ? (
        <div className="card cafe-paycard">
          <h2>{tt('Kaspi арқылы төлеу', 'Оплата через Kaspi')}</h2>
          {o.paymentStatus === 'failed' ? <p className="small urgent">{tt('Төлем өтпеді не уақыты бітті. Қайта көріңіз.', 'Оплата не прошла или истекла. Попробуйте снова.')}</p> : null}
          {o.pay ? <KaspiPay pay={o.pay} /> : <p className="small muted">{busy ? tt('Kaspi сілтемесі жасалып жатыр…', 'Создаём ссылку Kaspi…') : ''}</p>}
          <ErrorLine err={perr} />
          <div className="btn-row">
            {!o.pay || o.paymentStatus === 'failed' ? <button className="btn" type="button" disabled={busy} onClick={() => void pay()}>{tt('Kaspi QR алу', 'Получить Kaspi QR')}</button> : null}
            <button className="btn ghost" type="button" disabled={busy} onClick={() => void pay('pickup')}>{tt('Кассада төлеймін', 'Оплачу на кассе')}</button>
          </div>
          <p className="tiny muted">{tt('Төлем кофейняның Kaspi шотына тікелей түседі. Бұл бет өзі жаңарады.', 'Оплата идёт прямо на Kaspi-счёт кофейни. Страница обновится сама.')}</p>
        </div>
      ) : null}
      <div className="card">
        <div className="group cafe-lines">
          {o.items.map((l, i) => (
            <div className="row" key={i}>
              <span className="row-main"><span className="row-title">{l.qty} × {nameOf(l, lang)}</span>
                {l.options.length ? <span className="row-sub">{l.options.map(x => nameOf(x, lang)).join(' · ')}</span> : null}</span>
              <span className="row-end num">{money(l.total)}</span>
            </div>
          ))}
        </div>
        <p className="cafe-total"><span>{tt('Барлығы', 'Итого')}</span><b className="num">{money(o.total)}</b></p>
        <p className="small muted">{paymentLabel(o, tt)}{o.note ? ' · ' + o.note : ''}</p>
        {o.cancelReason && o.cancelReason !== 'student' ? <p className="small">{o.cancelReason === 'unpaid' ? tt('Төленбегендіктен болдырылмады.', 'Отменён: не оплачен.') : o.cancelReason}</p> : null}
      </div>
      <ErrorLine err={err} />
      <div className="btn-row">
        {o.status === 'new' ? <button className="btn danger" type="button" onClick={cancel}>{tt('Болдырмау', 'Отменить')}</button> : null}
        <button className="btn ghost" type="button" onClick={() => { void reload(); }}>{tt('Жаңарту', 'Обновить')}</button>
        {!isActive(o.status) ? <button className="btn secondary" type="button" onClick={() => go(`cafe:${o.cafeId}`)}>{tt('Тағы тапсырыс', 'Заказать ещё')}</button> : null}
      </div>
    </div>
  );
}

function OrderHistory() {
  const { tt, lang } = useApp();
  const acc = useAccount();
  const q = useLoad<{ orders: Order[] }>(hasAccount(acc) ? '/api/cafe/orders/mine' : null);
  if (!acc.loaded) return <Loading />;
  if (!hasAccount(acc)) return <SignInCard text={tt('Тапсырыстар тарихы аккаунтта сақталады.', 'История заказов хранится в аккаунте.')} />;
  return (
    <div className="cafe">
      <ErrorLine err={q.err} />
      {!q.data ? <Loading /> : !q.data.orders.length ? <Empty icon="cup">{tt('Әзірге тапсырыс жоқ.', 'Заказов пока нет.')}</Empty> : (
        <div className="group">
          {q.data.orders.map(o => (
            <Row key={o.id} go={`cafe:o${o.id}`} icon="cup" title={<span className="num">№{o.pickupCode} · {money(o.total)}</span>}
              sub={`${nameOf({ nameKk: o.cafeNameKk, nameRu: o.cafeNameRu }, lang)} · ${o.pickupAt.slice(0, 10)} ${hhmm(o.pickupAt)}`} end={<OrderBadge o={o} />} />
          ))}
        </div>
      )}
    </div>
  );
}

/* ---------- the counter: order board (tablet) ---------- */

interface Board { cafeId: number; cafes: CafeT[]; orders: Order[] }

function StaffBoard() {
  const { tt, lang, openSheet, closeSheet, toast } = useApp();
  const acc = useAccount();
  const [cafe, setCafe] = useState(0);
  const [board, setBoard] = useState<Board | null>(null);
  const [err, setErr] = useState('');
  const [code, setCode] = useState('');
  const [tab, setTab] = useState('orders');
  const [col, setCol] = useState('new');
  const [sound, setSound] = useState(false);
  const seen = useRef<Set<number> | null>(null);
  const soundRef = useRef(sound);
  useEffect(() => { soundRef.current = sound; }, [sound]);
  const load = useCallback(async () => {
    try {
      const b = await api<Board>(`/api/cafe/staff/orders${cafe ? `?cafe=${cafe}` : ''}`, { lang });
      const fresh = newArrivals(seen.current, b.orders);
      if (fresh.length) {
        if (soundRef.current) beep();
        try { navigator.vibrate?.(200); } catch { /* not supported */ }
      }
      seen.current = new Set(b.orders.map(o => o.id));
      setBoard(b); setErr(''); setCode('');
    } catch (e) { setErr(errText(e)); setCode(errCode(e)); }
  }, [cafe, lang]);
  const signed = hasAccount(acc);
  useEffect(() => {
    if (!signed) return;
    const first = setTimeout(() => { void load(); }, 0);
    const t = setInterval(() => { if (document.visibilityState === 'visible') void load(); }, 5000);
    return () => { clearTimeout(first); clearInterval(t); };
  }, [signed, load]);
  if (!acc.loaded) return <Loading />;
  if (!signed) return <SignInCard text={tt('Кофейня қызметкері ретінде кіріңіз.', 'Войдите как сотрудник кофейни.')} />;
  if (code === 'not_staff' || code === 'forbidden') return <NotFound />;
  if (!board) return <><ErrorLine err={err} />{err ? null : <Loading />}</>;
  const me = board.cafes.find(c => c.id === board.cafeId);
  const act = async (o: Order, action: string, body?: unknown) => {
    try {
      await api(`/api/cafe/staff/orders/${o.id}/${action}`, { method: 'POST', body: body ?? {}, lang });
      await load();
    } catch (e) { toast(errText(e)); }
  };
  const decline = (o: Order) => openSheet(
    <ReasonSheet title={tt(`№${o.pickupCode} бас тарту`, `Отклонить №${o.pickupCode}`)} label={tt('Себебі (студент көреді)', 'Причина (студент увидит)')} required={false} danger
      action={tt('Бас тарту', 'Отклонить')} onSubmit={async reason => { await act(o, 'decline', { reason }); closeSheet(); }} />,
  );
  const cols: [string, string, Order[]][] = [
    ['new', tt('Жаңа', 'Новые'), board.orders.filter(o => o.status === 'new')],
    ['accepted', tt('Дайындалуда', 'Готовятся'), board.orders.filter(o => o.status === 'accepted')],
    ['ready', tt('Дайын', 'Готовы'), board.orders.filter(o => o.status === 'ready')],
  ];
  const manual = board.orders.filter(o => o.refundManual);
  return (
    <div className="cafe cafe-staff">
      <div className="cafe-staff-head">
        {board.cafes.length > 1 ? (
          <select className="input" aria-label={tt('Кофейня', 'Кофейня')} value={board.cafeId} onChange={e => { seen.current = null; setCafe(Number(e.target.value)); }}>
            {board.cafes.map(c => <option key={c.id} value={c.id}>{nameOf(c, lang)}</option>)}
          </select>
        ) : <h2>{me ? nameOf(me, lang) : ''}</h2>}
        <button className="btn ghost" type="button" aria-pressed={sound} onClick={() => { if (!sound) beep(); setSound(!sound); }}>
          <Icon n="bell" s={18} /> {sound ? tt('Дыбыс қосулы', 'Звук включён') : tt('Дыбысты қосу', 'Включить звук')}
        </button>
      </div>
      <Seg items={[['orders', tt('Тапсырыстар', 'Заказы')], ['menu', tt('Мәзір', 'Меню')]]} cur={tab} onPick={setTab} />
      <ErrorLine err={err} />
      {tab === 'menu' ? <StaffMenu cafeId={board.cafeId} role={me?.myRole ?? ''} /> : (
        <>
          {manual.length ? (
            <div className="card cafe-alert" role="alert">
              <b>{tt('Ақшаны қолмен қайтару керек', 'Нужен ручной возврат')}</b>
              {manual.map(o => (
                <div className="btn-row" key={o.id}>
                  <span className="small num">№{o.pickupCode} · {money(o.total)}</span>
                  {me?.myRole === 'manager' ? <button className="btn secondary" type="button" onClick={() => void act(o, 'refunded')}>{tt('Қайтарылды', 'Возвращено')}</button> : null}
                </div>
              ))}
            </div>
          ) : null}
          <div className="cafe-colpick"><Seg items={cols.map(([k, l, list]) => [k, `${l} ${list.length}`])} cur={col} onPick={setCol} /></div>
          <div className="cafe-board">
            {cols.map(([k, label, list]) => (
              <section key={k} className={`cafe-col${col === k ? ' cur' : ''}`} aria-label={label}>
                <h3 className="cafe-col-h">{label} <span className="badge b-line num">{list.length}</span></h3>
                {!list.length ? <p className="small muted">{tt('Бос', 'Пусто')}</p> : list.map(o => (
                  <article key={o.id} className="card cafe-ticket" data-status={o.status}>
                    <div className="cafe-ticket-top">
                      <b className="cafe-ticket-code num">№{o.pickupCode}</b>
                      <span className="num cafe-ticket-time"><Icon n="clock" s={16} /> {hhmm(o.pickupAt)}</span>
                    </div>
                    {o.customer ? <span className="small muted">{o.customer}</span> : null}
                    <ul className="cafe-ticket-items">
                      {o.items.map((l, i) => <li key={i}><b className="num">{l.qty}×</b> {nameOf(l, lang)}{l.options.length ? <span className="muted"> · {l.options.map(x => nameOf(x, lang)).join(', ')}</span> : null}</li>)}
                    </ul>
                    {o.note ? <p className="small cafe-note">{o.note}</p> : null}
                    <span className={`badge ${o.paymentMethod === 'kaspi' ? 'b-ok' : 'b-line'}`}>{paymentLabel(o, tt)} · {money(o.total)}</span>
                    <div className="btn-row cafe-ticket-btns">
                      {o.status === 'new' ? <><button className="btn" type="button" onClick={() => void act(o, 'accept')}>{tt('Қабылдау', 'Принять')}</button>
                        <button className="btn ghost" type="button" onClick={() => decline(o)}>{tt('Бас тарту', 'Отклонить')}</button></> : null}
                      {o.status === 'accepted' ? <button className="btn" type="button" onClick={() => void act(o, 'ready')}>{tt('Дайын', 'Готово')}</button> : null}
                      {o.status === 'ready' ? <><button className="btn" type="button" onClick={() => void act(o, 'picked-up')}>{tt('Берілді', 'Выдан')}</button>
                        <button className="btn ghost" type="button" onClick={() => void act(o, 'no-show')}>{tt('Келмеді', 'Не пришёл')}</button></> : null}
                    </div>
                  </article>
                ))}
              </section>
            ))}
          </div>
        </>
      )}
    </div>
  );
}

/* ---------- menu at the counter: availability for everyone, editing for managers (and admins) ---------- */

export function StaffMenu({ cafeId, role }: { cafeId: number; role: string }) {
  const { tt, lang, openSheet, toast } = useApp();
  const q = useLoad<{ categories: Menu['categories']; items: Item[]; role: string }>(`/api/cafe/staff/menu?cafe=${cafeId}`);
  const canEdit = (q.data?.role || role) === 'manager' || (q.data?.role || role) === 'admin';
  // the switch flips at once; a refused change flips back
  const [local, setLocal] = useState<Record<number, boolean>>({});
  const isOn = (it: Item) => local[it.id] ?? it.available;
  const toggle = async (it: Item) => {
    const next = !isOn(it);
    setLocal(m => ({ ...m, [it.id]: next }));
    try { await api(`/api/cafe/staff/menu/items/${it.id}/availability`, { method: 'POST', body: { available: next }, lang }); }
    catch (e) { setLocal(m => ({ ...m, [it.id]: !next })); toast(errText(e)); }
  };
  if (!q.data) return <><ErrorLine err={q.err} />{q.err ? null : <Loading />}</>;
  return (
    <section className="section">
      {canEdit ? (
        <div className="btn-row">
          <button className="btn secondary" type="button" onClick={() => openSheet(<ItemForm cafeId={cafeId} categories={q.data!.categories} onDone={q.reload} />)}><Icon n="plus" s={18} /> {tt('Позиция', 'Позиция')}</button>
          <button className="btn ghost" type="button" onClick={() => openSheet(<CategoryForm cafeId={cafeId} onDone={q.reload} />)}><Icon n="plus" s={18} /> {tt('Санат', 'Категория')}</button>
        </div>
      ) : null}
      <div className="group">
        {q.data.items.filter(i => !i.archived).map(it => (
          <div className="row" key={it.id}>
            <span className="row-main"><span className="row-title">{nameOf(it, lang)}</span><span className="row-sub num">{money(it.price)}{it.options.length ? tt(` · ${it.options.length} опция`, ` · опций: ${it.options.length}`) : ''}</span></span>
            {canEdit ? <button className="icon-btn" type="button" aria-label={tt('Өзгерту', 'Изменить')} onClick={() => openSheet(<ItemForm cafeId={cafeId} categories={q.data!.categories} item={it} onDone={q.reload} />)}><Icon n="edit" s={18} /></button> : null}
            <label className="cafe-switch">
              <input type="checkbox" checked={isOn(it)} onChange={() => void toggle(it)} aria-label={tt(`${nameOf(it, lang)}: бар`, `${nameOf(it, lang)}: в наличии`)} />
              <span aria-hidden="true">{isOn(it) ? tt('Бар', 'Есть') : tt('Жоқ', 'Нет')}</span>
            </label>
          </div>
        ))}
      </div>
    </section>
  );
}

function CategoryForm({ cafeId, onDone }: { cafeId: number; onDone: () => void }) {
  const { tt, lang, closeSheet } = useApp();
  const [kk, setKk] = useState(''), [ru, setRu] = useState('');
  const [err, setErr] = useState('');
  return (
    <form className="form" onSubmit={async e => {
      e.preventDefault();
      try { await api('/api/cafe/staff/menu/categories', { method: 'POST', body: { cafeId, nameKk: kk.trim(), nameRu: ru.trim(), sort: 0 }, lang }); closeSheet(); onDone(); }
      catch (x) { setErr(errText(x)); }
    }}>
      <h2>{tt('Жаңа санат', 'Новая категория')}</h2>
      <div className="field"><label htmlFor="ccKk">{tt('Атауы (қаз.)', 'Название (каз.)')}</label><input className="input" id="ccKk" value={kk} onChange={e => setKk(e.target.value)} required maxLength={60} /></div>
      <div className="field"><label htmlFor="ccRu">{tt('Атауы (орыс.)', 'Название (рус.)')}</label><input className="input" id="ccRu" value={ru} onChange={e => setRu(e.target.value)} required maxLength={60} /></div>
      <ErrorLine err={err} />
      <button className="btn block" type="submit">{tt('Сақтау', 'Сохранить')}</button>
    </form>
  );
}

function ItemForm({ cafeId, categories, item, onDone }: { cafeId: number; categories: Menu['categories']; item?: Item; onDone: () => void }) {
  const { tt, lang, closeSheet } = useApp();
  const [f, setF] = useState(() => ({
    nameKk: item?.nameKk ?? '', nameRu: item?.nameRu ?? '', descKk: item?.descKk ?? '', descRu: item?.descRu ?? '',
    price: item ? String(item.price / 100) : '', categoryId: item?.categoryId ? String(item.categoryId) : '',
    options: JSON.stringify(item?.options ?? [], null, 1), archived: !!item?.archived,
  }));
  const [err, setErr] = useState('');
  const set = (k: keyof typeof f) => (e: { target: { value: string } }) => setF(x => ({ ...x, [k]: e.target.value }));
  const optsOk = useMemo(() => { try { return Array.isArray(JSON.parse(f.options || '[]')); } catch { return false; } }, [f.options]);
  return (
    <form className="form" onSubmit={async e => {
      e.preventDefault();
      const price = Math.round(Number(f.price.replace(/\s/g, '')) * 100);
      if (!Number.isFinite(price) || price <= 0 || price % 100 !== 0 || !optsOk) { setErr(tt('Баға бүтін теңгемен, опциялар JSON тізімі болуы керек.', 'Цена — целые тенге, опции — JSON-список.')); return; }
      const body = { cafeId, nameKk: f.nameKk.trim(), nameRu: f.nameRu.trim(), descKk: f.descKk.trim(), descRu: f.descRu.trim(), price,
        categoryId: f.categoryId ? Number(f.categoryId) : null, options: JSON.parse(f.options || '[]'), archived: f.archived, available: item?.available ?? true, sort: item?.sort ?? 0 };
      try {
        await api(item ? `/api/cafe/staff/menu/items/${item.id}` : '/api/cafe/staff/menu/items', { method: item ? 'PUT' : 'POST', body, lang });
        closeSheet(); onDone();
      } catch (x) { setErr(errText(x)); }
    }}>
      <h2>{item ? tt('Позицияны өзгерту', 'Изменить позицию') : tt('Жаңа позиция', 'Новая позиция')}</h2>
      <div className="field"><label htmlFor="miKk">{tt('Атауы (қаз.)', 'Название (каз.)')}</label><input className="input" id="miKk" value={f.nameKk} onChange={set('nameKk')} required maxLength={80} /></div>
      <div className="field"><label htmlFor="miRu">{tt('Атауы (орыс.)', 'Название (рус.)')}</label><input className="input" id="miRu" value={f.nameRu} onChange={set('nameRu')} required maxLength={80} /></div>
      <div className="field"><label htmlFor="miPrice">{tt('Бағасы, ₸', 'Цена, ₸')}</label><input className="input num" id="miPrice" inputMode="numeric" value={f.price} onChange={set('price')} required /></div>
      <div className="field"><label htmlFor="miCat">{tt('Санат', 'Категория')}</label>
        <select className="input" id="miCat" value={f.categoryId} onChange={set('categoryId')}>
          <option value="">{tt('Санатсыз', 'Без категории')}</option>
          {categories.map(c => <option key={c.id} value={c.id}>{nameOf(c, lang)}</option>)}
        </select></div>
      <div className="field"><label htmlFor="miDescKk">{tt('Сипаттама (қаз.)', 'Описание (каз.)')}</label><input className="input" id="miDescKk" value={f.descKk} onChange={set('descKk')} maxLength={300} /></div>
      <div className="field"><label htmlFor="miDescRu">{tt('Сипаттама (орыс.)', 'Описание (рус.)')}</label><input className="input" id="miDescRu" value={f.descRu} onChange={set('descRu')} maxLength={300} /></div>
      <div className="field"><label htmlFor="miOpts">{tt('Опциялар (JSON: көлем, сүт…; delta — тиынмен)', 'Опции (JSON: объём, молоко…; delta — в тиынах)')}</label>
        <textarea className="input mono" id="miOpts" rows={5} value={f.options} onChange={set('options')} aria-invalid={!optsOk} /></div>
      {item ? <label className="cafe-switch"><input type="checkbox" checked={f.archived} onChange={e => setF(x => ({ ...x, archived: e.target.checked }))} /><span>{tt('Мәзірден алып тастау', 'Убрать из меню')}</span></label> : null}
      <ErrorLine err={err} />
      <button className="btn block" type="submit">{tt('Сақтау', 'Сохранить')}</button>
    </form>
  );
}
