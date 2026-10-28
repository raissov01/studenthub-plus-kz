'use client';
/* Detail screens: vacancy, opportunity, event. Job and opportunity pages carry the trust block. */
import { useEffect } from 'react';
import { useApp } from '@/components/app';
import { track } from '@/features/analytics/track';
import { remindLink } from '@/features/telegram/webapp';
import { useConfig } from '@/lib/config';
import { openTelegramLink, useTelegram } from '@/lib/telegram';
import { DemoBadge, Icon, Qr, Row, SectionHead, ShareRow } from '@/components/ui';
import { VerifiedSheet } from '@/components/views/sheets';
import { reportHref } from '@/lib/config';
import { CITIES, EVENTS, EVENT_CATS, JOB_TAGS } from '@/lib/data/static';
import { hourly, oppLeft, payText, qualify, typeLabel } from '@/lib/domain';
import { dOff, money, parseYmd } from '@/lib/format';
import { pathOf } from '@/lib/routes';
import type { Provenance } from '@/lib/types';

/** one view event per screen opening (effects may run twice for the same mount in development) */
let lastView = { key: '', at: 0 };
function trackView(name: 'job_view' | 'opportunity_view', id: string) {
  const key = name + ':' + id, now = Date.now();
  if (lastView.key === key && now - lastView.at < 1500) return;
  lastView = { key, at: now };
  track(name, { id });
}

/** «Тексерілген» badge; tapping it explains what was checked */
function VerifiedBadge({ item, kind, id }: { item: Provenance; kind: string; id: string }) {
  const { tt, openSheet } = useApp();
  const path = pathOf(`${kind}:${id}`);
  return (
    <button type="button" className="badge b-ok verified" onClick={() => openSheet(<VerifiedSheet item={item} kind={kind} id={id} path={path} />)}
      aria-haspopup="dialog">
      <Icon n="shield" s={12} /> {tt('Тексерілген', 'Проверен')}
    </button>
  );
}

const hostOf = (url: string) => { try { return new URL(url).hostname.replace(/^www\./, ''); } catch { return url; } };

/** source, dates, contacts and the report link, in the card/kv style of the rest of the page */
function TrustBlock({ item, kind, id }: { item: Provenance; kind: string; id: string }) {
  const { tt, fDateY, lang } = useApp();
  const c = item.contact || {};
  const url = window.location.origin + pathOf(`${kind}:${id}`);
  return (
    <section className="section trust">
      <h2>{tt('Дереккөз және тексеру', 'Источник и проверка')}</h2>
      <div className="card">
        <dl className="kv">
          <dt>{tt('Ресми дереккөз', 'Официальный источник')}</dt>
          <dd>{item.is_demo || !item.source_url
            ? <span className="muted">{tt('Үлгі жазба — ресми дереккөз әлі жоқ', 'Пример — официального источника пока нет')}</span>
            : <a href={item.source_url} target="_blank" rel="noopener">{hostOf(item.source_url)}</a>}</dd>
          {item.posted_at ? <><dt>{tt('Жарияланған күні', 'Дата публикации')}</dt><dd>{fDateY(parseYmd(item.posted_at))}</dd></> : null}
          {item.verified_at ? <><dt>{tt('Соңғы тексерілген күні', 'Последняя проверка')}</dt><dd>{fDateY(parseYmd(item.verified_at))}</dd></> : null}
          {c.phone ? <><dt>{tt('Телефон', 'Телефон')}</dt><dd><a href={`tel:${c.phone.replace(/[^\d+]/g, '')}`}>{c.phone}</a></dd></> : null}
          {c.email ? <><dt>Email</dt><dd><a href={`mailto:${c.email}`}>{c.email}</a></dd></> : null}
          {c.site ? <><dt>{tt('Сайт', 'Сайт')}</dt><dd><a href={c.site} target="_blank" rel="noopener">{hostOf(c.site)}</a></dd></> : null}
        </dl>
        <a className="btn ghost" href={reportHref(id, kind, url, lang)} target="_blank" rel="noopener">
          <Icon n="bell" s={18} /> {tt('Қате не алаяқтық туралы хабарлау', 'Сообщить об ошибке или мошенничестве')}
        </a>
      </div>
    </section>
  );
}

