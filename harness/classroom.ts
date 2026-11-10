// The browser driver: one browser, one isolated session per student.
//
// Every command a student (AI agent or script) gives goes through
// Classroom.command(). The driver, not the student, records each step:
// the command, the screen text after it, a screenshot, and every API request
// the page sent with its reply. That record is what the agents' own reports
// are compared against.

import { createWriteStream, mkdirSync, type WriteStream } from 'node:fs';
import path from 'node:path';
import { chromium, devices, type Browser, type BrowserContext, type Page, type Request, type Response } from 'playwright';
import { WIDE_JS } from './elements';
import type { Clickable, InputField, StepRecord } from './steps';

// Reads what a person would see: page text, things to click, places to type.
// (Passed to the page as text, so no build tool can change the function.)
const LOOK_JS = String.raw`() => {
  const visible = el => {
    if (!el.getClientRects().length) return false;
    const style = getComputedStyle(el);
    return style.visibility !== 'hidden' && style.display !== 'none' && el.closest('.hidden') === null;
  };
  const text = (document.body.innerText || '').replace(/\n{3,}/g, '\n\n').trim();
  const clickable = [];
  const imageName = img => {
    let name = (img.getAttribute('src') || '').split('?')[0].split('/').pop() || '';
    try { name = decodeURIComponent(name); } catch (e) { /* keep it */ }
    return name;
  };
  document.querySelectorAll('button, .option, a').forEach(el => {
    if (!visible(el)) return;
    let label = (el.innerText || '').trim().replace(/\s+/g, ' ');
    // A picture (an answer option or the question's picture) is named by its file
    const img = el.querySelector('img');
    if (img && visible(img)) label = ('[image ' + imageName(img) + '] ' + label).trim();
    if (!label) return;
    const disabled = !!el.disabled || el.classList.contains('disabled');
    clickable.push({kind: el.classList.contains('option') ? 'option' : el.tagName.toLowerCase(), label, disabled});
  });
  const inputs = [];
  document.querySelectorAll('input[type=text], input:not([type]), textarea').forEach(el => {
    if (!visible(el)) return;
    const own = el.id ? document.querySelector('label[for="' + el.id + '"]') : null;
    const label = own && own.innerText.trim() ? own.innerText.trim() : (el.placeholder || el.id || '');
    inputs.push({kind: el.tagName.toLowerCase(), label, value: el.value, disabled: el.disabled});
  });
  return {text, clickable, inputs};
}`;

// A short state summary for scripted students
const STATE_JS = String.raw`() => {
  const shown = id => { const el = document.getElementById(id); return !!el && !el.classList.contains('hidden') && el.getClientRects().length > 0; };
  const q = document.querySelector('#current-question-container .question-text');
  const timer = document.getElementById('question-timer');
  return {
    question: q ? q.innerText.trim() : null,
    question_timer: timer ? timer.textContent : null,
    quiz_timer: shown('quiz-timer-box') ? document.getElementById('quiz-timer').textContent : null,
    results: shown('results'),
    can_continue: shown('continue-btn') && !document.getElementById('continue-btn').disabled,
    can_next: shown('explanation-step') && !document.getElementById('explanation-next-btn').disabled,
    time_up: !!document.querySelector('#current-question-container .time-up-message'),
  };
}`;

