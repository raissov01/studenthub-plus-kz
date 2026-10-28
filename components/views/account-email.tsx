'use client';
/* Email + password sign-in (docs/api/email-auth.md), the second way in next to Telegram: the «Email» tab of the
   sign-in sheet (sign in · register · forgot password, the code screen) and the Settings sheets for adding or
   changing the address and the password. Passwords and codes stay in component state for the step that needs them
   and are never stored. The server's messages are shown as they come: they never tell whether an address has an
   account, and the UI must not guess either. */
import { useEffect, useRef, useState, type FormEvent, type ReactNode } from 'react';
import { useApp } from '@/components/app';
import { Icon } from '@/components/ui';
import {
  ApiError, changePassword, confirmEmail, emailLogin, emailRegister, emailResend, emailVerify, linkEmail, resetConfirm,
  resetRequest, useAccount, type CodePurpose, type Sent,
} from '@/lib/account';

type TT = (kk: string, ru: string) => string;
const errOf = (e: unknown, tt: TT) => (e instanceof Error && e.message ? e.message : tt('Болмады. Кейінірек қайталаңыз.', 'Не получилось. Попробуйте позже.'));
const code = (e: unknown) => (e instanceof ApiError ? e.code : '');
const looksLikeEmail = (s: string) => /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(s.trim());
/** seconds until a new code may be asked for: the server's cooldown, or Retry-After of a 429 */
const untilFrom = (secs: number) => Date.now() + Math.max(1, secs) * 1000;

/** an address that wraps before «@» on narrow screens instead of mid-word */
export function Addr({ email, bold }: { email: string; bold?: boolean }) {
  const at = email.lastIndexOf('@');
  const body = at > 0 ? <>{email.slice(0, at)}<wbr />{email.slice(at)}</> : email;
  return bold ? <b className="addr">{body}</b> : <span className="addr">{body}</span>;
}

/* ---------- password field with show/hide and the server's rules as a hint ---------- */
export function PasswordField({ id, label, value, onChange, autoComplete, error, hint }: {
  id: string; label: string; value: string; onChange: (v: string) => void;
  autoComplete: 'new-password' | 'current-password'; error?: string; hint?: ReactNode;
}) {
  const { tt } = useApp();
  const [shown, setShown] = useState(false);
  return (
    <div className="field">
      <label htmlFor={id}>{label}</label>
      <div className="pw-field">
        <input className="input" id={id} type={shown ? 'text' : 'password'} autoComplete={autoComplete} autoCapitalize="off"
          autoCorrect="off" spellCheck={false} maxLength={256} value={value} onChange={e => onChange(e.target.value)}
          aria-invalid={error ? true : undefined} aria-describedby={error ? id + 'Err' : hint ? id + 'Hint' : undefined} />
        <button type="button" className="pw-toggle" aria-pressed={shown} aria-controls={id} onClick={() => setShown(v => !v)}>
          {shown ? tt('Жасыру', 'Скрыть') : tt('Көрсету', 'Показать')}
        </button>
      </div>
      {error ? <p className="small urgent" id={id + 'Err'} role="alert">{error}</p> : hint ? <div id={id + 'Hint'}>{hint}</div> : null}
    </div>
  );
}

/* The same rules as pkg/auth CheckPassword; the server still decides (its common list is longer). */
const COMMON = new Set(['password', 'password1', 'password123', 'passw0rd', '12345678', '123456789', '1234567890', '87654321',
  '11111111', '00000000', '12341234', 'qwertyui', 'qwertyuiop', 'qwerty12', 'qwerty123', '1q2w3e4r', '1qaz2wsx', 'asdfghjk', 'iloveyou']);
export function PasswordRules({ pw, email }: { pw: string; email: string }) {
  const { tt } = useApp();
  const low = pw.toLowerCase();
  const n = [...pw].length;
  const em = email.trim().toLowerCase();
  const local = em.split('@')[0] || '';
  const rules: [boolean, string][] = [
    [n >= 8 && n <= 128, tt('Кемінде 8 таңба', 'Не короче 8 символов')],
    [n > 0 && !COMMON.has(low) && !/^(.)\1*$/u.test(low), tt('Тым оңай емес (12345678, password сияқты емес)', 'Не слишком простой (не 12345678, не password)')],
    [n > 0 && low !== em && !(local.length >= 4 && low === local), tt('Email-мен бірдей емес', 'Не совпадает с email')],
  ];
  return (
    <ul className="pw-rules">
      {rules.map(([ok, txt]) => (
        <li key={txt} className={pw && ok ? 'ok' : ''}>
          {pw ? <Icon n={ok ? 'check' : 'x'} s={14} /> : <span className="pw-dot" aria-hidden="true" />}
          <span>{txt}</span>
          {pw ? <span className="visually-hidden">{ok ? tt(' — орындалды', ' — выполнено') : tt(' — орындалмады', ' — не выполнено')}</span> : null}
        </li>
      ))}
    </ul>
  );
}

