'use client';
/* Optional account: sign-in prompt (Telegram: Login Widget outside Telegram, a button inside the Mini App; Email when
   the server offers it, see account-email.tsx; Google when the server offers it, docs/GOOGLE_LOGIN.md), the consent
   screen, and the «Аккаунт» section of Settings (email, password, Telegram and Google link, sync, export, sign out,
   delete). The device profile keeps working without any of this. */
import { useEffect, useRef, useState } from 'react';
import { useApp } from '@/components/app';
import { Icon, Row, Seg } from '@/components/ui';
import { Addr, EmailSheet, EmailSignIn, PasswordSheet } from '@/components/views/account-email';
import {
  ApiError, deleteAccount, downloadExport, getAccount, giveConsent, googleStartUrl, loadMe, signInWebApp, signInWidget,
  signOut, syncProfile, unlinkGoogle, useAccount, type Role,
} from '@/lib/account';
import { useConfig } from '@/lib/config';
import { CITIES, SPECS } from '@/lib/data/static';
import { requestWriteAccess, useTelegram } from '@/lib/telegram';
import { SEIFULLIN } from '@/lib/universities';

declare global { interface Window { onTelegramAuth?: (user: Record<string, unknown>) => void } }

const errText = (e: unknown, fb: string) => (e instanceof ApiError || e instanceof Error ? e.message : fb);

export function roleLabel(r: Role | string, tt: (kk: string, ru: string) => string): string {
  switch (r) {
    case 'admin': return tt('Әкімші', 'Администратор');
    case 'ambassador': return tt('Амбассадор', 'Амбассадор');
    case 'university_staff': return tt('Университет қызметкері', 'Сотрудник университета');
    case 'employer': return tt('Жұмыс беруші', 'Работодатель');
    default: return tt('Студент', 'Студент');
  }
}

/* ---------- sign in ---------- */
function LoginWidget({ bot, onUser }: { bot: string; onUser: (u: Record<string, unknown>) => void }) {
  const { S } = useApp();
  const box = useRef<HTMLDivElement>(null);
  const cb = useRef(onUser);
  useEffect(() => { cb.current = onUser; }, [onUser]);
  useEffect(() => {
    const el = box.current;
    if (!el) return;
    window.onTelegramAuth = u => cb.current(u);
    const s = document.createElement('script');
    s.async = true;
    s.src = 'https://telegram.org/js/telegram-widget.js?22';
    s.setAttribute('data-telegram-login', bot);
    s.setAttribute('data-size', 'large');
    s.setAttribute('data-radius', '12');
    s.setAttribute('data-request-access', 'write');
    s.setAttribute('data-userpic', 'false');
    if (S.lang !== 'kk') s.setAttribute('data-lang', S.lang); // the widget has no Kazakh
    s.setAttribute('data-onauth', 'onTelegramAuth(user)');
    el.appendChild(s);
    return () => { el.innerHTML = ''; delete window.onTelegramAuth; };
  }, [bot, S.lang]);
  return <div className="tg-login" ref={box} />;
}

/** Telegram sign-in (widget or Mini App button). Used by the sign-in sheet and, while signed in by email, to link
    Telegram to the same account (the server attaches it; 409 telegram_taken comes back as a message). */
