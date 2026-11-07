/* The role-scoped admin console (/admin, GET /api/console/me and /api/admin/*): types and the pure helpers the
   screens share. Every number comes from the API; nothing here invents data. */

export interface ShopRef { id: number; nameKk: string; nameRu: string; role: string; active: boolean; open: number }
export interface ClubRef { id: number; nameKk: string; nameRu: string | null; status: string; members: number; requests: number; pendingPosts: number }
export interface RecipientRef { id: number; university: string; nameKk: string; nameRu: string; open: number; overdue: number }
export interface Panels {
  admin: boolean; moderator: boolean; blocked: boolean;
  moderation: { pending: number; reports: number } | null;
  cafes: ShopRef[]; printShops: ShopRef[]; clubs: ClubRef[];
  university: { name: string; drafts: number; published: number } | null;
  feedback: RecipientRef[];
  employer: { id: number; name: string; status: string; role: string; jobs: number; pending: number; newApply: number } | null;
  ambassador: boolean; mentor: boolean;
}

/** how many panels the person has (0 = the console is not for them) */
export function panelCount(p: Panels | null | undefined): number {
  if (!p) return 0;
  return (p.admin ? 1 : 0) + (p.moderator && !p.admin ? 1 : 0) + p.cafes.length + p.printShops.length + p.clubs.length
    + (p.university || p.feedback.length ? 1 : 0) + (p.employer ? 1 : 0) + (p.ambassador ? 1 : 0) + (p.mentor ? 1 : 0);
}

export interface DayCount { day: string; n: number }

/** bar heights (0–100, at least 2 for a non-zero day) for a small day chart; max is the tallest bar */
export function bars(series: DayCount[]): { day: string; n: number; h: number }[] {
  const max = Math.max(0, ...series.map(d => d.n));
  return series.map(d => ({ ...d, h: max > 0 ? (d.n > 0 ? Math.max(2, Math.round((d.n / max) * 100)) : 0) : 0 }));
}

/** sum of a series */
export const total = (series: { n: number }[]) => series.reduce((a, d) => a + d.n, 0);

/** the query string of a filter object: empty values dropped, keys in a stable order */
export function query(f: Record<string, string | number | boolean | null | undefined>): string {
  const q = new URLSearchParams();
  for (const k of Object.keys(f).sort()) {
    const v = f[k];
    if (v === '' || v == null || v === false) continue;
    q.set(k, v === true ? '1' : String(v));
  }
  const s = q.toString();
  return s ? '?' + s : '';
}

/** display name of an account: name, @username or #id */
export const who = (p: { id?: number | null; userId?: number | null; name?: string | null; username?: string | null } | null | undefined): string =>
  !p ? '—' : p.name || (p.username ? '@' + p.username : `#${p.id ?? p.userId ?? ''}`);

/** a reason the server accepts (3–500 characters after trimming) */
export const reasonOk = (s: string) => { const n = s.trim().length; return n >= 3 && n <= 500; };

/** the audit actions an admin may see, with Kazakh/Russian labels; unknown ones show their code */
export const AUDIT_ACTIONS: Record<string, [string, string]> = {
  role_grant: ['Рөл берілді', 'Роль выдана'],
  role_revoke: ['Рөл алынды', 'Роль снята'],
  scope_grant: ['Модератор тағайындалды', 'Назначен модератор'],
  scope_revoke: ['Модератор құқығы алынды', 'Права модератора сняты'],
  user_block: ['Аккаунт шектелді', 'Аккаунт ограничен'],
  user_unblock: ['Шектеу алынды', 'Ограничение снято'],
  account_delete: ['Аккаунт өшірілді (өзі)', 'Аккаунт удалён (сам)'],
  account_delete_admin: ['Аккаунт өшірілді (әкімші)', 'Аккаунт удалён (админ)'],
  staff_grant: ['Қызметкер қосылды', 'Сотрудник добавлен'],
  staff_revoke: ['Қызметкер алынды', 'Сотрудник убран'],
  export: ['Excel-ге жүктелді', 'Выгрузка для Excel'],
  invite_create: ['Email-ге рөл алдын ала берілді', 'Роль назначена на email заранее'],
  invite_delete: ['Шақыру болдырылмады', 'Приглашение отменено'],
  invite_applied: ['Шақыру бойынша рөл қосылды', 'Роль добавлена по приглашению'],
  club_admin_grant: ['Клуб әкімшісі тағайындалды', 'Назначен админ клуба'],
  club_admin_revoke: ['Клуб әкімшісі алынды', 'Снят админ клуба'],
  note_add: ['Ішкі жазба қосылды', 'Добавлена заметка'],
  note_delete: ['Ішкі жазба өшірілді', 'Заметка удалена'],
  view_save: ['Сүзгі сақталды', 'Фильтр сохранён'],
  view_delete: ['Сүзгі өшірілді', 'Фильтр удалён'],
  settings_update: ['Байланыс деректері өзгерді', 'Изменены контакты поддержки'],
  event_create: ['Іс-шара құрылды', 'Создано мероприятие'],
  event_update: ['Іс-шара өзгерді', 'Изменено мероприятие'],
  refund_returned: ['Ақша қолмен қайтарылды', 'Деньги возвращены вручную'],
  moderation_approve: ['Модерация: бекітілді', 'Модерация: одобрено'],
  moderation_reject: ['Модерация: қайтарылды', 'Модерация: отклонено'],
  telegram_link: ['Telegram байланысты', 'Привязан Telegram'],
};
