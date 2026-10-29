-- 02-014: the optional «Не жетіспейді?» feedback text is now scrubbed in pkg/analytics
-- (contacts, numbers, GPA and name-like word pairs are replaced before storing). Answers stored
-- before that rule may hold a name or a phone, so their text is dropped; the 1–5 score stays.
update analytics_events set props = props - 'missing' where name = 'feedback' and props ? 'missing';
