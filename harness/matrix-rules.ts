// What WebQuiz 1.19 shows a student, written as small pure functions of a case.
//
// Each rule names the Python 1.19 code it comes from (file:line in the
// oduvan/webquiz repository at v1.19; templates/index.html is the quiz page).
// The check functions at the end compare a rule's expected value with what the
// page showed (a Snap, read by matrix-page.ts) and return CheckResults. Nothing
// here needs a browser or a server, so matrix.test.ts tests it directly.

import { DISABLED_FIELDS, FIELDS, USERNAME_LABEL, type MatrixCase } from './matrix-cases';
import {
  PER_QUESTION_SECONDS,
  QUESTIONS,
  QUIZ_TITLE,
  WHOLE_QUIZ_SECONDS,
  type Answer,
  type MatrixQuestion,
  type QuizSettings,
} from './matrix-quiz';
import { TEXTS, type StudentTexts } from './matrix-texts';

// ---- What the page reader returns (plain data) ----

export interface ImageSnap {
  src: string;
  loaded: boolean;
  width?: number; // shown width in CSS pixels
  natural?: number; // the file's own width
}

export interface OptionSnap {
  text: string;
  classes: string[];
  /** The feedback label the option shows ("✓ Правильна відповідь"), '' when none. */
  label: string;
  image: ImageSnap | null;
}

export interface QuestionSnap {
  text: string;
  badge: string | null;
  timer: { visible: boolean; text: string; label: string; warning: boolean } | null; // null = no timer box
  userSelect: string;
  hint: string | null;
  options: OptionSnap[];
  textInput: { label: string; value: string; placeholder: string; disabled: boolean; classes: string[]; userSelect: string } | null;
  checkerError: string | null;
  correctValue: string | null;
  image: ImageSnap | null;
  file: { name: string; href: string; download: boolean } | null;
  timeUp: string | null;
}

export interface ButtonSnap {
  visible: boolean;
  disabled: boolean;
  text: string;
}

export interface ResultCellSnap {
  text: string; // the whole cell, spaces collapsed
  firstLine: string;
  /** The answer span: its parts (one per answer) and images; null when the cell has none. */
  span: { parts: string[]; images: string[]; classes: string[] } | null;
  timeUp: string | null;
  hint: { text: string; images: string[] } | null;
  images: string[];
  fileLink: string | null;
}

export interface ResultsSnap {
  paragraphs: string[];
  text: string;
  buttons: string[];
  table: { headers: string[]; rows: ResultCellSnap[][] } | null;
}

export interface Snap {
  visible: { loading: boolean; registration: boolean; waiting: boolean; test: boolean; results: boolean; explanation: boolean; question: boolean };
  title: string;
  docTitle: string;
  pageText: string;
  dark: boolean;
  overflowX: number;
  viewportWidth?: number;
  userInfo: string | null;
  registration: { heading: string; labels: string[]; inputs: number; button: string };
  waiting: { heading: string; text: string; labels: string[]; values: string[]; buttons: string[]; message: string | null };
  progress: string;
  progressWidth: number | null;
  quizTimer: { visible: boolean; text: string; label: string; warning: boolean };
  question: QuestionSnap | null;
  submit: ButtonSnap;
  cont: ButtonSnap;
  explanation: {
    title: string;
    question: string;
    strong: string[];
    listItems: number;
    links: { text: string; href: string; target: string; rel: string }[];
    next: ButtonSnap;
  };
  results: ResultsSnap | null;
}

// ---- Check results ----

export interface CheckResult {
  name: string;
  pass: boolean;
  expected: string;
  actual: string;
  /** Where the rule comes from in Python 1.19. */
  rule: string;
}

export function show(value: unknown): string {
  if (value === null || value === undefined) return '(none)';
  if (typeof value === 'string') return value === '' ? '(empty)' : value;
  return JSON.stringify(value);
}

/** JSON with the keys of every object sorted, so key order does not matter. */
export function canonical(value: unknown): string {
  return JSON.stringify(value ?? null, (_key, item) =>
    item && typeof item === 'object' && !Array.isArray(item)
      ? Object.fromEntries(Object.keys(item).sort().map((key) => [key, item[key]]))
      : item,
  );
}

/** Pass when expected and actual are equal (compared as JSON, keys in any order). */
export function same(name: string, rule: string, expected: unknown, actual: unknown): CheckResult {
  const pass = canonical(expected) === canonical(actual);
  return { name, pass, expected: show(expected), actual: show(actual), rule };
}

export function truth(name: string, rule: string, pass: boolean, expected: string, actual: string): CheckResult {
  return { name, pass, expected, actual, rule };
}

/** Spaces and line breaks collapsed, ends trimmed. */
export function norm(text: string | null | undefined): string {
  return (text ?? '').replace(/\s+/g, ' ').trim();
}

// ---- Rules: texts and registration ----

/** The quiz page uses the texts of the configured language (server.py:1346-1347, translations.py:197-207). */
export function textsFor(matrixCase: MatrixCase): StudentTexts {
  return TEXTS[matrixCase.language];
}

/** The username label: the config's label, else the translated one (server.py:1306-1310). */
export function usernameLabel(matrixCase: MatrixCase): string {
  return matrixCase.fields ? USERNAME_LABEL : textsFor(matrixCase).username_label;
}

/**
 * The labels of the registration form: the username, then each of registration.fields, each
 * followed by ':' (server.py:1312-1331). disabled_fields is never read there, so those stay hidden.
 */
export function registrationLabels(matrixCase: MatrixCase): string[] {
  const fields = matrixCase.fields ? FIELDS : [];
  return [usernameLabel(matrixCase) + ':', ...fields.map((field) => field + ':')];
}

/** Field labels that must not appear on the form (config.py RegistrationConfig.disabled_fields). */
export function hiddenFieldLabels(matrixCase: MatrixCase): string[] {
  return matrixCase.fields ? DISABLED_FIELDS : [];
}

