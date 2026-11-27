import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import type { Translations } from '../shared/boot';
import { Results } from './Results';
import type { FinalResults } from './types';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const T = new Proxy({ pending_review_notice: 'Not checked yet: {n}.', pending_review_row: '⏳ Waiting' } as Record<string, string>, {
  get: (texts, key) => (String(key) in texts ? texts[String(key)] : '[' + String(key) + ']'),
}) as Translations;

let container: HTMLDivElement;
let root: Root;

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
});

afterEach(() => {
  act(() => root.unmount());
  container.remove();
});

const final: FinalResults = {
  test_results: [
    { question: 'Why?', selected_answer: 'Because', is_correct: null, pending_review: true, points: 2, earned_points: 0 },
    { question: '2 + 2?', selected_answer: '4', is_correct: true, correct_answer: '4', points: 1, earned_points: 1 },
  ],
  correct_count: 1,
  total_count: 2,
  percentage: 50,
  earned_points: 1,
  total_points: 3,
  pending_count: 1,
};

describe('results with answers that wait for the teacher', () => {
  it('highlights the waiting row and counts it in the notice', () => {
    act(() =>
      root.render(<Results content={{ kind: 'server', final, sawPendingReview: true }} T={T} showFinalList={true} showRightAnswer={true} />),
    );
    expect(container.querySelector('#pending-review-notice')!.textContent).toBe('Not checked yet: 1.');
    const rows = container.querySelectorAll('.results-table tbody tr');
    expect(rows[0].className).toBe('pending-review-row');
    expect(rows[0].textContent).toContain('⏳ Waiting');
    expect(rows[0].textContent).toContain('?/2'); // no points yet
    expect(rows[0].textContent).not.toContain('✗');
    expect(rows[1].className).toBe('');
    expect(rows[1].textContent).toContain('✓');
  });

  it('says that everything is checked once the waiting answers are decided', () => {
    const done: FinalResults = { ...final, pending_count: 0, test_results: [{ ...final.test_results[0], pending_review: undefined, is_correct: true, earned_points: 2 }] };
    act(() =>
      root.render(<Results content={{ kind: 'server', final: done, sawPendingReview: true }} T={T} showFinalList={true} showRightAnswer={true} />),
    );
    expect(container.querySelector('#pending-review-notice')).toBeNull();
    expect(container.querySelector('#pending-review-done')!.textContent).toBe('[pending_review_done]');
    expect(container.querySelector('.pending-review-row')).toBeNull();
  });
});
