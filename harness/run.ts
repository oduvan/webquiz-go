// Run one scenario: server, browser, teacher, scripted students, and the API the AI agents use.
//
//     npx tsx harness/run.ts normal-lesson [--no-agents] [--class-size N]
//
// It prints "READY <run_dir>" once the teacher has started the quiz. The AI
// agent students (started by the Claude session) then use
// `npx tsx harness/student.ts <name> <command>`. When they are done, save each
// agent's report as <run_dir>/agents/<name>.md and create <run_dir>/agents_done.
// The run then waits for the server to finish its own time checks, stops
// everything and writes <run_dir>/report.md.

import { ChildProcess, spawn } from 'node:child_process';
import { closeSync, existsSync, mkdirSync, openSync, writeFileSync } from 'node:fs';
import http from 'node:http';
import net from 'node:net';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { stringify } from 'yaml';
import { analyze } from './checks';
import { Classroom, CommandError, sleep } from './classroom';
import { bigClassScenario, SCENARIOS, type Scenario } from './scenarios';
import { scriptedStudent, teacherApproves, teacherStart } from './scripted';
import { formatStep } from './steps';

const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const SERVER_PORT = 8199;
const API_PORT = 8198;
const MASTER_KEY = 'harness';
const DEFAULT_GRACE_SECONDS = 5;

const clock = () => performance.now() / 1000;

/** The grace the server uses (tests can shorten it with the same variable the server reads). */
function graceSeconds(): number {
  const value = Number(process.env.WEBQUIZ_TIME_LIMIT_GRACE_SECONDS);
  return process.env.WEBQUIZ_TIME_LIMIT_GRACE_SECONDS && Number.isFinite(value) ? value : DEFAULT_GRACE_SECONDS;
}

