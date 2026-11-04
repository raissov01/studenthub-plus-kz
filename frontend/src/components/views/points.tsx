'use client';
/* «Ұпайлар / Баллы» (docs/api/points.md): level and XP bar, daily streak with a purchasable freeze, coins, today's
   tasks, weekly leaderboards, the rewards shop and the student's requests; the compact home chip; the admin screen
   (redemption queue, rewards catalog, per-student ledger with audited manual adjustments).
   Every number is the server's. Coins are not money: no cash-out, no transfers. */
import { useCallback, useEffect, useRef, useState } from 'react';
import { copyText, useApp } from '@/components/app';
import { Icon, Row, SectionHead, Seg } from '@/components/ui';
import { AdminOnly, ErrorLine, Feature, Loading, ReasonSheet, SignInCard, errCode, errText, hasAccount, useFeature, useLoad } from '@/components/views/common';
import { api, useAccount } from '@/lib/account';
import {
  almatyToday, awardText, levelPct, newKey, plural, pointsChanged, POINTS_CHANGED,
  type Leaderboard, type PointsReply, type PointsSummary, type PointsTask, type Redemption, type Reward, type RewardKind,
} from '@/lib/points';

type TT = (kk: string, ru: string) => string;

const coinsWord = (n: number, tt: TT) => tt('монета', plural(n, 'монета', 'монеты', 'монет'));
const daysWord = (n: number, tt: TT) => tt('күн', plural(n, 'день', 'дня', 'дней'));

function taskTitle(id: string, tt: TT): string {
  switch (id) {
    case 'daily_open': return tt('Қосымшаны бүгін ашу', 'Открыть приложение сегодня');
    case 'quiz': return tt('Біреудің квизін өту', 'Пройти чужой квиз');
    case 'event_checkin': return tt('Іс-шараға QR арқылы келу', 'Отметиться на мероприятии по QR');
    case 'roadmap_step': return tt('Даму жоспарының қадамы', 'Шаг плана развития');
    default: return id;
  }
}
const TASK_GO: Record<string, string> = { quiz: 'quiz', event_checkin: 'opps:events', roadmap_step: 'roadmap' };

export function reasonLabel(reason: string, tt: TT): string {
  const base = reason.replace(/_revoked$/, '');
  const map: Record<string, string> = {
    daily_open: tt('Күнделікті кіру', 'Ежедневный вход'), quiz: tt('Квиз', 'Квиз'), event_checkin: tt('Іс-шара', 'Мероприятие'),
    roadmap_step: tt('Даму жоспары', 'План развития'), freeze_purchase: tt('Серия қорғанысы', 'Защита серии'),
    redeem: tt('Сыйлыққа өтінім', 'Заявка на награду'), redeem_refund: tt('Өтінім қабылданбады — қайтарылды', 'Заявка отклонена — возврат'),
    admin_adjust: tt('Әкімші түзетуі', 'Корректировка администратора'),
  };
  const label = map[base] ?? base;
  return base !== reason ? label + tt(' (кері алынды)', ' (отменено)') : label;
}

export function kindLabel(k: RewardKind | string, tt: TT): string {
  return ({
    discount: tt('Серіктес жеңілдігі', 'Скидка партнёра'), merch: tt('Университет мерчі', 'Мерч университета'),
    print_quota: tt('Қосымша басып шығару', 'Доп. квота печати'), priority_event: tt('Іс-шараға басым тіркелу', 'Приоритетная запись на мероприятие'),
  } as Record<string, string>)[k] ?? k;
}

function statusBadge(s: Redemption['status'], tt: TT) {
  if (s === 'approved') return <span className="badge b-ok">{tt('Бекітілді', 'Одобрено')}</span>;
  if (s === 'rejected') return <span className="badge b-bad">{tt('Қабылданбады', 'Отклонено')}</span>;
  return <span className="badge b-line">{tt('Қаралуда', 'На рассмотрении')}</span>;
}

/* ---------- the compact home row ---------- */

const DAILY_KEY = 'studenthub.points.daily';
let dailyInFlight = '';

/** One row under the home hero: level, streak, coins. Also sends the once-a-day «first open». Signed in only. */
export function PointsChip() {
  const { tt, lang, go, toast } = useApp();
  const on = useFeature('points');
  const acc = useAccount();
  const ready = on && hasAccount(acc);
  const uid = acc.user?.id ?? 0;
  const [s, setS] = useState<PointsSummary | null>(null);
  useEffect(() => {
    if (!ready) return;
    let live = true;
    const load = () => { api<PointsSummary>('/api/points/me', { lang }).then(v => { if (live) setS(v); }, () => {}); };
    const mark = `${uid}:${almatyToday()}`;
    let seen = '';
    try { seen = localStorage.getItem(DAILY_KEY) || ''; } catch { /* private mode */ }
    if (seen !== mark && dailyInFlight !== mark) {
      // one request per device and Almaty day, even when the chip remounts (language switch) meanwhile
      dailyInFlight = mark;
      api<PointsReply>('/api/points/daily', { method: 'POST', lang }).then(r => {
        try { localStorage.setItem(DAILY_KEY, mark); } catch { /* ignore */ }
        const text = awardText(r.award, tt);
        if (text) toast(text);
        if (live) setS(r.me); else pointsChanged();
      }, () => { dailyInFlight = ''; load(); });
    } else load();
    window.addEventListener(POINTS_CHANGED, load);
    return () => { live = false; window.removeEventListener(POINTS_CHANGED, load); };
  }, [ready, uid, lang, tt, toast]);
  if (!ready || !s) return null;
  const label = tt(`Ұпайлар: ${s.level}-деңгей, ${s.streak.days} күн қатарынан, ${s.coins} монета`,
    `Баллы: уровень ${s.level}, ${s.streak.days} ${daysWord(s.streak.days, tt)} подряд, ${s.coins} ${coinsWord(s.coins, tt)}`);
  return (
    <button type="button" className="pts-chip" id="pointsChip" onClick={() => go('points')} aria-label={label}>
      <span className="pts-chip-i"><Icon n="trophy" s={16} /><b className="num">{s.level}</b><span className="tiny muted">{tt('деңгей', 'ур.')}</span></span>
      <span className="pts-chip-i"><Icon n="streak" s={16} /><b className="num">{s.streak.days}</b><span className="tiny muted">{tt('күн', 'дн.')}</span></span>
      <span className="pts-chip-i"><Icon n="coin" s={16} /><b className="num">{s.coins}</b><span className="tiny muted">{tt('монета', 'мон.')}</span></span>
      <Icon n="chev" s={16} className="pts-chip-chev" />
    </button>
  );
}

