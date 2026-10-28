'use client';
/* Renders the screen for a view id ('home', 'opps:jobs', 'job:j1', ...). */
import { Ai, Cards, Chat, DeckView, Exam, Interview, Match, PlanView, Resume, Study } from '@/components/views/ai';
import { EventView, JobView, OppView } from '@/components/views/detail';
import { Home, Materials, Notifications, Saved, Search, Services } from '@/components/views/home';
import { MapView } from '@/components/views/map';
import { Mine, Opps } from '@/components/views/opps';
import { Admission, Bonus, DocView, Docs, Edit, Gpa, Pay, Portfolio, Profile, Settings } from '@/components/views/profile';
import { parseView } from '@/lib/routes';

export function Screen({ v }: { v: string }) {
  const [name, prm] = parseView(v);
  switch (name) {
    case 'search': return <Search />;
    case 'notif': return <Notifications />;
    case 'profile': return <Profile />;
    case 'services': return <Services />;
    case 'materials': return <Materials />;
    case 'saved': return <Saved />;
    case 'opps': return <Opps tab={prm || 'jobs'} />;
    case 'mine': return <Mine />;
    case 'job': return <JobView id={prm} />;
    case 'opp': return <OppView id={prm} />;
    case 'event': return <EventView id={prm} />;
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
    case 'bonus': return <Bonus />;
    case 'edit': return <Edit />;
    case 'gpa': return <Gpa />;
    case 'docs': return <Docs />;
    case 'doc': return <DocView id={prm} />;
    case 'admission': return <Admission />;
    case 'map': return <MapView tab={prm === 'near' ? 'near' : 'campus'} />;
    case 'pay': return <Pay />;
    case 'portfolio': return <Portfolio />;
    case 'settings': return <Settings />;
    default: return <Home />;
  }
}
