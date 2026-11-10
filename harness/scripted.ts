// Scripted students (no AI) and the scripted teacher.
//
// Scripted students act at exact times, so they cover the timing edges that
// an AI agent cannot hit on purpose. They use the same driver commands as the
// agents, so their steps are recorded the same way.

import path from 'node:path';
import { AUTO_STATE_JS, type AutoState } from './auto';
import { baseName, fieldValue } from './answers';
import type { Classroom } from './classroom';
import { sleep } from './classroom';
import type { Action, AnswerSpec, Plan } from './scenarios';

const POLL = 0.3;

const clock = () => performance.now() / 1000;

type Resolved = { kind: 'options'; labels: string[] } | { kind: 'text'; value: string };

/**
 * Turn an answer spec into the option labels or the text to type.
 *
 * null means the scenario's correct answer for this question id, and {wrong: true}
 * its wrong answer (so both also work when every student gets the questions in a
 * different order).
 */
export function resolveAnswer(
  spec: AnswerSpec,
  questionId: number,
  answers: Record<number, AnswerSpec>,
  wrongs: Record<number, AnswerSpec> = {},
): Resolved {
  if (spec === null) spec = answers[questionId];
  if (spec !== null && typeof spec === 'object' && !Array.isArray(spec) && 'wrong' in spec) {
    spec = wrongs[questionId];
    if (spec === undefined || spec === null) throw new Error(`no wrong answer for question ${questionId}`);
  }
  if (spec !== null && typeof spec === 'object' && !Array.isArray(spec) && 'text' in spec) return { kind: 'text', value: spec.text };
  if (typeof spec === 'string') return { kind: 'options', labels: [spec] };
  return { kind: 'options', labels: [...(spec as string[])] };
}

/** The answers of one scenario: the correct and the wrong one of each question id. */
export interface AnswerKey {
  right: Record<number, AnswerSpec>;
  wrong: Record<number, AnswerSpec>;
}

/** The option on the screen with this text, or with this picture ("/imgs/circle.png"). */
async function optionIndex(classroom: Classroom, name: string, label: string): Promise<number> {
  const state = await classroom.evaluate<AutoState>(name, AUTO_STATE_JS);
  const options = state ? state.options : [];
  const byText = options.find((option) => option.text === label);
  if (byText) return byText.index;
  const byImage = options.find((option) => option.image !== null && baseName(option.image) === baseName(label));
  if (byImage) return byImage.index;
  throw new Error(`no option '${label}' on the screen`);
}

async function giveAnswer(
  classroom: Classroom,
  name: string,
  spec: AnswerSpec,
  questionNumber: number,
  key: AnswerKey,
  submit = true,
): Promise<void> {
  const answer = resolveAnswer(spec, questionNumber, key.right, key.wrong);
  if (answer.kind === 'text') {
    await classroom.command(name, 'type', answer.value, { screenshot: false, settle: 0.2, selector: '#text-answer-input' });
  } else {
    for (const label of answer.labels) {
      let index: number;
      try {
        index = await optionIndex(classroom, name, label);
      } catch {
        // Not on the screen: the click by text records the failure as a step
        await classroom.command(name, 'click', label, { screenshot: false, settle: 0.2 });
        continue;
      }
      await classroom.command(name, 'click', label, {
        screenshot: false,
        settle: 0.2,
        selector: `#current-question-container .option[data-option-index="${index}"]`,
      });
    }
  }
  if (submit) await classroom.clickSelector(name, '#submit-answer-btn', true);
}

/** Type the name and every registration field, then press the register button. */
export async function register(classroom: Classroom, name: string, number: number): Promise<void> {
  const state = await classroom.evaluate<AutoState>(name, AUTO_STATE_JS);
  await classroom.command(name, 'type', name, { screenshot: false, settle: 0.2, selector: '#username' });
  for (const field of state ? state.fields : []) {
    const value = fieldValue(field.label || field.name, name, number);
    await classroom.command(name, 'type', value, { screenshot: false, settle: 0.1, selector: field.selector });
  }
  await classroom.clickSelector(name, '.register-btn', true);
}

/** Sleep until `seconds` after `start` (a clock() value). */
async function until(start: number, seconds: number): Promise<void> {
  const delay = start + seconds - clock();
  if (delay > 0) await sleep(delay);
}

