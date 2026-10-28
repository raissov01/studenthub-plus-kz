'use client';
/* Optional Telegram account: sign-in prompt (Login Widget outside Telegram, automatic inside the Mini App), the
   consent screen, and the «Аккаунт» section of Settings (sync, export, sign out, delete). The device profile keeps
   working without any of this. */
import { useEffect, useRef, useState } from 'react';
import { useApp } from '@/components/app';
import { Icon, Row } from '@/components/ui';
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

export function SignInSheet() {
  const { tt, S, toast, closeSheet, openSheet } = useApp();
  const cfg = useConfig();
  const tg = useTelegram();
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  const after = (needsConsent: boolean) => {
    if (needsConsent) openSheet(<ConsentSheet />);
    else { closeSheet(); toast(tt('Кірдіңіз', 'Вы вошли')); }
  };
  const onWidget = async (u: Record<string, unknown>) => {
    setBusy(true); setErr('');
    try { const st = await signInWidget(u, S.lang); after(st.needsConsent); } catch (e) { setErr(errText(e, '')); } finally { setBusy(false); }
  };
  const onMiniApp = async () => {
    if (!tg.app) return;
    setBusy(true); setErr('');
    try { const st = await signInWebApp(tg.app.initData); after(st.needsConsent); } catch (e) { setErr(errText(e, '')); } finally { setBusy(false); }
  };
  return (
    <>
      <h2>{tt('Telegram арқылы кіру', 'Вход через Telegram')}</h2>
      <p className="small muted">{tt(
        'Кіру міндетті емес: профиль, жоспарлар мен сақталғандар осы құрылғыда кірмей-ақ жұмыс істейді. Аккаунт тек ортақ деректер керек әрекеттерге қажет.',
        'Вход необязателен: профиль, планы и сохранённое работают на этом устройстве и без него. Аккаунт нужен только для действий с общими данными.')}</p>
      {tg.app
        ? <button className="btn block" type="button" disabled={busy} onClick={onMiniApp}><Icon n="send" s={18} /> {tt('Telegram арқылы кір', 'Войти через Telegram')}</button>
        : cfg.botUsername
          ? <LoginWidget bot={cfg.botUsername} onUser={onWidget} />
          : <p className="note">{tt('Telegram арқылы кіру әзірге қосылмаған.', 'Вход через Telegram пока не подключён.')}</p>}
      {busy ? <p className="tiny muted" role="status">{tt('Кіріп жатырмыз…', 'Входим…')}</p> : null}
      {err ? <p className="small urgent" role="alert">{err}</p> : null}
      <p className="tiny muted">{tt('Кіргенде Telegram атыңыз бен username-іңіз ғана алынады. Телефон нөміріңіз бізге берілмейді.', 'При входе мы получаем только имя и username в Telegram. Номер телефона нам не передаётся.')}</p>
    </>
  );
}

/* ---------- consent ---------- */
export function ConsentSheet() {
  const { tt, S, toast, closeSheet, go } = useApp();
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
        <p className="small">{tt(
          'Telegram id, username және аты; тіл; өзіңіз жіберсеңіз — университет, факультет, курс, қала; рөліңіз, келісім уақыты, соңғы кіру уақыты; ботқа қойған еске салуларыңыз.',
          'Telegram id, username и имя; язык; если вы сами отправите — вуз, факультет, курс, город; ваша роль, время согласия и последнего входа; напоминания, которые вы поставили в боте.')}</p>
        <p className="small"><b>{tt('GPA мен телефон аккаунтта ешқашан сақталмайды', 'GPA и телефон никогда не хранятся в аккаунте')}</b> — {tt('олар тек осы құрылғыда.', 'они только на этом устройстве.')}</p>
        <h3>{tt('Не үшін', 'Зачем')}</h3>
        <p className="small">{tt('Сізді танып, Telegram-да мерзімдерді еске салу және ортақ деректер керек әрекеттер үшін.', 'Чтобы узнавать вас, напоминать о сроках в Telegram и для действий с общими данными.')}</p>
        <h3>{tt('Кім көреді', 'Кто видит')}</h3>
        <p className="small">{tt('Сіз және Student Hub әкімшілері ғана. Деректер сатылмайды және жарнамаға берілмейді.', 'Только вы и администраторы Student Hub. Данные не продаются и не передаются для рекламы.')}</p>
        <h3>{tt('Қай сервистерге кетеді', 'Какие сервисы получают данные')}</h3>
        <p className="small">{tt(
          'Аккаунт деректері Student Hub серверіндегі дерекқорда сақталады. Кіру Telegram арқылы өтеді. AI-чатты қолдансаңыз, хабарламаңыз бен профильдің бір бөлігі (аты, мамандық, курс, GPA, ағылшын деңгейі, дағдылар, тәжірибе) жауап алу үшін AI провайдеріне (OpenAI, АҚШ) жіберіледі, бізде сақталмайды. Аналитика анонимді, Student Hub серверінде.',
          'Данные аккаунта хранятся в базе на сервере Student Hub. Вход идёт через Telegram. Если вы пользуетесь AI-чатом, сообщение и часть профиля (имя, специальность, курс, GPA, уровень английского, навыки, опыт) отправляются AI-провайдеру (OpenAI, США) для ответа и у нас не хранятся. Аналитика анонимная, на сервере Student Hub.')}</p>
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
          <Row onClick={() => openSheet(<SignInSheet />)} icon="send" title={tt('Telegram арқылы кір', 'Войти через Telegram')}
            sub={tt('Міндетті емес. Профиль осы құрылғыда кірмей-ақ жұмыс істейді.', 'Необязательно. Профиль работает на этом устройстве и без входа.')} />
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
        <Row icon="user" title={u.name || (u.username ? '@' + u.username : 'Telegram')} sub={[u.username ? '@' + u.username : '', roleLabel(u.role, tt)].filter(Boolean).join(' · ')} chev={false} />
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
