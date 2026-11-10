import { describe, expect, it } from 'vitest';
import {
  draftFromQuiz,
  explanationImageMarkdown,
  explanationToggleText,
  fieldNameToLabel,
  formatFileSize,
  indicatorsOf,
  insertAt,
  newQuestion,
  newQuizDraft,
  numberValue,
  startSettingsFromQuiz,
  startSettingsPayload,
  validateWizardPoints,
  wizardData,
  type QuestionDraft,
} from './wizard';

function question(patch: Partial<QuestionDraft> = {}): QuestionDraft {
  return { ...newQuestion(), collapsed: true, focus: false, ...patch };
}

describe('draftFromQuiz', () => {
  it('reads the quiz settings like the old editor', () => {
    const draft = draftFromQuiz({
      title: 'Maths',
      show_right_answer: false,
      show_answers_on_completion: true,
      randomize_questions: true,
      show_final_list: false,
      show_explanation: true,
      questions: [],
    });
    expect(draft.title).toBe('Maths');
    expect(draft.showRightAnswer).toBe(false);
    expect(draft.showAnswersOnCompletion).toBe(true);
    expect(draft.randomizeQuestions).toBe(true);
    expect(draft.showFinalList).toBe(false);
    expect(draft.showExplanation).toBe(true);
    expect(draft.timeLimitMode).toBe('none');
    expect(draft.timeLimitSeconds).toBe('60');
  });

  it('clears "answers after everyone finishes" when correct answers are shown', () => {
    expect(draftFromQuiz({ show_answers_on_completion: true }).showAnswersOnCompletion).toBe(false);
    expect(draftFromQuiz({ show_right_answer: true, show_answers_on_completion: true }).showAnswersOnCompletion).toBe(false);
  });

  it('uses the defaults for a missing or odd quiz', () => {
    for (const parsed of [null, undefined, [], 'text', 5]) {
      const draft = draftFromQuiz(parsed);
      expect(draft.title).toBe('');
      expect(draft.showRightAnswer).toBe(true);
      expect(draft.showFinalList).toBe(true);
      expect(draft.questions).toEqual([]);
    }
    expect(draftFromQuiz({ questions: { a: 1 } }).questions).toEqual([]);
    expect(draftFromQuiz({ title: 0 }).title).toBe('');
    expect(draftFromQuiz({ title: 2024 }).title).toBe('2024');
  });

  it('reads the time limit mode and seconds', () => {
    expect(draftFromQuiz({ time_limit: { per_question: 30 } })).toMatchObject({
      timeLimitMode: 'per_question',
      timeLimitSeconds: '30',
    });
    expect(draftFromQuiz({ time_limit: { whole_quiz: 600 } })).toMatchObject({
      timeLimitMode: 'whole_quiz',
      timeLimitSeconds: '600',
    });
    // per_question wins when both are written; a null value still names the mode
    expect(draftFromQuiz({ time_limit: { whole_quiz: 5, per_question: null } })).toMatchObject({
      timeLimitMode: 'per_question',
      timeLimitSeconds: '',
    });
    expect(draftFromQuiz({ time_limit: 30 })).toMatchObject({ timeLimitMode: 'none', timeLimitSeconds: '60' });
  });

  it('reads choice questions', () => {
    const [single, multiple, noOptions] = draftFromQuiz({
      questions: [
        { question: 'Q1', options: ['A', 'B', 'C'], correct_answer: 2, points: 0.5 },
        { question: 'Q2', options: ['A', 'B', 'C'], correct_answer: [0, 2], min_correct: 1, stick_to_the_previous: true },
        { image: '/imgs/a.png' },
      ],
    }).questions;

    expect(single.type).toBe('single');
    expect(single.collapsed).toBe(true);
    expect(single.options.map((option) => [option.text, option.correct])).toEqual([
      ['A', false],
      ['B', false],
      ['C', true],
    ]);
    expect(single.points).toBe('0.5');

    expect(multiple.type).toBe('multiple');
    expect(multiple.choiceKind).toBe('multiple');
    expect(multiple.options.map((option) => option.correct)).toEqual([true, false, true]);
    expect(multiple.minCorrect).toBe('1');
    expect(multiple.stick).toBe(true);
    expect(multiple.points).toBe('1');

    // Without options: two empty ones, the first marked
    expect(noOptions.text).toBe('');
    expect(noOptions.image).toBe('/imgs/a.png');
    expect(noOptions.options.map((option) => [option.text, option.correct])).toEqual([
      ['', true],
      ['', false],
    ]);
  });

  it('knows a text question by its checker key, even an empty one', () => {
    const [withChecker, emptyChecker] = draftFromQuiz({
      questions: [
        { question: 'T', checker: 'assert x', default_value: 'start', correct_value: 'end', time_limit: 10 },
        { question: 'T2', checker: null },
      ],
    }).questions;
    expect(withChecker).toMatchObject({
      type: 'text',
      checker: 'assert x',
      defaultValue: 'start',
      correctValue: 'end',
      timeLimit: '10',
      options: [],
    });
    expect(emptyChecker.type).toBe('text');
    expect(emptyChecker.checker).toBe('');
  });

  it('keeps only values a number field can show', () => {
    const [q] = draftFromQuiz({ questions: [{ question: 'Q', options: ['1', '2'], points: 'many', min_correct: 'x' }] })
      .questions;
    expect(q.points).toBe('');
    expect(q.minCorrect).toBe('');
    expect(numberValue('1.')).toBe('');
    expect(numberValue('.5')).toBe('.5');
    expect(numberValue('-2e3')).toBe('-2e3');
    expect(numberValue(null)).toBe('');
  });

  it('shows option values as text', () => {
    const [q] = draftFromQuiz({ questions: [{ question: 'Q', options: [1, null, true], correct_answer: 0 }] }).questions;
    expect(q.options.map((option) => option.text)).toEqual(['1', '', 'true']);
  });

  it('opens a written explanation and folds an empty one', () => {
    const [written, empty, blank] = draftFromQuiz({
      questions: [
        { question: 'A', options: ['x', 'y'], explanation: 'Because' },
        { question: 'B', options: ['x', 'y'] },
        { question: 'C', options: ['x', 'y'], explanation: '   ' },
      ],
    }).questions;
    expect(written.explanationOpen).toBe(true);
    expect(empty.explanationOpen).toBe(false);
    expect(blank.explanationOpen).toBe(false);
    expect(blank.explanation).toBe('   ');
  });
});

