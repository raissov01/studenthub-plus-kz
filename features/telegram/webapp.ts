// Telegram Mini App glue. Outside Telegram every function is a no-op and the site works as usual.
// Loads https://telegram.org/js/telegram-web-app.js lazily only when opened inside Telegram.

type TgTheme = Record<string, string | undefined>;
type TgWebApp = {
  initData: string;
  initDataUnsafe: { user?: { first_name?: string; language_code?: string } };
  colorScheme: "light" | "dark";
  themeParams: TgTheme;
  contentSafeAreaInset?: { top: number };
  safeAreaInset?: { top: number };
  ready(): void;
  expand(): void;
  openTelegramLink(url: string): void;
  onEvent(e: string, cb: () => void): void;
  BackButton: { show(): void; hide(): void; onClick(cb: () => void): void; offClick(cb: () => void): void };
};

declare global { interface Window { Telegram?: { WebApp?: TgWebApp } } }

/** True when the page was opened from Telegram (it adds tgWebAppData to the URL hash). */
export function launchedFromTelegram(): boolean {
  if (typeof window === "undefined") return false;
  return /tgWebApp(Data|Platform)=/.test(window.location.hash) || !!sessionStorage.getItem("studenthub.inTelegram");
}

let loading: Promise<TgWebApp | null> | null = null;

export function telegram(): Promise<TgWebApp | null> {
  if (typeof window === "undefined" || !launchedFromTelegram()) return Promise.resolve(null);
  if (window.Telegram?.WebApp?.initData) return Promise.resolve(window.Telegram.WebApp);
  loading ??= new Promise((resolve) => {
    const s = document.createElement("script");
    s.src = "https://telegram.org/js/telegram-web-app.js";
    s.onload = () => {
      const app = window.Telegram?.WebApp ?? null;
      if (app?.initData) sessionStorage.setItem("studenthub.inTelegram", "1");
      resolve(app?.initData ? app : null);
    };
    s.onerror = () => resolve(null);
    document.head.appendChild(s);
  });
  return loading;
}

/** Maps Telegram theme colours onto the app's CSS tokens and keeps content below Telegram's header. */
export function applyTelegramTheme(app: TgWebApp): void {
  const r = document.documentElement;
  const t = app.themeParams;
  r.setAttribute("data-theme", app.colorScheme === "dark" ? "dark" : "light");
  const map: Record<string, string | undefined> = {
    "--bg": t.secondary_bg_color ?? t.bg_color,
    "--surface": t.bg_color,
    "--text": t.text_color,
    "--muted": t.hint_color,
    "--primary": t.button_color,
    "--on-primary": t.button_text_color,
  };
  for (const [k, v] of Object.entries(map)) if (v) r.style.setProperty(k, v);
  const top = (app.contentSafeAreaInset?.top ?? 0) + (app.safeAreaInset?.top ?? 0);
  r.style.setProperty("--tg-top", `${top}px`);
}

/** Shows Telegram's own back button on inner screens and wires it to the app's back action. */
export function bindBackButton(app: TgWebApp, isRoot: boolean, onBack: () => void): () => void {
  if (isRoot) { app.BackButton.hide(); return () => {}; }
  app.BackButton.show();
  app.BackButton.onClick(onBack);
  return () => app.BackButton.offClick(onBack);
}

export const firstName = (app: TgWebApp | null) => app?.initDataUnsafe.user?.first_name ?? "";

/** Deep link to the reminder bot for one opportunity. Empty when no bot is configured. */
export function remindLink(botUsername: string, opportunityId: string): string {
  if (!botUsername || !/^[A-Za-z0-9_-]{1,40}$/.test(opportunityId)) return "";
  return `https://t.me/${botUsername}?start=remind_${opportunityId}`;
}
