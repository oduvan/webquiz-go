// Unit tests for how automatic students find answers (harness/answers.ts).

import { mkdtempSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { describe, expect, it } from 'vitest';
import {
  baseName,
  chooseAnswer,
  correctAnswer,
  fieldValue,
  findQuestion,
  loadQuizFiles,
  pickRunningQuiz,
  questionsLeft,
  thinkSeconds,
  timerSeconds,
  wrongAnswer,
  type QuizFile,
  type ShownQuestion,
} from './answers';

const geography: QuizFile = {
  file: 'geography.yaml',
  questions: [
    { question: 'Столиця Польщі?', options: ['Варшава', 'Краків'], correct_answer: 0 },
    { question: 'Які міста в Україні?', options: ['Київ', 'Рим', 'Львів'], correct_answer: [0, 2] },
    { question: 'Скільки областей?', checker: 'assert to_int(user_answer) == 24', correct_value: '24' },
    { image: '/imgs/map.png', options: ['/imgs/a.png', '/imgs/b.png'], correct_answer: 1 },
  ],
};
const copy: QuizFile = {
  file: 'geography_copy.yaml',
  questions: [{ question: 'Столиця Польщі?', options: ['Краків', 'Варшава'], correct_answer: 1 }],
};

function shown(text: string | null, options: string[], extra: Partial<ShownQuestion> = {}): ShownQuestion {
  return { text, image: null, options: options.map((o) => ({ text: o, image: null })), isText: options.length === 0, ...extra };
}

describe('finding the question on the screen in the quiz files', () => {
  it('matches the text, then the options, then the running quiz', () => {
    const found = findQuestion([geography, copy], shown('Столиця  Польщі?', ['Краків', 'Варшава']));
    expect(found?.file.file).toBe('geography_copy.yaml');
    expect(findQuestion([geography, copy], shown('Столиця Польщі?', ['Варшава', 'Краків']))?.file.file).toBe('geography.yaml');
    expect(findQuestion([geography, copy], shown('Скільки областей?', []))?.index).toBe(2);
    expect(findQuestion([geography, copy], shown('Чого немає?', ['a']))).toBeNull();
  });

  it('matches a question with only a picture by the picture', () => {
    const screen: ShownQuestion = {
      text: '',
      image: 'imgs/map.png',
      options: [
        { text: '', image: 'imgs/a.png' },
        { text: '', image: 'imgs/b.png' },
      ],
      isText: false,
    };
    expect(findQuestion([geography], screen)?.index).toBe(3);
  });

  it('prefers the quiz that runs now when several files fit', () => {
    const same: QuizFile = { ...geography, file: 'other.yaml' };
    expect(findQuestion([geography, same], shown('Столиця Польщі?', ['Варшава', 'Краків']), 'other.yaml')?.file.file).toBe('other.yaml');
  });

  it('knows the running quiz from the question texts of the page', () => {
    expect(pickRunningQuiz([geography, copy], ['Столиця Польщі?'])).toBe('geography_copy.yaml');
    expect(pickRunningQuiz([geography, copy], ['Інше питання'])).toBeNull();
  });

  it('reads the quiz files of a folder and skips broken ones', () => {
    const dir = mkdtempSync(path.join(tmpdir(), 'answers-'));
    writeFileSync(path.join(dir, 'good.yaml'), 'title: A\nquestions:\n  - question: Q?\n    options: ["1", "2"]\n    correct_answer: 1\n');
    writeFileSync(path.join(dir, 'broken.yaml'), 'questions: [');
    writeFileSync(path.join(dir, 'notes.txt'), 'not a quiz');
    const files = loadQuizFiles(dir);
    expect(files.map((f) => f.file)).toEqual(['good.yaml']);
    expect(files[0].questions[0].options).toEqual(['1', '2']);
  });
});

describe('right and wrong answers', () => {
  it('gives the right answer of each kind of question', () => {
    expect(correctAnswer(geography.questions[0])).toEqual({ kind: 'options', indices: [0] });
    expect(correctAnswer(geography.questions[1])).toEqual({ kind: 'options', indices: [0, 2] });
    expect(correctAnswer(geography.questions[2])).toEqual({ kind: 'text', value: '24' });
    expect(correctAnswer({ question: 'x', checker: 'pass' })).toBeNull();
  });

  it('gives a wrong answer of each kind of question', () => {
    const always0 = () => 0;
    expect(wrongAnswer(geography.questions[0], always0)).toEqual({ kind: 'options', indices: [1] });
    // A wrong option makes a several-answers question wrong, whatever min_correct says
    expect(wrongAnswer(geography.questions[1], always0)).toEqual({ kind: 'options', indices: [0, 1] });
    expect(wrongAnswer(geography.questions[2], always0)).toEqual({ kind: 'text', value: '25' });
    expect(wrongAnswer({ checker: 'x', correct_value: 'mice' }, always0)).toEqual({ kind: 'text', value: 'micex' });
  });

  it('answers right with the given rate', () => {
    const found = { file: geography, index: 0, question: geography.questions[0] };
    const screen = shown('Столиця Польщі?', ['Варшава', 'Краків']);
    expect(chooseAnswer(found, screen, () => 0.5, 0.7).meant).toBe('right');
    expect(chooseAnswer(found, screen, () => 0.9, 0.7).meant).toBe('wrong');
    expect(chooseAnswer(null, screen, () => 0.1).meant).toBe('guess');
    expect(chooseAnswer(null, shown('?', []), () => 0.1).answer).toEqual({ kind: 'text', value: '42' });
  });
});

describe('timing and forms', () => {
  it('reads timers and the progress text', () => {
    expect(timerSeconds('1:05')).toBe(65);
    expect(timerSeconds('0:09')).toBe(9);
    expect(timerSeconds(null)).toBeNull();
    expect(questionsLeft('Питання 2 з 5')).toBe(4);
    expect(questionsLeft('Question 5 of 5')).toBe(1);
  });

  it('thinks 2-8 s, less when a time limit is close', () => {
    expect(thinkSeconds(() => 0.5, null, null, null)).toBe(5);
    expect(thinkSeconds(() => 1, 6, null, null)).toBe(3);
    expect(thinkSeconds(() => 1, 2, null, null)).toBe(0.5);
    expect(thinkSeconds(() => 1, null, 20, 4)).toBe(3.5);
  });

  it('fills registration fields with values that fit their labels', () => {
    expect(fieldValue('Клас', 'auto-01', 1)).toMatch(/^\d+-[АБВ]$/);
    expect(fieldValue('Email', 'auto-01', 1)).toBe('auto-01@example.com');
    expect(fieldValue('Улюблений колір', 'auto-01', 1)).toBe('auto-01 Улюблений колір');
  });

  it('takes the file name from an address', () => {
    expect(baseName('imgs/a%20b.png?x=1')).toBe('a b.png');
    expect(baseName('/imgs/c.png')).toBe('c.png');
  });
});
