'use client';
/* Saved filters of an admin list (users, payments): chips to switch, «Сүзгіні сақтау» to keep the current one under a
   name. Each person sees only their own; the server checks the right to the screen. */
import { useState } from 'react';
import { useApp } from '@/components/app';
import { ErrorLine, errText, useLoad } from '@/components/views/common';
import { api } from '@/lib/account';

interface View { id: number; name: string; query: string }

/** `query` is the current filter without «?»; `onApply` gets a saved one back */
export function SavedViews({ screen, query, onApply }: { screen: 'users' | 'payments'; query: string; onApply: (q: string) => void }) {
  const { tt, lang, toast, openSheet } = useApp();
  const r = useLoad<{ views: View[] }>(`/api/admin/views?screen=${screen}`);
  const list = r.data?.views ?? [];
  const cur = query.replace(/^\?/, '');
  if (!r.data && !cur) return null;
  return (
    <div className="saved-views" aria-label={tt('Сақталған сүзгілер', 'Сохранённые фильтры')} role="group">
      {list.map(v => (
        <span key={v.id} className={`chip saved-chip${v.query === cur ? ' is-on' : ''}`}>
          <button type="button" aria-pressed={v.query === cur} onClick={() => onApply(v.query)}>{v.name}</button>
          <button type="button" className="saved-x" aria-label={tt(`«${v.name}» сүзгісін өшіру`, `Удалить фильтр «${v.name}»`)} onClick={async () => {
            try { await api(`/api/admin/views/${v.id}`, { method: 'DELETE', lang }); r.reload(); } catch (e) { toast(errText(e)); }
          }}>×</button>
        </span>
      ))}
      {cur && !list.some(v => v.query === cur) ? (
        <button className="link-btn" type="button" onClick={() => openSheet(<SaveViewSheet screen={screen} query={cur} onDone={r.reload} />)}>
          {tt('Осы сүзгіні сақтау', 'Сохранить этот фильтр')}</button>
      ) : null}
    </div>
  );
}

function SaveViewSheet({ screen, query, onDone }: { screen: string; query: string; onDone: () => void }) {
  const { tt, lang, toast, closeSheet } = useApp();
  const [name, setName] = useState('');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  return (
    <form className="form" onSubmit={async e => {
      e.preventDefault();
      if (!name.trim() || busy) return;
      setBusy(true); setErr('');
      try { await api('/api/admin/views', { method: 'POST', body: { screen, name: name.trim(), query }, lang }); closeSheet(); toast(tt('Сүзгі сақталды', 'Фильтр сохранён')); onDone(); }
      catch (x) { setErr(errText(x)); } finally { setBusy(false); }
    }}>
      <h2>{tt('Сүзгіні сақтау', 'Сохранить фильтр')}</h2>
      <div className="field"><label htmlFor="svName">{tt('Атауы', 'Название')}</label>
        <input className="input" id="svName" maxLength={60} autoFocus value={name} onChange={e => setName(e.target.value)}
          placeholder={tt('Мысалы: «Жаңа модераторлар»', 'Например: «Новые модераторы»')} /></div>
      <ErrorLine err={err} />
      <div className="btn-row">
        <button className="btn ghost" type="button" onClick={closeSheet}>{tt('Бас тарту', 'Отмена')}</button>
        <button className="btn" type="submit" disabled={!name.trim() || busy}>{tt('Сақтау', 'Сохранить')}</button>
      </div>
    </form>
  );
}

/** a query string -> filter object over the given defaults (unknown keys dropped; «1» -> true for booleans) */
export function parseQuery<T extends Record<string, string | boolean>>(q: string, base: T): T {
  const p = new URLSearchParams(q);
  const out = { ...base };
  for (const k of Object.keys(base) as (keyof T)[]) {
    const v = p.get(String(k));
    if (v === null) continue;
    (out[k] as unknown) = typeof base[k] === 'boolean' ? v === '1' : v;
  }
  return out;
}
