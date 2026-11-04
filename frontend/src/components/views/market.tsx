'use client';
/* eslint-disable @next/next/no-img-element -- market photos come from our API (some only with the session); next/image would proxy them */
/* «Отдам даром» (prompt 08): /market grid with filters, /market/<id> item page, /market/new publishing (rules once,
   username check, up to 3 photos resized in the browser), /market/mine owner actions, and the search results block. */
import { useEffect, useRef, useState } from 'react';
import { useApp } from '@/components/app';
import { Chip, Empty, Icon, Row, SectionHead } from '@/components/ui';
import { agoDays, ErrorLine, errText, Feature, hasAccount, Loading, NotFound, SignInCard, useFeature, useLoad, useNeedAccount, ymd } from '@/components/views/common';
import { api, apiUpload, useAccount } from '@/lib/account';
import { useConfig } from '@/lib/config';
import {
  MARKET_CATS, MARKET_CONDITIONS, MARKET_LIMITS, MARKET_REPORT_REASONS, marketSearchItems, shrinkPhoto,
  type MarketItem, type MarketList, type MarketMe,
} from '@/lib/market';
import { openTelegramLink } from '@/lib/telegram';

export function Market({ prm }: { prm: string }) {
  return (
    <Feature name="market">
      {!prm ? <MarketGrid /> : prm === 'new' ? <MarketNew /> : prm === 'mine' ? <MarketMine /> : /^\d+$/.test(prm) ? <MarketItemView id={prm} /> : <NotFound />}
    </Feature>
  );
}

function MarketCard({ it }: { it: MarketItem }) {
  const { tt, L, go } = useApp();
  return (
    <button className="mk-card" type="button" onClick={() => go('market:' + it.id)}>
      <span className="mk-photo">{it.photos[0] ? <img src={it.photos[0]} alt="" loading="lazy" /> : <Icon n="gift" s={30} />}</span>
      <span className="mk-body">
        <span className="row-title">{it.title}</span>
        <span className="tiny muted">{L(MARKET_CONDITIONS[it.condition])} · {agoDays(it.daysAgo, tt)}</span>
        {it.status === 'reserved' ? <span className="badges"><span className="badge b-line">{tt('Брондалды', 'Забронировано')}</span></span> : null}
      </span>
    </button>
  );
}

