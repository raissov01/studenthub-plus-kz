// One short in-app question on the 3rd visit day: «Student Hub саған пайдалы ма?» (1–5) +
// optional «Не жетіспейді?». Shown once; closing it counts as answered. Answers go to our
// analytics as a `feedback` event (no personal data is asked for).

export type FeedbackState = { visitDays: string[]; asked: boolean };
export const FEEDBACK_KEY = "studenthub.feedback";
export const ASK_ON_VISIT = 3;
export const MAX_MISSING_LEN = 500;

export const emptyFeedback = (): FeedbackState => ({ visitDays: [], asked: false });

/** Records today's visit (Almaty date) and says whether to show the question now. */
export function onVisit(s: FeedbackState, today: string): { state: FeedbackState; show: boolean } {
  const visitDays = s.visitDays.includes(today) ? s.visitDays : [...s.visitDays, today].slice(-ASK_ON_VISIT);
  const state = { ...s, visitDays };
  return { state, show: !s.asked && visitDays.length >= ASK_ON_VISIT };
}

export const markAsked = (s: FeedbackState): FeedbackState => ({ ...s, asked: true });

export function cleanMissing(text: string): string {
  return text.replace(/\s+/g, " ").trim().slice(0, MAX_MISSING_LEN);
}

export const FEEDBACK_TEXT = {
  question: { kk: "Student Hub саған пайдалы ма?", ru: "Student Hub вам полезен?" },
  missing: { kk: "Не жетіспейді? (міндетті емес)", ru: "Чего не хватает? (необязательно)" },
  hint: { kk: "Аты-жөніңді немесе телефоныңды жазба — жауап анонимді.", ru: "Не пишите имя или телефон — ответ анонимный." },
  send: { kk: "Жіберу", ru: "Отправить" },
  close: { kk: "Жабу", ru: "Закрыть" },
  thanks: { kk: "Рақмет! Жауабың жақсартуға көмектеседі.", ru: "Спасибо! Ваш ответ поможет стать лучше." },
};
