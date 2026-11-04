'use client';
/* «Мүмкіндік білесің бе? Ұсын» on the site (docs/SUGGEST.md): /suggest — the form (Telegram sign-in and consent first,
   POST /api/submissions, no personal data asked), /suggestions — «Менің ұсыныстарым» with the status and the return
   reason, and the admin's «Түзету» sheet for a pending opportunity / place in /admin/queue (PATCH /api/admin/queue/{id}),
   all three with the same fields and rules. Not behind the ambassador switch. */
import { useMemo, useState, type ReactNode } from 'react';
import { useApp } from '@/components/app';
import { Icon } from '@/components/ui';
import { MySubmissions, type Submission } from '@/components/views/ambassador';
import { ErrorLine, errCode, errText, hasAccount, Loading, SignInCard, useLoad } from '@/components/views/common';
import { api, ApiError, useAccount } from '@/lib/account';
import type { Msg } from '@/lib/admincontent';
import { BUILDINGS } from '@/lib/data/campus';
import { CITIES, OPP_TYPES } from '@/lib/data/static';
import { checkSug, emptySug, fieldOfCode, PLACE_CAT_CODES, SUG_MESSAGES, sugOfPayload, sugPayload, type SugForm, type SugKind } from '@/lib/suggest';

const almatyToday = () => new Date(Date.now() + 5 * 3600_000).toISOString().slice(0, 10);

const PLACE_CATS: Record<string, [string, string]> = {
  canteen: ['Асхана', 'Столовая'], cafe: ['Кафе', 'Кафе'], print: ['Басып шығару', 'Печать'], copy: ['Көшірме', 'Ксерокс'],
  library: ['Кітапхана', 'Библиотека'], shop: ['Дүкен', 'Магазин'], pharmacy: ['Дәріхана', 'Аптека'], sport: ['Спорт', 'Спорт'],
  cowork: ['Коворкинг', 'Коворкинг'], other: ['Басқа', 'Другое'],
};

/* ---------- the shared fields ---------- */
interface FieldsProps {
  kind: SugKind; f: SugForm; set: (p: Partial<SugForm>) => void;
  /** the message a field shows now (own rule after leaving it / trying to send, or the server's code) */
  msg: (k: string) => Msg | null; touch: (k: string) => void; idp: string;
}