/** Run one plan action. Returns false when the student stops (left the quiz). */
async function handleQuestion(
  classroom: Classroom,
  name: string,
  action: Action,
  number: number,
  answers: AnswerKey,
  shownAt: number,
  quizStart: number,
): Promise<boolean> {
  switch (action[0]) {
    case 'answer':
      await until(shownAt, action[2]);
      await giveAnswer(classroom, name, action[1], number, answers);
      break;
    case 'text':
      await until(shownAt, action[2]);
      await giveAnswer(classroom, name, { text: action[1] }, number, answers);
      break;
    case 'pick':
      await until(shownAt, action[2]);
      await giveAnswer(classroom, name, action[1], number, answers, false);
      break;
    case 'idle':
      break;
    case 'leave':
      await until(shownAt, action[1]);
      await classroom.command(name, 'close', null, { screenshot: false });
      return false;
    case 'reload':
      await until(shownAt, action[1]);
      await classroom.command(name, 'reload', null, { screenshot: true, settle: 1.0 });
      await until(shownAt, action[3]);
      await giveAnswer(classroom, name, action[2], number, answers);
      break;
    case 'offline_answer': {
      const [, label, offAt, offFor, delay] = action;
      await until(shownAt, offAt);
      await classroom.command(name, 'offline', offFor, { screenshot: false, settle: 0 });
      await until(shownAt, delay);
      await giveAnswer(classroom, name, label, number, answers);
      break;
    }
    case 'offline_at_quiz_time':
      await until(quizStart, action[1]);
      await classroom.command(name, 'offline', action[2], { screenshot: false, settle: 0 });
      break;
    default:
      throw new Error(`unknown action ${JSON.stringify(action)}`);
  }
  return true;
}

/**
 * Press "Check" at the planned times (seconds since registration) until the quiz starts.
 *
 * After the planned checks the student keeps checking every 5 s for 20 s more, then gives up.
 * Returns true when the quiz started.
 */
async function waitForApproval(classroom: Classroom, name: string, checks: number[]): Promise<boolean> {
  const registeredAt = clock();
  const last = checks[checks.length - 1];
  const times = [...checks, ...[1, 2, 3, 4].map((n) => last + 5 * n)];
  for (const at of times) {
    await until(registeredAt, at);
    await classroom.clickSelector(name, '.check-status-btn', true);
    await sleep(0.5);
    if ((await classroom.state(name)).question) return true;
  }
  return false;
}

/**
 * Register, then follow the plan question by question until the results or the deadline.
 *
 * plan.actions: {position: action}; position 1 is the first question this student sees.
 * plan.check: when approval is on, the times (seconds after registering) to press "Check".
 * questionIds: {question text: question id}, to find the correct answer in a random order.
 * deadline: a clock() value. studentNumber: gives each student other registration values.
 */
export async function scriptedStudent(
  classroom: Classroom,
  name: string,
  plan: Plan,
  answers: AnswerKey,
  questionIds: Record<string, number>,
  deadline: number,
  studentNumber = 1,
): Promise<void> {
  await classroom.command(name, 'open', null, { screenshot: false });
  await register(classroom, name, studentNumber);

  if (plan.check && !(await waitForApproval(classroom, name, plan.check))) {
    await classroom.command(name, 'look', null, { screenshot: true });
    return;
  }

  const seen = new Set<string>();
  let number = 0;
  let quizStart: number | null = null;
  while (clock() < deadline) {
    const state = await classroom.state(name);
    if (state.results) {
      await sleep(1.0); // the results come from the server; give them a moment
      await classroom.command(name, 'look', null, { screenshot: true });
      return;
    }
    const question = state.question;
    if (question && !seen.has(question) && !state.time_up) {
      seen.add(question);
      number += 1;
      const shownAt = clock();
      quizStart = quizStart ?? shownAt;
      const action: Action = plan.actions[number] ?? ['answer', null, 3];
      const questionId = questionIds[question] ?? number;
      if (!(await handleQuestion(classroom, name, action, questionId, answers, shownAt, quizStart))) return;
      continue;
    }
    if (state.can_continue) {
      await classroom.clickSelector(name, '#continue-btn', false);
      continue;
    }
    if (state.can_next) {
      // The explanation step after an answer: read it for a moment, then go on
      await sleep(0.8);
      await classroom.clickSelector(name, '#explanation-next-btn', true);
      continue;
    }
    await sleep(POLL);
  }
  await classroom.command(name, 'look', null, { screenshot: true });
}

