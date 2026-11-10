// Unit tests for the teacher tool's element list (harness/elements.ts).

import { describe, expect, it } from 'vitest';
import {
  formatElement,
  formatTeacherStep,
  matchOption,
  replaceOnce,
  unescapeNewlines,
  valueBlock,
  type ElementInfo,
  type TeacherStepRecord,
} from './elements';

describe('one element', () => {
  it('shows buttons, inputs, checkboxes and selects like the examples', () => {
    expect(formatElement({ n: 3, kind: 'button', label: 'Зберегти' })).toBe('[3] button "Зберегти"');
    expect(formatElement({ n: 7, kind: 'input', label: 'Назва тесту', value: 'Біологія' })).toBe('[7] input "Назва тесту" = "Біологія"');
    expect(formatElement({ n: 9, kind: 'checkbox', label: 'Показувати правильні відповіді', checked: true })).toBe(
      '[9] checkbox "Показувати правильні відповіді" = on',
    );
    expect(formatElement({ n: 10, kind: 'radio', label: '5', checked: false })).toBe('[10] radio "5" = off');
    expect(formatElement({ n: 12, kind: 'select', label: 'Мова', value: 'uk', options: ['uk', 'en'] })).toBe(
      '[12] select "Мова" = "uk" (options: "uk", "en")',
    );
  });

  it('shows a select where several options can be chosen', () => {
    const element: ElementInfo = { n: 2, kind: 'select', label: 'Quizzes', multiple: true, value: 'a.yaml\nb.yaml', options: ['a.yaml', 'b.yaml', 'c.yaml'] };
    expect(formatElement(element)).toBe('[2] select "Quizzes" = "a.yaml", "b.yaml" (several can be chosen; options: "a.yaml", "b.yaml", "c.yaml")');
    expect(formatElement({ ...element, value: '' })).toContain('= nothing chosen');
  });

  it('shows where a link goes, and the extra facts in brackets', () => {
    expect(formatElement({ n: 4, kind: 'link', label: 'Live Stats', href: '/live-stats/' })).toBe('[4] link "Live Stats" -> /live-stats/');
    expect(formatElement({ n: 5, kind: 'button', label: '📋', disabled: true, hint: 'duplicate-btn', hintKind: 'id' })).toBe(
      '[5] button "📋" (disabled; id: duplicate-btn)',
    );
    expect(formatElement({ n: 6, kind: 'button', label: '🌙', hint: 'Toggle theme' })).toBe('[6] button "🌙" (title: "Toggle theme")');
    expect(formatElement({ n: 8, kind: 'tab', label: 'CSV', active: true })).toBe('[8] tab "CSV" (selected)');
    expect(formatElement({ n: 9, kind: 'option', label: 'Київ', active: true })).toBe('[9] option "Київ" (chosen)');
    expect(formatElement({ n: 11, kind: 'button', label: '✅ Підтвердити', near: '👤 auto-01 Клас: 5-А' })).toBe(
      '[11] button "✅ Підтвердити" (at: 👤 auto-01 Клас: 5-А)',
    );
    expect(formatElement({ n: 12, kind: 'input', label: 'Секунди:', value: '30', inputType: 'number' })).toBe(
      '[12] input "Секунди:" = "30" (number field)',
    );
  });

  it('shows a value with line breaks on its own lines', () => {
    const text = formatElement({ n: 14, kind: 'textarea', label: 'Quiz YAML', value: 'title: A\nquestions:\n- question: B' });
    expect(text.split('\n')).toEqual([
      '[14] textarea "Quiz YAML" = (3 lines, shown below)',
      '    | title: A',
      '    | questions:',
      '    | - question: B',
    ]);
  });

  it('cuts a very long value', () => {
    const value = Array.from({ length: 100 }, (_, i) => `line ${i}`).join('\n');
    const block = valueBlock(value);
    expect(block.length).toBe(61);
    expect(block[block.length - 1]).toContain('100 lines in all');
  });
});

describe('one teacher step', () => {
  const record: TeacherStepRecord = {
    step: 4,
    t: 12.34,
    result: 'clicked [3] button "Увійти"',
    error: null,
    events: ['DIALOG: a box asked "Sure?" and was answered OK (yes)'],
    url: 'http://127.0.0.1:8199/admin/',
    tab: 1,
    tabs: 1,
    screen: 'Панель\nУвійти',
    elements: [{ n: 1, kind: 'button', label: 'Увійти' }],
  };

  it('has the step, the page, the result, the events, the screen and the elements', () => {
    expect(formatTeacherStep(record).split('\n')).toEqual([
      'STEP 4  (time +12.3 s)',
      'PAGE: http://127.0.0.1:8199/admin/',
      'RESULT: clicked [3] button "Увійти"',
      'DIALOG: a box asked "Sure?" and was answered OK (yes)',
      'SCREEN:',
      'Панель',
      'Увійти',
      'ELEMENTS (use these numbers in your next command):',
      '[1] button "Увійти"',
    ]);
  });

  it('says which tab is open when there are several, and when no tab is open', () => {
    expect(formatTeacherStep({ ...record, tab: 2, tabs: 2 })).toContain('PAGE: http://127.0.0.1:8199/admin/  (tab 2 of 2)');
    const closed = formatTeacherStep({ ...record, url: null, elements: [], error: 'no tab is open' });
    expect(closed).toContain('PAGE: no tab is open');
    expect(closed).toContain('ERROR: no tab is open');
    expect(closed).toContain('ELEMENTS: none');
  });
});

describe('choosing an option', () => {
  const options = [
    { value: 'none', label: 'Без обмеження часу' },
    { value: 'per_question', label: 'На кожне питання' },
    { value: 'whole_quiz', label: 'На весь тест' },
  ];

  it('finds the option by its text, its value, or the start of its text', () => {
    expect(matchOption(options, 'На весь тест')).toEqual(options[2]);
    expect(matchOption(options, 'per_question')).toEqual(options[1]);
    expect(matchOption(options, 'на кожне')).toEqual(options[1]);
    expect(matchOption([{ value: 'a.yaml', label: 'a.yaml - Біологія (поточний)' }], 'a.yaml')).toEqual({
      value: 'a.yaml',
      label: 'a.yaml - Біологія (поточний)',
    });
  });

  it('explains when the text fits several options or none', () => {
    expect(matchOption(options, 'На')).toContain('fits 2 options');
    expect(matchOption(options, 'Хвилина')).toContain('no option "Хвилина"');
    expect(matchOption(options, 'Хвилина')).toContain('"На весь тест"');
  });
});

describe('changing text', () => {
  it('replaces one place in a value', () => {
    expect(replaceOnce('question: A\nquestion: B', 'question:', 'question')).toEqual({ value: 'question A\nquestion: B' });
    expect(replaceOnce('abc', 'x', 'y')).toEqual({ error: 'the field does not contain "x"' });
    expect(replaceOnce('abc', '', 'y')).toEqual({ error: 'the text to replace is empty' });
  });

  it('reads \\n as a new line', () => {
    expect(unescapeNewlines('a\\nb')).toBe('a\nb');
  });
});
