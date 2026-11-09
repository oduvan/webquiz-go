import { execFileSync } from 'node:child_process';
import { existsSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url));
const root = path.resolve(here, '..');

// Builds the web pages and the Go binary once before all tests.
// The Go build is skipped while cmd/webquiz does not exist yet.
export default function globalSetup() {
  const npm = process.platform === 'win32' ? 'npm.cmd' : 'npm';
  if (!existsSync(path.join(root, 'web', 'node_modules'))) {
    execFileSync(npm, ['--prefix', 'web', 'ci'], { cwd: root, stdio: 'inherit' });
  }
  execFileSync(npm, ['--prefix', 'web', 'run', 'build'], { cwd: root, stdio: 'inherit' });

  if (!existsSync(path.join(root, 'cmd', 'webquiz', 'main.go'))) {
    console.log('cmd/webquiz/main.go is missing: skipping the Go build');
    return;
  }
  const exe = process.platform === 'win32' ? 'webquiz.exe' : 'webquiz';
  execFileSync('go', ['build', '-o', path.join('e2e', '.bin', exe), './cmd/webquiz'], {
    cwd: root,
    stdio: 'inherit',
    env: { ...process.env, CGO_ENABLED: '0' },
  });
}
