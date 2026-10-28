'use client';
/* Start-up work that has no UI of its own: referral capture and the daily visit (anonymous analytics), runtime
   config, the Telegram Mini App (theme, back button, automatic sign-in), the account, and the one-time
   3rd-visit-day feedback question. */
import { useEffect, useState } from 'react';
import { useApp } from '@/components/app';
import { Chip } from '@/components/ui';
import { captureRef, track, trackVisit } from '@/features/analytics/track';
import { cleanMissing, emptyFeedback, FEEDBACK_KEY, FEEDBACK_TEXT, markAsked, MAX_MISSING_LEN, onVisit, type FeedbackState } from '@/features/feedback/logic';
import { almatyDate } from '@/features/roadmap/logic';
import { bindBackButton, startParam } from '@/features/telegram/webapp';
import { CHECKIN_KEY } from '@/lib/events';
import { getAccount, loadMe, signInWebApp } from '@/lib/account';
import { loadConfig } from '@/lib/config';
import { initTelegram, useTelegram } from '@/lib/telegram';

function readFeedback(): FeedbackState {
  try {
    const v = JSON.parse(window.localStorage.getItem(FEEDBACK_KEY) || 'null') as Partial<FeedbackState> | null;
    if (v && Array.isArray(v.visitDays)) return { visitDays: v.visitDays.filter(d => typeof d === 'string'), asked: !!v.asked };
  } catch { /* corrupted: start over */ }
  return emptyFeedback();
}
function writeFeedback(s: FeedbackState) {
  try { window.localStorage.setItem(FEEDBACK_KEY, JSON.stringify(s)); } catch { /* ignore */ }
}

let booted = false;

export function Boot({ root }: { root: boolean }) {
  const { S, sheet, back, openSheet, go } = useApp();
  const tg = useTelegram();

  useEffect(() => {
    if (booted) return;
    booted = true;
    captureRef(); // before anything creates the anonymous id: the referral only counts on a first visit
    trackVisit();
    void loadConfig();
    void initTelegram().then(app => {
      let sent = false;
      try { sent = !!window.sessionStorage.getItem('studenthub.tgOpenSent'); window.sessionStorage.setItem('studenthub.tgOpenSent', '1'); } catch { /* ignore */ }
      if (!sent) track('telegram_open', { in_telegram: !!app });
      // opened from an event's live QR (t.me/<bot>/<app>?startapp=ci_…): straight to the check-in screen, once
      const sp = startParam(app);
      if (/^ci_[A-Za-z0-9_-]{8,64}$/.test(sp)) {
        let first = true;
        try { first = window.sessionStorage.getItem('studenthub.ciUsed') !== sp; window.sessionStorage.setItem('studenthub.ciUsed', sp); window.sessionStorage.setItem(CHECKIN_KEY, sp); } catch { /* ignore */ }
        if (first) go('checkin', { replace: true });
      }
      // Already signed in (e.g. by email in this browser)? Keep that session: linking Telegram must be the
      // user's explicit choice in Settings, never a side effect of opening the Mini App.
      if (app?.initData) {
        void loadMe()
          .then(() => (getAccount().user ? undefined : signInWebApp(app.initData)))
          .catch(() => loadMe());
      } else void loadMe();
    });
  }, [go]);

  // 3rd visit day: one short question, then never again (closing counts as answered)
  const onboarded = S.onboarded;
  useEffect(() => {
    const { state, show } = onVisit(readFeedback(), almatyDate(new Date()));
    writeFeedback(state);
    if (!show || !onboarded) return;
    const id = setTimeout(() => {
      if (document.querySelector('.sheet')) return; // something else is open; ask on the next load
      writeFeedback(markAsked(readFeedback()));
      openSheet(<FeedbackSheet />);
    }, 2500);
    return () => clearTimeout(id);
  }, [onboarded, openSheet]);

  // Telegram's own back button follows the app's back navigation (and closes sheets)
  const app = tg.app;
  const atRoot = root && !sheet;
  useEffect(() => {
    if (!app) return;
    return bindBackButton(app, atRoot, back);
  }, [app, atRoot, back]);

  return null;
}

export function FeedbackSheet() {
  const { lang, closeSheet } = useApp();
  const [score, setScore] = useState(0);
  const [missing, setMissing] = useState('');
  const [sent, setSent] = useState(false);
  const T = (k: keyof typeof FEEDBACK_TEXT) => FEEDBACK_TEXT[k][lang];
  if (sent) {
    return <><h2>{T('thanks')}</h2><button className="btn block" type="button" onClick={closeSheet}>{T('close')}</button></>;
  }
  return (
    <form className="form" onSubmit={e => {
      e.preventDefault();
      if (!score) return;
      const m = cleanMissing(missing);
      track('feedback', m ? { score, missing: m } : { score });
      setSent(true);
    }}>
      <h2 id="fbQ">{T('question')}</h2>
      <div className="chips fb-score" role="radiogroup" aria-labelledby="fbQ">
        {[1, 2, 3, 4, 5].map(n => <Chip key={n} pressed={score === n} onClick={() => setScore(n)}>{n}</Chip>)}
      </div>
      <p className="tiny muted">{lang === 'ru' ? '1 — бесполезен, 5 — очень полезен' : '1 — пайдасыз, 5 — өте пайдалы'}</p>
      <div className="field">
        <label htmlFor="fbMissing">{T('missing')}</label>
        <textarea className="input" id="fbMissing" maxLength={MAX_MISSING_LEN} value={missing} onChange={e => setMissing(e.target.value)} />
        <span className="tiny muted">{T('hint')}</span>
      </div>
      <div className="btn-row">
        <button className="btn ghost" type="button" onClick={closeSheet}>{T('close')}</button>
        <button className="btn" type="submit" disabled={!score}>{T('send')}</button>
      </div>
    </form>
  );
}
