'use client';
/* Pieces shared by «Табылған заттар», «Сұрақ-жауап» and «Пәтерлес»: the complaint sheet (shared POST /api/reports),
   the revealed-contact card with its safety note, and the inline rule warning. */
import { useState } from 'react';
import { useApp } from '@/components/app';
import { Chip, Icon } from '@/components/ui';
import { ErrorLine, errText, useNeedAccount } from '@/components/views/common';
import { api } from '@/lib/account';
import { RULE_MSG, type Contact } from '@/lib/community';
import { openTelegramLink } from '@/lib/telegram';

const REASONS: [string, string, string][] = [
  ['contact', 'Байланыс деректері / жарнама', 'Контакты / реклама'],
  ['personal', 'Біреудің жеке деректері', 'Чужие личные данные'],
  ['scam', 'Алаяқтық', 'Мошенничество'],
  ['honesty', 'Емтихан жауаптары, көшіру', 'Ответы на экзамен, списывание'],
  ['rude', 'Дөрекілік', 'Оскорбления'],
];

/** «Шағымдану»: the shared report rule hides an item after several reports and puts it in the admin queue */
export function ReportSheet({ kind, id }: { kind: string; id: number }) {
  const { tt, lang, toast, closeSheet } = useApp();
  const [reason, setReason] = useState('');
  const [text, setText] = useState('');
  const [err, setErr] = useState('');
  const [busy, setBusy] = useState(false);
  const picked = REASONS.find(r => r[0] === reason);
  return (
    <form className="form" onSubmit={async e => {
      e.preventDefault();
      if (!picked || busy) return;
      setBusy(true); setErr('');
      const full = [lang === 'ru' ? picked[2] : picked[1], text.trim()].filter(Boolean).join(': ').slice(0, 300);
      try {
        await api('/api/reports', { method: 'POST', body: { kind, id: String(id), reason: full }, lang });
        closeSheet();
        toast(tt('Шағым жіберілді. Рақмет!', 'Жалоба отправлена. Спасибо!'));
      } catch (e2) { setErr(errText(e2)); } finally { setBusy(false); }
    }}>
      <h2>{tt('Шағымдану', 'Пожаловаться')}</h2>
      <div className="chips wrap" role="group" aria-label={tt('Себебі', 'Причина')}>
        {REASONS.map(([k, kk, ru]) => <Chip key={k} pressed={reason === k} onClick={() => setReason(k)}>{tt(kk, ru)}</Chip>)}
      </div>
      <div className="field"><label htmlFor="cmRepText">{tt('Толығырақ (міндетті емес)', 'Подробнее (необязательно)')}</label>
        <textarea className="input" id="cmRepText" maxLength={200} value={text} onChange={e => setText(e.target.value)} /></div>
      <ErrorLine err={err} />
      <div className="btn-row">
        <button className="btn ghost" type="button" onClick={closeSheet}>{tt('Бас тарту', 'Отмена')}</button>
        <button className="btn" type="submit" disabled={!picked || busy}>{tt('Жіберу', 'Отправить')}</button>
      </div>
    </form>
  );
}

/** a «Шағымдану» link that asks for an account first */
export function ReportLink({ kind, id }: { kind: string; id: number }) {
  const { tt, openSheet } = useApp();
  const need = useNeedAccount();
  return (
    <button className="link-btn cm-report" type="button" onClick={() => { if (need()) openSheet(<ReportSheet kind={kind} id={id} />); }}>
      {tt('Шағымдану', 'Пожаловаться')}
    </button>
  );
}

/** the other side's contact, shown only after both agreed; with the meeting safety note */
export function ContactCard({ c, title }: { c: Contact; title: string }) {
  const { tt } = useApp();
  return (
    <div className="card cm-contact" role="group" aria-label={title}>
      <span className="row-title">{title}</span>
      <span>{c.name || tt('Студент', 'Студент')}</span>
      {c.username ? <span className="small">@{c.username}</span> : null}
      {c.email ? <a className="small" href={`mailto:${c.email}`}>{c.email}</a> : null}
      {c.telegramUrl ? (
        <button className="btn block" type="button" onClick={() => openTelegramLink(c.telegramUrl!)}><Icon n="send" s={18} /> {tt('Telegram-да жазу', 'Написать в Telegram')}</button>
      ) : null}
      <p className="tiny muted">{tt('Кездесуді университетте не адам көп жерде өткізіңіз. Алдын ала ақша аудармаңыз, құжатыңыздың көшірмесін жібермеңіз.',
        'Встречайтесь в университете или в людном месте. Не переводите деньги заранее и не отправляйте копии документов.')}</p>
    </div>
  );
}

/** the rule a draft breaks, in the reader's language (role=alert so a screen reader hears it) */
export function RuleWarning({ code }: { code: string }) {
  const { L } = useApp();
  const m = RULE_MSG[code];
  return m ? <p className="small urgent" role="alert">{L(m)}</p> : null;
}
