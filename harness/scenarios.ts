// The pilot scenarios.
//
// Each scenario has a quiz file, the settings the teacher picks in the start
// pop-up, scripted students with exact plans, and AI agent personas.
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
// A label of null means the scenario's correct answer for that question. A text
// answer is written as {text: "6"}. Times are seconds since the question appeared
// on that student's screen (for offline_at_quiz_time: since the first question appeared).
//
// "expectations" give the outcome each scripted edge case must have in the stored
// data: "correct", "wrong", "timeout" or "missing" (no row), by question id.

export type Dict = Record<string, unknown>;

/** What a scripted student answers: null = the correct one, a label, several labels, or typed text. */
export type AnswerSpec = null | string | string[] | { text: string };

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
}

export type Outcome = 'correct' | 'wrong' | 'timeout' | 'missing';

export interface Agent {
  device: 'desktop' | 'phone';
  persona: string;
}

export interface Scenario {
  name: string;
  description: string;
  quiz_file: string;
  quiz: { title: string; questions: Dict[]; [key: string]: unknown };
  config?: Record<string, Dict>;
  start_settings: Dict;
  answers: Record<number, AnswerSpec>;
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

export const SCENARIOS: Record<string, Scenario> = Object.fromEntries(
  [FRACTIONS, TIME_EDGES, WHOLE_QUIZ, APPROVAL, bigClassScenario()].map((s) => [s.name, s]),
);
