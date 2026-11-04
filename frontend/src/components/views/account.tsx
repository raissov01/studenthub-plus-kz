'use client';
/* Optional account: sign-in prompt (Telegram: Login Widget outside Telegram, a button inside the Mini App; Email when
   the server offers it, see account-email.tsx), the consent screen, and the «Аккаунт» section of Settings (email,
   password and Telegram link, sync, export, sign out, delete). The device profile keeps working without any of this. */
import { useEffect, useRef, useState } from 'react';
import { useApp } from '@/components/app';
import { Icon, Row, Seg } from '@/components/ui';
import { Addr, EmailSheet, EmailSignIn, PasswordSheet } from '@/components/views/account-email';
import {
  ApiError, deleteAccount, downloadExport, giveConsent, signInWebApp, signInWidget, signOut, syncProfile, useAccount,
  type Role,
} from '@/lib/account';
import { useConfig } from '@/lib/config';
import { CITIES, SPECS } from '@/lib/data/static';
import { requestWriteAccess, useTelegram } from '@/lib/telegram';

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
    if (S.lang === 'ru') s.setAttribute('data-lang', 'ru');
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
          ? <LoginWidget bot={cfg.botUsername} onUser={onWidget} />
          : <p className="note">{tt('Telegram арқылы кіру әзірге қосылмаған.', 'Вход через Telegram пока не подключён.')}</p>}
      {busy ? <p className="tiny muted" role="status">{tt('Кіріп жатырмыз…', 'Входим…')}</p> : null}
      {err ? <p className="small urgent" role="alert">{err}</p> : null}
      <p className="tiny muted">{tt('Кіргенде Telegram атыңыз бен username-іңіз ғана алынады. Телефон нөміріңіз бізге берілмейді.', 'При входе мы получаем только имя и username в Telegram. Номер телефона нам не передаётся.')}</p>
    </>
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
  return (
    <>
      <h2>{cfg.emailAuth ? tt('Аккаунтқа кіру', 'Вход в аккаунт') : tt('Telegram арқылы кіру', 'Вход через Telegram')}</h2>
      {deep && email ? null : <p className="small muted">{tt(
        'Кіру міндетті емес: профиль, жоспарлар мен сақталғандар осы құрылғыда кірмей-ақ жұмыс істейді. Аккаунт тек ортақ деректер керек әрекеттерге қажет.',
        'Вход необязателен: профиль, планы и сохранённое работают на этом устройстве и без него. Аккаунт нужен только для действий с общими данными.')}</p>}
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
        <p className="small">{tt(
          'Telegram id, username және аты; тіл; өзіңіз жіберсеңіз — университет, факультет, курс, қала; рөліңіз, келісім уақыты, соңғы кіру уақыты; ботқа қойған еске салуларыңыз. Сессия студентке 90 күн (қолданған сайын ұзарады), қызметкер рөлдеріне 14 күн; «Шығу» сессияны серверде де жабады.',
          'Telegram id, username и имя; язык; если вы сами отправите — вуз, факультет, курс, город; ваша роль, время согласия и последнего входа; напоминания, которые вы поставили в боте. Сессия студенту 90 дней (продлевается при использовании), служебным ролям 14 дней; «Выйти» закрывает сессию и на сервере.')}</p>
        <p className="small"><b>{tt('GPA мен телефон аккаунтта ешқашан сақталмайды', 'GPA и телефон никогда не хранятся в аккаунте')}</b> — {tt('олар тек осы құрылғыда.', 'они только на этом устройстве.')}</p>
        <h3>{tt('Не үшін', 'Зачем')}</h3>
        <p className="small">{tt('Сізді танып, Telegram-да мерзімдерді еске салу және ортақ деректер керек әрекеттер үшін.', 'Чтобы узнавать вас, напоминать о сроках в Telegram и для действий с общими данными.')}</p>
        <h3>{tt('Кім көреді', 'Кто видит')}</h3>
        <p className="small">{tt('Сіз және Student Hub әкімшілері ғана. Деректер сатылмайды және жарнамаға берілмейді.', 'Только вы и администраторы Student Hub. Данные не продаются и не передаются для рекламы.')}</p>
        <h3>{tt('Қай сервистерге кетеді', 'Какие сервисы получают данные')}</h3>
        <p className="small">{tt(
          'Аккаунт деректері Student Hub-тың жалға алынған серверіндегі (VPS) дерекқорда сақталады, күн сайын резервтік көшірмесі алынады (7 күн сақталады); сервер елі заңгер тексеруінде нақтыланады. Кіру Telegram не email арқылы өтеді; email кодтары бар хаттар Resend (АҚШ) поштасы арқылы жіберіледі. AI-чатты қолдансаңыз, хабарламаңыз бен профильдің бір бөлігі (мамандық, курс, GPA, ағылшын деңгейі, дағдылар, тәжірибе) жауап алу үшін AI провайдеріне (OpenAI, АҚШ) жіберіледі, бізде сақталмайды. Аналитика анонимді, Student Hub серверінде.',
          'Данные аккаунта хранятся в базе на арендованном сервере Student Hub (VPS), ежедневно делается резервная копия (хранится 7 дней); страна сервера уточняется при проверке юристом. Вход идёт через Telegram или email; письма с кодами отправляются через почтовый сервис Resend (США). Если вы пользуетесь AI-чатом, сообщение и часть профиля (специальность, курс, GPA, уровень английского, навыки, опыт) отправляются AI-провайдеру (OpenAI, США) для ответа и у нас не хранятся. Аналитика анонимная, на сервере Student Hub.')}</p>
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
          {cfg.emailAuth
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
    name: p.name.trim(), lang: S.lang, university: tt('Сейфуллин университеті', 'Университет им. С. Сейфуллина'),
    faculty: SPECS[p.spec] ? L(SPECS[p.spec]) : '', course: p.course, city: CITIES[p.city] ? L(CITIES[p.city]) : '',
  }, S.lang), tt('Аккаунтпен синхрондалды', 'Синхронизировано с аккаунтом'));
  return (
    <section className="section"><h2>{tt('Аккаунт', 'Аккаунт')}</h2>
      <div className="group">
        <Row icon="user" title={u.name || (u.username ? '@' + u.username : u.email ? <Addr email={u.email} /> : 'Telegram')} sub={[u.username ? '@' + u.username : '', roleLabel(u.role, tt)].filter(Boolean).join(' · ')} chev={false} />
        {/* email sign-in: shown only while the server offers it */}
        {cfg.emailAuth
          ? <Row onClick={() => openSheet(<EmailSheet />)} icon="chat" title={u.email ? tt('Email-ді өзгерту', 'Сменить email') : tt('Email қосу', 'Добавить email')}
            sub={u.email
              ? <><Addr email={u.email} /> <span className={`badge ${u.emailVerified ? 'b-ok' : 'b-sun'}`}>{u.emailVerified ? tt('расталған', 'подтверждён') : tt('расталмаған', 'не подтверждён')}</span></>
              : tt('Email мен құпиясөз арқылы да кіру үшін', 'Чтобы входить и по email с паролем')} />
          : null}
        {cfg.emailAuth && u.email
          ? <Row onClick={() => openSheet(<PasswordSheet />)} icon="shield" title={tt('Құпиясөзді өзгерту', 'Сменить пароль')} sub={tt('Басқа құрылғылардағы кірулер жабылады', 'Входы на других устройствах закроются')} />
          : null}
        {u.telegramId
          ? (cfg.emailAuth ? <Row icon="send" title={tt('Telegram байланысқан', 'Telegram привязан')} sub={u.username ? '@' + u.username : tt('Бот хабарламалары қолжетімді', 'Сообщения бота доступны')} chev={false} /> : null)
          : <Row onClick={() => openSheet(<LinkTelegramSheet />)} icon="send" tone="sun" title={tt('Telegram-ды байланыстыру', 'Привязать Telegram')}
            sub={tt('Telegram жоқ: бот еске салу мен хабарлама жібере алмайды', 'Telegram не привязан: бот не сможет напоминать и присылать уведомления')} />}
        {acc.needsConsent
          ? <Row onClick={() => openSheet(<ConsentSheet />)} icon="shield" tone="sun" title={tt('Келісім беру', 'Дать согласие')} sub={tt('Аккаунтты қолдану үшін деректер туралы келісім керек', 'Чтобы пользоваться аккаунтом, нужно согласие на обработку данных')} />
          : <Row onClick={sync} icon="copy" title={tt('Аккаунтпен синхрондау', 'Синхронизировать с аккаунтом')}
            sub={busy === 'sync' ? tt('Жіберіліп жатыр…', 'Отправляем…') : tt('Тек аты, тіл, университет, факультет, курс және қала жіберіледі. GPA мен телефон — ешқашан.', 'Отправляются только имя, язык, вуз, факультет, курс и город. GPA и телефон — никогда.')} chev={false} />}
        <Row onClick={() => run('export', () => downloadExport(S.lang), tt('Файл жүктелді', 'Файл скачан'))} icon="file" title={tt('Деректерімді жүктеу', 'Скачать мои данные')} sub={tt('Сервердегі бар деректеріңіз, JSON файл', 'Все ваши данные с сервера, файл JSON')} chev={false} />
        {u.role === 'admin' ? <Row go="admin" icon="chart" title={tt('Әкімші беті', 'Панель администратора')} /> : null}
        <Row onClick={() => run('out', () => signOut(false, S.lang), tt('Шықтыңыз', 'Вы вышли'))} icon="back" title={tt('Шығу', 'Выйти')} chev={false} />
        <Row onClick={() => run('outAll', () => signOut(true, S.lang), tt('Барлық құрылғыдан шықтыңыз', 'Вы вышли на всех устройствах'))} icon="x" title={tt('Барлық құрылғыдан шығу', 'Выйти на всех устройствах')} chev={false} />
        <Row onClick={() => openSheet(<DeleteSheet />)} icon="trash" tone="bad" title={tt('Аккаунтты өшіру', 'Удалить аккаунт')} sub={tt('Сервердегі деректер толық өшеді', 'Данные на сервере удалятся полностью')} chev={false} />
      </div>
    </section>
  );
}
