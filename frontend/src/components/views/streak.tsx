'use client';
/* Spec 08 «kind streak» (FEATURE_STREAK, backend/internal/points/streak.go): the next milestone, the free exam-session
   pause, the evening reminder switch and the milestone sheet. Warm and plain: no guilt, no countdown pressure, and
   every state is said in words, not only with an icon. Mounted from points.tsx (streak card, chip) and notify.tsx. */
import { useEffect, useState } from 'react';
import { useApp } from '@/components/app';
import { Icon } from '@/components/ui';
import { ErrorLine, errText, hasAccount, useFeature, useLoad } from '@/components/views/common';
import { api, useAccount } from '@/lib/account';
import { milestoneToShow, pointsChanged, REMINDER_TIMES, type PointsReply, type PointsSummary } from '@/lib/points';

type TT = (kk: string, ru: string) => string;
const daysTxt = (n: number, tt: TT) => tt(`${n} күн`, `${n} ${n % 10 === 1 && n % 100 !== 11 ? 'день' : n % 10 >= 2 && n % 10 <= 4 && (n % 100 < 10 || n % 100 >= 20) ? 'дня' : 'дней'}`);

/** under the streak card: next milestone, «Сессия режимі», the evening reminder */
export function StreakExtras({ s, onChange }: { s: PointsSummary; onChange: (v: PointsSummary) => void }) {
  const { tt, lang } = useApp();
  const on = useFeature('streak');
  const [days, setDays] = useState(14);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  const st = s.streak;
  if (!on || !st.pause) return null;
  const p = st.pause;
  const send = async (path: string, method: 'POST' | 'PATCH', body: object) => {
    if (busy) return;
    setBusy(true); setErr('');
    try {
      const r = await api<PointsReply>(path, { method, body, lang });
      onChange(r.me);
    } catch (e) { setErr(errText(e)); } finally { setBusy(false); }
  };
  return (
    <div className="card streak-extras" id="streakExtras">
      {st.nextMilestone ? (
        <p className="small" id="streakNext">{tt(`Келесі белес: ${daysTxt(st.nextMilestone, tt)} (тағы ${st.nextMilestone - st.days})`,
          `Следующая отметка: ${daysTxt(st.nextMilestone, tt)} (ещё ${st.nextMilestone - st.days})`)}</p>
      ) : <p className="small">{tt('Барлық белес алынды. Жарайсың!', 'Все отметки пройдены. Отлично!')}</p>}

      <h3 className="row-title">{tt('Сессия режимі', 'Режим сессии')}</h3>
      {p.active ? (
        <>
          <p className="small" id="streakPaused">{tt(`Үзілісте: серия ${p.until} дейін сақталады (${daysTxt(p.daysLeft, tt)} қалды).`,
            `На паузе: серия сохранится до ${p.until} (осталось ${daysTxt(p.daysLeft, tt)}).`)}</p>
          <button className="btn ghost" type="button" disabled={busy} onClick={() => send('/api/points/pause', 'POST', { days: 0 })}>
            {tt('Үзілісті аяқтау', 'Завершить паузу')}
          </button>
        </>
      ) : p.usedThisSemester ? (
        <p className="tiny muted">{tt('Бұл семестрдің тегін үзілісі қолданылды. Келесі семестрде қайта болады.', 'Бесплатная пауза этого семестра уже использована. В следующем семестре будет снова.')}</p>
      ) : (
        <>
          <p className="tiny muted">{tt(`Емтихан кезінде серияны тегін тоқтатып қоюға болады: семестрге бір рет, ${p.maxDays} күнге дейін. Қорғаныс жұмсалмайды.`,
            `На время экзаменов серию можно бесплатно поставить на паузу: раз в семестр, до ${p.maxDays} дней. Защита не тратится.`)}</p>
          <div className="btn-row">
            <label className="visually-hidden" htmlFor="pauseDays">{tt('Үзіліс ұзақтығы', 'Длительность паузы')}</label>
            <select className="input" id="pauseDays" value={days} onChange={e => setDays(Number(e.target.value))} style={{ width: 'auto', flex: 'none' }}>
              {Array.from({ length: p.maxDays }, (_, i) => i + 1).map(n => <option key={n} value={n}>{daysTxt(n, tt)}</option>)}
            </select>
            <button className="btn secondary" type="button" disabled={busy} onClick={() => send('/api/points/pause', 'POST', { days })}>
              <Icon n="clock" s={18} /> {tt('Сессия режимін қосу', 'Включить режим сессии')}
            </button>
          </div>
        </>
      )}
      <StreakReminderControls s={s} busy={busy} send={send} />
      <ErrorLine err={err} />
    </div>
  );
}

