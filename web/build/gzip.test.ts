// @vitest-environment node
import { mkdirSync, mkdtempSync, readFileSync, readdirSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { gunzipSync } from 'node:zlib';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { writeGzipCopies } from './gzip';

// Text that gzip makes much smaller, like a minified script.
const SCRIPT = 'function show(a){return document.getElementById(a)}\n'.repeat(100);

let dir: string;

beforeEach(() => {
  dir = mkdtempSync(join(tmpdir(), 'webquiz-gzip-'));
  mkdirSync(join(dir, 'assets'));
});

afterEach(() => {
  rmSync(dir, { recursive: true, force: true });
});

function write(name: string, content: string | Buffer) {
  writeFileSync(join(dir, name), content);
}

describe('writeGzipCopies', () => {
  it('writes a gzip copy of scripts, styles, SVG, JSON and HTML that unpacks to the same bytes', () => {
    const files: Record<string, string> = {
      'assets/app-abc.js': SCRIPT,
      'assets/app-abc.css': 'body{margin:0;padding:0;color:#222}\n'.repeat(60),
      'assets/logo.svg': '<svg xmlns="http://www.w3.org/2000/svg"><rect width="10" height="10"/></svg>\n'.repeat(30),
      'assets/data.json': JSON.stringify(Array(200).fill({ question: 'What is 2 + 2?' })),
      'index.html': '<!doctype html><html><head><title>Quiz</title></head><body></body></html>\n'.repeat(30),
    };
    for (const [name, content] of Object.entries(files)) {
      write(name, content);
    }

    const written = writeGzipCopies(dir);

    expect([...written].sort()).toEqual(Object.keys(files).map((name) => join(dir, name + '.gz')).sort());
    for (const [name, content] of Object.entries(files)) {
      const packed = readFileSync(join(dir, name + '.gz'));
      expect(packed.length, name).toBeLessThan(Buffer.byteLength(content));
      expect(gunzipSync(packed).equals(Buffer.from(content)), name).toBe(true);
      expect(readFileSync(join(dir, name), 'utf8'), `${name} itself stays as it was`).toBe(content);
    }
  });

  it('writes no copy when gzip does not make the file smaller', () => {
    write('assets/tiny.js', 'x()');
    write('assets/tiny.css', 'a{}');

    expect(writeGzipCopies(dir)).toEqual([]);
    expect(readdirSync(join(dir, 'assets')).sort()).toEqual(['tiny.css', 'tiny.js']);
  });

  it('leaves other kinds of files alone', () => {
    const others = ['picture.png', 'readme.txt', 'app-abc.js.map', 'font.woff2', 'app-abc.js.gz'];
    for (const name of others) {
      write('assets/' + name, SCRIPT);
    }

    expect(writeGzipCopies(dir)).toEqual([]);
    expect(readdirSync(join(dir, 'assets')).sort()).toEqual([...others].sort());
    for (const name of others) {
      expect(readFileSync(join(dir, 'assets', name), 'utf8'), name).toBe(SCRIPT);
    }
  });
});
