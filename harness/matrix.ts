// The settings matrix: students go through many combinations of quiz settings and
// config, every screen is a screenshot, and every screen is checked against what
// WebQuiz 1.19 shows for that combination. The teacher's pages get a tour too.
//
//     harness/node_modules/.bin/tsx harness/matrix.ts [--case NAME] [--quick] [--jobs N] [--binary PATH]
//
// --case NAME   run one case (see the list printed at the start); "teacher" runs only the tour
// --quick       only desktop with the light theme (and one teacher tour)
// --jobs N      runs at the same time (default 3)
// --binary PATH the WebQuiz binary (default: $WEBQUIZ_BINARY, then e2e/.bin/webquiz, then bin/webquiz)
//
// Output: harness/runs/<stamp>-matrix/ with report.md, manifest.json, shots/ and
// servers/ (each run's server folder: config, quiz, CSV files, log).

import { execFileSync } from 'node:child_process';
import { existsSync, mkdirSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { chromium } from 'playwright';
import { allCases, describeCase, DESKTOP_LIGHT, type MatrixCase, type Screen } from './matrix-cases';
import { writeManifest, writeReport, type RunRecord } from './matrix-report';
import { runStudentCase, type MatrixEnv } from './matrix-student';
import { runTeacherTour, TOURS, type TourScreen } from './matrix-teacher';

const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');

interface Options {
  caseName: string | null;
  quick: boolean;
  jobs: number;
  binary: string | null;
}

function fail(message: string): never {
  console.error(`error: ${message}`);
  process.exit(2);
}

function parseArgs(args: string[]): Options {
  const options: Options = { caseName: null, quick: false, jobs: 3, binary: null };
  for (let i = 0; i < args.length; i++) {
    const arg = args[i];
    if (arg === '--quick') options.quick = true;
    else if (arg === '--case') options.caseName = args[++i] ?? fail('--case needs a name');
    else if (arg === '--jobs') options.jobs = Number(args[++i]);
    else if (arg === '--binary') options.binary = args[++i] ?? fail('--binary needs a path');
    else fail(`unknown option ${arg}`);
  }
  if (!Number.isInteger(options.jobs) || options.jobs < 1) fail('--jobs needs a whole number, 1 or more');
  return options;
}

function findBinary(given: string | null): string {
  const exe = process.platform === 'win32' ? 'webquiz.exe' : 'webquiz';
  const candidates = [given, process.env.WEBQUIZ_BINARY, path.join(ROOT, 'e2e', '.bin', exe), path.join(ROOT, 'bin', exe)];
  for (const candidate of candidates) {
    if (candidate && existsSync(candidate)) return path.resolve(candidate);
  }
  return fail('no WebQuiz binary found: pass --binary PATH, set WEBQUIZ_BINARY, or run `make build`');
}

function stamp(): string {
  const now = new Date();
  const two = (n: number) => String(n).padStart(2, '0');
  return `${now.getFullYear()}${two(now.getMonth() + 1)}${two(now.getDate())}-${two(now.getHours())}${two(now.getMinutes())}${two(now.getSeconds())}`;
}

type Job = { kind: 'student'; matrixCase: MatrixCase; screen: Screen; weight: number } | { kind: 'teacher'; tour: TourScreen; weight: number };

/** Long runs first, so the last runs do not wait alone. */
function weight(matrixCase: MatrixCase, screen: Screen): number {
  const time = { whole_quiz: 3, per_question: 2, none: 1 }[matrixCase.settings.time_limit];
  return time * 10 + (matrixCase.approve ? 2 : 0) + (screen.device === 'phone' ? 1 : 0);
}

function planJobs(cases: MatrixCase[], options: Options): Job[] {
  const jobs: Job[] = [];
  const wanted = options.caseName;
  if (wanted !== 'teacher') {
    for (const matrixCase of cases) {
      if (wanted && matrixCase.name !== wanted) continue;
      const screens = options.quick ? [DESKTOP_LIGHT] : matrixCase.screens;
      for (const screen of screens) jobs.push({ kind: 'student', matrixCase, screen, weight: weight(matrixCase, screen) });
    }
  }
  if (!wanted || wanted === 'teacher') {
    for (const tour of options.quick ? TOURS.slice(0, 1) : TOURS) jobs.push({ kind: 'teacher', tour, weight: 25 });
  }
  return jobs.sort((a, b) => b.weight - a.weight);
}

async function pool<T, R>(items: T[], size: number, work: (item: T) => Promise<R>): Promise<R[]> {
  const results: R[] = new Array(items.length);
  let next = 0;
  const workers = Array.from({ length: Math.min(size, items.length) }, async () => {
    while (next < items.length) {
      const index = next++;
      results[index] = await work(items[index]);
    }
  });
  await Promise.all(workers);
  return results;
}

async function main(): Promise<void> {
  const options = parseArgs(process.argv.slice(2));
  const binary = findBinary(options.binary);
  const version = execFileSync(binary, ['--version'], { encoding: 'utf-8' }).trim().replace(/^webquiz\s+/i, '');
  const cases = allCases();
  const names = cases.map((c) => c.name);
  if (options.caseName && options.caseName !== 'teacher' && !names.includes(options.caseName)) {
    fail(`unknown case ${options.caseName}; the cases are: ${names.join(', ')}, teacher`);
  }
  const jobs = planJobs(cases, options);
  const runDir = path.join(ROOT, 'harness', 'runs', `${stamp()}-matrix`);
  mkdirSync(runDir, { recursive: true });
  console.log(`Matrix run: ${runDir}`);
  console.log(`Binary: ${binary} (webquiz ${version})`);
  console.log(`Cases: ${names.join(', ')}`);
  console.log(`Runs: ${jobs.length}, ${options.jobs} at a time`);

  const started = Date.now();
  const browser = await chromium.launch();
  const env: MatrixEnv = { browser, binary, version, runDir };
  let done = 0;
  const records: RunRecord[] = [];
  try {
    const finished = await pool(jobs, options.jobs, async (job) => {
      const record = job.kind === 'student' ? await runStudentCase(env, job.matrixCase, job.screen) : await runTeacherTour(env, job.tour);
      done++;
      const failed = record.failed().length;
      console.log(
        `[${done}/${jobs.length}] ${record.label}: ${record.checks.length} checks, ${failed} failed, ` +
          `${record.shots.length} screenshots, ${Math.round(record.seconds)} s${record.error ? ` — stopped: ${record.error}` : ''}`,
      );
      return record;
    });
    records.push(...finished);
  } finally {
    await browser.close();
  }

  // The report lists the runs in case order
  const order = (record: RunRecord) => {
    const index = names.indexOf(record.caseName);
    return (index === -1 ? names.length : index) * 10 + ['desktop-light', 'phone-dark', 'desktop-dark', 'phone-light'].indexOf(`${record.device}-${record.theme}`);
  };
  records.sort((a, b) => order(a) - order(b) || a.language.localeCompare(b.language));
  const shownCases = cases.filter((c) => records.some((record) => record.caseName === c.name));
  const report = writeReport(
    runDir,
    {
      title: `WebQuiz settings matrix, ${path.basename(runDir)}`,
      binary,
      version: `webquiz ${version}`,
      started: new Date(started).toISOString(),
      seconds: (Date.now() - started) / 1000,
      caseLines: [
        ...shownCases.map((c) => ({ name: c.name, summary: describeCase(c) })),
        ...(records.some((record) => record.caseName === 'teacher') ? [{ name: 'teacher', summary: 'teacher pages: desktop and phone, light and dark, uk and en' }] : []),
      ],
      notes: [
        'Expected values come from WebQuiz 1.19 (Python): each check names the file and line of the rule.',
        'Screenshots: JPEG, quality 75; desktop 1280×860 at scale 1, phone = Playwright "iPhone 13".',
      ],
    },
    records,
  );
  const manifest = writeManifest(runDir, records);
  const checks = records.reduce((sum, record) => sum + record.checks.length, 0);
  const failed = records.reduce((sum, record) => sum + record.failed().length, 0);
  const shots = records.reduce((sum, record) => sum + record.shots.length, 0);
  console.log(`DONE ${runDir}`);
  console.log(`${records.length} runs, ${shots} screenshots, ${checks} checks, ${failed} failed. See ${path.relative(ROOT, report)} and ${path.relative(ROOT, manifest)}.`);
}

await main();