/** The form field name of a label: lower case, spaces to "_" (server.py:1324, 1968). */
export function fieldName(label: string): string {
  return label.toLowerCase().replaceAll(' ', '_');
}

/** A new student waits for approval only with registration.approve (server.py:1954, 1961; index.html:1552). */
export function waitsForApproval(matrixCase: MatrixCase): boolean {
  return matrixCase.approve;
}

// ---- Rules: questions ----

/**
 * The time limit of one question in seconds, 0 = none (time_limits.py:92-105; server.py:1293-1294).
 * Only per_question mode gives questions a limit; a question's own time_limit is used only there.
 */
export function effectiveLimit(settings: QuizSettings, question: MatrixQuestion): number {
  if (settings.time_limit !== 'per_question') return 0;
  const own = question.time_limit;
  return Number.isInteger(own) && (own as number) >= 0 ? (own as number) : PER_QUESTION_SECONDS;
}

/** The whole-quiz countdown shows only in whole_quiz mode (server.py:2563-2567, 2587-2590; index.html:1130-1131, 1170-1177). */
export function quizTimerShown(settings: QuizSettings): boolean {
  return settings.time_limit === 'whole_quiz';
}

/** Points shown with at most 2 decimals and no trailing zeros (index.html:994-997). */
export function formatPoints(value: number): string {
  return String(Math.round((Number(value) || 0) * 100) / 100);
}

/** Python's round(): halves go to the even neighbour. */
export function pyRound(value: number): number {
  const floor = Math.floor(value);
  const diff = value - floor;
  if (Math.abs(diff - 0.5) < 1e-9) return floor % 2 === 0 ? floor : floor + 1;
  return Math.round(value);
}

/** Round to 2 decimals, as normalize_points does (server.py:144-161). */
export function normalizePoints(value: number): number {
  return Math.round(value * 100) / 100;
}

export function pointsOf(question: MatrixQuestion): number {
  return question.points ?? 1; // server.py:2322: default 1
}

/** The trophy badge: only for points other than 1 (index.html:1815). */
export function pointsBadge(question: MatrixQuestion, T: StudentTexts): string | null {
  const points = pointsOf(question);
  if (points === 1) return null;
  const word = Number.isInteger(points) && points >= 2 && points <= 4 ? T.points_2_4 : T.points_other;
  return `🏆 ${formatPoints(points)} ${word}`;
}

/** "Питання 3 з 10" (index.html:1767). */
export function progressText(T: StudentTexts, position: number, total: number): string {
  return T.question_n_of_m.replace('{n}', String(position)).replace('{m}', String(total));
}

export function isText(question: MatrixQuestion): boolean {
  return question.checker !== undefined; // server.py:1258: a checker field makes a text question
}

/** Several answers can be picked when correct_answer is a list (server.py:1273; index.html:1788). */
export function isMultiple(question: MatrixQuestion): boolean {
  return Array.isArray(question.correct_answer);
}

function indicesOf(answer: Answer): number[] {
  return 'indices' in answer ? answer.indices : [];
}

function textOf(answer: Answer): string {
  return 'text' in answer ? answer.text : '';
}

/**
 * Is the answer right? (server.py:1680-1729 for choices; 1731-1802 for text.)
 * Several answers: no wrong option, and at least min_correct right ones (default: all of them).
 * Text: the checker passes; without checker code, the text equals correct_value after strip().
 */
export function judge(question: MatrixQuestion, answer: Answer): { correct: boolean; checkerError: string | null } {
  if (isText(question)) {
    if (!question.checker) {
      return { correct: textOf(answer).trim() === (question.correct_value ?? '').trim(), checkerError: null };
    }
    const error = question.runChecker ? question.runChecker(textOf(answer)) : null;
    return { correct: error === null, checkerError: error };
  }
  const selected = indicesOf(answer);
  const right = question.correct_answer;
  if (typeof right === 'number') return { correct: selected.length === 1 && selected[0] === right, checkerError: null };
  const rightSet = new Set(right ?? []);
  if (selected.some((index) => !rightSet.has(index))) return { correct: false, checkerError: null };
  const minCorrect = question.min_correct ?? rightSet.size;
  return { correct: new Set(selected).size >= minCorrect, checkerError: null };
}

/** The answer as the CSV and the results show it: option texts joined by "|" in index order (server.py:1804-1842). */
export function answerText(question: MatrixQuestion, answer: Answer): string {
  if (isText(question)) return textOf(answer);
  return [...indicesOf(answer)]
    .sort((a, b) => a - b)
    .map((index) => question.options![index])
    .join('|');
}

/** The right answer as stored: correct_value for text, else the option texts (server.py:2282-2285). */
export function correctAnswerText(question: MatrixQuestion): string {
  if (isText(question)) return question.correct_value ?? '';
  const right = question.correct_answer!;
  return answerText(question, { indices: typeof right === 'number' ? [right] : right });
}

export type FeedbackClass = 'feedback-correct' | 'feedback-incorrect' | 'feedback-missed';

/**
 * The feedback class of each option after an answer, null = none (index.html:2118-2158).
 * Several answers: picked right -> correct, picked wrong -> incorrect, right but not picked -> missed.
 * One answer: the picked option is correct or incorrect; after a wrong answer (or no answer,
 * when the time ran out) the right option is marked correct.
 */
export function feedbackClasses(question: MatrixQuestion, answer: Answer | null): (FeedbackClass | null)[] {
  const options = question.options ?? [];
  const classes: (FeedbackClass | null)[] = options.map(() => null);
  const selected = answer ? indicesOf(answer) : [];
  const right = question.correct_answer;
  if (Array.isArray(right)) {
    for (const index of selected) classes[index] = right.includes(index) ? 'feedback-correct' : 'feedback-incorrect';
    for (const index of right) if (!selected.includes(index)) classes[index] = 'feedback-missed';
  } else {
    const correct = answer ? judge(question, answer).correct : false;
    if (selected.length) classes[selected[0]] = correct ? 'feedback-correct' : 'feedback-incorrect';
    if (!correct && typeof right === 'number') classes[right] = 'feedback-correct';
  }
  return classes;
}

