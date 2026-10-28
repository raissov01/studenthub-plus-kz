'use client';
/* In-site inbox (docs/api/inbox.md, pkg/notify/inbox.go) on the «Хабарламалар» screen for a signed-in student: every
   message the server sends (bot or not) newest first, paged by `before`, an «unread only» filter, tap → mark read and
   open the in-site link, «mark all read». The header/tab badge reads `inboxUnread` from the account state; this file
   keeps it fresh (focus, visibility, a gentle poll) and feeds it the counts the inbox endpoints answer. */
import { useCallback, useEffect, useRef, useState } from 'react';
import { useRouter } from 'next/navigation';
import { useApp } from '@/components/app';
import { Chip, Empty, Icon, SectionHead } from '@/components/ui';
import { ErrorLine, errText, hasAccount, Loading } from '@/components/views/common';
import { api, getAccount, refreshMe, setInboxUnread, useAccount } from '@/lib/account';
import { useConfig } from '@/lib/config';
import { INBOX_POLL_MS, inboxAgo, inboxCategory, inboxLink, inboxPath, type InboxItem, type InboxPage, type InboxReadResp } from '@/lib/inbox';
import { viewOfPath } from '@/lib/routes';

/** re-reads GET /api/me (inboxUnread) when the page comes back into view and every two minutes while it is visible */
export function useInboxPoll() {
  const acc = useAccount();
  const signedIn = hasAccount(acc);
  useEffect(() => {
    if (!signedIn) return;
    let last = Date.now();
    const check = (minGap: number) => {
      if (document.visibilityState !== 'visible' || Date.now() - last < minGap) return;
      last = Date.now();
      void refreshMe();
    };
    const onShow = () => check(15_000);
    const id = window.setInterval(() => check(INBOX_POLL_MS - 1000), INBOX_POLL_MS);
    window.addEventListener('focus', onShow);
    document.addEventListener('visibilitychange', onShow);
    return () => {
      window.clearInterval(id);
      window.removeEventListener('focus', onShow);
      document.removeEventListener('visibilitychange', onShow);
    };
  }, [signedIn]);
}

export function InboxSection() {
  const acc = useAccount();
  if (!hasAccount(acc)) return null;
  return <InboxSection_ key={acc.user?.id} />;
}