/* ---------- the student screen ---------- */

export function Points() {
  return <Feature name="points"><PointsGate /></Feature>;
}

function PointsGate() {
  const { tt } = useApp();
  const acc = useAccount();
  if (!acc.loaded) return <Loading />;
  if (!hasAccount(acc)) {
    return (
      <>
        <PointsRules />
        <SignInCard text={tt('Ұпай мен монета жинау үшін аккаунтқа кіріңіз. Олар серверде сақталады — құрылғы ауысса да жоғалмайды.',
          'Чтобы копить баллы и монеты, войдите в аккаунт. Они хранятся на сервере и не пропадут при смене устройства.')} />
      </>
    );
  }
  return <PointsHome />;
}

function PointsRules() {
  const { tt } = useApp();
  return (
    <div className="card">
      <h2>{tt('Ұпайлар қалай жиналады', 'Как копятся баллы')}</h2>
      <p className="small muted">{tt('Квиз өту, іс-шараға QR арқылы келу және күн сайын кіру — XP мен монета береді. Монетаны жеңілдік, мерч сияқты сыйлықтарға айырбастауға болады.',
        'Квизы, отметка на мероприятиях по QR и ежедневный вход дают XP и монеты. Монеты можно обменять на награды: скидки, мерч.')}</p>
    </div>
  );
}

function PointsHome() {
  const { tt, lang } = useApp();
  const me = useLoad<PointsSummary>('/api/points/me');
  const [fresh, setFresh] = useState<PointsSummary | null>(null);
  const [tab, setTab] = useState('top');
  const s = fresh ?? me.data;
  const apply = useCallback((v: PointsSummary) => { setFresh(v); pointsChanged(); }, []);
  if (!s) return me.err ? <ErrorLine err={me.err} /> : <Loading />;
  return (
    <div className="pts">
      <LevelCard s={s} />
      <StreakCard s={s} onChange={apply} />
      <section className="section" aria-labelledby="ptsToday">
        <SectionHead title={<span id="ptsToday">{tt('Бүгінгі тапсырмалар', 'Задания на сегодня')}</span>} />
        <p className="tiny muted num">{tt(`Бүгін: ${s.today.xp}/${s.today.xpCap} XP, ${s.today.coins}/${s.today.coinsCap} монета`,
          `Сегодня: ${s.today.xp}/${s.today.xpCap} XP, ${s.today.coins}/${s.today.coinsCap} ${coinsWord(s.today.coinsCap, tt)}`)}</p>
        <div className="group">{s.tasks.map(t => <TaskRow key={t.id} t={t} />)}</div>
      </section>
      <Seg items={[['top', tt('Рейтинг', 'Рейтинг')], ['shop', tt('Дүкен', 'Магазин')], ['mine', tt('Өтінімдерім', 'Мои заявки')]]} cur={tab} onPick={setTab} />
      <div role="tabpanel" aria-label={tab === 'top' ? tt('Рейтинг', 'Рейтинг') : tab === 'shop' ? tt('Дүкен', 'Магазин') : tt('Өтінімдерім', 'Мои заявки')}>
        {tab === 'top' ? <Leaders s={s} onChange={apply} /> : tab === 'shop' ? <Shop s={s} onChange={apply} onDone={() => setTab('mine')} /> : <MyRedemptions />}
      </div>
      {s.recent.length ? (
        <section className="section">
          <SectionHead title={tt('Монета тарихы', 'История монет')} />
          <div className="group">{s.recent.map((e, i) => (
            <Row key={i} title={reasonLabel(e.reason, tt)} sub={new Date(e.createdAt).toLocaleDateString(lang === 'ru' ? 'ru-RU' : 'kk-KZ', { timeZone: 'Asia/Almaty' })}
              end={<b className={`num ${e.delta < 0 ? 'urgent' : ''}`}>{e.delta > 0 ? '+' : ''}{e.delta}</b>} chev={false} />
          ))}</div>
        </section>
      ) : null}
      <p className="note">{tt('Монета — ақша емес: оны ақшаға айырбастау не басқа студентке беру мүмкін емес. Бір әрекет үшін ұпай бір рет қана беріледі, күнделікті шегі бар.',
        'Монеты — не деньги: их нельзя обменять на деньги или передать другому студенту. За одно действие баллы начисляются один раз, есть дневной лимит.')}</p>
    </div>
  );
}

