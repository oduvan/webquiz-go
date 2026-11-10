// The scenarios.
//
// A student scenario has a quiz file, the settings the scripted teacher picks
// in the start pop-up, scripted students with exact plans, and AI agent
// personas. A teacher scenario (TEACHER_SCENARIOS, at the end) has no scripted
// teacher: it sets up the server (config, quiz files, pictures, attachments)
// and gives one AI agent teacher a task list.
//
// Scripted plans map a question number (1 = the first question the student
// sees) to an action. Questions without an entry get the scenario's correct
// answer after 3 seconds. Actions:
//
//     ["answer", label_or_labels, delay]        click the option(s), submit at `delay` s
//     ["text", value, delay]                    type into the answer box, submit at `delay` s
//     ["pick", label, delay]                    click an option at `delay` s, never submit
//     ["idle"]                                  do nothing, let the time run out
//     ["leave", delay]                          close the tab at `delay` s and stop
//     ["reload", reload_at, label, delay]       reload at `reload_at` s, then answer at `delay` s
//     ["offline_answer", label, off_at, off_for, delay]
//                                               Wi-Fi off at `off_at` s for `off_for` s,
//                                               click and submit at `delay` s (while offline)
//     ["offline_at_quiz_time", at, off_for]     Wi-Fi off when the whole quiz has run `at` s,
//                                               then do nothing
//
// A label of null means the scenario's correct answer for that question, and
// {wrong: true} a wrong answer to it (both work with a random question order).
// A text answer is written as {text: "6"}. An option that is a picture is
// written as its path ("/imgs/circle.png"). Times are seconds since the question
// appeared on that student's screen (for offline_at_quiz_time: since the first
// question appeared). A plan may set the student's device (default: desktop).
//
// "expectations" give the outcome each scripted edge case must have in the stored
// data: "correct", "wrong", "timeout" or "missing" (no row), by question id.

import type { PictureSpec } from './pngs';

export type Dict = Record<string, unknown>;

/** What a scripted student answers: null = the correct one, a label, several labels, typed text, or a wrong answer. */
export type AnswerSpec = null | string | string[] | { text: string } | { wrong: true };

export type Action =
  | ['answer', AnswerSpec, number]
  | ['text', string, number]
  | ['pick', AnswerSpec, number]
  | ['idle']
  | ['leave', number]
  | ['reload', number, AnswerSpec, number]
  | ['offline_answer', AnswerSpec, number, number, number]
  | ['offline_at_quiz_time', number, number];

export interface Plan {
  /** When approval is on: the times (seconds after registering) to press "Check". */
  check?: number[];
  /** Question position (1 = first seen) -> action. */
  actions: Record<number, Action>;
  /** The student's device (default: desktop). */
  device?: 'desktop' | 'phone';
}

export type Outcome = 'correct' | 'wrong' | 'timeout' | 'missing';

export interface Agent {
  device: 'desktop' | 'phone';
  persona: string;
}

export type QuizData = { title: string; questions: Dict[]; [key: string]: unknown };

/** Files a scenario puts next to its quizzes. */
export interface ScenarioFiles {
  /** quizzes/imgs/<name>: pictures made in code. */
  images?: Record<string, PictureSpec>;
  /** quizzes/attach/<name>: file contents. */
  attachments?: Record<string, string>;
}

export interface Scenario {
  name: string;
  description: string;
  quiz_file: string;
  quiz: QuizData;
  /** webquiz.yaml values: a section (object) is merged, a top-level value (like language) is set. */
  config?: Record<string, Dict | string | number | boolean>;
  files?: ScenarioFiles;
  start_settings: Dict;
  answers: Record<number, AnswerSpec>;
  /** Question id -> a wrong answer, for {wrong: true} (filled from the quiz when missing). */
  wrong_answers?: Record<number, AnswerSpec>;
  scripted: Record<string, Plan>;
  expectations: Record<string, Record<number, Outcome>>;
  approvals?: Record<string, number | null>;
  agent_approval_delay?: number;
  agents: Record<string, Agent>;
  finish_after: number;
  max_seconds: number;
}

/** The same action for each listed question position. */
function each(positions: number[], make: (q: number) => Action): Record<number, Action> {
  return Object.fromEntries(positions.map((q) => [q, make(q)]));
}

function range(from: number, to: number): number[] {
  return Array.from({ length: to - from + 1 }, (_, i) => from + i);
}

function all(positions: number[], outcome: Outcome): Record<number, Outcome> {
  return Object.fromEntries(positions.map((q) => [q, outcome]));
}