function SugFields({ kind, f, set, msg, touch, idp }: FieldsProps) {
  const { tt, tp, L } = useApp();
  const today = almatyToday();
  const field = (k: keyof SugForm, label: string, ctl: (a: { id: string; 'aria-invalid'?: true; 'aria-describedby'?: string }) => ReactNode, hint?: string) => {
    const m = msg(k);
    const id = idp + k;
    return (
      <div className="field" key={k} onBlur={() => touch(k)}>
        <label htmlFor={id}>{label}</label>
        {ctl(m ? { id, 'aria-invalid': true, 'aria-describedby': id + '_err' } : { id })}
        {hint ? <span className="tiny muted">{hint}</span> : null}
        {m ? <span className="field-err" id={id + '_err'} role="alert">{tt(...m)}</span> : null}
      </div>
    );
  };
  const text = (k: keyof SugForm, label: string, max: number, o: { area?: boolean; type?: string; hint?: string; placeholder?: string } = {}) => field(k, label, a => (
    o.area ? <textarea className="input" maxLength={max} value={f[k] as string} {...a} onChange={e => set({ [k]: e.target.value })} />
      : <input className="input" type={o.type || 'text'} maxLength={max} value={f[k] as string} placeholder={o.placeholder} {...a} onChange={e => set({ [k]: e.target.value })} />
  ), o.hint);
  if (kind === 'place') {
    return (
      <>
        {field('cat', tt('Түрі *', 'Тип *'), a => (
          <select className="input" value={f.cat} {...a} onChange={e => { set({ cat: e.target.value }); touch('cat'); }}>
            <option value="">{tt('— таңдаңыз —', '— выберите —')}</option>
            {PLACE_CAT_CODES.map(k => <option key={k} value={k}>{tp(PLACE_CATS[k])}</option>)}
          </select>
        ))}
        {text('name_kk', tt('Атауы (қазақша) *', 'Название (на казахском) *'), 120)}
        {text('name_ru', tt('Атауы (орысша)', 'Название (на русском)'), 120)}
        {text('address', tt('Мекенжайы *', 'Адрес *'), 200)}
        {field('building_id', tt('Корпус', 'Корпус'), a => (
          <select className="input" value={f.building_id} {...a} onChange={e => set({ building_id: e.target.value })}>
            <option value="">—</option>
            {BUILDINGS.map(b => <option key={b.id} value={b.id}>{b.n}. {L(b.name)}</option>)}
          </select>
        ))}
        {text('hours', tt('Жұмыс уақыты', 'Часы работы'), 100)}
        {text('desc_kk', tt('Сипаттама (қазақша)', 'Описание (на казахском)'), 1000, { area: true })}
        {text('desc_ru', tt('Сипаттама (орысша)', 'Описание (на русском)'), 1000, { area: true })}
        {text('source_url', tt('Сілтеме (https://)', 'Ссылка (https://)'), 500, { type: 'url', placeholder: 'https://' })}
      </>
    );
  }
  return (
    <>
      {field('type', tt('Түрі *', 'Тип *'), a => (
        <select className="input" value={f.type} {...a} onChange={e => { set({ type: e.target.value }); touch('type'); }}>
          <option value="">{tt('— таңдаңыз —', '— выберите —')}</option>
          {Object.entries(OPP_TYPES).map(([k, l]) => <option key={k} value={k}>{tp(l)}</option>)}
        </select>
      ))}
      {text('title_kk', tt('Атауы (қазақша) *', 'Название (на казахском) *'), 140, { hint: tt('3–140 таңба', '3–140 символов') })}
      {text('title_ru', tt('Атауы (орысша)', 'Название (на русском)'), 140)}
      {text('org_kk', tt('Ұйым (қазақша) *', 'Организация (на казахском) *'), 140)}
      {text('org_ru', tt('Ұйым (орысша)', 'Организация (на русском)'), 140)}
      {text('source_url', tt('Ресми сілтеме * (https://)', 'Официальная ссылка * (https://)'), 500, {
        type: 'url', placeholder: 'https://', hint: tt('Ұйымның өз сайты, университет не gov.kz беті', 'Сайт самой организации, вуза или gov.kz'),
      })}
      {field('deadline', tt('Соңғы күн', 'Дедлайн'), a => (
        <>
          <input className="input" type="date" min={today} value={f.deadline} disabled={f.noDeadline} {...a} onChange={e => set({ deadline: e.target.value })} />
          <label className="check"><input type="checkbox" checked={f.noDeadline} onChange={e => { set({ noDeadline: e.target.checked, deadline: e.target.checked ? '' : f.deadline }); touch('deadline'); }} />
            <span className="small">{tt('Мерзімсіз / білмеймін', 'Без срока / не знаю')}</span></label>
        </>
      ))}
      {field('city', tt('Қала', 'Город'), a => (
        <select className="input" value={f.city} {...a} onChange={e => set({ city: e.target.value })}>
          <option value="">{tt('Көрсетілмеген', 'Не указан')}</option>
          {Object.entries(CITIES).map(([k, c]) => <option key={k} value={k}>{L(c)}</option>)}
        </select>
      ))}
      {text('desc_kk', tt('Қысқаша сипаттама (қазақша) *', 'Краткое описание (на казахском) *'), 2000, { area: true, hint: tt('Кімге арналған, не береді — 10–2000 таңба', 'Для кого и что даёт — 10–2000 символов') })}
      {text('desc_ru', tt('Сипаттама (орысша)', 'Описание (на русском)'), 2000, { area: true })}
      <label className="check"><input type="checkbox" checked={f.funded} onChange={e => set({ funded: e.target.checked })} /><span className="small">{tt('Қаржыландырылады', 'Финансируется')}</span></label>
      <label className="check"><input type="checkbox" checked={f.free} onChange={e => set({ free: e.target.checked })} /><span className="small">{tt('Ақы сұрамайды (қатысу тегін)', 'Не требует оплаты (участие бесплатное)')}</span></label>
    </>
  );
}

