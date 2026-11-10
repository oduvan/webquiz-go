// Checks for a harness run, and the report that lines up the three records.
//
// The three records of one run:
// 1. what each AI agent says it did and saw (its report, by step number);
// 2. what the driver recorded (each step's command, screen text, screenshot,
//    and every request the page sent, with the reply);
// 3. what the server stored (answers CSV, server log, live stats messages).
//
// The check functions are pure: they take plain lists and objects and return a
// list of problem descriptions (empty = fine). Run the analysis with:
//
//     npx tsx harness/checks.ts <run_dir>

import { existsSync, mkdirSync, readFileSync, readdirSync, writeFileSync } from 'node:fs';
import path from 'node:path';
import { pathToFileURL } from 'node:url';
import { formatTeacherStep, type TeacherStepRecord } from './elements';
import { formatStep, type StepRecord } from './steps';

const STEP_LINE = /^\s*step\s+(\d+)\s*\|\s*did:\s*(.*?)\s*\|\s*saw:\s*(.*)$/i;
const QUOTE_SEPARATOR = /["“”»]\s*;\s*["“«]/;
const QUOTE_MARKS = '"“”«» ';
const SCORE = /(?:Результат|Result):\s*(\d+)\s*\/\s*(\d+)/;
const WORD_CHAR = /[\p{L}\p{N}_]/u;

export interface AnswerRow {
  user_id: string;
  question: string;
  selected_answer: string;
  is_correct: boolean;
  time_taken: number;
}

export interface Submit {
  user_id: string;
  question: string;
  status: number;
  response: Record<string, unknown>;
}

export interface AgentStep {
  step: number;
  did: string;
  quotes: string[];
}

export interface DriverStep {
  cmd: string;
  arg?: string | null;
  /** A teacher command's arguments: each must show in what the agent says it did. */
  args?: string[];
  screen?: string;
}

/** Lower case with single spaces, for comparing what an agent quoted with the screen. */
function plain(text: unknown): string {
  return String(text).replace(/\s+/g, ' ').trim().toLowerCase();
}

function fixed1(value: number): string {
  return value.toFixed(1);
}

// ---- 1 vs 2: the agent's report against the driver's record ----

/**
 * Split 'SAW: "a"; "b"' into ["a", "b"].
 *
 * Splitting on the '"; "' separator (instead of matching quote pairs) keeps a quote that
 * itself contains quotes, like '"TYPE INTO: ... = "6""', in one piece.
 */
export function splitQuotes(saw: string): string[] {
  // Some agents write quotes inside a quote with a backslash (\") as in JSON
  const pieces = saw.trim().replaceAll('\\"', '"').split(QUOTE_SEPARATOR);
  const quotes: string[] = [];
  for (let piece of pieces) {
    piece = piece.trim();
    if (piece.length && QUOTE_MARKS.includes(piece[0])) piece = piece.slice(1);
    if (piece.length && QUOTE_MARKS.includes(piece[piece.length - 1])) piece = piece.slice(0, -1);
    piece = piece.trim();
    if (WORD_CHAR.test(piece)) quotes.push(piece);
  }
  return quotes;
}

/** Read the lines 'STEP n | DID: ... | SAW: "quote"; "quote"' from an agent's report. */
export function parseAgentReport(text: string): AgentStep[] {
  const steps: AgentStep[] = [];
  for (const line of text.split(/\r?\n/)) {
    const match = STEP_LINE.exec(line);
    if (match) {
      steps.push({ step: Number(match[1]), did: match[2].trim(), quotes: splitQuotes(match[3]) });
    }
  }
  return steps;
}

/**
 * Compare what the agent says it did and saw with the driver's record of the same step.
 *
 * driverSteps: {step number: {cmd, arg, screen}} for this student.
 * A problem here usually means the agent misread or misreported, not an app bug.
 */
export function checkAgentAlignment(agentSteps: AgentStep[], driverSteps: Map<number, DriverStep>): string[] {
  const problems: string[] = [];
  for (const reported of agentSteps) {
    const number = reported.step;
    const recorded = driverSteps.get(number);
    if (recorded === undefined) {
      problems.push(`step ${number}: the agent reports a step the driver never ran`);
      continue;
    }
    const did = plain(reported.did);
    const expected = plain([recorded.cmd, recorded.arg].filter((part) => part !== undefined && part !== null).join(' '));
    const hasArg = recorded.arg !== undefined && recorded.arg !== null;
    const argsSeen = Array.isArray(recorded.args)
      ? recorded.args.every((arg) => did.includes(plain(arg)))
      : !hasArg || did.includes(plain(recorded.arg));
    if (!did.startsWith(plain(recorded.cmd)) || !argsSeen) {
      problems.push(`step ${number}: the agent says it did '${reported.did}', the driver ran '${expected}'`);
    }
    const screen = plain(recorded.screen ?? '');
    for (const quote of reported.quotes) {
      if (!screen.includes(plain(quote))) {
        problems.push(`step ${number}: the agent says it saw "${quote}", but the screen did not show it`);
      }
    }
  }
  return problems;
}

// ---- 2 vs 3: what happened in the browser against what the server stored ----

/** Each student has at most one stored answer per question. */
export function checkUniqueAnswers(rows: AnswerRow[]): string[] {
  const seen = new Map<string, { user: string; question: string; count: number }>();
  for (const item of rows) {
    const key = JSON.stringify([item.user_id, item.question]);
    const entry = seen.get(key) ?? { user: item.user_id, question: item.question, count: 0 };
    entry.count += 1;
    seen.set(key, entry);
  }
  return [...seen.values()]
    .filter((entry) => entry.count > 1)
    .map((entry) => `user ${entry.user}: question '${entry.question}' is stored ${entry.count} times`);
}

/** An empty answer means time ran out, so it must be failed. */
export function checkTimeoutRows(rows: AnswerRow[]): string[] {
  return rows
    .filter((item) => item.selected_answer === '' && item.is_correct)
    .map((item) => `user ${item.user_id}: question '${item.question}' has an empty answer marked correct`);
}

/** A stored (non-empty) answer must have arrived within the question's limit plus the grace. */
export function checkAnswerTimes(
  rows: AnswerRow[],
  limits: Record<string, number>,
  grace: number,
  tolerance = 1.0,
): string[] {
  const problems: string[] = [];
  for (const item of rows) {
    const limit = limits[item.question] ?? 0;
    if (limit > 0 && item.selected_answer !== '' && item.time_taken > limit + grace + tolerance) {
      problems.push(
        `user ${item.user_id}: question '${item.question}' was answered after ` +
          `${fixed1(item.time_taken)} s, limit ${limit} s + ${grace} s grace, but it counts as an answer`,
      );
    }
  }
  return problems;
}

/**
 * Every accepted answer is stored, the stored result matches what the page was told,
 * and every stored row comes from a request (or from the server's own timeout check).
 *
 * submits: [{user_id, question, status, response}] from the driver's request log.
 * allowedWithoutSubmit: {[user_id, question]} the server stored by itself (background check,
 * whole-quiz end), as a set of "user_id\u0000question" keys (see pairKey).
 */
export function checkSubmitsMatchRows(submits: Submit[], rows: AnswerRow[], allowedWithoutSubmit: Set<string>): string[] {
  const problems: string[] = [];
  const stored = new Map(rows.map((item) => [pairKey(item.user_id, item.question), item]));
  const accepted = new Set<string>();
  for (const submit of submits) {
    if (submit.status !== 200) continue;
    const key = pairKey(submit.user_id, submit.question);
    accepted.add(key);
    const item = stored.get(key);
    if (item === undefined) {
      problems.push(`user ${submit.user_id}: the answer to '${submit.question}' was accepted (200) but is not stored`);
      continue;
    }
    const response = submit.response || {};
    if (response.timed_out && item.selected_answer !== '') {
      problems.push(
        `user ${submit.user_id}: the page was told 'time is up' for '${submit.question}', ` +
          `but the stored answer is '${item.selected_answer}'`,
      );
    }
    if ('is_correct' in response && !response.timed_out && Boolean(response.is_correct) !== Boolean(item.is_correct)) {
      problems.push(
        `user ${submit.user_id}: the page was told is_correct=${pyBool(response.is_correct)} for '${submit.question}', ` +
          `but the stored row says ${pyBool(item.is_correct)}`,
      );
    }
  }
  for (const [key, item] of stored) {
    if (!accepted.has(key) && !allowedWithoutSubmit.has(key)) {
      problems.push(
        `user ${item.user_id}: '${item.question}' is stored, but no request sent it and the server did not time it out`,
      );
    }
  }
  return problems;
}

