// One case on one device and theme: a server, a teacher who starts the quiz through
// the API, a main student in a real browser (every screen is a screenshot with its
// checks) and a second student who answers through the API, after the main one.

import { randomBytes } from 'node:crypto';
import path from 'node:path';
import type { Browser, BrowserContext, Page } from 'playwright';
import { caseConfig, describeCase, FIELDS, type MatrixCase, type Screen } from './matrix-cases';
import {
  AdminApi,
  api,
  newSession,
  pageState,
  readChoice,
  readSnap,
  scrollAndMeasure,
  readStoredData,
  settle,
  shoot,
  shownSeconds,
  sleep,
  startServer,
  until,
  watchPage,
  type ChoiceSeen,
  type RunningServer,
} from './matrix-page';
import {
  ATTACHMENT_TEXT,
  oppositeSettings,
  questionById,
  questionByText,
  QUESTIONS,
  QUIZ_FILE,
  quizFile,
  startSettings,
  WHOLE_QUIZ_SAFE_SECONDS,
  WHOLE_QUIZ_STOP_AT,
  type Answer,
  type MatrixQuestion,
  type QuizSettings,
} from './matrix-quiz';
import { RunRecord } from './matrix-report';
import {
  checkAttachmentDownload,
  checkAutoAdvance,
  checkChoice,
  checkFooter,
  checkImageSizes,
  checkTopControls,
  checkCsvAgainstScreen,
  checkCsvRows,
  checkExplanation,
  checkFeedback,
  checkNoSideScroll,
  checkNotApprovedYet,
  checkQuestion,
  checkQuestionTimeUp,
  checkRegistration,
  checkResults,
  checkTheme,
  checkWaiting,
  effectiveLimit,
  expectedResults,
  explanationShown,
  feedbackShown,
  fieldName,
  orderProblems,
  same,
  textsFor,
  truth,
  type AdvanceSeen,
  type CheckResult,
  type CsvRow,
  type Outcome,
  type Snap,
} from './matrix-rules';

export interface MatrixEnv {
  browser: Browser;
  binary: string;
  version: string;
  runDir: string;
}

interface Student {
  username: string;
  fields: Record<string, string>;
}

const MAIN: Student = { username: 'Олена Коваль', fields: { Клас: '7-А', Школа: 'Ліцей №1' } };
const SECOND: Student = { username: 'Тарас Шевчук', fields: { Клас: '7-Б', Школа: 'Ліцей №2' } };

/** The settings the switch-quiz reply must report (server.py:975-987). */
function runningSettings(settings: QuizSettings): Record<string, unknown> {
  return startSettings(settings);
}

/** The answer as the API takes it: an index, a sorted list of indexes, or text. */
export function apiAnswer(question: MatrixQuestion, answer: Answer): unknown {
  if ('text' in answer) return answer.text;
  return Array.isArray(question.correct_answer) ? [...answer.indices].sort((a, b) => a - b) : answer.indices[0];
}

async function registerByApi(base: string, matrixCase: MatrixCase, who: Student): Promise<{ userId: string; order: number[] | null }> {
  const body: Record<string, string> = { username: who.username, registration_token: randomBytes(16).toString('hex') };
  if (matrixCase.fields) for (const label of FIELDS) body[fieldName(label)] = who.fields[label];
  const reply = await api(base, 'POST', '/api/register', body);
  if (reply.status !== 200) throw new Error(`the second student could not register: ${reply.status} ${JSON.stringify(reply.json)}`);
  return { userId: String(reply.json.user_id), order: Array.isArray(reply.json.question_order) ? reply.json.question_order : null };
}

/** The second student answers every question right, in its own order (server.py:2500-2561, 2110-2235). */
async function finishByApi(base: string, userId: string, order: number[]): Promise<CheckResult[]> {
  const statuses: number[] = [];
  let completed = false;
  for (const id of order) {
    const question = questionById(id);
    const started = await api(base, 'POST', '/api/question-start', { user_id: userId, question_id: id });
    const reply = await api(base, 'POST', '/api/submit-answer', { user_id: userId, question_id: id, selected_answer: apiAnswer(question, question.right) });
    statuses.push(started.status, reply.status);
    completed = reply.json?.test_completed === true;
  }
  return [
    same('second student: every request accepted', 'server.py:2500-2561, 2110-2235', order.flatMap(() => [200, 200]), statuses),
    same('second student: test_completed after the last answer', 'server.py:2357, 2370', true, completed),
  ];
}