function TelegramSignIn({ after, label }: { after: (needsConsent: boolean) => void; label?: string }) {
  const { tt, S } = useApp();
  const cfg = useConfig();
  const tg = useTelegram();
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  const onWidget = async (u: Record<string, unknown>) => {
    setBusy(true); setErr('');
    try { const st = await signInWidget(u, S.lang); after(st.needsConsent); } catch (e) { setErr(errText(e, '')); } finally { setBusy(false); }
  };
  const onMiniApp = async () => {
    if (!tg.app) return;
    setBusy(true); setErr('');
    try { const st = await signInWebApp(tg.app.initData, S.lang); after(st.needsConsent); } catch (e) { setErr(errText(e, '')); } finally { setBusy(false); }
  };
  return (
    <>
      {tg.app
        ? <button className="btn block" type="button" disabled={busy} onClick={onMiniApp}><Icon n="send" s={18} /> {label || tt('Telegram арқылы кір', 'Войти через Telegram')}</button>
        : cfg.botUsername
          ? (
            // Telegram draws the widget's button itself (a cross-origin iframe: its own colours, no Kazakh), and the
            // signed data it returns is what the server verifies; so the button stays, with a Kazakh label above it
            <div className="tg-login-wrap" role="group" aria-labelledby="tgLoginL">
              <p className="tg-login-label" id="tgLoginL">{tt('Telegram арқылы кіру — төмендегі батырманы басыңыз', 'Вход через Telegram — нажмите кнопку ниже')}</p>
              <LoginWidget bot={cfg.botUsername} onUser={onWidget} />
            </div>
          )
          : <p className="note">{tt('Telegram арқылы кіру әзірге қосылмаған.', 'Вход через Telegram пока не подключён.')}</p>}
      {busy ? <p className="tiny muted" role="status">{tt('Кіріп жатырмыз…', 'Входим…')}</p> : null}
      {err ? <p className="small urgent" role="alert">{err}</p> : null}
      <p className="tiny muted">{tt('Кіргенде Telegram атыңыз бен username-іңіз ғана алынады. Телефон нөміріңіз бізге берілмейді.', 'При входе мы получаем только имя и username в Telegram. Номер телефона нам не передаётся.')}</p>
    </>
  );
}

/* ---------- Google ---------- */
/** Google's own «G» (Sign in with Google branding guidelines: the four-colour mark, unaltered, on white) */
export function GoogleG({ s = 18 }: { s?: number }) {
  return (
    <svg className="google-g" width={s} height={s} viewBox="0 0 48 48" aria-hidden="true" focusable="false">
      <path fill="#EA4335" d="M24 9.5c3.54 0 6.71 1.22 9.21 3.6l6.85-6.85C35.9 2.38 30.47 0 24 0 14.62 0 6.51 5.38 2.56 13.22l7.98 6.19C12.43 13.72 17.74 9.5 24 9.5z" />
      <path fill="#4285F4" d="M46.98 24.55c0-1.57-.15-3.09-.38-4.55H24v9.02h12.94c-.58 2.96-2.26 5.48-4.78 7.18l7.73 6c4.51-4.18 7.09-10.36 7.09-17.65z" />
      <path fill="#FBBC05" d="M10.53 28.59c-.48-1.45-.76-2.99-.76-4.59s.27-3.14.76-4.59l-7.98-6.19C.92 16.46 0 20.12 0 24c0 3.88.92 7.54 2.56 10.78l7.97-6.19z" />
      <path fill="#34A853" d="M24 48c6.48 0 11.93-2.13 15.89-5.81l-7.73-6c-2.15 1.45-4.92 2.3-8.16 2.3-6.26 0-11.57-4.22-13.47-9.91l-7.98 6.19C6.51 42.62 14.62 48 24 48z" />
    </svg>
  );
}

/** «Continue with Google»: a full-page navigation to the server, which runs the whole OAuth round trip and comes back here.
    Hidden inside the Telegram Mini App: Google refuses sign-in in embedded web views. */
function GoogleButton() {
  const { tt, S } = useApp();
  // the return path is this page, read at click time (the shell is static: no location while prerendering)
  return (
    <button className="btn-google" type="button" onClick={() => window.location.assign(googleStartUrl(S.lang))}>
      <GoogleG /> <span>{tt('Google арқылы кіру', 'Войти через Google')}</span>
    </button>
  );
}

