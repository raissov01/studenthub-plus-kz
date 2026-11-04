/* The skeleton shown while a screen waits: the route's loading.tsx and screens whose item is not in the feed yet
   (the first visit, before GET /api/content answers). */
export function ScreenLoading() {
  return (
    <div className="screen-loading" role="status" aria-label="Жүктелуде / Загрузка">
      <span className="sk sk-title" /><span className="sk sk-line" /><span className="sk sk-card" /><span className="sk sk-card" />
    </div>
  );
}
