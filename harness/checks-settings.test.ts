// Unit tests for the checks of the quiz settings and of teacher reports (harness/checks.ts).
//
// The expected behaviour is Python WebQuiz 1.19's: templates/index.html
// (showCompletedTestResults) and server.py (all_students_completed,
// get_user_final_results, _record_answer, register_user, flush_users_to_csv).

import { describe, expect, it } from 'vitest';
import {
  checkAgentAlignment,
  checkAnswersHidden,
  checkCheckerMessages,
  checkExplanations,
  checkLanguage,
  checkNotes,
  checkPhoneWidth,
  checkRegistrationFields,
  checkResultsScreens,
  checkStickyOrder,
  classDone,
  fieldName,
  parseAgentReport,
  parseNotes,
  type AnswerReply,
  type ClassEvent,
  type DriverStep,
  type FinalReply,
  type ResultScreen,
} from './checks';

const UK = {
  result_label: 'Результат:',
  question_header: 'Питання',
  your_answer_header: 'Ваша відповідь',
  correct_header_alt: 'Правильно',
  answers_available_later: 'ℹ️ Правильні відповіді будуть доступні після завершення тесту всіма учнями.',
};
const EN = {
  result_label: 'Result:',
  question_header: 'Question',
  your_answer_header: 'Your answer',
  correct_header_alt: 'Correct',
  answers_available_later: 'ℹ️ Correct answers will be available after all students complete the quiz.',
};

function screen(over: Partial<ResultScreen> = {}): ResultScreen {
  return {
    student: 's1',
    user: 'u1',
    screen: 'Результат: 2/3 (67%)\nПитання\tВаша відповідь\tПравильно\nQ1\tA\t✓',
    texts: UK,
    showFinalList: true,
    final: { all_completed: true, show_answers_on_completion: false, has_correctness: true },
    ...over,
  };
}

describe('results screen', () => {
  it('passes a screen that follows the settings', () => {
    expect(checkResultsScreens([screen()])).toEqual([]);
  });

  it('wants the list of questions only with show_final_list', () => {
    expect(checkResultsScreens([screen({ showFinalList: false })])[0]).toContain('show_final_list is off');
    const noList = screen({ screen: 'Result: 2/3 (67%)', texts: EN, showFinalList: false });
    expect(checkResultsScreens([noList])).toEqual([]);
    expect(checkResultsScreens([{ ...noList, showFinalList: true }])[0]).toContain('has no list of questions');
  });

  it('wants the score line', () => {
    expect(checkResultsScreens([screen({ screen: 'Питання\tВаша відповідь\tПравильно' })])[0]).toContain('no "Результат:" line');
  });

  it('wants the note "answers later" exactly while not everyone has finished', () => {
    const waiting = { all_completed: false, show_answers_on_completion: true, has_correctness: false };
    const withNote = `Результат: 2/3 (67%)\n${UK.answers_available_later}\nПитання\tВаша відповідь\nQ1\tA`;
    expect(checkResultsScreens([screen({ screen: withNote, final: waiting })])).toEqual([]);
    expect(checkResultsScreens([screen({ screen: 'Результат: 2/3 (67%)\nПитання\tВаша відповідь\nQ1\tA', final: waiting })])[0]).toContain(
      'is missing',
    );
    const done = { ...waiting, all_completed: true, has_correctness: true };
    expect(checkResultsScreens([screen({ screen: withNote + '\tПравильно', final: done })]).some((p) => p.includes('shows'))).toBe(true);
  });

  it('wants the "correct" column exactly when the reply has the correct answers', () => {
    const hidden = { all_completed: false, show_answers_on_completion: false, has_correctness: false };
    expect(checkResultsScreens([screen({ final: hidden })])[0]).toContain('"Правильно" column shows');
    expect(checkResultsScreens([screen({ screen: 'Результат: 1/1 (100%)\nПитання\tВаша відповідь\nQ\tA', final: hidden })])).toEqual([]);
  });

  it('reads the phone layout, where every header cell is a line of its own', () => {
    const phone = 'Result: 1/2 (50%)\nQuestion\nYour answer\nCorrect\nWhat?\nParis\n✓';
    expect(checkResultsScreens([screen({ screen: phone, texts: EN })])).toEqual([]);
    // "Correct: Paris" (the hint) and the note are not the column
    const hidden = { all_completed: false, show_answers_on_completion: true, has_correctness: false };
    const text = `Result: 1/2 (50%)\n${EN.answers_available_later}\nQuestion\nYour answer\nWhat?\nRome`;
    expect(checkResultsScreens([screen({ screen: text, texts: EN, final: hidden })])).toEqual([]);
  });
});

