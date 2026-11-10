// How an automatic student finds the right answer: it matches the question on
// its screen with the quiz files on disk (the run's quizzes folder).
//
// Everything here is pure (no browser), so it is tested with Vitest.

import { existsSync, readdirSync, readFileSync } from 'node:fs';
import path from 'node:path';
import { parse } from 'yaml';

/** A question as written in a quiz file (only the fields used here). */
export interface FileQuestion {
  question?: unknown;
  image?: unknown;
  options?: unknown[];
  correct_answer?: unknown;
  correct_value?: unknown;
  checker?: unknown;
  min_correct?: unknown;
}

export interface QuizFile {
  file: string;
  title?: string;
  questions: FileQuestion[];
}

/** What the screen shows for the current question. */
export interface ShownQuestion {
  text: string | null;
  /** The question picture's address, for example "imgs/shape.png". */
  image: string | null;
  options: { text: string; image: string | null }[];
  isText: boolean;
}

export type Answer = { kind: 'options'; indices: number[] } | { kind: 'text'; value: string };

export interface Found {
  file: QuizFile;
  index: number;
  question: FileQuestion;
}

function clean(text: unknown): string {
  return String(text ?? '')
    .replace(/\s+/g, ' ')
    .trim();
}

/** The file name at the end of a path or address: "/imgs/a%20b.png" -> "a b.png". */
export function baseName(address: unknown): string {
  let name = String(address ?? '').split('?')[0].split('/').pop() ?? '';
  try {
    name = decodeURIComponent(name);
  } catch {
    // keep the name as it is
  }
  return name;
}

/** Read every quiz file of a folder. Files that cannot be read are left out. */
export function loadQuizFiles(dir: string): QuizFile[] {
  if (!existsSync(dir)) return [];
  const files: QuizFile[] = [];
  for (const name of readdirSync(dir).sort()) {
    if (!/\.ya?ml$/i.test(name)) continue;
    try {
      const data = parse(readFileSync(path.join(dir, name), 'utf-8')) as Record<string, unknown> | null;
      if (!data || !Array.isArray(data.questions)) continue;
      files.push({
        file: name,
        title: typeof data.title === 'string' ? data.title : undefined,
        questions: data.questions as FileQuestion[],
      });
    } catch {
      // a broken file (the teacher may be editing it): skip it
    }
  }
  return files;
}

export function isTextQuestion(question: FileQuestion): boolean {
  return question.checker !== undefined || (question.options === undefined && question.correct_value !== undefined);
}

/** Do the options on the screen look like this question's options? */
function sameOptions(question: FileQuestion, shown: ShownQuestion): boolean {
  if (isTextQuestion(question) !== shown.isText) return false;
  if (shown.isText) return true;
  const options = question.options ?? [];
  if (options.length !== shown.options.length) return false;
  return options.every((option, index) => {
    const screen = shown.options[index];
    const text = clean(option);
    if (text.startsWith('/')) return screen.image !== null && baseName(screen.image) === baseName(text);
    return clean(screen.text) === text;
  });
}

/**
 * Find the question on the screen in the quiz files: the same text (or the same
 * picture when the question has no text), then the same options. When several
 * files have it, the file `prefer` (the quiz that runs now, if known) wins.
 */
export function findQuestion(files: QuizFile[], shown: ShownQuestion, prefer?: string | null): Found | null {
  const text = clean(shown.text);
  const candidates: Found[] = [];
  for (const file of files) {
    file.questions.forEach((question, index) => {
      if (!question || typeof question !== 'object') return;
      const own = clean(question.question);
      if (text) {
        if (own === text) candidates.push({ file, index, question });
      } else if (shown.image && question.image && baseName(question.image) === baseName(shown.image) && !own) {
        candidates.push({ file, index, question });
      }
    });
  }
  if (candidates.length === 0) return null;
  const fitting = candidates.filter((found) => sameOptions(found.question, shown));
  const pool = fitting.length ? fitting : candidates;
  return pool.find((found) => found.file.file === prefer) ?? pool[0];
}

/** The quiz file whose questions are exactly the given texts, in any order (the quiz that runs now). */
export function pickRunningQuiz(files: QuizFile[], questionTexts: string[]): string | null {
  const wanted = questionTexts.map(clean).sort();
  const fits = files.filter((file) => {
    const own = file.questions.map((q) => clean(q?.question)).sort();
    return own.length === wanted.length && own.every((text, i) => text === wanted[i]);
  });
  return fits.length ? fits[0].file : null;
}

function correctIndices(question: FileQuestion): number[] {
  const value = question.correct_answer;
  if (typeof value === 'number') return [value];
  if (Array.isArray(value)) return value.filter((v): v is number => typeof v === 'number').sort((a, b) => a - b);
  return [];
}