/* ---------- 6-digit code: paste anywhere, autofill (one-time-code) fills all boxes ---------- */
export function CodeBoxes({ value, onChange, onComplete, disabled }: {
  value: string; onChange: (v: string) => void; onComplete?: (v: string) => void; disabled?: boolean;
}) {
  const { tt } = useApp();
  const refs = useRef<(HTMLInputElement | null)[]>([]);
  useEffect(() => { if (!value) refs.current[0]?.focus(); }, [value]);
  const put = (at: number, raw: string) => {
    const digits = raw.replace(/\D/g, '');
    const cur = value.padEnd(6, ' ').split('');
    if (!digits) { cur[at] = ' '; }
    else if (digits.length >= 6) { digits.slice(0, 6).split('').forEach((d, i) => { cur[i] = d; }); }
    else digits.split('').forEach((d, i) => { if (at + i < 6) cur[at + i] = d; });
    // keep it contiguous: the code is what is typed from the first box on
    const next = cur.join('').replace(/\s+$/, '').replace(/\s/g, '').slice(0, 6);
    onChange(next);
    const focusAt = digits.length >= 6 ? 5 : Math.min(5, digits ? at + digits.length : at);
    refs.current[Math.min(focusAt, next.length)]?.focus();
    if (next.length === 6) onComplete?.(next);
  };
  return (
    <div className="code-boxes" role="group" aria-label={tt('Хаттағы 6 таңбалы код', '6-значный код из письма')}>
      {Array.from({ length: 6 }, (_, i) => (
        <input key={i} ref={el => { refs.current[i] = el; }} className="input" inputMode="numeric" pattern="[0-9]*"
          autoComplete={i === 0 ? 'one-time-code' : 'off'} enterKeyHint="done" disabled={disabled}
          aria-label={tt(`${i + 1}-сан`, `Цифра ${i + 1}`)} value={value[i] || ''}
          onFocus={e => { if (i > value.length) refs.current[value.length]?.focus(); else e.target.select(); }}
          onChange={e => put(i, e.target.value)}
          onPaste={e => { e.preventDefault(); put(0, e.clipboardData.getData('text')); }}
          onKeyDown={e => {
            if (e.key === 'Backspace' && !value[i] && i > 0) { e.preventDefault(); onChange(value.slice(0, i - 1)); refs.current[i - 1]?.focus(); }
            else if (e.key === 'Backspace' && value[i]) { e.preventDefault(); onChange(value.slice(0, i)); }
            else if (e.key === 'ArrowLeft' && i > 0) refs.current[i - 1]?.focus();
            else if (e.key === 'ArrowRight' && i < value.length) refs.current[i + 1]?.focus();
          }} />
      ))}
    </div>
  );
}

/* ---------- resend with the server's cooldown ---------- */
function ResendButton({ until, onResend }: { until: number; onResend: () => Promise<void> }) {
  const { tt } = useApp();
  const [now, setNow] = useState(() => Date.now());
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(t);
  }, []);
  const left = Math.max(0, Math.ceil((until - now) / 1000));
  const mmss = `${Math.floor(left / 60)}:${String(left % 60).padStart(2, '0')}`;
  return (
    <button className="link-btn" type="button" disabled={left > 0 || busy}
      onClick={async () => { setBusy(true); try { await onResend(); } finally { setBusy(false); setNow(Date.now()); } }}>
      {left > 0 ? tt(`Кодты қайта жіберу · ${mmss}`, `Отправить код снова · ${mmss}`) : tt('Кодты қайта жіберу', 'Отправить код снова')}
    </button>
  );
}