describe('correct answers hidden until everyone finishes', () => {
  const events: ClassEvent[] = [
    { t: 10, kind: 'joined', user: 'a' },
    { t: 11, kind: 'joined', user: 'b' },
    { t: 30, kind: 'completed', user: 'a' },
    { t: 50, kind: 'completed', user: 'b' },
  ];
  const reply = (over: Partial<FinalReply>): FinalReply => ({
    student: 's',
    user: 'a',
    t: 31,
    all_completed: false,
    show_answers_on_completion: true,
    has_correctness: false,
    show_right_answer: false,
    ...over,
  });

  it('knows when the class is done', () => {
    expect(classDone(events, 5)).toBe(false);
    expect(classDone(events, 40)).toBe(false);
    expect(classDone(events, 60)).toBe(true);
    // A new student makes it not done again
    expect(classDone([...events, { t: 70, kind: 'joined', user: 'c' }], 75)).toBe(false);
    expect(classDone([...events, { t: 35, kind: 'forced' }], 40)).toBe(true);
  });

  it('accepts the rules of 1.19', () => {
    const good = [
      reply({ t: 31 }),
      reply({ t: 60, all_completed: true, has_correctness: true }),
      reply({ t: 60, show_right_answer: true, show_answers_on_completion: false, all_completed: true, has_correctness: true }),
      reply({ t: 40, show_right_answer: true, has_correctness: true }),
    ];
    expect(checkAnswersHidden(good, events)).toEqual([]);
  });

  it('finds answers shown too early, hidden too long, or a wrong all_completed', () => {
    expect(checkAnswersHidden([reply({ t: 31, has_correctness: true })], events)[0]).toContain('must stay hidden');
    expect(checkAnswersHidden([reply({ t: 60, all_completed: true })], events)[0]).toContain('should show now');
    expect(checkAnswersHidden([reply({ t: 60 })], events)[0]).toContain('every student had finished');
    expect(checkAnswersHidden([reply({ t: 31, all_completed: true, has_correctness: true })], events)[0]).toContain(
      'not every student had finished',
    );
  });

  it('does not judge a reply right at a change, and counts the teacher\'s button', () => {
    expect(checkAnswersHidden([reply({ t: 50.5 })], events)).toEqual([]);
    const forced = [...events, { t: 35, kind: 'forced' as const }];
    expect(checkAnswersHidden([reply({ t: 40, all_completed: true, has_correctness: true })], forced)).toEqual([]);
  });
});

describe('registration fields', () => {
  const fields = [
    { name: 'клас', label: 'Клас' },
    { name: 'школа', label: 'Школа' },
  ];
  const sent = { student: 's1', user: 'u1', sent: { клас: '7-А', школа: 'Школа 1' }, fields };

  it('makes field names like 1.19', () => {
    expect(fieldName('Номер групи')).toBe('номер_групи');
  });

  it('finds the typed values in the users CSV', () => {
    const rows = new Map([['u1', { user_id: 'u1', клас: '7-А', школа: 'Школа 1' }]]);
    expect(checkRegistrationFields([sent], rows, ['Email'])).toEqual([]);
  });

  it('finds a missing column, a different value, a missing user and a disabled field', () => {
    const rows = new Map([['u1', { user_id: 'u1', клас: '7-Б', email: '' }]]);
    const problems = checkRegistrationFields([sent], rows, ['Email']);
    expect(problems.some((p) => p.includes("'7-А'") && p.includes("'7-Б'"))).toBe(true);
    expect(problems.some((p) => p.includes('no column "школа"'))).toBe(true);
    expect(problems.some((p) => p.includes('disabled field "Email"'))).toBe(true);
    expect(checkRegistrationFields([sent], new Map(), [])[0]).toContain('not in the users CSV');
    const shown = { ...sent, fields: [...fields, { name: 'email', label: 'Email' }] };
    expect(checkRegistrationFields([shown], new Map([['u1', { клас: '7-А', школа: 'Школа 1', email: '' }]]), ['Email'])[0]).toContain(
      'is on the registration form',
    );
  });
});

describe('explanations', () => {
  const base: AnswerReply = {
    student: 's1',
    user: 'u1',
    question: 'Q1',
    response: { is_correct: true, explanation_html: '<p>Why</p>' },
    showRightAnswer: true,
    wholeQuiz: false,
    showExplanation: true,
    hasExplanation: true,
    laterScreens: ['📖 Пояснення\nWhy\nДалі'],
    explanationTitle: '📖 Пояснення',
  };

  it('passes an explanation that comes and shows', () => {
    expect(checkExplanations([base])).toEqual([]);
    expect(checkExplanations([{ ...base, response: { is_correct: true }, hasExplanation: false }])).toEqual([]);
  });

  it('finds explanations against the settings', () => {
    expect(checkExplanations([{ ...base, showRightAnswer: false, laterScreens: [] }])[0]).toContain('correct answers are hidden');
    expect(checkExplanations([{ ...base, showExplanation: false }])[0]).toContain('show_explanation is off');
    const timeUp = { ...base, wholeQuiz: true, response: { timed_out: true, explanation_html: '<p>x</p>' } };
    expect(checkExplanations([timeUp])[0]).toContain('whole-quiz time had run out');
    expect(checkExplanations([{ ...base, response: { is_correct: false } }])[0]).toContain('the reply had none');
    expect(checkExplanations([{ ...base, laterScreens: ['Питання 2 з 3'] }])[0]).toContain('never showed');
  });
});