/** Key for a (user, question) pair in a Set or Map. */
export function pairKey(user: unknown, question: unknown): string {
  return `${user}\u0000${question}`;
}

function pyBool(value: unknown): string {
  return value ? 'True' : 'False';
}

/**
 * The planned outcome of each scripted edge case matches the stored answer.
 *
 * The other checks only test that the records agree with each other. This one tests that
 * they agree with what the scenario says must happen, for example that an answer arriving
 * inside the grace counts.
 *
 * expectations: {student: {question id: "correct" | "wrong" | "timeout" | "missing"}}
 */
export function checkExpectations(
  expectations: Record<string, Record<string, string>>,
  rows: AnswerRow[],
  userOf: Record<string, string>,
  questionText: Record<string, string>,
): string[] {
  const problems: string[] = [];
  const stored = new Map(rows.map((item) => [pairKey(item.user_id, item.question), item]));
  for (const [student, outcomes] of Object.entries(expectations)) {
    const user = userOf[student];
    if (user === undefined) {
      problems.push(`${student}: never registered, so the planned outcomes cannot be checked`);
      continue;
    }
    for (const [questionId, expected] of Object.entries(outcomes)) {
      const question = questionText[String(Number(questionId))];
      const item = stored.get(pairKey(user, question));
      let actual: string;
      if (item === undefined) actual = 'missing';
      else if (item.selected_answer === '') actual = 'timeout';
      else actual = item.is_correct ? 'correct' : 'wrong';
      if (actual !== expected) {
        problems.push(`${student}: '${question}' should be ${expected}, the stored answer is ${actual}`);
      }
    }
  }
  return problems;
}

/**
 * The time the server stored for an answer matches the time the browser measured.
 *
 * measured: {(user_id, question) key: seconds from sending question-start to sending the
 * accepted answer}. A bigger gap means the server was slow to handle requests (load).
 */
export function checkTimeAccuracy(rows: AnswerRow[], measured: Map<string, number>, tolerance = 1.5): string[] {
  const problems: string[] = [];
  for (const item of rows) {
    const key = pairKey(item.user_id, item.question);
    const browser = measured.get(key);
    if (item.selected_answer === '' || browser === undefined) continue;
    if (Math.abs(item.time_taken - browser) > tolerance) {
      problems.push(
        `user ${item.user_id}: '${item.question}' took ${fixed1(item.time_taken)} s on the server, ` +
          `but ${fixed1(browser)} s in the browser`,
      );
    }
  }
  return problems;
}

/** Read 'Результат: 2/3 (67%)' (or 'Result: ...') from a screen. null when there is no result. */
export function parseScreenScore(text: string | null | undefined): [number, number] | null {
  const match = SCORE.exec(text || '');
  if (!match) return null;
  return [Number(match[1]), Number(match[2])];
}

/** The score a student saw on the results screen equals the stored answers. */
export function checkScreenScores(
  finalScores: Record<string, [number, number]>,
  rows: AnswerRow[],
  totalQuestions: number,
): string[] {
  const problems: string[] = [];
  const correct = new Map<string, number>();
  for (const item of rows) {
    if (item.is_correct) correct.set(item.user_id, (correct.get(item.user_id) ?? 0) + 1);
  }
  for (const [user, [shownCorrect, shownTotal]] of Object.entries(finalScores)) {
    const stored = correct.get(user) ?? 0;
    if (shownCorrect !== stored || shownTotal !== totalQuestions) {
      problems.push(
        `user ${user}: the results screen showed ${shownCorrect}/${shownTotal}, ` +
          `the stored answers give ${stored}/${totalQuestions}`,
      );
    }
  }
  return problems;
}

/** The last live stats state of each answered question matches the stored answer. */
export function checkLiveStats(finalStates: Map<string, string>, rows: AnswerRow[]): string[] {
  const problems: string[] = [];
  for (const item of rows) {
    let expected: string;
    if (item.selected_answer === '') expected = 'timeout';
    else expected = item.is_correct ? 'ok' : 'fail';
    const actual = finalStates.get(pairKey(item.user_id, item.question));
    if (actual !== expected) {
      problems.push(
        `user ${item.user_id}: live stats show '${actual ?? 'None'}' for '${item.question}', ` +
          `the stored answer means '${expected}'`,
      );
    }
  }
  return problems;
}

// ---- The quiz settings: results screen, hidden answers, fields, explanations, language ----

/** What the verify-user reply said that the results screen was drawn from. */
export interface FinalInfo {
  all_completed: boolean;
  show_answers_on_completion: boolean;
  /** The results carry is_correct: the correct answers are shown. */
  has_correctness: boolean;
}

export interface ResultScreen {
  student: string;
  user: string;
  screen: string;
  /** The page's texts (its language), from its boot data. */
  texts: Record<string, string>;
  showFinalList: boolean;
  final: FinalInfo | null;
}

/** The screen's lines and table cells (a table row is one line with tabs between the cells). */
function cells(screen: string): string[] {
  return screen
    .split('\n')
    .flatMap((line) => line.split('\t'))
    .map((cell) => cell.trim())
    .filter(Boolean);
}

/**
 * The results screen follows the quiz settings (Python 1.19's showCompletedTestResults):
 * the score always shows; the list of questions only with show_final_list; the note
 * "answers later" only with show_answers_on_completion while not everyone has finished;
 * the "correct" column only when the reply carries the correct answers.
 */
export function checkResultsScreens(screens: ResultScreen[]): string[] {
  const problems: string[] = [];
  for (const item of screens) {
    const who = `${item.student} (user ${item.user})`;
    const { texts, screen } = item;
    const all = cells(screen);
    if (texts.result_label && !screen.includes(texts.result_label)) {
      problems.push(`${who}: the results screen has no "${texts.result_label}" line`);
    }
    const list = !!texts.question_header && !!texts.your_answer_header && all.includes(texts.question_header) && all.includes(texts.your_answer_header);
    if (item.showFinalList && !list) problems.push(`${who}: show_final_list is on, but the results screen has no list of questions`);
    if (!item.showFinalList && list) problems.push(`${who}: show_final_list is off, but the results screen shows the list of questions`);
    if (!item.final) continue;
    if (texts.answers_available_later) {
      const shown = screen.includes(texts.answers_available_later);
      const expected = item.final.show_answers_on_completion && !item.final.all_completed;
      if (shown !== expected) {
        problems.push(
          `${who}: the note "${texts.answers_available_later}" ${shown ? 'shows' : 'is missing'}, but the server said ` +
            `show_answers_on_completion=${pyBool(item.final.show_answers_on_completion)}, all_completed=${pyBool(item.final.all_completed)}`,
        );
      }
    }
    if (list && texts.correct_header_alt) {
      const column = all.includes(texts.correct_header_alt);
      if (column !== item.final.has_correctness) {
        problems.push(
          `${who}: the "${texts.correct_header_alt}" column ${column ? 'shows' : 'is missing'}, but the reply ` +
            `${item.final.has_correctness ? 'has' : 'has no'} correct answers`,
        );
      }
    }
  }
  return problems;
}

/** A verify-user reply with final results, as the page got it. */
export interface FinalReply {
  student: string;
  user: string;
  t: number;
  all_completed: boolean;
  show_answers_on_completion: boolean;
  has_correctness: boolean;
  /** From the page's boot data. */
  show_right_answer: boolean;
}

/** Something that changes whether "everyone has finished": from live stats, and the teacher's button. */
export interface ClassEvent {
  t: number;
  kind: 'joined' | 'completed' | 'forced';
  user?: string;
}

