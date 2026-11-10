// Browser and server helpers for the matrix: start a WebQuiz server in its own
// folder, talk to its API, open browser sessions with a device and theme, read
// what a page shows, and take screenshots.

import { spawn, type ChildProcess } from 'node:child_process';
import { closeSync, existsSync, mkdirSync, openSync, readdirSync, readFileSync, writeFileSync } from 'node:fs';
import net from 'node:net';
import path from 'node:path';
import { devices, type Browser, type BrowserContext, type Page } from 'playwright';
import { stringify } from 'yaml';
import { parseCSV } from './checks';
import type { Screen } from './matrix-cases';
import { quizAssets } from './matrix-quiz';
import type { RunRecord } from './matrix-report';
import type { Snap } from './matrix-rules';

export const MASTER_KEY = 'matrix-key';
const RESERVED_PORTS = new Set([8198, 8199]); // used by harness/run.ts

export const sleep = (ms: number) => new Promise<void>((resolve) => setTimeout(resolve, ms));

// ---- Server ----

export interface RunningServer {
  url: string;
  port: number;
  dir: string;
  stop(): Promise<void>;
}

export function freePort(): Promise<number> {
  return new Promise((resolve, reject) => {
    const probe = net.createServer();
    probe.once('error', reject);
    probe.listen(0, '127.0.0.1', () => {
      const { port } = probe.address() as net.AddressInfo;
      probe.close(() => (RESERVED_PORTS.has(port) ? freePort().then(resolve, reject) : resolve(port)));
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

/**
 * Start the binary in `dir` with these quiz files and config sections, on a free port.
 * Like e2e/fixtures/server.ts: WEBQUIZ_BINARY_DIR is the folder, WEBQUIZ_IS_BINARY=0 keeps the
 * browser closed, and the developer's time limit and master key variables are removed.
 */
export async function startServer(
  binary: string,
  dir: string,
  quizzes: Record<string, unknown>,
  configSections: Record<string, unknown>,
): Promise<RunningServer> {
  const quizzesDir = path.join(dir, 'quizzes');
  mkdirSync(quizzesDir, { recursive: true });
  for (const [relative, content] of Object.entries(quizAssets())) {
    const file = path.join(quizzesDir, relative);
    mkdirSync(path.dirname(file), { recursive: true });
    writeFileSync(file, content);
  }
  for (const [name, quiz] of Object.entries(quizzes)) writeFileSync(path.join(quizzesDir, name), stringify(quiz), 'utf-8');
  const port = await freePort();
  const config: Record<string, unknown> = {
    server: { port },
    paths: {
      quizzes_dir: quizzesDir,
      logs_dir: path.join(dir, 'logs'),
      csv_dir: path.join(dir, 'data'),
      static_dir: path.join(dir, 'static'),
    },
    admin: { master_key: MASTER_KEY, trusted_ips: [] },
    ...configSections,
  };
  const configPath = path.join(dir, 'webquiz.yaml');
  writeFileSync(configPath, stringify(config), 'utf-8');
  // PYTHONDONTWRITEBYTECODE: a Python 1.19 server (to check the checks) writes nothing into its source folder
  const env: NodeJS.ProcessEnv = { ...process.env, WEBQUIZ_BINARY_DIR: dir, WEBQUIZ_IS_BINARY: '0', PYTHONDONTWRITEBYTECODE: '1' };
  for (const name of ['WEBQUIZ_MASTER_KEY', 'WEBQUIZ_TIME_LIMIT_GRACE_SECONDS', 'WEBQUIZ_TIME_LIMIT_CHECK_INTERVAL']) delete env[name];
  const log = openSync(path.join(dir, 'server.log'), 'w');
  const child: ChildProcess = spawn(binary, ['--config', configPath, '--port', String(port)], {
    cwd: dir,
    env,
    stdio: ['ignore', log, log],
  });
  closeSync(log);
  let exited = false;
  child.once('exit', () => (exited = true));
  const stop = async () => {
    if (exited) return;
    const done = new Promise<void>((resolve) => child.once('exit', () => resolve()));
    child.kill('SIGTERM'); // the server writes the CSV rows it still holds
    const timer = setTimeout(() => child.kill('SIGKILL'), 10_000);
    await done;
    clearTimeout(timer);
  };
  const deadline = Date.now() + 20_000;
  while (Date.now() < deadline) {
    if (exited) throw new Error(`the server exited at start, see ${dir}/server.log`);
    if (await canConnect(port)) return { url: `http://127.0.0.1:${port}`, port, dir, stop };
    await sleep(100);
  }
  await stop();
  throw new Error(`the server did not answer on port ${port}, see ${dir}/server.log`);
}

// ---- HTTP API ----

export interface Reply {
  status: number;
  json: any; // eslint-disable-line @typescript-eslint/no-explicit-any
}

export async function api(base: string, method: string, apiPath: string, body?: unknown, cookie = ''): Promise<Reply> {
  const response = await fetch(base + apiPath, {
    method,
    headers: { 'Content-Type': 'application/json', ...(cookie ? { Cookie: cookie } : {}) },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const text = await response.text();
  let json: unknown = text;
  try {
    json = JSON.parse(text);
  } catch {
    // not JSON: keep the text
  }
  return { status: response.status, json };
}

/** The admin API with a session cookie (POST /api/admin/auth, server.py:2900-2947). */
export class AdminApi {
  private cookie = '';
  constructor(readonly base: string) {}

  async login(): Promise<void> {
    const response = await fetch(this.base + '/api/admin/auth', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ master_key: MASTER_KEY }),
    });
    const session = response.headers.getSetCookie().find((item) => item.startsWith('admin_session='));
    if (response.status !== 200 || !session) throw new Error(`admin login failed: ${response.status}`);
    this.cookie = session.split(';')[0];
  }

  call(method: string, apiPath: string, body?: unknown): Promise<Reply> {
    return api(this.base, method, apiPath, body, this.cookie);
  }
}

// ---- Stored data ----

export function readCsvRecords(file: string): Record<string, string>[] {
  const [header, ...rows] = parseCSV(readFileSync(file, 'utf-8').replace(/^﻿/, ''));
  if (!header) return [];
  return rows.map((row) => Object.fromEntries(header.map((name, index) => [name, row[index] ?? ''])));
}

/** The rows of the answers CSV files and the users CSV files in a data folder. */
export function readStoredData(dataDir: string): { answers: Record<string, string>[]; users: Record<string, string>[]; files: string[] } {
  const files = existsSync(dataDir) ? readdirSync(dataDir).filter((name) => name.endsWith('.csv')).sort() : [];
  const answers: Record<string, string>[] = [];
  const users: Record<string, string>[] = [];
  for (const name of files) {
    if (name.endsWith('.answers_with_users.csv')) continue;
    const records = readCsvRecords(path.join(dataDir, name));
    if (name.endsWith('.users.csv')) users.push(...records);
    else answers.push(...records);
  }
  return { answers, users, files };
}

// ---- Browser ----

export function contextOptions(screen: Screen) {
  if (screen.device === 'phone') return { ...devices['iPhone 13'] };
  return { viewport: { width: 1280, height: 860 }, deviceScaleFactor: 1 };
}

/**
 * A browser session with the device and theme of `screen`. The theme goes into localStorage
 * before any page script runs. Requests to other hosts never leave the machine: the GitHub
 * "latest release" question gets this version as the answer, anything else is refused and noted.
 */
export async function newSession(browser: Browser, screen: Screen, record: RunRecord, version: string): Promise<BrowserContext> {
  const context = await browser.newContext(contextOptions(screen));
  await context.addInitScript({ content: `try { localStorage.setItem('theme', '${screen.theme}'); } catch (e) {}` });
  await context.route(/^https?:\/\/(?!127\.0\.0\.1[:/]|localhost[:/])/, async (route) => {
    const url = route.request().url();
    if (/^https:\/\/api\.github\.com\/repos\/[^/]+\/[^/]+\/releases\/latest/.test(url)) {
      record.notes.push(`the page asked GitHub for the latest release (${url}); the matrix answered v${version}`);
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ tag_name: `v${version}` }) });
      return;
    }
    record.problems.push({ page: record.where, kind: 'external', text: `request to another host refused: ${url}` });
    await route.abort();
  });
  return context;
}

