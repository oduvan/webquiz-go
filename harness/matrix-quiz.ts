// The quiz the matrix runs in every case, the answers the two students give,
// and the small files the quiz needs (PNG images and an attachment).
//
// Every case uses the same ten questions. Only the quiz-wide settings change,
// and they live either in the quiz file or in the start settings of
// POST /api/admin/switch-quiz (see matrix-cases.ts).

import { deflateSync } from 'node:zlib';

/** A student's answer: option indexes (0-based, as the quiz file counts them) or typed text. */
export type Answer = { indices: number[] } | { text: string };

export type QuestionKind =
  | 'single'
  | 'multiple'
  | 'multiple-min'
  | 'text-checker'
  | 'text-exact'
  | 'image'
  | 'image-options'
  | 'file'
  | 'sticky'
  | 'own-limit';

/** What the rendered explanation must contain (bold words, list items, links). */
export interface ExplanationExpect {
  strong: string[];
  listItems: number;
  links: { text: string; href: string }[];
}

export interface MatrixQuestion {
  id: number; // the 1-based position in the quiz file; the server uses it as the question id
  kind: QuestionKind;
  label: string; // a short English name for captions and the report
  question: string;
  options?: string[];
  correct_answer?: number | number[];
  min_correct?: number;
  checker?: string;
  correct_value?: string;
  default_value?: string;
  points?: number;
  image?: string;
  file?: string;
  stick_to_the_previous?: boolean;
  time_limit?: number;
  explanation?: string;
  explanationExpect?: ExplanationExpect;
  /** The checker code above, written again in TypeScript (only for the answers the students give). */
  runChecker?: (answer: string) => string | null; // null = the checker passes, else the error message
  main: Answer; // what the main student answers (when it answers at all)
  right: Answer; // the right answer (the second student gives it)
}

export const QUIZ_FILE = 'matrix.yaml';
export const QUIZ_TITLE = 'Матриця налаштувань';
export const PER_QUESTION_SECONDS = 15;
export const OWN_LIMIT_SECONDS = 8; // question 10's own time_limit
export const WHOLE_QUIZ_SECONDS = 60;
/** In whole-quiz mode the main student stops answering at this position and lets the time run out. */
export const WHOLE_QUIZ_STOP_AT = 8;
/** ...or earlier, when less than this many seconds of quiz time are left. */
export const WHOLE_QUIZ_SAFE_SECONDS = 18;

export const ATTACHMENT_NAME = 'dovidka.txt';
export const ATTACHMENT_TEXT = 'Довідка для питання 8\nШукане число: 42\n';

export const IMAGE_NAMES = ['figure.png', 'red.png', 'blue.png', 'green.png', 'yellow.png'];