/** the shared code screen: boxes, submit, resend; `submit` throws ApiError, invalid_code clears the boxes */
function CodeStep({ email, until, setUntil, resend, submit, extra, submitLabel, auto = true }: {
  email: string; until: number; setUntil: (t: number) => void; resend: () => Promise<Sent>;
  submit: (code: string) => Promise<void>; extra?: ReactNode; submitLabel: string; auto?: boolean;
}) {
  const { tt } = useApp();
  const [val, setVal] = useState('');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  const [info, setInfo] = useState('');
  const go = async (c: string) => {
    if (busy || c.length !== 6) return;
    setBusy(true); setErr(''); setInfo('');
    try { await submit(c); } catch (e) {
      if (code(e) === 'invalid_code') setVal('');
      if (e instanceof ApiError && e.status === 429 && e.retryAfter) setUntil(untilFrom(e.retryAfter));
      setErr(errOf(e, tt));
    } finally { setBusy(false); }
  };
  const doResend = async () => {
    setErr(''); setInfo('');
    try {
      const r = await resend();
      setUntil(untilFrom(r.resendAfter || 60));
      setVal('');
      setInfo(tt('Жаңа код жіберілді.', 'Новый код отправлен.'));
    } catch (e) {
      if (e instanceof ApiError && e.retryAfter) setUntil(untilFrom(e.retryAfter));
      setErr(errOf(e, tt));
    }
  };
  return (
    <form className="form" noValidate onSubmit={e => { e.preventDefault(); void go(val); }}>
      <p className="small">{tt('Кодты мына адреске жібердік:', 'Мы отправили код на адрес:')} <Addr email={email} bold /></p>
      <p className="tiny muted">{tt('Хат бірнеше минутта келеді. Көрмесеңіз, «Спам» бумасын тексеріңіз. Код 15 минут жарамды.', 'Письмо придёт в течение нескольких минут. Если его нет, проверьте «Спам». Код действует 15 минут.')}</p>
      <div className="field">
        <span className="lbl">{tt('Код', 'Код')}</span>
        <CodeBoxes value={val} onChange={v => { setVal(v); if (err) setErr(''); }} onComplete={auto ? c => void go(c) : undefined} disabled={busy} />
      </div>
      {extra}
      {err ? <p className="small urgent" role="alert">{err}</p> : null}
      {info ? <p className="small muted" role="status">{info}</p> : null}
      <button className="btn block" type="submit" disabled={busy || val.length !== 6}>{busy ? tt('Тексеріп жатырмыз…', 'Проверяем…') : submitLabel}</button>
      <ResendButton until={until} onResend={doResend} />
    </form>
  );
}

function EmailField({ id, value, onChange, autoComplete = 'email', label }: { id: string; value: string; onChange: (v: string) => void; autoComplete?: string; label?: string }) {
  const { tt } = useApp();
  return (
    <div className="field">
      <label htmlFor={id}>{label || 'Email'}</label>
      <input className="input" id={id} type="email" inputMode="email" autoComplete={autoComplete} autoCapitalize="off" autoCorrect="off"
        spellCheck={false} maxLength={254} placeholder={tt('ат@мысал.kz', 'имя@пример.kz')} value={value} onChange={e => onChange(e.target.value)} />
    </div>
  );
}

/* ---------- the «Email» tab of the sign-in sheet ---------- */
type Step =
  | { s: 'login' } | { s: 'register' } | { s: 'forgot' }
  | { s: 'code'; email: string; password: string; until: number }
  | { s: 'reset'; email: string; until: number };

