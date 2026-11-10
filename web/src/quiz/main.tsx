import { readBoot, type QuizBoot, type Translations } from '../shared/boot';
import { mountPage } from '../shared/mount';
import { QuizController } from './controller';
import { QuizPage } from './QuizPage';
import './quiz.css';

// In the Vite dev server the page has no boot data: a small sample quiz is
// used, and every text shows its translation key.
function devBoot(): QuizBoot {
  const keys = new Proxy({}, { get: (_target, key) => String(key) }) as Translations;
  return {
    state: 'quiz',
    language: 'en',
    translations: keys,
    version: 'dev',
    title: 'Sample quiz',
    questions: [
      { id: 1, points: 1, time_limit: 0, question: '2 + 2 = ?', options: ['3', '4'], is_multiple_choice: false },
      { id: 2, points: 2, time_limit: 30, question: 'Pick the even numbers', options: ['1', '2', '4'], is_multiple_choice: true, min_correct: 2 },
      { id: 3, points: 0.5, time_limit: 0, question: 'Type 42', type: 'text', default_value: '' },
    ],
    username_label: 'username_label',
    registration_fields: [],
    show_right_answer: true,
    show_final_list: true,
    quiz_time_limit: 0,
    quizzes: [],
  };
}

export function startQuizPage() {
  const boot = readBoot<QuizBoot>(devBoot());
  document.documentElement.lang = boot.language; // the page's language follows the config
  // The controller starts once, before React renders (StrictMode may render twice)
  const controller = boot.state === 'quiz' ? new QuizController(boot) : null;
  if (controller) controller.start();
  mountPage(<QuizPage boot={boot} controller={controller} />, {
    themeTitle: boot.translations.toggle_theme,
  });
}