/** ?google=<status> the server's callback appends (internal/auth/google.go) */
function googleStatusText(st: string, tt: (kk: string, ru: string) => string): string {
  switch (st) {
    case 'ok': return tt('Кірдіңіз', 'Вы вошли');
    case 'linked': return tt('Google байланыстырылды', 'Google привязан');
    case 'cancelled': return tt('Google арқылы кіру тоқтатылды', 'Вход через Google отменён');
    case 'expired': case 'state': return tt('Google арқылы кіру үзілді немесе тым ұзаққа созылды. Қайта көріңіз.', 'Вход через Google прервался или занял слишком долго. Попробуйте ещё раз.');
    case 'email_unverified': return tt('Google аккаунтыңыздағы email расталмаған. Оны Google-да растап, қайта көріңіз.', 'Email в вашем аккаунте Google не подтверждён. Подтвердите его в Google и попробуйте снова.');
    case 'taken': return tt('Бұл Google аккаунты басқа SIVA аккаунтына байланған. Аккаунттар біріктірілмейді.', 'Этот аккаунт Google уже привязан к другому аккаунту SIVA. Аккаунты не объединяются.');
    case 'mismatch': return tt('Бұл аккаунтқа басқа Google аккаунты байланған. Сол Google арқылы кіріңіз.', 'К этому аккаунту привязан другой аккаунт Google. Войдите через него.');
    case 'email_taken': return tt('Бұл email басқа, расталмаған аккаунтта тұр. Email мен құпиясөз арқылы кіріп, оны растаңыз.', 'Этот email указан в другом, неподтверждённом аккаунте. Войдите по email и паролю и подтвердите его.');
    case 'blocked': return tt('Бұл аккаунт бұғатталған.', 'Этот аккаунт заблокирован.');
    case 'session': return tt('Google-ды байланыстыру үшін алдымен аккаунтқа кіріңіз.', 'Чтобы привязать Google, сначала войдите в аккаунт.');
    case 'rate_limited': return tt('Тым көп әрекет. Біраздан кейін қайталаңыз.', 'Слишком много попыток. Повторите чуть позже.');
    case 'invalid': return tt('Google жауабын тексере алмадық. Қайта көріңіз.', 'Не удалось проверить ответ Google. Попробуйте ещё раз.');
    default: return tt('Google арқылы кіру қазір істемей тұр. Кейінірек көріңіз.', 'Вход через Google сейчас не работает. Попробуйте позже.');
  }
}

let googleReturnSeen = false;
/** Back from Google: drop ?google= from the address, then the consent screen (new account) or a short message. */
export function useGoogleReturn() {
  const { tt, toast, openSheet } = useApp();
  useEffect(() => {
    if (googleReturnSeen) return;
    googleReturnSeen = true;
    const url = new URL(window.location.href);
    const st = url.searchParams.get('google');
    if (!st) return;
    url.searchParams.delete('google');
    window.history.replaceState(window.history.state, '', url.pathname + url.search + url.hash);
    void loadMe(true).then(() => {
      if ((st === 'ok' || st === 'linked') && getAccount().needsConsent) openSheet(<ConsentSheet />);
      else toast(googleStatusText(st.slice(0, 32), tt));
    });
  }, [tt, toast, openSheet]);
}

/** Settings → Аккаунт: link Google (a round trip, link=1) or unlink it (not while it is the only way in) */
function GoogleRow({ linked }: { linked: boolean }) {
  const { tt, S, toast } = useApp();
  const [busy, setBusy] = useState(false);
  if (!linked) {
    return (
      <button className="row" type="button" onClick={() => window.location.assign(googleStartUrl(S.lang, true))}>
        <span className="ico google-ico"><GoogleG /></span>
        <span className="row-main"><span className="row-title">{tt('Google-ды байланыстыру', 'Привязать Google')}</span>
          <span className="row-sub">{tt('Google аккаунтыңыз арқылы да кіру үшін', 'Чтобы входить и через аккаунт Google')}</span></span>
        <span className="chev"><Icon n="chev" s={18} /></span>
      </button>
    );
  }
  return (
    <div className="row">
      <span className="ico google-ico"><GoogleG /></span>
      <span className="row-main"><span className="row-title">{tt('Google байланысқан', 'Google привязан')}</span>
        <span className="row-sub">{tt('Google аккаунтыңыз арқылы кіре аласыз', 'Можно входить через аккаунт Google')}</span></span>
      <span className="row-end">
        <button className="btn ghost small-btn" type="button" disabled={busy} onClick={async () => {
          setBusy(true);
          try { await unlinkGoogle(S.lang); toast(tt('Google ажыратылды', 'Google отвязан')); } catch (e) { toast(errText(e, tt('Болмады', 'Не получилось'))); } finally { setBusy(false); }
        }}>{tt('Ажырату', 'Отвязать')}</button>
      </span>
    </div>
  );
}