describe('wizardData', () => {
  it('saves a new quiz like the old editor', () => {
    const draft = newQuizDraft();
    draft.title = '  My quiz  ';
    draft.questions[0] = question({
      text: ' What? ',
      options: [
        { key: 1, text: ' A ', correct: false },
        { key: 2, text: 'B', correct: true },
      ],
    });
    expect(wizardData(draft)).toEqual({
      questions: [{ options: ['A', 'B'], correct_answer: 1, question: 'What?' }],
      title: 'My quiz',
      show_right_answer: true,
      randomize_questions: false,
      show_answers_on_completion: false,
      show_final_list: true,
    });
  });

  it('leaves out questions without text and image, and choice questions with an empty option', () => {
    const draft = newQuizDraft();
    draft.questions = [
      question({ text: '' }),
      question({ text: 'Q', options: [{ key: 1, text: 'A', correct: true }, { key: 2, text: ' ', correct: false }] }),
      question({ image: '/imgs/a.png', options: [{ key: 1, text: 'A', correct: true }, { key: 2, text: 'B', correct: false }] }),
    ];
    expect(wizardData(draft).questions).toEqual([{ options: ['A', 'B'], correct_answer: 0, image: '/imgs/a.png' }]);
  });

  it('saves the first option when no radio button is ticked', () => {
    const draft = newQuizDraft();
    draft.questions = [question({ text: 'Q', options: [{ key: 1, text: 'A', correct: false }, { key: 2, text: 'B', correct: false }] })];
    expect((wizardData(draft).questions as Record<string, unknown>[])[0].correct_answer).toBe(0);
  });

  it('saves multiple correct answers with min_correct only when it fits', () => {
    const options = [
      { key: 1, text: 'A', correct: true },
      { key: 2, text: 'B', correct: false },
      { key: 3, text: 'C', correct: true },
    ];
    const draft = newQuizDraft();
    draft.questions = [
      question({ text: 'Q1', type: 'multiple', choiceKind: 'multiple', options, minCorrect: '1' }),
      question({ text: 'Q2', type: 'multiple', choiceKind: 'multiple', options, minCorrect: '3' }),
      question({
        text: 'Q3',
        type: 'multiple',
        choiceKind: 'multiple',
        options: options.map((option) => ({ ...option, correct: false })),
      }),
    ];
    const [q1, q2, q3] = wizardData(draft).questions as Record<string, unknown>[];
    expect(q1).toMatchObject({ correct_answer: [0, 2], min_correct: 1 });
    expect(q2.correct_answer).toEqual([0, 2]);
    expect('min_correct' in q2).toBe(false);
    expect(q3.correct_answer).toEqual([0]);
  });

  it('saves a text question without options and without a type', () => {
    const draft = newQuizDraft();
    draft.questions = [question({ text: 'T', type: 'text', defaultValue: '', correctValue: '42', checker: '  ' })];
    expect(wizardData(draft).questions).toEqual([{ correct_value: '42', question: 'T' }]);
    draft.questions = [question({ text: 'T', type: 'text' })];
    expect(wizardData(draft).questions).toEqual([
      {
        default_value: '0',
        correct_value: '1000',
        checker: 'assert 900 < to_int(user_answer) < 1100, "Вийшли за рамки"',
        question: 'T',
      },
    ]);
  });

  it('saves points other than 1, rounded, and the other question fields', () => {
    const draft = newQuizDraft();
    const base = { options: [{ key: 1, text: 'A', correct: true }, { key: 2, text: 'B', correct: false }] };
    draft.questions = [
      question({ ...base, text: 'Q1', points: '0.5', file: ' data.xlsx ', stick: true, timeLimit: '0', explanation: ' Why ' }),
      question({ ...base, text: 'Q2', points: '1' }),
      question({ ...base, text: 'Q3', points: '' }),
    ];
    const [q1, q2, q3] = wizardData(draft).questions as Record<string, unknown>[];
    expect(q1).toMatchObject({ points: 0.5, file: 'data.xlsx', stick_to_the_previous: true, time_limit: 0, explanation: ' Why ' });
    expect('points' in q2).toBe(false);
    expect('points' in q3).toBe(false);
  });

  it('saves show_explanation only while some question has an explanation', () => {
    const draft = newQuizDraft();
    draft.showExplanation = true;
    draft.questions = [question({ text: 'Q', options: [{ key: 1, text: 'A', correct: true }, { key: 2, text: 'B', correct: false }] })];
    expect('show_explanation' in wizardData(draft)).toBe(false);
    draft.questions[0].explanation = 'Because';
    expect(wizardData(draft).show_explanation).toBe(true);
  });

  it('saves the quiz time limit unless "none" is chosen', () => {
    const draft = newQuizDraft();
    expect('time_limit' in wizardData(draft)).toBe(false);
    draft.timeLimitMode = 'whole_quiz';
    draft.timeLimitSeconds = '600';
    expect(wizardData(draft).time_limit).toEqual({ whole_quiz: 600 });
    draft.timeLimitMode = 'per_question';
    draft.timeLimitSeconds = ' ';
    expect(wizardData(draft).time_limit).toEqual({ per_question: 0 });
  });

  it('keeps the keys the editor does not write, in their order', () => {
    const quiz = {
      title: 'Rivers',
      description: 'A quiz about rivers',
      questions: [
        { question: 'Q1', hint: 'Think of water', options: ['A', 'B'], correct_answer: 0, extra: { level: 2 } },
        { question: 'T', checker: 'assert True', note: 'kept' },
      ],
      show_right_answer: true,
      custom_setting: [1, 2],
    };
    const saved = wizardData(draftFromQuiz(quiz));
    expect(saved.description).toBe('A quiz about rivers');
    expect(saved.custom_setting).toEqual([1, 2]);
    const [q1, q2] = saved.questions as Record<string, unknown>[];
    expect(q1).toEqual({ question: 'Q1', hint: 'Think of water', options: ['A', 'B'], correct_answer: 0, extra: { level: 2 } });
    expect(q2).toEqual({ question: 'T', checker: 'assert True', note: 'kept' });
    // The file's order; the editor's new keys come last
    expect(Object.keys(saved)).toEqual([
      'title',
      'description',
      'questions',
      'show_right_answer',
      'custom_setting',
      'randomize_questions',
      'show_answers_on_completion',
      'show_final_list',
    ]);
    expect(Object.keys(q1)).toEqual(['question', 'hint', 'options', 'correct_answer', 'extra']);
  });

  it('never copies its own keys from the file', () => {
    const draft = draftFromQuiz({
      time_limit: { whole_quiz: 60 },
      show_explanation: true,
      questions: [
        { question: 'Q', checker: 'assert True', correct_value: '1', hint: 'kept', min_correct: 3 },
        { question: 'R', options: ['A', 'B'], correct_answer: [0, 1], min_correct: 2, points: 2, note: 'kept' },
      ],
    });
    draft.timeLimitMode = 'none';
    // The text question becomes a choice question, the other one a text question
    draft.questions[0] = {
      ...draft.questions[0],
      type: 'single',
      options: [
        { key: 1, text: 'X', correct: true },
        { key: 2, text: 'Y', correct: false },
      ],
    };
    draft.questions[1] = { ...draft.questions[1], type: 'text', checker: '', correctValue: 'r', points: '1' };
    const saved = wizardData(draft);
    expect('time_limit' in saved).toBe(false);
    expect('show_explanation' in saved).toBe(false);
    const [q1, q2] = saved.questions as Record<string, unknown>[];
    expect(q1).toEqual({ question: 'Q', options: ['X', 'Y'], correct_answer: 0, hint: 'kept' });
    expect(q2).toEqual({ question: 'R', correct_value: 'r', note: 'kept' });
  });

  it('keeps an odd key name as a plain key', () => {
    const parsed = JSON.parse('{"__proto__": "x", "questions": [{"question": "Q", "options": ["A", "B"], "correct_answer": 0}]}');
    const text = JSON.stringify(wizardData(draftFromQuiz(parsed)));
    expect(JSON.parse(text)).toMatchObject({ questions: [{ question: 'Q' }] });
    expect(text).toContain('"__proto__":"x"');
  });

  it('loads and saves a quiz back to the same data', () => {
    const quiz = {
      title: 'Round trip',
      show_right_answer: false,
      randomize_questions: true,
      show_answers_on_completion: true,
      show_final_list: false,
      show_explanation: true,
      time_limit: { per_question: 30 },
      questions: [
        { question: 'Q1', options: ['A', 'B'], correct_answer: 1, points: 2.5, time_limit: 10, explanation: 'E' },
        { question: 'Q2', options: ['A', 'B', 'C'], correct_answer: [1, 2], min_correct: 1, stick_to_the_previous: true },
        { question: 'Q3', checker: 'assert True', default_value: 'x', correct_value: 'y', file: 'a.pdf' },
      ],
    };
    expect(wizardData(draftFromQuiz(quiz))).toEqual(quiz);
  });
});

