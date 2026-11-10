// Takes the WebQuiz screenshots of docs/imgs from the real Go binary.
//
// Run from the repository root:
//   make build
//   npm --prefix scripts ci
//   npm --prefix scripts run screenshots
//
// The script starts servers with a small sample quiz (in Ukrainian, because the
// screenshots are shared by docs/uk and docs/en), drives the pages with
// Playwright, and writes PNG files to docs/imgs. It does not touch the mac_*.png
// files. Every shot uses the light theme and deviceScaleFactor 2.
import { chromium, type Browser, type BrowserContext, type Page } from '@playwright/test';
import { spawn, type ChildProcess } from 'node:child_process';
import { existsSync, mkdirSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs';
import net from 'node:net';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url));
const root = path.resolve(here, '..');
const outDir = path.join(root, 'docs', 'imgs');
const binary = process.env.WEBQUIZ_BINARY ?? path.join(root, 'bin', process.platform === 'win32' ? 'webquiz.exe' : 'webquiz');
const MASTER_KEY = 'secret123';

type Dict = Record<string, unknown>;

interface Server {
  url: string;
  dir: string;
  stop(): Promise<void>;
}

// ---- Sample data ----

const geography = {
  title: 'Географія України',
  description: 'Короткий тест для уроку географії',
  show_right_answer: true,
  questions: [
    {
      question: 'Яка столиця України?',
      options: ['Львів', 'Київ', 'Одеса', 'Харків'],
      correct_answer: 1,
    },
    {
      question: 'Яка річка найдовша в Україні?',
      options: ['Дніпро', 'Дністер', 'Південний Буг', 'Тиса'],
      correct_answer: 0,
    },
    {
      question: 'Які з цих міст стоять на Дніпрі?',
      options: ['Київ', 'Запоріжжя', 'Львів', 'Черкаси'],
      correct_answer: [0, 1, 3],
      points: 2,
    },
    {
      question: 'Як називаються найвищі гори України?',
      options: ['Альпи', 'Карпати', 'Кавказ', 'Урал'],
      correct_answer: 1,
    },
    {
      question: 'Скільки областей в Україні (без Криму)? Напишіть число.',
      checker: "assert to_int(user_answer) == 24, 'Має бути 24'",
      correct_value: '24',
      default_value: '',
      points: 2,
    },
  ],
};

const history = {
  title: 'Історія України',
  questions: [
    { question: 'У якому році Україна проголосила незалежність?', options: ['1989', '1991', '1996'], correct_answer: 1 },
    { question: 'Хто був першим гетьманом Війська Запорозького?', options: ['Іван Мазепа', 'Дмитро Вишневецький', 'Богдан Хмельницький'], correct_answer: 1 },
  ],
};

const math = {
  title: 'Математика, 7 клас',
  questions: [
    { question: 'Скільки буде 2 + 2?', options: ['3', '4', '5'], correct_answer: 1 },
    { question: 'Чому дорівнює корінь з 16?', checker: "assert abs(float(user_answer) - math.sqrt(16)) < 0.01, 'Має бути 4'", correct_value: '4' },
  ],
};

// ---- Servers ----

function freePort(): Promise<number> {
  return new Promise((resolve, reject) => {
    const probe = net.createServer();
    probe.once('error', reject);
    probe.listen(0, '127.0.0.1', () => {
      const { port } = probe.address() as net.AddressInfo;
      probe.close(() => resolve(port));
    });
  });
}

function canConnect(port: number): Promise<boolean> {
  return new Promise((resolve) => {
    const socket = net.connect({ port, host: '127.0.0.1' });
    socket.once('connect', () => {
      socket.destroy();
      resolve(true);
    });
    socket.once('error', () => resolve(false));
  });
}

interface StartOptions {
  config?: Dict;
  quizzes?: Record<string, Dict>;
  // Files to write into the folder before the start: path inside the folder -> text
  files?: Record<string, string>;
}

