/* one small quiz for the screenshot tool (tools/desktop): its meta, the play view and a finished attempt */
export const quizMeta = {
  id: 1, title: 'Микроэкономика: сұраныс пен ұсыныс', description: 'Нарық тепе-теңдігі, икемділік және баға туралы 2 сұрақ.', subject: 'Микроэкономика',
  visibility: 'public', isDraft: false, timePerQuestion: 30, shuffle: false, source: 'manual', questionCount: 2, plays: 48, version: 1,
  createdAt: '2026-11-01T05:00:00Z', updatedAt: '2026-11-01T05:00:00Z', isMine: false,
};
export const quizPlay = {
  quiz: quizMeta,
  questions: [
    { id: 11, position: 1, type: 'mcq', prompt: 'Баға өскенде сұраныс көлемі қалай өзгереді?', timeLimit: 30, options: ['Азаяды', 'Көбейеді', 'Өзгермейді', 'Алдымен көбейіп, кейін азаяды'] },
    { id: 12, position: 2, type: 'mcq', prompt: 'Нарықтық тепе-теңдік дегеніміз не?', timeLimit: 30, options: ['Сұраныс = ұсыныс', 'Баға = шығын', 'Пайда = 0', 'Салық = 0'] },
  ],
};
export const quizResult = {
  attemptId: 7, quizId: 1, quizTitle: quizMeta.title, mode: 'practice', score: 1, maxScore: 2, percentage: 50, correctCount: 1, total: 2, wrongCount: 1,
  timeSpent: 24, createdAt: '2026-11-07T05:00:00Z',
  answers: [
    { position: 1, questionId: 11, type: 'mcq', prompt: quizPlay.questions[0].prompt, view: { options: quizPlay.questions[0].options }, given: 0, correct: 0, credit: 1, isCorrect: true, graded: true, timeSpent: 10, explanation: 'Сұраныс заңы: баға өссе, сұраныс көлемі азаяды.' },
    { position: 2, questionId: 12, type: 'mcq', prompt: quizPlay.questions[1].prompt, view: { options: quizPlay.questions[1].options }, given: 1, correct: 0, credit: 0, isCorrect: false, graded: true, timeSpent: 14 },
  ],
};
