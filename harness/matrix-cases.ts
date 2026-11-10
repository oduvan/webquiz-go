// The cases of the matrix: every pair of values of any two factors appears in
// at least one case (all-pairs), plus two fixed cases, "defaults" and
// "everything-on".
//
// The generator is greedy and has no randomness, so the same factors always
// give the same cases.

import type { QuizSettings, TimeLimitMode } from './matrix-quiz';
import type { Language } from './matrix-texts';

export type Value = string | boolean;

export interface Factor {
  name: string;
  values: readonly Value[];
}

export type Row = Record<string, Value>;

/** Key of one pair of factor values, for the sets below. */
function pairKey(i: number, a: number, j: number, b: number): string {
  return `${i}:${a}|${j}:${b}`;
}

/** Every pair (factor i = value a, factor j = value b) with i < j. */
function allPairKeys(factors: Factor[]): string[] {
  const keys: string[] = [];
  for (let i = 0; i < factors.length; i++) {
    for (let j = i + 1; j < factors.length; j++) {
      for (let a = 0; a < factors[i].values.length; a++) {
        for (let b = 0; b < factors[j].values.length; b++) keys.push(pairKey(i, a, j, b));
      }
    }
  }
  return keys;
}

/** The pairs one row covers (the row as value indexes). */
function rowPairKeys(row: number[]): string[] {
  const keys: string[] = [];
  for (let i = 0; i < row.length; i++) {
    for (let j = i + 1; j < row.length; j++) keys.push(pairKey(i, row[i], j, row[j]));
  }
  return keys;
}

/**
 * Rows that together cover every pair of values of any two factors.
 *
 * Greedy: each new row starts from one pair that is not covered yet (every such
 * pair is tried) and fills the other factors with the value that covers the most
 * new pairs; a tie goes to the value used least so far, then to the first value.
 * The candidate row that covers the most new pairs is kept.
 */
export function allPairs(factors: Factor[]): Row[] {
  const uncovered = new Set(allPairKeys(factors));
  const uses = factors.map((factor) => factor.values.map(() => 0));
  const rows: number[][] = [];
  while (uncovered.size > 0) {
    let best: number[] | null = null;
    let bestGain = -1;
    for (const seed of uncovered) {
      const [left, right] = seed.split('|');
      const [i, a] = left.split(':').map(Number);
      const [j, b] = right.split(':').map(Number);
      const row: number[] = factors.map(() => -1);
      row[i] = a;
      row[j] = b;
      for (let k = 0; k < factors.length; k++) {
        if (row[k] !== -1) continue;
        let choice = 0;
        let choiceGain = -1;
        for (let v = 0; v < factors[k].values.length; v++) {
          let gain = 0;
          for (let m = 0; m < factors.length; m++) {
            if (m === k || row[m] === -1) continue;
            const key = m < k ? pairKey(m, row[m], k, v) : pairKey(k, v, m, row[m]);
            if (uncovered.has(key)) gain++;
          }
          if (gain > choiceGain || (gain === choiceGain && uses[k][v] < uses[k][choice])) {
            choice = v;
            choiceGain = gain;
          }
        }
        row[k] = choice;
      }
      const gain = rowPairKeys(row).filter((key) => uncovered.has(key)).length;
      if (gain > bestGain) {
        best = row;
        bestGain = gain;
      }
    }
    for (const key of rowPairKeys(best!)) uncovered.delete(key);
    best!.forEach((v, k) => uses[k][v]++);
    rows.push(best!);
  }
  return rows.map((row) => Object.fromEntries(row.map((v, k) => [factors[k].name, factors[k].values[v]])));
}

/** The pairs no row covers, as text ("a=x & b=y"); empty when the rows cover them all. */
export function missingPairs(factors: Factor[], rows: Row[]): string[] {
  const missing: string[] = [];
  for (let i = 0; i < factors.length; i++) {
    for (let j = i + 1; j < factors.length; j++) {
      for (const a of factors[i].values) {
        for (const b of factors[j].values) {
          const found = rows.some((row) => row[factors[i].name] === a && row[factors[j].name] === b);
          if (!found) missing.push(`${factors[i].name}=${a} & ${factors[j].name}=${b}`);
        }
      }
    }
  }
  return missing;
}

// ---- The matrix ----

export const FACTORS: Factor[] = [
  { name: 'time_limit', values: ['none', 'per_question', 'whole_quiz'] },
  { name: 'show_right_answer', values: [true, false] },
  { name: 'show_explanation', values: [true, false] },
  { name: 'show_final_list', values: [true, false] },
  { name: 'show_answers_on_completion', values: [true, false] },
  { name: 'randomize_questions', values: [true, false] },
  // Where the quiz settings come from: the quiz file, or the start settings of switch-quiz
  { name: 'settings_from', values: ['file', 'start'] },
  { name: 'approve', values: [true, false] },
  { name: 'language', values: ['uk', 'en'] },
  { name: 'fields', values: [true, false] },
];