// Starts the binary in a new temp folder. JSON is valid YAML, so the config and
// the quizzes are written with JSON.stringify.
async function startServer(options: StartOptions = {}): Promise<Server> {
  const dir = mkdtempSync(path.join(os.tmpdir(), 'webquiz-shots-'));
  const port = await freePort();
  const config: Dict = {
    language: 'uk',
    server: { port },
    admin: { master_key: MASTER_KEY, trusted_ips: [] },
    ...(options.config ?? {}),
  };
  writeFileSync(path.join(dir, 'webquiz.yaml'), JSON.stringify(config, null, 2));
  if (options.quizzes) {
    mkdirSync(path.join(dir, 'quizzes'), { recursive: true });
    for (const [name, quiz] of Object.entries(options.quizzes)) {
      writeFileSync(path.join(dir, 'quizzes', name), JSON.stringify(quiz, null, 2));
    }
  }
  for (const [name, text] of Object.entries(options.files ?? {})) {
    mkdirSync(path.dirname(path.join(dir, name)), { recursive: true });
    writeFileSync(path.join(dir, name), text);
  }
  const env = { ...process.env, WEBQUIZ_BINARY_DIR: dir, WEBQUIZ_IS_BINARY: '0' } as Record<string, string>;
  delete env.WEBQUIZ_MASTER_KEY;
  const child: ChildProcess = spawn(binary, ['--config', path.join(dir, 'webquiz.yaml'), '--port', String(port)], {
    cwd: dir,
    env,
    stdio: ['ignore', 'ignore', 'ignore'],
  });
  let exited = false;
  child.once('exit', () => (exited = true));
  const stop = async () => {
    if (!exited) {
      child.kill('SIGKILL');
      await new Promise((resolve) => child.once('exit', resolve));
    }
    rmSync(dir, { recursive: true, force: true });
  };
  const deadline = Date.now() + 15_000;
  while (Date.now() < deadline) {
    if (exited) throw new Error('The server exited at start');
    if (await canConnect(port)) return { url: `http://127.0.0.1:${port}`, dir, stop };
    await sleep(100);
  }
  await stop();
  throw new Error('The server did not start in 15 seconds');
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

// ---- Student API ----

async function post(server: Server, url: string, body: Dict, cookie?: string): Promise<any> {
  const response = await fetch(server.url + url, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...(cookie ? { Cookie: cookie } : {}) },
    body: JSON.stringify(body),
  });
  if (!response.ok) throw new Error(`${url}: ${response.status} ${await response.text()}`);
  return response.json();
}

async function put(server: Server, url: string, body: Dict, cookie: string): Promise<void> {
  const response = await fetch(server.url + url, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json', Cookie: cookie },
    body: JSON.stringify(body),
  });
  if (!response.ok) throw new Error(`${url}: ${response.status} ${await response.text()}`);
}

async function adminCookie(server: Server): Promise<string> {
  const response = await fetch(server.url + '/api/admin/auth', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ master_key: MASTER_KEY }),
  });
  if (!response.ok) throw new Error('admin login failed');
  const cookie = response.headers.get('set-cookie') ?? '';
  return cookie.split(';')[0];
}

async function register(server: Server, username: string, fields: Dict = {}): Promise<string> {
  const reply = await post(server, '/api/register', { username, ...fields });
  return String(reply.user_id);
}

// One student answers the first `count` questions. `answers[i]` is the chosen
// answer of question i + 1; `pauses[i]` is the seconds to wait before it.
async function answerQuestions(server: Server, userId: string, answers: unknown[], pauses: number[] = []): Promise<void> {
  for (let i = 0; i < answers.length; i++) {
    await post(server, '/api/question-start', { user_id: userId, question_id: i + 1 });
    await sleep((pauses[i] ?? 0) * 1000);
    await post(server, '/api/submit-answer', { user_id: userId, question_id: i + 1, selected_answer: answers[i] });
  }
}