/** Console messages that 1.19 shows too, with the reason. */
const EXPECTED_CONSOLE: { test: (text: string, url: string) => boolean; reason: string }[] = [
  {
    test: (text, url) => /status of 401/.test(text) && /\/api\/admin\/check-session$/.test(url),
    reason:
      '1.19 does the same: before the login the page asks /api/admin/check-session, which answers 401 ' +
      '(admin.html:4195, files.html:1597; server.py:2957-2963)',
  },
];

/** Note every console error, uncaught exception and failed request of a page in the record. */
export function watchPage(page: Page, record: RunRecord, name: string): void {
  page.on('console', (message) => {
    if (message.type() !== 'error') return;
    const url = message.location().url ?? '';
    const text = message.text() + (url && !message.text().includes(url) ? ` [${url}]` : '');
    const expected = EXPECTED_CONSOLE.find((item) => item.test(message.text(), url))?.reason;
    record.problems.push({ page: `${name}: ${record.where}`, kind: 'console', text, ...(expected ? { expected } : {}) });
  });
  page.on('pageerror', (error) => record.problems.push({ page: `${name}: ${record.where}`, kind: 'pageerror', text: String(error) }));
  page.on('requestfailed', (request) => {
    const failure = request.failure()?.errorText ?? '';
    if (failure.includes('ERR_ABORTED') && /\/(api|ws)\//.test(request.url())) return; // a page left while waiting
    if (!/^https?:\/\/(127\.0\.0\.1|localhost)[:/]/.test(request.url())) return; // noted by the route above
    record.problems.push({ page: `${name}: ${record.where}`, kind: 'requestfailed', text: `${request.method()} ${request.url()} ${failure}` });
  });
}

// What a person sees on the quiz page. Works with the markup of 1.19 (templates/index.html)
// and of the Go version: both use the same ids and class names. Passed as text, so no build
// tool can change the function.
const READ_JS = String.raw`() => {
  const $ = (selector, root) => (root || document).querySelector(selector);
  const $$ = (selector, root) => Array.from((root || document).querySelectorAll(selector));
  const visible = (el) => {
    if (!el || !el.getClientRects().length) return false;
    const style = getComputedStyle(el);
    return style.visibility === 'visible' && style.display !== 'none';
  };
  const text = (el) => (el ? (el.innerText || '').trim() : '');
  const content = (el) => (el ? (el.textContent || '').replace(/\s+/g, ' ').trim() : '');
  const box = (el) => {
    if (!visible(el)) return null;
    const rect = el.getBoundingClientRect();
    return { left: Math.round(rect.left), right: Math.round(rect.right), top: Math.round(rect.top), bottom: Math.round(rect.bottom) };
  };
  const button = (el) => ({ visible: visible(el), disabled: !!(el && el.disabled), text: content(el), box: box(el) });
  // Where an option's radio circle ends, with the gap after it (0 without a circle, as in 1.19)
  const circleEnd = (option) => {
    const marker = $('.option-marker', option);
    if (!visible(marker)) return 0;
    return Math.round(marker.getBoundingClientRect().right + (parseFloat(getComputedStyle(marker).marginRight) || 0));
  };
  const image = (img) =>
    img
      ? {
          src: img.getAttribute('src') || '',
          loaded: img.complete && img.naturalWidth > 0,
          width: img.offsetWidth, // the layout width: a hover zoom (transform) does not count
          natural: img.naturalWidth,
        }
      : null;
  const afterLabel = (el) => {
    const value = getComputedStyle(el, '::after').content;
    if (!value || value === 'none' || value === 'normal') return '';
    return value.replace(/^["']|["']$/g, '');
  };
  const parts = (el) => {
    const divs = Array.from(el.children).filter((child) => child.tagName === 'DIV');
    const list = divs.length ? divs.map((div) => content(div)) : [content(el)];
    return list.filter((item) => item !== '');
  };

  const sections = {
    loading: visible($('#loading')),
    registration: visible($('#registration')),
    waiting: visible($('#waiting-approval')),
    test: visible($('#test-section')),
    results: visible($('#results')),
    explanation: visible($('#explanation-step')),
    question: visible($('#current-question-container')),
  };

  const background = (el) => getComputedStyle(el).backgroundColor;
  let bg = background(document.body);
  if (/rgba\(\s*0,\s*0,\s*0,\s*0\s*\)|transparent/.test(bg)) bg = background(document.documentElement);
  const rgb = (bg.match(/[\d.]+/g) || ['255', '255', '255']).map(Number);
  const luminance = (0.2126 * rgb[0] + 0.7152 * rgb[1] + 0.0722 * rgb[2]) / 255;

  const userInfo = $('#user-info');
  const reg = $('#registration');
  const wait = $('#waiting-approval');
  const waitError = $('#waiting-error');

  let question = null;
  const cards = $$('#current-question-container .current-question');
  const card = cards[cards.length - 1];
  if (card) {
    const timerBox = $('.question-timer-box', card);
    const textInput = $('#text-answer-input', card);
    const inputLabel = textInput ? $('.text-input-container > p, .text-input-container > label', card) : null;
    const file = $('a.question-file', card);
    question = {
      text: content($('.question-text', card)),
      badge: $('.question-points-badge', card) ? content($('.question-points-badge', card)) : null,
      timer: timerBox
        ? {
            visible: visible(timerBox),
            text: content($('#question-timer', card)),
            label: content(timerBox),
            warning: !!$('#question-timer', card) && $('#question-timer', card).classList.contains('timer-warning'),
          }
        : null,
      userSelect: getComputedStyle(card).userSelect,
      hint: $('.multiple-choice-hint', card) ? content($('.multiple-choice-hint', card)) : null,
      options: $$('.option', card).map((el) => {
        const copy = el.cloneNode(true);
        copy.querySelectorAll('.feedback-label, .option-marker').forEach((child) => child.remove());
        const label = $('.feedback-label', el);
        return {
          text: content(copy),
          classes: Array.from(el.classList),
          label: label ? content(label) : afterLabel(el),
          image: image($('img.option-image', el)),
          circleEnd: circleEnd(el),
        };
      }),
      textInput: textInput
        ? {
            label: content(inputLabel),
            value: textInput.value,
            placeholder: textInput.getAttribute('placeholder') || '',
            disabled: textInput.disabled,
            classes: Array.from(textInput.classList),
            userSelect: getComputedStyle(textInput).userSelect,
          }
        : null,
      checkerError: $('.checker-error', card) ? text($('.checker-error', card)) : null,
      correctValue: $('.correct-value-display', card) ? text($('.correct-value-display', card)) : null,
      image: image($('img.question-image', card)),
      file: file ? { name: content($('.question-file-name', file)), href: file.getAttribute('href') || '', download: file.hasAttribute('download') } : null,
      timeUp: $('.time-up-message', card) ? content($('.time-up-message', card)) : null,
    };
  }

  const step = $('#explanation-step');
  const explanation = {
    title: content($('.explanation-title', step)),
    question: content($('.explanation-question', step)),
    strong: $$('.explanation-content strong', step).map(content),
    listItems: $$('.explanation-content li', step).length,
    links: $$('.explanation-content a', step).map((a) => ({ text: content(a), href: a.getAttribute('href') || '', target: a.getAttribute('target') || '', rel: a.getAttribute('rel') || '' })),
    next: button($('#explanation-next-btn')),
  };

  let results = null;
  const resultsContent = $('#results-content');
  if (resultsContent && sections.results) {
    const table = $('table.results-table', resultsContent);
    results = {
      paragraphs: $$('p', resultsContent).filter((p) => !p.closest('table')).map((p) => content(p)),
      text: text(resultsContent),
      buttons: $$('button', resultsContent).filter(visible).map(content),
      table: table
        ? {
            headers: $$('thead th', table).map(content),
            rows: $$('tbody tr', table).map((tr) =>
              $$('td', tr).map((td) => {
                const span = Array.from(td.children).find((child) => child.tagName === 'SPAN');
                const hint = $('.correct-answer-hint', td);
                const lines = (td.innerText || '').split('\n').map((line) => line.trim()).filter(Boolean);
                return {
                  text: content(td),
                  firstLine: lines[0] || '',
                  span: span ? { parts: parts(span), images: $$('img', span).map((img) => img.getAttribute('src') || ''), classes: Array.from(span.classList) } : null,
                  timeUp: $('.time-up-message', td) ? content($('.time-up-message', td)) : null,
                  hint: hint ? { text: (hint.innerText || '').replace(/\s+/g, ' ').trim(), images: $$('img', hint).map((img) => img.getAttribute('src') || '') } : null,
                  images: $$('img', td).map((img) => img.getAttribute('src') || ''),
                  fileLink: $('.question-file-name', td) ? content($('.question-file-name', td)) : null,
                };
              }),
            ),
          }
        : null,
    };
  }

  return {
    visible: sections,
    title: content($('h1')),
    docTitle: document.title,
    pageText: document.body.innerText || '',
    dark: luminance < 0.4,
    overflowX: document.documentElement.scrollWidth - document.documentElement.clientWidth,
    viewportWidth: document.documentElement.clientWidth,
    userInfo: visible(userInfo) ? content(userInfo) : null,
    registration: {
      heading: content($('h2', reg)),
      labels: $$('.registration-label', reg).map(content),
      inputs: $$('input', reg).filter((input) => !input.type || input.type === 'text').length,
      button: content($('.register-btn', reg)),
    },
    waiting: {
      heading: content($('h2', wait)),
      text: text(wait),
      labels: $$('.registration-label', wait).map(content),
      values: $$('input', wait).filter((input) => !input.type || input.type === 'text').map((input) => input.value),
      buttons: $$('button', wait).filter(visible).map(content),
      message: visible(waitError) ? content(waitError) : null,
    },
    progress: content($('#progress-text')),
    progressWidth: $('#progress-fill') ? parseFloat($('#progress-fill').style.width) : null,
    quizTimer: {
      visible: visible($('#quiz-timer-box')),
      text: content($('#quiz-timer')),
      label: content($('#quiz-timer-box .timer-label')),
      warning: !!$('#quiz-timer') && $('#quiz-timer').classList.contains('timer-warning'),
    },
    question,
    submit: button($('#submit-answer-btn')),
    cont: button($('#continue-btn')),
    // The verdict next to "Continue" is new in 2.0: a 1.19 page has none
    verdict: visible($('#answer-verdict')) ? { text: content($('#answer-verdict')), box: box($('#answer-verdict')) } : null,
    explanation,
    results,
  };
}`;

// A short state for waiting loops.
const STATE_JS = String.raw`() => {
  const visible = (el) => {
    if (!el || !el.getClientRects().length) return false;
    const style = getComputedStyle(el);
    return style.visibility === 'visible' && style.display !== 'none';
  };
  const $ = (selector) => document.querySelector(selector);
  const cards = document.querySelectorAll('#current-question-container .current-question');
  const card = cards[cards.length - 1];
  const questionText = card && visible($('#current-question-container')) ? (card.querySelector('.question-text') || {}).textContent : null;
  const cont = $('#continue-btn');
  const resultsContent = $('#results-content');
  return {
    registration: visible($('#registration')),
    waiting: visible($('#waiting-approval')),
    waitingMessage: visible($('#waiting-error')) ? $('#waiting-error').textContent.trim() : null,
    question: questionText ? questionText.replace(/\s+/g, ' ').trim() : null,
    cardClasses: card ? card.className : '',
    results: visible($('#results')) && !!resultsContent && (resultsContent.innerText || '').trim().length > 0,
    explanation: visible($('#explanation-step')),
    explanationNext: visible($('#explanation-next-btn')) && !$('#explanation-next-btn').disabled,
    contVisible: visible(cont),
    contReady: visible(cont) && !cont.disabled,
    verdict: visible($('#answer-verdict')),
    marks: document.querySelectorAll('#current-question-container [class*="feedback-"], #current-question-container .checker-error, #current-question-container .correct-value-display').length,
    timeUp: !!(card && card.querySelector('.time-up-message')),
    questionTimer: ($('#question-timer') || {}).textContent || '',
    questionTimerVisible: visible($('#question-timer')),
    quizTimer: visible($('#quiz-timer-box')) ? ($('#quiz-timer') || {}).textContent || '' : null,
    animating: document.getAnimations().some((animation) => animation.playState === 'running'),
    imagesLoading: Array.from(document.images).some((img) => visible(img) && !img.complete),
  };
}`;

export interface PageState {
  registration: boolean;
  waiting: boolean;
  waitingMessage: string | null;
  question: string | null;
  cardClasses: string;
  results: boolean;
  explanation: boolean;
  explanationNext: boolean;
  contVisible: boolean;
  contReady: boolean;
  /** The verdict next to "Continue" shows (new in 2.0; never on a 1.19 page). */
  verdict: boolean;
  marks: number;
  timeUp: boolean;
  questionTimer: string;
  questionTimerVisible: boolean;
  quizTimer: string | null;
  animating: boolean;
  imagesLoading: boolean;
}

// What is chosen before the answer is sent.
const CHOICE_JS = String.raw`() => {
  const submit = document.getElementById('submit-answer-btn');
  return {
    selected: Array.from(document.querySelectorAll('#current-question-container .option.selected')).map((el) =>
      Number(el.getAttribute('data-option-index') ?? String(el.id).replace('option_', '')),
    ),
    submitEnabled: !!submit && !submit.disabled && submit.getClientRects().length > 0,
  };
}`;

export interface ChoiceSeen {
  selected: number[];
  submitEnabled: boolean;
}

export async function readChoice(page: Page): Promise<ChoiceSeen> {
  return (await page.evaluate(`(${CHOICE_JS})()`)) as ChoiceSeen;
}

// Scroll down and see whether the name badge and the theme button move with the page.
const SCROLL_JS = String.raw`(distance) => {
  const place = (el) => (el && el.getClientRects().length ? Math.round(el.getBoundingClientRect().top + window.scrollY) : null);
  const badge = document.getElementById('user-info');
  const toggle = document.querySelector('.theme-toggle');
  window.scrollTo(0, 0);
  const before = { badge: place(badge), toggle: place(toggle) };
  window.scrollTo(0, distance);
  const scrolled = Math.round(window.scrollY);
  const after = { badge: place(badge), toggle: place(toggle) };
  const change = (a, b) => (a === null || b === null ? null : b - a);
  return { scrolled, badge: change(before.badge, after.badge), toggle: change(before.toggle, after.toggle) };
}`;

/** Scrolls the page down by `distance` and leaves it there (for a screenshot). */
export async function scrollAndMeasure(page: Page, distance: number): Promise<import('./matrix-rules').ScrollSeen> {
  return (await page.evaluate(`(${SCROLL_JS})(${distance})`)) as import('./matrix-rules').ScrollSeen;
}

export async function readSnap(page: Page): Promise<Snap> {
  return (await page.evaluate(`(${READ_JS})()`)) as Snap;
}

export async function pageState(page: Page): Promise<PageState> {
  return (await page.evaluate(`(${STATE_JS})()`)) as PageState;
}

/** Wait until `test` holds for the page state; throws with `what` after `timeoutMs`. */
export async function until(page: Page, test: (state: PageState) => boolean, timeoutMs: number, what: string): Promise<PageState> {
  const deadline = Date.now() + timeoutMs;
  let state = await pageState(page);
  while (!test(state)) {
    if (Date.now() > deadline) throw new Error(`waited ${Math.round(timeoutMs / 1000)} s for ${what}`);
    await sleep(80);
    state = await pageState(page);
  }
  return state;
}

/** Wait for animations and images to finish, so a screenshot shows a still page. */
export async function settle(page: Page, timeoutMs = 4000): Promise<void> {
  // The mouse stays where the last click was: move it away, so nothing on the page is hovered
  await page.mouse.move(0, 0);
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    const state = await pageState(page);
    if (!state.animating && !state.imagesLoading) break;
    await sleep(60);
  }
  await sleep(120);
}