export function JobView({ id }: { id: string }) {
  const { S, tt, tp, L, I, go, update, toast, content } = useApp();
  const j = content.jobs.find(x => x.id === id);
  const found = !!j;
  useEffect(() => { if (found) trackView('job_view', id); }, [found, id]);
  if (!j) return null;
  const pt = payText(j, I), applied = !!S.applied[j.id];
  return (
    <>
      <div className="detail-head">
        <div className="badges">
          {j.today ? <span className="badge b-sun"><Icon n="bolt" s={12} /> {tt('Бүгінгі жұмыс', 'Работа на сегодня')}</span> : null}
          {j.tags.filter(k => JOB_TAGS[k]).map(k => <span key={k} className="badge b-line">{tp(JOB_TAGS[k])}</span>)}
          <DemoBadge show={j.is_demo} />
        </div>
        <h1>{L(j.title)}</h1>
        <ShareRow path={pathOf('job:' + j.id)} title={L(j.title)} kind="job" id={j.id} />
        <p className="muted">{L(j.org)} <VerifiedBadge item={j} kind="job" id={j.id} /></p>
      </div>
      <div className="card">
        <div className="card-row" style={{ justifyContent: 'space-between', alignItems: 'baseline' }}><span className="big-num">{pt.main}</span><span className="muted small">{pt.sub}</span></div>
        <dl className="kv">
          <dt>{tt('Сағатына', 'В час')}</dt><dd className="num">≈ {money(hourly(j))}</dd>
          <dt>{tt('Кесте', 'График')}</dt><dd>{L(j.sched)}</dd>
          <dt>{tt('Қала', 'Город')}</dt><dd>{L(CITIES[j.city])}</dd>
        </dl>
      </div>
      <section className="section"><h2>{tt('Не істеу керек', 'Что нужно делать')}</h2><p>{L(j.desc)}</p></section>
      <section className="section"><h2>{tt('Талаптар', 'Требования')}</h2><ul className="bullets">{j.req.map((r, i) => <li key={i}>{L(r)}</li>)}</ul></section>
      <div className="demo-note"><Icon n="shield" s={18} /><span>{tt('Қауіпсіздік: жұмысқа орналасу үшін алдын ала ақша төлемеңіз. Төлем мен жұмыс уақыты алдын ала жазылуы тиіс.', 'Безопасность: не платите деньги за трудоустройство. Оплата и время работы должны быть указаны заранее.')}</span></div>
      <TrustBlock item={j} kind="job" id={j.id} />
      <div className="sticky-cta">
        {applied
          ? <button className="btn secondary" type="button" onClick={() => { update(s => { delete s.applied[j.id]; }); toast(tt('Өтінім кері қайтарылды', 'Отклик отозван')); }}><Icon n="check" s={18} /> {tt('Өтінім жіберілді', 'Отклик отправлен')}</button>
          : <button className="btn" type="button" onClick={() => { update(s => { s.applied[j.id] = Date.now(); }); toast(tt('Өтінім жіберілді', 'Отклик отправлен')); }}><Icon n="send" s={18} /> {tt('Өтінім жіберу', 'Откликнуться')}</button>}
        <button className="btn ghost" type="button" onClick={() => go('match:' + j.id)}><Icon n="target" s={18} /> {tt('Сәйкестікті тексеру', 'Проверить совпадение')}</button>
      </div>
    </>
  );
}