function LevelCard({ s }: { s: PointsSummary }) {
  const { tt } = useApp();
  const pct = levelPct(s);
  return (
    <div className="card pts-level">
      <div className="pts-level-top">
        <span className="pts-badge" aria-hidden="true"><Icon n="trophy" s={22} /></span>
        <div className="pts-level-main">
          <h2>{tt(`${s.level}-деңгей`, `Уровень ${s.level}`)}</h2>
          <span className="small muted num">{s.xp} XP · {tt(`келесі деңгейге ${s.nextLevelXp - s.xp} XP`, `до следующего ${s.nextLevelXp - s.xp} XP`)}</span>
        </div>
      </div>
      <span className="meter" role="progressbar" aria-valuenow={pct} aria-valuemin={0} aria-valuemax={100}
        aria-label={tt(`${s.level}-деңгей прогресі`, `Прогресс уровня ${s.level}`)}><i style={{ width: `${pct}%` }}></i></span>
      <div className="stat-grid pts-stats">
        <div className="stat"><b className="num" id="ptsCoins">{s.coins}</b><span><Icon n="coin" s={14} /> {coinsWord(s.coins, tt)}</span></div>
        <div className="stat"><b className="num" id="ptsStreak">{s.streak.days}</b><span><Icon n="streak" s={14} /> {tt('күн қатарынан', `${daysWord(s.streak.days, tt)} подряд`)}</span></div>
      </div>
    </div>
  );
}

function StreakCard({ s, onChange }: { s: PointsSummary; onChange: (v: PointsSummary) => void }) {
  const { tt, lang, toast } = useApp();
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  const key = useRef('');
  const st = s.streak;
  const canBuy = st.freezes < st.maxFreezes;
  const buy = async () => {
    if (busy) return;
    key.current ||= newKey();
    setBusy(true); setErr('');
    try {
      const r = await api<PointsReply>('/api/points/freeze', { method: 'POST', body: { key: key.current }, lang });
      key.current = '';
      onChange(r.me);
      toast(tt('Қорғаныс сатып алынды', 'Защита куплена'));
    } catch (e) {
      if (errCode(e) !== 'network' && errCode(e) !== 'offline') key.current = '';
      setErr(errText(e));
    } finally { setBusy(false); }
  };
  return (
    <section className="section">
      <SectionHead title={tt('Серия', 'Серия')} />
      <div className="card">
        <p className="small">{st.activeToday
          ? tt('Бүгін белсенді болдыңыз — серия сақталды.', 'Сегодня вы были активны — серия сохранена.')
          : tt('Серияны сақтау үшін бүгін бір тапсырма орындаңыз.', 'Чтобы сохранить серию, выполните сегодня одно задание.')}</p>
        <p className="tiny muted num">{tt(`Ең ұзын серия: ${st.best} күн. Қорғаныс: ${st.freezes}/${st.maxFreezes} — бір күн өткізіп алсаңыз, серия үзілмейді.`,
          `Лучшая серия: ${st.best} ${daysWord(st.best, tt)}. Защита: ${st.freezes}/${st.maxFreezes} — один пропущенный день не прервёт серию.`)}</p>
        {canBuy ? (
          <button className="btn secondary" type="button" onClick={buy} disabled={busy || s.coins < st.freezeCost}>
            <Icon n="shield" s={18} /> {tt(`Қорғаныс сатып алу — ${st.freezeCost} монета`, `Купить защиту — ${st.freezeCost} ${coinsWord(st.freezeCost, tt)}`)}
          </button>
        ) : null}
        <ErrorLine err={err} />
      </div>
    </section>
  );
}

function TaskRow({ t }: { t: PointsTask }) {
  const { tt } = useApp();
  const done = t.done >= t.perDay || (!!t.perWeek && t.week >= t.perWeek);
  const reward = `+${t.xp} XP` + (t.coins ? ' · ' + tt(`+${t.coins} монета`, `+${t.coins} ${coinsWord(t.coins, tt)}`) : '');
  const limit = t.perWeek ? tt(`бүгін ${t.done}/${t.perDay}, аптада ${t.week}/${t.perWeek}`, `сегодня ${t.done}/${t.perDay}, за неделю ${t.week}/${t.perWeek}`)
    : tt(`бүгін ${t.done}/${t.perDay}`, `сегодня ${t.done}/${t.perDay}`);
  const target = TASK_GO[t.id];
  return (
    <Row icon={done ? 'check' : 'target'} tone={done ? 'ok' : ''} title={taskTitle(t.id, tt)}
      sub={<span className="num">{reward} · {limit}{t.deviceOnly ? tt(' · тек XP', ' · только XP') : ''}</span>}
      go={!done && target ? target : undefined} chev={!done && !!target} />
  );
}

/* ---------- leaderboard ---------- */