/** The label each feedback class shows, from the CSS ::after content (index.html:114-182, 1348-1350). */
export function feedbackLabel(name: FeedbackClass | null, T: StudentTexts): string {
  if (name === 'feedback-correct') return T.correct_answer_label;
  if (name === 'feedback-incorrect') return T.your_answer_label;
  if (name === 'feedback-missed') return T.also_correct_label;
  return '';
}

/** Feedback (marks, "Continue") follows an answer only with show_right_answer (server.py:2377; index.html:2067-2091). */
export function feedbackShown(settings: QuizSettings): boolean {
  return settings.show_right_answer;
}

export type AnswerWay = 'answer' | 'question-timeout' | 'quiz-timeout';

/**
 * The explanation step comes after "Continue" only when the reply carries explanation_html:
 * show_right_answer and show_explanation on and the question has an explanation
 * (server.py:2377, 2391-2393; index.html:2017-2020, 2176-2181). A whole-quiz timeout never
 * carries it (server.py:2486-2488, with_explanation=False), and the page then shows the results.
 */
export function explanationShown(settings: QuizSettings, question: MatrixQuestion, way: AnswerWay): boolean {
  if (way === 'quiz-timeout') return false;
  return settings.show_right_answer && settings.show_explanation && !!question.explanation?.trim();
}

// ---- Rules: results ----

export interface Outcome {
  id: number;
  position: number;
  /** null = time ran out before an answer (a per-question or whole-quiz timeout). */
  answer: Answer | null;
}

/** A timeout counts as wrong, earns 0 points and runs no checker (server.py:2269-2270, 2323). */
export function outcomeCorrect(outcome: Outcome): boolean {
  if (outcome.answer === null) return false;
  return judge(questionByIdLocal(outcome.id), outcome.answer).correct;
}

function questionByIdLocal(id: number): MatrixQuestion {
  return QUESTIONS.find((q) => q.id === id)!;
}

/** Correct answers are in the results when show_right_answer, or after everyone finished (server.py:2710-2723). */
export function answersInResults(settings: QuizSettings, allCompleted: boolean): boolean {
  return settings.show_right_answer || (settings.show_answers_on_completion && allCompleted);
}

/** "Answers later" note: show_answers_on_completion while some student is not done (index.html:2381-2385). */
export function waitingNoteShown(settings: QuizSettings, allCompleted: boolean): boolean {
  return settings.show_answers_on_completion && !allCompleted;
}

export interface ExpectedRow {
  id: number;
  question: string;
  timedOut: boolean;
  answerParts: string[]; // text answers; image answers are in answerImages
  answerImages: string[];
  correct: boolean;
  hint: { prefix: string; parts: string[]; images: string[] } | null;
  mark: string | null;
  points: string | null;
  questionImage: string | null;
  fileLink: string | null;
}

export interface ExpectedResults {
  score: string;
  points: string | null;
  waitingNote: boolean;
  answersShown: boolean;
  table: { headers: string[]; rows: ExpectedRow[] } | null;
  correctCount: number;
  earned: number;
  total: number;
}

function displayParts(text: string): { parts: string[]; images: string[] } {
  // formatAnswerForDisplay (index.html:2274-2300): "|" splits answers; "/..." is an image
  const pieces = text.includes('|') ? text.split('|').map((piece) => piece.trim()) : [text];
  const parts: string[] = [];
  const images: string[] = [];
  for (const piece of pieces) {
    if (!piece) continue;
    if (piece.startsWith('/')) images.push(piece.slice(1));
    else parts.push(piece);
  }
  return { parts, images };
}

/**
 * The results screen (index.html:2367-2446) for a student's outcomes, in the order they were
 * stored (server.py:2343; whole-quiz timeouts in the student's order, server.py:2472-2490).
 */
export function expectedResults(matrixCase: MatrixCase, outcomes: Outcome[], allCompleted: boolean): ExpectedResults {
  const T = textsFor(matrixCase);
  const settings = matrixCase.settings;
  const total = outcomes.length;
  let correctCount = 0;
  let earned = 0;
  let totalPoints = 0;
  for (const outcome of outcomes) {
    const points = pointsOf(questionByIdLocal(outcome.id));
    totalPoints += points;
    if (outcomeCorrect(outcome)) {
      correctCount++;
      earned += points;
    }
  }
  earned = normalizePoints(earned); // server.py:2622-2624
  totalPoints = normalizePoints(totalPoints);
  const percentage = total > 0 ? pyRound((correctCount / total) * 100) : 0; // server.py:2627
  const pointsPercentage = totalPoints > 0 ? pyRound((earned / totalPoints) * 100) : 0; // server.py:2628
  const answersShown = answersInResults(settings, allCompleted);
  const customPoints = outcomes.some((outcome) => pointsOf(questionByIdLocal(outcome.id)) !== 1); // index.html:2392
  const headers = [T.question_header, T.your_answer_header];
  if (answersShown) headers.push(T.correct_header_alt); // index.html:2418
  if (customPoints) headers.push(T.points_header); // index.html:2419
  const rows: ExpectedRow[] = outcomes.map((outcome) => {
    const question = questionByIdLocal(outcome.id);
    const correct = outcomeCorrect(outcome);
    const timedOut = outcome.answer === null;
    const shown = timedOut ? { parts: [], images: [] } : displayParts(answerText(question, outcome.answer!));
    const right = displayParts(correctAnswerText(question));
    const points = pointsOf(question);
    return {
      id: question.id,
      question: question.question,
      timedOut,
      answerParts: shown.parts,
      answerImages: shown.images,
      correct,
      // index.html:2430: only for a wrong answer, and only while correct answers are in the results
      hint: !correct && answersShown ? { prefix: T.result_correct_header, parts: right.parts, images: right.images } : null,
      mark: answersShown ? (correct ? '✓' : '✗') : null, // index.html:2432-2434
      points: customPoints ? `${formatPoints(correct ? points : 0)}/${formatPoints(points)}` : null, // index.html:2435
      questionImage: question.image ? question.image.slice(1) : null, // index.html:2253-2257
      fileLink: question.file ? question.file : null, // index.html:2259-2264
    };
  });
  return {
    score: `${T.result_label} ${correctCount}/${total} (${percentage}%)`, // index.html:2398
    // index.html:2378-2380, 2393-2395 (a 0 falls back to the count and the plain percentage, as JS || does)
    points: customPoints
      ? `${T.points_label} ${formatPoints(earned || 0)}/${formatPoints(totalPoints || total)} (${pointsPercentage || percentage}%)`
      : null,
    waitingNote: waitingNoteShown(settings, allCompleted),
    answersShown,
    table: settings.show_final_list ? { headers, rows } : null, // index.html:2411
    correctCount,
    earned,
    total: totalPoints,
  };
}