// What the quiz page was told when it loaded (its boot data), for the checks:
// the questions with their ids, the settings and the texts of the results screen.
const BOOT_JS = String.raw`() => {
  const el = document.getElementById('webquiz-boot');
  if (!el) return null;
  let boot;
  try { boot = JSON.parse(el.textContent || ''); } catch (e) { return null; }
  if (!boot || typeof boot !== 'object' || !('questions' in boot)) return null;
  const T = boot.translations || {};
  const keys = ['result_label', 'question_header', 'your_answer_header', 'correct_header_alt', 'correct_header',
    'answers_available_later', 'explanation_title', 'submit_answer', 'register_button', 'time_is_up', 'continue_button'];
  const texts = {};
  keys.forEach((key) => { if (typeof T[key] === 'string') texts[key] = T[key]; });
  return {
    state: boot.state,
    language: boot.language,
    title: boot.title,
    questions: (boot.questions || []).map((q) => ({
      id: q.id,
      question: q.question === undefined ? null : q.question,
      image: q.image === undefined ? null : q.image,
      time_limit: q.time_limit,
      type: q.type === 'text' ? 'text' : 'choice',
      options: Array.isArray(q.options) ? q.options : null,
      points: q.points,
    })),
    registration_fields: boot.registration_fields || [],
    username_label: boot.username_label,
    show_right_answer: boot.show_right_answer,
    show_final_list: boot.show_final_list,
    quiz_time_limit: boot.quiz_time_limit,
    texts,
  };
}`;

export const COMMANDS = ['open', 'look', 'click', 'type', 'wait', 'reload', 'offline', 'close'] as const;
/** How many "page too wide" screenshots to take per phone participant. */
const MAX_WIDE_SHOTS = 4;

export type Device = 'desktop' | 'phone';
export type Role = 'student' | 'teacher' | 'auto';

/** Browser settings for a device: a phone is an iPhone 13. */
export function contextOptions(device: string) {
  return device === 'phone' ? { ...devices['iPhone 13'] } : { viewport: { width: 1100, height: 900 } };
}
const MAX_SCREEN = 2500;

export interface PageState {
  question?: string | null;
  question_timer?: string | null;
  quiz_timer?: string | null;
  results?: boolean;
  can_continue?: boolean;
  can_next?: boolean;
  time_up?: boolean;
}

interface Look {
  text: string;
  clickable: Clickable[];
  inputs: InputField[];
}

/** A client mistake in a command (unknown student or command): the API answers 400. */
export class CommandError extends Error {}

export class Lock {
  private tail: Promise<unknown> = Promise.resolve();
  run<T>(task: () => Promise<T>): Promise<T> {
    const result = this.tail.then(task, task);
    this.tail = result.catch(() => undefined);
    return result;
  }
}

export class Student {
  page: Page | null = null;
  step = 0;
  lock = new Lock();
  offlineTimer: ReturnType<typeof setTimeout> | null = null;
  wideShots = 0;
  constructor(
    public name: string,
    public context: BrowserContext,
    public device: string,
    public role: Role = 'student',
  ) {}
}

const sleep = (seconds: number) => new Promise<void>((resolve) => setTimeout(resolve, seconds * 1000));
export { sleep };

function oneLine(text: string): string {
  return text.split('\n')[0].slice(0, 300);
}

function escapeRegExp(text: string): string {
  return text.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}

/** Owns the browser, the students and the run record (events.jsonl, shots/). */
export class Classroom {
  readonly baseUrl: string;
  readonly runDir: string;
  students = new Map<string, Student>();
  browser: Browser | null = null;
  private events: WriteStream;
  private t0 = performance.now();
  private reqCounter = 0;
  private reqIds = new WeakMap<Request, number>();

  constructor(baseUrl: string, runDir: string) {
    this.baseUrl = baseUrl.replace(/\/+$/, '');
    this.runDir = runDir;
    mkdirSync(path.join(runDir, 'shots'), { recursive: true });
    this.events = createWriteStream(path.join(runDir, 'events.jsonl'), { flags: 'a', encoding: 'utf-8' });
  }

  now(): number {
    return Math.round(((performance.now() - this.t0) / 1000) * 100) / 100;
  }

  private closed = false;

  log(kind: string, data: Record<string, unknown> = {}): Record<string, unknown> {
    const record = { t: this.now(), wall: new Date().toISOString(), kind, ...data };
    // Late replies can arrive while the run stops: they are not written then
    if (!this.closed) this.events.write(JSON.stringify(record) + '\n');
    return record;
  }