export function SignInSheet() {
  const { tt, toast, closeSheet, openSheet } = useApp();
  const cfg = useConfig();
  const tg = useTelegram();
  // inside the Mini App Telegram stays the default; the Email tab exists only when the server can deliver codes
  const [tab, setTab] = useState<'tg' | 'email'>('tg');
  const [deep, setDeep] = useState(false);
  const after = (needsConsent: boolean, note?: string) => {
    if (needsConsent) { openSheet(<ConsentSheet />); if (note) toast(note); }
    else { closeSheet(); toast(note || tt('Кірдіңіз', 'Вы вошли')); }
  };
  const email = cfg.emailAuth && tab === 'email';
  const google = cfg.googleLogin && !tg.app;
  return (
    <>
      <h2>{cfg.emailAuth || google ? tt('Аккаунтқа кіру', 'Вход в аккаунт') : tt('Telegram арқылы кіру', 'Вход через Telegram')}</h2>
      {deep && email ? null : <p className="small muted">{tt(
        'Кіру міндетті емес: профиль, жоспарлар мен сақталғандар осы құрылғыда кірмей-ақ жұмыс істейді. Аккаунт тек ортақ деректер керек әрекеттерге қажет.',
        'Вход необязателен: профиль, планы и сохранённое работают на этом устройстве и без него. Аккаунт нужен только для действий с общими данными.')}</p>}
      {google && !(deep && email)
        ? <>
          <GoogleButton />
          <p className="or-line" aria-hidden="true"><span>{tt('немесе', 'или')}</span></p>
        </>
        : null}
      {cfg.emailAuth && !(deep && email)
        ? <Seg items={[['tg', 'Telegram'], ['email', 'Email']]} cur={tab} onPick={k => setTab(k === 'email' ? 'email' : 'tg')} />
        : null}
      {email
        ? <EmailSignIn onSignedIn={after} onDeep={setDeep} />
        : <TelegramSignIn after={after} />}
      {email && !deep && tg.app ? <p className="tiny muted">{tt('Telegram ішінде Telegram арқылы кірген ыңғайлы.', 'Внутри Telegram удобнее входить через Telegram.')}</p> : null}
    </>
  );
}

/* ---------- Settings: link Telegram to an email-only account ---------- */
function LinkTelegramSheet() {
  const { tt, toast, closeSheet } = useApp();
  return (
    <>
      <h2>{tt('Telegram-ды байланыстыру', 'Привязать Telegram')}</h2>
      <p className="small muted">{tt(
        'Telegram қосылса, бот мерзімдерді еске салып, хабарламалар жібере алады, ал Telegram арқылы да кіре аласыз. Аккаунттар біріктірілмейді: Telegram басқа аккаунтта болса, байланыстыру болмайды.',
        'С привязанным Telegram бот сможет напоминать о сроках и присылать уведомления, а входить можно будет и через Telegram. Аккаунты не объединяются: если этот Telegram уже в другом аккаунте, привязать не получится.')}</p>
      <TelegramSignIn label={tt('Telegram-ды байланыстыру', 'Привязать Telegram')} after={() => { closeSheet(); toast(tt('Telegram байланыстырылды', 'Telegram привязан')); }} />
    </>
  );
}

