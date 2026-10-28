/* Runtime switches. For now these are static defaults; later they will be read from the API
   (the shape is fixed here so screens can already depend on it). */
export interface Config {
  /** where «Қате не алаяқтық туралы хабарлау» leads; a t.me link gets the item prefilled as ?text= */
  reportUrl: string;
  /** Telegram bot username without @, empty until the bot is public */
  botUsername: string;
  /** form for suggesting a new opportunity, empty until it exists */
  suggestFormUrl: string;
  features: {
    events: boolean;
    ambassador: boolean;
    mentors: boolean;
    market: boolean;
    portfolio: boolean;
    b2b: boolean;
  };
}

export const CONFIG: Config = {
  reportUrl: 'https://t.me/raissov01',
  botUsername: '',
  suggestFormUrl: '',
  features: { events: false, ambassador: false, mentors: false, market: false, portfolio: false, b2b: false },
};

/** Report link with the item prefilled where the target supports it. */
export function reportHref(itemId: string, kind: string, pageUrl: string, lang: 'kk' | 'ru'): string {
  const text = lang === 'ru'
    ? `Ошибка или мошенничество: ${kind} ${itemId}\n${pageUrl}\n`
    : `Қате не алаяқтық: ${kind} ${itemId}\n${pageUrl}\n`;
  try {
    const u = new URL(CONFIG.reportUrl);
    if (u.hostname === 't.me') u.searchParams.set('text', text);
    else u.searchParams.set('id', itemId);
    return u.toString();
  } catch {
    return CONFIG.reportUrl;
  }
}