  async start(): Promise<void> {
    this.browser = await chromium.launch();
  }

  /** Contexts that are not students' (teachers), closed on stop. */
  extraContexts: BrowserContext[] = [];

  async stop(): Promise<void> {
    for (const context of this.extraContexts) {
      try {
        await context.close();
      } catch {
        // already closed
      }
    }
    for (const student of this.students.values()) {
      if (student.offlineTimer) clearTimeout(student.offlineTimer);
      try {
        await student.context.close();
      } catch {
        // already closed
      }
    }
    if (this.browser) await this.browser.close();
    this.closed = true;
    await new Promise<void>((resolve) => this.events.end(resolve));
  }

  async addStudent(name: string, device = 'desktop', role: Role = 'student'): Promise<Student> {
    const context = await this.browser!.newContext(contextOptions(device));
    const student = new Student(name, context, device, role);
    this.students.set(name, student);
    this.log('participant', { student: name, role, device });
    this.log('note', { student: name, detail: `${role === 'student' ? 'student' : role} added (${device})` });
    return student;
  }

  /** Record every /api/ request the page sends, with its reply. */
  watch(student: { name: string }, page: Page): void {
    page.on('request', (request) => {
      const requestPath = request.url().replace(/^https?:\/\/[^/]+/, '').split('?')[0];
      if (!requestPath.startsWith('/api/')) return;
      this.reqCounter += 1;
      this.reqIds.set(request, this.reqCounter);
      let body: unknown = null;
      const raw = request.postData();
      if (raw) {
        try {
          body = JSON.parse(raw);
        } catch {
          body = raw;
        }
      }
      this.log('request', {
        student: student.name,
        req_id: this.reqCounter,
        method: request.method(),
        path: requestPath,
        body,
      });
    });
    page.on('response', (response: Response) => {
      const reqId = this.reqIds.get(response.request());
      if (reqId === undefined) return;
      void (async () => {
        let body: unknown;
        try {
          const text = await response.text();
          try {
            body = JSON.parse(text);
          } catch {
            body = text.slice(0, 500);
          }
        } catch (error) {
          body = `<unreadable: ${error instanceof Error ? error.message : error}>`;
        }
        this.log('response', { student: student.name, req_id: reqId, status: response.status(), body });
      })();
    });
    page.on('requestfailed', (request) => {
      const reqId = this.reqIds.get(request);
      if (reqId !== undefined) {
        this.log('request_failed', {
          student: student.name,
          req_id: reqId,
          detail: `${request.url()} ${request.failure()?.errorText ?? ''}`,
        });
      }
    });
  }

  async look(student: Student): Promise<Look> {
    if (!student.page || student.page.isClosed()) {
      return { text: "(no page open: use 'open')", clickable: [], inputs: [] };
    }
    try {
      return (await student.page.evaluate(`(${LOOK_JS})()`)) as Look;
    } catch (error) {
      return { text: `(page not readable: ${error instanceof Error ? error.message : error})`, clickable: [], inputs: [] };
    }
  }

  async state(name: string): Promise<PageState> {
    const student = this.students.get(name)!;
    if (!student.page || student.page.isClosed()) return {};
    try {
      return (await student.page.evaluate(`(${STATE_JS})()`)) as PageState;
    } catch {
      return {};
    }
  }

  /** Run a function (given as text) in a student's page; null when there is no page or it fails. */
  async evaluate<T>(name: string, js: string): Promise<T | null> {
    const student = this.students.get(name);
    if (!student || !student.page || student.page.isClosed()) return null;
    try {
      return (await student.page.evaluate(`(${js})()`)) as T;
    } catch {
      return null;
    }
  }

  /** Record what the quiz page was told when it loaded (see BOOT_JS). */
  async recordBoot(name: string, page: Page): Promise<void> {
    try {
      const boot = await page.evaluate(`(${BOOT_JS})()`);
      if (boot) this.log('boot', { student: name, url: page.url(), boot });
    } catch {
      // the page went away
    }
  }