/* ---------- consent ---------- */
export function ConsentSheet() {
  const { tt, S, toast, closeSheet, go } = useApp();
  const acc = useAccount();
  const tg = useTelegram();
  const [agree, setAgree] = useState(false);
  const [adult, setAdult] = useState<'' | 'yes' | 'no'>('');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  const submit = async () => {
    setBusy(true); setErr('');
    try {
      await giveConsent(adult === '' ? null : adult === 'yes', S.lang);
      if (tg.app) requestWriteAccess();
      closeSheet();
      toast(tt('Келісім сақталды', 'Согласие сохранено'));
    } catch (e) { setErr(errText(e, '')); } finally { setBusy(false); }
  };
  return (
    <>
      <h2>{tt('Аккаунт және деректеріңіз', 'Аккаунт и ваши данные')}</h2>
      <div className="consent">
        <h3>{tt('Не сақталады', 'Что хранится')}</h3>
        {acc.user?.email ? <p className="small">{tt('Email адресіңіз және құпиясөздің хэші (құпиясөздің өзі сақталмайды).', 'Ваш email и хэш пароля (сам пароль не хранится).')}</p> : null}
        {acc.user?.googleLinked ? <p className="small">{tt('Google аккаунтыңыздың тұрақты идентификаторы, расталған email және аты: Google-дан тек осылар алынады, кіру Google серверлері (АҚШ) арқылы өтеді.', 'Постоянный идентификатор вашего аккаунта Google, подтверждённый email и имя: от Google берутся только они, вход идёт через серверы Google (США).')}</p> : null}
        <p className="small">{tt(
          'Telegram id, username және аты; тіл; өзіңіз жіберсеңіз — университет, факультет, курс, қала; рөліңіз, келісім уақыты, соңғы кіру уақыты; ботқа қойған еске салуларыңыз. Сессия студентке 90 күн (қолданған сайын ұзарады), қызметкер рөлдеріне 14 күн; «Шығу» сессияны серверде де жабады.',
          'Telegram id, username и имя; язык; если вы сами отправите — вуз, факультет, курс, город; ваша роль, время согласия и последнего входа; напоминания, которые вы поставили в боте. Сессия студенту 90 дней (продлевается при использовании), служебным ролям 14 дней; «Выйти» закрывает сессию и на сервере.')}</p>
        <p className="small"><b>{tt('GPA мен телефон аккаунтта ешқашан сақталмайды', 'GPA и телефон никогда не хранятся в аккаунте')}</b> — {tt('олар тек осы құрылғыда.', 'они только на этом устройстве.')}</p>
        <h3>{tt('Не үшін', 'Зачем')}</h3>
        <p className="small">{tt('Сізді танып, Telegram-да мерзімдерді еске салу және ортақ деректер керек әрекеттер үшін.', 'Чтобы узнавать вас, напоминать о сроках в Telegram и для действий с общими данными.')}</p>
        <h3>{tt('Кім көреді', 'Кто видит')}</h3>
        <p className="small">{tt('Сіз және SIVA әкімшілері ғана. Деректер сатылмайды және жарнамаға берілмейді.', 'Только вы и администраторы SIVA. Данные не продаются и не передаются для рекламы.')}</p>
        <h3>{tt('Қай сервистерге кетеді', 'Какие сервисы получают данные')}</h3>
        <p className="small">{tt(
          'Аккаунт деректері SIVA-ның жалға алынған серверіндегі (VPS) дерекқорда сақталады, күн сайын резервтік көшірмесі алынады (7 күн сақталады); сервер елі заңгер тексеруінде нақтыланады. Кіру Telegram не email арқылы өтеді; email кодтары бар хаттар Resend (АҚШ) поштасы арқылы жіберіледі. ЖИ чатты қолдансаңыз, хабарламаңыз бен профильдің бір бөлігі (мамандық, курс, GPA, ағылшын деңгейі, дағдылар, тәжірибе) жауап алу үшін ЖИ провайдеріне (OpenAI, АҚШ) жіберіледі, бізде сақталмайды. Аналитика анонимді, SIVA серверінде.',
          'Данные аккаунта хранятся в базе на арендованном сервере SIVA (VPS), ежедневно делается резервная копия (хранится 7 дней); страна сервера уточняется при проверке юристом. Вход идёт через Telegram или email; письма с кодами отправляются через почтовый сервис Resend (США). Если вы пользуетесь AI-чатом, сообщение и часть профиля (специальность, курс, GPA, уровень английского, навыки, опыт) отправляются AI-провайдеру (OpenAI, США) для ответа и у нас не хранятся. Аналитика анонимная, на сервере SIVA.')}</p>
        <h3>{tt('Қалай өшіріледі', 'Как удалить')}</h3>
        <p className="small">{tt('Баптаулар → Аккаунт → «Аккаунтты өшіру». Деректеріңіз бірден, толық өшеді. Кез келген уақытта JSON етіп жүктеп алуға болады.', 'Настройки → Аккаунт → «Удалить аккаунт». Данные удаляются сразу и полностью. В любой момент их можно скачать в JSON.')}</p>
        <button className="link-btn" type="button" onClick={() => go('privacy')}>{tt('Құпиялылық саясатын толық оқу', 'Читать политику конфиденциальности')}</button>
      </div>
      <fieldset className="consent-age">
        <legend className="small">{tt('Жасыңыз', 'Ваш возраст')}</legend>
        <label className="check"><input type="radio" name="adult" checked={adult === 'yes'} onChange={() => setAdult('yes')} /><span>{tt('18 жасқа толдым', 'Мне есть 18 лет')}</span></label>
        <label className="check"><input type="radio" name="adult" checked={adult === 'no'} onChange={() => setAdult('no')} /><span>{tt('18-ге толмадым', 'Мне нет 18 лет')}</span></label>
      </fieldset>
      <label className="check"><input type="checkbox" checked={agree} onChange={e => setAgree(e.target.checked)} /><span>{tt('Келісемін', 'Согласен(на)')}</span></label>
      {err ? <p className="small urgent" role="alert">{err}</p> : null}
      <div className="btn-row">
        <button className="btn ghost" type="button" onClick={closeSheet}>{tt('Кейінірек', 'Позже')}</button>
        <button className="btn" type="button" disabled={!agree || busy} onClick={submit}>{tt('Растау', 'Подтвердить')}</button>
      </div>
    </>
  );
}