/** Has every counted student finished by time t (or did the teacher show the answers)? */
export function classDone(events: ClassEvent[], t: number): boolean {
  if (events.some((event) => event.kind === 'forced' && event.t <= t)) return true;
  const joined = new Set(events.filter((e) => e.kind === 'joined' && e.t <= t).map((e) => e.user));
  if (joined.size === 0) return false;
  const completed = new Set(events.filter((e) => e.kind === 'completed' && e.t <= t).map((e) => e.user));
  return [...joined].every((user) => completed.has(user));
}

/**
 * Correct answers stay hidden until everyone finishes (Python 1.19's all_students_completed
 * and get_user_final_results): all_completed must match the class (students counted when
 * live stats showed them: approved ones in approval mode), and the reply carries the correct
 * answers only with show_right_answer, or with show_answers_on_completion once all_completed.
 * Replies close to a change (within the tolerance) are not judged.
 */
export function checkAnswersHidden(replies: FinalReply[], events: ClassEvent[], tolerance = 1.5): string[] {
  const problems: string[] = [];
  for (const reply of replies) {
    const who = `${reply.student} (user ${reply.user}) at ${fixed1(reply.t)} s`;
    const before = classDone(events, reply.t - tolerance);
    const after = classDone(events, reply.t + tolerance);
    if (before === after && reply.all_completed !== before) {
      problems.push(
        `${who}: the server said all_completed=${pyBool(reply.all_completed)}, but ` +
          (before ? 'every student had finished (or the teacher had shown the answers)' : 'not every student had finished'),
      );
    }
    const expected = reply.show_right_answer || (reply.show_answers_on_completion && reply.all_completed);
    if (reply.has_correctness !== expected) {
      problems.push(
        reply.has_correctness
          ? `${who}: the reply has the correct answers, but they must stay hidden until everyone finishes`
          : `${who}: the reply has no correct answers, but they should show now`,
      );
    }
  }
  return problems;
}

/** What a student typed in the registration form, and the fields the form had. */
export interface SentRegistration {
  student: string;
  user: string;
  sent: Record<string, string>;
  /** The form's extra fields (boot data): name = label in lower case with "_" for spaces. */
  fields: { name: string; label: string }[];
}

/** The field's key, as Python 1.19 makes it from the label. */
export function fieldName(label: string): string {
  return label.toLowerCase().replace(/ /g, '_');
}

/**
 * The users CSV keeps every registration field the student typed (one column per enabled
 * field), and a disabled field is neither on the form nor in the CSV.
 */
export function checkRegistrationFields(
  registrations: SentRegistration[],
  usersRows: Map<string, Record<string, string>>,
  disabled: string[] = [],
): string[] {
  const problems: string[] = [];
  for (const item of registrations) {
    const who = `${item.student} (user ${item.user})`;
    for (const label of disabled) {
      if (item.fields.some((field) => field.name === fieldName(label))) {
        problems.push(`${who}: the disabled field "${label}" is on the registration form`);
      }
    }
    const row = usersRows.get(item.user);
    if (!row) {
      problems.push(`${who}: the user is not in the users CSV`);
      continue;
    }
    for (const field of item.fields) {
      if (!(field.name in row)) {
        problems.push(`${who}: the users CSV has no column "${field.name}" for the field "${field.label}"`);
      } else if (row[field.name].trim() !== (item.sent[field.name] ?? '').trim()) {
        problems.push(`${who}: "${field.label}" was sent as '${item.sent[field.name] ?? ''}', the users CSV has '${row[field.name]}'`);
      }
    }
    for (const label of disabled) {
      if (fieldName(label) in row) problems.push(`${who}: the users CSV has a column for the disabled field "${label}"`);
    }
  }
  return problems;
}

/** One answer reply, with what the student saw afterwards. */
export interface AnswerReply {
  student: string;
  user: string;
  question: string;
  response: Record<string, unknown>;
  /** From the page's boot data. */
  showRightAnswer: boolean;
  /** The quiz has a whole-quiz time limit. */
  wholeQuiz: boolean;
  /** show_explanation of the run when known (start settings), else null. */
  showExplanation: boolean | null;
  /** The question has an explanation (from the live stats questions), when known. */
  hasExplanation: boolean | null;
  /** The student's screens after the reply, until the next question started. */
  laterScreens: string[];
  /** "📖 Пояснення" / "📖 Explanation". */
  explanationTitle: string;
}

/**
 * Explanations follow Python 1.19: only with show_right_answer and show_explanation, never
 * on a whole-quiz timeout; and a reply with an explanation leads to the explanation step.
 */
export function checkExplanations(replies: AnswerReply[]): string[] {
  const problems: string[] = [];
  for (const item of replies) {
    const who = `${item.student} (user ${item.user})`;
    const html = typeof item.response.explanation_html === 'string' && item.response.explanation_html !== '';
    const wholeQuizEnd = Boolean(item.response.timed_out) && item.wholeQuiz;
    if (html && !item.showRightAnswer) problems.push(`${who}: '${item.question}' came with an explanation, but correct answers are hidden`);
    if (html && item.showExplanation === false) problems.push(`${who}: '${item.question}' came with an explanation, but show_explanation is off`);
    if (html && wholeQuizEnd) problems.push(`${who}: '${item.question}' came with an explanation, but the whole-quiz time had run out`);
    if (
      !html &&
      !wholeQuizEnd &&
      item.showRightAnswer &&
      item.showExplanation === true &&
      item.hasExplanation === true
    ) {
      problems.push(`${who}: '${item.question}' has an explanation and show_explanation is on, but the reply had none`);
    }
    if (html && item.laterScreens.length && item.explanationTitle && !item.laterScreens.some((s) => s.includes(item.explanationTitle))) {
      problems.push(`${who}: '${item.question}' came with an explanation, but the screen never showed "${item.explanationTitle}"`);
    }
  }
  return problems;
}

/** Texts of the quiz page in Ukrainian and in English that the other language never shows. */
export const UK_PHRASES = [
  'Зареєструватися',
  'Відправити відповідь',
  'Продовжити',
  'Результат:',
  'Залишилось часу',
  'Час вичерпано',
  'Перевірити',
  'Очікування підтвердження',
  'Правильна відповідь',
  '📖 Пояснення',
];
export const EN_PHRASES = [
  'Register to start',
  'Submit answer',
  'Continue',
  'Result:',
  'Time left',
  'Time is up',
  'Waiting for approval',
  'Correct answer',
  '📖 Explanation',
];

/** A student's quiz page is in the language of the config (uk or en). */
export function checkLanguage(screens: { student: string; step: number; language: string; screen: string }[]): string[] {
  const problems: string[] = [];
  const reported = new Set<string>();
  for (const item of screens) {
    const foreign = item.language === 'en' ? UK_PHRASES : item.language === 'uk' ? EN_PHRASES : [];
    const found = foreign.filter((phrase) => item.screen.includes(phrase));
    if (!found.length) continue;
    const key = `${item.student}\u0000${found.join('|')}`;
    if (reported.has(key)) continue;
    reported.add(key);
    problems.push(`${item.student} step ${item.step}: the page should be in '${item.language}', but shows "${found.join('", "')}"`);
  }
  return problems;
}

/** A checker message the server sent, and the screens right after it. */
export interface CheckerReply {
  student: string;
  user: string;
  question: string;
  message: string;
  screens: string[];
}

/** A text answer's checker message is shown to the student. */
export function checkCheckerMessages(replies: CheckerReply[]): string[] {
  return replies
    .filter((item) => item.screens.length && !item.screens.some((screen) => screen.includes(item.message)))
    .map((item) => `${item.student} (user ${item.user}): the checker said "${item.message}" for '${item.question}', but the screen did not show it`);
}

/** In a random order, a question with stick_to_the_previous comes right after the one before it. */
export function checkStickyOrder(orders: { student: string; order: number[] }[], pairs: [number, number][]): string[] {
  const problems: string[] = [];
  for (const item of orders) {
    for (const [first, second] of pairs) {
      const at = item.order.indexOf(first);
      if (at === -1 || item.order[at + 1] !== second) {
        problems.push(`${item.student}: question ${second} sticks to question ${first}, but the order is ${item.order.join(', ')}`);
      }
    }
  }
  return problems;
}

/** A step on a phone where the page was wider than the screen. */
export interface WideStep {
  student: string;
  step: number;
  page: number;
  view: number;
  culprits: string[];
  screenshot: string | null;
}