/** Seconds shown by a countdown ("1:05" -> 65), null when it shows no time. */
export function shownSeconds(text: string | null): number | null {
  const match = /(\d+):(\d\d)/.exec(text ?? '');
  return match ? Number(match[1]) * 60 + Number(match[2]) : null;
}

/** Take a JPEG screenshot and add it to the manifest with its checks. */
export async function shoot(
  page: Page,
  record: RunRecord,
  runDir: string,
  folder: string,
  screen: string,
  caption: string,
  checks: import('./matrix-rules').CheckResult[] = [],
  options: { fullPage?: boolean } = {},
): Promise<void> {
  const number = String(record.shots.length + 1).padStart(3, '0');
  const relative = path.join('shots', folder, `${number}-${screen}.jpg`);
  const file = path.join(runDir, relative);
  mkdirSync(path.dirname(file), { recursive: true });
  try {
    // A full-page shot draws fixed elements where they are on the screen: start from the top
    if (options.fullPage ?? true) await page.evaluate('window.scrollTo(0, 0)');
    // The mouse stays where the last click was: move it away, so no option looks hovered
    await page.mouse.move(0, 0);
    await page.screenshot({ path: file, type: 'jpeg', quality: 75, fullPage: options.fullPage ?? true, animations: 'disabled' });
  } catch (error) {
    record.notes.push(`screenshot ${screen} failed: ${error instanceof Error ? error.message.split('\n')[0] : error}`);
    return;
  }
  record.shots.push({
    case: record.caseName,
    device: record.device,
    theme: record.theme,
    language: record.language,
    screen,
    caption,
    file: relative.split(path.sep).join('/'),
    ...(checks.length ? { checks } : {}),
  });
}
