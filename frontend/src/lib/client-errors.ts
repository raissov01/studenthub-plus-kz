/** Only fixed diagnostics leave the browser. Never stringify an exception/object, stack,
 * user input, URL, profile or rejection reason. The API independently applies the same policy. */
const DIAGNOSTICS = new Set(['Script error.', 'Failed to fetch', 'NetworkError when attempting to fetch resource.', 'Load failed', 'ResizeObserver loop limit exceeded', 'ResizeObserver loop completed with undelivered notifications.', 'Cannot read properties of undefined', 'Cannot read properties of null']);
const CLASSES = new Set(['TypeError', 'ReferenceError', 'SyntaxError', 'RangeError', 'URIError', 'EvalError', 'Error', 'UnhandledRejection']);
export function safeMessage(value: unknown, rejection = false): string {
  // Do not access arbitrary objects/getters or call their toString().
  const text = typeof value === 'string' ? value.trim().replace(/^Uncaught (?:\(in promise\) )?/, '') : '';
  if (DIAGNOSTICS.has(text)) return text;
  const name = text.split(':', 1)[0];
  return `${CLASSES.has(name) ? name : rejection ? 'UnhandledRejection' : 'Client error'}: [details redacted]`;
}
const SCREENS = new Set(['admin', 'events', 'event', 'market', 'jobs', 'job', 'opportunities', 'opportunity', 'discounts', 'mentors', 'ambassador', 'employer', 'university', 'profile', 'portfolio', 'settings', 'health', 'applications', 'announcements', 'p', 'my', 'mine', 'new', 'edit', 'me', 'map', 'schedule', 'onboarding']);
export function safeScreen(path: string): string {
  const parts = path.replace(/^#/, '').split(/[?#]/, 1)[0].split('/').filter(Boolean);
  // A fixed root, optionally a known admin screen; never include resource IDs or slugs,
  // even when a user's slug happens to equal another screen name.
  if (!SCREENS.has(parts[0])) return '/';
  if (parts[0] === 'admin' && parts[1] === 'health') return '/admin/health';
  return '/' + parts[0];
}
export function safeBrowser(ua: string): string {
  for (const [family, pattern] of [
    ['Edge', /(?:Edg|Edge|EdgiOS|EdgA)\/(\d{1,3})(?:\D|$)/],
    ['Opera', /(?:OPR|Opera)\/(\d{1,3})(?:\D|$)/],
    ['Firefox', /(?:Firefox|FxiOS)\/(\d{1,3})(?:\D|$)/],
    ['Chrome', /(?:Chrome|CriOS)\/(\d{1,3})(?:\D|$)/],
    ['Safari', /Version\/(\d{1,3})(?:\D|$)/],
  ] as const) {
    const m = pattern.exec(ua);
    if (m) return `${family}/${m[1]}`;
  }
  return 'Unknown';
}

// Module lifetime = page lifetime; route changes/remounts never reset the budget.
let sent = 0, stopped = false, pending = false;
const seen = new Set<string>();
export function startClientErrorReporter(): () => void {
  if (process.env.NODE_ENV !== 'production' || typeof window === 'undefined') return () => {};
  const report = (value: unknown, rejection = false) => {
    if (stopped || pending || sent >= 5) return;
    const payload = {
      message: safeMessage(value, rejection),
      screen: safeScreen(window.location.hash.startsWith('#/') ? window.location.hash : window.location.pathname),
      browser: safeBrowser(navigator.userAgent),
    };
    const key = JSON.stringify(payload);
    if (seen.has(key)) return;
    seen.add(key); sent++; pending = true;
    // No cookies or bearer token: ingestion is anonymous. No retries, recursive reporting,
    // persistence, analytics IDs or console logging, including on a failed request/429.
    void fetch('/api/client-errors', {
      method: 'POST', credentials: 'omit', referrerPolicy: 'no-referrer', headers: { 'content-type': 'application/json' },
      body: key, cache: 'no-store', signal: AbortSignal.timeout(5000),
    }).then(r => { if (!r.ok) stopped = true; }, () => { stopped = true; })
      .finally(() => { pending = false; });
  };
  const error = (e: ErrorEvent) => report(e.message);
  const rejection = (e: PromiseRejectionEvent) => report(e.reason, true);
  window.addEventListener('error', error);
  window.addEventListener('unhandledrejection', rejection);
  return () => { window.removeEventListener('error', error); window.removeEventListener('unhandledrejection', rejection); };
}