function MarketGrid() {
  const { tt, L, go } = useApp();
  const acc = useAccount();
  const cfg = useConfig();
  const [uni, setUni] = useState<'own' | 'all'>('own');
  const [cat, setCat] = useState('all');
  const [cond, setCond] = useState('all');
  const [more, setMore] = useState<MarketItem[]>([]);
  const [next, setNext] = useState<number | undefined>(undefined);
  const own = !!acc.user?.university;
  const qs = new URLSearchParams();
  if (!own || uni === 'all') qs.set('university', 'all');
  if (cat !== 'all') qs.set('category', cat);
  if (cond !== 'all') qs.set('condition', cond);
  const path = acc.loaded ? '/api/market/items?' + qs.toString() : null;
  const { data, err } = useLoad<MarketList>(path);
  const [pageKey, setPageKey] = useState('');
  if (pageKey !== (path ?? '')) { setPageKey(path ?? ''); setMore([]); setNext(undefined); }
  const items = [...(data?.items ?? []), ...more];
  const nextBefore = next ?? (more.length ? undefined : data?.nextBefore);
  const loadMore = async () => {
    if (!nextBefore || !path) return;
    try {
      const r = await api<MarketList>(path + '&before=' + nextBefore);
      setMore(m => [...m, ...r.items]); setNext(r.nextBefore ?? 0);
    } catch { /* keep what we have */ }
  };
  const season = data?.season ?? cfg.marketSeason;
  return (
    <>
      {season?.active ? <div className="card banner"><b>{tt('Көшіп жатырсың ба? Заттарыңды жаңа студенттерге бер', 'Переезжаешь? Отдай вещи новым студентам')}</b>
        <button className="btn secondary" type="button" onClick={() => go('market:new')}>{tt('Зат беру', 'Отдать вещь')}</button></div> : null}
      <div className="btn-row">
        <button className="btn" type="button" onClick={() => go('market:new')}><Icon n="plus" s={18} /> {tt('Зат беру', 'Отдать вещь')}</button>
        {acc.user ? <button className="btn ghost" type="button" onClick={() => go('market:mine')}>{tt('Менің заттарым', 'Мои вещи')}</button> : null}
      </div>
      {own ? <div className="chips">
        <Chip pressed={uni === 'own'} onClick={() => setUni('own')}>{tt('Менің университетім', 'Мой университет')}</Chip>
        <Chip pressed={uni === 'all'} onClick={() => setUni('all')}>{tt('Барлық университет', 'Все университеты')}</Chip>
      </div> : null}
      <div className="chips">
        <Chip pressed={cat === 'all'} onClick={() => setCat('all')}>{tt('Барлығы', 'Все')}</Chip>
        {Object.entries(MARKET_CATS).map(([k, c]) => <Chip key={k} pressed={cat === k} onClick={() => setCat(k)}>{L(c)}</Chip>)}
      </div>
      <div className="field"><label htmlFor="mkCond">{tt('Жағдайы', 'Состояние')}</label>
        <select className="input" id="mkCond" value={cond} onChange={e => setCond(e.target.value)}>
          <option value="all">{tt('Кез келген', 'Любое')}</option>
          {Object.entries(MARKET_CONDITIONS).map(([k, c]) => <option key={k} value={k}>{L(c)}</option>)}
        </select></div>
      <ErrorLine err={err} />
      {!data && !err ? <Loading /> : items.length ? (
        <>
          <div className="mk-grid">{items.map(it => <MarketCard key={it.id} it={it} />)}</div>
          {nextBefore ? <button className="btn ghost block" type="button" onClick={loadMore}>{tt('Тағы көрсету', 'Показать ещё')}</button> : null}
        </>
      ) : data ? (
        <Empty icon="gift" action={<button className="btn secondary" type="button" onClick={() => go('market:new')}>{tt('Зат беру', 'Отдать вещь')}</button>}>
          {tt('Әзірге зат жоқ. Семестр соңында көп болады — керек емес заттарыңды бірінші болып бер.', 'Пока вещей нет. В конце семестра их будет больше — отдай ненужное первым.')}
        </Empty>
      ) : null}
      <p className="note">{tt('Тек тегін. Сату, дәрі-дәрмек, алкоголь, темекі, құжаттар, жануарлар, қару, ақша мен карталар — тыйым салынған.', 'Только бесплатно. Продажа, лекарства, алкоголь, табак, документы, животные, оружие, деньги и карты — запрещены.')}</p>
    </>
  );
}