// ---- Rules: question order ----

/**
 * With randomize_questions each student gets an order of sticky groups (server.py:1844-1894):
 * question 9 has stick_to_the_previous, so it always comes right after 8.
 * Without it, the order is the file order (server.py:2450-2454).
 */
export function orderProblems(order: number[], randomize: boolean): string[] {
  const problems: string[] = [];
  const ids = QUESTIONS.map((q) => q.id);
  if ([...order].sort((a, b) => a - b).join(',') !== ids.join(',')) problems.push(`not every question once: ${order.join(',')}`);
  if (!randomize && order.join(',') !== ids.join(',')) problems.push(`not the file order: ${order.join(',')}`);
  for (const question of QUESTIONS.filter((q) => q.stick_to_the_previous)) {
    const at = order.indexOf(question.id);
    if (at <= 0 || order[at - 1] !== question.id - 1) problems.push(`question ${question.id} is not right after ${question.id - 1}`);
  }
  return problems;
}

// ---- Checks: registration and waiting ----

export function checkRegistration(matrixCase: MatrixCase, snap: Snap): CheckResult[] {
  const T = textsFor(matrixCase);
  const hidden = hiddenFieldLabels(matrixCase);
  return [
    same('registration form shown', 'index.html:1472-1482', true, snap.visible.registration),
    same('quiz title', 'index.html:892; server.py:1165, 1337', QUIZ_TITLE, norm(snap.title)),
    same('browser tab title', 'index.html:6; server.py:1337', QUIZ_TITLE, snap.docTitle),
    same('form heading', 'index.html:902; translations.py register_title', T.register_title, norm(snap.registration.heading)),
    same('field labels (username label, then registration.fields)', 'server.py:1306-1331', registrationLabels(matrixCase), snap.registration.labels.map(norm)),
    same('number of inputs', 'server.py:1312-1331', registrationLabels(matrixCase).length, snap.registration.inputs),
    truth(
      'disabled_fields not on the form',
      'server.py:1321-1331 (only registration.fields is read)',
      hidden.every((label) => !snap.pageText.includes(label)),
      hidden.length ? `no ${hidden.join(', ')}` : '(no disabled fields)',
      hidden.filter((label) => snap.pageText.includes(label)).join(', ') || 'not shown',
    ),
    same('register button', 'index.html:906; translations.py register_button', T.register_button, norm(snap.registration.button)),
  ];
}

/** The version under the quiz (index.html:2449-2451; server.py:1343). */
export function checkFooter(version: string, snap: Snap): CheckResult {
  const line = snap.pageText.split('\n').find((text) => text.includes('WebQuiz v')) ?? null;
  return same('version under the quiz', 'index.html:2449-2451; server.py:1343', `WebQuiz v${version}`, line ? norm(line) : null);
}

/** How far the name badge and the theme button moved on the page when the page scrolled. */
export interface ScrollSeen {
  scrolled: number; // pixels the page scrolled
  badge: number | null; // change of the badge's place on the page (0 = it scrolled away with the page)
  toggle: number | null;
}

/**
 * The name badge and the theme button sit in .top-right-controls, placed with position: absolute
 * inside the quiz card, so they scroll away with the page and never cover the quiz
 * (index.html:51-66, 886-891).
 */
export function checkTopControls(seen: ScrollSeen): CheckResult[] {
  if (seen.scrolled <= 0) return [];
  return [
    same(
      'name badge and theme button scroll away with the page',
      'index.html:51-66, 886-891 (.top-right-controls: position absolute in the card)',
      { badge: 0, toggle: 0 },
      { badge: seen.badge, toggle: seen.toggle },
    ),
  ];
}

export function checkTheme(theme: 'light' | 'dark', snap: Snap): CheckResult {
  return same('theme from localStorage "theme"', 'index.html:1297-1303', theme === 'dark', snap.dark);
}

export function checkNoSideScroll(device: string, snap: Snap): CheckResult[] {
  if (device !== 'phone') return [];
  return [
    truth('no horizontal scroll on the phone', 'index.html:465-633 (mobile layout)', snap.overflowX <= 1, 'page width <= screen width', `${snap.overflowX}px wider`),
  ];
}

export function checkWaiting(matrixCase: MatrixCase, snap: Snap, values: string[]): CheckResult[] {
  const T = textsFor(matrixCase);
  return [
    same('waiting screen shown', 'index.html:1551-1555', true, snap.visible.waiting),
    same('waiting heading', 'index.html:912', T.waiting_title, norm(snap.waiting.heading)),
    truth(
      'waiting description and hint',
      'index.html:913-916',
      norm(snap.waiting.text).includes(norm(T.waiting_description)) && norm(snap.waiting.text).includes(norm(T.waiting_hint)),
      `${T.waiting_description} ${T.waiting_hint}`,
      norm(snap.waiting.text).slice(0, 300),
    ),
    same('waiting form labels', 'index.html:1578-1590', registrationLabels(matrixCase), snap.waiting.labels.map(norm)),
    same('waiting form keeps the typed values', 'index.html:1579, 1587-1589', values, snap.waiting.values),
    same('waiting buttons', 'index.html:920-921', [T.save_changes, T.check_status], snap.waiting.buttons.map(norm)),
    same('username shown at the top', 'index.html:1543-1544', `👤 ${values[0]}`, norm(snap.userInfo)),
    same('quiz not shown yet', 'index.html:1552-1555', false, snap.visible.test),
  ];
}

