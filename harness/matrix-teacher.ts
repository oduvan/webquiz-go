// The teacher's pages: admin login, admin panel, start pop-up, quiz editor (one
// screenshot per question type, and the explanation preview), live stats with a
// finished and an unfinished student, the file manager and the CSV table page.
//
// Each page gets a screenshot and checks: it loads without a JS error, follows the
// theme, has no horizontal scroll on the phone, and shows a few things 1.19 shows.

import path from 'node:path';
import type { BrowserContext, Page } from 'playwright';
import { FIELDS, type Screen } from './matrix-cases';
import { api, newSession, readStoredData, shoot, sleep, startServer, watchPage, type RunningServer } from './matrix-page';
import { QUESTIONS, QUIZ_FILE, quizFile, type QuizSettings } from './matrix-quiz';
import { RunRecord } from './matrix-report';
import { same, truth, type CheckResult } from './matrix-rules';
import type { Language } from './matrix-texts';
import type { MatrixEnv } from './matrix-student';
import { apiAnswer } from './matrix-student';

export interface TourScreen extends Screen {
  language: Language;
}

/** Every pair of device, theme and language appears in one of these four tours. */
export const TOURS: TourScreen[] = [
  { device: 'desktop', theme: 'light', language: 'uk' },
  { device: 'phone', theme: 'dark', language: 'en' },
  { device: 'desktop', theme: 'dark', language: 'en' },
  { device: 'phone', theme: 'light', language: 'uk' },
];

/** The quiz file the tour starts with (the start pop-up must show these values). */
export const TOUR_SETTINGS: QuizSettings = {
  show_right_answer: true,
  show_explanation: true,
  show_final_list: true,
  show_answers_on_completion: false,
  randomize_questions: false,
  time_limit: 'per_question',
};

const SECOND_QUIZ = {
  title: 'Друга вікторина',
  questions: [{ question: 'Скільки буде 2 + 2?', options: ['3', '4', '5'], correct_answer: 1 }],
};

const QUESTION_KIND_CAPTION: Record<string, string> = {
  single: 'one answer',
  multiple: 'several answers',
  'multiple-min': 'several answers with min_correct',
  'text-checker': 'text answer with a checker',
  'text-exact': 'text answer without a checker',
  image: 'question with an image',
  'image-options': 'image options',
  file: 'file attachment',
  sticky: 'sticks to the previous question',
  'own-limit': 'own time limit',
};

interface PageLook {
  dark: boolean;
  overflowX: number;
  text: string;
}

const LOOK_JS = String.raw`() => {
  const background = (el) => getComputedStyle(el).backgroundColor;
  let bg = background(document.body);
  if (/rgba\(\s*0,\s*0,\s*0,\s*0\s*\)|transparent/.test(bg)) bg = background(document.documentElement);
  const rgb = (bg.match(/[\d.]+/g) || ['255', '255', '255']).map(Number);
  return {
    dark: (0.2126 * rgb[0] + 0.7152 * rgb[1] + 0.0722 * rgb[2]) / 255 < 0.4,
    overflowX: document.documentElement.scrollWidth - document.documentElement.clientWidth,
    text: document.body.innerText || '',
  };
}`;

async function look(page: Page): Promise<PageLook> {
  return (await page.evaluate(`(${LOOK_JS})()`)) as PageLook;
}

/** The checks every teacher page gets. */
async function pageChecks(page: Page, record: RunRecord, problemsBefore: number, themeRule: string): Promise<CheckResult[]> {
  const seen = await look(page);
  const errors = record.problems
    .slice(problemsBefore)
    .filter((problem) => (problem.kind === 'console' || problem.kind === 'pageerror') && !problem.expected);
  const checks: CheckResult[] = [
    truth('no JS error on the page', '(every page)', errors.length === 0, 'no console error or uncaught exception', errors.map((e) => e.text).join(' / ') || 'none'),
    same('theme from localStorage "theme"', themeRule, record.theme === 'dark', seen.dark),
  ];
  if (record.device === 'phone') {
    checks.push(truth('no horizontal scroll on the phone', '(mobile layout)', seen.overflowX <= 1, 'page width <= screen width', `${seen.overflowX}px wider`));
  }
  return checks;
}

async function settled(page: Page, ms = 600): Promise<void> {
  await page.waitForLoadState('networkidle', { timeout: 5000 }).catch(() => undefined);
  await sleep(ms);
}

