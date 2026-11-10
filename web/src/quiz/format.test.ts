import { describe, expect, it } from 'vitest';
import type { ClientQuestion, Translations } from '../shared/boot';
import {
  answerParts,
  applyQuestionOrder,
  fileName,
  fillText,
  formatTimer,
  generateRequestToken,
  isImagePath,
  makeRelativePath,
  pointsBadgeText,
  progressPercent,
  questionDisplay,
  quizImageUrls,
} from './format';

const T = { points_2_4: 'бали', points_other: 'балів', no_question_content: 'No content' } as Translations;

function choice(id: number, options: unknown[], extra: Partial<ClientQuestion> = {}): ClientQuestion {
  return { id, points: 1, time_limit: 0, options, is_multiple_choice: false, ...extra } as ClientQuestion;
}

describe('paths', () => {
  it('makes quiz paths relative, for the tunnel prefix', () => {
    expect(makeRelativePath('/imgs/a.png')).toBe('imgs/a.png');
    expect(makeRelativePath('imgs/a.png')).toBe('imgs/a.png');
    expect(makeRelativePath(undefined)).toBe('');
  });
  it('treats only text that starts with "/" as an image path', () => {
    expect(isImagePath('/imgs/a.png')).toBe(true);
    expect(isImagePath('A')).toBe(false);
    expect(isImagePath(5)).toBe(false);
  });
  it('takes the last part of a path', () => {
    expect(fileName('/attach/data.xlsx')).toBe('data.xlsx');
  });
});

describe('formatTimer', () => {
  it('rounds seconds up and writes M:SS', () => {
    expect(formatTimer(65)).toBe('1:05');
    expect(formatTimer(0.2)).toBe('0:01');
    expect(formatTimer(600)).toBe('10:00');
  });
  it('never goes below 0:00', () => {
    expect(formatTimer(0)).toBe('0:00');
    expect(formatTimer(-3)).toBe('0:00');
  });
});

describe('texts', () => {
  it('fills placeholders', () => {
    expect(fillText('Question {n} of {m}', { n: 2, m: 5 })).toBe('Question 2 of 5');
  });
  it('picks the Ukrainian plural for 2, 3 and 4 points', () => {
    expect(pointsBadgeText(2, T)).toBe('🏆 2 бали');
    expect(pointsBadgeText(4, T)).toBe('🏆 4 бали');
    expect(pointsBadgeText(5, T)).toBe('🏆 5 балів');
    expect(pointsBadgeText(0.5, T)).toBe('🏆 0.5 балів');
    expect(pointsBadgeText(2.5, T)).toBe('🏆 2.5 балів');
  });
  it('computes the progress bar width', () => {
    expect(progressPercent(0, 4)).toBe(25);
    expect(progressPercent(3, 4)).toBe(100);
  });
});

describe('answerParts', () => {
  it('splits several answers at "|"', () => {
    expect(answerParts('Red | Blue')).toEqual({ multiple: true, parts: [{ text: 'Red' }, { text: 'Blue' }] });
  });
  it('shows image answers as images', () => {
    expect(answerParts('/imgs/a.png')).toEqual({ multiple: false, parts: [{ image: 'imgs/a.png' }] });
    expect(answerParts('/imgs/a.png | B')).toEqual({ multiple: true, parts: [{ image: 'imgs/a.png' }, { text: 'B' }] });
  });
  it('shows nothing for an empty answer', () => {
    expect(answerParts('')).toEqual({ multiple: false, parts: [] });
    expect(answerParts(undefined)).toEqual({ multiple: false, parts: [] });
  });
  it('keeps tags as text', () => {
    expect(answerParts('<b>x</b>')).toEqual({ multiple: false, parts: [{ text: '<b>x</b>' }] });
  });
});

describe('questionDisplay', () => {
  it('shows text, image and file', () => {
    expect(questionDisplay('Q?', '/imgs/q.png', '/attach/f.txt', T)).toEqual({
      text: 'Q?',
      image: 'imgs/q.png',
      file: { href: 'attach/f.txt', name: 'f.txt' },
      fallback: null,
    });
  });
  it('falls back to the raw value or a text when nothing can be shown', () => {
    expect(questionDisplay('', 'imgs/q.png', null, T).fallback).toBe('imgs/q.png');
    expect(questionDisplay('', null, null, T).fallback).toBe('No content');
  });
});

describe('applyQuestionOrder', () => {
  const questions = [choice(1, ['A']), choice(2, ['B']), choice(3, ['C'])];
  it('puts the questions in the given order', () => {
    expect(applyQuestionOrder(questions, [3, 1, 2]).map((q) => q.id)).toEqual([3, 1, 2]);
  });
  it('keeps the order when the given one does not match', () => {
    expect(applyQuestionOrder(questions, [3, 1]).map((q) => q.id)).toEqual([1, 2, 3]);
    expect(applyQuestionOrder(questions, null).map((q) => q.id)).toEqual([1, 2, 3]);
  });
});

describe('quizImageUrls', () => {
  it('lists question images and image options once', () => {
    const questions = [
      choice(1, ['A'], { image: '/imgs/q.png' }),
      choice(2, ['/imgs/a.png', '/imgs/q.png', 'text']),
      { id: 3, points: 1, time_limit: 0, type: 'text', default_value: '/imgs/not-an-image' } as ClientQuestion,
    ];
    expect(quizImageUrls(questions)).toEqual(['imgs/q.png', 'imgs/a.png']);
  });
});

describe('generateRequestToken', () => {
  it('makes 32 hex characters, different each time', () => {
    const a = generateRequestToken();
    expect(a).toMatch(/^[0-9a-f]{32}$/);
    expect(generateRequestToken()).not.toBe(a);
  });
});
