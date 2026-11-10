// The driver side of the teacher tool: one browser session per teacher, with tabs.
//
// Every command a teacher agent gives goes through TeacherDesk.command(). Like
// for students, the driver records each step itself: the command, the result,
// the screen text, the numbered list of elements, a screenshot, and every API
// request the pages sent with the reply. Dialogs are accepted, downloads are
// saved in <run_dir>/downloads/, and a link that opens a new tab moves the
// teacher to that tab.

import { existsSync, mkdirSync, statSync } from 'node:fs';
import path from 'node:path';
import type { BrowserContext, Dialog, Download, Frame, Locator, Page, Request } from 'playwright';
import { Classroom, CommandError, contextOptions, Lock, sleep } from './classroom';
import {
  COLLECT_JS,
  matchOption,
  replaceOnce,
  unescapeNewlines,
  type ElementInfo,
  type ElementKind,
  type TeacherStepRecord,
} from './elements';

export const TEACHER_COMMANDS = [
  'open',
  'look',
  'click',
  'fill',
  'replace',
  'select',
  'check',
  'uncheck',
  'press',
  'wait',
  'reload',
  'close',
  'students',
] as const;

const PAGES: Record<string, string> = {
  admin: '/admin/',
  files: '/files/',
  'live-stats': '/live-stats/',
  livestats: '/live-stats/',
  stats: '/live-stats/',
  quiz: '/',
};

const MAX_STUDENTS = 30;

function oneLine(text: string): string {
  return text.split('\n')[0].slice(0, 300);
}

