'use client';
/* «Жария портфолио» (prompt 09) inside the existing portfolio screen: the page is off until the student turns it on;
   the first time, an explicit question decides whether the device's experience, skills and certificates are saved to
   the account (otherwise only system-verified items appear). Block toggles, name / headline / bio, link actions
   (copy, share, QR, new link) and a preview rendered by the same component as the public page. Also the
   «Түйіндемеге портфолио сілтемесін қосу» checkbox for the résumé tool. GPA, phone and email never leave the device. */
import { useEffect, useState, useSyncExternalStore } from 'react';
import { copyText, useApp } from '@/components/app';
import { PortfolioPage } from '@/components/PortfolioPage';
import { Icon, Qr, Row, SectionHead } from '@/components/ui';
import { ErrorLine, errCode, errText, Loading, SignInCard, useFeature, useNeedAccount } from '@/components/views/common';
import { api, useAccount } from '@/lib/account';
import { localItems, PF_LIMITS, TOGGLES, type PortfolioMe, type PortfolioSettings, type ToggleKey } from '@/lib/portfolio';

/* one shared copy of GET /api/portfolio/me (settings screen and résumé) */
let cache: { uid: number; data: PortfolioMe | null; err: string } | null = null;
let loading = 0;
const subs = new Set<() => void>();
const sub = (l: () => void) => { subs.add(l); return () => { subs.delete(l); }; };
const emit = () => subs.forEach(l => l());
function fetchMe(uid: number, lang: 'kk' | 'ru') {
  if (loading === uid) return;
  loading = uid;
  api<PortfolioMe>('/api/portfolio/me', { lang }).then(
    d => { cache = { uid, data: d, err: '' }; },
    e => { cache = { uid, data: cache?.uid === uid ? cache.data : null, err: errCode(e) === 'consent_required' ? '' : errText(e) }; },
  ).finally(() => { loading = 0; emit(); });
}
function setSettings(uid: number, s: PortfolioSettings) {
  if (cache?.uid === uid && cache.data) { cache = { ...cache, data: { ...cache.data, settings: s } }; emit(); }
}

function usePortfolio() {
  const { lang } = useApp();
  const acc = useAccount();
  const on = useFeature('portfolio');
  const uid = on && acc.user && !acc.needsConsent ? acc.user.id : 0;
  const snap = useSyncExternalStore(sub, () => cache, () => null);
  useEffect(() => { if (uid && cache?.uid !== uid) fetchMe(uid, lang); }, [uid, lang]);
  const cur = uid && snap?.uid === uid ? snap : null;
  return { on, uid, data: cur?.data ?? null, err: cur?.err ?? '', reload: () => { if (uid) fetchMe(uid, lang); } };
}

/** the résumé's «Түйіндемеге портфолио сілтемесін қосу»: offered only while the public page is on */
export function usePortfolioLink(): string {
  const { data } = usePortfolio();
  return data?.settings.enabled ? data.settings.url : '';
}

export function ResumePortfolioCheck({ checked, onChange }: { checked: boolean; onChange: (v: boolean) => void }) {
  const { tt } = useApp();
  const url = usePortfolioLink();
  if (!url) return null;
  return (
    <label className="check">
      <input type="checkbox" checked={checked} onChange={e => onChange(e.target.checked)} />
      <span className="small">{tt('Түйіндемеге портфолио сілтемесін қосу', 'Добавить в резюме ссылку на портфолио')}</span>
    </label>
  );
}

/** «Жария портфолио» block at the top of /portfolio */
export function PublicPortfolioSection() {
  const { tt } = useApp();
  const acc = useAccount();
  const pf = usePortfolio();
  if (!pf.on) return null;
  const head = <SectionHead title={tt('Жария портфолио', 'Публичное портфолио')} />;
  if (!acc.loaded) return null;
  if (!acc.user || acc.needsConsent) {
    return <section className="section">{head}<SignInCard text={tt('Портфолионы жұмыс берушіге не стипендия комиссиясына бір сілтемемен көрсетіңіз. Ол үшін Telegram арқылы кіріңіз — бет әдепкіде өшірулі.', 'Покажите портфолио работодателю или стипендиальной комиссии одной ссылкой. Для этого войдите через Telegram — страница по умолчанию выключена.')} /></section>;
  }
  if (!pf.data) return <section className="section">{head}{pf.err ? <ErrorLine err={pf.err} /> : <Loading />}</section>;
  return <section className="section">{head}<Settings me={pf.data} uid={pf.uid} reload={pf.reload} /></section>;
}