/** Wait until the short messages at the edge of the page are gone (each stays 5 s). */
async function noMessages(page: Page): Promise<void> {
  const deadline = Date.now() + 7000;
  while (Date.now() < deadline && (await page.locator('#message-area .message').count()) > 0) await sleep(200);
}

async function tourShot(
  page: Page,
  env: MatrixEnv,
  record: RunRecord,
  folder: string,
  screen: string,
  caption: string,
  checks: CheckResult[],
  options: { fullPage?: boolean } = {},
): Promise<void> {
  await noMessages(page);
  await shoot(page, record, env.runDir, folder, screen, caption, record.add(screen, checks), options);
}

/**
 * Choose the matrix quiz in the admin panel's list. Right after a switch the page reloads
 * the list and clears the choice (AdminApp.tsx confirmStartQuiz), so choose again until
 * the buttons that need one chosen quiz are enabled.
 */
async function chooseQuiz(page: Page): Promise<void> {
  await page.waitForLoadState('networkidle', { timeout: 5000 }).catch(() => undefined);
  for (let attempt = 0; attempt < 5; attempt++) {
    await page.selectOption('#quiz-select', QUIZ_FILE);
    await sleep(400);
    if (await page.locator('#edit-btn').isEnabled()) return;
  }
  throw new Error('the quiz could not be chosen in the admin panel list');
}

/** Two students through the API: one answers every question, one answers three. */
async function students(server: RunningServer): Promise<void> {
  const register = async (username: string) => {
    const body: Record<string, string> = { username, registration_token: `${username}-${Date.now()}` };
    for (const label of FIELDS) body[label.toLowerCase()] = '8-В';
    const reply = await api(server.url, 'POST', '/api/register', body);
    if (reply.status !== 200) throw new Error(`could not register ${username}: ${JSON.stringify(reply.json)}`);
    return String(reply.json.user_id);
  };
  const answer = async (userId: string, count: number) => {
    for (const question of QUESTIONS.slice(0, count)) {
      await api(server.url, 'POST', '/api/question-start', { user_id: userId, question_id: question.id });
      await api(server.url, 'POST', '/api/submit-answer', { user_id: userId, question_id: question.id, selected_answer: apiAnswer(question, question.right) });
    }
  };
  await answer(await register('Марія Бойко'), QUESTIONS.length);
  await answer(await register('Іван Мельник'), 3);
}

