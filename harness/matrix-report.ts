// The record of one matrix run (a case on one device and theme, or a teacher tour),
// and the two output files: report.md and manifest.json.

import { writeFileSync } from 'node:fs';
import path from 'node:path';
import type { CheckResult } from './matrix-rules';

export interface ManifestEntry {
  case: string;
  device: string;
  theme: string;
  language: string;
  screen: string;
  caption: string;
  file: string; // relative to the run folder
  checks?: CheckResult[];
}

export interface PageProblem {
  page: string; // the screen or page it happened on
  kind: 'console' | 'pageerror' | 'requestfailed' | 'external';
  text: string;
  /** Set when 1.19 shows the same message: why it is expected. */
  expected?: string;
}

/** Everything one run produced. */
export class RunRecord {
  readonly checks: { screen: string; result: CheckResult }[] = [];
  readonly shots: ManifestEntry[] = [];
  readonly problems: PageProblem[] = [];
  readonly notes: string[] = [];
  /** The screen name the next page problem belongs to. */
  where = 'start';
  error: string | null = null;
  seconds = 0;

  constructor(
    readonly caseName: string,
    readonly device: string,
    readonly theme: string,
    readonly language: string,
    readonly summary: string,
  ) {}

  get label(): string {
    return `${this.caseName} ${this.device}-${this.theme}${this.caseName === 'teacher' ? '-' + this.language : ''}`;
  }

  add(screen: string, results: CheckResult[]): CheckResult[] {
    for (const result of results) this.checks.push({ screen, result });
    return results;
  }

  failed(): { screen: string; result: CheckResult }[] {
    return this.checks.filter((item) => !item.result.pass);
  }
}

/** Text safe for one cell of a Markdown table. */
export function cell(text: string, max = 400): string {
  const flat = text.replace(/\r?\n/g, ' ↵ ').replace(/\|/g, '\\|');
  return flat.length > max ? flat.slice(0, max) + '…' : flat;
}

export function writeManifest(runDir: string, records: RunRecord[]): string {
  const entries = records.flatMap((record) => record.shots);
  const file = path.join(runDir, 'manifest.json');
  writeFileSync(file, JSON.stringify(entries, null, 2) + '\n', 'utf-8');
  return file;
}

export interface ReportHeader {
  title: string;
  binary: string;
  version: string;
  started: string;
  seconds: number;
  caseLines: { name: string; summary: string }[];
  notes: string[];
}

export function writeReport(runDir: string, header: ReportHeader, records: RunRecord[]): string {
  const lines: string[] = [];
  const allChecks = records.flatMap((record) => record.checks);
  const failed = records.flatMap((record) => record.failed().map((item) => ({ record, ...item })));
  const shots = records.reduce((sum, record) => sum + record.shots.length, 0);
  const problems = records.flatMap((record) => record.problems.map((problem) => ({ record, problem })));
  const crashed = records.filter((record) => record.error);

  lines.push(`# ${header.title}`, '');
  lines.push(`- Binary: \`${header.binary}\` (${header.version})`);
  lines.push(`- Started: ${header.started}, took ${Math.round(header.seconds)} s`);
  lines.push(`- Cases: ${header.caseLines.length}, runs: ${records.length}, screenshots: ${shots}`);
  lines.push(`- Checks: ${allChecks.length}, passed: ${allChecks.length - failed.length}, **failed: ${failed.length}**`);
  const unexpected = problems.filter(({ problem }) => !problem.expected).length;
  lines.push(`- Console or page errors: ${problems.length} (${unexpected} not expected from 1.19); runs that stopped with an error: ${crashed.length}`);
  for (const note of header.notes) lines.push(`- ${note}`);
  lines.push('');

  lines.push('## Cases', '', '| Case | Settings |', '|---|---|');
  for (const item of header.caseLines) lines.push(`| ${item.name} | ${cell(item.summary)} |`);
  lines.push('');

  lines.push('## Failed checks', '');
  if (failed.length === 0) lines.push('None.');
  else {
    lines.push('| Run | Screen | Check | Expected | Actual | Python 1.19 |', '|---|---|---|---|---|---|');
    for (const item of failed) {
      lines.push(
        `| ${item.record.label} | ${cell(item.screen)} | ${cell(item.result.name)} | ${cell(item.result.expected)} | ${cell(item.result.actual)} | ${cell(item.result.rule)} |`,
      );
    }
  }
  lines.push('');

  if (crashed.length) {
    lines.push('## Runs that stopped with an error', '');
    for (const record of crashed) lines.push(`- ${record.label}: ${record.error}`);
    lines.push('');
  }

  lines.push('## Console and page errors', '');
  if (problems.length === 0) lines.push('None.');
  else {
    lines.push('| Run | Page or screen | Kind | Text | Expected? |', '|---|---|---|---|---|');
    for (const { record, problem } of problems) {
      lines.push(`| ${record.label} | ${cell(problem.page)} | ${problem.kind} | ${cell(problem.text)} | ${cell(problem.expected ?? 'no')} |`);
    }
  }
  lines.push('');

  lines.push('## Every check, by run', '');
  for (const record of records) {
    const bad = record.failed().length;
    lines.push(`### ${record.label}`, '');
    lines.push(`${record.summary}`, '');
    lines.push(`Checks: ${record.checks.length}, failed: ${bad}. Screenshots: ${record.shots.length}. Took ${Math.round(record.seconds)} s.`, '');
    for (const note of record.notes) lines.push(`- ${note}`);
    if (record.notes.length) lines.push('');
    if (record.error) lines.push(`**The run stopped with an error:** ${record.error}`, '');
    lines.push('| Screen | Check | Result | Expected | Actual | Python 1.19 |', '|---|---|---|---|---|---|');
    for (const { screen, result } of record.checks) {
      lines.push(
        `| ${cell(screen)} | ${cell(result.name)} | ${result.pass ? 'PASS' : '**FAIL**'} | ${cell(result.expected)} | ${cell(result.actual)} | ${cell(result.rule)} |`,
      );
    }
    lines.push('');
  }
  const file = path.join(runDir, 'report.md');
  writeFileSync(file, lines.join('\n') + '\n', 'utf-8');
  return file;
}
