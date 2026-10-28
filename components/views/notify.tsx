'use client';
/* F8: Settings → «Хабарламалар» (docs/api/notifications.md, pkg/notify/http.go): a switch per category, quiet hours
   and the daily cap of the Telegram bot's messages. «moderation» is always on (shown locked). Every change is a PUT of
   only the changed field; the reply's full prefs replace the local copy. */
import { useState } from 'react';
import { useApp } from '@/components/app';
import { Icon } from '@/components/ui';
import { ErrorLine, errText, hasAccount, Loading, useLoad } from '@/components/views/common';
import { api, useAccount } from '@/lib/account';
import { useConfig } from '@/lib/config';

type Cat = 'deadlines' | 'digest' | 'events' | 'mentors' | 'market' | 'jobs' | 'moderation';
export interface NotifyPrefs {
  categories: Record<Cat, boolean>;
  quietHours: { enabled: boolean; start: string; end: string; timezone: string };
  dailyCap: number;
  locked: Cat[];
}
type Patch = { categories?: Partial<Record<Cat, boolean>>; quietHours?: Partial<NotifyPrefs['quietHours']>; dailyCap?: number };

const CATS: [Cat, [string, string], [string, string]][] = [
  ['deadlines', ['Мерзім еске салулары', 'Напоминания о дедлайнах'], ['Өтінім мерзіміне 7, 3 және 1 күн қалғанда', 'За 7, 3 и 1 день до срока подачи']],
  ['digest', ['Апталық топтама', 'Подборка недели'], ['Аптасына бір рет жаңа мүмкіндіктер', 'Новые возможности раз в неделю']],
  ['events', ['Іс-шаралар', 'Мероприятия'], ['Күту тізімінен орын, еске салу, пікір сұрау', 'Место из листа ожидания, напоминание, отзыв']],
  ['mentors', ['Менторлар', 'Менторы'], ['Жұп туралы хабар және кейінгі сұрақтар', 'О паре с ментором и вопросы после встреч']],
  ['market', ['Маркет', 'Маркет'], ['Хабарландыру мерзімі бітуде, маусымдық ұсыныс', 'Срок объявления истекает, сезонные подсказки']],
  ['jobs', ['Жұмыс ұсыныстары', 'Вакансии'], ['Жұмыс берушілердің ұсыныстары', 'Предложения от работодателей']],
];
const HHMM = /^([01]\d|2[0-3]):[0-5]\d$/;

export function NotifySection() {
  const acc = useAccount();
  if (!hasAccount(acc)) return null;
  return <NotifySection_ />;
}