function Settings({ me, uid, reload }: { me: PortfolioMe; uid: number; reload: () => void }) {
  const { S, tt, L, lang, toast, openSheet } = useApp();
  const need = useNeedAccount();
  const s = me.settings;
  const [busy, setBusy] = useState('');
  const [err, setErr] = useState('');
  const patch = async (body: Partial<PortfolioSettings> & { syncConsent?: boolean }, key: string, done?: string) => {
    if (!need()) return false;
    setBusy(key); setErr('');
    try {
      const r = await api<{ settings: PortfolioSettings }>('/api/portfolio/settings', { method: 'PATCH', body, lang });
      setSettings(uid, r.settings);
      reload(); // the preview follows the new settings
      if (done) toast(done);
      return true;
    } catch (e) { setErr(errText(e)); return false; } finally { setBusy(''); }
  };
  const turnOn = () => {
    if (s.syncConsentAt) { void patch({ enabled: true }, 'enabled', tt('Жария бет қосылды', 'Публичная страница включена')); return; }
    openSheet(<SyncQuestion onAnswer={async yes => {
      if (yes) {
        const ok = await sync(true);
        if (!ok) return;
      }
      await patch({ enabled: true }, 'enabled', tt('Жария бет қосылды', 'Публичная страница включена'));
    }} />);
  };
  const sync = async (consent: boolean) => {
    setBusy('sync'); setErr('');
    try {
      await api('/api/portfolio/items', { method: 'PUT', body: { consent, items: localItems(S, lang) }, lang });
      reload();
      return true;
    } catch (e) { setErr(errText(e)); return false; } finally { setBusy(''); }
  };
  const local = localItems(S, lang);
  return (
    <>
      <div className="card">
        <label className="check">
          <input type="checkbox" checked={s.enabled} disabled={!!busy} onChange={e => { if (e.target.checked) turnOn(); else void patch({ enabled: false }, 'enabled', tt('Жария бет өшірілді: сілтеме бірден ашылмайды', 'Страница выключена: ссылка сразу перестала открываться')); }} />
          <span className="row-main"><span className="row-title">{tt('Жария бет', 'Публичная страница')}</span>
            <span className="row-sub">{s.enabled ? tt('Қосулы: сілтемесі бар кез келген адам көре алады (іздеу жүйелеріне түспейді).', 'Включена: видит любой, у кого есть ссылка (в поисковики не попадает).') : tt('Өшірулі: сілтеме «Мұндай бет жоқ» көрсетеді.', 'Выключена: по ссылке показывается «Такой страницы нет».')}</span></span>
        </label>
        <p className="tiny muted">{tt('GPA, телефон және email жария бетке ешқашан шықпайды.', 'GPA, телефон и email никогда не попадают на публичную страницу.')}</p>
        <LinkActions s={s} onRotated={reload} />
      </div>
      <ErrorLine err={err} />
      <TextFields s={s} busy={!!busy} onSave={b => patch(b, 'text', tt('Сақталды', 'Сохранено'))} />
      <section className="section">
        <h3>{tt('Бетте не көрінеді', 'Что показывать')}</h3>
        <div className="group">
          {TOGGLES.map(([k, l]) => (
            <label key={k} className="row check">
              <input type="checkbox" checked={s[k]} disabled={!!busy} onChange={e => void patch({ [k]: e.target.checked } as Partial<Record<ToggleKey, boolean>>, k)} />
              <span className="row-main"><span className="row-title">{L(l)}</span>
                {k === 'showContact' ? <span className="row-sub">{tt('Telegram username-іңіз арқылы. Әдепкіде өшірулі.', 'Через ваш username в Telegram. По умолчанию выключено.')}</span> : null}</span>
            </label>
          ))}
        </div>
      </section>
      <section className="section">
        <h3>{tt('Өзі көрсеткен жазбалар', 'Указанное самим студентом')}</h3>
        {s.syncConsentAt ? (
          <>
            <p className="small">{tt(`Аккаунтта сақталған: ${me.items.length}. Құрылғыда: ${local.length}.`, `Сохранено в аккаунте: ${me.items.length}. На устройстве: ${local.length}.`)}</p>
            <div className="btn-row">
              <button className="btn secondary" type="button" disabled={!!busy} onClick={async () => { if (await sync(false)) toast(tt('Жаңартылды', 'Обновлено')); }}>{tt('Қайта синхрондау', 'Синхронизировать снова')}</button>
              <button className="btn ghost" type="button" disabled={!!busy} onClick={async () => {
                setBusy('del'); setErr('');
                try { await api('/api/portfolio/items', { method: 'DELETE', lang }); toast(tt('Сақтау тоқтатылды, жазбалар өшірілді', 'Сохранение остановлено, записи удалены')); reload(); } catch (e) { setErr(errText(e)); } finally { setBusy(''); }
              }}>{tt('Сақтауды тоқтату', 'Перестать сохранять')}</button>
            </div>
            <p className="tiny muted">{tt('Құрылғыдағы тәжірибе, дағды не сертификатты өзгерткен соң «Қайта синхрондау» басыңыз.', 'Изменили опыт, навыки или сертификаты на устройстве — нажмите «Синхронизировать снова».')}</p>
          </>
        ) : (
          <>
            <p className="small">{tt('Тәжірибе, дағдылар мен сертификаттар тек осы құрылғыда. Бетте әзірге тек жүйе растаған жазбалар көрінеді.', 'Опыт, навыки и сертификаты есть только на этом устройстве. На странице пока видны только подтверждённые системой записи.')}</p>
            <button className="btn secondary" type="button" disabled={!!busy || !local.length} onClick={() => openSheet(<SyncQuestion onAnswer={async yes => { if (yes && await sync(true)) toast(tt('Аккаунтқа сақталды', 'Сохранено в аккаунте')); }} />)}>
              {tt('Аккаунтқа сақтау', 'Сохранить в аккаунте')}</button>
          </>
        )}
      </section>
      <button className="btn ghost block" type="button" onClick={() => openSheet(<PreviewSheet me={me} />)}><Icon n="user" s={18} /> {tt('Бетті алдын ала қарау', 'Предпросмотр страницы')}</button>
    </>
  );
}