/** The right answer of a question, or null when the file does not say. */
export function correctAnswer(question: FileQuestion): Answer | null {
  if (isTextQuestion(question)) {
    if (question.correct_value === undefined || question.correct_value === null) return null;
    return { kind: 'text', value: String(question.correct_value).trim() };
  }
  const indices = correctIndices(question);
  return indices.length ? { kind: 'options', indices } : null;
}

/**
 * A wrong answer: another option, an extra wrong option for several-answer
 * questions (any wrong option makes the answer wrong), or a different text.
 */
export function wrongAnswer(question: FileQuestion, random: () => number): Answer {
  if (isTextQuestion(question)) {
    const right = String(question.correct_value ?? '').trim();
    const number = Number(right);
    if (right !== '' && Number.isFinite(number)) return { kind: 'text', value: String(number + 1) };
    return { kind: 'text', value: right ? right + 'x' : 'abc' };
  }
  const count = (question.options ?? []).length;
  const right = correctIndices(question);
  const wrong = Array.from({ length: count }, (_, i) => i).filter((i) => !right.includes(i));
  if (wrong.length === 0) return { kind: 'options', indices: right.slice(0, 1) };
  const pick = wrong[Math.floor(random() * wrong.length)];
  if (Array.isArray(question.correct_answer)) {
    return { kind: 'options', indices: [...right.slice(0, 1), pick].sort((a, b) => a - b) };
  }
  return { kind: 'options', indices: [pick] };
}

/** An answer for a question the files do not have: the first option, or a guess. */
export function guessAnswer(shown: ShownQuestion): Answer {
  if (shown.isText) return { kind: 'text', value: '42' };
  return { kind: 'options', indices: [0] };
}

/** Right with probability `rightRate`, else wrong. */
export function chooseAnswer(found: Found | null, shown: ShownQuestion, random: () => number, rightRate = 0.7): {
  answer: Answer;
  meant: 'right' | 'wrong' | 'guess';
} {
  if (!found) return { answer: guessAnswer(shown), meant: 'guess' };
  const right = correctAnswer(found.question);
  if (!right) return { answer: guessAnswer(shown), meant: 'guess' };
  if (random() < rightRate) return { answer: right, meant: 'right' };
  return { answer: wrongAnswer(found.question, random), meant: 'wrong' };
}

/** A value for a registration field, from its label. */
export function fieldValue(label: string, name: string, number: number): string {
  const text = label.toLowerCase();
  if (/клас|class|grade|form/.test(text)) return `${5 + (number % 7)}-${'АБВ'[number % 3]}`;
  if (/школ|school/.test(text)) return `Школа ${number}`;
  if (/mail|пошт/.test(text)) return `${name}@example.com`;
  if (/телефон|phone/.test(text)) return `050000${String(number).padStart(4, '0')}`;
  if (/вік|age/.test(text)) return String(10 + (number % 8));
  return `${name} ${label}`.trim();
}

/** A small seeded random generator (mulberry32): the same name gives the same answers. */
export function seededRandom(seed: number): () => number {
  let a = seed >>> 0;
  return () => {
    a = (a + 0x6d2b79f5) >>> 0;
    let t = a;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

/** A number from a name, for seededRandom. */
export function seedOf(name: string): number {
  let hash = 2166136261;
  for (const char of name) hash = Math.imul(hash ^ char.charCodeAt(0), 16777619);
  return hash >>> 0;
}

/** Seconds from a timer text like "1:05" (null when it is not a timer). */
export function timerSeconds(text: string | null | undefined): number | null {
  const match = /(\d+):(\d{2})/.exec(text ?? '');
  return match ? Number(match[1]) * 60 + Number(match[2]) : null;
}

/** How many questions are left, from "Питання 2 з 5" / "Question 2 of 5" (the current one counts). */
export function questionsLeft(progress: string | null | undefined): number | null {
  const match = /(\d+)\D+(\d+)/.exec(progress ?? '');
  return match ? Number(match[2]) - Number(match[1]) + 1 : null;
}

/**
 * How long to think before answering: a random 2-8 s, but less when a time
 * limit is close (the answer must arrive at least 3 s before the question's
 * time runs out, and the whole-quiz time is shared by the questions left).
 */
export function thinkSeconds(
  random: () => number,
  questionLeft: number | null,
  quizLeft: number | null,
  questionsToGo: number | null,
): number {
  let seconds = 2 + random() * 6;
  if (questionLeft !== null) seconds = Math.min(seconds, Math.max(0.5, questionLeft - 3));
  if (quizLeft !== null) {
    const share = quizLeft / Math.max(1, questionsToGo ?? 1);
    seconds = Math.min(seconds, Math.max(0.5, share - 1.5));
  }
  return Math.round(seconds * 10) / 10;
}