// ---- Page helpers ----

async function newPage(browser: Browser, width: number, height: number): Promise<{ context: BrowserContext; page: Page }> {
  const context = await browser.newContext({
    viewport: { width, height },
    deviceScaleFactor: 2,
    colorScheme: 'light',
    locale: 'uk-UA',
    timezoneId: 'Europe/Kyiv',
  });
  // The theme is stored in localStorage; the light theme is the default
  await context.addInitScript(() => {
    try {
      localStorage.setItem('theme', 'light');
    } catch {
      // ignore
    }
  });
  const page = await context.newPage();
  return { context, page };
}

async function adminLogin(page: Page, server: Server): Promise<void> {
  await page.goto(server.url + '/admin/');
  await page.locator('#master-key').fill(MASTER_KEY);
  await page.getByRole('button', { name: 'Увійти' }).click();
  await page.locator('#admin-panel').waitFor();
  await page.locator('#quiz-select option').first().waitFor({ state: 'attached' });
  // The green "login successful" message goes away by itself
  await page.locator('#message-area .message').first().waitFor({ state: 'detached', timeout: 15_000 }).catch(() => {});
}

// Waits until the small green messages (login, session restored) are gone
async function waitForToasts(page: Page): Promise<void> {
  await page.locator('.message.success, .toast').first().waitFor({ state: 'detached', timeout: 15_000 }).catch(() => {});
}

function shot(name: string): string {
  mkdirSync(outDir, { recursive: true });
  return path.join(outDir, name);
}

// ---- CSV files for the file manager ----