function SyncQuestion({ onAnswer }: { onAnswer: (yes: boolean) => Promise<void> }) {
  const { S, tt, lang, closeSheet } = useApp();
  const items = localItems(S, lang);
  const n = (k: string) => items.filter(i => i.kind === k).length;
  const [busy, setBusy] = useState(false);
  const answer = async (yes: boolean) => { setBusy(true); closeSheet(); await onAnswer(yes); };
  return (
    <>
      <h2>{tt('Тәжірибе, дағдылар, сертификаттарды аккаунтқа сақтаймыз ба?', 'Сохранить опыт, навыки и сертификаты в аккаунте?')}</h2>
      <p className="small">{tt(`Олар қазір тек осы құрылғыда: тәжірибе — ${n('experience')}, дағды — ${n('skill')}, сертификат — ${n('cert')}. Сақтасаңыз, жария бетте «Өзі көрсеткен» бөлімінде шығады.`, `Сейчас они только на этом устройстве: опыт — ${n('experience')}, навыки — ${n('skill')}, сертификаты — ${n('cert')}. Если сохранить, они появятся на странице в разделе «Указано самим студентом».`)}</p>
      <p className="tiny muted">{tt('«Жоқ» десеңіз, бетте тек жүйе растаған жазбалар болады. Кейін кез келген уақытта өзгертуге болады. GPA, телефон және email жіберілмейді.', 'Если «Нет», на странице будут только подтверждённые системой записи. Решение можно изменить в любой момент. GPA, телефон и email не отправляются.')}</p>
      <div className="btn-row">
        <button className="btn ghost" type="button" disabled={busy} onClick={() => answer(false)}>{tt('Жоқ', 'Нет')}</button>
        <button className="btn" type="button" disabled={busy || !items.length} onClick={() => answer(true)}>{tt('Иә, сақтау', 'Да, сохранить')}</button>
      </div>
      <button className="link-btn" type="button" style={{ alignSelf: 'center' }} onClick={closeSheet}>{tt('Бас тарту', 'Отмена')}</button>
    </>
  );
}

function TextFields({ s, busy, onSave }: { s: PortfolioSettings; busy: boolean; onSave: (b: Partial<PortfolioSettings>) => Promise<boolean> }) {
  const { tt } = useApp();
  const [name, setName] = useState(s.displayName ?? '');
  const [headline, setHeadline] = useState(s.headline ?? '');
  const [bio, setBio] = useState(s.bio ?? '');
  const n = (v: string) => (v.trim() ? v.trim() : null);
  const dirty = n(name) !== s.displayName || n(headline) !== s.headline || n(bio) !== s.bio;
  return (
    <form className="form" onSubmit={e => { e.preventDefault(); if (dirty) void onSave({ displayName: n(name), headline: n(headline), bio: n(bio) }); }}>
      <div className="field"><label htmlFor="pfName">{tt('Аты (бос болса — аккаунттағы аты)', 'Имя (пусто — имя из аккаунта)')}</label>
        <input className="input" id="pfName" maxLength={PF_LIMITS.displayName} value={name} onChange={e => setName(e.target.value)} /></div>
      <div className="field"><label htmlFor="pfHead">{tt('Қысқаша (мысалы, «Backend стажер»)', 'Кратко (например, «Стажёр backend»)')}</label>
        <input className="input" id="pfHead" maxLength={PF_LIMITS.headline} value={headline} onChange={e => setHeadline(e.target.value)} />
        <span className="tiny muted num">{headline.length} / {PF_LIMITS.headline}</span></div>
      <div className="field"><label htmlFor="pfBio">{tt('Өзім туралы', 'О себе')}</label>
        <textarea className="input" id="pfBio" maxLength={PF_LIMITS.bio} value={bio} onChange={e => setBio(e.target.value)} />
        <span className="tiny muted num">{bio.length} / {PF_LIMITS.bio}</span></div>
      <button className="btn secondary" type="submit" disabled={!dirty || busy}>{tt('Сақтау', 'Сохранить')}</button>
    </form>
  );
}