/**
 * Log in to the admin panel and start the quiz through the start pop-up.
 *
 * Returns the settings the running quiz uses (from the switch-quiz reply).
 */
export async function teacherStart(
  classroom: Classroom,
  quizFile: string,
  settings: Record<string, any>, // eslint-disable-line @typescript-eslint/no-explicit-any
  masterKey: string,
): Promise<Record<string, unknown> | undefined> {
  const teacher = await classroom.addStudent('teacher');
  const page = await teacher.context.newPage();
  const shots = path.join(classroom.runDir, 'shots');
  await page.goto(classroom.baseUrl + '/admin/');
  // 127.0.0.1 is a trusted address by default, so the login form may not show
  await page.waitForFunction(`['admin-panel', 'master-key'].some(id => {
    const el = document.getElementById(id);
    return el && el.getClientRects().length > 0;
  })`);
  if (await page.locator('#master-key').isVisible()) {
    await page.fill('#master-key', masterKey);
    await page.click('#login-btn');
  }
  await page.waitForSelector('#admin-panel', { state: 'visible' });
  await page.waitForTimeout(800);
  await page.selectOption('#quiz-select', quizFile);
  await page.click('#switch-btn');
  await page.waitForSelector('#start-quiz-modal', { state: 'visible' });
  await page.waitForTimeout(600);
  const modal = page.locator('#start-quiz-modal .modal-content');
  await modal.screenshot({ path: path.join(shots, 'teacher-1-file-values.png') });

  const checkboxes: Record<string, string> = {
    show_right_answer: '#start-show-right-answer',
    show_answers_on_completion: '#start-show-answers-on-completion',
    randomize_questions: '#start-randomize-questions',
    show_final_list: '#start-show-final-list',
  };
  for (const [key, selector] of Object.entries(checkboxes)) {
    if (key in settings) {
      await page.setChecked(selector, Boolean(settings[key]), { force: true });
      await page.dispatchEvent(selector, 'change');
    }
  }
  if ('time_limit' in settings) {
    const limit = settings.time_limit as Record<string, number> | null;
    const mode = limit ? Object.keys(limit)[0] : 'none';
    await page.selectOption('#start-time-limit-mode', mode);
    if (limit) await page.fill('#start-time-limit-seconds', String(limit[mode]));
  }
  await modal.screenshot({ path: path.join(shots, 'teacher-2-chosen.png') });

  const [response] = await Promise.all([
    page.waitForResponse('**/api/admin/switch-quiz'),
    page.click('#start-quiz-confirm'),
  ]);
  const reply = (await response.json()) as { settings?: Record<string, unknown> };
  classroom.log('note', { student: 'teacher', detail: `started the quiz: ${JSON.stringify(reply)}` });
  teacher.page = page; // stays open: the teacher may approve students later
  return reply.settings;
}

/**
 * Click "approve" in the admin panel for each student at the planned time.
 *
 * approvals: {student: seconds after the start, or null = never}.
 * Agents are approved `agentDelay` seconds after they first show up in the waiting list.
 * started and deadline are clock() values.
 */
export async function teacherApproves(
  classroom: Classroom,
  approvals: Record<string, number | null>,
  agentNames: string[],
  agentDelay: number,
  started: number,
  deadline: number,
): Promise<void> {
  const page = classroom.students.get('teacher')!.page!;
  const planned = new Map(Object.entries(approvals).filter((entry): entry is [string, number] => entry[1] !== null));
  const agentsSeen = new Map<string, number>();
  const agentsLeft = new Set(agentNames);
  while ((planned.size || agentsLeft.size) && clock() < deadline) {
    const now = clock() - started;
    const due = [...planned].filter(([, at]) => now >= at).map(([name]) => name);
    for (const name of agentsLeft) {
      if (await page.locator('.pending-user-item', { hasText: `👤 ${name}` }).count()) {
        if (!agentsSeen.has(name)) agentsSeen.set(name, now);
        if (now - agentsSeen.get(name)! >= agentDelay) due.push(name);
      }
    }
    for (const name of due) {
      const item = page.locator('.pending-user-item', { hasText: `👤 ${name}` });
      if ((await item.count()) === 0) continue; // not registered yet: try again on the next round
      await item.first().locator('.approve-btn').click();
      classroom.log('note', { student: 'teacher', detail: `approved ${name} at ${now.toFixed(1)} s` });
      planned.delete(name);
      agentsLeft.delete(name);
    }
    await sleep(0.5);
  }
}
