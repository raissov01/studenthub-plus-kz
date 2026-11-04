import { ScreenLoading } from '@/components/ScreenLoading';

/* Shown at once when a link to a server-rendered page (job, opportunity, event) is tapped, and for the moment a
   not-yet-prefetched static screen takes to arrive on a slow network. Without it the old screen stayed put and taps
   looked dead. */
export default function Loading() {
  return <ScreenLoading />;
}