function findBinary(): string {
  const exe = process.platform === 'win32' ? 'webquiz.exe' : 'webquiz';
  const candidates = [process.env.WEBQUIZ_BINARY, path.join(ROOT, 'e2e', '.bin', exe), path.join(ROOT, 'bin', exe)];
  for (const candidate of candidates) {
    if (candidate && existsSync(candidate)) return candidate;
  }
  throw new Error('No WebQuiz binary found. Run `make build` (gives bin/webquiz) or set WEBQUIZ_BINARY.');
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

async function waitForPort(port: number, seconds = 20): Promise<void> {
  const deadline = clock() + seconds;
  while (clock() < deadline) {
    if (await canConnect(port)) return;
    await sleep(0.2);
  }
  throw new Error(`nothing listens on port ${port}`);
}

async function startServer(runDir: string, scenario: Scenario): Promise<ChildProcess> {
  mkdirSync(path.join(runDir, 'quizzes'), { recursive: true });
  writeFileSync(path.join(runDir, 'quizzes', scenario.quiz_file), stringify(scenario.quiz), 'utf-8');
  const config: Record<string, Record<string, unknown>> = {
    server: { port: SERVER_PORT },
    paths: {
      quizzes_dir: path.join(runDir, 'quizzes'),
      logs_dir: path.join(runDir, 'logs'),
      csv_dir: path.join(runDir, 'data'),
      static_dir: path.join(runDir, 'static'),
    },
    admin: { master_key: MASTER_KEY },
  };
  for (const [section, values] of Object.entries(scenario.config ?? {})) {
    config[section] = { ...(config[section] ?? {}), ...values };
  }
  const configPath = path.join(runDir, 'config.yaml');
  writeFileSync(configPath, stringify(config), 'utf-8');
  const log = openSync(path.join(runDir, 'server.log'), 'w');
  const child = spawn(findBinary(), ['--config', configPath, '--port', String(SERVER_PORT)], {
    cwd: runDir,
    // Like the e2e tests: the binary takes its base folder from WEBQUIZ_BINARY_DIR
    // and WEBQUIZ_IS_BINARY=0 keeps it from opening a browser.
    env: { ...process.env, WEBQUIZ_BINARY_DIR: runDir, WEBQUIZ_IS_BINARY: '0' },
    stdio: ['ignore', log, log],
  });
  closeSync(log);
  const exited = new Promise<never>((_, reject) =>
    child.once('exit', (code) => reject(new Error(`the server exited with code ${code}, see ${runDir}/server.log`))),
  );
  await Promise.race([waitForPort(SERVER_PORT), exited]);
  return child;
}

async function stopServer(server: ChildProcess): Promise<void> {
  if (server.exitCode !== null) return;
  const done = new Promise<void>((resolve) => server.once('exit', () => resolve()));
  server.kill('SIGTERM');
  const timer = setTimeout(() => server.kill('SIGKILL'), 10_000);
  await done;
  clearTimeout(timer);
}

/** Save every live stats message the teacher's screen would get. */
function recordLiveStats(classroom: Classroom, stop: { done: boolean }): Promise<void> {
  return new Promise((resolve) => {
    const socket = new WebSocket(`ws://127.0.0.1:${SERVER_PORT}/ws/live-stats`);
    socket.onmessage = (event) => {
      try {
        classroom.log('live', { message: JSON.parse(String(event.data)) });
      } catch {
        // not JSON: ignore
      }
    };
    socket.onerror = () => classroom.log('note', { detail: 'live stats recorder stopped: socket error' });
    socket.onclose = () => resolve();
    const watcher = setInterval(() => {
      if (stop.done) {
        clearInterval(watcher);
        socket.close();
        resolve();
      }
    }, 500);
  });
}

/** The HTTP API behind `npx tsx harness/student.ts`. */
function startApi(classroom: Classroom): Promise<http.Server> {
  const server = http.createServer((request, response) => {
    const reply = (status: number, text: string) => {
      response.writeHead(status, { 'Content-Type': 'text/plain; charset=utf-8' });
      response.end(text);
    };
    if (request.method !== 'POST' || request.url !== '/cmd') return reply(404, 'not found');
    let raw = '';
    request.setEncoding('utf-8');
    request.on('data', (chunk) => (raw += chunk));
    request.on('end', () => {
      void (async () => {
        try {
          const data = JSON.parse(raw) as { student: string; cmd: string; arg?: string | null };
          const record = await classroom.command(data.student, data.cmd, data.arg ?? null);
          reply(200, formatStep(record));
        } catch (error) {
          if (error instanceof CommandError || error instanceof SyntaxError) reply(400, `ERROR: ${error.message}`);
          else reply(500, `ERROR: ${error instanceof Error ? error.message : error}`);
        }
      })();
    });
  });
  return new Promise((resolve) => server.listen(API_PORT, '127.0.0.1', () => resolve(server)));
}

/** Per-question limits the running quiz uses (question id -> seconds), for the checks. */
function effectiveLimits(scenario: Scenario, settings: Record<string, any> | undefined): Record<number, number> {
  const timeLimit = settings?.time_limit as Record<string, number> | null | undefined;
  const perQuestion = timeLimit && typeof timeLimit.per_question === 'number' ? timeLimit.per_question : 0;
  const limits: Record<number, number> = {};
  scenario.quiz.questions.forEach((question, index) => {
    let limit = 0;
    if (perQuestion > 0) {
      const own = question.time_limit;
      limit = Number.isInteger(own) && (own as number) >= 0 ? (own as number) : perQuestion;
    }
    limits[index + 1] = limit;
  });
  return limits;
}

async function run(scenario: Scenario, withAgents: boolean): Promise<string> {
  const now = new Date();
  const two = (n: number) => String(n).padStart(2, '0');
  const stamp = `${now.getFullYear()}${two(now.getMonth() + 1)}${two(now.getDate())}-${two(now.getHours())}${two(now.getMinutes())}${two(now.getSeconds())}`;
  const runDir = path.join(ROOT, 'harness', 'runs', `${stamp}-${scenario.name}`);
  mkdirSync(path.join(runDir, 'agents'), { recursive: true });
  const server = await startServer(runDir, scenario);
  const classroom = new Classroom(`http://127.0.0.1:${SERVER_PORT}`, runDir);
  const stop = { done: false };
  let api: http.Server | null = null;
  let recorder: Promise<void> | null = null;
  try {
    await classroom.start();
    recorder = recordLiveStats(classroom, stop);
    const settings = await teacherStart(classroom, scenario.quiz_file, scenario.start_settings, MASTER_KEY);

    const agents = withAgents ? scenario.agents : {};
    for (const [name, agent] of Object.entries(agents)) await classroom.addStudent(name, agent.device);
    api = await startApi(classroom);

    const meta = {
      scenario: scenario.name,
      description: scenario.description,
      settings,
      limits: effectiveLimits(scenario, settings),
      grace: graceSeconds(),
      question_text: Object.fromEntries(scenario.quiz.questions.map((q, i) => [i + 1, q.question])),
      scripted: scenario.scripted,
      agents,
      expectations: scenario.expectations,
    };
    writeFileSync(path.join(runDir, 'meta.json'), JSON.stringify(meta, null, 2), 'utf-8');
    writeFileSync(path.join(runDir, 'READY'), 'ready\n');
    console.log(`READY ${runDir}`);

    const started = clock();
    const deadline = started + scenario.max_seconds;
    for (const name of Object.keys(scenario.scripted)) await classroom.addStudent(name);
    const questionIds = Object.fromEntries(scenario.quiz.questions.map((q, i) => [q.question as string, i + 1]));
    const students: Promise<void>[] = Object.entries(scenario.scripted).map(([name, plan]) =>
      scriptedStudent(classroom, name, plan, scenario.answers, questionIds, deadline),
    );
    if (scenario.approvals) {
      students.push(
        teacherApproves(
          classroom,
          scenario.approvals,
          Object.keys(agents),
          scenario.agent_approval_delay ?? 15,
          started,
          deadline,
        ),
      );
    }
    await Promise.all(students);
    console.log('Scripted students finished.');

    while (Object.keys(agents).length && !existsSync(path.join(runDir, 'agents_done')) && clock() < deadline) {
      await sleep(2);
    }
    while (clock() < started + scenario.finish_after) await sleep(1);
    await sleep(graceSeconds() + 7); // server time checks and the CSV flush
  } finally {
    stop.done = true;
    if (recorder) await recorder;
    if (api) await new Promise<void>((resolve) => api!.close(() => resolve()));
    await classroom.stop();
    await stopServer(server);
  }
  const found = await analyze(runDir);
  console.log(`DONE ${runDir} — ${found} problem(s) in the data checks. See report.md and timelines/.`);
  return runDir;
}

function fail(message: string): never {
  console.error(`error: ${message}`);
  process.exit(2);
}

async function main(): Promise<void> {
  const args = process.argv.slice(2);
  let name: string | undefined;
  let withAgents = true;
  let classSize: number | undefined;
  for (let i = 0; i < args.length; i++) {
    if (args[i] === '--no-agents') withAgents = false;
    else if (args[i] === '--class-size') classSize = Number(args[++i]);
    else if (args[i].startsWith('--')) fail(`unknown option ${args[i]}`);
    else name = args[i];
  }
  if (!name || !(name in SCENARIOS)) fail(`usage: run.ts <${Object.keys(SCENARIOS).sort().join('|')}> [--no-agents] [--class-size N]`);
  let scenario = SCENARIOS[name];
  if (classSize !== undefined) {
    if (name !== 'big-class' || !Number.isInteger(classSize) || classSize < 20) {
      fail('--class-size works only with big-class, and needs 20 or more');
    }
    scenario = bigClassScenario(classSize);
  }
  await run(scenario, withAgents);
  process.exit(0);
}

await main();
