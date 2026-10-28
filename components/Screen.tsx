'use client';
/* Renders the screen for a view id ('home', 'opps:jobs', 'job:j1', ...). */
import { Ai, Cards, Chat, DeckView, Exam, Interview, Match, PlanView, Resume, Study } from '@/components/views/ai';
import { EventView, JobView, OppView } from '@/components/views/detail';
import { Home, Materials, Notifications, Saved, Services } from '@/components/views/home';
import { Search } from '@/components/views/search';
import { MapView } from '@/components/views/map';
import { Mine } from '@/components/views/opps';
import { Admission, DocView, Docs, Edit, Gpa, Pay, Portfolio, Profile, Settings } from '@/components/views/profile';
import { Admin } from '@/components/views/admin';
import { Privacy } from '@/components/views/privacy';
import { Roadmap } from '@/components/views/roadmap';
import { Ambassador, CertificateView } from '@/components/views/ambassador';
import { Checkin, DbEventView, Organize } from '@/components/views/events';
import { Market } from '@/components/views/market';
import { AdminMentors, AmbassadorMentors, Mentors } from '@/components/views/mentors';
import { AdminAmbassadors, AdminMarket, AdminQueue, AdminReports } from '@/components/views/moderation';
import { NotFound } from '@/components/views/common';
import { parseView } from '@/lib/routes';
import { catOfView } from '@/lib/search';

export function Screen({ v }: { v: string }) {
  const [name, prm] = parseView(v);
  switch (name) {
    case 'search': return <Search />;
    case 'notif': return <Notifications />;
    case 'profile': return <Profile />;
    case 'services': return <Services />;
    case 'materials': return <Materials />;
    case 'saved': return <Saved />;
    // the old «Мүмкіндіктер» and discounts URLs are the search screen with their category selected
    case 'opps': case 'bonus': return <Search fixed={catOfView(name, prm)} />;
    case 'mine': return <Mine />;
    case 'job': return <JobView id={prm} />;
    case 'opp': return <OppView id={prm} />;
    case 'event': return /^\d+$/.test(prm) ? <DbEventView id={prm} /> : <EventView id={prm} />;
    case 'ai': return <Ai />;
    case 'chat': return <Chat />;
    case 'resume': return <Resume />;
    case 'interview': return <Interview />;
    case 'exam': return <Exam />;
    case 'plan': return <PlanView id={prm} />;
    case 'cards': return <Cards />;
    case 'deck': return <DeckView id={prm} />;
    case 'study': return <Study />;
    case 'match': return <Match id={prm || undefined} />;
    case 'edit': return <Edit />;
    case 'gpa': return <Gpa />;
    case 'docs': return <Docs />;
    case 'doc': return <DocView id={prm} />;
    case 'admission': return <Admission />;
    case 'map': return <MapView tab={prm === 'near' ? 'near' : 'campus'} />;
    case 'pay': return <Pay />;
    case 'portfolio': return <Portfolio />;
    case 'settings': return <Settings />;
    case 'roadmap': return <Roadmap />;
    case 'privacy': return <Privacy />;
    case 'admin':
      if (!prm) return <Admin />;
      if (prm === 'queue') return <AdminQueue />;
      if (prm === 'reports') return <AdminReports />;
      if (prm === 'mentors') return <AdminMentors />;
      if (prm === 'ambassadors') return <AdminAmbassadors />;
      if (prm === 'market') return <AdminMarket />;
      return <NotFound />;
    case 'ambassador': return prm === 'mentors' ? <AmbassadorMentors /> : <Ambassador prm={prm} />;
    case 'mentors': return <Mentors prm={prm} />;
    case 'market': return <Market prm={prm} />;
    case 'organize': return <Organize prm={prm} />;
    case 'checkin': return <Checkin />;
    case 'certificate': return prm ? <CertificateView code={prm} /> : <NotFound />;
    default: return <Home />;
  }
}
