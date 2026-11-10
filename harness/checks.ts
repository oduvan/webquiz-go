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
    if (!did.startsWith(plain(recorded.cmd)) || (hasArg && !did.includes(plain(recorded.arg))))  {
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

export function loadAnswerRows(runDir: string): AnswerRow[] {
  const dataDir = path.join(runDir, 'data');
  const files = existsSync(dataDir)
    ? readdirSync(dataDir)
        .filter((name) => name.endsWith('.csv') && !name.endsWith('.users.csv') && !name.endsWith('.answers_with_users.csv'))
        .sort()
    : [];
  const rows: AnswerRow[] = [];
  for (const name of files) {
    const [header, ...records] = parseCSV(readFileSync(path.join(dataDir, name), 'utf-8').replace(/^﻿/, ''));
    if (!header) continue;
    for (const record of records) {
      const item: Record<string, string> = {};
      header.forEach((column, index) => (item[column] = record[index] ?? ''));
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

/** Run every check on a finished run and write report.md. Returns the number of problems found. */
export async function analyze(runDirArg: string): Promise<number> {
  const runDir = path.resolve(runDirArg);
  const meta: Json = JSON.parse(readFileSync(path.join(runDir, 'meta.json'), 'utf-8'));
  if (!('expectations' in meta)) {
    // runs recorded before scenarios had expectations
    const { SCENARIOS } = await import('./scenarios');
    meta.expectations = SCENARIOS[meta.scenario]?.expectations ?? {};
  }
  const events = loadEvents(runDir);
  const rows = loadAnswerRows(runDir);
  const serverLog = readFileSync(path.join(runDir, 'server.log'), 'utf-8');
  const questionText: Record<string, string> = {};
  for (const [key, value] of Object.entries(meta.question_text)) questionText[String(Number(key))] = value as string;
  const textOf = (id: unknown): string | undefined => questionText[String(id)];

  // Which user_id belongs to which student, from the register replies
  const userOf: Record<string, string> = {};
  const pendingRegister = new Map<number, string>();
  for (const event of events) {
    if (event.kind === 'request' && event.path === '/api/register') {
      pendingRegister.set(event.req_id, event.student);
    } else if (event.kind === 'response' && pendingRegister.has(event.req_id)) {
      const body = event.body;
      if (body && typeof body === 'object' && body.user_id) userOf[pendingRegister.get(event.req_id)!] = body.user_id;
    }
  }

  // Submits with their replies
  const requests = new Map<number, Json>();
  for (const event of events) if (event.kind === 'request') requests.set(event.req_id, event);
  const submits: Submit[] = [];
  for (const event of events) {
    if (event.kind === 'response' && requests.get(event.req_id)?.path === '/api/submit-answer') {
      const body = requests.get(event.req_id).body || {};
      submits.push({
        user_id: body.user_id,
        question: textOf(body.question_id) ?? `#${body.question_id}`,
        status: event.status,
        response: event.body && typeof event.body === 'object' ? event.body : {},
      });
    }
  }

  // Browser-side time per answer: first question-start sent -> the accepted answer sent
  const firstStart = new Map<string, number>();
  const acceptedSend = new Map<string, number>();
  const latencies: number[] = [];
  for (const event of events) {
    if (event.kind !== 'response' || event.status !== 200) continue;
    const request = requests.get(event.req_id) ?? {};
    const body = request.body && typeof request.body === 'object' ? request.body : {};
    const key = pairKey(body.user_id, textOf(body.question_id));
    if (request.path === '/api/question-start') {
      if (!firstStart.has(key)) firstStart.set(key, request.t);
    } else if (request.path === '/api/submit-answer') {
      if (!acceptedSend.has(key)) acceptedSend.set(key, request.t);
      latencies.push(event.t - request.t);
    }
  }
  const measured = new Map<string, number>();
  for (const [key, sent] of acceptedSend) {
    if (firstStart.has(key)) measured.set(key, sent - firstStart.get(key)!);
  }

  // Rows the server stored by itself: the background check, and whole-quiz ends
  const allowed = new Set<string>();
  for (const match of serverLog.matchAll(/Time ran out for user (\d+) on question (\d+)/g)) {
    allowed.add(pairKey(match[1], textOf(match[2])));
  }
  const ended = new Set([...serverLog.matchAll(/Whole-quiz time is up for user (\d+)/g)].map((m) => m[1]));
  for (const item of rows) {
    if (ended.has(item.user_id) && item.selected_answer === '') allowed.add(pairKey(item.user_id, item.question));
  }

  // Last screen of each student
  const lastScreen: Record<string, string> = {};
  const driverSteps = new Map<string, Map<number, DriverStep>>();
  for (const event of events) {
    if (event.kind === 'step') {
      // The agent may quote anything the tool printed for the step, not only the page text
      const steps = driverSteps.get(event.student) ?? new Map<number, DriverStep>();
      steps.set(event.step, { ...event, screen: formatStep(event as StepRecord) });
      driverSteps.set(event.student, steps);
      lastScreen[event.student] = event.screen ?? '';
    }
  }
  const finalScores: Record<string, [number, number]> = {};
  for (const [student, screen] of Object.entries(lastScreen)) {
    const score = parseScreenScore(screen);
    if (score && student in userOf) finalScores[userOf[student]] = score;
  }

  // Live stats: last state per user and question
  const finalStates = new Map<string, string>();
  for (const event of events) {
    const message = event.kind === 'live' ? event.message : null;
    if (message && (message.type === 'state_update' || message.type === 'user_registered')) {
      finalStates.set(pairKey(message.user_id, textOf(message.question_id)), message.state);
    }
  }

  const limits: Record<string, number> = {};
  for (const [qid, limit] of Object.entries(meta.limits)) limits[questionText[String(Number(qid))]] = limit as number;

  const sections: Record<string, string[]> = {
    'Each question stored once per student': checkUniqueAnswers(rows),
    'Empty (timed-out) answers are failed': checkTimeoutRows(rows),
    'Answers arrived within limit + grace': checkAnswerTimes(rows, limits, meta.grace),
    'Requests and stored answers agree': checkSubmitsMatchRows(submits, rows, allowed),
    'Results screen matches stored answers': checkScreenScores(finalScores, rows, Object.keys(questionText).length),
    'Live stats match stored answers': checkLiveStats(finalStates, rows),
    'Server time per answer matches the browser (±1.5 s)': checkTimeAccuracy(rows, measured),
    'Planned outcomes of the scripted edge cases': checkExpectations(meta.expectations ?? {}, rows, userOf, questionText),
  };

  const agentSections: Record<string, [number, string[]]> = {};
  const agentsDir = path.join(runDir, 'agents');
  const reports = existsSync(agentsDir) ? readdirSync(agentsDir).filter((n) => n.endsWith('.md')).sort() : [];
  for (const file of reports) {
    const student = file.slice(0, -3);
    const agentSteps = parseAgentReport(readFileSync(path.join(agentsDir, file), 'utf-8'));
    const problems = checkAgentAlignment(agentSteps, driverSteps.get(student) ?? new Map());
    if (!agentSteps.length) problems.push("the report has no 'STEP n | DID: ... | SAW: ...' lines");
    agentSections[student] = [agentSteps.length, problems];
  }

  const liveCount = events.filter((e) => e.kind === 'live').length;
  const coverage = [
    `- Results screens read: ${Object.keys(finalScores).length} of ${Object.keys(userOf).length} students`,
    `- Live stats messages recorded: ${liveCount}`,
    `- Answer requests with a reply: ${submits.length}`,
  ];
  if (latencies.length) {
    const ordered = [...latencies].sort((a, b) => a - b);
    coverage.push(
      `- Answer reply time: median ${ordered[Math.floor(ordered.length / 2)].toFixed(2)} s, ` +
        `slowest ${ordered[ordered.length - 1].toFixed(2)} s (from sending the accepted answer to its reply)`,
    );
  }
  writeReport(runDir, meta, sections, agentSections, rows, userOf, coverage);
  writeTimelines(runDir, events, rows, userOf);
  return Object.values(sections).reduce((sum, problems) => sum + problems.length, 0);
}

function writeReport(
  runDir: string,
  meta: Json,
  sections: Record<string, string[]>,
  agentSections: Record<string, [number, string[]]>,
  rows: AnswerRow[],
  userOf: Record<string, string>,
  coverage: string[],
): void {
  const lines = [`# Harness run: ${meta.scenario}`, '', meta.description ?? '', ''];
  lines.push(`- Students: ${Object.keys(userOf).length} registered (${Object.keys(userOf).sort().join(', ')})`);
  lines.push(
    `- Stored answers: ${rows.length}, of them timed out (empty): ${rows.filter((r) => r.selected_answer === '').length}`,
  );
  lines.push(`- Settings used: \`${JSON.stringify(meta.settings ?? null)}\``);
  lines.push(...coverage);
  lines.push('');
  lines.push('## Checks: browser record vs server data');
  lines.push('');
  for (const [title, problems] of Object.entries(sections)) {
    const mark = problems.length === 0 ? 'PASS' : `FAIL (${problems.length})`;
    lines.push(`- **${mark}** ${title}`);
    for (const problem of problems) lines.push(`  - ${problem}`);
  }
  lines.push('');
  lines.push('## Agent reports vs driver record');
  lines.push('');
  if (Object.keys(agentSections).length === 0) lines.push('No agent reports in this run.');
  for (const [student, [count, problems]] of Object.entries(agentSections)) {
    const mark = problems.length === 0 ? 'aligned' : `${problems.length} difference(s)`;
    lines.push(`- **${student}**: ${count} reported steps, ${mark}`);
    for (const problem of problems) lines.push(`  - ${problem}`);
  }
  lines.push('');
  lines.push('Per-student timelines: `timelines/<student>.md`. Screenshots: `shots/`.');
  writeFileSync(path.join(runDir, 'report.md'), lines.join('\n') + '\n', 'utf-8');
}

/** One file per student: driver steps, requests and the stored rows, in time order. */
function writeTimelines(runDir: string, events: Json[], rows: AnswerRow[], userOf: Record<string, string>): void {
  const out = path.join(runDir, 'timelines');
  mkdirSync(out, { recursive: true });
  const requests = new Map<number, Json>();
  for (const event of events) if (event.kind === 'request') requests.set(event.req_id, event);
  const perStudent = new Map<string, [number, string][]>();
  const add = (student: string, at: number, line: string) => {
    const list = perStudent.get(student) ?? [];
    list.push([at, line]);
    perStudent.set(student, list);
  };
  const pad = (value: number) => value.toFixed(1).padStart(7);
  for (const event of events) {
    const student = event.student;
    if (!student) continue;
    if (event.kind === 'step') {
      const screen = String(event.screen ?? '')
        .split('\n')
        .filter((line) => line.trim())
        .join(' / ')
        .slice(0, 300);
      const arg = event.arg !== undefined && event.arg !== null ? ` ${event.arg}` : '';
      const error = event.error ? ` ERROR: ${event.error}` : '';
      const start = event.t_start ?? event.t;
      add(
        student,
        start,
        `${pad(start)}s  STEP ${event.step}: ${event.cmd}${arg}${error}  (done at ${event.t.toFixed(1)}s)\n          screen: ${screen}`,
      );
    } else if (event.kind === 'response') {
      const request = requests.get(event.req_id) ?? {};
      if (['/api/submit-answer', '/api/question-start', '/api/register'].includes(request.path)) {
        add(
          student,
          request.t,
          `${pad(request.t)}s  ${request.method} ${request.path} ${JSON.stringify(request.body)}` +
            ` -> ${event.status} at ${event.t.toFixed(1)}s ${JSON.stringify(event.body ?? null).slice(0, 200)}`,
        );
      }
    } else if (['request_failed', 'network', 'note'].includes(event.kind)) {
      add(student, event.t, `${pad(event.t)}s  ${String(event.kind).toUpperCase()}: ${event.detail ?? ''}`);
    }
  }
  for (const [student, entries] of perStudent) {
    const user = userOf[student];
    const stored = rows.filter((r) => r.user_id === user);
    const ordered = [...entries].sort((a, b) => a[0] - b[0]).map(([, line]) => line);
    const text = [`# ${student} (user_id ${user ?? 'None'})`, '', '```', ...ordered, '```', '', '## Stored answers', ''];
    for (const item of stored) {
      text.push(
        `- ${item.question} → '${item.selected_answer}', correct=${pyBool(item.is_correct)}, time=${fixed1(item.time_taken)}s`,
      );
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
