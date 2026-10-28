'use client';
/* Entry points of the account-backed features. Each row exists only while its feature switch is on, so with
   every switch off (production today) these render nothing. */
import { useApp } from '@/components/app';
import { Row } from '@/components/ui';
import { RatingEntry } from '@/components/views/ambassador';
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
    </>
  );
}

/** profile: ambassador cabinet / application, rating, my mentoring, organizer page, my market items */
export function ProfileCommunity() {
  const { tt } = useApp();
  const acc = useAccount();
  const amb = useFeature('ambassador'), mentors = useFeature('mentors'), market = useFeature('market'), events = useFeature('events');
  const u = acc.user;
  const rows = [
    amb ? (u?.role === 'ambassador' || u?.role === 'admin'
      ? <Row key="amb" go="ambassador" icon="award" title={tt('Амбассадор кабинеті', 'Кабинет амбассадора')} />
      : <Row key="amb" go="ambassador:apply" icon="award" title={tt('Амбассадор болу', 'Стать амбассадором')} sub={tt('Өз университетіңіздің командасы', 'Команда вашего университета')} />) : null,
    amb ? <RatingEntry key="rating" /> : null,
    mentors && u ? <Row key="mentors" go="mentors:me" icon="cap" title={tt('Менің менторлығым', 'Моё менторство')} /> : null,
    events && canCreateEvents(u?.role) ? <Row key="org" go="organize" icon="calendar" title={tt('Ұйымдастырушы беті', 'Страница организатора')} /> : null,
    market && u ? <Row key="market" go="market:mine" icon="gift" title={tt('Менің заттарым', 'Мои вещи')} /> : null,
  ].filter(Boolean);
  if (!rows.length) return null;
  return <div className="group">{rows}</div>;
}