export function checkNotApprovedYet(matrixCase: MatrixCase, snap: Snap): CheckResult[] {
  const T = textsFor(matrixCase);
  return [
    same('"not approved yet" after Check', 'index.html:1689-1692', T.not_approved_yet, norm(snap.waiting.message)),
    same('still waiting', 'index.html:1689-1692', true, snap.visible.waiting),
  ];
}

// ---- Checks: a question before the answer ----

function seconds(text: string): number | null {
  const match = /^(\d+):(\d\d)$/.exec(norm(text));
  return match ? Number(match[1]) * 60 + Number(match[2]) : null;
}

export function checkQuestion(
  matrixCase: MatrixCase,
  question: MatrixQuestion,
  position: number,
  snap: Snap,
  username: string,
): CheckResult[] {
  const T = textsFor(matrixCase);
  const settings = matrixCase.settings;
  const q = snap.question;
  const width = (position / QUESTIONS.length) * 100;
  const results: CheckResult[] = [
    same('progress text', 'index.html:1765-1767', progressText(T, position, QUESTIONS.length), norm(snap.progress)),
    truth(
      'progress bar width',
      'index.html:1765-1766',
      snap.progressWidth !== null && Math.abs(snap.progressWidth - width) < 0.01,
      `${formatPoints(width)}%`,
      snap.progressWidth === null ? '(no bar)' : `${formatPoints(snap.progressWidth)}%`,
    ),
    same('question text cannot be selected (copy protection)', 'index.html:338-349, 975-978', 'none', q?.userSelect ?? null),
    same('question text', 'index.html:1816', question.question, norm(q?.text)),
    same('points badge (only for points other than 1)', 'index.html:1815', pointsBadge(question, T), q?.badge === null ? null : norm(q?.badge)),
    same('username shown at the top', 'index.html:887-889, 1321-1324', `👤 ${username}`, norm(snap.userInfo)),
  ];
  const limit = effectiveLimit(settings, question);
  if (limit > 0) {
    const shown = q?.timer ? seconds(q.timer.text) : null;
    results.push(
      truth(
        'question countdown',
        'index.html:1814, 1151-1168; time_limits.py:92-105',
        !!q?.timer?.visible && shown !== null && shown > 0 && shown <= limit && norm(q!.timer!.label).includes(T.time_left),
        `"⏱ ${T.time_left}" with 0:01..${Math.floor(limit / 60)}:${String(limit % 60).padStart(2, '0')}`,
        q?.timer ? `${q.timer.visible ? 'visible' : 'hidden'} "${norm(q.timer.label)}"` : 'no countdown',
      ),
    );
    if (q?.timer && shown !== null) {
      results.push(same('countdown is red at 10 s or less', 'index.html:1160', shown <= 10, q.timer.warning));
    }
  } else {
    results.push(same('no question countdown', 'index.html:1814 (time_limit 0 = no box); time_limits.py:92-105', null, q?.timer ? norm(q.timer.label) : null));
  }
  if (quizTimerShown(settings)) {
    const shown = seconds(snap.quizTimer.text);
    results.push(
      truth(
        'whole-quiz countdown',
        'index.html:927, 1170-1182; server.py:2587-2590',
        snap.quizTimer.visible && shown !== null && shown > 0 && shown <= WHOLE_QUIZ_SECONDS && norm(snap.quizTimer.label) === T.time_left,
        `"⏱ ${T.time_left}" with 0:01..1:00`,
        `${snap.quizTimer.visible ? 'visible' : 'hidden'} "${norm(snap.quizTimer.label)}" ${snap.quizTimer.text}`,
      ),
    );
    if (shown !== null) results.push(same('whole-quiz countdown is red at 10 s or less', 'index.html:1200', shown <= 10, snap.quizTimer.warning));
  } else {
    results.push(same('no whole-quiz countdown', 'index.html:927 (hidden), 1130-1131', false, snap.quizTimer.visible));
  }
  results.push(same('"several answers" hint', 'index.html:1788', isMultiple(question) ? T.multiple_choice_hint : null, q?.hint ? norm(q.hint) : null));
  if (isText(question)) {
    results.push(
      same(
        'text answer box',
        'index.html:1775-1783',
        { label: T.enter_answer_label, placeholder: T.enter_answer_placeholder, value: question.default_value ?? '', disabled: false },
        q?.textInput ? { label: norm(q.textInput.label), placeholder: q.textInput.placeholder, value: q.textInput.value, disabled: q.textInput.disabled } : null,
      ),
    );
    results.push(same('the answer box text can be selected', 'index.html:351-356', 'text', q?.textInput?.userSelect ?? null));
  } else {
    const options = question.options!;
    const imageOptions = options.filter((option) => option.startsWith('/'));
    if (imageOptions.length) {
      results.push(
        same(
          'image options shown and loaded',
          'index.html:1790-1799',
          imageOptions.map((option) => ({ src: option.slice(1), loaded: true })),
          (q?.options ?? []).map((option) => (option.image ? { src: option.image.src.replace(/^\.?\//, ''), loaded: option.image.loaded } : null)),
        ),
      );
    } else {
      results.push(same('options', 'index.html:1800-1805', options, (q?.options ?? []).map((option) => norm(option.text))));
    }
  }
  results.push(
    same(
      'question image',
      'index.html:1817',
      question.image ? { src: question.image.slice(1), loaded: true } : null,
      q?.image ? { src: q.image.src.replace(/^\.?\//, ''), loaded: q.image.loaded } : null,
    ),
  );
  results.push(
    same(
      'file attachment link',
      'index.html:1818; server.py:1286-1291',
      question.file ? { name: question.file, href: `attach/${question.file}`, download: true } : null,
      q?.file ? { name: norm(q.file.name).replace(/^📎\s*/, ''), href: q.file.href.replace(/^\.?\//, ''), download: q.file.download } : null,
    ),
  );
  results.push(
    same(
      'submit button (disabled until an answer is chosen)',
      'index.html:944, 1835-1841',
      { visible: true, disabled: !(question.default_value ?? '').trim(), text: T.submit_answer },
      { visible: snap.submit.visible, disabled: snap.submit.disabled, text: norm(snap.submit.text) },
    ),
  );
  results.push(same('no "Continue" yet', 'index.html:1842', false, snap.cont.visible));
  results.push(
    same('no feedback marks yet', 'index.html:1786-1809', [], (q?.options ?? []).flatMap((option) => option.classes.filter((name) => name.startsWith('feedback-')))),
  );
  return results;
}

/** The attachment downloads as a file (server.py:3848-3881). */
export function checkAttachmentDownload(
  question: MatrixQuestion,
  content: string,
  reply: { status: number; disposition: string | null; body: string },
): CheckResult[] {
  return [
    same(
      'the attachment downloads as a file',
      'server.py:3848-3881 (Content-Disposition: attachment)',
      { status: 200, disposition: `attachment; filename="${question.file}"`, body: content },
      reply,
    ),
  ];
}

/** Before sending: the chosen options are marked and "Submit" works (index.html:1846-1893). */
export function checkChoice(question: MatrixQuestion, answer: Answer, seen: { selected: number[]; submitEnabled: boolean }): CheckResult[] {
  const indices = 'indices' in answer ? [...answer.indices].sort((a, b) => a - b) : [];
  return [
    same(
      isText(question) ? 'typed answer: "Submit" can be pressed' : 'chosen options are marked, "Submit" can be pressed',
      isText(question) ? 'index.html:1846-1850' : 'index.html:1852-1893',
      { selected: indices, submitEnabled: true },
      { selected: [...seen.selected].sort((a, b) => a - b), submitEnabled: seen.submitEnabled },
    ),
  ];
}

/**
 * Image sizes on a question screen. A question image keeps its own size when it fits
 * (index.html:214-223, max-width 100%). Image options are at most 300 px wide on a desktop
 * (index.html:228-235) and fill the screen width on a phone (index.html:501-514, width 100%).
 */
export function checkImageSizes(device: string, snap: Snap): CheckResult[] {
  const q = snap.question;
  const results: CheckResult[] = [];
  const screen = snap.viewportWidth ?? 0;
  if (q?.image && q.image.natural) {
    const want = Math.min(q.image.natural, screen);
    results.push(
      truth('question image at its own size', 'index.html:214-223', Math.abs((q.image.width ?? 0) - want) <= 2, `${want}px wide`, `${q.image.width}px wide`),
    );
  }
  const images = (q?.options ?? []).map((option) => option.image).filter((image): image is ImageSnap => !!image && !!image.natural);
  if (images.length) {
    if (device === 'phone') {
      results.push(
        truth(
          'image options fill the phone screen width',
          'index.html:501-514 (.option-image width 100%, no side padding)',
          images.every((image) => (image.width ?? 0) >= screen * 0.95),
          `${screen}px wide (the screen)`,
          images.map((image) => `${image.width}px`).join(', '),
        ),
      );
    } else {
      results.push(
        same(
          'image options at most 300 px wide',
          'index.html:228-235',
          images.map((image) => Math.min(image.natural!, 300)),
          images.map((image) => image.width),
        ),
      );
    }
  }
  return results;
}

// ---- Checks: after the answer ----

/** The screen right after an answer with show_right_answer on (index.html:2059-2174). */
export function checkFeedback(matrixCase: MatrixCase, question: MatrixQuestion, answer: Answer | null, snap: Snap): CheckResult[] {
  const T = textsFor(matrixCase);
  const q = snap.question;
  const results: CheckResult[] = [];
  const timedOut = answer === null;
  if (isText(question)) {
    const { correct, checkerError } = timedOut ? { correct: false, checkerError: null } : judge(question, answer);
    results.push(
      same(
        'answer box marked, locked, keeps the text',
        'index.html:2098-2101',
        { disabled: true, mark: correct ? 'feedback-correct' : 'feedback-incorrect', value: answer && 'text' in answer ? answer.text : (question.default_value ?? '') },
        q?.textInput
          ? { disabled: q.textInput.disabled, mark: q.textInput.classes.find((name) => name.startsWith('feedback-')) ?? null, value: q.textInput.value }
          : null,
      ),
    );
    results.push(same('checker message', 'index.html:2106-2109', !correct && checkerError ? `⚠️ ${checkerError}` : null, q?.checkerError ? norm(q.checkerError) : null));
    results.push(
      same(
        'right value after a wrong answer',
        'index.html:2111-2114; server.py:2380-2383',
        !correct && question.correct_value ? norm(T.correct_answer_value + question.correct_value) : null,
        q?.correctValue ? norm(q.correctValue) : null,
      ),
    );
  } else {
    const expected = feedbackClasses(question, answer);
    results.push(
      same(
        'option marks (correct / your answer / also correct)',
        'index.html:2118-2158',
        expected,
        (q?.options ?? []).map((option) => (option.classes.find((name) => name.startsWith('feedback-')) as FeedbackClass) ?? null),
      ),
    );
    results.push(
      same(
        'option mark labels',
        'index.html:114-182 (::after texts), 1348-1350',
        expected.map((name) => feedbackLabel(name, T)),
        (q?.options ?? []).map((option) => norm(option.label)),
      ),
    );
    results.push(
      same('options locked', 'index.html:2160-2165', true, (q?.options ?? []).length > 0 && (q?.options ?? []).every((option) => option.classes.includes('disabled'))),
    );
  }
  results.push(same('submit hidden, "Continue" shown', 'index.html:2167-2173', { submit: false, cont: true, text: T.continue_button, disabled: false }, {
    submit: snap.submit.visible,
    cont: snap.cont.visible,
    text: norm(snap.cont.text),
    disabled: snap.cont.disabled,
  }));
  return results;
}

/** What the page did after an answer with show_right_answer off: it went on by itself (index.html:2067-2091). */
export interface AdvanceSeen {
  advanced: boolean; // the next question or the results came without a click
  sawContinue: boolean;
  sawMarks: boolean;
  sawExplanation: boolean;
  millis: number;
}

export function checkAutoAdvance(seen: AdvanceSeen, afterTimeout: boolean): CheckResult[] {
  return [
    same(
      'no marks, no "Continue", no explanation: the page goes on by itself',
      'server.py:2376-2393 (no is_correct, no explanation_html); index.html:2067-2091',
      { advanced: true, sawContinue: false, sawMarks: false, sawExplanation: false },
      { advanced: seen.advanced, sawContinue: seen.sawContinue, sawMarks: seen.sawMarks, sawExplanation: seen.sawExplanation },
    ),
    truth(
      afterTimeout ? 'goes on about 1.5 s after "time is up"' : 'goes on at once',
      'index.html:2087-2089',
      afterTimeout ? seen.millis >= 900 && seen.millis <= 6000 : seen.millis <= 3000,
      afterTimeout ? '0.9..6 s' : '<= 3 s',
      `${(seen.millis / 1000).toFixed(1)} s`,
    ),
  ];
}

export function checkExplanation(matrixCase: MatrixCase, question: MatrixQuestion, position: number, snap: Snap): CheckResult[] {
  const T = textsFor(matrixCase);
  const expect = question.explanationExpect!;
  const e = snap.explanation;
  return [
    same('explanation step shown, question hidden', 'index.html:1711-1725', { step: true, question: false }, { step: snap.visible.explanation, question: snap.visible.question }),
    same('progress still shows the answered question', 'index.html:1711-1725 (the index does not move)', progressText(T, position, QUESTIONS.length), norm(snap.progress)),
    same('explanation title', 'index.html:1713', T.explanation_title, norm(e.title)),
    same('explanation repeats the question', 'index.html:1714', question.question, norm(e.question)),
    truth('bold text from the Markdown', 'explanations.py:59-73 (mistune)', expect.strong.every((word) => e.strong.map(norm).includes(word)), show(expect.strong), show(e.strong.map(norm))),
    same('list items from the Markdown', 'explanations.py:59-73 (mistune)', expect.listItems, e.listItems),
    same(
      'links open in a new tab',
      'explanations.py:39-46',
      expect.links.map((link) => ({ text: link.text, href: link.href, target: '_blank', noopener: true })),
      e.links.map((link) => ({ text: norm(link.text), href: link.href, target: link.target, noopener: link.rel.split(/\s+/).includes('noopener') })),
    ),
    same('"Next" button', 'index.html:1717-1719', { visible: true, disabled: false, text: T.next_button }, { visible: e.next.visible, disabled: e.next.disabled, text: norm(e.next.text) }),
  ];
}

/** The per-question time-up screen (index.html:1212-1219, 1269-1274, 1990-2003, 2063-2065). */
export function checkQuestionTimeUp(matrixCase: MatrixCase, question: MatrixQuestion, snap: Snap): CheckResult[] {
  const T = textsFor(matrixCase);
  const q = snap.question;
  const results: CheckResult[] = [
    same('"time is up" message', 'index.html:1269-1274; translations.py time_is_up', `⏱ ${T.time_is_up}`, q?.timeUp ? norm(q.timeUp) : null),
    same('countdown shows 0:00 in red', 'index.html:1997-2002', { text: '0:00', red: true }, q?.timer ? { text: norm(q.timer.text), red: q.timer.warning } : null),
  ];
  if (feedbackShown(matrixCase.settings)) results.push(...checkFeedback(matrixCase, question, null, snap));
  return results;
}

// ---- Checks: results ----

function rowColumns(headers: string[], T: StudentTexts): { mark: number; points: number } {
  return { mark: headers.indexOf(T.correct_header_alt), points: headers.indexOf(T.points_header) };
}

export function checkResults(matrixCase: MatrixCase, expected: ExpectedResults, snap: Snap): CheckResult[] {
  const T = textsFor(matrixCase);
  const r = snap.results;
  const paragraphs = (r?.paragraphs ?? []).map(norm);
  const results: CheckResult[] = [
    same('results shown', 'index.html:2367-2446', true, snap.visible.results && !snap.visible.test),
    same('score', 'index.html:2397-2398; server.py:2615-2627', expected.score, paragraphs.find((p) => p.startsWith(T.result_label)) ?? null),
    same('points line (some question is not worth 1 point)', 'index.html:2391-2395; server.py:2619-2628', expected.points, paragraphs.find((p) => p.startsWith(T.points_label)) ?? null),
    same(
      '"answers later" note',
      'index.html:2381-2385, 2400-2408',
      expected.waitingNote ? { note: norm(T.answers_available_later), reload: norm(T.reload_later), button: norm(T.reload_page) } : null,
      paragraphs.includes(norm(T.answers_available_later)) || (r?.buttons ?? []).map(norm).includes(norm(T.reload_page))
        ? {
            note: paragraphs.find((p) => p === norm(T.answers_available_later)) ?? null,
            reload: paragraphs.find((p) => p === norm(T.reload_later)) ?? null,
            button: (r?.buttons ?? []).map(norm).find((b) => b === norm(T.reload_page)) ?? null,
          }
        : null,
    ),
    same('results table (show_final_list)', 'index.html:2411', expected.table !== null, !!r?.table),
  ];
  if (!expected.table || !r?.table) return results;
  const table = r.table;
  results.push(same('table columns', 'index.html:2414-2420', expected.table.headers, table.headers.map(norm)));
  const columns = rowColumns(table.headers.map(norm), T);
  results.push(
    same('table rows (in the order answered)', 'server.py:2343, 2472-2490', expected.table.rows.map((row) => row.question), table.rows.map((cells) => norm(cells[0]?.firstLine))),
  );
  expected.table.rows.forEach((row, index) => {
    const cells = table.rows[index] ?? [];
    const answerCell = cells[1];
    const actual = {
      timeUp: answerCell?.timeUp ? norm(answerCell.timeUp) : null,
      answer: answerCell?.span && !answerCell.timeUp ? answerCell.span.parts.map(norm) : [],
      images: answerCell?.span && !answerCell.timeUp ? answerCell.span.images.map((src) => src.replace(/^\.?\//, '')) : [],
      class: answerCell?.span && !answerCell.timeUp ? answerCell.span.classes.find((name) => name.startsWith('answer-')) ?? null : null,
      hint: answerCell?.hint ? norm(answerCell.hint.text) : null,
      hintImages: answerCell?.hint ? answerCell.hint.images.map((src) => src.replace(/^\.?\//, '')) : [],
      mark: columns.mark >= 0 ? norm(cells[columns.mark]?.text) : null,
      points: columns.points >= 0 ? norm(cells[columns.points]?.text) : null,
      questionImage: cells[0]?.images.length ? cells[0].images[0].replace(/^\.?\//, '') : null,
      fileLink: cells[0]?.fileLink ? norm(cells[0].fileLink).replace(/^📎\s*/, '') : null,
    };
    const want = {
      timeUp: row.timedOut ? `⏱ ${T.time_is_up}` : null,
      answer: row.answerParts,
      images: row.answerImages,
      class: row.timedOut || !expected.answersShown ? null : row.correct ? 'answer-correct' : 'answer-incorrect',
      hint: row.hint ? norm(row.hint.prefix + row.hint.parts.join(' ')) : null,
      hintImages: row.hint ? row.hint.images : [],
      mark: row.mark,
      points: row.points,
      questionImage: row.questionImage,
      fileLink: row.fileLink,
    };
    results.push(same(`row ${index + 1}: question ${row.id}`, 'index.html:2423-2437', want, actual));
  });
  return results;
}

// ---- Checks: stored data against the screen ----

export interface CsvRow {
  user_id: string;
  question: string;
  selected_answer: string;
  correct_answer: string;
  is_correct: string;
}

/** The answers CSV rows of one student match the outcomes (server.py:1390-1405, 2282-2304). */
export function checkCsvRows(outcomes: Outcome[], rows: CsvRow[], who: string): CheckResult[] {
  const results: CheckResult[] = [same(`${who}: one CSV row per question`, 'server.py:2310, 1395-1405', outcomes.length, rows.length)];
  for (const outcome of outcomes) {
    const question = questionByIdLocal(outcome.id);
    const row = rows.find((item) => item.question === question.question);
    results.push(
      same(
        `${who}: CSV row of question ${question.id}`,
        'server.py:2282-2304, 1395-1405',
        {
          selected_answer: outcome.answer === null ? '' : answerText(question, outcome.answer),
          correct_answer: correctAnswerText(question),
          is_correct: outcomeCorrect(outcome) ? 'True' : 'False',
        },
        row ? { selected_answer: row.selected_answer, correct_answer: row.correct_answer, is_correct: row.is_correct } : null,
      ),
    );
  }
  return results;
}

/** The score and points on the screen agree with the stored CSV files (server.py:1507-1546, 2593-2641). */
export function checkCsvAgainstScreen(
  matrixCase: MatrixCase,
  snap: Snap,
  rows: CsvRow[],
  user: Record<string, string> | null,
): CheckResult[] {
  const T = textsFor(matrixCase);
  const paragraphs = (snap.results?.paragraphs ?? []).map(norm);
  const score = paragraphs.find((p) => p.startsWith(T.result_label)) ?? '';
  const points = paragraphs.find((p) => p.startsWith(T.points_label)) ?? '';
  const scoreMatch = /(\d+)\s*\/\s*(\d+)/.exec(score);
  const pointsMatch = /([\d.]+)\s*\/\s*([\d.]+)/.exec(points);
  const stored = rows.filter((row) => row.is_correct === 'True').length;
  const results: CheckResult[] = [
    same('screen score = correct rows in the answers CSV', 'server.py:2615-2617, 1395-1405', `${stored}/${rows.length}`, scoreMatch ? `${scoreMatch[1]}/${scoreMatch[2]}` : score),
  ];
  results.push(
    same(
      'users CSV row agrees with the screen',
      'server.py:1529-1544',
      user ? { correct_answers: scoreMatch?.[1] ?? '?', earned_points: pointsMatch?.[1] ?? '?', total_points: pointsMatch?.[2] ?? '?' } : 'a users CSV row',
      user ? { correct_answers: user.correct_answers, earned_points: user.earned_points, total_points: user.total_points } : null,
    ),
  );
  const table = snap.results?.table;
  if (table) {
    const columns = rowColumns(table.headers.map(norm), T);
    if (columns.mark >= 0) {
      const screen = table.rows.map((cells) => ({ question: norm(cells[0]?.firstLine), correct: norm(cells[columns.mark]?.text) === '✓' }));
      const csv = screen.map((item) => {
        const row = rows.find((r) => r.question === item.question);
        return { question: item.question, correct: row ? row.is_correct === 'True' : null };
      });
      results.push(same('✓/✗ marks = is_correct in the answers CSV', 'server.py:1395-1405; index.html:2432-2434', csv, screen));
    }
  }
  return results;
}

