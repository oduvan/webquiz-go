import { spawn, type ChildProcess } from 'node:child_process';
import { existsSync, mkdirSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs';
import net from 'node:net';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { stringify } from 'yaml';

const here = path.dirname(fileURLToPath(import.meta.url));
export const root = path.resolve(here, '..', '..');
export const binaryPath = path.join(root, 'e2e', '.bin', process.platform === 'win32' ? 'webquiz.exe' : 'webquiz');
export const hasServerSource = existsSync(path.join(root, 'cmd', 'webquiz', 'main.go'));

type Dict = Record<string, unknown>;

export interface StartOptions {
  config?: Dict;
  quizzes?: Record<string, Dict>;
  env?: Record<string, string>;
}

export interface RunningServer {
  url: string;
  port: number;
  dir: string;
  stop(): Promise<void>;
}

const defaultQuizzes: Record<string, Dict> = {
  'test_quiz.yaml': {
    title: 'Test Quiz',
    description: 'A test quiz for admin API testing',
    questions: [{ question: 'What is 2 + 2?', options: ['3', '4', '5', '6'], correct_answer: 1 }],
  },
};

function isDict(value: unknown): value is Dict {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

export function deepMerge(base: Dict, override: Dict): Dict {
  for (const [key, value] of Object.entries(override)) {
    const current = base[key];
    if (isDict(current) && isDict(value)) {
      deepMerge(current, value);
    } else {
      base[key] = value;
    }
  }
  return base;
}

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

// Starts a real server in a new temp folder, like the Python conftest did.
// Paths in the config stay inside the folder. The binary takes its base folder
// from WEBQUIZ_BINARY_DIR, and WEBQUIZ_IS_BINARY=0 keeps it from opening a browser.
export async function startServer(options: StartOptions = {}): Promise<RunningServer> {
  if (!existsSync(binaryPath)) {
    throw new Error(`Server binary not found: ${binaryPath} (run global setup after cmd/webquiz exists)`);
  }
  const dir = mkdtempSync(path.join(os.tmpdir(), 'webquiz-e2e-'));
  const port = await freePort();

  const config = deepMerge(
    {
      server: { port },
      paths: {
        quizzes_dir: 'quizzes',
        logs_dir: 'logs',
        csv_dir: 'data',
        static_dir: 'static',
      },
      admin: { master_key: 'test123', trusted_ips: [] },
    },
    JSON.parse(JSON.stringify(options.config ?? {})),
  );
  (config.server as Dict).port = port;

  const paths = config.paths as Dict;
  const quizzesDir = path.resolve(dir, String(paths.quizzes_dir));
  mkdirSync(quizzesDir, { recursive: true });
  for (const [name, quiz] of Object.entries(options.quizzes ?? defaultQuizzes)) {
    writeFileSync(path.join(quizzesDir, name), stringify(quiz));
  }
  const configPath = path.join(dir, 'webquiz.yaml');
  writeFileSync(configPath, stringify(config));

  let output = '';
  const child: ChildProcess = spawn(binaryPath, ['--config', configPath, '--port', String(port)], {
    cwd: dir,
    env: {
      ...process.env,
      WEBQUIZ_BINARY_DIR: dir,
      WEBQUIZ_IS_BINARY: '0',
      ...options.env,
    },
    stdio: ['ignore', 'pipe', 'pipe'],
  });
  child.stdout?.on('data', (chunk) => (output += chunk));
  child.stderr?.on('data', (chunk) => (output += chunk));
  let exited = false;
  const exitPromise = new Promise<void>((resolve) =>
    child.once('exit', () => {
      exited = true;
      resolve();
    }),
  );

  const stop = async () => {
    if (!exited) {
      child.kill('SIGKILL');
      await exitPromise;
    }
    rmSync(dir, { recursive: true, force: true });
  };

  const deadline = Date.now() + 15_000;
  while (Date.now() < deadline) {
    if (exited) {
      await stop();
      throw new Error(`Server exited before it answered on port ${port}. Output:\n${output}`);
    }
    if (await canConnect(port)) {
      return { url: `http://localhost:${port}`, port, dir, stop };
    }
    await new Promise((resolve) => setTimeout(resolve, 100));
  }
  await stop();
  throw new Error(`Server did not answer on port ${port} within 15 s. Output:\n${output}`);
}