async function clickByText(page: Page, selector: string, label: string): Promise<void> {
  const target = page.locator(selector).filter({ hasText: label }).first();
  await target.click({ timeout: 5000 });
}

/** Choose the answer, note what the page shows before sending, then press "Submit". */
async function giveAnswer(page: Page, answer: Answer): Promise<ChoiceSeen> {
  if ('text' in answer) {
    await page.fill('#text-answer-input', answer.text);
  } else {
    for (const index of answer.indices) await page.click(`#current-question-container #option_${index}`, { timeout: 5000 });
  }
  const seen = await readChoice(page);
  await page.click('#submit-answer-btn', { timeout: 5000 });
  return seen;
}

/** Watch the page after an answer until the next question or the results come, without clicking. */
async function observeAdvance(page: Page, fromQuestion: string, timeoutMs: number, startedAt: number): Promise<AdvanceSeen> {
  const seen: AdvanceSeen = { advanced: false, sawContinue: false, sawMarks: false, sawExplanation: false, millis: 0 };
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    const state = await pageState(page);
    seen.sawContinue ||= state.contVisible;
    seen.sawMarks ||= state.marks > 0;
    seen.sawExplanation ||= state.explanation;
    if (state.results || (state.question !== null && state.question !== fromQuestion)) {
      seen.advanced = true;
      break;
    }
    await sleep(40);
  }
  seen.millis = Date.now() - startedAt;
  return seen;
}

function isNextStep(question: MatrixQuestion) {
  return (state: { results: boolean; explanation: boolean; question: string | null }) =>
    state.results || state.explanation || (state.question !== null && state.question !== question.question);
}

/** Wait for the stored rows of both students (the server writes CSV every 5 s, server.py:1558-1570). */
async function waitForStoredData(dataDir: string, users: string[], rowsEach: number): Promise<ReturnType<typeof readStoredData>> {
  const deadline = Date.now() + 25_000;
  let data = readStoredData(dataDir);
  while (Date.now() < deadline) {
    const ready = users.every(
      (user) =>
        data.answers.filter((row) => row.user_id === user).length >= rowsEach &&
        data.users.some((row) => row.user_id === user && Number(row.total_questions_asked) >= rowsEach),
    );
    if (ready) break;
    await sleep(500);
    data = readStoredData(dataDir);
  }
  return data;
}

function csvRows(records: Record<string, string>[], user: string): CsvRow[] {
  return records
    .filter((row) => row.user_id === user)
    .map((row) => ({
      user_id: row.user_id,
      question: row.question,
      selected_answer: row.selected_answer,
      correct_answer: row.correct_answer,
      is_correct: row.is_correct,
    }));
}