describe('validateWizardPoints', () => {
  it('names the first question with bad points', () => {
    const ok = question({ points: '0.25' });
    expect(validateWizardPoints([ok, question({ points: '' })])).toBeNull();
    expect(validateWizardPoints([ok, question({ points: 'abc' })])).toBe('Питання 2: бали мають бути числом');
    expect(validateWizardPoints([question({ points: '0' })])).toBe('Питання 1: бали мають бути більше 0');
    expect(validateWizardPoints([question({ points: '-1' })])).toBe('Питання 1: бали мають бути більше 0');
    expect(validateWizardPoints([question({ points: '0.255' })])).toBe(
      'Питання 1: бали можуть мати не більше 2 знаків після коми',
    );
  });

  it('checks questions that will not be saved too', () => {
    expect(validateWizardPoints([question({ text: '', points: '0' })])).toBe('Питання 1: бали мають бути більше 0');
  });
});

describe('header and explanation helpers', () => {
  it('shows the trophy only for points other than 1', () => {
    expect(indicatorsOf(question({ points: '1' })).points).toBeNull();
    expect(indicatorsOf(question({ points: '' })).points).toBeNull();
    expect(indicatorsOf(question({ points: '0.5' })).points).toBe(0.5);
    expect(indicatorsOf(question({ image: ' ', file: 'a', stick: true }))).toMatchObject({ image: false, file: true, stick: true });
  });

  it('names the fold button by its state', () => {
    expect(explanationToggleText(question({ explanationOpen: false, explanation: '' }))).toBe('➕ Додати пояснення');
    expect(explanationToggleText(question({ explanationOpen: false, explanation: 'x' }))).toBe('📖 Пояснення ▸');
    expect(explanationToggleText(question({ explanationOpen: true }))).toBe('📖 Пояснення ▾');
  });

  it('writes image Markdown with angle brackets for unusual names', () => {
    expect(explanationImageMarkdown('/imgs/plain.png')).toBe('![](/imgs/plain.png)');
    expect(explanationImageMarkdown('/imgs/my pic.png')).toBe('![](</imgs/my pic.png>)');
    expect(explanationImageMarkdown('/imgs/pic(1).png')).toBe('![](</imgs/pic(1).png>)');
    expect(explanationImageMarkdown('other.png')).toBe('![](/imgs/other.png)');
  });

  it('inserts text at the selection', () => {
    expect(insertAt('abcd', 1, 3, 'X')).toEqual({ value: 'aXd', caret: 2 });
    expect(insertAt('', 0, 0, 'img')).toEqual({ value: 'img', caret: 3 });
  });
});