export const QUESTIONS: MatrixQuestion[] = [
  {
    id: 1,
    kind: 'single',
    label: 'one answer, has an explanation',
    question: 'Яке місто є столицею України?',
    options: ['Львів', 'Київ', 'Одеса', 'Харків'],
    correct_answer: 1,
    points: 1,
    explanation:
      '**Київ** — столиця України.\n\n- найбільше місто країни;\n- стоїть на Дніпрі.\n\n[Докладніше](https://example.com/kyiv)',
    explanationExpect: {
      strong: ['Київ'],
      listItems: 2,
      links: [{ text: 'Докладніше', href: 'https://example.com/kyiv' }],
    },
    main: { indices: [1] },
    right: { indices: [1] },
  },
  {
    id: 2,
    kind: 'multiple',
    label: 'several answers, all needed, 2 points',
    question: 'Які з цих чисел парні?',
    options: ['2', '3', '4', '5'],
    correct_answer: [0, 2],
    points: 2,
    explanation: 'Парне число ділиться на **2** без остачі.\n\n1. 2 = 2 × 1\n2. 4 = 2 × 2',
    explanationExpect: { strong: ['2'], listItems: 2, links: [] },
    main: { indices: [0, 1] }, // partly right: one right and one wrong option
    right: { indices: [0, 2] },
  },
  {
    id: 3,
    kind: 'multiple-min',
    label: 'several answers, min_correct 2',
    question: 'Які з цих тварин — ссавці? Достатньо двох.',
    options: ['Кіт', 'Собака', 'Кит', 'Акула'],
    correct_answer: [0, 1, 2],
    min_correct: 2,
    main: { indices: [0, 1] }, // two of three right ones and no wrong one: counts as right
    right: { indices: [0, 1, 2] },
  },
  {
    id: 4,
    kind: 'text-checker',
    label: 'text answer with a checker, 0.5 points',
    question: 'Скільки годин у добі? Введіть число.',
    checker: "assert to_int(user_answer) == 24, 'Має бути 24'",
    correct_value: '24',
    default_value: '',
    points: 0.5,
    explanation: 'У добі **24** години.\n\n- 12 годин до полудня;\n- 12 годин після.',
    explanationExpect: { strong: ['24'], listItems: 2, links: [] },
    runChecker: (answer) => (/^\s*[-+]?\d+\s*$/.test(answer) && parseInt(answer, 10) === 24 ? null : 'Має бути 24'),
    main: { text: '25' }, // wrong: shows the checker message and the right value
    right: { text: '24' },
  },
  {
    id: 5,
    kind: 'text-exact',
    label: 'text answer without a checker',
    question: 'Напишіть назву столиці Франції.',
    checker: '',
    correct_value: 'Париж',
    main: { text: 'Париж' },
    right: { text: 'Париж' },
  },
  {
    id: 6,
    kind: 'image',
    label: 'question with an image',
    question: 'Яка фігура на малюнку?',
    image: '/imgs/figure.png',
    options: ['Коло', 'Трикутник', 'Квадрат'],
    correct_answer: 1,
    main: { indices: [0] }, // wrong
    right: { indices: [1] },
  },
  {
    id: 7,
    kind: 'image-options',
    label: 'image options',
    question: 'Оберіть синій колір.',
    options: ['/imgs/red.png', '/imgs/blue.png', '/imgs/green.png', '/imgs/yellow.png'],
    correct_answer: 1,
    main: { indices: [1] },
    right: { indices: [1] },
  },
  {
    id: 8,
    kind: 'file',
    label: 'file attachment',
    question: 'Відкрийте файл. Яке число в ньому записане?',
    file: ATTACHMENT_NAME,
    options: ['7', '12', '42'],
    correct_answer: 2,
    main: { indices: [2] },
    right: { indices: [2] },
  },
  {
    id: 9,
    kind: 'sticky',
    label: 'sticks to question 8',
    question: 'Скільки рядків у цьому файлі?',
    options: ['1', '2', '3'],
    correct_answer: 1,
    stick_to_the_previous: true,
    main: { indices: [1] },
    right: { indices: [1] },
  },
  {
    id: 10,
    kind: 'own-limit',
    label: 'own time limit (8 s)',
    question: 'Скільки буде 7 × 8?',
    options: ['54', '56', '64'],
    correct_answer: 1,
    time_limit: OWN_LIMIT_SECONDS,
    main: { indices: [1] }, // in per-question mode the main student lets the time run out instead
    right: { indices: [1] },
  },
];

export function questionById(id: number): MatrixQuestion {
  const question = QUESTIONS.find((q) => q.id === id);
  if (!question) throw new Error(`no question ${id}`);
  return question;
}

/** The question with this text (the page shows the text, the checks need the id). */
export function questionByText(text: string): MatrixQuestion | undefined {
  const wanted = text.replace(/\s+/g, ' ').trim();
  return QUESTIONS.find((q) => q.question === wanted);
}

export type TimeLimitMode = 'none' | 'per_question' | 'whole_quiz';

/** The quiz-wide settings a case runs with. */
export interface QuizSettings {
  show_right_answer: boolean;
  show_explanation: boolean;
  show_final_list: boolean;
  show_answers_on_completion: boolean;
  randomize_questions: boolean;
  time_limit: TimeLimitMode;
}

/** The time_limit value of a quiz file or of the start settings (null = no limit). */
export function timeLimitValue(mode: TimeLimitMode): Record<string, number> | null {
  if (mode === 'per_question') return { per_question: PER_QUESTION_SECONDS };
  if (mode === 'whole_quiz') return { whole_quiz: WHOLE_QUIZ_SECONDS };
  return null;
}

/** The start settings for POST /api/admin/switch-quiz (server.py START_SETTING_KEYS, line 35). */
export function startSettings(settings: QuizSettings): Record<string, unknown> {
  return {
    show_right_answer: settings.show_right_answer,
    randomize_questions: settings.randomize_questions,
    show_answers_on_completion: settings.show_answers_on_completion,
    show_final_list: settings.show_final_list,
    show_explanation: settings.show_explanation,
    time_limit: timeLimitValue(settings.time_limit),
  };
}

/**
 * The opposite of each setting: written to the quiz file when the case uses start settings,
 * so a start setting that was ignored shows up as a failed check.
 */
export function oppositeSettings(settings: QuizSettings): QuizSettings {
  const next: Record<TimeLimitMode, TimeLimitMode> = { none: 'per_question', per_question: 'whole_quiz', whole_quiz: 'none' };
  return {
    show_right_answer: !settings.show_right_answer,
    show_explanation: !settings.show_explanation,
    show_final_list: !settings.show_final_list,
    show_answers_on_completion: !settings.show_answers_on_completion,
    randomize_questions: !settings.randomize_questions,
    time_limit: next[settings.time_limit],
  };
}

