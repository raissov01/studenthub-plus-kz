import { NotFound } from '@/components/views/common';

/* Unknown paths (and routes of features not built yet, e.g. /employer) get the app's neutral «Мұндай бет жоқ»
   screen inside the normal frame instead of the bare framework 404. */
export default function NotFoundPage() {
  return <NotFound />;
}
