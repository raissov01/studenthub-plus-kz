'use client';
/* Home «Жаңалықтар»: a horizontal carousel of solid pastel cards (next card peeks in), moving on by itself every few
   seconds. It stops while the student touches, hovers or focuses it, in a hidden tab, and for prefers-reduced-motion. */
import { useEffect, useRef, useState } from 'react';
import { useApp } from '@/components/app';
import { Icon } from '@/components/ui';
import type { IconName } from '@/lib/types';

export type NewsCard = { go: string; icon: IconName; id: string; title: string; sub: string; kind: string };

const TONES = ['violet', 'mint', 'peach', 'sky', 'rose'];
const EVERY_MS = 4500;

export function NewsCarousel({ items }: { items: NewsCard[] }) {
  const { tt, go } = useApp();
  const track = useRef<HTMLDivElement>(null);
  const [cur, setCur] = useState(0);
  const hold = useRef(false);

  // which card is in view (the track scrolls by itself, by swipe or by the dots)
  useEffect(() => {
    const el = track.current;
    if (!el) return;
    const on = () => {
      const w = (el.firstElementChild as HTMLElement | null)?.offsetWidth || 1;
      setCur(Math.round(el.scrollLeft / (w + 10)));
    };
    el.addEventListener('scroll', on, { passive: true });
    return () => el.removeEventListener('scroll', on);
  }, []);

  const show = (i: number) => {
    const el = track.current;
    const card = el?.children[i] as HTMLElement | undefined;
    if (el && card) el.scrollTo({ left: card.offsetLeft - el.offsetLeft, behavior: 'smooth' });
  };

  useEffect(() => {
    if (items.length < 2 || matchMedia('(prefers-reduced-motion: reduce)').matches) return;
    const id = setInterval(() => {
      if (hold.current || document.hidden) return;
      show((cur + 1) % items.length);
    }, EVERY_MS);
    return () => clearInterval(id);
  }, [cur, items.length]);

  if (!items.length) return null;
  const stop = () => { hold.current = true; };
  const go_on = () => { hold.current = false; };
  return (
    <div className="news-carousel" role="region" aria-roledescription="carousel" aria-label={tt('Жаңалықтар', 'Новости')}
      onPointerDown={stop} onPointerUp={go_on} onPointerLeave={go_on} onMouseEnter={stop} onFocus={stop} onBlur={go_on}>
      <div className="news-track" ref={track}>
        {items.map((n, i) => (
          <button key={n.go} type="button" className={`news-card tone-${TONES[i % TONES.length]}`} onClick={() => go(n.go)}
            aria-roledescription="slide" aria-label={`${i + 1} / ${items.length}: ${n.title}`}>
            <span className="news-card-text">
              <span className="news-kind">{n.kind}</span>
              <span className="news-title">{n.title}</span>
              <span className="news-sub">{n.sub}</span>
            </span>
            <span className="news-ico" aria-hidden="true"><Icon n={n.icon} s={34} /></span>
          </button>
        ))}
      </div>
      {items.length > 1 ? (
        <div className="news-dots">
          {items.map((n, i) => (
            <button key={n.go} type="button" className="news-dot" aria-current={i === cur ? 'true' : undefined}
              aria-label={tt(`${i + 1}-жаңалық`, `Новость ${i + 1}`)} onClick={() => show(i)} />
          ))}
        </div>
      ) : null}
    </div>
  );
}
