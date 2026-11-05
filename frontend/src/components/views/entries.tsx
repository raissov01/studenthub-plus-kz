'use client';
/* Entry points of the account-backed features. Each row exists only while its feature switch is on, so with
   every switch off (production today) these render nothing. */
import { useApp } from '@/components/app';
import { Row } from '@/components/ui';
import { AnnouncementsEntry } from '@/components/views/jobs-b2b';
import { useFeature } from '@/components/views/common';
import { useAccount } from '@/lib/account';
import { canCreateEvents } from '@/lib/events';

/** «Университет қызметтері» board: market and mentors (no feed on the home page — prompt 08) */
export function ServicesCommunity() {
  const { tt } = useApp();
  const market = useFeature('market'), mentors = useFeature('mentors');
  return (
    <>
      {market ? <Row go="market" icon="gift" title={tt('Тегін заттар', 'Отдам даром')} sub={tt('Кітап, конспект, жатақханаға керек заттар — тегін', 'Книги, конспекты, вещи для общежития — бесплатно')} /> : null}
      {mentors ? <Row go="mentors" icon="cap" title={tt('Менторлар', 'Менторы')} sub={tt('Алғашқы айда көмек: жатақхана, құжаттар, оқу', 'Помощь в первый месяц: общежитие, документы, учёба')} /> : null}
      <AnnouncementsEntry />
    </>
  );
}

/** profile: ambassador cabinet / application, rating, my mentoring, organizer page, my market items */
export function ProfileCommunity() {
  const { tt } = useApp();
  const acc = useAccount();
  const market = useFeature('market'), events = useFeature('events');
  const b2b = useFeature('b2b');
  const u = acc.user;
  const rows = [
    // ambassador cabinet, university rating and «my mentoring» are parked for later (team decision 2026-11-05)
    u ? <Row key="sugg" go="suggestions" icon="globe" title={tt('Менің ұсыныстарым', 'Мои предложения')} sub={tt('Ұсынған мүмкіндіктердің күйі', 'Статус предложенных возможностей')} /> : null,
    events && canCreateEvents(u?.role) ? <Row key="org" go="organize" icon="calendar" title={tt('Ұйымдастырушы беті', 'Страница организатора')} /> : null,
    market && u ? <Row key="market" go="market:mine" icon="gift" title={tt('Менің заттарым', 'Мои вещи')} /> : null,
    b2b && u?.role === 'university_staff' ? <Row key="uni" go="university" icon="cap" title={tt('Университет кабинеті', 'Кабинет университета')} /> : null,
    b2b && u ? (u.role === 'employer'
      ? <Row key="emp" go="employer" icon="briefcase" title={tt('Жұмыс беруші кабинеті', 'Кабинет работодателя')} />
      : <Row key="emp" go="employer" icon="briefcase" title={tt('Жұмыс берушілерге', 'Для работодателей')} sub={tt('Компанияны тіркеу, вакансия жариялау', 'Регистрация компании, публикация вакансий')} />) : null,
  ].filter(Boolean);
  if (!rows.length) return null;
  return <div className="group">{rows}</div>;
}