  /**
   * On a phone, is the page wider than the screen? Then a full-page screenshot is
   * kept (also when screenshots are off), so the report can point at it.
   */
  async checkWidth(
    student: { name: string; device: string; wideShots: number },
    page: Page | null,
    number: number,
    shot: string | null,
  ): Promise<{ wide: Record<string, unknown> | null; shot: string | null }> {
    if (student.device !== 'phone' || !page || page.isClosed()) return { wide: null, shot };
    let wide: Record<string, unknown> | null = null;
    try {
      wide = (await page.evaluate(`(${WIDE_JS})()`)) as Record<string, unknown> | null;
    } catch {
      return { wide: null, shot };
    }
    if (!wide) return { wide: null, shot };
    if (student.wideShots < MAX_WIDE_SHOTS) {
      student.wideShots += 1;
      const wideShot = `shots/${student.name}-${String(number).padStart(3, '0')}-wide.png`;
      try {
        await page.screenshot({ path: path.join(this.runDir, wideShot), fullPage: true });
        wide.screenshot = wideShot;
      } catch {
        // no screenshot
      }
    }
    if (!wide.screenshot && shot) wide.screenshot = shot;
    return { wide, shot };
  }

  /** Run one command for a student and record it as a step. Returns the step record. */
  async command(
    name: string,
    cmd: string,
    arg: string | number | null = null,
    {
      screenshot = true,
      settle = 0.7,
      selector,
    }: { screenshot?: boolean; settle?: number; selector?: string } = {},
  ): Promise<StepRecord> {
    const student = this.students.get(name);
    if (!student) throw new CommandError(`unknown student '${name}'`);
    if (!(COMMANDS as readonly string[]).includes(cmd)) {
      throw new CommandError(`unknown command '${cmd}', use one of: ${COMMANDS.join(', ')}`);
    }
    return student.lock.run(async () => {
      student.step += 1;
      const number = student.step;
      const started = this.now();
      let error: string | null = null;
      let result = '';
      try {
        result = await this.run(student, cmd, arg === null ? null : String(arg), selector);
      } catch (exc) {
        error = oneLine(exc instanceof Error ? exc.message : String(exc));
      }
      if (!['wait', 'look', 'close'].includes(cmd) && settle) await sleep(settle);
      const screen = await this.look(student);
      let shot: string | null = null;
      if (screenshot && student.page && !student.page.isClosed()) {
        shot = `shots/${name}-${String(number).padStart(3, '0')}.png`;
        try {
          await student.page.screenshot({ path: path.join(this.runDir, shot) });
        } catch {
          shot = null;
        }
      }
      const width = await this.checkWidth(student, student.page, number, shot);
      return this.log('step', {
        student: name,
        step: number,
        t_start: started,
        cmd,
        arg,
        result,
        error,
        screen: screen.text.slice(0, MAX_SCREEN),
        clickable: screen.clickable,
        inputs: screen.inputs,
        screenshot: shot,
        ...(width.wide ? { wide: width.wide } : {}),
      }) as unknown as StepRecord;
    });
  }