function Leaders({ s, onChange }: { s: PointsSummary; onChange: (v: PointsSummary) => void }) {
  const { tt, lang } = useApp();
  const [scope, setScope] = useState<'group' | 'faculty'>('group');
  const [rev, setRev] = useState(0);
  const lb = useLoad<Leaderboard>(`/api/points/leaderboard?scope=${scope}&r=${rev}`);
  const [group, setGroup] = useState(s.settings.studyGroup ?? '');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  const save = async (body: { studyGroup?: string; leaderboardOptOut?: boolean }) => {
    setBusy(true); setErr('');
    try {
      const r = await api<PointsReply>('/api/points/settings', { method: 'PATCH', body, lang });
      onChange(r.me);
      setGroup(r.me.settings.studyGroup ?? '');
      setRev(n => n + 1);
    } catch (e) { setErr(errText(e)); } finally { setBusy(false); }
  };
  const d = lb.data;
  return (
    <section className="section">
      <Seg items={[['group', tt('Топ', 'Группа')], ['faculty', tt('Факультет', 'Факультет')]]} cur={scope} onPick={k => setScope(k as 'group' | 'faculty')} />
      <p className="tiny muted">{tt('Апталық XP. Дүйсенбі 00:00-де (Астана уақыты) нөлденеді.', 'XP за неделю. Обнуляется в понедельник в 00:00 (время Астаны).')}</p>
      {lb.loading && !d ? <Loading /> : null}
      <ErrorLine err={lb.err} />
      {d?.status === 'no_scope' ? (
        <p className="small muted">{scope === 'group'
          ? tt('Тобыңызды төменде жазыңыз — сонда топ рейтингі көрінеді.', 'Укажите свою группу ниже — тогда появится рейтинг группы.')
          : tt('Профильде университет пен факультетті көрсетіңіз (Баптаулар → аккаунт).', 'Укажите университет и факультет в профиле (Параметры → аккаунт).')}</p>
      ) : null}
      {d?.status === 'too_small' ? (
        <p className="small muted">{tt(`Рейтинг кемінде ${d.minMembers} адам болғанда ғана көрсетіледі — шағын топта адамдарды тану оңай.`,
          `Рейтинг показывается, только когда участников не меньше ${d.minMembers}: в маленькой группе людей легко узнать.`)}</p>
      ) : null}
      {d?.status === 'ok' && d.leaders ? (
        <ol className="group pts-board" aria-label={scope === 'group' ? tt('Топ рейтингі', 'Рейтинг группы') : tt('Факультет рейтингі', 'Рейтинг факультета')}>
          {d.leaders.map(l => (
            <li key={l.rank} className={`row${l.me ? ' pts-me' : ''}`} aria-current={l.me ? 'true' : undefined}>
              <span className="pts-rank num" aria-label={tt(`${l.rank}-орын`, `${l.rank} место`)}>{l.rank}</span>
              <span className="row-main"><span className="row-title">{l.name}{l.me ? tt(' (сіз)', ' (вы)') : ''}</span></span>
              <b className="num">{l.xp} XP</b>
            </li>
          ))}
        </ol>
      ) : null}
      {d?.optedOut ? <p className="tiny muted">{tt('Сіз рейтингте көрінбейсіз.', 'Вас не видно в рейтинге.')}</p> : null}
      <form className="form pts-settings" onSubmit={e => { e.preventDefault(); void save({ studyGroup: group }); }}>
        <div className="field">
          <label htmlFor="ptsGroup">{tt('Академиялық тобым', 'Моя академическая группа')}</label>
          <div className="pts-inline">
            <input className="input" id="ptsGroup" maxLength={24} autoComplete="off" placeholder={tt('мысалы, ИС-21', 'например, ИС-21')} value={group} onChange={e => setGroup(e.target.value)} />
            <button className="btn ghost" type="submit" disabled={busy}>{tt('Сақтау', 'Сохранить')}</button>
          </div>
        </div>
        <label className="row toggle-row">
          <span className="row-main"><span className="row-title">{tt('Рейтингте көрсетпеу', 'Не показывать меня в рейтинге')}</span>
            <span className="row-sub">{tt('Ұпайлар жинала береді, бірақ басқалар сізді көрмейді.', 'Баллы копятся, но другие вас не видят.')}</span></span>
          <input type="checkbox" checked={s.settings.leaderboardOptOut} disabled={busy} onChange={e => void save({ leaderboardOptOut: e.target.checked })} />
        </label>
        <ErrorLine err={err} />
      </form>
    </section>
  );
}

/* ---------- shop ---------- */