/** form state + own rules + the server's `invalid_<field>`; `submit` runs the request and maps that code to its field */
function useSugForm(kind: SugKind, initial: SugForm) {
  const [f, setF] = useState<SugForm>(initial);
  const [touched, setTouched] = useState<Record<string, boolean>>({});
  const [tried, setTried] = useState(false);
  const [server, setServer] = useState<{ field: string; payload: string } | null>(null);
  const errs = useMemo(() => checkSug(kind, f, almatyToday()), [kind, f]);
  const payload = useMemo(() => sugPayload(kind, f), [kind, f]);
  const pkey = JSON.stringify(payload);
  const msg = (k: string): Msg | null => {
    if ((touched[k] || tried) && errs[k]) return errs[k];
    if (server && server.payload === pkey && server.field === k) return SUG_MESSAGES[k] ?? null;
    return null;
  };
  const touch = (k: string) => setTouched(t => (t[k] ? t : { ...t, [k]: true }));
  const set = (p: Partial<SugForm>) => setF(x => ({ ...x, ...p }));
  /** false when the form's own rules stop it; throws the API error otherwise */
  const submit = async (send: (p: Record<string, unknown>) => Promise<void>): Promise<boolean> => {
    setTried(true);
    if (Object.keys(errs).length) {
      setTimeout(() => document.querySelector('.field-err')?.closest('.field')?.scrollIntoView({ block: 'center' }), 50);
      return false;
    }
    try { await send(payload); return true; } catch (e) {
      const k = fieldOfCode(errCode(e));
      if (k) setServer({ field: k, payload: pkey });
      throw e;
    }
  };
  return { f, set, msg, touch, submit, nErr: Object.keys(errs).length, tried };
}

/* ---------- /suggest ---------- */
export function SuggestPage() {
  const { tt, go } = useApp();
  const acc = useAccount();
  const [sent, setSent] = useState(0);
  if (!acc.loaded) return <Loading />;
  if (sent) {
    return (
      <div className="card" role="status">
        <span className="ico ok ac-done"><Icon n="check" /></span>
        <h2>{tt('Рақмет! Ұсынысыңыз тексеріледі', 'Спасибо! Ваше предложение проверят')}</h2>
        <p className="small muted">{tt('Әкімші тексерген соң сайтта шығады. Шешім Telegram-ға келеді, күйін «Менің ұсыныстарым» бөлімінен көресіз.',
          'После проверки администратором оно появится на сайте. Решение придёт в Telegram, статус — в разделе «Мои предложения».')}</p>
        <div className="btn-row">
          <button className="btn" type="button" onClick={() => go('suggestions')}>{tt('Менің ұсыныстарым', 'Мои предложения')}</button>
          <button className="btn ghost" type="button" onClick={() => setSent(0)}>{tt('Тағы ұсыну', 'Предложить ещё')}</button>
        </div>
      </div>
    );
  }
  return (
    <>
      <p className="note">{tt('Грант, тағылымдама, байқау не бағдарлама білесің бе? Жібер — әкімші тексерген соң барлық студентке көрінеді. Автоматты жариялау жоқ.',
        'Знаешь грант, стажировку, конкурс или программу? Отправь — после проверки администратором её увидят все студенты. Автопубликации нет.')}</p>
      {hasAccount(acc)
        ? <SuggestForm onSent={() => setSent(Date.now())} />
        : <SignInCard text={tt('Ұсыну үшін Telegram арқылы кіріп, деректерді өңдеуге келісім беріңіз. Бұл алаяқтықтан қорғайды.', 'Чтобы предложить, войдите через Telegram и дайте согласие на обработку данных. Это защищает от мошенников.')} />}
    </>
  );
}

function SuggestForm({ onSent }: { onSent: () => void }) {
  const { tt, lang } = useApp();
  const form = useSugForm('opportunity', emptySug());
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  return (
    <form className="form" noValidate onSubmit={async e => {
      e.preventDefault();
      if (busy) return;
      setBusy(true); setErr('');
      try {
        const ok = await form.submit(async payload => { await api('/api/submissions', { method: 'POST', body: { kind: 'opportunity', payload }, lang }); });
        if (ok) { window.scrollTo(0, 0); onSent(); }
      } catch (e2) { setErr(errText(e2)); } finally { setBusy(false); }
    }}>
      <SugFields kind="opportunity" f={form.f} set={form.set} msg={form.msg} touch={form.touch} idp="sg_" />
      <p className="tiny muted">{tt('Жеке дерегіңізді (телефон, мекенжай) жазбаңыз — олар керек емес.', 'Не указывайте личные данные (телефон, адрес) — они не нужны.')}</p>
      {form.tried && form.nErr ? <p className="small urgent" role="alert">{tt('Белгіленген өрістерді түзетіңіз', 'Исправьте отмеченные поля')}</p> : null}
      <ErrorLine err={err} />
      <button className="btn block" type="submit" disabled={busy}><Icon n="send" s={18} /> {tt('Жіберу', 'Отправить')}</button>
    </form>
  );
}

