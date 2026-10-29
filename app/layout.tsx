import type { Metadata, Viewport } from 'next';
import { connection } from 'next/server';
import { AppRoot } from '@/components/AppRoot';
import { getContent } from '@/lib/content';
import { DEFAULT_DESCRIPTION, DEFAULT_TITLE, SITE_NAME, SITE_URL } from '@/lib/seo';
import './globals.css';

// Content IDs can appear after build time. Keep their fallback renders dynamic too,
// rather than entering on-demand static generation and failing at connection().
export const dynamic = 'force-dynamic';

export const metadata: Metadata = {
  metadataBase: new URL(SITE_URL),
  title: { default: SITE_NAME, template: '%s — Student Hub' },
  description: 'Student Hub — студентке арналған бәрі бір қосымшада: жұмыс, тағылымдама, іс-шаралар, жеңілдіктер, құжаттар, AI-көмекші.',
  applicationName: SITE_NAME,
  manifest: '/manifest.webmanifest',
  icons: {
    icon: [
      { url: '/icons/icon.svg', type: 'image/svg+xml' },
      { url: '/icons/icon-32.png', sizes: '32x32', type: 'image/png' },
      { url: '/favicon.ico', sizes: 'any' },
    ],
    apple: [{ url: '/icons/icon-180.png', sizes: '180x180' }],
  },
  openGraph: {
    type: 'website', siteName: SITE_NAME, url: '/', title: DEFAULT_TITLE, description: DEFAULT_DESCRIPTION, locale: 'kk_KZ',
    images: [{ url: '/og.png', width: 1200, height: 630 }],
  },
  twitter: { card: 'summary_large_image', title: DEFAULT_TITLE, description: DEFAULT_DESCRIPTION, images: ['/og.png'] },
};

export const viewport: Viewport = {
  width: 'device-width',
  initialScale: 1,
  viewportFit: 'cover',
  themeColor: '#F5F8FE',
};

/* Runs before first paint: theme and language from the saved state (incl. the old studygo.v1 key), so a dark-theme
   or Russian user does not see a flash of the defaults. */
const bootScript = `try{var s=localStorage.getItem('studenthub.v1')||localStorage.getItem('studygo.v1');var d=document.documentElement;if(s){s=JSON.parse(s);if(s.theme==='dark'||s.theme==='light')d.setAttribute('data-theme',s.theme);if(s.lang==='ru')d.lang='ru';}else{d.setAttribute('data-theme','light');}}catch(e){}`;

export default async function RootLayout({ children }: LayoutProps<'/'>) {
  // The feed is request-time data even when the API was unavailable (and returned no IDs) at build time.
  await connection();
  const content = await getContent();
  return (
    <html lang="kk" data-theme="light" suppressHydrationWarning>
      <head>
        <script dangerouslySetInnerHTML={{ __html: bootScript }} />
        <link rel="preconnect" href="https://fonts.googleapis.com" />
        <link rel="preconnect" href="https://fonts.gstatic.com" crossOrigin="" />
        {/* eslint-disable-next-line @next/next/no-page-custom-font -- kept from the legacy page (Fira Sans Extra Condensed) */}
        <link rel="stylesheet" href="https://fonts.googleapis.com/css2?family=Fira+Sans+Extra+Condensed:wght@500;600;700&display=swap" />
      </head>
      <body>
        <AppRoot content={content}>{children}</AppRoot>
      </body>
    </html>
  );
}