/** One question as written to the quiz file. */
function questionYaml(question: MatrixQuestion): Record<string, unknown> {
  const keys = [
    'question',
    'image',
    'file',
    'options',
    'correct_answer',
    'min_correct',
    'checker',
    'default_value',
    'correct_value',
    'points',
    'stick_to_the_previous',
    'time_limit',
    'explanation',
  ] as const;
  const out: Record<string, unknown> = {};
  for (const key of keys) {
    if (question[key] !== undefined) out[key] = question[key];
  }
  return out;
}

/** The quiz file. settings null = no quiz-wide settings at all (every 1.19 default applies). */
export function quizFile(settings: QuizSettings | null): Record<string, unknown> {
  const quiz: Record<string, unknown> = { title: QUIZ_TITLE };
  if (settings) {
    quiz.show_right_answer = settings.show_right_answer;
    quiz.show_explanation = settings.show_explanation;
    quiz.show_final_list = settings.show_final_list;
    quiz.show_answers_on_completion = settings.show_answers_on_completion;
    quiz.randomize_questions = settings.randomize_questions;
    const limit = timeLimitValue(settings.time_limit);
    if (limit) quiz.time_limit = limit;
  }
  quiz.questions = QUESTIONS.map(questionYaml);
  return quiz;
}

// ---- Small PNG files, made in code ----

const CRC_TABLE = (() => {
  const table = new Uint32Array(256);
  for (let n = 0; n < 256; n++) {
    let c = n;
    for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1;
    table[n] = c >>> 0;
  }
  return table;
})();

function crc32(data: Buffer): number {
  let crc = 0xffffffff;
  for (const byte of data) crc = CRC_TABLE[(crc ^ byte) & 0xff] ^ (crc >>> 8);
  return (crc ^ 0xffffffff) >>> 0;
}

function chunk(type: string, data: Buffer): Buffer {
  const length = Buffer.alloc(4);
  length.writeUInt32BE(data.length);
  const body = Buffer.concat([Buffer.from(type, 'ascii'), data]);
  const crc = Buffer.alloc(4);
  crc.writeUInt32BE(crc32(body));
  return Buffer.concat([length, body, crc]);
}

type RGB = [number, number, number];

/** An RGB PNG; pixel(x, y) gives the color of each pixel. */
export function makePng(width: number, height: number, pixel: (x: number, y: number) => RGB): Buffer {
  const raw = Buffer.alloc((width * 3 + 1) * height);
  let offset = 0;
  for (let y = 0; y < height; y++) {
    raw[offset++] = 0; // filter type "none"
    for (let x = 0; x < width; x++) {
      const [r, g, b] = pixel(x, y);
      raw[offset++] = r;
      raw[offset++] = g;
      raw[offset++] = b;
    }
  }
  const header = Buffer.alloc(13);
  header.writeUInt32BE(width, 0);
  header.writeUInt32BE(height, 4);
  header[8] = 8; // bits per channel
  header[9] = 2; // RGB
  const signature = Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]);
  return Buffer.concat([signature, chunk('IHDR', header), chunk('IDAT', deflateSync(raw)), chunk('IEND', Buffer.alloc(0))]);
}

/** A colored card with a darker frame, for the image options. */
function card(color: RGB): Buffer {
  const frame: RGB = [Math.round(color[0] * 0.6), Math.round(color[1] * 0.6), Math.round(color[2] * 0.6)];
  return makePng(200, 120, (x, y) => (x < 6 || y < 6 || x >= 194 || y >= 114 ? frame : color));
}

/** A blue triangle on a light background, for the question image. */
function triangle(): Buffer {
  const [ax, ay, bx, by, cx, cy] = [160, 20, 40, 180, 280, 180];
  const side = (px: number, py: number, qx: number, qy: number, x: number, y: number) => (qx - px) * (y - py) - (qy - py) * (x - px);
  return makePng(320, 200, (x, y) => {
    const d1 = side(ax, ay, bx, by, x, y);
    const d2 = side(bx, by, cx, cy, x, y);
    const d3 = side(cx, cy, ax, ay, x, y);
    const inside = (d1 >= 0 && d2 >= 0 && d3 >= 0) || (d1 <= 0 && d2 <= 0 && d3 <= 0);
    return inside ? [40, 90, 200] : [240, 240, 232];
  });
}

/** The files the quiz needs: path inside the quizzes folder -> content. */
export function quizAssets(): Record<string, Buffer> {
  return {
    'imgs/figure.png': triangle(),
    'imgs/red.png': card([220, 40, 40]),
    'imgs/blue.png': card([40, 90, 220]),
    'imgs/green.png': card([40, 170, 70]),
    'imgs/yellow.png': card([240, 200, 40]),
    [`attach/${ATTACHMENT_NAME}`]: Buffer.from(ATTACHMENT_TEXT, 'utf-8'),
  };
}