function StreakReminderControls({ s, busy, send }: { s: PointsSummary; busy: boolean; send: (path: string, method: 'POST' | 'PATCH', body: object) => void }) {
  const { tt } = useApp();
  const r = s.streak.reminder;
  if (!r) return null;
  return (
    <div className="group">
      <label className="row toggle-row">
        <span className="row-main"><span className="row-title">{tt('Серия туралы еске салу', 'Напоминание о серии')}</span>
          <span className="row-sub">{tt('Кешке бір рет, серия 3 күннен асса және бүгін әлі белсенділік болмаса. 22:00-ден кейін ешқашан.', 'Один раз вечером, если серия больше 3 дней и сегодня ещё не было активности. Никогда после 22:00.')}</span></span>
        <input type="checkbox" id="streakRemind" checked={r.on} disabled={busy} onChange={e => send('/api/points/streak', 'PATCH', { reminder: e.target.checked })} />
      </label>
      {r.on ? (
        <div className="field" style={{ padding: '0 12px 12px' }}>
          <label htmlFor="streakRemindAt">{tt('Уақыты (Астана)', 'Время (Астана)')}</label>
          <select className="input" id="streakRemindAt" value={r.time} disabled={busy} onChange={e => send('/api/points/streak', 'PATCH', { time: e.target.value })}>
            {REMINDER_TIMES.map(t => <option key={t} value={t}>{t}</option>)}
          </select>
        </div>
      ) : null}
    </div>
  );
}

/** Settings → «Хабарламалар»: the same reminder switch, for students who look for it there */
export function StreakReminderSetting() {
  const acc = useAccount();
  const points = useFeature('points');
  const on = useFeature('streak');
  if (!points || !on || !hasAccount(acc)) return null;
  return <StreakReminderSetting_ />;
}
function StreakReminderSetting_() {
  const { lang } = useApp();
  const me = useLoad<PointsSummary>('/api/points/me');
  const [fresh, setFresh] = useState<PointsSummary | null>(null);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  const s = fresh ?? me.data;
  if (!s?.streak.reminder) return null;
  const send = async (path: string, method: 'POST' | 'PATCH', body: object) => {
    setBusy(true); setErr('');
    try { const r = await api<PointsReply>(path, { method, body, lang }); setFresh(r.me); pointsChanged(); } catch (e) { setErr(errText(e)); } finally { setBusy(false); }
  };
  return <><StreakReminderControls s={s} busy={busy} send={send} /><ErrorLine err={err} /></>;
}

const SEEN_KEY = 'studenthub.milestone';

/** opens the milestone sheet once per milestone and device, after the action that reached it */
export function useMilestoneSheet(s: PointsSummary | null, uid: number) {
  const { openSheet, sheet, S } = useApp();
  const on = useFeature('streak');
  const last = s?.streak.lastMilestone;
  // wait until onboarding and any other sheet are out of the way, so the celebration is not lost behind them
  const free = S.onboarded && !sheet;
  useEffect(() => {
    if (!on || !uid || !last || !free) return;
    let seen = 0;
    try { seen = Number(localStorage.getItem(`${SEEN_KEY}.${uid}`) || 0); } catch { /* private mode */ }
    const n = milestoneToShow(last, seen);
    if (!n) return;
    try { localStorage.setItem(`${SEEN_KEY}.${uid}`, String(n)); } catch { /* ignore */ }
    openSheet(<MilestoneSheet n={n} />);
  }, [on, uid, last, free, openSheet]);
}

function MilestoneSheet({ n }: { n: number }) {
  const { tt, closeSheet } = useApp();
  return (
    <div className="milestone-sheet" role="status" style={{ display: 'grid', gap: 12, justifyItems: 'center', textAlign: 'center' }}>
      <span className="pts-badge" aria-hidden="true"><Icon n="streak" s={28} /></span>
      <h2>{tt(`${daysTxt(n, tt)} қатарынан!`, `${daysTxt(n, tt)} подряд!`)}</h2>
      <p className="small">{tt('Тұрақтылық — ең жақсы әдет. Белес үшін монета әмиянға түсті.', 'Постоянство — лучшая привычка. Монеты за отметку уже в кошельке.')}</p>
      <button className="btn" type="button" onClick={closeSheet}>{tt('Жалғастыру', 'Продолжить')}</button>
    </div>
  );
}