export type Device = 'desktop' | 'phone';
export type Theme = 'light' | 'dark';

export interface Screen {
  device: Device;
  theme: Theme;
}

export const DESKTOP_LIGHT: Screen = { device: 'desktop', theme: 'light' };
export const PHONE_DARK: Screen = { device: 'phone', theme: 'dark' };
export const ALL_SCREENS: Screen[] = [
  DESKTOP_LIGHT,
  PHONE_DARK,
  { device: 'desktop', theme: 'dark' },
  { device: 'phone', theme: 'light' },
];

export interface MatrixCase {
  name: string;
  settings: QuizSettings;
  /** "file": the settings are in the quiz file; "start": in the start settings (the file has the
   * opposite values); "none": the quiz file has no settings, so the 1.19 defaults apply. */
  settingsFrom: 'file' | 'start' | 'none';
  approve: boolean;
  language: Language;
  /** Registration fields "Клас" and "Школа", the disabled field "Телефон" and a custom username label. */
  fields: boolean;
  /** true: the config file has no language and registration sections (the 1.19 defaults). */
  defaultConfig: boolean;
  screens: Screen[];
}

export const FIELDS = ['Клас', 'Школа'];
export const DISABLED_FIELDS = ['Телефон'];
export const USERNAME_LABEL = "Прізвище та ім'я";

/** The 1.19 default of every setting (server.py lines 1165-1180, config.py RegistrationConfig). */
export const DEFAULT_SETTINGS: QuizSettings = {
  show_right_answer: true,
  show_explanation: false,
  show_final_list: true,
  show_answers_on_completion: false,
  randomize_questions: false,
  time_limit: 'none',
};

function caseFromRow(name: string, row: Row): MatrixCase {
  return {
    name,
    settings: {
      show_right_answer: row.show_right_answer as boolean,
      show_explanation: row.show_explanation as boolean,
      show_final_list: row.show_final_list as boolean,
      show_answers_on_completion: row.show_answers_on_completion as boolean,
      randomize_questions: row.randomize_questions as boolean,
      time_limit: row.time_limit as TimeLimitMode,
    },
    settingsFrom: row.settings_from as 'file' | 'start',
    approve: row.approve as boolean,
    language: row.language as Language,
    fields: row.fields as boolean,
    defaultConfig: false,
    screens: [DESKTOP_LIGHT, PHONE_DARK],
  };
}

/** All cases: the two fixed ones first, then the all-pairs cases (pair-01, pair-02, ...). */
export function allCases(): MatrixCase[] {
  const defaults: MatrixCase = {
    name: 'defaults',
    settings: { ...DEFAULT_SETTINGS },
    settingsFrom: 'none',
    approve: false,
    language: 'uk',
    fields: false,
    defaultConfig: true,
    screens: ALL_SCREENS,
  };
  const everythingOn: MatrixCase = {
    name: 'everything-on',
    settings: {
      show_right_answer: true,
      show_explanation: true,
      show_final_list: true,
      show_answers_on_completion: true,
      randomize_questions: true,
      time_limit: 'per_question',
    },
    settingsFrom: 'file',
    approve: true,
    language: 'en',
    fields: true,
    defaultConfig: false,
    screens: ALL_SCREENS,
  };
  const pairs = allPairs(FACTORS).map((row, index) => caseFromRow(`pair-${String(index + 1).padStart(2, '0')}`, row));
  return [defaults, everythingOn, ...pairs];
}

/** The config sections a case adds to the server's config file. */
export function caseConfig(matrixCase: MatrixCase): Record<string, unknown> {
  if (matrixCase.defaultConfig) return {};
  const registration: Record<string, unknown> = { approve: matrixCase.approve };
  if (matrixCase.fields) {
    registration.fields = FIELDS;
    registration.disabled_fields = DISABLED_FIELDS;
    registration.username_label = USERNAME_LABEL;
  }
  return { language: matrixCase.language, registration };
}

/** One line that names the settings of a case, for captions and the report. */
export function describeCase(matrixCase: MatrixCase): string {
  const s = matrixCase.settings;
  const on = (flag: boolean, name: string) => (flag ? name : `no ${name}`);
  return [
    on(s.show_right_answer, 'show_right_answer'),
    on(s.show_explanation, 'show_explanation'),
    on(s.show_final_list, 'show_final_list'),
    on(s.show_answers_on_completion, 'show_answers_on_completion'),
    on(s.randomize_questions, 'randomize_questions'),
    `time limit: ${s.time_limit}`,
    `settings from: ${matrixCase.settingsFrom}`,
    on(matrixCase.approve, 'approve'),
    `language: ${matrixCase.language}`,
    matrixCase.fields ? 'registration fields' : 'no registration fields',
  ].join(', ');
}