export function EmailSignIn({ onSignedIn, onDeep }: {
  /** after a session started: the caller shows the consent screen or closes; `note` is toasted */
  onSignedIn: (needsConsent: boolean, note?: string) => void;
  /** true while a step below the first screen is open (the caller hides its tabs) */
  onDeep?: (deep: boolean) => void;
}) {
  const { tt, S } = useApp();
  const lang = S.lang;
  const [step, setStepRaw] = useState<Step>({ s: 'login' });
  const setStep = (st: Step) => { setStepRaw(st); onDeep?.(st.s !== 'login' && st.s !== 'register'); };
  const [email, setEmail] = useState('');
  const [pw, setPw] = useState('');
  const [newPw, setNewPw] = useState('');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  const [pwErr, setPwErr] = useState('');
  const reset = () => { setErr(''); setPwErr(''); };
  const fail = (e: unknown) => {
    const c = code(e);
    if (c.startsWith('password_')) setPwErr(errOf(e, tt)); else setErr(errOf(e, tt));
  };
  const needEmail = () => {
    if (looksLikeEmail(email)) return true;
    setErr(tt('Email адресі дұрыс емес.', 'Некорректный email.'));
    return false;
  };
  const run = async (fn: () => Promise<void>) => {
    if (busy) return;
    setBusy(true); reset();
    try { await fn(); } catch (e) { fail(e); } finally { setBusy(false); }
  };
  const back = (to: Step = { s: 'login' }) => (
    <button className="link-btn" type="button" onClick={() => { reset(); setNewPw(''); setStep(to); }}>
      <Icon n="back" s={16} /> {tt('Артқа', 'Назад')}
    </button>
  );
  const errLine = err ? <p className="small urgent" role="alert">{err}</p> : null;

  if (step.s === 'code') {
    return (
      <div className="form">
        {back()}
        <h3>{tt('Email-ді растаңыз', 'Подтвердите email')}</h3>
        <CodeStep email={step.email} until={step.until} setUntil={until => setStep({ ...step, until })}
          resend={() => emailResend(step.email, 'verify_email', lang)}
          submit={async c => {
            const st = await emailVerify(step.email, c, step.password, lang);
            setPw('');
            onSignedIn(st.needsConsent);
          }}
          submitLabel={tt('Растау', 'Подтвердить')} />
      </div>
    );
  }

  if (step.s === 'reset') {
    const purpose: CodePurpose = 'reset_password';
    return (
      <div className="form">
        {back({ s: 'forgot' })}
        <h3>{tt('Жаңа құпиясөз', 'Новый пароль')}</h3>
        <CodeStep email={step.email} until={step.until} setUntil={until => setStep({ ...step, until })} auto={false}
          resend={() => emailResend(step.email, purpose, lang)}
          submit={async c => {
            try {
              const st = await resetConfirm(step.email, c, newPw, lang);
              setNewPw('');
              onSignedIn(st.needsConsent, tt('Құпиясөз жаңартылды. Басқа құрылғылардағы кірулер жабылды.', 'Пароль обновлён. Входы на других устройствах закрыты.'));
            } catch (e) {
              if (code(e).startsWith('password_')) { setPwErr(errOf(e, tt)); return; }
              throw e;
            }
          }}
          extra={<PasswordField id="resetPw" label={tt('Жаңа құпиясөз', 'Новый пароль')} value={newPw} onChange={v => { setNewPw(v); setPwErr(''); }}
            autoComplete="new-password" error={pwErr} hint={<PasswordRules pw={newPw} email={step.email} />} />}
          submitLabel={tt('Сақтап, кіру', 'Сохранить и войти')} />
        <p className="tiny muted">{tt('Құпиясөз өзгерген соң басқа құрылғылардан шығарыласыз.', 'После смены пароля вы выйдете на других устройствах.')}</p>
      </div>
    );
  }

  if (step.s === 'forgot') {
    return (
      <form className="form" noValidate onSubmit={(e: FormEvent) => { e.preventDefault(); if (!needEmail()) return; void run(async () => {
        const r = await resetRequest(email.trim(), lang);
        setStep({ s: 'reset', email: email.trim(), until: untilFrom(r.resendAfter) });
      }); }}>
        {back()}
        <h3>{tt('Құпиясөзді қалпына келтіру', 'Восстановление пароля')}</h3>
        <p className="small muted">{tt('Аккаунттың email-ін жазыңыз. Осы адрес бізде болса, оған код келеді.', 'Введите email аккаунта. Если этот адрес у нас есть, на него придёт код.')}</p>
        <EmailField id="fpEmail" value={email} onChange={v => { setEmail(v); reset(); }} autoComplete="username" />
        {errLine}
        <button className="btn block" type="submit" disabled={busy || !email.trim()}>{busy ? tt('Жіберіп жатырмыз…', 'Отправляем…') : tt('Код жіберу', 'Отправить код')}</button>
      </form>
    );
  }

  if (step.s === 'register') {
    return (
      <form className="form" noValidate onSubmit={e => { e.preventDefault(); if (!needEmail()) return; void run(async () => {
        const r = await emailRegister(email.trim(), pw, lang);
        setStep({ s: 'code', email: email.trim(), password: pw, until: untilFrom(r.resendAfter) });
      }); }}>
        <h3>{tt('Аккаунт ашу', 'Создать аккаунт')}</h3>
        <EmailField id="regEmail" value={email} onChange={v => { setEmail(v); reset(); }} />
        <PasswordField id="regPw" label={tt('Құпиясөз ойлап табыңыз', 'Придумайте пароль')} value={pw} onChange={v => { setPw(v); setPwErr(''); }}
          autoComplete="new-password" error={pwErr} hint={<PasswordRules pw={pw} email={email} />} />
        {errLine}
        <button className="btn block" type="submit" disabled={busy || !email.trim() || !pw}>{busy ? tt('Жіберіп жатырмыз…', 'Отправляем…') : tt('Жалғастыру', 'Продолжить')}</button>
        <p className="tiny muted">{tt('Email-ге 6 таңбалы код келеді. Тіркелгенде тек email мен құпиясөздің хэші сақталады.', 'На email придёт 6-значный код. Хранятся только email и хэш пароля.')}</p>
        <div className="auth-links">
          <span className="small muted">{tt('Аккаунтыңыз бар ма?', 'Уже есть аккаунт?')}</span>
          <button className="link-btn" type="button" onClick={() => { reset(); setStep({ s: 'login' }); }}>{tt('Кіру', 'Войти')}</button>
        </div>
      </form>
    );
  }

  return (
    <form className="form" noValidate onSubmit={e => { e.preventDefault(); if (!needEmail()) return; void run(async () => {
      try {
        const st = await emailLogin(email.trim(), pw, lang);
        setPw('');
        onSignedIn(st.needsConsent);
      } catch (e) {
        if (code(e) === 'email_not_verified') {
          // the password was right; the server sent a fresh code (or one is still on its way)
          setStep({ s: 'code', email: email.trim(), password: pw, until: untilFrom(60) });
          return;
        }
        throw e;
      }
    }); }}>
      <EmailField id="loginEmail" value={email} onChange={v => { setEmail(v); reset(); }} autoComplete="username" />
      <PasswordField id="loginPw" label={tt('Құпиясөз', 'Пароль')} value={pw} onChange={v => { setPw(v); reset(); }} autoComplete="current-password" />
      {errLine}
      <button className="btn block" type="submit" disabled={busy || !email.trim() || !pw}>{busy ? tt('Кіріп жатырмыз…', 'Входим…') : tt('Кіру', 'Войти')}</button>
      <div className="auth-links">
        <button className="link-btn" type="button" onClick={() => { reset(); setStep({ s: 'forgot' }); }}>{tt('Құпиясөзді ұмыттыңыз ба?', 'Забыли пароль?')}</button>
        <button className="link-btn" type="button" onClick={() => { reset(); setPw(''); setStep({ s: 'register' }); }}>{tt('Аккаунт ашу', 'Создать аккаунт')}</button>
      </div>
    </form>
  );
}