function Shop({ s, onChange, onDone }: { s: PointsSummary; onChange: (v: PointsSummary) => void; onDone: () => void }) {
  const { tt, lang, openSheet } = useApp();
  const list = useLoad<{ rewards: Reward[] }>('/api/points/rewards');
  const title = (r: Reward) => (lang === 'ru' ? r.titleRu : r.titleKk);
  if (!list.data) return list.err ? <ErrorLine err={list.err} /> : <Loading />;
  if (!list.data.rewards.length) return <p className="small muted">{tt('Әзірге сыйлық жоқ — жақында қосылады.', 'Наград пока нет — скоро появятся.')}</p>;
  return (
    <section className="section">
      <p className="tiny muted">{tt('Өтінімді әкімші қарайды. Қабылданбаса, монета толық қайтарылады.', 'Заявку рассматривает администратор. Если её отклонят, монеты вернутся полностью.')}</p>
      <div className="pts-shop">{list.data.rewards.map(r => {
        const out = r.stock !== null && r.stock <= 0, limit = (r.mine ?? 0) >= r.perUserLimit, poor = s.coins < r.cost;
        return (
          <article className="card pts-reward" key={r.id} aria-labelledby={`rw${r.id}`}>
            <span className="eyebrow">{kindLabel(r.kind, tt)}</span>
            <h3 id={`rw${r.id}`}>{title(r)}</h3>
            {(lang === 'ru' ? r.descRu : r.descKk) ? <p className="small muted">{lang === 'ru' ? r.descRu : r.descKk}</p> : null}
            <div className="pts-reward-foot">
              <b className="num"><Icon n="coin" s={16} /> {r.cost}</b>
              {r.stock !== null ? <span className="tiny muted num">{tt(`қалды: ${r.stock}`, `осталось: ${r.stock}`)}</span> : null}
              <button className="btn" type="button" disabled={out || limit || poor}
                onClick={() => openSheet(<RedeemSheet r={r} onDone={me => { onChange(me); list.reload(); onDone(); }} />)}
                aria-label={tt(`«${title(r)}» алу, ${r.cost} монета`, `Получить «${title(r)}» за ${r.cost} ${coinsWord(r.cost, tt)}`)}>
                {out ? tt('Таусылды', 'Закончилось') : limit ? tt('Алынды', 'Получено') : tt('Алу', 'Получить')}
              </button>
            </div>
            {poor && !out && !limit ? <span className="tiny muted num">{tt(`Тағы ${r.cost - s.coins} монета керек`, `Нужно ещё ${r.cost - s.coins} ${coinsWord(r.cost - s.coins, tt)}`)}</span> : null}
          </article>
        );
      })}</div>
    </section>
  );
}

function RedeemSheet({ r, onDone }: { r: Reward; onDone: (me: PointsSummary) => void }) {
  const { tt, lang, closeSheet, toast } = useApp();
  const key = useRef(newKey()); // one intent: a retry after a lost answer reuses the key and cannot pay twice
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  const submit = async () => {
    if (busy) return;
    setBusy(true); setErr('');
    try {
      const res = await api<{ redemption: Redemption; me: PointsSummary }>('/api/points/redeem', { method: 'POST', body: { rewardId: r.id, key: key.current }, lang });
      pointsChanged();
      closeSheet();
      toast(tt('Өтінім жіберілді', 'Заявка отправлена'));
      onDone(res.me);
    } catch (e) { setErr(errText(e)); } finally { setBusy(false); }
  };
  return (
    <div className="form">
      <h2>{lang === 'ru' ? r.titleRu : r.titleKk}</h2>
      <p className="small">{tt(`${r.cost} монета ұсталады. Әкімші бекіткенде код не нұсқау осы бетте шығады және хабарлама келеді.`,
        `Спишется ${r.cost} ${coinsWord(r.cost, tt)}. Когда администратор одобрит, код или инструкция появятся на этой странице, придёт уведомление.`)}</p>
      <ErrorLine err={err} />
      <div className="btn-row">
        <button className="btn ghost" type="button" onClick={closeSheet}>{tt('Бас тарту', 'Отмена')}</button>
        <button className="btn" type="button" onClick={submit} disabled={busy}>{tt('Растау', 'Подтвердить')}</button>
      </div>
    </div>
  );
}

function MyRedemptions() {
  const { tt, lang, toast } = useApp();
  const list = useLoad<{ redemptions: Redemption[] }>('/api/points/redemptions');
  if (!list.data) return list.err ? <ErrorLine err={list.err} /> : <Loading />;
  if (!list.data.redemptions.length) return <p className="small muted">{tt('Әзірге өтінім жоқ.', 'Заявок пока нет.')}</p>;
  return (
    <div className="group pts-mine">{list.data.redemptions.map(m => (
      <div className="row pts-red" key={m.id}>
        <span className="row-main">
          <span className="row-title">{lang === 'ru' ? m.titleRu : m.titleKk} {statusBadge(m.status, tt)}</span>
          <span className="row-sub num">{m.cost} {coinsWord(m.cost, tt)} · {new Date(m.createdAt).toLocaleDateString(lang === 'ru' ? 'ru-RU' : 'kk-KZ', { timeZone: 'Asia/Almaty' })}</span>
          {m.status === 'approved' && m.code ? (
            <span className="pts-code">
              <span className="code" aria-label={tt('Код', 'Код')}>{m.code}</span>
              <button className="btn ghost" type="button" onClick={() => copyText(m.code!, toast, tt)}><Icon n="copy" s={16} /> {tt('Көшіру', 'Копировать')}</button>
            </span>
          ) : null}
          {m.note ? <span className="row-sub">{m.status === 'rejected' ? tt('Себебі: ', 'Причина: ') : ''}{m.note}</span> : null}
        </span>
      </div>
    ))}</div>
  );
}

/* ---------- admin: /admin/points ---------- */

export function AdminPoints() {
  return <AdminOnly><Feature name="points"><AdminPointsBody /></Feature></AdminOnly>;
}

function AdminPointsBody() {
  const { tt } = useApp();
  const [tab, setTab] = useState('queue');
  return (
    <div className="pts">
      <p className="note">{tt('Монета — ақша емес. Ақшалай төлем жоқ: оған лицензиясы бар төлем серіктесі мен заңгер қорытындысы керек.',
        'Монеты — не деньги. Денежных выплат нет: для них нужны лицензированный платёжный партнёр и заключение юриста.')}</p>
      <Seg items={[['queue', tt('Кезек', 'Очередь')], ['rewards', tt('Сыйлықтар', 'Награды')], ['ledger', tt('Есепшот', 'Счёт')]]} cur={tab} onPick={setTab} />
      {tab === 'queue' ? <AdminQueue /> : tab === 'rewards' ? <AdminRewards /> : <AdminLedger />}
    </div>
  );
}

