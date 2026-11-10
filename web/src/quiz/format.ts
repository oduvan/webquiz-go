import type { ClientQuestion, Translations } from '../shared/boot';
import { formatPoints } from '../shared/points';
import { plain } from '../shared/text';

// Quiz paths like "/imgs/a.png" are made relative ("imgs/a.png"), so they also
// work when the page is served under a path prefix (the tunnel's /<socket>/).
export function makeRelativePath(path: unknown): string {
  const text = plain(path);
  return text.charAt(0) === '/' ? text.substring(1) : text;
}

// An option or answer that starts with "/" is an image path.
export function isImagePath(value: unknown): value is string {
  return typeof value === 'string' && value.charAt(0) === '/';
}

// The last part of a path: "/attach/data.xlsx" -> "data.xlsx".
export function fileName(path: unknown): string {
  const parts = plain(path).split('/');
  return parts[parts.length - 1];
}

// Countdown text: whole seconds rounded up, "M:SS".
export function formatTimer(seconds: number): string {
  const whole = Math.max(0, Math.ceil(seconds));
  const rest = String(whole % 60);
  return Math.floor(whole / 60) + ':' + (rest.length < 2 ? '0' + rest : rest);
}

// Fills "{name}" placeholders of a translated text (each one once, like the old page).
export function fillText(text: string, values: Record<string, string | number>): string {
  let result = text;
  Object.keys(values).forEach((name) => {
    result = result.replace('{' + name + '}', String(values[name]));
  });
  return result;
}

// The points badge of a question: "🏆 2 бали" / "🏆 0.5 балів" (Ukrainian has two plural forms).
export function pointsBadgeText(points: number, T: Translations): string {
  const word = Number.isInteger(points) && points >= 2 && points <= 4 ? T.points_2_4 : T.points_other;
  return '🏆 ' + formatPoints(points) + ' ' + word;
}

// Progress bar width in percent for the question at index (0-based).
export function progressPercent(index: number, total: number): number {
  return ((index + 1) / total) * 100;
}

// One part of a shown answer: an image or a text.
export interface AnswerPart {
  image?: string; // relative image path
  text?: string;
}

// How an answer is shown in the results: several answers are separated by "|"
// (multiple choice); an answer that starts with "/" is an image option.
export function answerParts(answer: unknown): { multiple: boolean; parts: AnswerPart[] } {
  const text = plain(answer);
  if (!text) {
    return { multiple: false, parts: [] };
  }
  const toPart = (value: string): AnswerPart =>
    value.charAt(0) === '/' ? { image: makeRelativePath(value) } : { text: value };
  if (text.indexOf('|') !== -1) {
    return { multiple: true, parts: text.split('|').map((value) => toPart(value.trim())) };
  }
  return { multiple: false, parts: [toPart(text)] };
}

// What the results table shows in the question column.
export interface QuestionDisplay {
  text: string; // "" when the question has no text
  image: string | null; // relative image path
  file: { href: string; name: string } | null;
  fallback: string | null; // shown when there is nothing else
}

export function questionDisplay(question: unknown, image: unknown, file: unknown, T: Translations): QuestionDisplay {
  const display: QuestionDisplay = {
    text: question ? plain(question) : '',
    image: isImagePath(image) ? makeRelativePath(image) : null,
    file: isImagePath(file) ? { href: makeRelativePath(file), name: fileName(file) } : null,
    fallback: null,
  };
  if (!display.text && !display.image && !display.file) {
    display.fallback = plain(image || file || T.no_question_content);
  }
  return display;
}

// Puts the questions in the student's own order (randomized quizzes). When the
// order does not match the questions, the original order stays.
export function applyQuestionOrder(questions: ClientQuestion[], order: unknown): ClientQuestion[] {
  if (!Array.isArray(order) || order.length === 0) {
    return questions;
  }
  const reordered: ClientQuestion[] = [];
  order.forEach((questionId) => {
    for (let i = 0; i < questions.length; i++) {
      if (questions[i].id === questionId) {
        reordered.push(questions[i]);
        break;
      }
    }
  });
  if (reordered.length === questions.length) {
    return reordered;
  }
  console.warn('Question order mismatch - keeping original order');
  return questions;
}

// The image paths of all questions and image options, relative, without repeats.
export function quizImageUrls(questions: ClientQuestion[]): string[] {
  const urls: string[] = [];
  const add = (url: string) => {
    if (urls.indexOf(url) === -1) urls.push(url);
  };
  questions.forEach((question) => {
    if (question.image) {
      add(makeRelativePath(question.image));
    }
    if (question.type !== 'text' && Array.isArray(question.options)) {
      question.options.forEach((option) => {
        if (isImagePath(option)) add(makeRelativePath(option));
      });
    }
  });
  return urls;
}

// A random token sent with a registration, so a retried request returns the same user.
// crypto.getRandomValues works on plain http (randomUUID needs a secure context).
export function generateRequestToken(): string {
  const bytes = new Uint8Array(16);
  crypto.getRandomValues(bytes);
  let token = '';
  for (let i = 0; i < bytes.length; i++) {
    const hex = bytes[i].toString(16);
    token += hex.length < 2 ? '0' + hex : hex;
  }
  return token;
}