/* ---------- Settings: add or change the address ---------- */
export function EmailSheet() {
  const { tt, S, toast, closeSheet } = useApp();
  const lang = S.lang;
  const acc = useAccount();
  const had = acc.user?.email || '';
  const [email, setEmail] = useState('');
  const [pw, setPw] = useState('');
  const [sent, setSent] = useState<{ email: string; until: number } | null>(null);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  const [pwErr, setPwErr] = useState('');
  // an account with a password confirms it; one without (Telegram only) sets its first password here
  const body = () => (had ? { currentPassword: pw } : { password: pw });
  const send = () => linkEmail(email.trim(), body(), lang);
  const title = had ? tt('Email-ді өзгерту', 'Сменить email') : tt('Email қосу', 'Добавить email');

  if (sent) {
    return (
      <>
        <h2>{title}</h2>
        <CodeStep email={sent.email} until={sent.until} setUntil={until => setSent({ ...sent, until })}
          resend={send}
          submit={async c => {
            await confirmEmail(c, lang);
            setPw('');
            closeSheet();
            toast(had ? tt('Email өзгертілді. Басқа құрылғылардағы кірулер жабылды.', 'Email изменён. Входы на других устройствах закрыты.')
              : tt('Email қосылды. Енді email арқылы да кіре аласыз.', 'Email добавлен. Теперь можно входить и по email.'));
          }}
          submitLabel={tt('Растау', 'Подтвердить')} />
      </>
    );
  }
  return (
    <form className="form" noValidate onSubmit={async e => {
      e.preventDefault();
      if (busy) return;
      if (!looksLikeEmail(email)) { setErr(tt('Email адресі дұрыс емес.', 'Некорректный email.')); return; }
      setBusy(true); setErr(''); setPwErr('');
      try {
        const r = await send();
        setSent({ email: email.trim(), until: untilFrom(r.resendAfter) });
      } catch (e2) {
        const c = code(e2);
        if (c.startsWith('password_') || c === 'wrong_password') setPwErr(errOf(e2, tt)); else setErr(errOf(e2, tt));
      } finally { setBusy(false); }
    }}>
      <h2>{title}</h2>
      {had
        ? <p className="small muted">{tt('Қазіргі адрес:', 'Текущий адрес:')} <Addr email={had} bold />. {tt('Жаңа адреске код келеді. Растаған соң басқа құрылғылардағы кірулер жабылады.', 'На новый адрес придёт код. После подтверждения входы на других устройствах закроются.')}</p>
        : <p className="small muted">{tt('Email мен құпиясөз қосылса, Telegram-сыз да кіре аласыз. Адреске код келеді.', 'С email и паролем можно входить и без Telegram. На адрес придёт код.')}</p>}
      <EmailField id="newEmail" label={had ? tt('Жаңа email', 'Новый email') : 'Email'} value={email} onChange={v => { setEmail(v); setErr(''); }} />
      {had
        ? <PasswordField id="curPw" label={tt('Қазіргі құпиясөз', 'Текущий пароль')} value={pw} onChange={v => { setPw(v); setPwErr(''); }} autoComplete="current-password" error={pwErr} />
        : <PasswordField id="firstPw" label={tt('Құпиясөз ойлап табыңыз', 'Придумайте пароль')} value={pw} onChange={v => { setPw(v); setPwErr(''); }}
          autoComplete="new-password" error={pwErr} hint={<PasswordRules pw={pw} email={email} />} />}
      {err ? <p className="small urgent" role="alert">{err}</p> : null}
      <div className="btn-row">
        <button className="btn ghost" type="button" onClick={closeSheet}>{tt('Бас тарту', 'Отмена')}</button>
        <button className="btn" type="submit" disabled={busy || !email.trim() || !pw}>{busy ? tt('Жіберіп жатырмыз…', 'Отправляем…') : tt('Код жіберу', 'Отправить код')}</button>
      </div>
    </form>
  );
}