const FRACTIONS: Scenario = {
  name: 'normal-lesson',
  description:
    'A normal lesson. The quiz file says 60 s per question; the teacher starts it with 45 s in the ' +
    'pop-up. Mixed question types, students of different speeds, one reload, one student who leaves.',
  quiz_file: 'fractions.yaml',
  quiz: {
    title: 'Урок: дроби',
    show_right_answer: true,
    time_limit: { per_question: 60 },
    questions: [
      { question: '1/2 + 1/4 = ?', options: ['3/4', '2/6', '1/6', '2/4'], correct_answer: 0 },
      {
        question: 'Які дроби дорівнюють 1/2? (усі правильні)',
        options: ['2/4', '3/6', '2/3', '4/8'],
        correct_answer: [0, 1, 3],
      },
      {
        question: 'Скільки буде 3/4 від 8? Введіть число.',
        checker: "assert to_int(user_answer) == 6, 'Ні, це не 6'",
        correct_value: '6',
        points: 2,
      },
      { question: '0,5 — це ...', options: ['1/5', '1/2', '5/1', '1/50'], correct_answer: 1 },
      { question: 'Що більше: 2/3 чи 3/5?', options: ['2/3', '3/5', 'Вони рівні'], correct_answer: 0 },
    ],
  },
  start_settings: { time_limit: { per_question: 45 } },
  answers: { 1: '3/4', 2: ['2/4', '3/6', '4/8'], 3: { text: '6' }, 4: '1/2', 5: '2/3' },
  scripted: {
    's01-fast': {
      actions: { ...each([1, 2, 4, 5], () => ['answer', null, 2]), 3: ['text', '6', 3] },
    },
    's02-slow': {
      actions: {
        1: ['answer', '3/4', 20],
        2: ['answer', ['2/4', '3/6', '4/8'], 25],
        3: ['text', '6', 30],
      },
    },
    's03-wrong': { actions: { 1: ['answer', '2/6', 4], 4: ['answer', '1/5', 4] } },
    's04-text-wrong': { actions: { 3: ['text', '5', 6] } },
    's05-reload': { actions: { 3: ['reload', 5, { text: '6' }, 12] } },
    's06-leaves': { actions: { 3: ['leave', 4] } },
    's07-partial': { actions: { 2: ['answer', ['2/4'], 6] } },
    's08-near-limit': { actions: { 1: ['answer', '3/4', 42], 4: ['answer', '1/2', 43] } },
  },
  expectations: {
    's01-fast': { 1: 'correct', 2: 'correct', 3: 'correct', 4: 'correct', 5: 'correct' },
    's03-wrong': { 1: 'wrong', 4: 'wrong' },
    's04-text-wrong': { 3: 'wrong' },
    's05-reload': { 3: 'correct' },
    's06-leaves': { 3: 'timeout', 4: 'missing' },
    's07-partial': { 2: 'wrong' },
    's08-near-limit': { 1: 'correct', 4: 'correct' },
  },
  agents: {
    'agent-careful': {
      device: 'desktop',
      persona: 'You are a careful student. Read each question, think, then answer. Try to get them right.',
    },
    'agent-phone': {
      device: 'phone',
      persona: 'You are a student on a phone who answers quickly and does not always read everything.',
    },
  },
  finish_after: 60,
  max_seconds: 600,
};

const TIME_EDGES: Scenario = {
  name: 'time-edges',
  description:
    'Time limit edges: 20 s per question, question 3 has its own 8 s, question 4 has no limit. ' +
    'Answers just before the limit, inside and after the 5 s grace (sent while the Wi-Fi is off), ' +
    'a picked but unsent option, no answer, a reload, and a student who closes the tab.',
  quiz_file: 'time-edges.yaml',
  quiz: {
    title: 'Перевірка часу',
    show_right_answer: true,
    time_limit: { per_question: 20 },
    questions: [
      { question: 'Скільки буде 6 × 7?', options: ['42', '36', '48', '49'], correct_answer: 0 },
      { question: 'Столиця України?', options: ['Львів', 'Київ', 'Харків', 'Одеса'], correct_answer: 1 },
      {
        question: 'Скільки сторін у трикутника?',
        options: ['3', '4', '5', '6'],
        correct_answer: 0,
        time_limit: 8,
      },
      {
        question: 'Скільки буде 15 − 7?',
        options: ['7', '8', '9', '6'],
        correct_answer: 1,
        time_limit: 0,
      },
    ],
  },
  start_settings: {},
  answers: { 1: '42', 2: 'Київ', 3: '3', 4: '8' },
  scripted: {
    'e01-before-limit': { actions: { 1: ['answer', '42', 17], 4: ['answer', '8', 30] } },
    'e02-late-in-grace': { actions: { 1: ['offline_answer', '42', 15, 8, 17] } },
    'e03-late-after-grace': { actions: { 1: ['offline_answer', '42', 15, 15, 17] } },
    'e04-picked-not-sent': { actions: { 1: ['pick', '36', 4] } },
    'e05-idle': { actions: { 1: ['idle'], 2: ['idle'], 3: ['idle'] } },
    'e06-reload': { actions: { 2: ['reload', 8, 'Київ', 14] } },
    'e07-no-limit': { actions: { 4: ['answer', '8', 45] } },
    'e08-leaves': { actions: { 2: ['leave', 5] } },
  },
  expectations: {
    'e01-before-limit': { 1: 'correct', 4: 'correct' },
    'e02-late-in-grace': { 1: 'correct' },
    'e03-late-after-grace': { 1: 'timeout' },
    'e04-picked-not-sent': { 1: 'timeout' },
    'e05-idle': { 1: 'timeout', 2: 'timeout', 3: 'timeout', 4: 'correct' },
    'e06-reload': { 2: 'correct' },
    'e07-no-limit': { 4: 'correct' },
    'e08-leaves': { 2: 'timeout', 3: 'missing' },
  },
  agents: {
    'agent-slow': {
      device: 'desktop',
      persona:
        'You are a slow student. Before answering each question, run `wait 25` once ' +
        '(you read slowly), then answer.',
    },
    'agent-normal': {
      device: 'desktop',
      persona: 'You are an ordinary student. Answer each question at a normal pace.',
    },
  },
  finish_after: 60,
  max_seconds: 480,
};