describe('language, checker messages, sticky questions, phone width', () => {
  it('finds texts of the other language', () => {
    expect(checkLanguage([{ student: 's', step: 3, language: 'en', screen: 'Question 1 of 3\nSubmit answer' }])).toEqual([]);
    const problems = checkLanguage([
      { student: 's', step: 3, language: 'en', screen: 'Question 1 of 3\nВідправити відповідь' },
      { student: 's', step: 4, language: 'en', screen: 'Відправити відповідь' },
      { student: 't', step: 1, language: 'uk', screen: 'Submit answer' },
    ]);
    expect(problems).toHaveLength(2);
    expect(problems[0]).toContain('"Відправити відповідь"');
  });

  it('finds a checker message the screen did not show', () => {
    const item = { student: 's', user: 'u', question: 'Q', message: 'Порахуйте ще раз', screens: ['⚠️ Порахуйте ще раз'] };
    expect(checkCheckerMessages([item])).toEqual([]);
    expect(checkCheckerMessages([{ ...item, screens: ['Продовжити'] }])).toHaveLength(1);
    expect(checkCheckerMessages([{ ...item, screens: [] }])).toEqual([]);
  });

  it('finds questions that should stick together but do not', () => {
    expect(checkStickyOrder([{ student: 's', order: [4, 2, 3, 1, 5] }], [[2, 3]])).toEqual([]);
    expect(checkStickyOrder([{ student: 's', order: [3, 2, 4, 1, 5] }], [[2, 3]])[0]).toContain('3, 2, 4, 1, 5');
  });

  it('reports a page wider than a phone once per student, with a screenshot', () => {
    const problems = checkPhoneWidth([
      { student: 'p', step: 3, page: 520, view: 390, culprits: ['table.results-table (500 px)'], screenshot: 'shots/p-003-wide.png' },
      { student: 'p', step: 4, page: 610, view: 390, culprits: [], screenshot: null },
    ]);
    expect(problems).toHaveLength(1);
    expect(problems[0]).toContain('610 px wide on a 390 px phone screen');
    expect(problems[0]).toContain('steps 3, 4');
    expect(problems[0]).toContain('shots/p-003-wide.png');
    expect(checkPhoneWidth([])).toEqual([]);
  });
});

describe('teacher reports', () => {
  const report = `STEP 1 | DID: open admin | SAW: "Ключ:"
STEP 2 | DID: fill 2 "harness" | SAW: "[2] input "Ключ:" = "harness""
STEP 3 | DID: select 4 "a.yaml" "b.yaml" | SAW: "Об'єднати"

NOTES:
- Step 2: the key field showed the key as text.
- Steps 3-4: the list changed after the click.
- The page was slow.
`;

  it('reads the steps and checks every argument of a teacher command', () => {
    const steps = parseAgentReport(report);
    expect(steps.map((s) => s.did)).toEqual(['open admin', 'fill 2 "harness"', 'select 4 "a.yaml" "b.yaml"']);
    const driver = new Map<number, DriverStep>([
      [1, { cmd: 'open', arg: 'admin', args: ['admin'], screen: 'Ключ:' }],
      [2, { cmd: 'fill', arg: '2 harness', args: ['2', 'harness'], screen: '[2] input "Ключ:" = "harness"' }],
      [3, { cmd: 'select', arg: '4 a.yaml c.yaml', args: ['4', 'a.yaml', 'c.yaml'], screen: "🔗 Об'єднати quizzes" }],
    ]);
    const problems = checkAgentAlignment(steps, driver);
    expect(problems).toHaveLength(1);
    expect(problems[0]).toContain('step 3');
  });

  it('reads the notes and checks their step numbers', () => {
    const notes = parseNotes(report);
    expect(notes.map((n) => n.steps)).toEqual([[2], [3, 4], []]);
    const problems = checkNotes(notes, new Set([1, 2, 3]));
    expect(problems.some((p) => p.includes('step 4'))).toBe(true);
    expect(problems.some((p) => p.includes('names no step'))).toBe(true);
    expect(checkNotes(Array.from({ length: 9 }, () => ({ steps: [1], text: 'x' })), new Set([1]))[0]).toContain('9 notes');
  });
});
