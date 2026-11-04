'use client';
/* Home hero: the greeting beside a WebGL scene (hero-scene.tsx); until it loads, or without WebGL, a CSS cube with one
   face per main section. Decorative only (aria-hidden). */
import { useApp } from '@/components/app';
import { Icon } from '@/components/ui';
import type { IconName } from '@/lib/types';
import { HeroScene } from '@/components/views/hero-scene';

export function Hero3D({ who }: { who: string }) {
  const { tt, go } = useApp();
  const faces: [string, IconName, string][] = [
    ['front', 'briefcase', tt('Жұмыс', 'Работа')],
    ['right', 'globe', tt('Тағылымдама', 'Стажировки')],
    ['back', 'tag', tt('Жеңілдік', 'Скидки')],
    ['left', 'book', tt('Оқу', 'Учёба')],
    ['top', 'spark', 'AI'],
    ['bottom', 'calendar', tt('Іс-шара', 'События')],
  ];
  return (
    <section className="hero3d">
      <div className="hero3d-text">
        <h1>{tt(`Сәлем, ${who}!`, `Привет, ${who}!`)}</h1>
        <p>{tt('Жұмыс, тағылымдама, жеңілдік — бәрі бір жерде.', 'Работа, стажировки, скидки — всё в одном месте.')}</p>
        <button className="btn hero3d-btn" type="button" onClick={() => go('search:all')}>{tt('Мүмкіндіктерді қарау', 'Смотреть возможности')}</button>
      </div>
      <HeroScene fallback={<>
        <div className="hero3d-cube">
          {faces.map(([f, i, l], n) => (
            <span key={f} className={`hero3d-face hero3d-${f} hf-${n % 5}`}><Icon n={i} s={26} /><b>{l}</b></span>
          ))}
        </div>
        <i className="hero3d-shadow"></i>
      </>} />
    </section>
  );
}
