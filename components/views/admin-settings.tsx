'use client';
import { useEffect, useState } from 'react';
import { useApp } from '@/components/app';
import { Row, SectionHead } from '@/components/ui';
import { Loading, NotFound } from '@/components/views/common';
import { api, ApiError } from '@/lib/account';
import type { Content } from '@/lib/types';

const SECTIONS = [['jobs', 'Жұмыс', 'Работа'], ['opportunities', 'Мүмкіндіктер', 'Возможности'], ['events', 'Іс-шаралар', 'Мероприятия'], ['discounts', 'Жеңілдіктер', 'Скидки']] as const;
export function DemoSettings() {
  const { tt, lang, refreshContent } = useApp();
  const [show, setShow] = useState<boolean | null>(null);
  const [feed, setFeed] = useState<Content | null>(null);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  const [saved, setSaved] = useState(false);
  const [denied, setDenied] = useState(false);
  const [revision, reload] = useState(0);
  useEffect(() => {
    let live = true;
    Promise.all([api<{ showDemo: boolean }>('/api/admin/settings', { lang, cache: 'no-store', signal: AbortSignal.timeout(12_000) }), refreshContent()])
      .then(([settings, data]) => { if (live) { setShow(settings.showDemo); setFeed(data); setErr(''); } })
      .catch(e => { if (live) { setErr(lang === 'ru' ? 'Не удалось загрузить настройки. Повторите попытку.' : 'Баптаулар жүктелмеді. Қайта көріңіз.'); setDenied(e instanceof ApiError && [401, 403].includes(e.status)); } });
    return () => { live = false; };
  }, [lang, refreshContent, revision]);
  const save = async (value: boolean) => {
    const previous = show;
    setShow(value); setBusy(true); setErr(''); setSaved(false);
    try {
      const settings = await api<{ showDemo: boolean }>('/api/admin/settings', { method: 'PUT', body: { showDemo: value }, lang, signal: AbortSignal.timeout(12_000) });
      setShow(settings.showDemo);
      setFeed(null);
      try { setFeed(await refreshContent()); setSaved(true); }
      catch { setErr(tt('Баптау сақталды, бірақ тізім жаңармады. Қайта жүктеңіз.', 'Настройка сохранена, но список не обновился. Повторите загрузку.')); }
    } catch (e) {
      setShow(previous);
      setErr(tt('Баптау сақталмады. Қайта көріңіз.', 'Настройка не сохранена. Повторите попытку.'));
      setDenied(e instanceof ApiError && [401, 403].includes(e.status));
    } finally { setBusy(false); }
  };
  if (denied) return <NotFound />;
  return <section className="section launch-admin">
    <SectionHead title={tt('Демо деректер', 'Демонстрационные данные')} />
    {show === null && !err ? <Loading /> : null}
    <label className="check"><input type="checkbox" checked={show === true} disabled={show === null || busy} onChange={e => { void save(e.target.checked); }} aria-describedby="demo-help" /><span>{tt('Демо деректерді көрсету', 'Показывать демоданные')}</span></label>
    <p id="demo-help" className="tiny muted">{tt('Әр бөлімде 5-тен көп нақты жазба болса, оның үлгілері автоматты жасырылады. Жазбалар жойылмайды.', 'Если в разделе больше 5 реальных записей, его примеры скрываются автоматически. Записи не удаляются.')}</p>
    <p className="tiny muted" role="status">{busy ? tt('Сақталып жатыр…', 'Сохранение…') : saved ? tt('Сақталды. Тізім жаңартылды.', 'Сохранено. Список обновлён.') : ''}</p>
    {err ? <><p className="small urgent" role="alert">{err}</p><button className="btn ghost" type="button" disabled={busy} onClick={() => reload(n => n + 1)}>{tt('Қайта жүктеу', 'Повторить загрузку')}</button></> : null}
    {feed ? <div className="group">{SECTIONS.map(([key, kk, ru]) => {
      const count = (feed[key] ?? []).filter(r => !r.is_demo).length;
      const auto = count > 5;
      return <Row key={key} title={tt(kk, ru)} sub={<>{tt(`Нақты жазба: ${count}. `, `Реальных записей: ${count}. `)}{auto ? tt('Автоматты жасыру: қосулы (>5).', 'Автоскрытие: включено (>5).') : tt('Автоматты жасыру: өшірулі (≤5).', 'Автоскрытие: выключено (≤5).')}{!show ? ' ' + tt('Үлгілер жалпы баптаумен жасырылған.', 'Примеры скрыты общей настройкой.') : ''}</>} chev={false} />;
    })}</div> : null}
  </section>;
}