function AdminQueue() {
  const { tt, lang, openSheet, closeSheet, toast } = useApp();
  const [status, setStatus] = useState('pending');
  const q = useLoad<{ redemptions: Redemption[] }>(`/api/admin/points/redemptions?status=${status}`);
  const reject = (m: Redemption) => openSheet(
    <ReasonSheet title={tt('Өтінімді қабылдамау', 'Отклонить заявку')} label={tt('Себебі (студент көреді)', 'Причина (студент увидит)')} danger
      action={tt('Қабылдамау', 'Отклонить')} onSubmit={async note => {
        await api(`/api/admin/points/redemptions/${m.id}/reject`, { method: 'POST', body: { note }, lang });
        closeSheet(); toast(tt('Қабылданбады, монета қайтарылды', 'Отклонено, монеты возвращены')); q.reload();
      }} />,
  );
  return (
    <section className="section">
      <Seg items={[['pending', tt('Күтуде', 'Ожидают')], ['approved', tt('Бекітілген', 'Одобрены')], ['rejected', tt('Қабылданбаған', 'Отклонены')]]} cur={status} onPick={setStatus} />
      <ErrorLine err={q.err} />
      {!q.data ? <Loading /> : !q.data.redemptions.length ? <p className="small muted">{tt('Бос.', 'Пусто.')}</p> : q.data.redemptions.map(m => (
        <article className="card" key={m.id}>
          <h3>{lang === 'ru' ? m.titleRu : m.titleKk} {statusBadge(m.status, tt)}</h3>
          <p className="small muted num">{kindLabel(m.kind, tt)} · {m.cost} {coinsWord(m.cost, tt)} · #{m.id}</p>
          <p className="small num">{m.userName || tt('Аты жоқ', 'Без имени')} (id {m.userId}) · {tt(`балансы ${m.userBalance}`, `баланс ${m.userBalance}`)}</p>
          {m.code ? <p className="small">{tt('Код: ', 'Код: ')}<span className="code">{m.code}</span></p> : null}
          {m.note ? <p className="small muted">{m.note}</p> : null}
          {m.status === 'pending' ? (
            <div className="btn-row">
              <button className="btn ghost" type="button" onClick={() => reject(m)}>{tt('Қабылдамау', 'Отклонить')}</button>
              <button className="btn" type="button" onClick={() => openSheet(<ApproveSheet m={m} onDone={q.reload} />)}>{tt('Бекіту', 'Одобрить')}</button>
            </div>
          ) : null}
        </article>
      ))}
    </section>
  );
}

function ApproveSheet({ m, onDone }: { m: Redemption; onDone: () => void }) {
  const { tt, lang, closeSheet, toast } = useApp();
  const [code, setCode] = useState('');
  const [note, setNote] = useState('');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  return (
    <form className="form" onSubmit={async e => {
      e.preventDefault();
      if (busy) return;
      setBusy(true); setErr('');
      try {
        await api(`/api/admin/points/redemptions/${m.id}/approve`, { method: 'POST', body: { code: code.trim(), note: note.trim() }, lang });
        closeSheet(); toast(tt('Бекітілді', 'Одобрено')); onDone();
      } catch (e2) { setErr(errText(e2)); } finally { setBusy(false); }
    }}>
      <h2>{tt('Өтінімді бекіту', 'Одобрить заявку')}</h2>
      <div className="field">
        <label htmlFor="apCode">{tt('Промокод не алу нұсқауы', 'Промокод или как получить')}</label>
        <input className="input" id="apCode" maxLength={200} value={code} onChange={e => setCode(e.target.value)} autoComplete="off" />
        <span className="tiny muted">{tt('Бос қалса — сыйлықтың код қорынан келесі код беріледі.', 'Если пусто — выдаётся следующий код из запаса награды.')}</span>
      </div>
      <div className="field">
        <label htmlFor="apNote">{tt('Ескертпе (міндетті емес)', 'Примечание (необязательно)')}</label>
        <input className="input" id="apNote" maxLength={500} value={note} onChange={e => setNote(e.target.value)} />
      </div>
      <ErrorLine err={err} />
      <div className="btn-row">
        <button className="btn ghost" type="button" onClick={closeSheet}>{tt('Бас тарту', 'Отмена')}</button>
        <button className="btn" type="submit" disabled={busy}>{tt('Бекіту', 'Одобрить')}</button>
      </div>
    </form>
  );
}

const KINDS: RewardKind[] = ['discount', 'merch', 'print_quota', 'priority_event'];

function AdminRewards() {
  const { tt, lang, openSheet } = useApp();
  const list = useLoad<{ rewards: Reward[] }>('/api/admin/points/rewards');
  return (
    <section className="section">
      <button className="btn block" type="button" onClick={() => openSheet(<RewardForm onDone={list.reload} />)}><Icon n="plus" s={18} /> {tt('Сыйлық қосу', 'Добавить награду')}</button>
      <ErrorLine err={list.err} />
      {!list.data ? <Loading /> : (
        <div className="group">{list.data.rewards.map(r => (
          <Row key={r.id} icon="gift" title={<>{lang === 'ru' ? r.titleRu : r.titleKk} {r.active ? null : <span className="badge b-line">{tt('мұрағатта', 'в архиве')}</span>}</>}
            sub={<span className="num">{kindLabel(r.kind, tt)} · {r.cost} · {r.stock === null ? tt('шектеусіз', 'без лимита') : tt(`қалды ${r.stock}`, `осталось ${r.stock}`)} · {tt(`кодтар ${r.codesLeft ?? 0}`, `кодов ${r.codesLeft ?? 0}`)}</span>}
            onClick={() => openSheet(<RewardForm r={r} onDone={list.reload} />)} />
        ))}</div>
      )}
    </section>
  );
}