/* ---------- Settings: change the password ---------- */
export function PasswordSheet() {
  const { tt, S, toast, closeSheet } = useApp();
  const acc = useAccount();
  const [cur, setCur] = useState('');
  const [next, setNext] = useState('');
  const [busy, setBusy] = useState(false);
  const [curErr, setCurErr] = useState('');
  const [nextErr, setNextErr] = useState('');
  const [err, setErr] = useState('');
  return (
    <form className="form" noValidate onSubmit={async e => {
      e.preventDefault();
      if (busy) return;
      setBusy(true); setErr(''); setCurErr(''); setNextErr('');
      try {
        await changePassword(cur, next, S.lang);
        setCur(''); setNext('');
        closeSheet();
        toast(tt('Құпиясөз өзгертілді. Басқа құрылғылардағы кірулер жабылды.', 'Пароль изменён. Входы на других устройствах закрыты.'));
      } catch (e2) {
        const c = code(e2);
        if (c === 'wrong_password') setCurErr(errOf(e2, tt));
        else if (c.startsWith('password_')) setNextErr(errOf(e2, tt));
        else setErr(errOf(e2, tt));
      } finally { setBusy(false); }
    }}>
      <h2>{tt('Құпиясөзді өзгерту', 'Сменить пароль')}</h2>
      {/* lets password managers attach the new password to the right account */}
      <input type="email" autoComplete="username" value={acc.user?.email || ''} readOnly hidden />
      <PasswordField id="pwCur" label={tt('Қазіргі құпиясөз', 'Текущий пароль')} value={cur} onChange={v => { setCur(v); setCurErr(''); }} autoComplete="current-password" error={curErr} />
      <PasswordField id="pwNew" label={tt('Жаңа құпиясөз', 'Новый пароль')} value={next} onChange={v => { setNext(v); setNextErr(''); }}
        autoComplete="new-password" error={nextErr} hint={<PasswordRules pw={next} email={acc.user?.email || ''} />} />
      <p className="tiny muted">{tt('Сақтаған соң басқа құрылғылардан шығарыласыз, осы құрылғыда қаласыз.', 'После сохранения вы выйдете на других устройствах и останетесь на этом.')}</p>
      {err ? <p className="small urgent" role="alert">{err}</p> : null}
      <div className="btn-row">
        <button className="btn ghost" type="button" onClick={closeSheet}>{tt('Бас тарту', 'Отмена')}</button>
        <button className="btn" type="submit" disabled={busy || !cur || !next}>{busy ? tt('Сақтап жатырмыз…', 'Сохраняем…') : tt('Сақтау', 'Сохранить')}</button>
      </div>
    </form>
  );
}