export function OppView({ id }: { id: string }) {
  const { S, ui, tt, L, I, setUi, update, toast, go, content } = useApp();
  const o = content.opportunities.find(x => x.id === id);
  const found = !!o;
  useEffect(() => { if (found) trackView('opportunity_view', id); }, [found, id]);
  const cfg = useConfig();
  const tg = useTelegram();
  if (!o) return null;
  const remind = remindLink(cfg.botUsername, o.id);
  const showCheck = ui.oppCheck[id];
  const q = qualify(o, S.profile, I);
  const docs = S.oppDocs[id] || {};
  const ready = o.docs.filter((_, i) => docs[i]).length;
  const left = oppLeft(o);
  const saved = !!S.saved[o.id];
  return (
    <>
      <div className="detail-head">
        <div className="badges">
          <span className="badge b-sky">{typeLabel(o.type, I)}</span>
          {o.funded ? <span className="badge b-ok">{tt('Қаржыландырылады', 'Финансируется')}</span> : null}
          <DemoBadge show={o.is_demo} />
        </div>
        <h1>{L(o.title)}</h1>
        <ShareRow path={pathOf('opp:' + o.id)} title={L(o.title)} kind="opp" id={o.id} />
        <p className="muted">{L(o.org)} <VerifiedBadge item={o} kind="opp" id={o.id} /></p>
        <p className={`small ${left <= 14 ? 'urgent' : ''}`}><Icon n="clock" s={14} /> {Number.isFinite(left)
          ? <>{tt('Қабылдау', 'Приём до')}: {I.fDate(dOff(left))} · {I.daysLeftTxt(left)}</>
          : tt('Өтінім мерзімі шектелмеген', 'Приём заявок без срока')}</p>
      </div>
      <p>{L(o.desc)}</p>
      <section className="card">
        <div className="card-row" style={{ alignItems: 'center' }}>
          <span className={`ico ${showCheck ? (q.ok ? 'ok' : 'bad') : ''}`}><Icon n="target" /></span>
          <div className="row-main"><h3>{tt('Мен сәйкеспін бе?', 'Подхожу ли я?')}</h3><span className="row-sub">{tt('Профиліңізді талаптармен салыстырамыз', 'Сравним ваш профиль с требованиями')}</span></div>
        </div>
        {showCheck ? (
          <>
            <div>{q.res.map((r, i) => <div key={i} className="res"><span className={`dot ${r.ok ? (r.mid ? 'mid' : 'ok') : 'no'}`}>{r.ok ? '✓' : '✕'}</span><div className="row-main"><span className="row-title">{r.t}</span><span className="row-sub">{r.you}</span></div></div>)}</div>
            <p className="small"><b>{q.ok ? tt('Негізгі талаптарға сәйкес келесіз.', 'Вы соответствуете основным требованиям.') : tt('Кейбір талаптар әзірге орындалмаған.', 'Некоторые требования пока не выполнены.')}</b></p>
            <p className="tiny muted">{tt('Бұл алдын ала бағалау. Соңғы шешімді бағдарлама ұйымдастырушысы қабылдайды.', 'Это предварительная оценка. Окончательное решение принимает организатор программы.')}</p>
            {!q.ok ? <button className="btn ghost" type="button" onClick={() => go('edit')}><Icon n="edit" s={18} /> {tt('Профильді жаңарту', 'Обновить профиль')}</button> : null}
          </>
        ) : <button className="btn" type="button" onClick={() => setUi(u => ({ oppCheck: { ...u.oppCheck, [id]: true } }))}>{tt('Тексеру', 'Проверить')}</button>}
      </section>
      <section className="section">
        <SectionHead title={tt('Құжаттар', 'Документы')} more={<span className="tiny muted num">{ready}/{o.docs.length} {tt('дайын', 'готово')}</span>} />
        <div className="group">
          {o.docs.map((d, i) => (
            <label key={i} className="row check">
              <input type="checkbox" checked={!!docs[i]} onChange={e => { const v = e.target.checked; update(s => { (s.oppDocs[o.id] ||= {})[i] = v; }); }} />
              <span className="row-main"><span className="row-title">{L(d)}</span></span>
            </label>
          ))}
        </div>
      </section>
      <section className="section"><h2>{tt('Қалай рәсімдеу керек', 'Как оформить')}</h2><ol className="steps">{o.steps.map((s, i) => <li key={i}><span>{L(s)}</span></li>)}</ol></section>
      <TrustBlock item={o} kind="opp" id={o.id} />
      <div className="sticky-cta">
        <button className={`btn ${saved ? 'secondary' : ''} block`} type="button" onClick={() => {
          update(s => { if (s.saved[o.id]) delete s.saved[o.id]; else s.saved[o.id] = Date.now(); });
          if (!saved) track('deadline_saved', { id: o.id });
          toast(!saved ? tt('Сақталды', 'Сохранено') : tt('Сақталғаннан алынды', 'Убрано из сохранённых'));
        }}>
          {saved ? <><Icon n="check" s={18} /> {tt('Сақталған', 'Сохранено')}</> : <><Icon n="bell" s={18} /> {tt('Сақтау және мерзімді бақылау', 'Сохранить и следить за сроком')}</>}
        </button>
        {saved && remind ? (
          <a className="btn ghost block" href={remind} target="_blank" rel="noopener" onClick={e => { if (tg.app) { e.preventDefault(); openTelegramLink(remind); } }}>
            <Icon n="send" s={18} /> {tt('Telegram-да еске салу', 'Напомнить в Telegram')}
          </a>
        ) : null}
      </div>
    </>
  );
}