export async function runTeacherTour(env: MatrixEnv, tour: TourScreen): Promise<RunRecord> {
  const started = Date.now();
  const record = new RunRecord('teacher', tour.device, tour.theme, tour.language, `Teacher pages, language ${tour.language} (admin pages are not translated in 1.19)`);
  const folder = `teacher/${tour.device}-${tour.theme}-${tour.language}`;
  const serverDir = path.join(env.runDir, 'servers', `teacher-${tour.device}-${tour.theme}-${tour.language}`);
  let server: RunningServer | null = null;
  let context: BrowserContext | null = null;
  try {
    server = await startServer(
      env.binary,
      serverDir,
      { [QUIZ_FILE]: quizFile(TOUR_SETTINGS), 'second.yaml': SECOND_QUIZ },
      { language: tour.language, registration: { approve: false, fields: FIELDS, disabled_fields: ['Телефон'], username_label: "Прізвище та ім'я" } },
    );
    record.notes.push(`server folder: ${path.relative(env.runDir, serverDir)}`);
    context = await newSession(env.browser, tour, record, env.version);
    const page = await context.newPage();
    watchPage(page, record, 'admin page');

    // 1. Login
    record.where = 'admin login';
    let before = record.problems.length;
    await page.goto(server.url + '/admin/');
    await page.locator('#master-key').waitFor({ state: 'visible', timeout: 15_000 });
    await settled(page);
    let seen = await look(page);
    await tourShot(page, env, record, folder, 'admin-login', 'Admin login', [
      ...(await pageChecks(page, record, before, 'admin.html:1559-1566')),
      truth('admin page title (not translated)', 'admin.html:1227; server.py:4606-4671 (no language)', seen.text.includes('Панель Адміністратора WebQuiz'), '🔧 Панель Адміністратора WebQuiz', seen.text.split('\n')[0]),
      same('login button (not translated)', 'admin.html:1236', 'Увійти', (await page.locator('#login-btn, #auth-section button').first().innerText()).trim()),
    ]);

    // 2. The admin panel with the quiz list
    record.where = 'admin panel';
    before = record.problems.length;
    await page.fill('#master-key', 'matrix-key');
    await page.click('#login-btn, #auth-section button');
    await page.locator('#admin-panel').waitFor({ state: 'visible', timeout: 15_000 });
    await page.locator(`#quiz-select option[value="${QUIZ_FILE}"]`).waitFor({ state: 'attached', timeout: 10_000 });
    await settled(page);
    const values = await page.locator('#quiz-select option').evaluateAll((options) => options.map((option) => (option as HTMLOptionElement).value));
    await tourShot(page, env, record, folder, 'admin-panel', 'Admin panel with the quiz list', [
      ...(await pageChecks(page, record, before, 'admin.html:1559-1566')),
      same('quiz list (sorted by file name)', 'server.py:915-937', [QUIZ_FILE, 'second.yaml'], values),
    ]);

    // 3. The start pop-up
    record.where = 'start pop-up';
    before = record.problems.length;
    await chooseQuiz(page);
    await page.click('#switch-btn');
    await page.locator('#start-quiz-modal').waitFor({ state: 'visible', timeout: 10_000 });
    await settled(page);
    const popup = await page.evaluate(`(() => {
      const box = (id) => document.getElementById(id);
      const shown = (id) => { const el = box(id); return !!el && el.getClientRects().length > 0 && getComputedStyle(el).display !== 'none'; };
      return {
        show_right_answer: box('start-show-right-answer').checked,
        randomize_questions: box('start-randomize-questions').checked,
        show_final_list: box('start-show-final-list').checked,
        show_explanation_offered: shown('start-show-explanation-group'),
        show_explanation: box('start-show-explanation').checked,
        answers_on_completion_offered: shown('start-show-answers-on-completion-group'),
        time_limit_mode: box('start-time-limit-mode').value,
        time_limit_seconds: box('start-time-limit-seconds').value,
      };
    })()`);
    await tourShot(page, env, record, folder, 'start-popup', 'Start pop-up with the settings from the quiz file', [
      ...(await pageChecks(page, record, before, 'admin.html:1559-1566')),
      same(
        'pop-up filled from the quiz file',
        'admin.html:1823-1846, 1856-1862',
        {
          show_right_answer: true,
          randomize_questions: false,
          show_final_list: true,
          show_explanation_offered: true,
          show_explanation: true,
          answers_on_completion_offered: false,
          time_limit_mode: 'per_question',
          time_limit_seconds: '15',
        },
        popup,
      ),
    ]);
    const [switched] = await Promise.all([page.waitForResponse('**/api/admin/switch-quiz'), page.click('#start-quiz-confirm')]);
    record.add('start pop-up', [same('the quiz starts from the pop-up', 'admin.html:1890-1930; server.py:2869-2898', 200, switched.status())]);
    await page.locator('#start-quiz-modal').waitFor({ state: 'hidden', timeout: 10_000 }).catch(() => undefined);
    // The switch ends when the page has reloaded the list and cleared the choice
    await page
      .waitForFunction(`document.getElementById('quiz-select').selectedOptions.length === 0`, null, { timeout: 10_000 })
      .catch(() => undefined);

    // 4. The quiz editor, one screenshot per question type
    record.where = 'quiz editor';
    before = record.problems.length;
    await chooseQuiz(page);
    await page.click('#edit-btn');
    await page.locator('#quiz-editor-modal').waitFor({ state: 'visible', timeout: 10_000 });
    await page.locator('#quiz-editor-modal .question-item').first().waitFor({ state: 'visible', timeout: 10_000 });
    await settled(page);
    const items = page.locator('#quiz-editor-modal .question-item');
    await tourShot(page, env, record, folder, 'editor', 'Quiz editor (questions folded)', [
      ...(await pageChecks(page, record, before, 'admin.html:1559-1566')),
      same('one editor item per question', 'admin.html (wizard editor)', QUESTIONS.length, await items.count()),
    ], { fullPage: false });
    for (const [index, question] of QUESTIONS.entries()) {
      record.where = `quiz editor: question ${question.id}`;
      before = record.problems.length;
      const item = items.nth(index);
      if (((await item.getAttribute('class')) ?? '').includes('collapsed')) await item.locator('.question-header').click();
      await sleep(200);
      // The editor scrolls inside the pop-up: show the question from its top
      await item.evaluate((el) => el.scrollIntoView({ block: 'start' }));
      await sleep(300);
      await tourShot(
        page,
        env,
        record,
        folder,
        `editor-q${question.id}-${question.kind}`,
        `Quiz editor: question ${question.id}, ${QUESTION_KIND_CAPTION[question.kind]}`,
        await pageChecks(page, record, before, 'admin.html:1559-1566'),
        { fullPage: false },
      );
      if (question.id === 1) {
        // 5. The explanation preview of question 1
        record.where = 'quiz editor: explanation preview';
        before = record.problems.length;
        const group = item.locator('.explanation-group');
        if (((await group.getAttribute('class')) ?? '').includes('folded')) await group.locator('.explanation-toggle-btn').click();
        const [preview] = await Promise.all([
          page.waitForResponse('**/api/admin/preview-explanation', { timeout: 10_000 }),
          group.locator('.explanation-preview-btn').click(),
        ]);
        await group.locator('.explanation-preview').first().waitFor({ state: 'visible', timeout: 10_000 });
        await group.evaluate((el) => el.scrollIntoView({ block: 'center' }));
        await sleep(300);
        const html = (await preview.json().catch(() => ({}))) as { html?: string };
        const shown = await group.locator('.explanation-preview').first().evaluate((el) => ({
          strong: el.querySelectorAll('strong').length,
          items: el.querySelectorAll('li').length,
          links: Array.from(el.querySelectorAll('a')).map((a) => a.getAttribute('target')),
        }));
        await tourShot(page, env, record, folder, 'editor-explanation-preview', 'Quiz editor: explanation preview of question 1', [
          ...(await pageChecks(page, record, before, 'admin.html:1559-1566')),
          same('preview rendered on the server', 'server.py:2839-2849', true, typeof html.html === 'string' && html.html.includes('<strong>')),
          same('preview shows the Markdown as HTML', 'explanations.py:39-46, 59-73', { strong: 1, items: 2, links: ['_blank'] }, shown),
        ], { fullPage: false });
        await group.locator('.explanation-preview-btn').click().catch(() => undefined);
      }
      if (((await item.getAttribute('class')) ?? '').includes('collapsed') === false) await item.locator('.question-header').click();
    }
    await page.click('#cancel-editor-btn');
    await page.locator('#quiz-editor-modal').waitFor({ state: 'hidden', timeout: 10_000 }).catch(() => undefined);

    // 6. Live stats with one finished and one unfinished student
    record.where = 'live stats';
    await students(server);
    const stats = await context.newPage();
    watchPage(stats, record, 'live stats page');
    before = record.problems.length;
    await stats.goto(server.url + '/live-stats/');
    await stats.locator('#statsTable tbody tr').first().waitFor({ state: 'visible', timeout: 15_000 });
    await settled(stats, 900);
    const counts = async () => ({
      all: (await stats.locator('#countAll').innerText()).trim(),
      inProgress: (await stats.locator('#countInProgress').innerText()).trim(),
      completed: (await stats.locator('#countCompleted').innerText()).trim(),
    });
    await tourShot(stats, env, record, folder, 'live-stats', 'Live stats: one student finished, one in progress', [
      ...(await pageChecks(stats, record, before, 'live_stats.html:479-486')),
      same('group counts', 'live_stats.html:440-452, 928-930', { all: '2', inProgress: '1', completed: '1' }, await counts()),
    ]);
    for (const [button, screen, caption] of [
      ['#filterInProgress', 'live-stats-in-progress', 'Live stats: the "in progress" group'],
      ['#filterCompleted', 'live-stats-completed', 'Live stats: the "completed" group'],
    ] as const) {
      before = record.problems.length;
      await stats.click(button);
      await sleep(400);
      const rows = await stats.locator('#statsTable tbody tr').count();
      await tourShot(stats, env, record, folder, screen, caption, [
        ...(await pageChecks(stats, record, before, 'live_stats.html:479-486')),
        same('one student in the group', 'live_stats.html:571-590', 1, rows),
      ]);
    }
    await stats.close();

    // 7. The file manager
    record.where = 'file manager';
    const dataDir = path.join(serverDir, 'data');
    const deadline = Date.now() + 20_000;
    while (Date.now() < deadline && readStoredData(dataDir).answers.length < QUESTIONS.length + 3) await sleep(500);
    const csvFiles = readStoredData(dataDir).files;
    before = record.problems.length;
    await page.goto(server.url + '/files/');
    await page.locator('[data-tab="csv"], .tab').first().waitFor({ state: 'visible', timeout: 15_000 });
    await settled(page);
    seen = await look(page);
    await tourShot(page, env, record, folder, 'files-csv', 'File manager: CSV files', [
      ...(await pageChecks(page, record, before, 'files.html:926-933')),
      truth('page title', 'files.html:723', seen.text.includes('📁 WebQuiz File Manager'), '📁 WebQuiz File Manager', seen.text.split('\n').find((line) => line.includes('WebQuiz')) ?? '(none)'),
      truth('the CSV files are listed', 'server.py:4351-4390', csvFiles.length > 0 && csvFiles.every((name) => seen.text.includes(name)), csvFiles.join(', ') || '(no CSV yet)', seen.text.slice(0, 200)),
    ]);
    for (const [tab, screen, caption, rule] of [
      ['logs', 'files-logs', 'File manager: logs', 'server.py:4351-4390'],
      ['quizzes', 'files-quizzes', 'File manager: quizzes', 'server.py:4351-4390'],
    ] as const) {
      before = record.problems.length;
      await page.click(`[data-tab="${tab}"]`);
      await settled(page, 400);
      seen = await look(page);
      const checks = await pageChecks(page, record, before, 'files.html:926-933');
      if (tab === 'quizzes') checks.push(truth('both quizzes listed', rule, seen.text.includes(QUIZ_FILE) && seen.text.includes('second.yaml'), `${QUIZ_FILE}, second.yaml`, seen.text.slice(0, 200)));
      else checks.push(truth('a log file listed', rule, /\.log\b/.test(seen.text), 'a .log file', seen.text.slice(0, 200)));
      await tourShot(page, env, record, folder, screen, caption, checks);
    }
    before = record.problems.length;
    await page.click('[data-tab="config"]');
    await settled(page, 400);
    const language = await page.locator('#cfg-language').inputValue().catch(() => '(no language field)');
    await tourShot(page, env, record, folder, 'files-config-form', 'File manager: config, form view', [
      ...(await pageChecks(page, record, before, 'files.html:926-933')),
      same('form shows the config language', 'files.html:1276-1279', tour.language, language),
    ]);
    before = record.problems.length;
    await page.click('[data-view="yaml"]');
    await settled(page, 400);
    const yamlText = await page.evaluate(`(() => { const area = document.querySelector('#config-tab textarea, textarea'); return area ? area.value : ''; })()`);
    await tourShot(page, env, record, folder, 'files-config-yaml', 'File manager: config, YAML view', [
      ...(await pageChecks(page, record, before, 'files.html:926-933')),
      truth('YAML view shows the config file', 'files.html:802-804; server.py:4296-4349', String(yamlText).includes('registration:') && String(yamlText).includes(`language: ${tour.language}`), 'the webquiz.yaml text', String(yamlText).slice(0, 120)),
    ]);

    // 8. The CSV table page
    record.where = 'CSV table';
    const answersCsv = csvFiles.find((name) => !name.endsWith('.users.csv') && !name.endsWith('.answers_with_users.csv'));
    if (answersCsv) {
      before = record.problems.length;
      await page.goto(`${server.url}/files/csv/${encodeURIComponent(answersCsv)}`);
      await page.locator('table tbody tr').first().waitFor({ state: 'visible', timeout: 15_000 });
      await settled(page);
      const title = (await page.locator('#csv-title').innerText().catch(() => '')).trim();
      const count = (await page.locator('#row-count').innerText().catch(() => '')).trim();
      const rows = readStoredData(dataDir).answers.length;
      await tourShot(page, env, record, folder, 'csv-table', 'CSV table page (answers)', [
        ...(await pageChecks(page, record, before, 'csv_table.html:333-340')),
        same('title is the file name', 'csv_table.html:669', answersCsv, title),
        same('row count', 'csv_table.html:530-531', `Showing ${rows} of ${rows} rows`, count),
      ]);
    } else {
      record.add('CSV table', [truth('an answers CSV to open', 'server.py:1368-1427', false, 'an answers CSV file', 'none written')]);
    }
  } catch (error) {
    record.error = error instanceof Error ? error.message.split('\n')[0] : String(error);
    record.add(record.where, [truth('the tour finished', '(the matrix itself)', false, 'no error', record.error)]);
  } finally {
    if (context) await context.close().catch(() => undefined);
    if (server) await server.stop().catch(() => undefined);
    record.seconds = (Date.now() - started) / 1000;
  }
  return record;
}