/* ---------- /suggestions ---------- */
export function MySuggestionsPage() {
  const { tt, go } = useApp();
  const acc = useAccount();
  const ok = hasAccount(acc);
  const r = useLoad<{ submissions: Submission[] }>(ok ? '/api/submissions/my' : null);
  if (!acc.loaded) return <Loading />;
  if (!ok) return <SignInCard text={tt('Ұсыныстарыңызды көру үшін Telegram арқылы кіріңіз.', 'Чтобы увидеть свои предложения, войдите через Telegram.')} />;
  const list = r.data ? r.data.submissions.filter(s => s.kind === 'opportunity' || s.kind === 'place') : null;
  return (
    <>
      <p className="note">{tt('Қаралуда — әкімші әлі тексермеді. Жарияланды — сайтта көрінеді. Қайтарылды — себебі төменде жазылған.',
        'На проверке — администратор ещё не смотрел. Опубликовано — видно на сайте. Возвращено — причина указана ниже.')}</p>
      <ErrorLine err={r.err} />
      {r.err && !list ? null : <MySubmissions list={list} />}
      <button className="btn ghost block" type="button" onClick={() => go('suggest')}><Icon n="plus" s={18} /> {tt('Мүмкіндік ұсыну', 'Предложить возможность')}</button>
    </>
  );
}

/* ---------- admin: correct a pending suggestion before approving ---------- */
export function QueueEditSheet({ id, kind, payload, onDone }: { id: number; kind: SugKind; payload: Record<string, unknown>; onDone: () => void }) {
  const { tt, lang, toast, closeSheet } = useApp();
  const form = useSugForm(kind, sugOfPayload(payload));
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  return (
    <form className="form" noValidate onSubmit={async e => {
      e.preventDefault();
      if (busy) return;
      setBusy(true); setErr('');
      try {
        const ok = await form.submit(async p => { await api(`/api/admin/queue/${id}`, { method: 'PATCH', body: { payload: p }, lang }); });
        if (ok) { closeSheet(); toast(tt('Түзетілді. Енді бекітуге болады', 'Исправлено. Теперь можно одобрить')); onDone(); }
      } catch (e2) {
        setErr(e2 instanceof ApiError && e2.code === 'not_pending' ? tt('Ұсыныс бұрын шешілген', 'Заявка уже рассмотрена') : errText(e2));
      } finally { setBusy(false); }
    }}>
      <h2>{kind === 'place' ? tt('Пайдалы орынды түзету', 'Исправить полезное место') : tt('Ұсынысты түзету', 'Исправить предложение')}</h2>
      <p className="tiny muted">{tt('Ұсыну кезіндегі ережелермен тексеріледі; өзгерістер журналға жазылады. Құжаттар мен қадамдар керек болса — /admin/content редакторында толық жазба жасаңыз.',
        'Проверяется по правилам отправки; изменения пишутся в журнал. Нужны документы и шаги — создайте полную запись в редакторе /admin/content.')}</p>
      <SugFields kind={kind} f={form.f} set={form.set} msg={form.msg} touch={form.touch} idp="qe_" />
      {form.tried && form.nErr ? <p className="small urgent" role="alert">{tt('Белгіленген өрістерді түзетіңіз', 'Исправьте отмеченные поля')}</p> : null}
      <ErrorLine err={err} />
      <div className="btn-row">
        <button className="btn ghost" type="button" onClick={closeSheet}>{tt('Бас тарту', 'Отмена')}</button>
        <button className="btn" type="submit" disabled={busy}>{tt('Сақтау', 'Сохранить')}</button>
      </div>
    </form>
  );
}