/** On a phone the page must fit the screen: a sideways scroll bar is a problem. */
export function checkPhoneWidth(steps: WideStep[]): string[] {
  const byStudent = new Map<string, WideStep[]>();
  for (const step of steps) {
    const list = byStudent.get(step.student) ?? [];
    list.push(step);
    byStudent.set(step.student, list);
  }
  const problems: string[] = [];
  for (const [student, list] of byStudent) {
    const widest = list.reduce((a, b) => (b.page > a.page ? b : a));
    const shot = list.find((step) => step.screenshot)?.screenshot;
    const numbers = list.map((step) => step.step);
    const shown = numbers.length > 12 ? `${numbers.slice(0, 12).join(', ')} … (${numbers.length} steps)` : numbers.join(', ');
    problems.push(
      `${student}: the page is ${widest.page} px wide on a ${widest.view} px phone screen (a sideways scroll bar) at steps ${shown}` +
        (widest.culprits.length ? `; sticking out: ${widest.culprits.join(', ')}` : '') +
        (shot ? `; screenshot: ${shot}` : ''),
    );
  }
  return problems;
}

/** A NOTES line of an agent's report: its text and the step numbers it names. */
export interface AgentNote {
  steps: number[];
  text: string;
}

/** Read the NOTES section of an agent's report: one note per "- " line. */
export function parseNotes(text: string): AgentNote[] {
  const lines = text.split(/\r?\n/);
  const start = lines.findIndex((line) => /^\s*[#*]*\s*notes\s*[*]*\s*:?\s*$/i.test(line) || /^\s*notes\s*:/i.test(line));
  if (start === -1) return [];
  const notes: AgentNote[] = [];
  for (const line of lines.slice(start + 1)) {
    const match = /^\s*[-*•]\s+(.*)$/.exec(line);
    if (!match) continue;
    const steps: number[] = [];
    for (const found of match[1].matchAll(/\bsteps?\s+(\d+)(?:\s*(?:-|–|to|and|,)\s*(\d+))?/gi)) {
      steps.push(Number(found[1]));
      if (found[2]) steps.push(Number(found[2]));
    }
    notes.push({ steps, text: match[1].trim() });
  }
  return notes;
}

/** Notes name steps the driver ran; at most `max` notes. */
export function checkNotes(notes: AgentNote[], driverSteps: Set<number>, max = 8): string[] {
  const problems: string[] = [];
  if (notes.length > max) problems.push(`the report has ${notes.length} notes, the prompt asks for at most ${max}`);
  for (const note of notes) {
    if (!note.steps.length) problems.push(`a note names no step: "${note.text.slice(0, 80)}"`);
    for (const step of note.steps) {
      if (!driverSteps.has(step)) problems.push(`a note names step ${step}, which the driver never ran: "${note.text.slice(0, 80)}"`);
    }
  }
  return problems;
}

// ---- Loading a run ----

type Json = any; // eslint-disable-line @typescript-eslint/no-explicit-any

export function loadEvents(runDir: string): Json[] {
  return readFileSync(path.join(runDir, 'events.jsonl'), 'utf-8')
    .split('\n')
    .filter((line) => line.trim())
    .map((line) => JSON.parse(line));
}

/** Reads CSV text (quoted fields with commas, "" and line breaks) into rows of cells. */
export function parseCSV(text: string): string[][] {
  const rows: string[][] = [];
  let row: string[] = [];
  let cell = '';
  let quoted = false;
  for (let i = 0; i < text.length; i++) {
    const c = text[i];
    if (quoted) {
      if (c === '"') {
        if (text[i + 1] === '"') {
          cell += '"';
          i++;
        } else quoted = false;
      } else cell += c;
    } else if (c === '"') quoted = true;
    else if (c === ',') {
      row.push(cell);
      cell = '';
    } else if (c === '\n' || c === '\r') {
      if (c === '\r' && text[i + 1] === '\n') i++;
      row.push(cell);
      cell = '';
      rows.push(row);
      row = [];
    } else cell += c;
  }
  if (cell !== '' || row.length) {
    row.push(cell);
    rows.push(row);
  }
  return rows.filter((r) => !(r.length === 1 && r[0] === ''));
}

function csvFiles(runDir: string, test: (name: string) => boolean): string[] {
  const dataDir = path.join(runDir, 'data');
  return existsSync(dataDir) ? readdirSync(dataDir).filter(test).sort() : [];
}

function readTable(file: string): Record<string, string>[] {
  const [header, ...records] = parseCSV(readFileSync(file, 'utf-8').replace(/^﻿/, ''));
  if (!header) return [];
  return records.map((record) => {
    const item: Record<string, string> = {};
    header.forEach((column, index) => (item[column] = record[index] ?? ''));
    return item;
  });
}

export function loadAnswerRows(runDir: string): AnswerRow[] {
  const files = csvFiles(
    runDir,
    (name) => name.endsWith('.csv') && !name.endsWith('.users.csv') && !name.endsWith('.answers_with_users.csv'),
  );
  const rows: AnswerRow[] = [];
  for (const name of files) {
    for (const item of readTable(path.join(runDir, 'data', name))) {
      rows.push({
        user_id: item.user_id,
        question: item.question,
        selected_answer: item.selected_answer,
        is_correct: item.is_correct === 'True' || item.is_correct === 'true',
        time_taken: Number(item.time_taken_seconds || 0),
      });
    }
  }
  return rows;
}

/** user_id -> the user's row of the users CSV files (with the file name in "_file"). */
export function loadUsersRows(runDir: string): Map<string, Record<string, string>> {
  const users = new Map<string, Record<string, string>>();
  for (const name of csvFiles(runDir, (n) => n.endsWith('.users.csv'))) {
    for (const item of readTable(path.join(runDir, 'data', name))) users.set(item.user_id, { ...item, _file: name });
  }
  return users;
}

// ---- The record of a run, cut into quiz runs ----

/** One run of a quiz: from a start (or restart) to the next one. Users belong to the run they registered in. */
interface QuizRun {
  index: number;
  t: number;
  end: number;
  quiz: string | null;
  questions: Json[];
  text: Record<string, string>;
  users: string[];
  /** The start settings the switch-quiz reply reported, when there was one. */
  settings: Json | null;
}

interface Registration {
  student: string;
  user: string;
  t: number;
  body: Json;
  reply: Json;
  run: QuizRun;
  /** Until when this user is the student's user (the student's next registration). */
  until: number;
  boot: Json | null;
}

interface Participant {
  role: string;
  device: string;
}

/** The text of a question as the CSV stores it ("" for a question with only a picture). */
function questionTextMap(questions: Json[]): Record<string, string> {
  const text: Record<string, string> = {};
  (Array.isArray(questions) ? questions : []).forEach((question: Json, index: number) => {
    const id = question && question.id !== undefined ? question.id : index + 1;
    text[String(id)] = question && question.question !== undefined && question.question !== null ? String(question.question) : '';
  });
  return text;
}

function participantsOf(events: Json[]): Map<string, Participant> {
  const people = new Map<string, Participant>();
  for (const event of events) {
    if (event.kind === 'participant') people.set(event.student, { role: event.role, device: event.device });
  }
  // Runs recorded before participants were logged: read the "added" notes
  for (const event of events) {
    if (event.kind !== 'note' || !event.student || people.has(event.student)) continue;
    const match = /^(student|teacher|auto) added \((\w+)\)$/.exec(String(event.detail ?? ''));
    if (match) people.set(event.student, { role: match[1], device: match[2] });
  }
  return people;
}

/** Cut the run into quiz runs from the live stats messages (initial_state, then quiz_switched). */
function quizRuns(events: Json[], meta: Json): QuizRun[] {
  const runs: QuizRun[] = [];
  for (const event of events) {
    const message = event.kind === 'live' ? event.message : null;
    if (!message) continue;
    if ((message.type === 'initial_state' && runs.length === 0) || message.type === 'quiz_switched') {
      runs.push({
        index: runs.length + 1,
        t: message.type === 'initial_state' ? 0 : event.t,
        end: Infinity,
        quiz: message.current_quiz ?? null,
        questions: Array.isArray(message.questions) ? message.questions : [],
        text: questionTextMap(message.questions),
        users: [],
        settings: null,
      });
    }
  }
  if (runs.length === 0) {
    const text: Record<string, string> = {};
    for (const [key, value] of Object.entries(meta.question_text ?? {})) text[String(Number(key))] = String(value ?? '');
    runs.push({ index: 1, t: 0, end: Infinity, quiz: null, questions: [], text, users: [], settings: null });
  }
  runs.forEach((run, i) => (run.end = i + 1 < runs.length ? runs[i + 1].t : Infinity));
  // The settings of each start, from the teacher's switch-quiz reply (sent right after the switch)
  const requests = new Map<number, Json>();
  for (const event of events) if (event.kind === 'request') requests.set(event.req_id, event);
  for (const event of events) {
    if (event.kind !== 'response' || event.status !== 200) continue;
    const request = requests.get(event.req_id);
    if (!request || request.path !== '/api/admin/switch-quiz' || !event.body || !event.body.settings) continue;
    const run = [...runs].reverse().find((r) => r.t <= event.t + 0.5);
    if (run && !run.settings) run.settings = event.body.settings;
  }
  if (runs.length && meta.settings && runs.every((run) => !run.settings)) runs[runs.length - 1].settings = meta.settings;
  return runs;
}

function runAt(runs: QuizRun[], t: number): QuizRun {
  return [...runs].reverse().find((run) => run.t <= t) ?? runs[0];
}

/** Everything the checks need, read once from events.jsonl, the CSV files and the server log. */
class RunRecord {
  readonly requests = new Map<number, Json>();
  readonly responses = new Map<number, Json>();
  readonly people: Map<string, Participant>;
  readonly runs: QuizRun[];
  readonly registrations: Registration[] = [];
  readonly regOf = new Map<string, Registration>();
  readonly stepsOf = new Map<string, Json[]>();
  readonly bootsOf = new Map<string, Json[]>();

  constructor(
    readonly events: Json[],
    readonly meta: Json,
  ) {
    for (const event of events) {
      if (event.kind === 'request') this.requests.set(event.req_id, event);
      else if (event.kind === 'response') this.responses.set(event.req_id, event);
      else if (event.kind === 'step') {
        const list = this.stepsOf.get(event.student) ?? [];
        list.push(event);
        this.stepsOf.set(event.student, list);
      } else if (event.kind === 'boot') {
        const list = this.bootsOf.get(event.student) ?? [];
        list.push(event);
        this.bootsOf.set(event.student, list);
      }
    }
    this.people = participantsOf(events);
    this.runs = quizRuns(events, meta);
    for (const event of events) {
      if (event.kind !== 'response' || event.status !== 200) continue;
      const request = this.requests.get(event.req_id);
      if (!request || request.path !== '/api/register' || !event.body || !event.body.user_id) continue;
      const run = runAt(this.runs, request.t);
      const boots = (this.bootsOf.get(request.student) ?? []).filter((boot) => boot.t <= request.t);
      const registration: Registration = {
        student: request.student,
        user: String(event.body.user_id),
        t: request.t,
        body: request.body ?? {},
        reply: event.body,
        run,
        until: Infinity,
        boot: boots.length ? boots[boots.length - 1].boot : null,
      };
      // A repeated register request (same token) gives the same user: keep the first
      if (this.regOf.has(registration.user)) continue;
      this.registrations.push(registration);
      this.regOf.set(registration.user, registration);
      run.users.push(registration.user);
    }
    const byStudent = new Map<string, Registration[]>();
    for (const registration of this.registrations) {
      const list = byStudent.get(registration.student) ?? [];
      list.push(registration);
      byStudent.set(registration.student, list);
    }
    for (const list of byStudent.values()) list.forEach((reg, i) => (reg.until = i + 1 < list.length ? list[i + 1].t : Infinity));
  }

  textOf(user: unknown, questionId: unknown): string {
    const registration = this.regOf.get(String(user));
    const run = registration ? registration.run : this.runs[this.runs.length - 1];
    return run.text[String(questionId)] ?? `#${questionId}`;
  }

  /**
   * The steps a student took on the quiz page while this user was theirs. (A teacher may
   * register on the quiz page too; their steps on the admin pages are left out.)
   */
  stepsOfUser(registration: Registration): Json[] {
    const onQuizPage = (step: Json) => {
      if (!step.url) return true;
      try {
        return new URL(step.url).pathname === '/';
      } catch {
        return false;
      }
    };
    return (this.stepsOf.get(registration.student) ?? []).filter(
      (step) => (step.t_start ?? step.t) >= registration.t - 1 && step.t < registration.until && onQuizPage(step),
    );
  }

  /** API calls (request + reply) of one path start, in time order. */
  calls(prefix: string): { request: Json; response: Json | undefined }[] {
    const list: { request: Json; response: Json | undefined }[] = [];
    for (const request of this.requests.values()) {
      if (String(request.path).startsWith(prefix)) list.push({ request, response: this.responses.get(request.req_id) });
    }
    return list.sort((a, b) => a.request.t - b.request.t);
  }
}

// ---- The analysis ----

interface RunResult {
  run: QuizRun;
  sections: Record<string, string[]>;
  coverage: string[];
  rows: AnswerRow[];
  userOf: Record<string, string>;
}

/** Every check for the students of one quiz run. */
function checkRun(
  record: RunRecord,
  run: QuizRun,
  allRows: AnswerRow[],
  usersRows: Map<string, Record<string, string>>,
  serverLog: string,
  expectations: Record<string, Record<string, string>>,
): RunResult {
  const meta = record.meta;
  const users = new Set(run.users);
  const registrations = record.registrations.filter((reg) => reg.run === run);
  const rows = allRows.filter((row) => users.has(row.user_id));
  const textOf = (user: unknown, id: unknown) => record.textOf(user, id);
  const userOf: Record<string, string> = {};
  for (const reg of registrations) userOf[reg.student] = reg.user;

  // Submits with their replies
  const submits: Submit[] = [];
  const answerCalls = record.calls('/api/submit-answer').filter((call) => call.response && users.has(String(call.request.body?.user_id)));
  for (const { request, response } of answerCalls) {
    const body = request.body || {};
    submits.push({
      user_id: body.user_id,
      question: textOf(body.user_id, body.question_id),
      status: response.status,
      response: response.body && typeof response.body === 'object' ? response.body : {},
    });
  }

  // Browser-side time per answer: first question-start sent -> the accepted answer sent
  const firstStart = new Map<string, number>();
  const acceptedSend = new Map<string, number>();
  const latencies: number[] = [];
  for (const prefix of ['/api/question-start', '/api/submit-answer']) {
    for (const { request, response } of record.calls(prefix)) {
      if (!response || response.status !== 200) continue;
      const body = request.body && typeof request.body === 'object' ? request.body : {};
      if (!users.has(String(body.user_id))) continue;
      const key = pairKey(body.user_id, textOf(body.user_id, body.question_id));
      if (prefix === '/api/question-start') {
        if (!firstStart.has(key) || firstStart.get(key)! > request.t) firstStart.set(key, request.t);
      } else {
        if (!acceptedSend.has(key)) acceptedSend.set(key, request.t);
        latencies.push(response.t - request.t);
      }
    }
  }
  const measured = new Map<string, number>();
  for (const [key, sent] of acceptedSend) if (firstStart.has(key)) measured.set(key, sent - firstStart.get(key)!);

  // Rows the server stored by itself: the background check, and whole-quiz ends
  const allowed = new Set<string>();
  for (const match of serverLog.matchAll(/Time ran out for user (\d+) on question (\d+)/g)) {
    if (users.has(match[1])) allowed.add(pairKey(match[1], textOf(match[1], match[2])));
  }
  const ended = new Set([...serverLog.matchAll(/Whole-quiz time is up for user (\d+)/g)].map((m) => m[1]));
  for (const item of rows) if (ended.has(item.user_id) && item.selected_answer === '') allowed.add(pairKey(item.user_id, item.question));

  // The last screen of each user, and the results screens
  const finalScores: Record<string, [number, number]> = {};
  const resultScreens: ResultScreen[] = [];
  const verify = record.calls('/api/verify-user/');
  for (const reg of registrations) {
    const steps = record.stepsOfUser(reg);
    if (!steps.length) continue;
    const last = steps[steps.length - 1];
    const score = parseScreenScore(last.screen);
    if (!score) continue;
    finalScores[reg.user] = score;
    const boot = reg.boot;
    if (!boot) continue;
    const reply = verify
      .filter((call) => call.request.path === `/api/verify-user/${reg.user}` && call.response && call.request.t <= last.t + 0.5)
      .map((call) => call.response.body)
      .filter((body) => body && body.final_results)
      .pop();
    const final = reply ? reply.final_results : null;
    resultScreens.push({
      student: reg.student,
      user: reg.user,
      screen: String(last.screen ?? ''),
      texts: boot.texts ?? {},
      showFinalList: boot.show_final_list !== false,
      final: final
        ? {
            all_completed: Boolean(final.all_completed),
            show_answers_on_completion: Boolean(final.show_answers_on_completion),
            has_correctness: Array.isArray(final.test_results) && final.test_results.length > 0 && 'is_correct' in final.test_results[0],
          }
        : null,
    });
  }

  // Live stats: last state per user and question; who joined and finished when
  const finalStates = new Map<string, string>();
  const classEvents: ClassEvent[] = [];
  for (const event of record.events) {
    const message = event.kind === 'live' ? event.message : null;
    if (!message || event.t < run.t || event.t >= run.end) continue;
    if (!users.has(String(message.user_id))) continue;
    if (message.type === 'state_update' || message.type === 'user_registered') {
      finalStates.set(pairKey(message.user_id, textOf(message.user_id, message.question_id)), message.state);
    }
    if (message.type === 'user_registered') classEvents.push({ t: event.t, kind: 'joined', user: String(message.user_id) });
    if (message.type === 'state_update' && message.completed) classEvents.push({ t: event.t, kind: 'completed', user: String(message.user_id) });
  }
  for (const { request, response } of record.calls('/api/admin/force-show-answers')) {
    if (response && response.status === 200 && request.t >= run.t && request.t < run.end) classEvents.push({ t: response.t, kind: 'forced' });
  }
  const finalReplies: FinalReply[] = [];
  for (const { request, response } of verify) {
    const user = String(request.path).split('/').pop() ?? '';
    const reg = record.regOf.get(user);
    if (!reg || reg.run !== run || !response || response.status !== 200) continue;
    const final = response.body && response.body.final_results;
    if (!final) continue;
    finalReplies.push({
      student: reg.student,
      user,
      t: response.t,
      all_completed: Boolean(final.all_completed),
      show_answers_on_completion: Boolean(final.show_answers_on_completion),
      has_correctness: Array.isArray(final.test_results) && final.test_results.length > 0 && 'is_correct' in final.test_results[0],
      show_right_answer: reg.boot ? reg.boot.show_right_answer !== false : true,
    });
  }

  // Time limits: the page's own (boot data), else the scenario's
  const limits: Record<string, number> = {};
  const someBoot = registrations.find((reg) => reg.boot)?.boot;
  if (someBoot && Array.isArray(someBoot.questions)) {
    for (const question of someBoot.questions) limits[run.text[String(question.id)] ?? `#${question.id}`] = Number(question.time_limit) || 0;
  } else {
    for (const [qid, limit] of Object.entries(meta.limits ?? {})) limits[run.text[String(Number(qid))] ?? `#${qid}`] = limit as number;
  }

  // Registration fields
  const sent: SentRegistration[] = registrations
    .filter((reg) => reg.boot && Array.isArray(reg.boot.registration_fields))
    .map((reg) => {
      const values: Record<string, string> = {};
      for (const [key, value] of Object.entries(reg.body ?? {})) {
        if (key !== 'username' && key !== 'registration_token') values[key] = String(value ?? '');
      }
      return {
        student: reg.student,
        user: reg.user,
        sent: values,
        fields: reg.boot.registration_fields.map((field: Json) => ({ name: String(field.name), label: String(field.label) })),
      };
    });

  // Explanations and checker messages, with what the student saw afterwards
  const explanationFor = new Map<string, boolean>();
  for (const question of run.questions) {
    explanationFor.set(String(question.id), typeof question.explanation === 'string' && question.explanation.trim() !== '');
  }
  const answerReplies: AnswerReply[] = [];
  const checkerReplies: CheckerReply[] = [];
  for (const { request, response } of answerCalls) {
    if (response.status !== 200 || !response.body || typeof response.body !== 'object') continue;
    const body = request.body || {};
    const reg = record.regOf.get(String(body.user_id));
    if (!reg || !reg.boot) continue;
    const steps = record.stepsOfUser(reg);
    const nextStart = record
      .calls('/api/question-start')
      .find((call) => call.request.t > request.t && String(call.request.body?.user_id) === reg.user);
    const later = steps.filter((step) => step.t >= response.t && (!nextStart || (step.t_start ?? step.t) <= nextStart.request.t));
    const question = textOf(body.user_id, body.question_id);
    answerReplies.push({
      student: reg.student,
      user: reg.user,
      question,
      response: response.body,
      showRightAnswer: reg.boot.show_right_answer !== false,
      wholeQuiz: Number(reg.boot.quiz_time_limit) > 0,
      showExplanation: run.settings && typeof run.settings.show_explanation === 'boolean' ? run.settings.show_explanation : null,
      hasExplanation: explanationFor.has(String(body.question_id)) ? explanationFor.get(String(body.question_id))! : null,
      laterScreens: later.map((step) => String(step.screen ?? '')),
      explanationTitle: reg.boot.texts?.explanation_title ?? '',
    });
    if (typeof response.body.checker_error === 'string' && response.body.checker_error && response.body.is_correct === false) {
      checkerReplies.push({
        student: reg.student,
        user: reg.user,
        question,
        message: response.body.checker_error,
        screens: steps.filter((step) => step.t >= response.t).slice(0, 2).map((step) => String(step.screen ?? '')),
      });
    }
  }

  // The language of the quiz page
  const languageScreens: { student: string; step: number; language: string; screen: string }[] = [];
  for (const reg of registrations) {
    if (!reg.boot || !reg.boot.language) continue;
    for (const step of record.stepsOfUser(reg)) {
      languageScreens.push({ student: reg.student, step: step.step, language: reg.boot.language, screen: String(step.screen ?? '') });
    }
  }

  // Random order with questions that stick together
  const pairs: [number, number][] = [];
  run.questions.forEach((question: Json, index: number) => {
    if (question && question.stick_to_the_previous && index > 0) pairs.push([run.questions[index - 1].id ?? index, question.id ?? index + 1]);
  });
  const orders = registrations
    .filter((reg) => Array.isArray(reg.reply.question_order))
    .map((reg) => ({ student: reg.student, order: reg.reply.question_order as number[] }));

  const sections: Record<string, string[]> = {
    'Each question stored once per student': checkUniqueAnswers(rows),
    'Empty (timed-out) answers are failed': checkTimeoutRows(rows),
    'Answers arrived within limit + grace': checkAnswerTimes(rows, limits, meta.grace ?? 5),
    'Requests and stored answers agree': checkSubmitsMatchRows(submits, rows, allowed),
    'Results screen matches stored answers': checkScreenScores(finalScores, rows, Object.keys(run.text).length),
    'Live stats match stored answers': checkLiveStats(finalStates, rows),
    'Server time per answer matches the browser (±1.5 s)': checkTimeAccuracy(rows, measured),
    'Planned outcomes of the scripted edge cases': checkExpectations(expectations, rows, userOf, run.text),
    'Results screen follows the settings (score, list, "answers later", correct column)': checkResultsScreens(resultScreens),
    'Correct answers stay hidden until everyone finishes': checkAnswersHidden(finalReplies, classEvents),
    'Registration fields are stored (users CSV)': checkRegistrationFields(sent, usersRows, meta.disabled_fields ?? []),
    'Explanations follow the settings and are shown': checkExplanations(answerReplies),
    'Checker messages are shown': checkCheckerMessages(checkerReplies),
    'The quiz page is in the configured language': checkLanguage(languageScreens),
  };
  if (pairs.length && orders.length) sections['Questions that stick together stay together'] = checkStickyOrder(orders, pairs);

  const coverage = [
    `- Results screens read: ${Object.keys(finalScores).length} of ${registrations.length} students`,
    `- Answer requests with a reply: ${submits.length}`,
    `- Results screens checked against the settings: ${resultScreens.length}; final-results replies checked: ${finalReplies.length}`,
  ];
  if (latencies.length) {
    const ordered = [...latencies].sort((a, b) => a - b);
    coverage.push(
      `- Answer reply time: median ${ordered[Math.floor(ordered.length / 2)].toFixed(2)} s, ` +
        `slowest ${ordered[ordered.length - 1].toFixed(2)} s (from sending the accepted answer to its reply)`,
    );
  }
  return { run, sections, coverage, rows, userOf };
}

/** Run every check on a finished run and write report.md. Returns the number of problems found. */
export async function analyze(runDirArg: string): Promise<number> {
  const runDir = path.resolve(runDirArg);
  const meta: Json = JSON.parse(readFileSync(path.join(runDir, 'meta.json'), 'utf-8'));
  if (!('expectations' in meta) && meta.kind !== 'teacher') {
    // runs recorded before scenarios had expectations
    const { SCENARIOS } = await import('./scenarios');
    meta.expectations = SCENARIOS[meta.scenario]?.expectations ?? {};
  }
  if (meta.kind !== 'teacher' && meta.disabled_fields === undefined) {
    const { SCENARIOS } = await import('./scenarios');
    const registration = SCENARIOS[meta.scenario]?.config?.registration as { disabled_fields?: string[] } | undefined;
    meta.disabled_fields = registration?.disabled_fields ?? [];
  }
  const events = loadEvents(runDir);
  const rows = loadAnswerRows(runDir);
  const usersRows = loadUsersRows(runDir);
  const serverLog = existsSync(path.join(runDir, 'server.log')) ? readFileSync(path.join(runDir, 'server.log'), 'utf-8') : '';
  const record = new RunRecord(events, meta);

  // The scripted students' planned outcomes belong to the run they took part in
  const expectations: Record<string, Record<string, string>> = meta.kind === 'teacher' ? {} : meta.expectations ?? {};
  const scripted = new Set(Object.keys(meta.scripted ?? expectations));
  const withStudents = record.runs.filter((run) => run.users.length > 0);
  const count = (run: QuizRun) => record.registrations.filter((reg) => reg.run === run && scripted.has(reg.student)).length;
  const mainRun = withStudents.reduce<QuizRun | null>((best, run) => (!best || count(run) > count(best) ? run : best), null);
  const results = withStudents.map((run) =>
    checkRun(record, run, rows, usersRows, serverLog, run === mainRun ? expectations : {}),
  );

  // Phone screens of everyone (students, automatic students, teachers)
  const wide: WideStep[] = events
    .filter((event) => event.kind === 'step' && event.wide)
    .map((event) => ({
      student: event.student,
      step: event.step,
      page: Number(event.wide.page),
      view: Number(event.wide.view),
      culprits: Array.isArray(event.wide.culprits) ? event.wide.culprits : [],
      screenshot: event.wide.screenshot ?? event.screenshot ?? null,
    }));
  const phoneProblems = checkPhoneWidth(wide);

  // Agent reports against the driver's record (students and teachers)
  const agentSections: Record<string, { count: number; problems: string[]; notes: AgentNote[]; noteProblems: string[]; role: string }> = {};
  const agentsDir = path.join(runDir, 'agents');
  const reports = existsSync(agentsDir) ? readdirSync(agentsDir).filter((n) => n.endsWith('.md')).sort() : [];
  for (const file of reports) {
    const name = file.slice(0, -3);
    const text = readFileSync(path.join(agentsDir, file), 'utf-8');
    const role = record.people.get(name)?.role ?? (meta.teachers && name in meta.teachers ? 'teacher' : 'student');
    const driverSteps = new Map<number, DriverStep>();
    for (const step of record.stepsOf.get(name) ?? []) {
      // The agent may quote anything the tool printed for the step, not only the page text
      const shown = step.role === 'teacher' ? formatTeacherStep(step as TeacherStepRecord) : formatStep(step as StepRecord);
      driverSteps.set(step.step, { cmd: step.cmd, arg: step.arg, args: step.role === 'teacher' ? step.args : undefined, screen: shown });
    }
    const agentSteps = parseAgentReport(text);
    const problems = checkAgentAlignment(agentSteps, driverSteps);
    if (!agentSteps.length) problems.push("the report has no 'STEP n | DID: ... | SAW: ...' lines");
    const notes = parseNotes(text);
    const noteProblems = checkNotes(notes, new Set(driverSteps.keys()), role === 'teacher' ? 8 : 5);
    agentSections[name] = { count: agentSteps.length, problems, notes, noteProblems, role };
  }

  writeReport(runDir, meta, record, results, phoneProblems, agentSections, rows);
  writeTimelines(runDir, record, rows);
  const dataProblems = results.reduce(
    (sum, result) => sum + Object.values(result.sections).reduce((n, problems) => n + problems.length, 0),
    0,
  );
  return dataProblems + phoneProblems.length;
}

function mark(problems: string[]): string {
  return problems.length === 0 ? 'PASS' : `FAIL (${problems.length})`;
}

/** Admin requests worth listing in a teacher's summary (polling and page loads are left out). */
const QUIET_PATHS = [
  '/api/admin/check-session',
  '/api/admin/version-check',
  '/api/admin/transfer',
  '/api/admin/list-quizzes',
  '/api/admin/list-checker-templates',
  '/api/files/list',
  '/api/verify-user/',
];

function teacherSummary(record: RunRecord, name: string): string[] {
  const steps = record.stepsOf.get(name) ?? [];
  const failed = steps.filter((step) => step.error);
  const dialogs = steps.flatMap((step) => (step.events ?? []).filter((line: string) => line.startsWith('DIALOG')));
  const downloads = steps.flatMap((step) => step.downloads ?? []);
  const tabs = steps.filter((step) => (step.events ?? []).some((line: string) => line.startsWith('NEW TAB')));
  const person = record.people.get(name);
  const lines = [
    `- **${name}** (${person?.device ?? '?'}): ${steps.length} steps, ${failed.length} with an error, ${dialogs.length} dialogs, ` +
      `${downloads.length} downloads, ${tabs.length} new tabs`,
  ];
  for (const step of failed) lines.push(`  - step ${step.step} \`${step.cmd} ${(step.args ?? []).join(' ')}\`: ${step.error}`);
  for (const download of downloads) lines.push(`  - downloaded ${download.name} (${download.size} bytes) → \`${download.saved_as}\``);
  const calls = [...record.requests.values()]
    .filter((request) => request.student === name && !QUIET_PATHS.some((quiet) => String(request.path).startsWith(quiet)))
    .filter((request) => request.method !== 'GET' || String(request.path).startsWith('/api/files/') || String(request.path).startsWith('/api/admin/quiz/'))
    .sort((a, b) => a.t - b.t);
  if (calls.length) {
    lines.push('  - What the server was asked to do (admin requests, in order):');
    for (const request of calls) {
      const response = record.responses.get(request.req_id);
      const status = response ? response.status : 'no reply';
      let extra = '';
      const body = response?.body;
      if (body && typeof body === 'object') {
        if (body.error) extra = ` error: ${String(body.error).slice(0, 120)}`;
        else if (body.message) extra = ` ${String(body.message).slice(0, 120)}`;
        else if (body.settings) extra = ` settings ${JSON.stringify(body.settings)}`;
      }
      lines.push(`    - ${request.t.toFixed(1)} s ${request.method} ${request.path} → ${status}${extra}`);
    }
  }
  return lines;
}

function writeReport(
  runDir: string,
  meta: Json,
  record: RunRecord,
  results: RunResult[],
  phoneProblems: string[],
  agentSections: Record<string, { count: number; problems: string[]; notes: AgentNote[]; noteProblems: string[]; role: string }>,
  rows: AnswerRow[],
): void {
  const lines = [`# Harness run: ${meta.scenario}`, '', meta.description ?? '', ''];
  const students = record.registrations.map((reg) => reg.student);
  const devices = (names: string[]) =>
    names.map((name) => `${name}${record.people.get(name)?.device === 'phone' ? ' 📱' : ''}`).sort().join(', ');
  lines.push(`- Students: ${new Set(students).size} registered (${devices([...new Set(students)])}); 📱 = phone`);
  lines.push(`- Stored answers: ${rows.length}, of them timed out (empty): ${rows.filter((r) => r.selected_answer === '').length}`);
  if (meta.settings) lines.push(`- Settings used: \`${JSON.stringify(meta.settings ?? null)}\``);
  lines.push(`- Live stats messages recorded: ${record.events.filter((e) => e.kind === 'live').length}`);
  const autos = [...record.people.entries()].filter(([, person]) => person.role === 'auto').map(([name]) => name);
  if (autos.length) {
    const finished = autos.filter((name) =>
      (record.stepsOf.get(name) ?? []).some((step) => parseScreenScore(step.screen) !== null),
    );
    lines.push(
      `- Automatic students: ${autos.length} started (${devices(autos)}), ${autos.filter((n) => students.includes(n)).length} registered, ` +
        `${finished.length} reached the results`,
    );
  }

  if (meta.kind === 'teacher') {
    lines.push('');
    lines.push('## The teacher');
    lines.push('');
    for (const name of Object.keys(meta.teachers ?? {})) lines.push(...teacherSummary(record, name));
    lines.push('');
    lines.push('## Quiz runs');
    lines.push('');
    lines.push('Every start of a quiz (and every restart, for example after a config save) begins a new run with new CSV files.');
    lines.push('');
    for (const run of record.runs) {
      const settings = run.settings ? ` — settings \`${JSON.stringify(run.settings)}\`` : '';
      lines.push(`- Run ${run.index}: ${run.quiz ?? '(no quiz)'} from ${run.t.toFixed(1)} s, ${run.users.length} students${settings}`);
    }
  }

  for (const result of results) {
    lines.push('');
    if (meta.kind === 'teacher' || results.length > 1) {
      lines.push(`## Checks for run ${result.run.index} (${result.run.quiz ?? 'no quiz'}): browser record vs server data`);
      lines.push('');
      lines.push(`- Students: ${devices(Object.keys(result.userOf))}`);
    } else {
      lines.push('## Checks: browser record vs server data');
      lines.push('');
    }
    lines.push(...result.coverage);
    lines.push('');
    for (const [title, problems] of Object.entries(result.sections)) {
      lines.push(`- **${mark(problems)}** ${title}`);
      for (const problem of problems) lines.push(`  - ${problem}`);
    }
  }
  if (!results.length) {
    lines.push('');
    lines.push('## Checks: browser record vs server data');
    lines.push('');
    lines.push('No student registered, so there is no data to check.');
  }
  lines.push('');
  lines.push('## Phone screens');
  lines.push('');
  lines.push(`- **${mark(phoneProblems)}** Phone screens have no sideways scroll bar (the page is not wider than the screen)`);
  for (const problem of phoneProblems) lines.push(`  - ${problem}`);

  lines.push('');
  lines.push('## Agent reports vs driver record');
  lines.push('');
  if (Object.keys(agentSections).length === 0) lines.push('No agent reports in this run.');
  for (const [name, section] of Object.entries(agentSections)) {
    const state = section.problems.length === 0 ? 'aligned' : `${section.problems.length} difference(s)`;
    lines.push(`- **${name}** (${section.role}): ${section.count} reported steps, ${state}`);
    for (const problem of section.problems) lines.push(`  - ${problem}`);
    if (section.notes.length) {
      lines.push(`  - Notes from the report (${section.notes.length}):`);
      for (const note of section.notes) lines.push(`    - ${note.text}`);
    }
    for (const problem of section.noteProblems) lines.push(`  - ${problem}`);
  }
  lines.push('');
  lines.push('Timelines: `timelines/<name>.md` (students and teachers). Screenshots: `shots/`. Downloads: `downloads/`.');
  writeFileSync(path.join(runDir, 'report.md'), lines.join('\n') + '\n', 'utf-8');
}

const TIMELINE_PATHS = ['/api/submit-answer', '/api/question-start', '/api/register'];

/** One file per participant: steps, requests and the stored rows, in time order. */
function writeTimelines(runDir: string, record: RunRecord, rows: AnswerRow[]): void {
  const out = path.join(runDir, 'timelines');
  mkdirSync(out, { recursive: true });
  const perStudent = new Map<string, [number, string][]>();
  const add = (student: string, at: number, line: string) => {
    const list = perStudent.get(student) ?? [];
    list.push([at, line]);
    perStudent.set(student, list);
  };
  const pad = (value: number) => value.toFixed(1).padStart(7);
  const flat = (text: unknown, size = 300) =>
    String(text ?? '')
      .split('\n')
      .filter((line) => line.trim())
      .join(' / ')
      .slice(0, size);
  for (const event of record.events) {
    const student = event.student;
    if (!student) continue;
    const teacher = record.people.get(student)?.role === 'teacher';
    if (event.kind === 'step') {
      const arg = event.arg !== undefined && event.arg !== null ? ` ${event.arg}` : '';
      const error = event.error ? ` ERROR: ${event.error}` : '';
      const start = event.t_start ?? event.t;
      const lines = [`${pad(start)}s  STEP ${event.step}: ${event.cmd}${arg}${error}  (done at ${event.t.toFixed(1)}s)`];
      if (teacher) {
        lines.push(`          result: ${event.result ?? ''}`);
        for (const note of event.events ?? []) lines.push(`          ${note}`);
        if (event.url) lines.push(`          page: ${event.url}  (${(event.elements ?? []).length} elements listed)`);
      }
      lines.push(`          screen: ${flat(event.screen, teacher ? 400 : 300)}`);
      if (event.wide) lines.push(`          PAGE WIDER THAN THE PHONE: ${event.wide.page} px on ${event.wide.view} px (${event.wide.screenshot ?? 'no screenshot'})`);
      add(student, start, lines.join('\n'));
    } else if (event.kind === 'response') {
      const request = record.requests.get(event.req_id) ?? {};
      if (TIMELINE_PATHS.includes(request.path) || (teacher && !QUIET_PATHS.some((quiet) => String(request.path).startsWith(quiet)))) {
        add(
          student,
          request.t,
          `${pad(request.t)}s  ${request.method} ${request.path} ${JSON.stringify(request.body ?? null).slice(0, 200)}` +
            ` -> ${event.status} at ${event.t.toFixed(1)}s ${JSON.stringify(event.body ?? null).slice(0, 200)}`,
        );
      }
    } else if (['request_failed', 'network', 'note'].includes(event.kind)) {
      add(student, event.t, `${pad(event.t)}s  ${String(event.kind).toUpperCase()}: ${event.detail ?? ''}`);
    }
  }
  for (const [student, entries] of perStudent) {
    const person = record.people.get(student);
    const users = record.registrations.filter((reg) => reg.student === student);
    const ids = users.length ? users.map((reg) => `${reg.user} in run ${reg.run.index}`).join(', ') : 'None';
    const ordered = [...entries].sort((a, b) => a[0] - b[0]).map(([, line]) => line);
    const heading = `# ${student} (${person ? `${person.role}, ${person.device}` : 'device unknown'}; user_id ${ids})`;
    const text = [heading, '', '```', ...ordered, '```', ''];
    if (users.length) {
      text.push('## Stored answers', '');
      for (const reg of users) {
        for (const item of rows.filter((r) => r.user_id === reg.user)) {
          text.push(
            `- ${item.question} → '${item.selected_answer}', correct=${pyBool(item.is_correct)}, time=${fixed1(item.time_taken)}s`,
          );
        }
      }
    }
    writeFileSync(path.join(out, `${student}.md`), text.join('\n') + '\n', 'utf-8');
  }
}

const isMain = process.argv[1] !== undefined && import.meta.url === pathToFileURL(path.resolve(process.argv[1])).href;
if (isMain) {
  if (process.argv.length !== 3) {
    console.log('Usage: npx tsx harness/checks.ts <run_dir>');
    process.exit(2);
  }
  const found = await analyze(process.argv[2]);
  console.log(`${found} problem(s) found. Report: ${path.join(path.resolve(process.argv[2]), 'report.md')}`);
}