export function EventView({ id }: { id: string }) {
  const { S, tt, L, I, monS, wd, update, toast } = useApp();
  const e = EVENTS.find(x => x.id === id);
  if (!e) return null;
  const d = dOff(e.day), reg = S.events[e.id];
  return (
    <>
      <div className="card-row">
        <div className="date" style={{ width: 64 }}><small>{monS[d.getMonth()]}</small><b style={{ fontSize: '1.6rem' }}>{d.getDate()}</b></div>
        <div className="detail-head" style={{ minWidth: 0 }}><h1>{L(e.title)}</h1>
          <div className="badges"><span className="badge b-line">{L(EVENT_CATS[e.cat])}</span>{e.cert ? <span className="badge b-sun"><Icon n="award" s={12} /> {tt('Сертификат беріледі', 'Выдаётся сертификат')}</span> : null}</div></div>
      </div>
      <div className="group">
        <Row icon="clock" title={`${wd[d.getDay()]}, ${I.fDate(d)}`} sub={e.time} chev={false} />
        <Row go="map" icon="pin" title={L(e.place)} sub={tt('Кампус картасынан көру', 'Посмотреть на карте кампуса')} />
      </div>
      {L(e.desc) ? <p>{L(e.desc)}</p> : null}
      {reg ? (
        <>
          <div className="card" style={{ alignItems: 'center', textAlign: 'center' }}>
            <span className="eyebrow">{tt('Электрондық билет', 'Электронный билет')}</span>
            <Qr text={`STUDENTHUB|${e.id}|${reg.code}|${S.profile.name}`} />
            <span className="code">{reg.code}</span>
            <span className="tiny muted">{tt('Кіреберісте көрсетіңіз', 'Покажите на входе')}</span>
          </div>
          <button className="btn danger block" type="button" onClick={() => { update(s => { delete s.events[e.id]; delete s.remind[e.id]; }); toast(tt('Тіркеу жойылды', 'Запись отменена')); }}>{tt('Тіркеуден бас тарту', 'Отменить запись')}</button>
        </>
      ) : (
        <div className="sticky-cta">
          <button className="btn block" type="button" onClick={() => {
            update(s => { s.events[e.id] = { code: `EV-${e.id.toUpperCase()}-${String(Math.floor(1000 + Math.random() * 9000))}` }; s.remind[e.id] = true; });
            toast(tt('Сіз тіркелдіңіз', 'Вы записаны'));
          }}><Icon n="check" s={18} /> {tt('Тіркелу', 'Записаться')}</button>
        </div>
      )}
      {e.cert ? <p className="tiny muted">{tt('Қатысқаннан кейін сертификат портфолиоңызға қосылады (егер ұйымдастырушы берсе).', 'После участия сертификат появится в портфолио (если его выдаёт организатор).')}</p> : null}
    </>
  );
}
