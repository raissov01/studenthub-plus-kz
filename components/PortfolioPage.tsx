/* The public portfolio page body (prompt 09). No hooks and no client APIs: the server page /p/<slug> renders it into
   the HTML, and the student's settings screen reuses it for the live preview. Everything shown is the student's raw
   text rendered as text (React escapes it). «Расталған» appears only where the API says verified: true. */
import type { ReactNode } from 'react';
import { i18n } from '@/lib/format';
import { IC, type IconName } from '@/lib/icons';
import { BADGES, type PfBadge, type PfSelfItem, type PortfolioPage as Page } from '@/lib/portfolio';

type Lang = 'kk' | 'ru';

function Ico({ n, s = 16 }: { n: IconName; s?: number }) {
  // static icon markup from lib/icons (never user data)
  return <svg viewBox="0 0 24 24" width={s} height={s} fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" dangerouslySetInnerHTML={{ __html: IC[n] }} />;
}

/** a calendar day of an RFC 3339 instant, read in Astana time (UTC+5) whatever the server's zone */
function day(iso: string, lang: Lang): string {
  const t = new Date(iso).getTime();
  if (!Number.isFinite(t)) return '';
  const a = new Date(t + 5 * 3600_000);
  return i18n(lang).fDateY(new Date(a.getUTCFullYear(), a.getUTCMonth(), a.getUTCDate()));
}
const hostPath = (url: string) => url.replace(/^https?:\/\//, '').replace(/\/$/, '');

function Verified({ lang }: { lang: Lang }) {
  return <span className="badge b-ok pf-ok"><Ico n="check" s={12} /> {lang === 'ru' ? 'Подтверждено' : 'Расталған'}</span>;
}
function VerifyLink({ url, lang }: { url: string; lang: Lang }) {
  return <a className="pf-verify" href={url} target="_blank" rel="noopener">{lang === 'ru' ? 'Проверить' : 'Тексеру'}<span className="pf-url"> {hostPath(url)}</span></a>;
}
function Item({ icon, title, meta, end, children }: { icon: IconName; title: ReactNode; meta?: ReactNode; end?: ReactNode; children?: ReactNode }) {
  return (
    <li className="pf-item">
      <span className="pf-ico"><Ico n={icon} s={18} /></span>
      <span className="pf-main">
        <span className="pf-title">{title}</span>
        {meta ? <span className="pf-meta">{meta}</span> : null}
        {children}
      </span>
      {end ? <span className="pf-end">{end}</span> : null}
    </li>
  );
}
const badgeName = (b: PfBadge, lang: Lang) => (BADGES[b.badge] ? BADGES[b.badge][lang] : b.badge);
function SelfList({ items, icon }: { items: PfSelfItem[]; icon: IconName }) {
  return (
    <ul className="pf-list">
      {items.map((it, i) => (
        <Item key={i} icon={icon} title={it.title} meta={[it.org, it.period].filter(Boolean).join(' · ') || undefined}>
          {it.desc ? <span className="pf-desc">{it.desc}</span> : null}
          {it.url ? <a className="pf-verify" href={it.url} target="_blank" rel="noopener nofollow">{hostPath(it.url)}</a> : null}
        </Item>
      ))}
    </ul>
  );
}

export function PortfolioPage({ page, lang, preview }: { page: Page; lang: Lang; preview?: boolean }) {
  const { tt, L } = i18n(lang);
  const p = page.profile, v = page.verified, s = page.selfReported;
  const hasVerified = v.events.length + v.certificates.length + v.badges.length > 0;
  const hasSelf = s.certificates.length + s.experience.length + s.skills.length + s.badges.length > 0;
  const where = [p.university, p.course ? tt(`${p.course}-курс`, `${p.course} курс`) : ''].filter(Boolean).join(' · ');
  return (
    <article className={`pf${preview ? ' pf-preview' : ''}`} lang={lang}>
      <header className="pf-head">
        <span className="eyebrow">{tt('Student Hub портфолиосы', 'Портфолио Student Hub')}</span>
        <h1 className="pf-name">{p.name}</h1>
        {p.headline ? <p className="pf-headline">{p.headline}</p> : null}
        {where ? <p className="pf-where">{where}</p> : null}
        {p.bio ? <p className="pf-bio">{p.bio}</p> : null}
        {page.contact ? (
          <>
            <a className="btn pf-tg no-print" href={page.contact.telegramUrl} target="_blank" rel="noopener"><Ico n="send" s={18} /> {tt('Telegram арқылы жазу', 'Написать в Telegram')}</a>
            <p className="pf-print-only">Telegram: {hostPath(page.contact.telegramUrl)}</p>
          </>
        ) : null}
      </header>

      <section className="pf-sec">
        <h2><Ico n="shield" s={20} /> {tt('Расталған жетістіктер', 'Подтверждённые достижения')}</h2>
        {hasVerified ? (
          <ul className="pf-list">
            {v.events.map((e, i) => (
              <Item key={'e' + i} icon="calendar" title={L(e.title)} meta={[day(e.date, lang), e.organizer].filter(Boolean).join(' · ')} end={e.verified ? <Verified lang={lang} /> : undefined}>
                <VerifyLink url={e.verifyUrl} lang={lang} />
              </Item>
            ))}
            {v.certificates.map((c, i) => (
              <Item key={'c' + i} icon="award" title={L(c.title)} end={c.verified ? <Verified lang={lang} /> : undefined}
                meta={[c.kind === 'ambassador' ? tt('Амбассадор сертификаты', 'Сертификат амбассадора') : tt('Іс-шара сертификаты', 'Сертификат мероприятия'), c.issuer, day(c.issuedAt, lang)].filter(Boolean).join(' · ')}>
                <VerifyLink url={c.verifyUrl} lang={lang} />
              </Item>
            ))}
            {v.badges.map((b, i) => (
              <Item key={'b' + i} icon="cap" title={badgeName(b, lang)} end={b.verified ? <Verified lang={lang} /> : undefined}
                meta={b.badge === 'mentor' ? tt('Ментор жұбын аяқтап, жоғары баға алды', 'Завершил(а) менторскую пару с высокой оценкой') : day(b.awardedAt, lang)} />
            ))}
          </ul>
        ) : <p className="pf-empty">{tt('Әзірге жүйе растаған жетістік жоқ.', 'Подтверждённых системой достижений пока нет.')}</p>}
      </section>

      {hasSelf ? (
        <section className="pf-sec">
          <h2>{tt('Өзі көрсеткен', 'Указано самим студентом')}</h2>
          {s.certificates.length ? <><h3>{tt('Сертификаттар', 'Сертификаты')}</h3><SelfList items={s.certificates} icon="award" /></> : null}
          {s.experience.length ? <><h3>{tt('Тәжірибе', 'Опыт')}</h3><SelfList items={s.experience} icon="briefcase" /></> : null}
          {s.skills.length ? <><h3>{tt('Дағдылар', 'Навыки')}</h3><p className="pf-skills">{s.skills.map((k, i) => <span key={i} className="badge b-line">{k.title}</span>)}</p></> : null}
          {s.badges.length ? <><h3>{tt('Қауымдастық белгілері', 'Значки сообщества')}</h3><p className="pf-skills">{s.badges.map((b, i) => <span key={i} className="badge b-line">{badgeName(b, lang)}</span>)}</p></> : null}
        </section>
      ) : null}

      <p className="pf-legend"><Ico n="check" s={14} /> <span>{tt('— Student Hub жүйесі арқылы расталған. Қалғанын студенттің өзі көрсеткен.', '— подтверждено системой Student Hub. Остальное указал сам студент.')}</span></p>
      <footer className="pf-foot">
        <span>{tt('Student Hub — Сейфуллин университеті студенттеріне арналған', 'Student Hub — для студентов Университета им. С. Сейфуллина')}</span>
        <a href={page.site} target="_blank" rel="noopener">{hostPath(page.site)}</a>
      </footer>
    </article>
  );
}
