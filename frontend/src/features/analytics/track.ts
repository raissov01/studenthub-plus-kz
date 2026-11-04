// Anonymous product analytics. A random id per device, an event name from a fixed list and a few
// categorical properties go to our own API (POST /api/events); the server whitelists them again.
// Never pass names, GPA, phone numbers or chat text here.

export type EventName =
  | "visit" | "onboarding_complete" | "opportunity_view" | "job_view" | "share_click"
  | "deadline_saved" | "ai_message_sent" | "roadmap_item_done" | "telegram_open" | "feedback";
type Props = Record<string, string | number | boolean>;

const ID_KEY = "studenthub.anon";
const REF_KEY = "studenthub.ref";
const VISIT_KEY = "studenthub.lastVisit";
const queue: { name: EventName; props?: Props }[] = [];
let timer: ReturnType<typeof setTimeout> | undefined;

function storage(): Storage | null {
  try { return typeof window === "undefined" ? null : window.localStorage; } catch { return null; }
}

function anonId(): string {
  const ls = storage();
  let id = ls?.getItem(ID_KEY) ?? "";
  if (!/^[0-9a-f-]{36}$/.test(id)) {
    id = crypto.randomUUID();
    try { ls?.setItem(ID_KEY, id); } catch { /* private mode: id lives for this page only */ }
  }
  return id;
}

/**
 * Reads ?ref=share / ?ref=amb_<code> on the first visit, remembers it, and removes it from the
 * address bar so a re-shared link does not carry someone else's referral.
 */
export function captureRef(): void {
  if (typeof window === "undefined") return;
  const url = new URL(window.location.href);
  const ref = url.searchParams.get("ref");
  if (!ref) return;
  if (ref === "share" || /^amb_[A-Za-z0-9_-]{1,30}$/.test(ref)) {
    const ls = storage();
    if (ls && !ls.getItem(ID_KEY) && !ls.getItem(REF_KEY)) { try { ls.setItem(REF_KEY, ref); } catch { /* ignore */ } }
  }
  url.searchParams.delete("ref");
  window.history.replaceState(window.history.state, "", url.pathname + url.search + url.hash);
}

/** Adds ?ref=share to a link the student shares. */
export function withShareRef(href: string): string {
  const u = new URL(href);
  u.searchParams.set("ref", "share");
  return u.toString();
}

function flush(): void {
  timer = undefined;
  if (!queue.length) return;
  const events = queue.splice(0, 20);
  const body = JSON.stringify({ anon_id: anonId(), ref: storage()?.getItem(REF_KEY) ?? "", events });
  const sent = typeof navigator !== "undefined" && navigator.sendBeacon?.("/api/events", new Blob([body], { type: "application/json" }));
  if (!sent) fetch("/api/events", { method: "POST", headers: { "content-type": "application/json" }, body, keepalive: true }).catch(() => {});
  if (queue.length) timer = setTimeout(flush, 0);
}

export function track(name: EventName, props?: Props): void {
  if (typeof window === "undefined") return;
  queue.push({ name, props });
  if (!timer) timer = setTimeout(flush, 1500);
}

/** One "visit" per Almaty day — that is what day-1/day-7 return is measured from. */
export function trackVisit(): void {
  const ls = storage();
  const today = new Date(Date.now() + 5 * 3600 * 1000).toISOString().slice(0, 10);
  if (ls?.getItem(VISIT_KEY) === today) return;
  try { ls?.setItem(VISIT_KEY, today); } catch { /* ignore */ }
  track("visit");
}

if (typeof window !== "undefined") {
  window.addEventListener("pagehide", flush);
}