/* ---------- delete ---------- */
function DeleteSheet() {
  const { tt, S, toast, closeSheet } = useApp();
  const [txt, setTxt] = useState('');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  const ok = txt.trim() === 'DELETE';
  return (
    <>
      <h2>{tt('Аккаунтты өшіру', 'Удалить аккаунт')}</h2>
      <p className="small">{tt(
        'Серверден аккаунтыңыз, еске салуларыңыз және бот жазбаларыңыз толық өшеді. Бұл әрекетті қайтару мүмкін емес. Осы құрылғыдағы профиль мен жоспарлар қалады.',
        'С сервера полностью удалятся аккаунт, напоминания и записи бота. Это нельзя отменить. Профиль и планы на этом устройстве останутся.')}</p>
      <div className="field">
        <label htmlFor="delConfirm">{tt('Растау үшін DELETE деп жазыңыз', 'Для подтверждения введите DELETE')}</label>
        <input className="input" id="delConfirm" autoComplete="off" autoCapitalize="characters" spellCheck={false} value={txt} onChange={e => setTxt(e.target.value)} />
      </div>
      {err ? <p className="small urgent" role="alert">{err}</p> : null}
      <div className="btn-row">
        <button className="btn ghost" type="button" onClick={closeSheet}>{tt('Бас тарту', 'Отмена')}</button>
        <button className="btn danger" type="button" disabled={!ok || busy} onClick={async () => {
          setBusy(true); setErr('');
          try { await deleteAccount(S.lang); closeSheet(); toast(tt('Аккаунт өшірілді', 'Аккаунт удалён')); } catch (e) { setErr(errText(e, '')); } finally { setBusy(false); }
        }}>{tt('Біржола өшіру', 'Удалить навсегда')}</button>
      </div>
    </>
  );
}

