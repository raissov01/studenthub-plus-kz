/* Quiz HTTP contract. Keep wire types, paths and adaptation here. Source: pkg/quiz in the
   backend worktree (read-only). source_quote from the model is exposed as sourceQuote. */
import { api, apiUpload, apiBlob } from './account';
export type QuizLang = 'kk' | 'ru';
export type QuestionType =
  | 'mcq'
  | 'mcq_multi'
  | 'true_false'
  | 'fill_blank'
  | 'reorder'
  | 'matching'
  | 'categorization'
  | 'dropdown'
  | 'poll'
  | 'hotspot'
  | 'labeling'
  | 'comprehension';
export const QUESTION_TYPES: QuestionType[] = [
  'mcq',
  'mcq_multi',
  'true_false',
  'fill_blank',
  'reorder',
  'matching',
  'categorization',
  'dropdown',
  'poll',
  'hotspot',
  'labeling',
  'comprehension',
];
export type Answer = null | string | number | boolean | string[] | number[] | { [key: string]: Answer };
export interface Zone {
  id: string;
  x: number;
  y: number;
  w: number;
  h: number;
  label?: string;
}
export interface SubQuestion {
  id: string;
  type: 'mcq' | 'true_false' | 'fill_blank';
  prompt: string;
  options?: string[];
  correctIndex?: number;
  correctBool?: boolean;
  answers?: string[];
}
export interface Question {
  id?: number;
  type: QuestionType;
  prompt: string;
  options?: string[];
  correctIndex?: number;
  correctIndexes?: number[];
  correctBool?: boolean;
  answers?: string[];
  items?: string[];
  pairs?: { left: string; right: string }[];
  zones?: Zone[];
  correctZone?: string;
  passage?: string;
  subQuestions?: SubQuestion[];
  explanation?: string;
  sourceQuote?: string;
  image?: string;
  audio?: string;
  timeLimit?: number;
}
export interface PlayQuestion {
  id: number;
  position: number;
  type: QuestionType;
  prompt: string;
  imageUrl?: string;
  audioUrl?: string;
  timeLimit: number;
  options?: string[];
  multi?: boolean;
  items?: string[];
  lefts?: string[];
  rights?: string[];
  categories?: string[];
  zones?: Zone[];
  labels?: string[];
  passage?: string;
  subQuestions?: SubQuestion[];
}
export interface QuizMeta {
  id: number;
  title: string;
  description: string;
  subject: string;
  visibility: 'public' | 'unlisted' | 'private';
  isDraft: boolean;
  shareCode?: string;
  timePerQuestion: number;
  shuffle: boolean;
  source: string;
  questionCount: number;
  plays: number;
  version: number;
  createdAt: string;
  updatedAt: string;
  isMine: boolean;
}
export type QuizInput = Pick<
  QuizMeta,
  'title' | 'description' | 'subject' | 'visibility' | 'isDraft' | 'timePerQuestion' | 'shuffle' | 'version'
> & { questions: Question[] };
export interface QuizEdit {
  quiz: QuizMeta;
  questions: Question[];
}
export interface QuizPlay {
  quiz: QuizMeta;
  questions: PlayQuestion[];
  retryOf?: number;
}
export interface AnswerInput {
  questionId: number;
  answer: Answer;
  timeSpent: number;
}
export interface ReviewedAnswer {
  position: number;
  questionId: number | null;
  type: QuestionType;
  prompt: string;
  view: Partial<PlayQuestion>;
  imageUrl?: string;
  explanation?: string;
  sourceQuote?: string;
  given: Answer;
  correct: Answer;
  credit: number;
  isCorrect: boolean;
  graded: boolean;
  timeSpent: number;
}
export interface QuizResult {
  attemptId: number | null;
  quizId: number;
  quizTitle: string;
  mode: string;
  retryOf?: number;
  score: number;
  maxScore: number;
  percentage: number;
  correctCount: number;
  total: number;
  wrongCount: number;
  timeSpent: number;
  createdAt: string;
  answers: ReviewedAnswer[];
}
export interface AttemptSummary {
  id: number;
  quizId: number;
  quizTitle: string;
  percentage: number;
  total: number;
  createdAt: string;
}
export interface GenerationOptions {
  count: number;
  difficulty: 'easy' | 'medium' | 'hard';
  types: QuestionType[];
  lang: QuizLang;
  title: string;
}
export interface GeneratedDraft {
  id: number;
  quiz: QuizMeta;
  generated: number;
  dropped: number;
  truncated: boolean;
  files?: { name: string; kind: string; pages: number; chars: number }[];
}
export interface LiveTicket {
  token: string;
  wsUrl: string;
  playerId: string;
  nickname: string;
  rejoinKey: string;
  expiresAt: number;
  title: string;
}
export interface HostTicket {
  code: string;
  hostToken: string;
  wsUrl: string;
  expiresAt: number;
  title: string;
}
export interface Leader {
  rank: number;
  id: string;
  nickname: string;
  score: number;
  correct: number;
}
export interface LivePlayer {
  id: string;
  nickname: string;
  online: boolean;
}
export type LiveCommand =
  { type: 'start' | 'next' | 'end' } | { type: 'answer'; index: number; answer: Answer };
