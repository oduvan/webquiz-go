import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

// indicatorsOf runs once each time a question is drawn, so its calls count
// the drawings.
vi.mock('./wizard', async (importOriginal) => {
  const original = await importOriginal<typeof import('./wizard')>();
  return { ...original, indicatorsOf: vi.fn(original.indicatorsOf) };
});

import { MessageProvider } from './messages';
import { QuizEditor } from './QuizEditor';
import { draftFromQuiz, indicatorsOf } from './wizard';

// React's act() needs this flag outside a test framework that sets it.
(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

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

// Types a value the way a browser does: the input's value, then an input event.
function type(input: HTMLInputElement, value: string) {
  const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!;
  setter.call(input, value);
  input.dispatchEvent(new Event('input', { bubbles: true }));
}

describe('QuizEditor', () => {
  it('redraws only the question that changes', () => {
    const question = (text: string) => ({ question: text, options: ['A', 'B'], correct_answer: 0 });
    const draft = draftFromQuiz({ questions: [question('One'), question('Two'), question('Three')] });
    act(() =>
      root.render(
        <MessageProvider>
          <QuizEditor
            start={{ mode: 'edit', editingName: 'q.yaml', filename: 'q', draft }}
            onClose={() => undefined}
            onListChanged={() => undefined}
          />
        </MessageProvider>,
      ),
    );
    const drawings = vi.mocked(indicatorsOf);
    expect(drawings).toHaveBeenCalledTimes(3);
    drawings.mockClear();

    const fields = container.querySelectorAll<HTMLInputElement>('.question-text');
    act(() => type(fields[1], 'Two and a half'));

    expect(drawings).toHaveBeenCalledTimes(1);
    expect(drawings.mock.calls[0][0].text).toBe('Two and a half');
    expect(Array.from(container.querySelectorAll('.question-preview')).map((preview) => preview.textContent)).toEqual([
      'One',
      'Two and a half',
      'Three',
    ]);
  });
});
