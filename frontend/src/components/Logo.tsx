import { LOGO_PATH, LOGO_VIEWBOX } from '@/lib/logo';

/** the brand mark in a solid tile, coloured by the theme (--primary / --on-primary) */
export function Logo() {
  return (
    <i className="logo" aria-hidden="true">
      <svg viewBox={LOGO_VIEWBOX} width="18" height="18"><path fill="currentColor" fillRule="evenodd" d={LOGO_PATH} /></svg>
    </i>
  );
}