function InboxSection_() {
  const { tt, lang, go } = useApp();
  const router = useRouter();
  const acc = useAccount();
  const cfg = useConfig();
  const [onlyUnread, setOnlyUnread] = useState(false);
  const [items, setItems] = useState<InboxItem[] | null>(null);
  const [next, setNext] = useState<number | null>(null);
  const [err, setErr] = useState('');
  const [busy, setBusy] = useState(false);
  const [now, setNow] = useState(() => Date.now());
  // the unread count this list last agreed with; a different one from the poll means new items arrived
  const seen = useRef<number | null>(null);
  const req = useRef(0);

  /* state changes only after the request answers (it also runs from effects); «more» sets busy itself */
  const load = useCallback((before: number | null) => {
    const n = ++req.current;
    api<InboxPage>(inboxPath({ before, unread: onlyUnread }), { lang, cache: 'no-store' }).then(r => {
      if (n !== req.current) return;
      seen.current = r.unread;
      setInboxUnread(r.unread);
      setErr('');
      setItems(prev => (before && prev ? [...prev, ...r.items.filter(x => !prev.some(p => p.id === x.id))] : r.items));
      setNext(r.nextBefore);
      setNow(Date.now());
      setBusy(false);
    }, e => {
      if (n !== req.current) return;
      setErr(errText(e, tt('Хабарламалар жүктелмеді', 'Не удалось загрузить уведомления')));
      setBusy(false);
    });
  }, [onlyUnread, lang, tt]);

  // first page on open and when the filter changes
  useEffect(() => { load(null); }, [load]);
  // the poll saw a different count: fetch the first page again
  useEffect(() => {
    if (seen.current !== null && acc.inboxUnread !== seen.current) load(null);
  }, [acc.inboxUnread, load]);

  const markRead = async (body: { ids: number[] } | { all: true }) => {
    try {
      const r = await api<InboxReadResp>('/api/me/inbox/read', { method: 'POST', body, lang });
      seen.current = r.unread;
      setInboxUnread(r.unread);
      return true;
    } catch (e) {
      setErr(errText(e, tt('Белгіленбеді', 'Не удалось отметить')));
      void refreshMe();
      return false;
    }
  };

  const open = (it: InboxItem) => {
    if (!it.read) {
      const at = new Date().toISOString();
      setItems(prev => prev && (onlyUnread ? prev.filter(x => x.id !== it.id) : prev.map(x => (x.id === it.id ? { ...x, read: true, readAt: at } : x))));
      const left = Math.max(0, getAccount().inboxUnread - 1);
      seen.current = left;
      setInboxUnread(left);
      void markRead({ ids: [it.id] });
    }
    const link = inboxLink(it.link);
    if (!link) return;
    const v = /[?#]/.test(link) ? null : viewOfPath(link);
    if (v) go(v); else router.push(link);
  };

  const markAll = async () => {
    const before = items;
    setItems(prev => prev && (onlyUnread ? [] : prev.map(x => (x.read ? x : { ...x, read: true, readAt: new Date().toISOString() }))));
    if (!(await markRead({ all: true }))) setItems(before);
  };

  const filter = (v: boolean) => {
    if (v === onlyUnread) return;
    setItems(null);
    setNext(null);
    setOnlyUnread(v);
  };

  const unread = acc.inboxUnread;
  const bot = cfg.botUsername;
  return (
    <section className="section inbox" id="inbox">
      <SectionHead title={tt('Аккаунт хабарламалары', 'Сообщения аккаунта')} />
      {acc.user && !acc.user.telegramId ? (
        <p className="note">
          {tt('Аккаунтқа Telegram байланыспаған, сондықтан хабарламалар тек осында келеді. Telegram-да да алу үшін Профильдегі «Аккаунт» бөлімінде байланыстырып, ботқа /start жазыңыз.',
            'К аккаунту не привязан Telegram, поэтому уведомления приходят только сюда. Чтобы получать их и в Telegram, привяжите его в разделе «Аккаунт» профиля и напишите боту /start.')}
          {bot ? <> <a href={`https://t.me/${bot}`} target="_blank" rel="noopener">@{bot}</a></> : null}
        </p>
      ) : null}
      <div className="chips">
        <Chip pressed={!onlyUnread} onClick={() => filter(false)}>{tt('Барлығы', 'Все')}</Chip>
        <Chip pressed={onlyUnread} onClick={() => filter(true)}>{tt('Тек оқылмағандар', 'Только непрочитанные')}</Chip>
      </div>
      {items === null ? (err ? <ErrorLine err={err} /> : <Loading />) : (
        <>
          {items.length
            ? <div className="group">{items.map(it => <InboxRow key={it.id} it={it} now={now} onOpen={open} />)}</div>
            : <Empty icon="bell">{onlyUnread ? tt('Оқылмаған хабарлама жоқ.', 'Непрочитанных уведомлений нет.') : tt('Әзірге хабарлама жоқ.', 'Пока уведомлений нет.')}</Empty>}
          <ErrorLine err={err} />
          {next || unread ? (
            <div className="inbox-actions">
              {next ? (
                <button className="link-btn" type="button" disabled={busy} onClick={() => { setBusy(true); load(next); }}>
                  {busy ? tt('Жүктеліп жатыр…', 'Загрузка…') : tt('Тағы көрсету', 'Показать ещё')}
                </button>
              ) : null}
              {unread ? (
                <button className="link-btn" type="button" onClick={() => void markAll()}>
                  {tt('Барлығын оқылды деп белгілеу', 'Отметить все прочитанными')}
                </button>
              ) : null}
            </div>
          ) : null}
        </>
      )}
    </section>
  );
}

function InboxRow({ it, now, onOpen }: { it: InboxItem; now: number; onOpen: (it: InboxItem) => void }) {
  const { tt, lang } = useApp();
  const c = inboxCategory(it.category, tt);
  const link = inboxLink(it.link);
  return (
    <button className={`nrow ${it.read ? '' : 'unread'}`} type="button" onClick={() => onOpen(it)} data-inbox-id={it.id}>
      <span className={`nico nico-${c.tone}`}><Icon n={c.icon} s={22} /></span>
      <span className="row-main">
        <span className="row-title">{c.label}</span>
        <span className="row-sub inbox-text">{it.text}</span>
        <span className="tiny muted">
          {inboxAgo(it.createdAt, now, tt, lang)}
          {link ? <> · {tt('Ашу', 'Открыть')}</> : null}
        </span>
      </span>
      <span className="ndot" aria-label={it.read ? undefined : tt('Оқылмаған', 'Непрочитано')}></span>
    </button>
  );
}