function LinkActions({ s, onRotated }: { s: PortfolioSettings; onRotated: () => void }) {
  const { tt, toast, openSheet } = useApp();
  const url = s.url;
  const share = () => {
    const text = tt('Менің портфолиом — Student Hub', 'Моё портфолио — Student Hub');
    if (navigator.share && window.matchMedia('(pointer: coarse)').matches) navigator.share({ title: text, url }).catch(() => { /* cancelled */ });
    else window.open('https://t.me/share/url?url=' + encodeURIComponent(url) + '&text=' + encodeURIComponent(text), '_blank', 'noopener');
  };
  return (
    <>
      <div className="pre small" style={{ overflowWrap: 'anywhere' }}>{url}</div>
      <div className="chips wrap">
        <button className="chip" type="button" onClick={() => copyText(url, toast, tt)}><Icon n="copy" s={16} /> {tt('Сілтемені көшіру', 'Скопировать ссылку')}</button>
        <button className="chip" type="button" onClick={share}><Icon n="share" s={16} /> {tt('Бөлісу', 'Поделиться')}</button>
        <button className="chip" type="button" onClick={() => openSheet(<QrSheet url={url} />)}><Icon n="qr" s={16} /> {tt('QR-код', 'QR-код')}</button>
        <button className="chip" type="button" onClick={() => openSheet(<RotateSheet onDone={onRotated} />)}><Icon n="x" s={16} /> {tt('Жаңа сілтеме жасау', 'Создать новую ссылку')}</button>
      </div>
      {!s.enabled ? <p className="tiny muted">{tt('Бет өшірулі тұрғанда сілтеме ашылмайды.', 'Пока страница выключена, ссылка не открывается.')}</p> : null}
    </>
  );
}

function QrSheet({ url }: { url: string }) {
  const { tt, closeSheet } = useApp();
  return (
    <>
      <h2>{tt('Портфолио QR-коды', 'QR-код портфолио')}</h2>
      <Qr text={url} />
      <p className="tiny muted" style={{ textAlign: 'center', overflowWrap: 'anywhere' }}>{url}</p>
      <button className="btn ghost block" type="button" onClick={closeSheet}>{tt('Жабу', 'Закрыть')}</button>
    </>
  );
}

function RotateSheet({ onDone }: { onDone: () => void }) {
  const { tt, lang, toast, closeSheet } = useApp();
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  return (
    <>
      <h2>{tt('Жаңа сілтеме жасау керек пе?', 'Создать новую ссылку?')}</h2>
      <p className="small">{tt('Ескі сілтеме бірден жұмыс істемей қалады — оны кімге жібердіңіз, солар «Мұндай бет жоқ» көреді. Сілтеме тарап кетсе, осылай қайтарып аласыз.', 'Старая ссылка сразу перестанет работать — все, кому вы её отправили, увидят «Такой страницы нет». Так можно отозвать ссылку, если она разошлась.')}</p>
      <ErrorLine err={err} />
      <div className="btn-row">
        <button className="btn ghost" type="button" onClick={closeSheet}>{tt('Бас тарту', 'Отмена')}</button>
        <button className="btn danger" type="button" disabled={busy} onClick={async () => {
          setBusy(true); setErr('');
          try { await api('/api/portfolio/slug', { method: 'POST', lang }); closeSheet(); toast(tt('Жаңа сілтеме дайын, ескісі жабылды', 'Новая ссылка готова, старая закрыта')); onDone(); }
          catch (e) { setErr(errText(e)); } finally { setBusy(false); }
        }}>{tt('Иә, жаңа сілтеме', 'Да, новая ссылка')}</button>
      </div>
    </>
  );
}

function PreviewSheet({ me }: { me: PortfolioMe }) {
  const { tt, lang, closeSheet } = useApp();
  return (
    <>
      <p className="tiny muted">{me.settings.enabled ? tt('Жұмыс беруші осылай көреді.', 'Так страницу увидит работодатель.') : tt('Бет өшірулі. Қосқанда осылай көрінеді.', 'Страница выключена. Так она будет выглядеть после включения.')}</p>
      <PortfolioPage page={me.preview} lang={lang} preview />
      <Row onClick={closeSheet} icon="x" title={tt('Жабу', 'Закрыть')} chev={false} />
    </>
  );
}