function messageOf(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

function short(text: string, size = 60): string {
  const flat = text.replace(/\s+/g, ' ');
  return flat.length > size ? flat.slice(0, size - 1) + '…' : flat;
}

function describe(item: ElementInfo): string {
  return `[${item.n}] ${item.kind} "${short(item.label, 50)}"`;
}

interface Collected {
  text: string;
  items: ElementInfo[];
  count: number;
  overlay: boolean;
  frames: number;
}

interface Mark {
  frame: Frame;
  item: ElementInfo;
}

export class TeacherDesk {
  pages: Page[] = [];
  step = 0;
  wideShots = 0;
  private lock = new Lock();
  private marks = new Map<number, Mark>();
  private notes: string[] = [];
  private downloads: Promise<void>[] = [];
  private saved: { name: string; size: number; saved_as: string }[] = [];
  private inflight = new Set<Request>();
  private openingPage = false;
  private newTab = false;

  private constructor(
    private classroom: Classroom,
    readonly name: string,
    readonly device: string,
    readonly context: BrowserContext,
    private startStudents: (count: number) => { name: string; device: string }[],
  ) {
    context.on('page', (page) => {
      if (this.openingPage) return;
      this.adopt(page);
      this.pages.push(page);
      this.newTab = true;
    });
  }

  static async create(
    classroom: Classroom,
    name: string,
    device: string,
    startStudents: (count: number) => { name: string; device: string }[],
  ): Promise<TeacherDesk> {
    const context = await classroom.browser!.newContext({ ...contextOptions(device), acceptDownloads: true });
    classroom.extraContexts.push(context);
    classroom.log('participant', { student: name, role: 'teacher', device });
    classroom.log('note', { student: name, detail: `teacher added (${device})` });
    return new TeacherDesk(classroom, name, device, context, startStudents);
  }

  /** The tab the teacher works in: the last one opened that is still open. */
  get page(): Page | null {
    const open = this.pages.filter((page) => !page.isClosed());
    return open.length ? open[open.length - 1] : null;
  }

  private adopt(page: Page): void {
    this.classroom.watch({ name: this.name }, page);
    page.on('request', (request) => {
      const requestPath = request.url().replace(/^https?:\/\/[^/]+/, '');
      if (requestPath.startsWith('/api/')) this.inflight.add(request);
    });
    page.on('requestfinished', (request) => this.inflight.delete(request));
    page.on('requestfailed', (request) => this.inflight.delete(request));
    page.on('dialog', (dialog) => void this.onDialog(dialog));
    page.on('download', (download) => this.downloads.push(this.onDownload(download)));
    page.on('close', () => {
      this.pages = this.pages.filter((item) => item !== page);
    });
  }

  private async onDialog(dialog: Dialog): Promise<void> {
    const type = dialog.type();
    const message = dialog.message();
    try {
      if (type === 'prompt') {
        const value = dialog.defaultValue();
        await dialog.accept(value);
        this.notes.push(`DIALOG: a box asked "${message}" and was answered with its suggested text "${value}" (OK)`);
      } else if (type === 'confirm') {
        await dialog.accept();
        this.notes.push(`DIALOG: a box asked "${message}" and was answered OK (yes)`);
      } else {
        await dialog.accept();
        this.notes.push(`DIALOG: a message box said "${message}" (closed with OK)`);
      }
    } catch {
      // the page closed the dialog itself
    }
    this.classroom.log('note', { student: this.name, detail: `dialog (${type}): ${message}` });
  }

  private async onDownload(download: Download): Promise<void> {
    const name = download.suggestedFilename();
    const dir = path.join(this.classroom.runDir, 'downloads');
    mkdirSync(dir, { recursive: true });
    const ext = path.extname(name);
    const stem = name.slice(0, name.length - ext.length);
    let target = path.join(dir, name);
    for (let k = 2; existsSync(target); k++) target = path.join(dir, `${stem}-${k}${ext}`);
    try {
      await download.saveAs(target);
      const size = statSync(target).size;
      const savedAs = `downloads/${path.basename(target)}`;
      this.saved.push({ name, size, saved_as: savedAs });
      this.notes.push(`DOWNLOAD: the file "${name}" was downloaded (${size} bytes)`);
      this.classroom.log('note', { student: this.name, detail: `downloaded ${name} (${size} bytes) as ${savedAs}` });
    } catch (error) {
      this.notes.push(`DOWNLOAD: the file "${name}" could not be downloaded: ${oneLine(messageOf(error))}`);
    }
  }

  /** Run one teacher command and record it as a step. */
  async command(cmd: string, args: string[]): Promise<TeacherStepRecord> {
    if (!(TEACHER_COMMANDS as readonly string[]).includes(cmd)) {
      throw new CommandError(`unknown command '${cmd}', use one of: ${TEACHER_COMMANDS.join(', ')}`);
    }
    return this.lock.run(async () => {
      this.step += 1;
      const number = this.step;
      const started = this.classroom.now();
      this.notes = [];
      this.saved = [];
      this.newTab = false;
      let result = '';
      let error: string | null = null;
      try {
        result = await this.run(cmd, args);
      } catch (exc) {
        error = oneLine(messageOf(exc));
      }
      if (!['look', 'wait', 'students', 'close'].includes(cmd)) await this.settle();
      await Promise.all(this.downloads);
      this.downloads = [];
      const page = this.page;
      if (this.newTab && page) {
        this.notes.push(
          `NEW TAB: a new tab opened with ${page.url()} and you are in it now (${this.pages.length} tabs are open). ` +
            '"close" closes it and takes you back to the tab before.',
        );
      }
      const look = await this.lookAll();
      let shot: string | null = null;
      if (page && !page.isClosed()) {
        shot = `shots/${this.name}-${String(number).padStart(3, '0')}.png`;
        try {
          await page.screenshot({ path: path.join(this.classroom.runDir, shot) });
        } catch {
          shot = null;
        }
      }
      const width = await this.classroom.checkWidth(this, page, number, shot);
      const open = this.pages.filter((item) => !item.isClosed());
      return this.classroom.log('step', {
        student: this.name,
        role: 'teacher',
        step: number,
        t_start: started,
        cmd,
        args,
        arg: args.length ? args.join(' ') : null,
        result,
        error,
        events: [...this.notes],
        downloads: [...this.saved],
        url: page ? page.url() : null,
        tab: page ? open.indexOf(page) + 1 : 0,
        tabs: open.length,
        screen: look.text,
        elements: look.items,
        clickable: [],
        inputs: [],
        screenshot: shot,
        ...(width.wide ? { wide: width.wide } : {}),
      }) as unknown as TeacherStepRecord;
    });
  }

  /** Wait until the page has reacted: a short pause, then until its API requests are answered. */
  private async settle(): Promise<void> {
    await sleep(0.6);
    const deadline = Date.now() + 6000;
    while (this.inflight.size && Date.now() < deadline) await sleep(0.1);
    const page = this.page;
    if (page) {
      try {
        await page.waitForLoadState('domcontentloaded', { timeout: 5000 });
      } catch {
        // still loading: the screen shows what is there
      }
    }
    await sleep(0.3);
  }

  /** The screen text and the numbered elements of the current tab (and of the frames in it). */
  private async lookAll(): Promise<{ text: string; items: ElementInfo[] }> {
    this.marks.clear();
    const page = this.page;
    if (!page) return { text: '(no tab is open)', items: [] };
    let main: Collected;
    try {
      main = (await page.mainFrame().evaluate(`(${COLLECT_JS})(${JSON.stringify({ start: 1 })})`)) as Collected;
    } catch (error) {
      return { text: `(the page cannot be read: ${oneLine(messageOf(error))})`, items: [] };
    }
    const items = [...main.items];
    for (const item of main.items) this.marks.set(item.n, { frame: page.mainFrame(), item });
    let text = main.text;
    let next = 1 + main.count;
    if (main.frames > 0) {
      for (const frame of page.frames()) {
        if (frame === page.mainFrame()) continue;
        let mark: string | null = null;
        try {
          mark = await (await frame.frameElement()).getAttribute('data-harness-frame');
        } catch {
          continue;
        }
        if (!mark) continue;
        try {
          const inner = (await frame.evaluate(`(${COLLECT_JS})(${JSON.stringify({ start: next })})`)) as Collected;
          const address = frame.url().replace(/^https?:\/\/[^/]+/, '');
          text += `\n[a page inside the window: ${address}]\n${inner.text}`;
          for (const item of inner.items) {
            items.push(item);
            this.marks.set(item.n, { frame, item });
          }
          next += inner.count;
        } catch {
          // the frame is still loading
        }
      }
    }
    return { text, items };
  }

  private async element(arg: string | undefined, command: string, kinds?: ElementKind[]): Promise<{ locator: Locator; item: ElementInfo }> {
    const n = Number(arg);
    if (arg === undefined || !Number.isInteger(n)) throw new Error(`write the element number, for example: ${command} 5`);
    const mark = this.marks.get(n);
    if (!mark) {
      throw new Error(`there is no element [${n}] in the last list (it has ${this.marks.size}). Run "look" to see the list again.`);
    }
    const locator = mark.frame.locator(`[data-harness-n="${n}"]`);
    let count = 0;
    try {
      count = await locator.count();
    } catch {
      count = 0;
    }
    if (count === 0) throw new Error(`element [${n}] is gone (the page changed). Run "look" to get a new list.`);
    if (kinds && !kinds.includes(mark.item.kind)) {
      const use: Record<string, string> = {
        select: `select ${n} "option"`,
        input: `fill ${n} "text"`,
        textarea: `fill ${n} "text"`,
        checkbox: `check ${n} or uncheck ${n}`,
        radio: `check ${n}`,
      };
      const hint = use[mark.item.kind] ?? `click ${n}`;
      throw new Error(`[${n}] is a ${mark.item.kind}; "${command}" does not work on it. Use: ${hint}`);
    }
    return { locator: locator.first(), item: mark.item };
  }

  private async newPage(): Promise<Page> {
    this.openingPage = true;
    try {
      const page = await this.context.newPage();
      this.adopt(page);
      this.pages.push(page);
      return page;
    } finally {
      this.openingPage = false;
    }
  }

  private async run(cmd: string, args: string[]): Promise<string> {
    switch (cmd) {
      case 'open': {
        const where = (args[0] ?? '').toLowerCase();
        const target = PAGES[where];
        if (!target) throw new Error('open what? Use: open admin, open files, open live-stats or open quiz');
        const page = this.page ?? (await this.newPage());
        await page.goto(this.classroom.baseUrl + target);
        if (target === '/') await this.classroom.recordBoot(this.name, page);
        return `opened the ${where} page`;
      }
      case 'look':
        return 'looked at the screen';
      case 'wait': {
        const seconds = Math.min(Math.max(Number(args[0] || 1), 0), 90);
        if (!Number.isFinite(seconds)) throw new Error('write the number of seconds, for example: wait 10');
        await sleep(seconds);
        return `waited ${seconds} s`;
      }
      case 'students': {
        const count = Number(args[0]);
        if (!Number.isInteger(count) || count < 1 || count > MAX_STUDENTS) {
          throw new Error(`write how many students (1 to ${MAX_STUDENTS}), for example: students 5`);
        }
        const started = this.startStudents(count);
        const names = started.map((student) => `${student.name} (${student.device})`);
        this.classroom.log('note', { student: this.name, detail: `started automatic students: ${names.join(', ')}` });
        return (
          `${count} student${count === 1 ? ' is' : 's are'} joining: ${names.join(', ')}. ` +
          'They open the quiz page and take the quiz that runs now. Watch them on the live stats page.'
        );
      }
      default:
        break;
    }
    const page = this.page;
    if (!page) throw new Error('no tab is open. Use: open admin, open files, open live-stats or open quiz');
    switch (cmd) {
      case 'reload':
        await page.reload();
        if (new URL(page.url()).pathname === '/') await this.classroom.recordBoot(this.name, page);
        return 'reloaded the page';
      case 'close': {
        await page.close();
        const back = this.page;
        return back
          ? `closed the tab; you are back in the tab with ${back.url()}`
          : 'closed the last tab; no tab is open now (use: open admin, open files, open live-stats or open quiz)';
      }
      case 'press': {
        const key = args[0];
        if (!key) throw new Error('write the key, for example: press Enter');
        await page.keyboard.press(key);
        return `pressed ${key}`;
      }
      case 'click': {
        const { locator, item } = await this.element(args[0], 'click', [
          'button',
          'link',
          'option',
          'tab',
          'clickable',
          'checkbox',
          'radio',
          'input',
          'textarea',
        ]);
        await locator.click({ timeout: 5000 });
        return `clicked ${describe(item)}`;
      }
      case 'fill': {
        const { locator, item } = await this.element(args[0], 'fill', ['input', 'textarea']);
        if (args.length < 2) throw new Error(`write the text too, for example: fill ${args[0]} "text"`);
        const text = unescapeNewlines(args.slice(1).join(' '));
        await locator.fill(text, { timeout: 5000 });
        return `filled ${describe(item)} with "${short(text, 80)}"`;
      }
      case 'replace': {
        const { locator, item } = await this.element(args[0], 'replace', ['input', 'textarea']);
        if (args.length !== 3) {
          throw new Error(`write: replace ${args[0] ?? 'N'} "old text" "new text" (two texts, each in quotes)`);
        }
        const before = await locator.inputValue();
        const changed = replaceOnce(before, unescapeNewlines(args[1]), unescapeNewlines(args[2]));
        if ('error' in changed) throw new Error(changed.error);
        await locator.fill(changed.value, { timeout: 5000 });
        return `replaced "${short(args[1], 40)}" with "${short(args[2], 40)}" in ${describe(item)}`;
      }
      case 'select': {
        const { locator, item } = await this.element(args[0], 'select', ['select']);
        const wanted = args.slice(1);
        if (!wanted.length) throw new Error(`write the option too, for example: select ${args[0]} "${item.options?.[0] ?? 'option'}"`);
        const options = (await locator.evaluate((el) =>
          [...(el as HTMLSelectElement).options].map((o) => ({ value: o.value, label: (o.label || o.text).replace(/\s+/g, ' ').trim() })),
        )) as { value: string; label: string }[];
        let chosen = wanted.map((text) => matchOption(options, text));
        if (chosen.some((c) => typeof c === 'string') && wanted.length > 1) {
          const joined = matchOption(options, wanted.join(' '));
          if (typeof joined !== 'string') chosen = [joined];
        }
        const problem = chosen.find((c): c is string => typeof c === 'string');
        if (problem) throw new Error(problem);
        const picked = chosen as { value: string; label: string }[];
        if (picked.length > 1 && !item.multiple) throw new Error(`[${item.n}] lets you choose only one option`);
        await locator.selectOption(
          picked.map((option) => ({ value: option.value })),
          { timeout: 5000 },
        );
        return `chose ${picked.map((option) => `"${option.label}"`).join(', ')} in ${describe(item)}`;
      }
      case 'check':
      case 'uncheck': {
        const { locator, item } = await this.element(args[0], cmd, ['checkbox', 'radio']);
        if (cmd === 'uncheck' && item.kind === 'radio') {
          throw new Error(`[${item.n}] is a radio button: choose another one with check instead`);
        }
        const want = cmd === 'check';
        const before = await locator.isChecked();
        if (before === want) return `${describe(item)} was already ${want ? 'on' : 'off'}`;
        await locator.setChecked(want, { timeout: 5000 });
        return `turned ${want ? 'on' : 'off'} ${describe(item)}`;
      }
      default:
        throw new Error(cmd);
    }
  }
}