function RewardForm({ r, onDone }: { r?: Reward; onDone: () => void }) {
  const { tt, lang, L, content, closeSheet, toast } = useApp();
  const [f, setF] = useState({
    kind: r?.kind ?? 'merch' as RewardKind, titleKk: r?.titleKk ?? '', titleRu: r?.titleRu ?? '', descKk: r?.descKk ?? '', descRu: r?.descRu ?? '',
    cost: String(r?.cost ?? 50), stock: r?.stock == null ? '' : String(r.stock), perUserLimit: String(r?.perUserLimit ?? 1),
    discountId: r?.discountId ?? '', active: r?.active ?? true, codes: '',
  });
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  const set = (k: keyof typeof f) => (e: { target: { value: string } }) => setF(x => ({ ...x, [k]: e.target.value }));
  const field = (id: string, label: string, k: 'titleKk' | 'titleRu' | 'descKk' | 'descRu' | 'cost' | 'stock' | 'perUserLimit', type = 'text', max = 120) => (
    <div className="field">
      <label htmlFor={id}>{label}</label>
      {k.startsWith('desc')
        ? <textarea className="input" id={id} maxLength={600} value={f[k]} onChange={set(k)} />
        : <input className="input" id={id} type={type} inputMode={type === 'number' ? 'numeric' : undefined} maxLength={max} value={f[k]} onChange={set(k)} />}
    </div>
  );
  return (
    <form className="form" onSubmit={async e => {
      e.preventDefault();
      if (busy) return;
      setBusy(true); setErr('');
      const body = {
        kind: f.kind, titleKk: f.titleKk, titleRu: f.titleRu, descKk: f.descKk, descRu: f.descRu, cost: Number(f.cost),
        stock: f.stock.trim() === '' ? null : Number(f.stock), perUserLimit: Number(f.perUserLimit), discountId: f.kind === 'discount' ? f.discountId : null,
        active: f.active, addCodes: f.codes.split('\n').map(x => x.trim()).filter(Boolean),
      };
      try {
        await api(r ? `/api/admin/points/rewards/${r.id}` : '/api/admin/points/rewards', { method: r ? 'PUT' : 'POST', body, lang });
        closeSheet(); toast(tt('Сақталды', 'Сохранено')); onDone();
      } catch (e2) {
        const fieldName = (e2 as { detail?: { field?: string } }).detail?.field;
        setErr(errText(e2) + (fieldName ? ` (${fieldName})` : ''));
      } finally { setBusy(false); }
    }}>
      <h2>{r ? tt('Сыйлықты өзгерту', 'Изменить награду') : tt('Жаңа сыйлық', 'Новая награда')}</h2>
      <div className="field">
        <label htmlFor="rwKind">{tt('Түрі', 'Тип')}</label>
        <select className="input" id="rwKind" value={f.kind} onChange={e => setF(x => ({ ...x, kind: e.target.value as RewardKind }))}>
          {KINDS.map(k => <option key={k} value={k}>{kindLabel(k, tt)}</option>)}
        </select>
      </div>
      {f.kind === 'discount' ? (
        <div className="field">
          <label htmlFor="rwDiscount">{tt('Жеңілдік («Жеңілдіктер» бөлімінен)', 'Скидка (из раздела «Скидки»)')}</label>
          <select className="input" id="rwDiscount" value={f.discountId} onChange={set('discountId')}>
            <option value="">—</option>
            {content.discounts.map(d => <option key={d.id} value={d.id}>{L(d.name)} · {d.pct}</option>)}
          </select>
        </div>
      ) : null}
      {field('rwTitleKk', tt('Атауы (қазақша)', 'Название (каз.)'), 'titleKk')}
      {field('rwTitleRu', tt('Атауы (орысша)', 'Название (рус.)'), 'titleRu')}
      {field('rwDescKk', tt('Сипаттама (қазақша)', 'Описание (каз.)'), 'descKk')}
      {field('rwDescRu', tt('Сипаттама (орысша)', 'Описание (рус.)'), 'descRu')}
      <div className="cal-form-row">
        {field('rwCost', tt('Бағасы, монета', 'Цена, монет'), 'cost', 'number', 6)}
        {field('rwStock', tt('Саны (бос — шектеусіз)', 'Количество (пусто — без лимита)'), 'stock', 'number', 7)}
        {field('rwLimit', tt('Бір студентке', 'На одного студента'), 'perUserLimit', 'number', 2)}
      </div>
      <div className="field">
        <label htmlFor="rwCodes">{tt('Кодтар қосу (әр жолға біреу)', 'Добавить коды (по одному в строке)')}</label>
        <textarea className="input" id="rwCodes" value={f.codes} onChange={set('codes')} />
        {r ? <span className="tiny muted num">{tt(`Қорда қалды: ${r.codesLeft ?? 0}`, `В запасе: ${r.codesLeft ?? 0}`)}</span> : null}
      </div>
      <label className="row toggle-row">
        <span className="row-main"><span className="row-title">{tt('Дүкенде көрсету', 'Показывать в магазине')}</span></span>
        <input type="checkbox" checked={f.active} onChange={e => setF(x => ({ ...x, active: e.target.checked }))} />
      </label>
      <ErrorLine err={err} />
      <div className="btn-row">
        <button className="btn ghost" type="button" onClick={closeSheet}>{tt('Бас тарту', 'Отмена')}</button>
        <button className="btn" type="submit" disabled={busy}>{tt('Сақтау', 'Сохранить')}</button>
      </div>
    </form>
  );
}