describe('start pop-up settings', () => {
  it('takes the values of the quiz file', () => {
    const settings = startSettingsFromQuiz({
      show_right_answer: true,
      show_answers_on_completion: true,
      questions: [{ question: 'Q', explanation: ' ' }, { question: 'Q2', explanation: 'Why' }],
      show_explanation: true,
      time_limit: { per_question: 45 },
    });
    expect(settings).toEqual({
      showRightAnswer: true,
      // kept here even while hidden, as the old pop-up did
      showAnswersOnCompletion: true,
      randomizeQuestions: false,
      showFinalList: true,
      showExplanation: true,
      hasExplanations: true,
      timeLimitMode: 'per_question',
      timeLimitSeconds: '45',
    });
    expect(startSettingsFromQuiz(null)).toMatchObject({ showRightAnswer: true, hasExplanations: false, timeLimitMode: 'none' });
  });

  it('sends the settings of switch-quiz', () => {
    const settings = startSettingsFromQuiz({ time_limit: { whole_quiz: 600 } });
    expect(startSettingsPayload(settings)).toEqual({
      show_right_answer: true,
      show_answers_on_completion: false,
      randomize_questions: false,
      show_final_list: true,
      time_limit: { whole_quiz: 600 },
    });
    expect(startSettingsPayload({ ...settings, timeLimitMode: 'none' }).time_limit).toBeNull();
    expect(startSettingsPayload({ ...settings, timeLimitSeconds: '' }).time_limit).toEqual({ whole_quiz: 0 });
    expect(startSettingsPayload({ ...settings, hasExplanations: true, showExplanation: false }).show_explanation).toBe(false);
  });
});

describe('other helpers', () => {
  it('makes a label of a registration field name', () => {
    expect(fieldNameToLabel('моля_парола')).toBe('Моля Парола');
    expect(fieldNameToLabel('group')).toBe('Group');
  });

  it('formats file sizes', () => {
    expect(formatFileSize(0)).toBe('0 B');
    expect(formatFileSize(512)).toBe('512 B');
    expect(formatFileSize(1536)).toBe('1.5 KB');
    expect(formatFileSize(1048576)).toBe('1 MB');
  });
});
