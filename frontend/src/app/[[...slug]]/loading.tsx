/* Shown at once when a tab or link is tapped, while the server renders the next screen (the root layout is dynamic:
   the feed is request-time data). Without it the old screen stayed put and taps looked dead on a slow network. */
export default function Loading() {
  return (
    <div className="screen-loading" role="status" aria-label="Жүктелуде / Загрузка">
      <span className="sk sk-title" /><span className="sk sk-line" /><span className="sk sk-card" /><span className="sk sk-card" />
    </div>
  );
}
