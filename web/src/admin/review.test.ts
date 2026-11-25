import { describe, expect, it } from 'vitest';
import { manualQuestionCount, reviewPageUrls, reviewStatusOf } from './review';

describe('manual check in the admin panel', () => {
  it('counts the questions with manual_check: true', () => {
    const quiz = {
      questions: [
        { question: 'A', manual_check: true },
        { question: 'B', manual_check: false, options: ['a', 'b'], correct_answer: 0 },
        { question: 'C', checker: '' },
        { question: 'D', manual_check: true },
        null,
      ],
    };
    expect(manualQuestionCount(quiz)).toBe(2);
    expect(manualQuestionCount({})).toBe(0);
    expect(manualQuestionCount(null)).toBe(0);
  });

  it('gives a review address for every network address', () => {
    const urls = [
      { label: 'Network Access (192.168.1.5)', quiz_url: 'http://192.168.1.5:8080/' },
      { label: 'Network Access (10.0.0.2)', quiz_url: 'http://10.0.0.2/webquiz' },
    ];
    expect(reviewPageUrls(urls, 'http://localhost:8080')).toEqual(['http://192.168.1.5:8080/review/', 'http://10.0.0.2/webquiz/review/']);
    expect(reviewPageUrls([], 'http://localhost:8080')).toEqual(['http://localhost:8080/review/']);
  });

  it('reads the review status of the admin socket', () => {
    expect(reviewStatusOf({ has_manual: true, pending_count: 3, reviewers: 1 })).toEqual({ has_manual: true, pending_count: 3, reviewers: 1 });
    expect(reviewStatusOf(undefined)).toBeNull();
  });
});