const WHOLE_QUIZ: Scenario = {
  name: 'whole-quiz-wifi',
  description:
    'Whole-quiz limit chosen in the start pop-up: the file says 30 s per question and correct answers ' +
    'shown; the teacher starts it with 75 s for the whole quiz and correct answers hidden. Students ' +
    'finish early, run out of time, lose Wi-Fi when time runs out, close the tab, or reload a lot.',
  quiz_file: 'whole-quiz.yaml',
  quiz: {
    title: 'Контрольна: множення',
    show_right_answer: true,
    time_limit: { per_question: 30 },
    questions: [
      { question: '3 × 4 = ?', options: ['7', '12', '34', '9'], correct_answer: 1 },
      { question: '5 × 6 = ?', options: ['30', '11', '56', '25'], correct_answer: 0 },
      { question: '7 × 8 = ?', options: ['54', '56', '64', '48'], correct_answer: 1 },
      { question: '9 × 9 = ?', options: ['18', '72', '81', '99'], correct_answer: 2 },
      { question: '6 × 7 = ?', options: ['42', '36', '48', '13'], correct_answer: 0 },
      { question: '8 × 4 = ?', options: ['12', '24', '32', '36'], correct_answer: 2 },
    ],
  },
  start_settings: { time_limit: { whole_quiz: 75 }, show_right_answer: false },
  answers: { 1: '12', 2: '30', 3: '56', 4: '81', 5: '42', 6: '32' },
  scripted: {
    'w01-early': { actions: {} },
    'w02-slow': { actions: each(range(1, 6), () => ['answer', null, 15]) },
    'w03-wifi-at-end': { actions: { 4: ['offline_at_quiz_time', 70, 25] } },
    'w04-leaves': { actions: { 3: ['leave', 3] } },
    'w05-reloader': { actions: each(range(1, 6), () => ['reload', 3, null, 6]) },
  },
  expectations: {
    'w01-early': all(range(1, 6), 'correct'),
    'w02-slow': { 1: 'correct', 6: 'timeout' },
    'w03-wifi-at-end': { 1: 'correct', 4: 'timeout', 5: 'timeout', 6: 'timeout' },
    'w04-leaves': { 2: 'correct', 3: 'timeout', 4: 'timeout', 5: 'timeout', 6: 'timeout' },
    'w05-reloader': all(range(1, 6), 'correct'),
  },
  agents: {
    'agent-wifi': {
      device: 'phone',
      persona:
        'You are a student with bad Wi-Fi. Twice during the quiz, run `offline 10` and keep going ' +
        '(click an answer and submit while offline).',
    },
    'agent-reloader': {
      device: 'desktop',
      persona: 'You are a nervous student who reloads the page (`reload`) a few times during the quiz.',
    },
  },
  finish_after: 95,
  max_seconds: 420,
};

const WAITING_NOTE =
  'The teacher has to let you in: after you register you see a waiting screen. Press the button ' +
  '"🔄 Перевірити" about every 10 seconds (use `wait 10` in between) until the quiz starts.';

const APPROVAL: Scenario = {
  name: 'approval-whole-quiz',
  description:
    'Approval mode with a whole-quiz limit: the file says 20 s per question; the teacher starts it with ' +
    '50 s for the whole quiz. The teacher approves students at different times. One student presses ' +
    "'Check' 40 s after being approved, one checks too early, one is approved late, one runs out of time, " +
    'one is never approved. Waiting must never use up quiz time.',
  quiz_file: 'approval.yaml',
  config: { registration: { approve: true } },
  quiz: {
    title: 'Географія',
    show_right_answer: true,
    time_limit: { per_question: 20 },
    questions: [
      { question: 'Столиця Польщі?', options: ['Варшава', 'Краків', 'Гданськ'], correct_answer: 0 },
      { question: 'Найдовша річка України?', options: ['Дністер', 'Дніпро', 'Південний Буг'], correct_answer: 1 },
      { question: 'Яке море на півдні України?', options: ['Балтійське', 'Чорне', 'Біле'], correct_answer: 1 },
      { question: 'Найвища гора України?', options: ['Говерла', 'Петрос', 'Піп Іван'], correct_answer: 0 },
      { question: 'Скільки областей в Україні?', options: ['20', '24', '27'], correct_answer: 1 },
    ],
  },
  start_settings: { time_limit: { whole_quiz: 50 } },
  answers: { 1: 'Варшава', 2: 'Дніпро', 3: 'Чорне', 4: 'Говерла', 5: '24' },
  // When the teacher clicks "approve" (seconds after the start); null = never
  approvals: {
    'a01-prompt': 3,
    'a02-late-check': 3,
    'a03-checks-early': 15,
    'a04-never-approved': null,
    'a05-slow': 3,
    'a06-approved-late': 40,
  },
  agent_approval_delay: 15,
  scripted: {
    'a01-prompt': { check: [5], actions: {} },
    'a02-late-check': { check: [45], actions: {} },
    'a03-checks-early': { check: [2, 20], actions: {} },
    'a04-never-approved': { check: [10, 30], actions: {} },
    'a05-slow': { check: [5], actions: each(range(1, 5), () => ['answer', null, 10]) },
    'a06-approved-late': { check: [45], actions: {} },
  },
  expectations: {
    'a01-prompt': all(range(1, 5), 'correct'),
    'a02-late-check': all(range(1, 5), 'correct'),
    'a03-checks-early': all(range(1, 5), 'correct'),
    'a04-never-approved': all(range(1, 5), 'missing'),
    'a05-slow': { 1: 'correct', 2: 'correct', 3: 'correct', 4: 'correct', 5: 'timeout' },
    'a06-approved-late': all(range(1, 5), 'correct'),
  },
  agents: {
    'agent-waiting': { device: 'desktop', persona: 'You are an ordinary student. ' + WAITING_NOTE },
    'agent-impatient': {
      device: 'phone',
      persona: 'You are an impatient student on a phone who also reloads the page while waiting. ' + WAITING_NOTE,
    },
  },
  finish_after: 120,
  max_seconds: 480,
};