export async function runStudentCase(env: MatrixEnv, matrixCase: MatrixCase, screen: Screen): Promise<RunRecord> {
  const started = Date.now();
  const record = new RunRecord(matrixCase.name, screen.device, screen.theme, matrixCase.language, describeCase(matrixCase));
  const folder = `${matrixCase.name}/${screen.device}-${screen.theme}`;
  const serverDir = path.join(env.runDir, 'servers', `${matrixCase.name}-${screen.device}-${screen.theme}`);
  const fileSettings =
    matrixCase.settingsFrom === 'none' ? null : matrixCase.settingsFrom === 'start' ? oppositeSettings(matrixCase.settings) : matrixCase.settings;
  let server: RunningServer | null = null;
  let context: BrowserContext | null = null;
  try {
    server = await startServer(env.binary, serverDir, { [QUIZ_FILE]: quizFile(fileSettings) }, caseConfig(matrixCase));
    record.notes.push(`server folder: ${path.relative(env.runDir, serverDir)}`);
    const admin = new AdminApi(server.url);
    await admin.login();
    const switched = await admin.call('POST', '/api/admin/switch-quiz', {
      quiz_filename: QUIZ_FILE,
      ...(matrixCase.settingsFrom === 'start' ? { settings: startSettings(matrixCase.settings) } : {}),
    });
    record.add('start', [
      same('the teacher starts the quiz (switch-quiz)', 'server.py:2869-2898', 200, switched.status),
      same(
        matrixCase.settingsFrom === 'start' ? 'running settings: the start settings win over the file' : 'running settings: the quiz file values',
        'server.py:954-987, 1029-1030, 1165-1180',
        runningSettings(matrixCase.settings),
        switched.json?.settings ?? null,
      ),
    ]);

    const second = await registerByApi(server.url, matrixCase, SECOND);
    if (matrixCase.approve) {
      const approved = await admin.call('PUT', '/api/admin/approve-user', { user_id: second.userId });
      record.add('start', [same('second student approved', 'server.py:2989-3057', 200, approved.status)]);
    }

    context = await newSession(env.browser, screen, record, env.version);
    const page = await context.newPage();
    watchPage(page, record, 'student page');
    const mainReply: { userId: string | null; order: number[] | null } = { userId: null, order: null };
    page.on('response', (response) => {
      if (!response.url().endsWith('/api/register') || response.status() !== 200) return;
      response
        .json()
        .then((json) => {
          mainReply.userId = String(json.user_id);
          if (Array.isArray(json.question_order)) mainReply.order = json.question_order;
        })
        .catch(() => undefined);
    });

    const outcomes = await mainStudent(env, matrixCase, screen, record, folder, page, admin, mainReply);
    const order = mainReply.order ?? QUESTIONS.map((q) => q.id);

    // The results screen while the second student has not finished
    record.where = 'results';
    await until(page, (state) => state.results, 90_000, 'the results');
    await sleep(300);
    await settle(page);
    let snap = await readSnap(page);
    const wholeQuizEnded = matrixCase.settings.time_limit === 'whole_quiz';
    const resultsChecks = [
      ...checkResults(matrixCase, expectedResults(matrixCase, outcomes, false), snap),
      checkTheme(screen.theme, snap),
      ...checkNoSideScroll(screen.device, snap),
    ];
    await shoot(
      page,
      record,
      env.runDir,
      folder,
      'results',
      wholeQuizEnded ? 'Results after the whole-quiz time ran out (the second student is not done yet)' : 'Results (the second student is not done yet)',
      record.add('results', resultsChecks),
    );

    // The page scrolls: in 1.19 the name and the theme button go with it
    const scroll = await scrollAndMeasure(page, 260);
    const scrollChecks = record.add('results-scrolled', checkTopControls(scroll));
    if (scroll.scrolled > 0) {
      await shoot(
        page,
        record,
        env.runDir,
        folder,
        'results-scrolled',
        `Results scrolled down by ${scroll.scrolled} px (in 1.19 the name and the theme button scroll away with the page)`,
        scrollChecks,
        { fullPage: false },
      );
    }
    await page.evaluate('window.scrollTo(0, 0)');

    // The second student finishes after the main one
    record.where = 'second student';
    record.add('second student', await finishByApi(server.url, second.userId, second.order ?? QUESTIONS.map((q) => q.id)));

    if (matrixCase.settings.show_answers_on_completion) {
      record.where = 'results after reload';
      // The note's own button reloads the page (index.html:2404)
      await Promise.all([page.waitForEvent('load', { timeout: 20_000 }), clickByText(page, '#results-content button', textsFor(matrixCase).reload_page)]);
      await until(page, (state) => state.results, 20_000, 'the results after the reload');
      await sleep(300);
      await settle(page);
      snap = await readSnap(page);
      const revealChecks = [
        ...checkResults(matrixCase, expectedResults(matrixCase, outcomes, true), snap),
        ...checkNoSideScroll(screen.device, snap),
      ];
      await shoot(
        page,
        record,
        env.runDir,
        folder,
        'results-revealed',
        'Results after everyone finished and a reload: the correct answers are shown',
        record.add('results-revealed', revealChecks),
      );
    } else {
      // The same results come back after a reload (index.html:1454-1456, 2224-2236)
      record.where = 'results after reload';
      await page.reload();
      await until(page, (state) => state.results, 20_000, 'the results after the reload');
      await sleep(300);
      await settle(page);
      snap = await readSnap(page);
      record.add('results after reload', checkResults(matrixCase, expectedResults(matrixCase, outcomes, true), snap));
    }

    // The stored data
    record.where = 'stored data';
    const data = await waitForStoredData(path.join(serverDir, 'data'), [mainReply.userId ?? '?', second.userId], QUESTIONS.length);
    const mainRows = csvRows(data.answers, mainReply.userId ?? '?');
    const secondRows = csvRows(data.answers, second.userId);
    const secondOutcomes: Outcome[] = (second.order ?? QUESTIONS.map((q) => q.id)).map((id, index) => ({
      id,
      position: index + 1,
      answer: questionById(id).right,
    }));
    const mainUser = data.users.find((row) => row.user_id === mainReply.userId) ?? null;
    record.add('stored data', [
      ...checkCsvRows(outcomes, mainRows, 'main student'),
      ...checkCsvRows(secondOutcomes, secondRows, 'second student'),
      ...checkCsvAgainstScreen(matrixCase, snap, mainRows, mainUser),
    ]);
    record.notes.push(`CSV files: ${data.files.join(', ') || '(none)'}`);

    // The question order
    const randomize = matrixCase.settings.randomize_questions;
    const secondOrder = second.order ?? QUESTIONS.map((q) => q.id);
    const shownOrder = outcomes.filter((outcome) => outcome.position > 0).map((outcome) => outcome.id);
    const orderChecks: CheckResult[] = [
      same('main student: order is valid', 'server.py:1844-1894, 2450-2454', [], orderProblems(order, randomize)),
      same('second student: order is valid', 'server.py:1844-1894, 2450-2454', [], orderProblems(secondOrder, randomize)),
      same('the page shows the questions in the server order', 'index.html:1390-1417, 1546-1549', order.slice(0, shownOrder.length), shownOrder),
      same(
        randomize ? 'the server sends each student an order' : 'no order is sent without randomize_questions',
        'server.py:1975-1978, 2042-2044',
        randomize,
        mainReply.order !== null,
      ),
    ];
    if (randomize) {
      orderChecks.push(
        truth('the two students get different orders', 'server.py:1885-1886 (random.shuffle)', order.join(',') !== secondOrder.join(','), 'different', `${order.join(',')} / ${secondOrder.join(',')}`),
      );
    }
    record.add('order', orderChecks);
    record.notes.push(`orders: main ${order.join(',')}; second ${secondOrder.join(',')}`);
  } catch (error) {
    record.error = error instanceof Error ? error.message.split('\n')[0] : String(error);
    record.add(record.where, [truth('the run finished', '(the matrix itself)', false, 'no error', record.error)]);
  } finally {
    if (context) await context.close().catch(() => undefined);
    if (server) await server.stop().catch(() => undefined);
    record.seconds = (Date.now() - started) / 1000;
  }
  return record;
}