function NotifySection_() {
  const { tt, lang } = useApp();
  const cfg = useConfig();
  const { data, err } = useLoad<{ prefs: NotifyPrefs }>('/api/me/notifications');
  // the server's copy until the first change; after that the last PUT reply (or the optimistic value meanwhile)
  const [local, setPrefs] = useState<NotifyPrefs | null>(null);
  const prefs = local ?? data?.prefs ?? null;
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');

  const save = async (patch: Patch, optimistic: NotifyPrefs) => {
    const before = prefs;
    setPrefs(optimistic);
    setError('');
    setSaving(true);
    try {
      const r = await api<{ prefs: NotifyPrefs }>('/api/me/notifications', { method: 'PUT', body: patch, lang });
      setPrefs(r.prefs);
    } catch (e) {
      setPrefs(before);
      setError(errText(e, tt('Сақталмады', 'Не сохранилось')));
    } finally { setSaving(false); }
  };

  const bot = cfg.botUsername;
  return (
    <section className="section" id="notify-settings"><h2>{tt('Хабарламалар', 'Уведомления')}</h2>
      <p className="small muted">{tt('Telegram ботынан келетін хабарламалар. Бұл баптаулар аккаунтта сақталады.', 'Сообщения от Telegram-бота. Настройки сохраняются в аккаунте.')}</p>
      {!prefs ? (err ? <ErrorLine err={err} /> : <Loading />) : (
        <>
          <div className="group">
            {CATS.map(([k, name, sub]) => (
              <label key={k} className="row toggle-row">
                <span className="row-main"><span className="row-title">{tt(...name)}</span><span className="row-sub">{tt(...sub)}</span></span>
                <input type="checkbox" checked={!!prefs.categories[k]} disabled={saving}
                  onChange={e => save({ categories: { [k]: e.target.checked } }, { ...prefs, categories: { ...prefs.categories, [k]: e.target.checked } })} />
              </label>
            ))}
            <label className="row toggle-row locked">
              <span className="row-main">
                <span className="row-title">{tt('Модерация шешімдері', 'Решения модерации')} <Icon n="shield" s={14} /></span>
                <span className="row-sub">{tt('Өз өтінімдерің туралы шешімдер әрқашан келеді — өшірілмейді.', 'Решения по вашим заявкам приходят всегда — отключить нельзя.')}</span>
              </span>
              <input type="checkbox" checked disabled />
            </label>
          </div>

          <div className="group">
            <label className="row toggle-row">
              <span className="row-main"><span className="row-title">{tt('Тыныш уақыт', 'Тихие часы')}</span>
                <span className="row-sub">{tt('Бұл уақытта хабарлама келмейді — кейін жіберіледі (Астана уақыты).', 'В это время сообщения не приходят — отправим позже (время Астаны).')}</span></span>
              <input type="checkbox" checked={prefs.quietHours.enabled} disabled={saving}
                onChange={e => save({ quietHours: { enabled: e.target.checked } }, { ...prefs, quietHours: { ...prefs.quietHours, enabled: e.target.checked } })} />
            </label>
            {prefs.quietHours.enabled ? (
              <div className="quiet-times">
                <QuietTime key={'f' + prefs.quietHours.start} id="nfFrom" label={tt('Басталуы', 'С')} value={prefs.quietHours.start} disabled={saving}
                  onSave={v => save({ quietHours: { start: v } }, { ...prefs, quietHours: { ...prefs.quietHours, start: v } })} />
                <QuietTime key={'t' + prefs.quietHours.end} id="nfTo" label={tt('Аяқталуы', 'До')} value={prefs.quietHours.end} disabled={saving}
                  onSave={v => save({ quietHours: { end: v } }, { ...prefs, quietHours: { ...prefs.quietHours, end: v } })} />
              </div>
            ) : null}
          </div>

          <div className="field">
            <label htmlFor="nfCap">{tt('Күніне ең көбі', 'Не больше в день')}</label>
            <select className="input" id="nfCap" value={prefs.dailyCap} disabled={saving}
              onChange={e => { const n = Number(e.target.value); save({ dailyCap: n }, { ...prefs, dailyCap: n }); }}>
              {Array.from({ length: 20 }, (_, i) => i + 1).map(n => <option key={n} value={n}>{tt(`${n} хабарлама`, `${n} ${n % 10 === 1 && n % 100 !== 11 ? 'сообщение' : n % 10 >= 2 && n % 10 <= 4 && (n % 100 < 10 || n % 100 >= 20) ? 'сообщения' : 'сообщений'}`)}</option>)}
            </select>
            <span className="tiny muted">{tt('Шектен асқаны келесі күні келеді.', 'Сверх лимита придёт на следующий день.')}</span>
          </div>
          <ErrorLine err={error} />
          <p className="note">
            {tt('Telegram-да да өзгертуге болады: ботқа /settings жазыңыз.', 'Можно изменить и в Telegram: напишите боту /settings.')}
            {bot ? <> <a href={`https://t.me/${bot}`} target="_blank" rel="noopener">@{bot}</a></> : null}
          </p>
        </>
      )}
    </section>
  );
}

/** one quiet-hours end: saved when a whole HH:MM is picked and it differs from the stored value (keyed by the stored value, so a reply resets it) */
function QuietTime({ id, label, value, disabled, onSave }: { id: string; label: string; value: string; disabled: boolean; onSave: (v: string) => void }) {
  const [v, setV] = useState(value);
  const commit = (x: string) => { if (HHMM.test(x) && x !== value) onSave(x); };
  return (
    <div className="field">
      <label htmlFor={id}>{label}</label>
      <input className="input" id={id} type="time" step={60} value={v} disabled={disabled}
        onChange={e => { setV(e.target.value); commit(e.target.value); }} onBlur={() => { if (!HHMM.test(v)) setV(value); }} />
    </div>
  );
}