export type LiveMessage =
  | {
      type: 'hello';
      data: {
        code: string;
        status: string;
        title: string;
        total: number;
        index: number;
        you?: { answered: boolean; score: number; rank: number };
      };
    }
  | { type: 'lobby'; data: { players: LivePlayer[]; count: number; max: number } }
  | {
      type: 'question';
      data: {
        index: number;
        total: number;
        question: PlayQuestion;
        deadline: number;
        serverTime: number;
        timeLimitMs: number;
      };
    }
  | { type: 'progress'; data: { index: number; answered: number; total: number } }
  | { type: 'answer_ack'; data: { index: number } }
  | { type: 'answer_rejected'; data: { index: number; reason: string } }
  | {
      type: 'reveal';
      data: {
        index: number;
        correct: Answer;
        explanation: string;
        leaderboard: Leader[];
        last: boolean;
        you?: { isCorrect: boolean; points: number; score: number; rank: number };
      };
    }
  | { type: 'ended'; data: { reason: string; leaderboard: Leader[] } }
  | { type: 'kicked'; data: null }
  | { type: 'error'; data: { code: string } };
/** Keep uploads below the frontend proxy limit and the Go image limit. Camera originals
 * can be several MB; resize locally before sending them to generation or question media. */
async function preparePhoto(file: File, lang: QuizLang): Promise<File> {
  if (!/^image\/(jpeg|png|webp)$/.test(file.type) || file.size <= 800 * 1024) return file;
  let bitmap: ImageBitmap;
  try {
    bitmap = await createImageBitmap(file);
  } catch {
    throw new Error(
      lang === 'ru'
        ? 'Не удалось прочитать изображение. Выберите другое фото.'
        : 'Суретті оқу мүмкін болмады. Басқа фото таңдаңыз.',
    );
  }
  try {
    const scale = Math.min(1, 1600 / Math.max(bitmap.width, bitmap.height));
    const canvas = document.createElement('canvas');
    canvas.width = Math.max(1, Math.round(bitmap.width * scale));
    canvas.height = Math.max(1, Math.round(bitmap.height * scale));
    const ctx = canvas.getContext('2d');
    if (!ctx) return file;
    ctx.fillStyle = '#ffffff';
    ctx.fillRect(0, 0, canvas.width, canvas.height);
    ctx.drawImage(bitmap, 0, 0, canvas.width, canvas.height);
    for (const quality of [0.85, 0.7, 0.55, 0.4]) {
      const blob = await new Promise<Blob | null>((resolve) => canvas.toBlob(resolve, 'image/jpeg', quality));
      if (blob && blob.size <= 800 * 1024)
        return new File([blob], file.name.replace(/\.[^.]+$/, '') + '.jpg', { type: 'image/jpeg' });
    }
    return file;
  } finally {
    bitmap.close();
  }
}
function uploadLimit(files: File[], lang: QuizLang) {
  if (files.reduce((sum, f) => sum + f.size, 0) > 4 * 1024 * 1024) {
    throw new Error(
      lang === 'ru'
        ? 'Общий размер файлов должен быть не больше 4 МБ. Разделите конспект на части.'
        : 'Файлдардың жалпы көлемі 4 МБ-тан аспауы керек. Конспектіні бөліктерге бөліңіз.',
    );
  }
}
const root = '/api/quiz';
const enc = encodeURIComponent;
export const quizApi = {
  browse: (lang: QuizLang, q = '', page = 0) =>
    api<{ quizzes: QuizMeta[]; hasMore: boolean }>(`${root}/quizzes?q=${enc(q)}&page=${page}`, { lang }),
  mine: (lang: QuizLang) => api<{ quizzes: QuizMeta[] }>(`${root}/mine`, { lang }),
  edit: (id: string | number, lang: QuizLang) =>
    api<QuizEdit>(`${root}/quizzes/${enc(String(id))}`, { lang }),
  play: (id: string | number, lang: QuizLang, share = '') =>
    api<QuizPlay>(share ? `${root}/s/${enc(share)}` : `${root}/quizzes/${enc(String(id))}/play`, { lang }),
  save: (input: QuizInput, lang: QuizLang, id?: number, source = '') =>
    api<{ quiz: QuizMeta }>(id ? `${root}/quizzes/${id}` : `${root}/quizzes?source=${enc(source)}`, {
      method: id ? 'PUT' : 'POST',
      body: input,
      lang,
    }),
  clone: (id: number, lang: QuizLang, share = '') =>
    api<{ id: number; quiz: QuizMeta }>(
      share ? `${root}/s/${enc(share)}/clone` : `${root}/quizzes/${id}/clone`,
      { method: 'POST', lang },
    ),
  history: (lang: QuizLang, quizId = '') =>
    api<{ attempts: AttemptSummary[] }>(`${root}/attempts?quizId=${enc(quizId)}`, { lang }),
  result: (id: string | number, lang: QuizLang) =>
    api<QuizResult>(`${root}/attempts/${enc(String(id))}`, { lang }),
  retry: (id: number, lang: QuizLang) => api<QuizPlay>(`${root}/attempts/${id}/retry`, { lang }),
  submit: (
    id: number,
    answers: AnswerInput[],
    lang: QuizLang,
    questionIds: number[],
    retryOf?: number,
    share = '',
  ) =>
    api<QuizResult>(`${root}/quizzes/${id}/attempts`, {
      method: 'POST',
      body: { answers, questionIds, retryOf: retryOf || 0, share },
      lang,
    }),
  generateText: (text: string, options: GenerationOptions) =>
    api<GeneratedDraft>(`${root}/generate/from-text`, {
      method: 'POST',
      body: { text, ...options },
      lang: options.lang,
    }),
  generateFiles: async (files: File[], options: GenerationOptions) => {
    const prepared = await Promise.all(files.map((f) => preparePhoto(f, options.lang)));
    uploadLimit(prepared, options.lang);
    const form = new FormData();
    prepared.forEach((f) => form.append('files', f));
    Object.entries(options).forEach(([k, v]) => form.append(k, Array.isArray(v) ? v.join(',') : String(v)));
    return apiUpload<GeneratedDraft>(`${root}/generate/from-files`, form, options.lang);
  },
  importExcel: (file: File, lang: QuizLang) => {
    uploadLimit([file], lang);
    const form = new FormData();
    form.append('file', file);
    return apiUpload<{
      questions: Question[];
      warnings: { row: number; code: string }[];
      imported: number;
      skipped: number;
    }>(`${root}/import/xlsx`, form, lang);
  },
  uploadImage: async (file: File, lang: QuizLang) => {
    const prepared = await preparePhoto(file, lang);
    if (prepared.size > 800 * 1024)
      throw new Error(
        lang === 'ru' ? 'Уменьшите изображение до 800 КБ.' : 'Сурет көлемін 800 КБ-қа дейін азайтыңыз.',
      );
    const form = new FormData();
    form.append('file', prepared);
    return apiUpload<{ name: string; url: string }>(`${root}/media?kind=image`, form, lang);
  },
  media: (url: string, lang: QuizLang) => apiBlob(url, lang),
  imageUrl: (name: string) => `${root}/media/${enc(name)}`,
  templateUrl: `${root}/import/template.xlsx`,
  host: (quizId: number, lang: QuizLang) =>
    api<HostTicket>(`${root}/live/sessions`, { method: 'POST', body: { quizId }, lang }),
  hostToken: (code: string, lang: QuizLang) =>
    api<Pick<HostTicket, 'hostToken' | 'expiresAt' | 'wsUrl'>>(`${root}/live/sessions/${enc(code)}/host-token`, {
      method: 'POST',
      lang,
    }),
  join: (code: string, nickname: string, lang: QuizLang, rejoinKey = '') =>
    api<LiveTicket>(`${root}/live/join`, { method: 'POST', body: { code, nickname, rejoinKey }, lang }),
};
// Server metadata deliberately never forwards credentials or reveals private/draft titles.
export async function quizServerInfo(
  id?: string,
  share = '',
): Promise<{ enabled: boolean; quiz?: Pick<QuizMeta, 'title' | 'description'> }> {
  const origin =
    process.env.API_ORIGIN ||
    (process.env.NODE_ENV === 'development'
      ? 'http://localhost:8080'
      : 'https://studenthub.85-137-91-47.sslip.io');
  try {
    const config = await fetch(`${origin}/api/config`, {
      cache: 'no-store',
      signal: AbortSignal.timeout(4000),
    });
    if (!config.ok || !(await config.json()).features?.quiz) return { enabled: false };
    if (!id && !share) return { enabled: true };
    const r = await fetch(
      share ? `${origin}${root}/s/${enc(share)}/meta` : `${origin}${root}/quizzes/${enc(id!)}/play`,
      { cache: 'no-store', signal: AbortSignal.timeout(4000) },
    );
    if (!r.ok) return { enabled: true };
    const data = await r.json();
    return { enabled: true, quiz: share ? data : data.quiz };
  } catch {
    return { enabled: false };
  }
}
export interface LiveIdentity {
  code: string;
  role: 'host' | 'player';
  nickname?: string;
  rejoinKey?: string;
}
export function saveLiveIdentity(identity: LiveIdentity) {
  sessionStorage.setItem(`studenthub.quiz.live.${identity.role}.${identity.code}`, JSON.stringify(identity));
}
export function readLiveIdentity(role: LiveIdentity['role'], code: string): LiveIdentity | null {
  try {
    const v = JSON.parse(sessionStorage.getItem(`studenthub.quiz.live.${role}.${code}`) || 'null');
    return v?.code === code && v.role === role ? v : null;
  } catch {
    return null;
  }
}