/** Register, wait for approval, and go through the questions. Returns what happened to each question. */
async function mainStudent(
  env: MatrixEnv,
  matrixCase: MatrixCase,
  screen: Screen,
  record: RunRecord,
  folder: string,
  page: Page,
  admin: AdminApi,
  mainReply: { userId: string | null; order: number[] | null },
): Promise<Outcome[]> {
  const T = textsFor(matrixCase);
  const settings = matrixCase.settings;
  const base = new URL(admin.base);
  const snapChecks = (snap: Snap) => [checkTheme(screen.theme, snap), ...checkNoSideScroll(screen.device, snap)];

  record.where = 'registration';
  await page.goto(base.origin + '/');
  await until(page, (state) => state.registration, 20_000, 'the registration form');
  await settle(page);
  let snap = await readSnap(page);
  await shoot(
    page,
    record,
    env.runDir,
    folder,
    'registration',
    'Registration form',
    record.add('registration', [...checkRegistration(matrixCase, snap), checkFooter(env.version, snap), ...snapChecks(snap)]),
  );

  await page.fill('#username', MAIN.username);
  if (matrixCase.fields) {
    for (const label of FIELDS) await page.fill(`#registration .registration-field[data-field-name="${fieldName(label)}"]`, MAIN.fields[label]);
  }
  await page.click('#registration .register-btn');

  if (matrixCase.approve) {
    record.where = 'waiting';
    await until(page, (state) => state.waiting, 15_000, 'the waiting screen');
    await settle(page);
    snap = await readSnap(page);
    const values = [MAIN.username, ...(matrixCase.fields ? FIELDS.map((label) => MAIN.fields[label]) : [])];
    await shoot(page, record, env.runDir, folder, 'waiting', 'Waiting for approval', record.add('waiting', [...checkWaiting(matrixCase, snap, values), ...snapChecks(snap)]));

    record.where = 'waiting: Check before approval';
    await clickByText(page, '#waiting-approval button', T.check_status);
    await until(page, (state) => !!state.waitingMessage, 10_000, 'the "not approved yet" message');
    await settle(page);
    snap = await readSnap(page);
    await shoot(page, record, env.runDir, folder, 'waiting-not-approved', 'Waiting: "Check" pressed before the teacher approved', record.add('waiting-not-approved', checkNotApprovedYet(matrixCase, snap)));

    const deadline = Date.now() + 5000;
    while (!mainReply.userId && Date.now() < deadline) await sleep(50);
    const approved = await admin.call('PUT', '/api/admin/approve-user', { user_id: mainReply.userId });
    record.add('approval', [same('the teacher approves the main student', 'server.py:2989-3057', 200, approved.status)]);
    await clickByText(page, '#waiting-approval button', T.check_status);
  }

  const outcomes: Outcome[] = [];
  let position = 0;
  for (;;) {
    record.where = `after question ${position}`;
    const state = await until(
      page,
      (s) => s.results || (s.question !== null && !outcomes.some((o) => questionById(o.id).question === s.question) && !s.timeUp),
      position === 0 ? 20_000 : 90_000,
      'the next question or the results',
    );
    if (state.results) break;
    const question = questionByText(state.question!);
    if (!question) throw new Error(`unknown question on the screen: ${state.question}`);
    position++;
    const tag = `pos${String(position).padStart(2, '0')}-q${question.id}`;
    record.where = `${tag} question`;
    const limit = effectiveLimit(settings, question);
    await settle(page);
    // The countdowns get their time from the question-start reply
    const timersReady = (s: { questionTimer: string; questionTimerVisible: boolean; quizTimer: string | null }) =>
      (limit === 0 || (s.questionTimerVisible && s.questionTimer !== '')) && (settings.time_limit !== 'whole_quiz' || !!s.quizTimer);
    await until(page, timersReady, 5000, 'the countdowns').catch(() => undefined);
    snap = await readSnap(page);
    const questionChecks = [
      ...checkQuestion(matrixCase, question, position, snap, MAIN.username),
      ...checkImageSizes(screen.device, snap),
      ...snapChecks(snap),
    ];
    if (position === 1 && !matrixCase.approve) {
      questionChecks.push(same('no waiting screen without approve', 'index.html:1551-1562; server.py:1961', false, snap.visible.waiting));
    }
    await shoot(page, record, env.runDir, folder, `${tag}-question`, `Question ${position} of ${QUESTIONS.length}: question ${question.id}, ${question.label}`, record.add(`${tag}-question`, questionChecks));
    if (question.file && snap.question?.file) {
      const response = await fetch(new URL(snap.question.file.href, base.origin + '/'));
      record.add(`${tag}-question`, checkAttachmentDownload(question, ATTACHMENT_TEXT, {
        status: response.status,
        disposition: response.headers.get('content-disposition'),
        body: await response.text(),
      }));
    }

    const quizLeft = shownSeconds(snap.quizTimer.visible ? snap.quizTimer.text : null);
    const idleForQuestionLimit = settings.time_limit === 'per_question' && question.time_limit !== undefined;
    const idleForQuizLimit =
      settings.time_limit === 'whole_quiz' && (position >= WHOLE_QUIZ_STOP_AT || (quizLeft !== null && quizLeft <= WHOLE_QUIZ_SAFE_SECONDS));

    if (idleForQuizLimit) {
      record.where = `${tag} waiting for the quiz time to run out`;
      record.notes.push(`whole quiz: stopped answering at position ${position} with ${quizLeft ?? '?'} s left`);
      await until(page, (s) => s.results, 90_000, 'the results after the quiz time ran out');
      const order = mainReply.order ?? QUESTIONS.map((q) => q.id);
      for (const id of order) {
        if (!outcomes.some((o) => o.id === id)) outcomes.push({ id, position: id === question.id ? position : 0, answer: null });
      }
      break;
    }

    if (idleForQuestionLimit) {
      record.where = `${tag} time-up`;
      await until(
        page,
        (s) => s.results || (s.timeUp && (!feedbackShown(settings) || s.contReady)),
        (limit + 15) * 1000,
        '"time is up"',
      );
      const seenAt = Date.now();
      snap = await readSnap(page);
      outcomes.push({ id: question.id, position, answer: null });
      await shoot(page, record, env.runDir, folder, `${tag}-time-up`, `Question ${position}: its own ${limit} s ran out`, record.add(`${tag}-time-up`, checkQuestionTimeUp(matrixCase, question, snap)));
      if (feedbackShown(settings)) {
        await afterFeedback(env, matrixCase, record, folder, page, question, tag, position, 'question-timeout');
      } else {
        const seen = await observeAdvance(page, question.question, 10_000, seenAt);
        record.add(`${tag}-time-up`, checkAutoAdvance(seen, true));
      }
      continue;
    }

    record.where = `${tag} answer`;
    const submittedAt = Date.now();
    const choice = await giveAnswer(page, question.main);
    outcomes.push({ id: question.id, position, answer: question.main });
    record.add(`${tag}-answer`, checkChoice(question, question.main, choice));
    if (feedbackShown(settings)) {
      await until(page, (s) => s.contReady || s.results, 15_000, 'the feedback after the answer');
      await settle(page);
      snap = await readSnap(page);
      await shoot(page, record, env.runDir, folder, `${tag}-feedback`, `Question ${position}: after the answer`, record.add(`${tag}-feedback`, [...checkFeedback(matrixCase, question, question.main, snap), ...snapChecks(snap)]));
      await afterFeedback(env, matrixCase, record, folder, page, question, tag, position, 'answer');
    } else {
      const seen = await observeAdvance(page, question.question, 10_000, submittedAt);
      record.add(`${tag}-answer`, checkAutoAdvance(seen, false));
    }
  }
  return outcomes;
}