function csvLine(row: string[]): string {
  return row.map((cell) => (/[",\r\n]/.test(cell) ? `"${cell.replace(/"/g, '""')}"` : cell)).join(',');
}

function csvText(rows: string[][]): string {
  return rows.map(csvLine).join('\r\n') + '\r\n';
}

function sampleCsvFiles(): Record<string, string> {
  const questions = geography.questions.map((q) => q.question);
  const right = ['Київ', 'Дніпро', 'Київ, Запоріжжя, Черкаси', 'Карпати', '24'];
  const students: [string, string, string, string[], string[]][] = [
    ['364562', 'Олена', '10-А', right, ['2.4', '3.1', '8.6', '2.2', '14.0']],
    ['218305', 'Андрій', '10-А', ['Київ', 'Дністер', 'Київ, Львів', 'Карпати', '25'], ['3.0', '4.7', '9.9', '3.5', '11.2']],
    ['809400', 'Марія', '10-Б', ['Київ', 'Дніпро', 'Київ, Запоріжжя', 'Карпати', '24'], ['2.9', '2.5', '12.4', '4.1', '9.8']],
  ];
  const header = ['user_id', 'username', 'група', 'question', 'selected_answer', 'correct_answer', 'is_correct', 'time_taken_seconds'];
  const withUsers: string[][] = [header];
  const plain: string[][] = [['user_id', 'question', 'selected_answer', 'correct_answer', 'is_correct', 'time_taken_seconds']];
  const users: string[][] = [
    ['user_id', 'username', 'група', 'registered_at', 'total_questions_asked', 'correct_answers', 'earned_points', 'total_points', 'total_time'],
  ];
  for (const [id, name, group, chosen, times] of students) {
    let correct = 0;
    let earned = 0;
    let seconds = 0;
    chosen.forEach((answer, i) => {
      const ok = answer === right[i];
      const points = geography.questions[i].points ?? 1;
      if (ok) {
        correct += 1;
        earned += points;
      }
      seconds += Number(times[i]);
      withUsers.push([id, name, group, questions[i], answer, right[i], ok ? 'True' : 'False', times[i]]);
      plain.push([id, questions[i], answer, right[i], ok ? 'True' : 'False', times[i]]);
    });
    const mm = Math.floor(seconds / 60);
    const ss = String(Math.round(seconds % 60)).padStart(2, '0');
    users.push([id, name, group, '2026-11-09T09:15:00', '5', String(correct), String(earned), '8', `${mm}:${ss}`]);
  }
  return {
    'data/geography_0001.csv': csvText(plain),
    'data/geography_0001.users.csv': csvText(users),
    'data/geography_0001.answers_with_users.csv': csvText(withUsers),
  };
}

// ---- The shots ----

// Fresh start: the admin panel and the quiz page of a server with no config.
async function freshStart(browser: Browser): Promise<void> {
  const server = await startServer({ quizzes: { 'default.yaml': geography } });
  try {
    {
      const { context, page } = await newPage(browser, 760, 900);
      await adminLogin(page, server);
      await page.screenshot({ path: shot('first_admin.png'), fullPage: true });
      await context.close();
    }
    {
      const { context, page } = await newPage(browser, 830, 420);
      await page.goto(server.url + '/');
      await page.locator('#username').waitFor();
      await page.screenshot({ path: shot('reg_default_test.png'), fullPage: true });

      // A student takes the quiz and sees the results
      await page.locator('#username').fill('Олена');
      await page.locator('.register-btn').click();
      const picks: (number[] | string)[] = [[1], [0], [0, 1, 3], [1], '24'];
      for (let i = 0; i < picks.length; i++) {
        await page.locator('#current-question-container .current-question').waitFor();
        const pick = picks[i];
        if (typeof pick === 'string') {
          await page.locator('#current-question-container textarea').fill(pick);
        } else {
          for (const index of pick) await page.locator(`[data-option-index="${index}"]`).click();
        }
        await page.locator('#submit-answer-btn').click();
        await page.locator('#continue-btn').click();
      }
      await page.waitForSelector('#results, .results, #final-results', { timeout: 10_000 }).catch(() => {});
      await sleep(500);
      await page.screenshot({ path: shot('done_default_test.png'), fullPage: true });
      await context.close();
    }
  } finally {
    await server.stop();
  }
}

// A class at work: quiz editor, admin panel with approvals, live stats, files.
async function classroom(browser: Browser): Promise<void> {
  const server = await startServer({
    config: {
      registration: { approve: true, username_label: "Ім'я та прізвище", fields: ['Група'] },
      quizzes: [
        {
          name: 'Квізи з географії',
          download_path: 'https://example.com/webquiz-quizzes/archive/refs/heads/master.zip',
          folder: 'quizzes/',
        },
      ],
      tunnel: {
        server: 'tunnel.example.com',
        public_key: 'keys/webquiz_tunnel.pub',
        private_key: 'keys/webquiz_tunnel',
        config: { username: 'webquiz', socket_directory: '/var/run/webquiz', base_url: 'https://tunnel.example.com' },
      },
    },
    quizzes: { 'geography.yaml': geography, 'history.yaml': history, 'math.yaml': math },
    files: {
      ...sampleCsvFiles(),
    },
  });
  try {
    const cookie = await adminCookie(server);
    await post(server, '/api/admin/switch-quiz', { quiz_filename: 'geography.yaml' }, cookie);

    // Students: five approved, one waiting
    const names = ['Олена Коваль', 'Андрій Мельник', 'Марія Шевченко', 'Тарас Бондар', 'Софія Лисенко'];
    const ids: string[] = [];
    for (const name of names) {
      const id = await register(server, name, { група: '10-А' });
      ids.push(id);
      await put(server, '/api/admin/approve-user', { user_id: id }, cookie);
    }
    await register(server, 'Максим Ткачук', { група: '10-Б' });
    const right = [1, 0, [0, 1, 3], 1, '24'];
    await answerQuestions(server, ids[0], right, [2, 2, 3, 2, 3]);
    await answerQuestions(server, ids[1], [1, 1, [0, 2], 1], [2, 3, 2, 2]);
    await answerQuestions(server, ids[2], [1, 0, [0, 1]], [1, 1, 2]);
    await answerQuestions(server, ids[3], [0, 0], [1, 2]);
    await answerQuestions(server, ids[4], [1], [1]);
    // The student of the fifth row is thinking about the next question now
    await post(server, '/api/question-start', { user_id: ids[3], question_id: 3 });
    await post(server, '/api/question-start', { user_id: ids[4], question_id: 2 });
    await sleep(1000);

    // Admin panel
    {
      const { context, page } = await newPage(browser, 760, 900);
      await adminLogin(page, server);
      await page.locator('#quiz-select option').first().click();
      await sleep(300);
      await page.screenshot({ path: shot('admin_advanced.png'), fullPage: true });
      // The quiz editor
      await page.locator('#edit-btn').click();
      await page.locator('#quiz-editor-modal').waitFor();
      await sleep(500);
      await page.setViewportSize({ width: 860, height: 1100 });
      await sleep(300);
      await page.locator('.question-item').first().locator('.question-header, .question-title').first().click().catch(() => {});
      await sleep(300);
      await page.screenshot({ path: shot('edit_quiz.png') });
      await context.close();
    }

    // Live stats
    {
      const { context, page } = await newPage(browser, 1280, 760);
      await page.goto(server.url + '/live-stats/');
      await page.locator('#countAll').waitFor();
      await sleep(1500);
      await page.screenshot({ path: shot('livestats.png'), fullPage: true });
      await context.close();
    }

    // Wait until the server has flushed the answers of this run to its CSV files
    await sleep(6000);

    // File manager
    {
      const { context, page } = await newPage(browser, 1100, 640);
      // The files page needs the admin session cookie
      await adminLogin(page, server);
      await page.goto(server.url + '/files/');
      await page.locator('#csv-files .file-item').first().waitFor();
      await waitForToasts(page);
      await page.screenshot({ path: shot('file_manager.png') });

      // The table pop-up of the sample answers file
      const item = page.locator('#csv-files .file-item[data-name="geography_0001.answers_with_users.csv"]');
      await item.getByRole('button', { name: /Table|Таблиця/ }).click();
      const frameElement = page.locator('#csv-table-frame');
      await frameElement.waitFor();
      const frame = page.frameLocator('#csv-table-frame');
      await frame.locator('#table-container tbody tr').first().waitFor();
      await frame.locator('#user-filter').selectOption({ label: 'Олена (364562)' });
      await waitForToasts(page);
      await sleep(500);
      await page.setViewportSize({ width: 1300, height: 640 });
      await sleep(300);
      await page.screenshot({ path: shot('file_manager_table.png') });
      await page.keyboard.press('Escape');
      await context.close();
    }
    {
      const { context, page } = await newPage(browser, 1100, 760);
      await adminLogin(page, server);
      await page.goto(server.url + '/files/');
      await page.locator('#config-tab-button').click();
      await page.locator('#cfg-reg-approve').waitFor();
      await waitForToasts(page);
      // Show a normal folder name instead of the temp folder of this run
      await page.evaluate((dir) => {
        const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
        for (let node = walker.nextNode(); node; node = walker.nextNode()) {
          if (node.textContent?.includes(dir)) node.textContent = node.textContent.replace(dir, '/home/teacher/webquiz');
        }
      }, server.dir);
      await page.screenshot({ path: shot('file_manager_config.png'), fullPage: true });
      await context.close();
    }
  } finally {
    await server.stop();
  }
}

async function main(): Promise<void> {
  if (!existsSync(binary)) {
    throw new Error(`Binary not found: ${binary}. Run "make build" first, or set WEBQUIZ_BINARY.`);
  }
  const browser = await chromium.launch({ args: ['--no-sandbox'] });
  try {
    await freshStart(browser);
    await classroom(browser);
  } finally {
    await browser.close();
  }
  console.log('Screenshots written to', outDir);
}

main().catch((error) => {
  console.error(error);
  process.exit(1);
});