  private async run(student: Student, cmd: string, arg: string | null, selector?: string): Promise<string> {
    if (cmd === 'open') {
      if (!student.page || student.page.isClosed()) {
        student.page = await student.context.newPage();
        this.watch(student, student.page);
      }
      await student.page.goto(this.baseUrl + '/');
      await this.recordBoot(student.name, student.page);
      return 'opened the quiz page';
    }
    if (cmd === 'look') return 'looked at the screen';
    if (!student.page || student.page.isClosed()) {
      if (cmd === 'wait') {
        await sleep(Math.min(Number(arg || 1), 90));
        return `waited ${arg} s (no page open)`;
      }
      throw new Error("no page open: use 'open' first");
    }
    const page = student.page;
    if (cmd === 'click' && selector) {
      // Scripted and automatic students know exactly what to click (also an option with only a picture)
      await page.locator(selector).first().click({ timeout: 3000 });
      return `clicked '${String(arg ?? '').trim()}'`;
    }
    if (cmd === 'type' && selector) {
      await page.locator(selector).first().fill(String(arg ?? ''), { timeout: 3000 });
      return `typed '${arg}'`;
    }
    if (cmd === 'click') {
      const label = String(arg ?? '').trim();
      const everything = page.locator('button, .option, a');
      const picture = /^\[image ([^\]]+)\]/.exec(label);
      if (picture) {
        // "[image circle.png]": the option (or link) that shows this picture
        const withImage = everything
          .filter({ has: page.locator(`img[src$=${JSON.stringify(encodeURI(picture[1]))}], img[src$=${JSON.stringify(picture[1])}]`) })
          .locator('visible=true');
        if ((await withImage.count()) === 0) throw new Error(`nothing to click with the picture '${picture[1]}'`);
        await withImage.first().click({ timeout: 3000 });
        return `clicked '${label}'`;
      }
      let candidates = everything.filter({ hasText: new RegExp('^\\s*' + escapeRegExp(label) + '\\s*$') }).locator('visible=true');
      if ((await candidates.count()) === 0) {
        candidates = everything.filter({ hasText: label }).locator('visible=true');
      }
      if ((await candidates.count()) === 0) throw new Error(`nothing to click with the text '${label}'`);
      await candidates.first().click({ timeout: 3000 });
      return `clicked '${label}'`;
    }
    if (cmd === 'type') {
      // The answer box; else the first empty field of a form (the name, then the other fields in turn)
      let field = page.locator('textarea:visible:enabled');
      if ((await field.count()) === 0) {
        field = page.locator('input[type=text]:visible:enabled, input:not([type]):visible:enabled');
      }
      const count = await field.count();
      if (count === 0) throw new Error('no place to type on the screen');
      let target = field.first();
      for (let i = 0; i < count; i++) {
        if ((await field.nth(i).inputValue()) === '') {
          target = field.nth(i);
          break;
        }
      }
      const name = await target.evaluate(
        (el) => (el.id && document.querySelector(`label[for="${el.id}"]`)?.textContent?.trim()) || (el as HTMLInputElement).placeholder || el.id || 'the field',
      );
      await target.fill(String(arg ?? ''));
      return `typed '${arg}' into ${name}`;
    }
    if (cmd === 'wait') {
      const seconds = Math.min(Number(arg || 1), 90);
      await sleep(seconds);
      return `waited ${seconds} s`;
    }
    if (cmd === 'reload') {
      await page.reload();
      await this.recordBoot(student.name, page);
      return 'reloaded the page';
    }
    if (cmd === 'offline') {
      const seconds = Math.min(Number(arg || 5), 120);
      await student.context.setOffline(true);
      this.log('network', { student: student.name, detail: `offline for ${seconds} s` });
      if (student.offlineTimer) clearTimeout(student.offlineTimer);
      student.offlineTimer = setTimeout(() => {
        void (async () => {
          try {
            await student.context.setOffline(false);
            this.log('network', { student: student.name, detail: 'online again' });
          } catch {
            // the context was closed
          }
        })();
      }, seconds * 1000);
      return `Wi-Fi off for ${seconds} s (the command returns at once)`;
    }
    if (cmd === 'close') {
      await page.close();
      return 'closed the tab';
    }
    throw new Error(cmd);
  }

  /** Scripted students use exact selectors, but still go through command() for "click". */
  async clickSelector(name: string, selector: string, screenshot = false, label?: string): Promise<StepRecord> {
    const student = this.students.get(name)!;
    const text = label ?? (await student.page!.locator(selector).first().innerText()).trim();
    return this.command(name, 'click', text, { screenshot, settle: 0.3, selector });
  }
}