/* ---------- Settings → «Аккаунт» ---------- */
export function AccountSection() {
  const { S, tt, L, toast, openSheet } = useApp();
  const acc = useAccount();
  const cfg = useConfig();
  const tg = useTelegram();
  const [busy, setBusy] = useState('');
  const run = async (key: string, fn: () => Promise<void>, done: string) => {
    if (busy) return;
    setBusy(key);
    try { await fn(); if (done) toast(done); } catch (e) { toast(errText(e, tt('Болмады', 'Не получилось'))); } finally { setBusy(''); }
  };
  if (!acc.loaded) return null;
  const u = acc.user;
  if (!u) {
    return (
      <section className="section"><h2>{tt('Аккаунт', 'Аккаунт')}</h2>
        <div className="group">
          {cfg.googleLogin
            ? <Row onClick={() => openSheet(<SignInSheet />)} icon="user" title={tt('Аккаунтқа кіру', 'Войти в аккаунт')}
              sub={cfg.emailAuth
                ? tt('Telegram, Google немесе email арқылы. Міндетті емес: профиль осы құрылғыда кірмей-ақ жұмыс істейді.', 'Через Telegram, Google или email. Необязательно: профиль работает на этом устройстве и без входа.')
                : tt('Telegram немесе Google арқылы. Міндетті емес: профиль осы құрылғыда кірмей-ақ жұмыс істейді.', 'Через Telegram или Google. Необязательно: профиль работает на этом устройстве и без входа.')} />
            : cfg.emailAuth
            ? <Row onClick={() => openSheet(<SignInSheet />)} icon="user" title={tt('Аккаунтқа кіру', 'Войти в аккаунт')}
              sub={tt('Telegram немесе email арқылы. Міндетті емес: профиль осы құрылғыда кірмей-ақ жұмыс істейді.', 'Через Telegram или email. Необязательно: профиль работает на этом устройстве и без входа.')} />
            : <Row onClick={() => openSheet(<SignInSheet />)} icon="send" title={tt('Telegram арқылы кір', 'Войти через Telegram')}
              sub={tt('Міндетті емес. Профиль осы құрылғыда кірмей-ақ жұмыс істейді.', 'Необязательно. Профиль работает на этом устройстве и без входа.')} />}
        </div>
      </section>
    );
  }
  const p = S.profile;
  const sync = () => run('sync', () => syncProfile({
    name: p.name.trim(), lang: S.lang, university: SEIFULLIN,
    faculty: SPECS[p.spec] ? L(SPECS[p.spec]) : '', course: p.course, city: CITIES[p.city] ? L(CITIES[p.city]) : '',
  }, S.lang), tt('Аккаунтпен синхрондалды', 'Синхронизировано с аккаунтом'));
  return (
    <section className="section"><h2>{tt('Аккаунт', 'Аккаунт')}</h2>
      <div className="group">
        <Row icon="user" title={u.name || (u.username ? '@' + u.username : u.email ? <Addr email={u.email} /> : 'Telegram')} sub={[u.username ? '@' + u.username : '', roleLabel(u.role, tt)].filter(Boolean).join(' · ')} chev={false} />
        {/* email sign-in: shown only while the server offers it */}
        {cfg.emailAuth
          ? <Row onClick={() => openSheet(<EmailSheet />)} icon="mail" title={u.email ? tt('Email-ді өзгерту', 'Сменить email') : tt('Email қосу', 'Добавить email')}
            sub={u.email
              ? <><Addr email={u.email} /> <span className={`badge ${u.emailVerified ? 'b-ok' : 'b-sun'}`}>{u.emailVerified ? tt('расталған', 'подтверждён') : tt('расталмаған', 'не подтверждён')}</span></>
              : tt('Email мен құпиясөз арқылы да кіру үшін', 'Чтобы входить и по email с паролем')} />
          : null}
        {cfg.emailAuth && u.email
          ? <Row onClick={() => openSheet(<PasswordSheet />)} icon="key" title={tt('Құпиясөзді өзгерту', 'Сменить пароль')} sub={tt('Басқа құрылғылардағы кірулер жабылады', 'Входы на других устройствах закроются')} />
          : null}
        {u.telegramId
          ? (cfg.emailAuth ? <Row icon="send" title={tt('Telegram байланысқан', 'Telegram привязан')} sub={u.username ? '@' + u.username : tt('Бот хабарламалары қолжетімді', 'Сообщения бота доступны')} chev={false} /> : null)
          : <Row onClick={() => openSheet(<LinkTelegramSheet />)} icon="send" tone="sun" title={tt('Telegram-ды байланыстыру', 'Привязать Telegram')}
            sub={tt('Telegram жоқ: бот еске салу мен хабарлама жібере алмайды', 'Telegram не привязан: бот не сможет напоминать и присылать уведомления')} />}
        {/* Google: shown only while the server offers it; never inside the Mini App (Google refuses web views) */}
        {cfg.googleLogin && (u.googleLinked || !tg.app) ? <GoogleRow linked={!!u.googleLinked} /> : null}
        {acc.needsConsent
          ? <Row onClick={() => openSheet(<ConsentSheet />)} icon="shield" tone="sun" title={tt('Келісім беру', 'Дать согласие')} sub={tt('Аккаунтты қолдану үшін деректер туралы келісім керек', 'Чтобы пользоваться аккаунтом, нужно согласие на обработку данных')} />
          : <Row onClick={sync} icon="sync" title={tt('Аккаунтпен синхрондау', 'Синхронизировать с аккаунтом')}
            sub={busy === 'sync' ? tt('Жіберіліп жатыр…', 'Отправляем…') : tt('Тек аты, тіл, университет, факультет, курс және қала жіберіледі. GPA мен телефон — ешқашан.', 'Отправляются только имя, язык, вуз, факультет, курс и город. GPA и телефон — никогда.')} chev={false} />}
        <Row onClick={() => run('export', () => downloadExport(S.lang), tt('Файл жүктелді', 'Файл скачан'))} icon="file" title={tt('Деректерімді жүктеу', 'Скачать мои данные')} sub={tt('Сервердегі бар деректеріңіз, JSON файл', 'Все ваши данные с сервера, файл JSON')} chev={false} />
        {u.role === 'admin' || acc.console || u.scopes?.length ? <Row go="admin" icon="dashboard" title={u.role === 'admin' ? tt('Әкімші беті', 'Панель администратора') : tt('Басқару панелі', 'Панель управления')}
          sub={u.role === 'admin' ? undefined : tt('Сіз басқаратын бөлімдер: кофехана, клуб, модерация…', 'Разделы, которыми вы управляете: кофейня, клуб, модерация…')} /> : null}
        {u.blocked ? <Row icon="shield" tone="bad" title={tt('Аккаунт шектелген', 'Аккаунт ограничен')} chev={false}
          sub={tt('Әкімші шешімімен жазу, жариялау және тапсырыс беру уақытша жабық. Оқу, шығу және аккаунтты өшіру жұмыс істейді.', 'По решению администратора писать, публиковать и заказывать временно нельзя. Чтение, выход и удаление аккаунта работают.')} /> : null}
        <Row onClick={() => run('out', () => signOut(false, S.lang), tt('Шықтыңыз', 'Вы вышли'))} icon="logout" title={tt('Шығу', 'Выйти')} chev={false} />
        <Row onClick={() => run('outAll', () => signOut(true, S.lang), tt('Барлық құрылғыдан шықтыңыз', 'Вы вышли на всех устройствах'))} icon="x" title={tt('Барлық құрылғыдан шығу', 'Выйти на всех устройствах')} chev={false} />
        <Row onClick={() => openSheet(<DeleteSheet />)} icon="trash" tone="bad" title={tt('Аккаунтты өшіру', 'Удалить аккаунт')} sub={tt('Сервердегі деректер толық өшеді', 'Данные на сервере удалятся полностью')} chev={false} />
      </div>
    </section>
  );
}