interface LedgerView {
  user: { id: number; name: string | null; coins: number; ledgerSum: number; xp: number; streak: number; streakBest: number; freezes: number };
  ledger: { id: number; delta: number; reason: string; refType: string; refId: string; note: string | null; actorId: number | null; createdAt: string }[];
}

function AdminLedger() {
  const { tt, lang } = useApp();
  const [uid, setUid] = useState('');
  const [view, setView] = useState<LedgerView | null>(null);
  const [err, setErr] = useState('');
  const [delta, setDelta] = useState('');
  const [reason, setReason] = useState('');
  const [busy, setBusy] = useState(false);
  const key = useRef('');
  const open = async (id: string) => {
    setErr('');
    try { setView(await api<LedgerView>(`/api/admin/points/users/${encodeURIComponent(id)}`, { lang })); } catch (e) { setView(null); setErr(errText(e)); }
  };
  const fmt = (iso: string) => new Date(iso).toLocaleString(lang === 'ru' ? 'ru-RU' : 'kk-KZ', { timeZone: 'Asia/Almaty', dateStyle: 'short', timeStyle: 'short' });
  return (
    <section className="section">
      <form className="form" onSubmit={e => { e.preventDefault(); if (/^\d+$/.test(uid.trim())) void open(uid.trim()); }}>
        <div className="field">
          <label htmlFor="lgUser">{tt('Студенттің id-і (/admin пайдаланушылар тізімінен)', 'id студента (из списка пользователей в /admin)')}</label>
          <div className="pts-inline">
            <input className="input" id="lgUser" inputMode="numeric" value={uid} onChange={e => setUid(e.target.value)} />
            <button className="btn" type="submit">{tt('Ашу', 'Открыть')}</button>
          </div>
        </div>
      </form>
      <ErrorLine err={err} />
      {view ? <>
        <div className="stat-grid">
          <div className="stat"><b className="num">{view.user.coins}</b><span>{tt('монета (кэш)', 'монет (кэш)')}</span></div>
          <div className="stat"><b className="num">{view.user.ledgerSum}</b><span>{tt('журнал қосындысы', 'сумма журнала')}</span></div>
          <div className="stat"><b className="num">{view.user.xp}</b><span>XP</span></div>
        </div>
        {view.user.coins !== view.user.ledgerSum ? <p className="small urgent" role="alert">{tt('Баланс журналмен сәйкес емес!', 'Баланс не совпадает с журналом!')}</p> : null}
        <form className="form" onSubmit={async e => {
          e.preventDefault();
          const d = Number(delta);
          if (busy || !Number.isInteger(d) || d === 0 || reason.trim().length < 5) return;
          key.current ||= newKey();
          setBusy(true); setErr('');
          try {
            setView(await api<LedgerView>(`/api/admin/points/users/${view.user.id}/adjust`, { method: 'POST', body: { delta: d, reason: reason.trim(), key: key.current }, lang }));
            key.current = ''; setDelta(''); setReason('');
          } catch (e2) { setErr(errText(e2)); } finally { setBusy(false); }
        }}>
          <h3>{tt('Қолмен түзету', 'Ручная корректировка')} — {view.user.name || '#' + view.user.id}</h3>
          <div className="field">
            <label htmlFor="lgDelta">{tt('Өзгеріс (мысалы, 10 не -5)', 'Изменение (например, 10 или -5)')}</label>
            <input className="input" id="lgDelta" inputMode="numeric" value={delta} onChange={e => { setDelta(e.target.value); key.current = ''; }} />
          </div>
          <div className="field">
            <label htmlFor="lgReason">{tt('Себебі (міндетті, журналға жазылады)', 'Причина (обязательно, пишется в журнал)')}</label>
            <textarea className="input" id="lgReason" maxLength={500} value={reason} onChange={e => { setReason(e.target.value); key.current = ''; }} />
          </div>
          <button className="btn" type="submit" disabled={busy || reason.trim().length < 5 || !/^-?\d+$/.test(delta.trim()) || Number(delta) === 0}>{tt('Түзету', 'Скорректировать')}</button>
        </form>
        <div className="group">{view.ledger.map(l => (
          <Row key={l.id} title={<>{reasonLabel(l.reason, tt)} <span className="tiny muted">{l.refType}:{l.refId}</span></>}
            sub={<>{fmt(l.createdAt)}{l.actorId ? ` · admin ${l.actorId}` : ''}{l.note ? ` · ${l.note}` : ''}</>}
            end={<b className={`num ${l.delta < 0 ? 'urgent' : ''}`}>{l.delta > 0 ? '+' : ''}{l.delta}</b>} chev={false} />
        ))}</div>
      </> : null}
    </section>
  );
}