/** A small seeded random generator (mulberry32), so the same class size gives the same class. */
function seededRandom(seed: number): () => number {
  let a = seed >>> 0;
  return () => {
    a = (a + 0x6d2b79f5) >>> 0;
    let t = a;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

/**
 * `size` scripted students (at least 20): 10 special ones reload, leave, lose Wi-Fi or skip a
 * question; all others answer correctly at random speeds. The same size always gives the same class.
 */
function bigClass(size: number): Pick<Scenario, 'scripted' | 'expectations'> {
  const random = seededRandom(7);
  const between = (low: number, high: number) => low + random() * (high - low);
  const integer = (low: number, high: number) => Math.floor(between(low, high + 1));
  const scripted: Record<string, Plan> = {};
  const expectations: Record<string, Record<number, Outcome>> = {};
  const regular = size - 10;
  for (let n = 1; n <= regular; n++) {
    const name = `c${String(n).padStart(2, '0')}`;
    scripted[name] = { actions: each(range(1, 6), () => ['answer', null, Math.round(between(2, 18) * 10) / 10]) };
    expectations[name] = all(range(1, 6), 'correct');
  }
  let number = regular;
  for (const kind of ['reload', 'reload', 'reload', 'leaves', 'leaves', 'leaves', 'wifi', 'wifi', 'wifi', 'idle']) {
    number += 1;
    const name = `c${String(number).padStart(2, '0')}-${kind}`;
    if (kind === 'reload') {
      scripted[name] = { actions: { [integer(1, 6)]: ['reload', 3, null, 8] } };
      expectations[name] = all(range(1, 6), 'correct');
    } else if (kind === 'leaves') {
      scripted[name] = { actions: { [integer(2, 5)]: ['leave', 4] } };
    } else if (kind === 'wifi') {
      scripted[name] = { actions: each([2, 4], () => ['offline_answer', null, 3, 4, 5]) };
      expectations[name] = all(range(1, 6), 'correct');
    } else {
      scripted[name] = { actions: { 3: ['idle'] } };
    }
  }
  return { scripted, expectations };
}

export function bigClassScenario(size = 40): Scenario {
  const { scripted, expectations } = bigClass(size);
  return {
    name: 'big-class',
    description:
      `A big class at once: ${size} scripted students plus AI agents, random question order, 30 s per question. ` +
      'Most answer correctly at random speeds; some reload, leave, lose Wi-Fi for a moment, or skip a question. ' +
      'Checks that the clocks stay accurate under load.',
    quiz_file: 'big-class.yaml',
    quiz: {
      title: 'Природознавство',
      show_right_answer: true,
      randomize_questions: true,
      time_limit: { per_question: 30 },
      questions: [
        { question: 'Скільки планет у Сонячній системі?', options: ['7', '8', '9'], correct_answer: 1 },
        { question: 'Яка планета найбільша?', options: ['Марс', 'Юпітер', 'Земля'], correct_answer: 1 },
        {
          question: 'Що з цього — ссавці? (усі правильні)',
          options: ['Кит', 'Акула', 'Кажан', 'Пінгвін'],
          correct_answer: [0, 2],
        },
        {
          question: 'За якої температури кипить вода (°C)? Введіть число.',
          checker: "assert to_int(user_answer) == 100, 'Ні'",
          correct_value: '100',
        },
        {
          question: 'Що потрібно рослинам для фотосинтезу?',
          options: ['Світло', 'Темрява', 'Сіль'],
          correct_answer: 0,
        },
        { question: 'Який газ ми видихаємо?', options: ['Кисень', 'Вуглекислий газ', 'Азот'], correct_answer: 1 },
      ],
    },
    start_settings: {},
    answers: { 1: '8', 2: 'Юпітер', 3: ['Кит', 'Кажан'], 4: { text: '100' }, 5: 'Світло', 6: 'Вуглекислий газ' },
    scripted,
    expectations,
    agents: {
      'agent-big-1': { device: 'desktop', persona: 'You are an ordinary student. Answer at a normal pace.' },
      'agent-big-2': { device: 'phone', persona: 'You are a quick student on a phone.' },
    },
    finish_after: 60,
    max_seconds: 600,
  };
}


// ---- Richer student scenarios ----

const RICH: Scenario = {
  name: 'rich-quiz',
  description:
    'A rich quiz: a question picture and picture options, a file to download, explanations after the answers ' +
    '(show_explanation on), points 2 and 0.5, a several-answers question with min_correct 2, a text question whose ' +
    'checker gives a message, registration fields (one of them disabled) and a username label, ' +
    'show_answers_on_completion on, 40 s per question. Most scripted students use phones.',
  quiz_file: 'rich.yaml',
  config: {
    registration: {
      fields: ['Клас', 'Школа'],
      disabled_fields: ['Email'],
      username_label: "Прізвище та ім'я",
    },
  },
  files: {
    images: {
      'shape.png': { shape: 'square', color: [200, 40, 40] },
      'circle.png': { shape: 'circle', color: [40, 90, 210] },
      'square.png': { shape: 'square', color: [40, 160, 70] },
      'triangle.png': { shape: 'triangle', color: [230, 140, 20] },
    },
    attachments: { 'data.csv': 'name,count\nA,1\nB,2\nC,3\n' },
  },
  quiz: {
    title: 'Фігури і числа',
    show_right_answer: true,
    show_explanation: true,
    show_answers_on_completion: true,
    time_limit: { per_question: 40 },
    questions: [
      {
        question: 'Яка фігура на малюнку?',
        image: '/imgs/shape.png',
        options: ['Квадрат', 'Коло', 'Трикутник'],
        correct_answer: 0,
        explanation: '**Квадрат** має:\n\n- чотири рівні сторони\n- чотири прямі кути',
      },
      {
        question: 'Оберіть коло',
        options: ['/imgs/square.png', '/imgs/circle.png', '/imgs/triangle.png'],
        correct_answer: 1,
        points: 2,
      },
      {
        question: 'Відкрийте файл data.csv. Скільки в ньому рядків з даними (без заголовка)?',
        file: 'data.csv',
        checker: "assert to_int(user_answer) == 3, 'Порахуйте рядки ще раз'",
        correct_value: '3',
        points: 0.5,
        explanation: 'У файлі є заголовок `name,count` і **три** рядки з даними.',
      },
      {
        question: 'Які числа парні? (достатньо двох правильних)',
        options: ['2', '3', '4', '6'],
        correct_answer: [0, 2, 3],
        min_correct: 2,
        explanation: 'Парні числа діляться на 2 без остачі:\n\n1. 2\n2. 4\n3. 6',
      },
      { question: 'Скільки сторін у трикутника?', options: ['3', '4', '5'], correct_answer: 0 },
    ],
  },
  start_settings: {},
  answers: { 1: 'Квадрат', 2: '/imgs/circle.png', 3: { text: '3' }, 4: ['2', '4', '6'], 5: '3' },
  scripted: {
    'r01-phone-right': { device: 'phone', actions: {} },
    'r02-desk-mixed': { actions: { 1: ['answer', 'Коло', 3], 4: ['answer', ['2', '4'], 4] } },
    'r03-phone-checker': { device: 'phone', actions: { 3: ['text', '5', 4], 4: ['answer', ['2'], 4] } },
    'r04-phone-extra': { device: 'phone', actions: { 4: ['answer', ['2', '3', '4'], 4] } },
    'r05-desk-idle': { actions: { 2: ['idle'] } },
  },
  expectations: {
    'r01-phone-right': all(range(1, 5), 'correct'),
    'r02-desk-mixed': { 1: 'wrong', 2: 'correct', 3: 'correct', 4: 'correct', 5: 'correct' },
    'r03-phone-checker': { 1: 'correct', 3: 'wrong', 4: 'wrong', 5: 'correct' },
    'r04-phone-extra': { 4: 'wrong' },
    'r05-desk-idle': { 1: 'correct', 2: 'timeout', 3: 'correct' },
  },
  agents: {
    'agent-rich-desk': {
      device: 'desktop',
      persona:
        'You are a careful student. Some answers are pictures: click the picture option (CLICKABLE shows it as ' +
        '"[image circle.png]" or similar). One question has a file to download; you may click it. After some answers ' +
        'an explanation is shown: read it and click "Далі".',
    },
    'agent-rich-phone': {
      device: 'phone',
      persona:
        'You are a student on a phone who answers quickly. After some answers an explanation is shown: click "Далі" ' +
        'to go on. Some answers are pictures.',
    },
  },
  finish_after: 120,
  max_seconds: 600,
};

const WAITING_NOTE_EN =
  'The teacher has to let you in: after you register you see a waiting screen. Press the button ' +
  '"🔄 Check" about every 10 seconds (use `wait 10` in between) until the quiz starts.';

const ENGLISH: Scenario = {
  name: 'english-approval',
  description:
    'English interface with approval: the file says 30 s per question and correct answers shown; the teacher ' +
    'starts it with 2 minutes for the whole quiz, correct answers hidden (no Continue button), no final list, ' +
    'and a random order where two questions stick together. Most scripted students use phones.',
  quiz_file: 'english.yaml',
  config: { language: 'en', registration: { approve: true } },
  quiz: {
    title: 'General knowledge',
    show_right_answer: true,
    time_limit: { per_question: 30 },
    questions: [
      { question: 'What is the capital of France?', options: ['Paris', 'Rome', 'Madrid'], correct_answer: 0 },
      { question: 'Look at the numbers 2, 4, 6, 8. Which number comes next?', options: ['9', '10', '12'], correct_answer: 1 },
      {
        question: 'And which number comes after that one?',
        options: ['11', '12', '14'],
        correct_answer: 1,
        stick_to_the_previous: true,
      },
      { question: 'Which of these numbers are even? (all correct ones)', options: ['3', '4', '7', '8'], correct_answer: [1, 3] },
      {
        question: 'How many days are there in a week? Type the number.',
        checker: "assert to_int(user_answer) == 7, 'Not quite'",
        correct_value: '7',
      },
    ],
  },
  start_settings: {
    show_right_answer: false,
    randomize_questions: true,
    show_final_list: false,
    time_limit: { whole_quiz: 120 },
  },
  answers: { 1: 'Paris', 2: '10', 3: '12', 4: ['4', '8'], 5: { text: '7' } },
  wrong_answers: { 1: 'Rome', 2: '12', 3: '14', 4: ['3'], 5: { text: '8' } },
  approvals: {
    'g01-phone': 3,
    'g02-desk-wrong': 3,
    'g03-phone-late': 25,
    'g04-phone-never': null,
    'g05-phone-idle': 5,
  },
  agent_approval_delay: 10,
  scripted: {
    'g01-phone': { device: 'phone', check: [5], actions: {} },
    'g02-desk-wrong': { check: [5], actions: each(range(1, 5), () => ['answer', { wrong: true }, 3]) },
    'g03-phone-late': { device: 'phone', check: [10, 30], actions: {} },
    'g04-phone-never': { device: 'phone', check: [10, 30], actions: {} },
    'g05-phone-idle': { device: 'phone', check: [8], actions: { 4: ['idle'] } },
  },
  expectations: {
    'g01-phone': all(range(1, 5), 'correct'),
    'g02-desk-wrong': all(range(1, 5), 'wrong'),
    'g03-phone-late': all(range(1, 5), 'correct'),
    'g04-phone-never': all(range(1, 5), 'missing'),
  },
  agents: {
    'agent-en-phone': {
      device: 'phone',
      persona:
        'You are a student on a phone. The quiz is in English: the register button is "Register" and the submit ' +
        'button is "Submit answer"; after an answer the next question comes by itself. ' + WAITING_NOTE_EN,
    },
    'agent-en-desk': {
      device: 'desktop',
      persona:
        'You are a careful student. The quiz is in English (the submit button is "Submit answer"). ' + WAITING_NOTE_EN,
    },
  },
  finish_after: 170,
  max_seconds: 600,
};

// ---- Teacher scenarios ----

export interface TeacherAgent {
  device: 'desktop' | 'phone';
  /** The task list the agent gets in its prompt. */
  task: string;
}

export interface TeacherScenario {
  kind: 'teacher';
  name: string;
  description: string;
  /** webquiz.yaml values, as in Scenario.config. */
  config?: Record<string, Dict | string | number | boolean>;
  /** quizzes/<file name>: the quiz files the server starts with. */
  quizzes: Record<string, QuizData>;
  files?: ScenarioFiles;
  /** One teacher agent per scenario. */
  teacher_agents: Record<string, TeacherAgent>;
  /** The run stops after this many seconds even without agents_done. */
  max_seconds: number;
}

/** Teachers log in with the master key: no address is trusted. */
const LOGIN_NEEDED = { admin: { trusted_ips: [] as string[] } };

const MATH: QuizData = {
  title: 'Математика 5 клас',
  show_right_answer: true,
  questions: [
    { question: '7 × 8 = ?', options: ['54', '56', '64'], correct_answer: 1 },
    { question: '81 : 9 = ?', options: ['8', '9', '7'], correct_answer: 1 },
    { question: 'Скільки буде 15 − 7? Введіть число.', checker: "assert to_int(user_answer) == 8, 'Ні'", correct_value: '8' },
  ],
};

const HISTORY: QuizData = {
  title: 'Історія: вступ',
  show_right_answer: true,
  questions: [
    { question: 'У якому році Україна стала незалежною?', options: ['1990', '1991', '1996'], correct_answer: 1 },
    { question: 'Яке місто — столиця Київської Русі?', options: ['Київ', 'Львів', 'Чернігів'], correct_answer: 0 },
  ],
};

const GEOGRAPHY: QuizData = {
  title: 'Географія',
  show_right_answer: true,
  questions: [
    { question: 'Столиця Польщі?', options: ['Варшава', 'Краків', 'Гданськ'], correct_answer: 0 },
    { question: 'Найдовша річка України?', options: ['Дністер', 'Дніпро', 'Південний Буг'], correct_answer: 1 },
    { question: 'Яке море на півдні України?', options: ['Балтійське', 'Чорне', 'Біле'], correct_answer: 1 },
    { question: 'Найвища гора України?', options: ['Говерла', 'Петрос', 'Піп Іван'], correct_answer: 0 },
    { question: 'Скільки областей в Україні?', options: ['20', '24', '27'], correct_answer: 1 },
  ],
};

const NATURE: QuizData = {
  title: 'Природа',
  show_right_answer: true,
  questions: [
    { question: 'Що дає Землі світло і тепло?', options: ['Сонце', 'Місяць', 'Хмари'], correct_answer: 0 },
    { question: 'Які з цих рослин — дерева?', options: ['Дуб', 'Ромашка', 'Береза', 'Тюльпан'], correct_answer: [0, 2] },
    {
      question: 'Скільки ніг у павука? Введіть число.',
      checker: "assert to_int(user_answer) == 8, 'Порахуйте ще раз'",
      correct_value: '8',
    },
    { question: 'Де живе риба?', options: ['У воді', 'На дереві', 'У пісках'], correct_answer: 0 },
  ],
};

const ANIMALS: QuizData = {
  title: 'Тварини',
  show_right_answer: true,
  questions: [
    { question: 'Яка тварина найбільша на суші?', options: ['Слон', 'Жираф', 'Ведмідь'], correct_answer: 0 },
    { question: 'Хто з них ссавці? (усі правильні)', options: ['Кит', 'Акула', 'Кажан', 'Пінгвін'], correct_answer: [0, 2] },
    { question: 'Скільки лап у кота? Введіть число.', checker: "assert to_int(user_answer) == 4, 'Ні'", correct_value: '4' },
  ],
};

const WORDS: QuizData = {
  title: 'Words',
  show_right_answer: true,
  questions: [
    { question: 'Which word is a colour?', options: ['Green', 'Table', 'Run'], correct_answer: 0 },
    { question: 'Which word is an animal?', options: ['Chair', 'Horse', 'Blue'], correct_answer: 1 },
    {
      question: 'Type the plural of "mouse".',
      checker: "assert user_answer.strip().lower() == 'mice', 'Not quite'",
      correct_value: 'mice',
    },
  ],
};

const FIRST_LESSON: TeacherScenario = {
  kind: 'teacher',
  name: 'teacher-first-lesson',
  description:
    'A teacher uses WebQuiz for the first time: logs in, creates a quiz with three question types, starts it ' +
    'with 30 s per question and correct answers shown, lets 5 automatic students take it, watches live stats, ' +
    'and opens the answers in the file manager table, filtered to one student.',
  config: LOGIN_NEEDED,
  quizzes: { 'math.yaml': MATH, 'history.yaml': HISTORY },
  teacher_agents: {
    'teacher-first': {
      device: 'desktop',
      task: [
        '1. Open the admin panel and log in with the master key.',
        '2. Create a new quiz. File name: "first-lesson". Title: "Перший урок". It has three questions:',
        '   - question 1 has one correct answer: "Скільки буде 2 + 3?" with the options "4", "5" and "6"; the correct one is "5";',
        '   - question 2 has several correct answers: "Які з цих чисел парні?" with the options "2", "3", "4" and "7"; the correct ones are "2" and "4";',
        '   - question 3 has a text answer: "Скільки годин у добі?"; the correct value is "24" (make sure that the answer 24 counts as correct).',
        '   Save the quiz.',
        '3. Start this quiz with 30 seconds for each question and with the correct answers shown to the students.',
        '4. Run `students 5` so that five students join.',
        '5. Open the live stats page and watch it until all five students have finished.',
        '6. Open the file manager. Find the answers CSV file of this quiz (its name starts with "first-lesson"), open it in the table view, and filter the table so that it shows one student only.',
      ].join('\n'),
    },
  },
  max_seconds: 3600,
};

const APPROVAL_TEACHER: TeacherScenario = {
  kind: 'teacher',
  name: 'teacher-approval',
  description:
    'A teacher turns on approval and a registration field in the config form, starts "Географія" with 2 minutes ' +
    'for the whole quiz and "show answers after everyone finishes", approves 4 automatic students as they come, ' +
    'shows the answers to everyone before all have finished, and looks at live stats.',
  config: LOGIN_NEEDED,
  quizzes: { 'geography.yaml': GEOGRAPHY, 'math.yaml': MATH },
  teacher_agents: {
    'teacher-approval': {
      device: 'desktop',
      task: [
        '1. Open the file manager and log in with the master key. Go to the Config tab. In the form, turn on approval by the teacher (admin) and add a registration field named "Клас". Save the configuration.',
        '2. Open the admin panel. Start the quiz "Географія" with a time limit of 2 minutes (120 seconds) for the whole quiz, and with "show answers after everyone finishes" turned on. (This choice only shows when the correct answers are not shown right away.)',
        '3. Run `students 4`.',
        '4. The students wait until you let them in. In the admin panel, approve each student as they show up. Look again (or wait a little) until all four are approved.',
        '5. When at least one student has finished, and before all of them have finished, press the button that shows the correct answers to all students now.',
        '6. Open the live stats page and look at the results.',
      ].join('\n'),
    },
  },
  max_seconds: 3600,
};

const EDITOR_TEACHER: TeacherScenario = {
  kind: 'teacher',
  name: 'teacher-editor',
  description:
    'A teacher edits a quiz in the admin editor (an explanation with bold text and a list and its preview, ' +
    '0.5 points, a question that sticks to the previous one, a picture from the image list, an attached file), ' +
    'duplicates and renames a quiz, unites two quizzes, deletes the copy, then starts the edited quiz with ' +
    'explanations on for 2 automatic students.',
  config: LOGIN_NEEDED,
  quizzes: { 'nature.yaml': NATURE, 'animals.yaml': ANIMALS },
  files: {
    images: {
      'sun.png': { shape: 'circle', color: [240, 200, 30] },
      'forest.png': { shape: 'stripes', color: [30, 140, 60] },
      'river.png': { shape: 'stripes', color: [40, 110, 220] },
    },
    attachments: { 'plants.txt': 'Дуб\nБереза\nРомашка\nТюльпан\n' },
  },
  teacher_agents: {
    'teacher-editor': {
      device: 'desktop',
      task: [
        '1. Open the admin panel and log in with the master key.',
        '2. Edit the quiz "nature.yaml" (title "Природа"). The questions are folded: click a question\'s header to open it.',
        '   a. For question 1, add an explanation with bold text and a list, for example: **Сонце** — це зоря. Then a list with two items: "дає світло" and "дає тепло". Preview the explanation, then go back to editing.',
        '   b. Give question 2 a value of 0.5 points.',
        '   c. Make question 3 stick to the previous question.',
        '   d. For question 4, pick a picture from the image list (any picture).',
        '   e. Attach the file "plants.txt" to question 2 (use the button next to the file field).',
        '   f. Save the quiz.',
        '3. Duplicate the quiz "nature.yaml". The copy opens in the editor: change its file name to "nature-copy-2" and save it.',
        '4. Unite the quizzes "nature.yaml" and "animals.yaml" into one new quiz (the suggested name is fine).',
        '5. Delete the copy "nature-copy-2.yaml".',
        '6. Start the quiz "nature.yaml" with the explanations turned on.',
        '7. Run `students 2` and watch the live stats until both students have finished.',
      ].join('\n'),
    },
  },
  max_seconds: 3600,
};

const SETTINGS_EN_TEACHER: TeacherScenario = {
  kind: 'teacher',
  name: 'teacher-settings-en',
  description:
    'A teacher switches the quiz language to English and hides a registration field in the config form, ' +
    'breaks a quiz file in the file manager on purpose, reads the validation error, fixes and saves it, ' +
    'starts that quiz, and looks at the student page.',
  config: { ...LOGIN_NEEDED, registration: { fields: ['Клас', 'Школа'] } },
  quizzes: { 'words.yaml': WORDS, 'math.yaml': MATH },
  teacher_agents: {
    'teacher-settings': {
      device: 'desktop',
      task: [
        '1. Open the file manager and log in with the master key.',
        '2. Go to the Config tab. In the form, change the quiz interface language to English, and turn off the registration field "Школа" with its checkbox (do not delete it). Save the configuration.',
        '3. Go to the quiz files tab and edit "words.yaml". Make a mistake in the YAML on purpose (for example, remove the colon after the first "question"). Press the button that validates the YAML and read the error. Then fix the mistake, validate again, and save.',
        '4. Open the admin panel and start the quiz "words.yaml" (keep its settings).',
        '5. Open the student quiz page (`open quiz`) and look at it: is it in English, and does it ask only for "Клас" besides the name? You do not need to take the quiz.',
      ].join('\n'),
    },
  },
  max_seconds: 3600,
};

const PHONE_TEACHER: TeacherScenario = {
  kind: 'teacher',
  name: 'teacher-phone',
  description:
    'A teacher on a phone logs in, starts a quiz with 45 s per question, lets 3 automatic students take it, ' +
    'watches live stats, and downloads the answers CSV from the file manager.',
  config: LOGIN_NEEDED,
  quizzes: { 'animals.yaml': ANIMALS, 'geography.yaml': GEOGRAPHY },
  teacher_agents: {
    'teacher-phone': {
      device: 'phone',
      task: [
        'You use a phone, so the pages are narrow.',
        '1. Open the admin panel and log in with the master key.',
        '2. Start the quiz "Тварини" (animals.yaml) with 45 seconds for each question.',
        '3. Run `students 3`.',
        '4. Open the live stats page and watch it until all three students have finished.',
        '5. Open the file manager and download the answers CSV file of this quiz (its name starts with "animals").',
      ].join('\n'),
    },
  },
  max_seconds: 3600,
};

export const TEACHER_SCENARIOS: Record<string, TeacherScenario> = Object.fromEntries(
  [FIRST_LESSON, APPROVAL_TEACHER, EDITOR_TEACHER, SETTINGS_EN_TEACHER, PHONE_TEACHER].map((s) => [s.name, s]),
);

export const SCENARIOS: Record<string, Scenario> = Object.fromEntries(
  [FRACTIONS, TIME_EDGES, WHOLE_QUIZ, APPROVAL, bigClassScenario(), RICH, ENGLISH].map((s) => [s.name, s]),
);