function MarketItemView({ id }: { id: string }) {
  const { tt, L, go, openSheet } = useApp();
  const acc = useAccount();
  const need = useNeedAccount();
  const { data, err, code } = useLoad<{ item: MarketItem }>(acc.loaded ? `/api/market/items/${id}${acc.user ? '?u=' + acc.user.id : ''}` : null);
  if (code === 'item_not_found' || code === 'not_found') return <NotFound />;
  if (err) return <ErrorLine err={err} />;
  const it = data?.item;
  if (!it) return <Loading />;
  return (
    <>
      {it.photos.length ? <div className="mk-photos">{it.photos.map((p, i) => <img key={i} src={p} alt={`${it.title} ${i + 1}`} />)}</div> : null}
      <div className="detail-head"><h1>{it.title}</h1>
        <div className="badges">
          <span className="badge b-ok">{tt('Тегін', 'Бесплатно')}</span>
          <span className="badge b-line">{L(MARKET_CATS[it.category])}</span>
          <span className="badge b-line">{L(MARKET_CONDITIONS[it.condition])}</span>
          {it.status === 'reserved' ? <span className="badge b-sun">{tt('Брондалды', 'Забронировано')}</span> : null}
        </div></div>
      {it.desc ? <p style={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>{it.desc}</p> : null}
      <div className="group">
        <Row icon="pin" title={it.pickup || tt('Университет аумағында', 'На территории университета')} sub={it.university} chev={false} />
        <Row icon="clock" title={agoDays(it.daysAgo, tt)} sub={tt(`${ymd(it.expiresAt)} дейін`, `до ${ymd(it.expiresAt)}`)} chev={false} />
        {it.author ? <Row icon="user" title={it.author.name} sub={it.author.username ? '@' + it.author.username : ''} chev={false} /> : null}
      </div>
      {it.mine ? <button className="btn secondary block" type="button" onClick={() => go('market:mine')}>{tt('Менің заттарым', 'Мои вещи')}</button>
        : it.author?.telegramUrl ? <button className="btn block" type="button" onClick={() => openTelegramLink(it.author!.telegramUrl!)}><Icon n="send" s={18} /> {tt('Жазу', 'Написать')}</button>
          : !it.author ? <SignInCard text={tt('Жариялаушының аты, Telegram-ы және нақты алу орны тек кірген студенттерге көрінеді.', 'Имя, Telegram и точное место видны только вошедшим студентам.')} /> : null}
      <p className="note">{tt('Кездесу тек университет аумағында не адам көп жерде. Ақша сұраса — шағымданыңыз.', 'Встречайтесь только на территории вуза или в людном месте. Если просят деньги — пожалуйтесь.')}</p>
      {!it.mine ? <button className="link-btn" type="button" style={{ alignSelf: 'flex-start' }} onClick={() => { if (need()) openSheet(<MarketReportSheet id={it.id} />); }}>{tt('Шағымдану', 'Пожаловаться')}</button> : null}
    </>
  );
}

function MarketReportSheet({ id }: { id: number }) {
  const { tt, L, lang, toast, closeSheet } = useApp();
  const [reason, setReason] = useState('');
  const [text, setText] = useState('');
  const [err, setErr] = useState('');
  const [busy, setBusy] = useState(false);
  return (
    <form className="form" onSubmit={async e => {
      e.preventDefault();
      if (!reason) return;
      setBusy(true); setErr('');
      try {
        const r = await api<{ alreadyReported: boolean; hidden: boolean }>(`/api/market/items/${id}/report`, { method: 'POST', body: { reason, text: text.trim() }, lang });
        closeSheet();
        toast(r.alreadyReported ? tt('Сіз бұрын шағымдандыңыз', 'Вы уже жаловались') : tt('Шағым жіберілді. Рақмет!', 'Жалоба отправлена. Спасибо!'));
      } catch (e2) { setErr(errText(e2)); } finally { setBusy(false); }
    }}>
      <h2>{tt('Шағымдану', 'Пожаловаться')}</h2>
      <div className="chips wrap" role="radiogroup">{Object.entries(MARKET_REPORT_REASONS).map(([k, r]) => <Chip key={k} pressed={reason === k} onClick={() => setReason(k)}>{L(r)}</Chip>)}</div>
      <div className="field"><label htmlFor="mkRepText">{tt('Толығырақ (міндетті емес)', 'Подробнее (необязательно)')}</label>
        <textarea className="input" id="mkRepText" maxLength={300} value={text} onChange={e => setText(e.target.value)} /></div>
      <ErrorLine err={err} />
      <div className="btn-row">
        <button className="btn ghost" type="button" onClick={closeSheet}>{tt('Бас тарту', 'Отмена')}</button>
        <button className="btn" type="submit" disabled={!reason || busy}>{tt('Жіберу', 'Отправить')}</button>
      </div>
    </form>
  );
}

/* ---------- publish ---------- */
interface Uploaded { name: string; url: string }

function MarketNew() {
  const { tt, L, lang, toast, go } = useApp();
  const acc = useAccount();
  const me = useLoad<MarketMe>(hasAccount(acc) ? '/api/market/me' : null);
  const [photos, setPhotos] = useState<Uploaded[]>([]);
  const [up, setUp] = useState(0);
  const [f, setF] = useState({ title: '', desc: '', category: '', condition: '', pickup: '' });
  const [err, setErr] = useState('');
  const [busy, setBusy] = useState(false);
  const [pending, setPending] = useState(false);
  const fileRef = useRef<HTMLInputElement>(null);
  if (!acc.loaded) return <Loading />;
  if (!hasAccount(acc)) return <SignInCard text={tt('Зат беру үшін Telegram арқылы кіріңіз: алушылар сізге Telegram-да жазады.', 'Чтобы отдать вещь, войдите через Telegram: вам напишут в Telegram.')} />;
  if (me.err) return <ErrorLine err={me.err} />;
  if (!me.data) return <Loading />;
  if (pending) {
    return <Empty icon="clock" action={<button className="btn secondary" type="button" onClick={() => go('market:mine')}>{tt('Менің заттарым', 'Мои вещи')}</button>}>
      {tt('Затыңыз тексерілуде: жаңа аккаунттың алғашқы заты әкімші қарағаннан кейін көрінеді.', 'Вещь на проверке: первая вещь нового аккаунта появится после просмотра администратором.')}</Empty>;
  }
  const b = me.data.blockers;
  if (b.includes('no_rules') && b.filter(x => x !== 'no_rules' && x !== 'daily_limit').length === 0) return <MarketRules onAccept={me.reload} />;
  const block = b.find(x => x !== 'no_rules');
  if (block) return <MarketBlocked code={block} me={me.data} />;
  const pick = async (files: FileList | null) => {
    if (!files) return;
    setErr('');
    for (const file of Array.from(files).slice(0, MARKET_LIMITS.photos - photos.length)) {
      setUp(n => n + 1);
      try {
        const blob = await shrinkPhoto(file);
        const fd = new FormData();
        fd.append('photo', blob, 'photo.jpg');
        const r = await apiUpload<Uploaded>('/api/market/photos', fd, lang);
        setPhotos(p => (p.length < MARKET_LIMITS.photos ? [...p, { name: r.name, url: r.url }] : p));
      } catch (e) { setErr(errText(e, tt('Фотоны оқу мүмкін болмады', 'Не удалось прочитать фото'))); } finally { setUp(n => n - 1); }
    }
    if (fileRef.current) fileRef.current.value = '';
  };
  const ok = f.title.trim().length >= 1 && f.category && f.condition && !up;
  const set = (k: keyof typeof f, v: string) => setF(x => ({ ...x, [k]: v }));
  return (
    <form className="form" onSubmit={async e => {
      e.preventDefault();
      if (!ok || busy) return;
      setBusy(true); setErr('');
      try {
        const r = await api<{ item: MarketItem; pending: boolean }>('/api/market/items', {
          method: 'POST', lang,
          body: { title: f.title.trim(), desc: f.desc.trim(), category: f.category, condition: f.condition, pickup: f.pickup.trim(), photos: photos.map(p => p.name) },
        });
        if (r.pending) setPending(true);
        else { toast(tt('Жарияланды', 'Опубликовано')); go('market:' + r.item.id, { replace: true }); }
      } catch (e2) { setErr(errText(e2)); me.reload(); } finally { setBusy(false); }
    }}>
      <div className="field"><span className="lbl">{tt(`Фото (${MARKET_LIMITS.photos}-ке дейін)`, `Фото (до ${MARKET_LIMITS.photos})`)}</span>
        <div className="mk-thumbs">
          {photos.map((p, i) => (
            <span key={p.name} className="mk-thumb"><img src={p.url} alt={tt(`${i + 1}-фото`, `Фото ${i + 1}`)} />
              <button type="button" className="mk-x" aria-label={tt('Фотоны алып тастау', 'Убрать фото')} onClick={() => setPhotos(ps => ps.filter(x => x.name !== p.name))}><Icon n="x" s={16} /></button></span>
          ))}
          {photos.length + up < MARKET_LIMITS.photos ? (
            <label className="mk-thumb mk-add"><Icon n="plus" s={24} /><span className="tiny">{up ? tt('Жүктелуде…', 'Загрузка…') : tt('Қосу', 'Добавить')}</span>
              <input ref={fileRef} type="file" accept="image/jpeg,image/png,image/webp,image/*" multiple onChange={e => void pick(e.target.files)} className="visually-hidden" /></label>
          ) : null}
        </div>
        <span className="tiny muted">{tt('Фото телефонда кішірейтіледі; орналасқан жер (GPS) деректері өшіріледі.', 'Фото уменьшается на телефоне; данные о месте съёмки (GPS) удаляются.')}</span></div>
      <div className="field"><label htmlFor="mkTitle">{tt('Атауы', 'Название')}</label>
        <input className="input" id="mkTitle" maxLength={MARKET_LIMITS.title} value={f.title} onChange={e => set('title', e.target.value)} required />
        <span className="tiny muted num">{f.title.length} / {MARKET_LIMITS.title}</span></div>
      <div className="grid-2">
        <div className="field"><label htmlFor="mkCat">{tt('Санаты', 'Категория')}</label>
          <select className="input" id="mkCat" value={f.category} onChange={e => set('category', e.target.value)} required>
            <option value="">{tt('Таңдаңыз', 'Выберите')}</option>
            {Object.entries(MARKET_CATS).map(([k, c]) => <option key={k} value={k}>{L(c)}</option>)}
          </select></div>
        <div className="field"><label htmlFor="mkCondNew">{tt('Жағдайы', 'Состояние')}</label>
          <select className="input" id="mkCondNew" value={f.condition} onChange={e => set('condition', e.target.value)} required>
            <option value="">{tt('Таңдаңыз', 'Выберите')}</option>
            {Object.entries(MARKET_CONDITIONS).map(([k, c]) => <option key={k} value={k}>{L(c)}</option>)}
          </select></div>
      </div>
      <div className="field"><label htmlFor="mkDesc">{tt('Сипаттама', 'Описание')}</label>
        <textarea className="input" id="mkDesc" maxLength={MARKET_LIMITS.desc} value={f.desc} onChange={e => set('desc', e.target.value)} />
        <span className="tiny muted num">{f.desc.length} / {MARKET_LIMITS.desc}</span></div>
      <div className="field"><label htmlFor="mkPickup">{tt('Қай жерден алуға болады', 'Где забрать')}</label>
        <input className="input" id="mkPickup" maxLength={MARKET_LIMITS.pickup} placeholder={tt('Мысалы: 4-корпус, кіреберіс', 'Например: 4-й корпус, вход')} value={f.pickup} onChange={e => set('pickup', e.target.value)} />
        <span className="tiny muted">{tt('Нақты орынды тек кірген студенттер көреді.', 'Точное место видят только вошедшие студенты.')}</span></div>
      <p className="note">{tt(`Бүгін: ${me.data.today} / ${me.data.dailyLimit}. Зат тегін беріледі — баға жазылмайды.`, `Сегодня: ${me.data.today} из ${me.data.dailyLimit}. Вещь отдаётся бесплатно — цену не указывают.`)}</p>
      <ErrorLine err={err} />
      <button className="btn block" type="submit" disabled={!ok || busy}>{tt('Жариялау', 'Опубликовать')}</button>
    </form>
  );
}

function MarketRules({ onAccept }: { onAccept: () => void }) {
  const { tt, lang } = useApp();
  const [agree, setAgree] = useState(false);
  const [err, setErr] = useState('');
  return (
    <>
      <div className="consent">
        <h3>{tt('Тек тегін', 'Только бесплатно')}</h3>
        <p className="small">{tt('Баға, «символдық ақы», айырбас — жоқ. Сату ұсынысы шағым арқылы өшіріледі.', 'Никаких цен, «символической платы» и обмена. Предложения о продаже удаляются по жалобам.')}</p>
        <h3>{tt('Тыйым салынған заттар', 'Запрещённые вещи')}</h3>
        <p className="small">{tt('Дәрі-дәрмек, алкоголь мен темекі, құжаттар мен студенттік билеттер, жануарлар, қару, ақша мен төлем карталары.', 'Лекарства, алкоголь и табак, документы и студенческие билеты, животные, оружие, деньги и платёжные карты.')}</p>
        <h3>{tt('Кездесу', 'Встреча')}</h3>
        <p className="small">{tt('Тек университет аумағында не адам көп жерде. Жеке қаржылық деректерді ешкімге бермеңіз.', 'Только на территории университета или в людном месте. Никому не передавайте личные финансовые данные.')}</p>
      </div>
      <label className="check"><input type="checkbox" checked={agree} onChange={e => setAgree(e.target.checked)} /><span>{tt('Ережелермен келісемін', 'Согласен(на) с правилами')}</span></label>
      <ErrorLine err={err} />
      <button className="btn block" type="button" disabled={!agree} onClick={async () => {
        try { await api('/api/market/rules/accept', { method: 'POST', lang }); onAccept(); } catch (e) { setErr(errText(e)); }
      }}>{tt('Жалғастыру', 'Продолжить')}</button>
    </>
  );
}

function MarketBlocked({ code, me }: { code: string; me: MarketMe }) {
  const { tt, go } = useApp();
  const need = useNeedAccount();
  const t: Record<string, [string, string]> = {
    consent_required: ['Аккаунтты қолдану үшін келісім керек.', 'Чтобы пользоваться аккаунтом, нужно согласие.'],
    no_account: ['Профильде университет көрсетілмеген. Баптаулар → «Аккаунтпен синхрондау» басыңыз.', 'В профиле не указан университет. Нажмите Настройки → «Синхронизировать с аккаунтом».'],
    no_username: ['Сатып алушы сізге жаза алуы үшін Telegram-да username орнатыңыз. Сосын шығып, қайта кіріңіз — username жаңарады.', 'Чтобы вам могли написать, установите username в Telegram. Затем выйдите и войдите снова — username обновится.'],
    minor: ['Зат жариялау 18 жастан бастап. Заттарды қарап, жариялаушыға жаза аласыз.', 'Публиковать вещи можно с 18 лет. Смотреть вещи и писать авторам — можно.'],
    banned: [`Жариялау уақытша тоқтатылған${me.banUntil ? ': ' + ymd(me.banUntil) + ' дейін' : ''}.`, `Публикация временно приостановлена${me.banUntil ? ': до ' + ymd(me.banUntil) : ''}.`],
    daily_limit: [`Бүгінге шек: күніне ${me.dailyLimit} зат. Ертең қайталаңыз.`, `Лимит на сегодня: ${me.dailyLimit} вещи в день. Попробуйте завтра.`],
  };
  const [kk, ru] = t[code] ?? ['Әзірге жариялау мүмкін емес.', 'Пока опубликовать нельзя.'];
  return (
    <div className="card" role="status">
      <p className="small">{tt(kk, ru)}</p>
      {code === 'consent_required' ? <button className="btn" type="button" onClick={need}>{tt('Келісім беру', 'Дать согласие')}</button> : null}
      {code === 'no_account' || code === 'no_username' ? <button className="btn secondary" type="button" onClick={() => go('settings')}>{tt('Баптауларға өту', 'Открыть настройки')}</button> : null}
    </div>
  );
}

/* ---------- my items ---------- */
function MarketMine() {
  const { tt, L, lang, toast, go, openSheet, closeSheet } = useApp();
  const acc = useAccount();
  const { data, err, reload } = useLoad<{ items: MarketItem[] }>(acc.user ? '/api/market/mine' : null);
  const [busy, setBusy] = useState(0);
  if (!acc.loaded) return <Loading />;
  if (!acc.user) return <SignInCard text={tt('Заттарыңызды көру үшін кіріңіз.', 'Войдите, чтобы увидеть свои вещи.')} />;
  if (err) return <ErrorLine err={err} />;
  if (!data) return <Loading />;
  const run = async (id: number, fn: () => Promise<unknown>, done: string) => {
    setBusy(id);
    try { await fn(); if (done) toast(done); reload(); } catch (e) { toast(errText(e)); } finally { setBusy(0); }
  };
  const status = (it: MarketItem) => {
    switch (it.status) {
      case 'active': return [tt('Белсенді', 'Активно'), 'b-ok'];
      case 'reserved': return [tt('Брондалды', 'Забронировано'), 'b-sun'];
      case 'given': return [tt('Берілді', 'Отдано'), 'b-ok'];
      case 'pending': return [tt('Тексерілуде', 'На проверке'), 'b-line'];
      case 'expired': return [tt('Мерзімі бітті', 'Срок истёк'), 'b-line'];
      default: return [tt('Жасырылған', 'Скрыто'), 'b-bad'];
    }
  };
  const note = (it: MarketItem) => it.status === 'hidden' ? (it.hiddenReason === 'reports' ? tt('Шағымдар бойынша жасырылды — әкімші тексереді.', 'Скрыто по жалобам — проверит администратор.') : tt('Әкімші жасырды.', 'Скрыто администратором.'))
    : it.status === 'pending' ? tt('Жаңа аккаунттың алғашқы заты — әкімші қарағаннан кейін көрінеді.', 'Первая вещь нового аккаунта — появится после проверки.')
      : it.status === 'expired' ? tt('30 күн өтті. Әлі берсеңіз — қайта жариялаңыз.', 'Прошло 30 дней. Если ещё отдаёте — опубликуйте заново.') : '';
  const editable = (it: MarketItem) => it.status === 'active' || it.status === 'reserved';
  return (
    <>
      <button className="btn block" type="button" onClick={() => go('market:new')}><Icon n="plus" s={18} /> {tt('Зат беру', 'Отдать вещь')}</button>
      {data.items.length ? data.items.map(it => {
        const [sl, sc] = status(it);
        return (
          <div key={it.id} className="card">
            <div className="card-row">
              <span className="mk-mini">{it.photos[0] ? <img src={it.photos[0]} alt="" /> : <Icon n="gift" s={22} />}</span>
              <div className="row-main">
                <span className="row-title">{it.title}</span>
                <span className="tiny muted">{L(MARKET_CATS[it.category])} · {agoDays(it.daysAgo, tt)}{editable(it) ? ' · ' + tt(`${ymd(it.expiresAt)} дейін`, `до ${ymd(it.expiresAt)}`) : ''}</span>
                <span className="badges"><span className={`badge ${sc}`}>{sl}</span></span>
              </div>
            </div>
            {note(it) ? <p className="tiny muted">{note(it)}</p> : null}
            {editable(it) ? (
              <div className="btn-row">
                <button className="btn secondary" type="button" disabled={busy === it.id} onClick={() => run(it.id, () => api(`/api/market/items/${it.id}`, { method: 'PATCH', body: { status: it.status === 'reserved' ? 'active' : 'reserved' }, lang }), '')}>
                  {it.status === 'reserved' ? tt('Броннан алу', 'Снять бронь') : tt('Брондалды', 'Забронировано')}</button>
                <button className="btn" type="button" disabled={busy === it.id} onClick={() => run(it.id, async () => {
                  const r = await api<{ badgeAwarded: boolean }>(`/api/market/items/${it.id}/given`, { method: 'POST', lang });
                  toast(r.badgeAwarded ? tt('Берілді! Сізге «Жомарт» белгісі берілді', 'Отдано! Вам присвоен значок «Щедрый»') : tt('Берілді. Рақмет!', 'Отдано. Спасибо!'));
                }, '')}><Icon n="check" s={18} /> {tt('Берілді', 'Отдано')}</button>
                <button className="btn ghost" type="button" disabled={busy === it.id} onClick={() => run(it.id, () => api(`/api/market/items/${it.id}/extend`, { method: 'POST', lang }), tt('30 күнге ұзартылды', 'Продлено на 30 дней'))}>{tt('Ұзарту', 'Продлить')}</button>
                <button className="btn danger" type="button" disabled={busy === it.id} onClick={() => openSheet(
                  <>
                    <h2>{tt('Затты өшіру керек пе?', 'Удалить вещь?')}</h2>
                    <div className="btn-row">
                      <button className="btn ghost" type="button" onClick={closeSheet}>{tt('Жоқ', 'Нет')}</button>
                      <button className="btn danger" type="button" onClick={() => { closeSheet(); void run(it.id, () => api(`/api/market/items/${it.id}`, { method: 'DELETE', lang }), tt('Өшірілді', 'Удалено')); }}>{tt('Иә, жою', 'Да, удалить')}</button>
                    </div>
                  </>,
                )}>{tt('Өшіру', 'Удалить')}</button>
              </div>
            ) : null}
          </div>
        );
      }) : <Empty icon="gift">{tt('Сіз әлі зат жарияламадыңыз.', 'Вы ещё ничего не публиковали.')}</Empty>}
    </>
  );
}

/* ---------- search: a separate «Тегін заттар» block ---------- */
export function MarketSearchResults({ q }: { q: string }) {
  const { tt, L, go } = useApp();
  const on = useFeature('market');
  const query = q.trim();
  const [debounced, setDebounced] = useState(query);
  useEffect(() => { const t = setTimeout(() => setDebounced(query), 300); return () => clearTimeout(t); }, [query]);
  const { data } = useLoad<MarketList>(on && debounced ? `/api/market/items?university=all&limit=12&q=${encodeURIComponent(debounced.slice(0, 64))}` : null);
  if (!on || !debounced || !data?.items.length) return null;
  const rows = marketSearchItems(data.items, L, tt('Тегін', 'Бесплатно'));
  return (
    <section className="section">
      <SectionHead title={tt('Тегін заттар', 'Отдам даром')} />
      <div className="group">{rows.map(r => <Row key={r.id} icon="gift" title={r.org} sub={r.title} end={<span className="badge b-ok">{r.meta}</span>} onClick={() => go(r.go)} />)}</div>
    </section>
  );
}