/** Press "Continue"; then the explanation step comes (and "Next"), or the next question. */
async function afterFeedback(
  env: MatrixEnv,
  matrixCase: MatrixCase,
  record: RunRecord,
  folder: string,
  page: Page,
  question: MatrixQuestion,
  tag: string,
  position: number,
  way: 'answer' | 'question-timeout',
): Promise<void> {
  record.where = `${tag} continue`;
  const state = await pageState(page);
  if (state.results) return; // the whole-quiz time ran out meanwhile
  await page.click('#continue-btn', { timeout: 5000 });
  const next = await until(page, isNextStep(question), 15_000, 'the step after "Continue"');
  const expected = explanationShown(matrixCase.settings, question, way);
  if (!expected) {
    record.add(`${tag}-continue`, [
      same(
        question.explanation ? 'no explanation step (show_explanation is off)' : 'no explanation step (the question has none)',
        'server.py:2391-2393; index.html:2176-2181',
        false,
        next.explanation,
      ),
    ]);
  }
  if (!next.explanation) {
    if (expected) record.add(`${tag}-continue`, [same('explanation step after "Continue"', 'server.py:2391-2393; index.html:2176-2181', true, false)]);
    return;
  }
  record.where = `${tag} explanation`;
  await until(page, (s) => s.explanationNext, 5000, 'the "Next" button').catch(() => undefined);
  await settle(page);
  const snap = await readSnap(page);
  const checks = expected ? [...checkExplanation(matrixCase, question, position, snap), ...checkNoSideScroll(record.device, snap)] : [];
  const caption = expected ? 'Question explanation (after "Continue")' : 'An explanation step that 1.19 would not show';
  await shoot(page, record, env.runDir, folder, `${tag}-explanation`, caption, record.add(`${tag}-explanation`, checks));
  await page.click('#explanation-next-btn', { timeout: 5000 });
  await until(page, (s) => s.results || (s.question !== null && s.question !== question.question), 15_000, 'the question after the explanation');
}
