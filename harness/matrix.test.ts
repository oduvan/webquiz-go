// Tests of the matrix's pure parts: the all-pairs cases and the 1.19 rules.
// No server, browser or files are needed.

import { describe, expect, it } from 'vitest';
import { allCases, allPairs, caseConfig, DEFAULT_SETTINGS, FACTORS, missingPairs, type Factor, type MatrixCase, type Row } from './matrix-cases';
import { makePng, oppositeSettings, questionById, QUESTIONS, quizFile, startSettings, type QuizSettings } from './matrix-quiz';
import {
  answersInResults,
  answerText,
  canonical,
  checkAutoAdvance,
  checkChoice,
  checkCsvRows,
  checkFeedback,
  checkImageSizes,
  checkNoVerdict,
  checkNoVerdictWhenHidden,
  checkQuestion,
  checkResults,
  checkThemeButtonScroll,
  checkTopControls,
  checkVerdict,
  correctAnswerText,
  effectiveLimit,
  expectedResults,
  expectedVerdict,
  explanationShown,
  feedbackClasses,
  formatPoints,
  judge,
  newIn2,
  orderProblems,
  pointsBadge,
  progressText,
  pyRound,
  registrationLabels,
  waitingNoteShown,
  type ExpectedResults,
  type Outcome,
  type QuestionSnap,
  type Snap,
} from './matrix-rules';
import { TEXTS } from './matrix-texts';

function testCase(over: Partial<MatrixCase> = {}, settings: Partial<QuizSettings> = {}): MatrixCase {
  return {
    name: 'test',
    settings: { ...DEFAULT_SETTINGS, ...settings },
    settingsFrom: 'file',
    approve: false,
    language: 'uk',
    fields: false,
    defaultConfig: false,
    screens: [],
    ...over,
  };
}

/** The main student's plan with every question answered, in file order. */
function answeredAll(): Outcome[] {
  return QUESTIONS.map((q, index) => ({ id: q.id, position: index + 1, answer: q.main }));
}

function emptySnap(): Snap {
  const button = { visible: false, disabled: false, text: '' };
  return {
    visible: { loading: false, registration: false, waiting: false, test: false, results: false, explanation: false, question: false },
    title: '',
    docTitle: '',
    pageText: '',
    dark: false,
    overflowX: 0,
    userInfo: null,
    registration: { heading: '', labels: [], inputs: 0, button: '' },
    waiting: { heading: '', text: '', labels: [], values: [], buttons: [], message: null },
    progress: '',
    progressWidth: null,
    quizTimer: { visible: false, text: '', label: '', warning: false },
    question: null,
    submit: button,
    cont: button,
    verdict: null,
    explanation: { title: '', question: '', strong: [], listItems: 0, links: [], next: button },
    results: null,
  };
}

/** A results snapshot that shows exactly what `expected` describes. */
function resultsSnap(matrixCase: MatrixCase, expected: ExpectedResults): Snap {
  const T = TEXTS[matrixCase.language];
  const paragraphs = [expected.score, ...(expected.points ? [expected.points] : [])];
  if (expected.waitingNote) paragraphs.push(T.answers_available_later, T.reload_later);
  const snap = emptySnap();
  snap.visible.results = true;
  snap.results = {
    paragraphs,
    text: paragraphs.join('\n'),
    buttons: expected.waitingNote ? [T.reload_page] : [],
    table: expected.table
      ? {
          headers: expected.table.headers,
          rows: expected.table.rows.map((row) => {
            const cells = [
              { text: row.question, firstLine: row.question, span: null, timeUp: null, hint: null, images: row.questionImage ? [row.questionImage] : [], fileLink: row.fileLink },
              {
                text: '',
                firstLine: '',
                span: row.timedOut
                  ? { parts: [`⏱ ${T.time_is_up}`], images: [], classes: ['time-up-message'] }
                  : { parts: row.answerParts, images: row.answerImages, classes: expected.answersShown ? [row.correct ? 'answer-correct' : 'answer-incorrect'] : [] },
                timeUp: row.timedOut ? `⏱ ${T.time_is_up}` : null,
                hint: row.hint ? { text: row.hint.prefix + row.hint.parts.join('\n'), images: row.hint.images } : null,
                images: [],
                fileLink: null,
              },
            ];
            if (row.mark) cells.push({ text: row.mark, firstLine: row.mark, span: null, timeUp: null, hint: null, images: [], fileLink: null });
            if (row.points) cells.push({ text: row.points, firstLine: row.points, span: null, timeUp: null, hint: null, images: [], fileLink: null });
            return cells;
          }),
        }
      : null,
  };
  return snap;
}

describe('all-pairs cases', () => {
  it('cover every pair of values of any two factors', () => {
    const rows = allPairs(FACTORS);
    expect(missingPairs(FACTORS, rows)).toEqual([]);
    // 3 time-limit modes x 2 values of any other factor: at least 6 rows; greedy stays small
    expect(rows.length).toBeGreaterThanOrEqual(6);
    expect(rows.length).toBeLessThanOrEqual(12);
  });

  it('give the same cases every time', () => {
    expect(allPairs(FACTORS)).toEqual(allPairs(FACTORS));
  });

  it('cover a small factor set too', () => {
    const factors: Factor[] = [
      { name: 'a', values: ['x', 'y', 'z'] },
      { name: 'b', values: ['p', 'q', 'r'] },
      { name: 'c', values: [true, false] },
    ];
    const rows = allPairs(factors);
    expect(missingPairs(factors, rows)).toEqual([]);
    expect(rows.length).toBe(9); // a x b alone needs 9
  });

  it('missingPairs finds a pair that no row covers', () => {
    const factors: Factor[] = [
      { name: 'a', values: [true, false] },
      { name: 'b', values: [true, false] },
    ];
    const rows: Row[] = [
      { a: true, b: true },
      { a: false, b: false },
      { a: true, b: false },
    ];
    expect(missingPairs(factors, rows)).toEqual(['a=false & b=true']);
  });

  it('all cases: unique names, the two fixed cases on four screens, pairs on two', () => {
    const cases = allCases();
    expect(new Set(cases.map((c) => c.name)).size).toBe(cases.length);
    const byName = Object.fromEntries(cases.map((c) => [c.name, c]));
    expect(byName.defaults.settings).toEqual(DEFAULT_SETTINGS);
    expect(byName.defaults.settingsFrom).toBe('none');
    expect(byName.defaults.screens).toHaveLength(4);
    expect(byName['everything-on'].screens).toHaveLength(4);
    const pairCases = cases.filter((c) => c.name.startsWith('pair-'));
    expect(pairCases.every((c) => c.screens.length === 2)).toBe(true);
    // The pair cases, read back as rows, still cover every pair
    const rows = pairCases.map((c) => ({ ...c.settings, settings_from: c.settingsFrom, approve: c.approve, language: c.language, fields: c.fields }));
    expect(missingPairs(FACTORS, rows as unknown as Row[])).toEqual([]);
  });

  it('about half of the pair cases take the settings from the start settings', () => {
    const pairCases = allCases().filter((c) => c.name.startsWith('pair-'));
    const fromStart = pairCases.filter((c) => c.settingsFrom === 'start').length;
    expect(fromStart).toBeGreaterThanOrEqual(Math.floor(pairCases.length / 2) - 1);
    expect(fromStart).toBeLessThanOrEqual(Math.ceil(pairCases.length / 2) + 1);
  });
});

describe('quiz file and config', () => {
  it('the defaults quiz file has no settings, a settings file has all of them', () => {
    const plain = quizFile(null);
    expect(Object.keys(plain).sort()).toEqual(['questions', 'title']);
    const full = quizFile({ ...DEFAULT_SETTINGS, time_limit: 'per_question', show_explanation: true });
    expect(full.show_explanation).toBe(true);
    expect(full.time_limit).toEqual({ per_question: 15 });
    expect('time_limit' in quizFile(DEFAULT_SETTINGS)).toBe(false);
    expect((full.questions as unknown[]).length).toBe(10);
  });

  it('start settings carry every key, time_limit null for none', () => {
    expect(startSettings(DEFAULT_SETTINGS)).toEqual({
      show_right_answer: true,
      randomize_questions: false,
      show_answers_on_completion: false,
      show_final_list: true,
      show_explanation: false,
      time_limit: null,
    });
  });

  it('the file of a start-settings case has every setting the other way', () => {
    const settings: QuizSettings = { ...DEFAULT_SETTINGS, time_limit: 'whole_quiz' };
    const opposite = oppositeSettings(settings);
    for (const key of ['show_right_answer', 'show_explanation', 'show_final_list', 'show_answers_on_completion', 'randomize_questions'] as const) {
      expect(opposite[key]).toBe(!settings[key]);
    }
    expect(opposite.time_limit).not.toBe(settings.time_limit);
  });

  it('config: nothing for defaults; fields, disabled field and label when asked', () => {
    expect(caseConfig(testCase({ defaultConfig: true }))).toEqual({});
    expect(caseConfig(testCase({ fields: true, approve: true, language: 'en' }))).toEqual({
      language: 'en',
      registration: { approve: true, fields: ['Клас', 'Школа'], disabled_fields: ['Телефон'], username_label: "Прізвище та ім'я" },
    });
  });

  it('makes a valid PNG file', () => {
    const png = makePng(3, 2, () => [10, 20, 30]);
    expect(png.subarray(0, 8)).toEqual(Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]));
    expect(png.readUInt32BE(16)).toBe(3); // IHDR width
    expect(png.readUInt32BE(20)).toBe(2); // IHDR height
  });
});

describe('1.19 rules', () => {
  it('time limits: only per_question gives questions a limit, question 10 keeps its own', () => {
    const q1 = questionById(1);
    const q10 = questionById(10);
    expect(effectiveLimit({ ...DEFAULT_SETTINGS, time_limit: 'none' }, q1)).toBe(0);
    expect(effectiveLimit({ ...DEFAULT_SETTINGS, time_limit: 'per_question' }, q1)).toBe(15);
    expect(effectiveLimit({ ...DEFAULT_SETTINGS, time_limit: 'per_question' }, q10)).toBe(8);
    expect(effectiveLimit({ ...DEFAULT_SETTINGS, time_limit: 'whole_quiz' }, q10)).toBe(0);
  });

  it('judge: several answers need all right ones, unless min_correct says fewer; no wrong one allowed', () => {
    const q2 = questionById(2);
    const q3 = questionById(3);
    expect(judge(q2, { indices: [0, 2] }).correct).toBe(true);
    expect(judge(q2, { indices: [0] }).correct).toBe(false);
    expect(judge(q2, { indices: [0, 1] }).correct).toBe(false);
    expect(judge(q3, { indices: [0, 1] }).correct).toBe(true);
    expect(judge(q3, { indices: [0] }).correct).toBe(false);
    expect(judge(q3, { indices: [0, 1, 3] }).correct).toBe(false);
    expect(judge(questionById(1), { indices: [1] }).correct).toBe(true);
  });

  it('judge: text with a checker gives its message; without one, strip() and exact match', () => {
    expect(judge(questionById(4), { text: '25' })).toEqual({ correct: false, checkerError: 'Має бути 24' });
    expect(judge(questionById(4), { text: '24' })).toEqual({ correct: true, checkerError: null });
    expect(judge(questionById(5), { text: '  Париж ' }).correct).toBe(true);
    expect(judge(questionById(5), { text: 'париж' }).correct).toBe(false);
  });

  it('answer texts are option texts in index order, joined by "|"', () => {
    expect(answerText(questionById(2), { indices: [1, 0] })).toBe('2|3');
    expect(answerText(questionById(7), { indices: [1] })).toBe('/imgs/blue.png');
    expect(correctAnswerText(questionById(3))).toBe('Кіт|Собака|Кит');
    expect(correctAnswerText(questionById(4))).toBe('24');
  });

  it('feedback marks', () => {
    expect(feedbackClasses(questionById(2), { indices: [0, 1] })).toEqual(['feedback-correct', 'feedback-incorrect', 'feedback-missed', null]);
    expect(feedbackClasses(questionById(3), { indices: [0, 1] })).toEqual(['feedback-correct', 'feedback-correct', 'feedback-missed', null]);
    expect(feedbackClasses(questionById(1), { indices: [0] })).toEqual(['feedback-incorrect', 'feedback-correct', null, null]);
    expect(feedbackClasses(questionById(1), { indices: [1] })).toEqual([null, 'feedback-correct', null, null]);
    // time ran out: nothing picked, the right option is shown
    expect(feedbackClasses(questionById(10), null)).toEqual([null, 'feedback-correct', null]);
  });

  it('points badge only for points other than 1, with the Ukrainian plural', () => {
    expect(pointsBadge(questionById(1), TEXTS.uk)).toBeNull();
    expect(pointsBadge(questionById(2), TEXTS.uk)).toBe('🏆 2 бали');
    expect(pointsBadge(questionById(4), TEXTS.uk)).toBe('🏆 0.5 балів');
    expect(pointsBadge(questionById(4), TEXTS.en)).toBe('🏆 0.5 points');
  });

  it('numbers as 1.19 shows them', () => {
    expect(progressText(TEXTS.uk, 3, 10)).toBe('Питання 3 з 10');
    expect(progressText(TEXTS.en, 3, 10)).toBe('Question 3 of 10');
    expect(pyRound(2.5)).toBe(2);
    expect(pyRound(3.5)).toBe(4);
    expect(pyRound((7 / 10.5) * 100)).toBe(67);
    expect(formatPoints(0.1 + 0.2)).toBe('0.3');
    expect(formatPoints(10.5)).toBe('10.5');
    expect(formatPoints(7)).toBe('7');
  });

  it('the explanation step needs show_right_answer, show_explanation and an explanation; never after a whole-quiz timeout', () => {
    const both = { ...DEFAULT_SETTINGS, show_explanation: true };
    expect(explanationShown(both, questionById(1), 'answer')).toBe(true);
    expect(explanationShown(both, questionById(1), 'question-timeout')).toBe(true);
    expect(explanationShown(both, questionById(1), 'quiz-timeout')).toBe(false);
    expect(explanationShown(both, questionById(3), 'answer')).toBe(false);
    expect(explanationShown({ ...both, show_right_answer: false }, questionById(1), 'answer')).toBe(false);
    expect(explanationShown(DEFAULT_SETTINGS, questionById(1), 'answer')).toBe(false);
  });

  it('correct answers in the results: show_right_answer, or everyone finished with show_answers_on_completion', () => {
    const hidden = { ...DEFAULT_SETTINGS, show_right_answer: false, show_answers_on_completion: true };
    expect(answersInResults(hidden, false)).toBe(false);
    expect(answersInResults(hidden, true)).toBe(true);
    expect(answersInResults({ ...hidden, show_answers_on_completion: false }, true)).toBe(false);
    expect(answersInResults(DEFAULT_SETTINGS, false)).toBe(true);
    // the note shows while someone is not done, even with show_right_answer on
    expect(waitingNoteShown({ ...DEFAULT_SETTINGS, show_answers_on_completion: true }, false)).toBe(true);
    expect(waitingNoteShown({ ...DEFAULT_SETTINGS, show_answers_on_completion: true }, true)).toBe(false);
  });

  it('registration labels: the custom username label, then the fields; the translated label without one', () => {
    expect(registrationLabels(testCase({ fields: true }))).toEqual(["Прізвище та ім'я:", 'Клас:', 'Школа:']);
    expect(registrationLabels(testCase({ language: 'en' }))).toEqual(['Username:']);
  });

  it('question order: file order without randomize; question 9 always right after 8', () => {
    const ids = QUESTIONS.map((q) => q.id);
    expect(orderProblems(ids, false)).toEqual([]);
    expect(orderProblems([2, 1, 3, 4, 5, 6, 7, 8, 9, 10], false)).toHaveLength(1);
    expect(orderProblems([10, 8, 9, 1, 2, 3, 4, 5, 6, 7], true)).toEqual([]);
    expect(orderProblems([9, 8, 10, 1, 2, 3, 4, 5, 6, 7], true)).toEqual(['question 9 is not right after 8']);
  });
});

describe('expected results screen', () => {
  it('the plan of the main student: 7 of 10, 7 of 10.5 points', () => {
    const expected = expectedResults(testCase(), answeredAll(), false);
    expect(expected.score).toBe('Результат: 7/10 (70%)');
    expect(expected.points).toBe('Бали: 7/10.5 (67%)');
    expect(expected.waitingNote).toBe(false);
    expect(expected.table!.headers).toEqual(['Питання', 'Ваша відповідь', 'Правильно', 'Бали']);
    const q2 = expected.table!.rows[1];
    expect(q2.hint).toEqual({ prefix: 'Правильна: ', parts: ['2', '4'], images: [] });
    expect(q2.mark).toBe('✗');
    expect(q2.points).toBe('0/2');
  });

  it('hidden answers: no column of marks, no hints, the note, but the points stay', () => {
    const matrixCase = testCase({}, { show_right_answer: false, show_answers_on_completion: true });
    const expected = expectedResults(matrixCase, answeredAll(), false);
    expect(expected.waitingNote).toBe(true);
    expect(expected.table!.headers).toEqual(['Питання', 'Ваша відповідь', 'Бали']);
    expect(expected.table!.rows.every((row) => row.hint === null && row.mark === null)).toBe(true);
    expect(expected.table!.rows[1].points).toBe('0/2');
    const revealed = expectedResults(matrixCase, answeredAll(), true);
    expect(revealed.waitingNote).toBe(false);
    expect(revealed.table!.headers).toContain('Правильно');
  });

  it('timeouts count as wrong, earn nothing, and get a hint while answers are shown', () => {
    const outcomes = answeredAll().map((outcome) => (outcome.id >= 8 ? { ...outcome, answer: null } : outcome));
    const expected = expectedResults(testCase({ language: 'en' }), outcomes, false);
    expect(expected.score).toBe('Result: 4/10 (40%)');
    expect(expected.points).toBe('Points: 4/10.5 (38%)');
    const q8 = expected.table!.rows[7];
    expect(q8.timedOut).toBe(true);
    expect(q8.hint).toEqual({ prefix: 'Correct: ', parts: ['42'], images: [] });
    expect(q8.points).toBe('0/1');
  });

  it('no table without show_final_list', () => {
    expect(expectedResults(testCase({}, { show_final_list: false }), answeredAll(), false).table).toBeNull();
  });
});

describe('check functions', () => {
  it('checkResults passes for a screen that shows the expected results, and fails on a wrong score', () => {
    const matrixCase = testCase({}, { show_right_answer: false, show_answers_on_completion: true });
    const expected = expectedResults(matrixCase, answeredAll(), false);
    const snap = resultsSnap(matrixCase, expected);
    expect(checkResults(matrixCase, expected, snap).filter((check) => !check.pass)).toEqual([]);
    snap.results!.paragraphs[0] = 'Результат: 8/10 (80%)';
    const failed = checkResults(matrixCase, expected, snap).filter((check) => !check.pass);
    expect(failed.map((check) => check.name)).toEqual(['score']);
  });

  it('checkQuestion passes for the right question screen and names what differs', () => {
    const matrixCase = testCase({}, { time_limit: 'per_question' });
    const question = questionById(2);
    const snap = emptySnap();
    const q: QuestionSnap = {
      text: question.question,
      badge: '🏆 2 бали',
      timer: { visible: true, text: '0:14', label: '⏱ Залишилось часу 0:14', warning: false },
      userSelect: 'none',
      hint: TEXTS.uk.multiple_choice_hint,
      options: question.options!.map((text) => ({ text, classes: ['option'], label: '', image: null })),
      textInput: null,
      checkerError: null,
      correctValue: null,
      image: null,
      file: null,
      timeUp: null,
    };
    snap.question = q;
    snap.progress = 'Питання 3 з 10';
    snap.progressWidth = 30.000000000000004;
    snap.userInfo = '👤 Олена';
    snap.submit = { visible: true, disabled: true, text: TEXTS.uk.submit_answer };
    expect(checkQuestion(matrixCase, question, 3, snap, 'Олена').filter((check) => !check.pass)).toEqual([]);
    q.badge = null;
    q.timer = null;
    const failed = checkQuestion(matrixCase, question, 3, snap, 'Олена').filter((check) => !check.pass);
    expect(failed.map((check) => check.name)).toEqual(['points badge (only for points other than 1)', 'question countdown']);
    q.timer = { visible: true, text: '0:09', label: '⏱ Залишилось часу 0:09', warning: false };
    const notRed = checkQuestion(matrixCase, question, 3, snap, 'Олена').filter((check) => !check.pass);
    expect(notRed.map((check) => check.name)).toContain('countdown is red at 10 s or less');
  });

  it('checkChoice wants the chosen options marked and "Submit" working', () => {
    const question = questionById(2);
    expect(checkChoice(question, { indices: [1, 0] }, { selected: [0, 1], submitEnabled: true })[0].pass).toBe(true);
    expect(checkChoice(question, { indices: [0, 1] }, { selected: [0], submitEnabled: true })[0].pass).toBe(false);
    expect(checkChoice(questionById(4), { text: '25' }, { selected: [], submitEnabled: false })[0].pass).toBe(false);
  });

  it('checkFeedback compares the marks and their labels', () => {
    const matrixCase = testCase();
    const question = questionById(2);
    const snap = emptySnap();
    const classes = ['feedback-correct', 'feedback-incorrect', 'feedback-missed', null];
    const labels = [TEXTS.uk.correct_answer_label, TEXTS.uk.your_answer_label, TEXTS.uk.also_correct_label, ''];
    snap.question = {
      ...(emptySnap().question as unknown as QuestionSnap),
      options: question.options!.map((text, index) => ({
        text,
        classes: ['option', 'disabled', ...(classes[index] ? [classes[index] as string] : [])],
        label: labels[index],
        image: null,
      })),
    } as QuestionSnap;
    snap.cont = { visible: true, disabled: false, text: TEXTS.uk.continue_button };
    expect(checkFeedback(matrixCase, question, { indices: [0, 1] }, snap).filter((check) => !check.pass)).toEqual([]);
    expect(checkFeedback(matrixCase, question, { indices: [0, 2] }, snap).filter((check) => !check.pass).length).toBeGreaterThan(0);
  });

  it('checkAutoAdvance wants no "Continue", no marks and a quick move on', () => {
    const quick = { advanced: true, sawContinue: false, sawMarks: false, sawExplanation: false, millis: 700 };
    expect(checkAutoAdvance(quick, false).every((check) => check.pass)).toBe(true);
    expect(checkAutoAdvance({ ...quick, sawContinue: true }, false).every((check) => check.pass)).toBe(false);
    expect(checkAutoAdvance({ ...quick, millis: 1900 }, true).every((check) => check.pass)).toBe(true);
    expect(checkAutoAdvance({ ...quick, millis: 200 }, true).every((check) => check.pass)).toBe(false);
  });

  it('checkCsvRows finds a stored row that disagrees', () => {
    const outcomes = answeredAll().slice(0, 2);
    const rows = outcomes.map((outcome) => {
      const question = questionById(outcome.id);
      return {
        user_id: '1',
        question: question.question,
        selected_answer: answerText(question, outcome.answer!),
        correct_answer: correctAnswerText(question),
        is_correct: judge(question, outcome.answer!).correct ? 'True' : 'False',
      };
    });
    expect(checkCsvRows(outcomes, rows, 'x').every((check) => check.pass)).toBe(true);
    rows[1].is_correct = 'True';
    expect(checkCsvRows(outcomes, rows, 'x').filter((check) => !check.pass)).toHaveLength(1);
  });

  it('checkTopControls: the name badge and theme button must scroll away with the page', () => {
    expect(checkTopControls({ scrolled: 260, badge: 0, toggle: 0 })[0].pass).toBe(true);
    expect(checkTopControls({ scrolled: 260, badge: 260, toggle: 260 })[0].pass).toBe(false);
    expect(checkTopControls({ scrolled: 0, badge: 0, toggle: 0 })).toEqual([]);
  });

  it('checkImageSizes: own size for the question image; options 300 px at most, full width on a phone', () => {
    const snap = emptySnap();
    snap.viewportWidth = 390;
    snap.question = {
      image: { src: 'imgs/figure.png', loaded: true, width: 320, natural: 320 },
      options: [{ text: '', classes: [], label: '', image: { src: 'imgs/red.png', loaded: true, width: 390, natural: 200 } }],
    } as unknown as QuestionSnap;
    expect(checkImageSizes('phone', snap).every((check) => check.pass)).toBe(true);
    snap.question.options[0].image!.width = 200;
    expect(checkImageSizes('phone', snap).map((check) => check.pass)).toEqual([true, false]);
    snap.viewportWidth = 1280;
    expect(checkImageSizes('desktop', snap).every((check) => check.pass)).toBe(true);
  });

  it('checkImageSizes: with a radio circle (2.0) the picture fills the phone width beside the circle', () => {
    const snap = emptySnap();
    snap.viewportWidth = 390;
    const option = { text: '', classes: [], label: '', circleEnd: 38, image: { src: 'imgs/red.png', loaded: true, width: 352, natural: 200 } };
    snap.question = { image: null, options: [option, { ...option }] } as unknown as QuestionSnap;
    const [check] = checkImageSizes('phone', snap);
    expect(check.pass).toBe(true);
    expect(check.expected).toBe('352px wide (the screen less the radio circle)');
    // The picture at its own size does not fill the room beside the circle
    option.image.width = 200;
    expect(checkImageSizes('phone', snap)[0].pass).toBe(false);
    // Without a circle (1.19) the whole screen width is wanted
    snap.question.options = [{ ...option, circleEnd: 0, image: { ...option.image, width: 352 } }];
    expect(checkImageSizes('phone', snap)[0].pass).toBe(false);
  });

  it('checkThemeButtonScroll: the theme button of a teacher page must scroll away with the page', () => {
    expect(checkThemeButtonScroll({ scrolled: 260, badge: null, toggle: 0 }, 'admin.html')[0].pass).toBe(true);
    expect(checkThemeButtonScroll({ scrolled: 260, badge: null, toggle: 260 }, 'admin.html')[0].pass).toBe(false);
    expect(checkThemeButtonScroll({ scrolled: 0, badge: null, toggle: 0 }, 'admin.html')).toEqual([]);
  });

  it('JSON keys may come in any order', () => {
    expect(canonical({ b: 1, a: { d: 2, c: 3 } })).toBe(canonical({ a: { c: 3, d: 2 }, b: 1 }));
  });
});

describe('rules new in 2.0', () => {
  it('newIn2: only a server of version 2 or later has the 2.0 features', () => {
    expect(newIn2('2.0')).toBe(true);
    expect(newIn2('2.1.3')).toBe(true);
    expect(newIn2('10.0')).toBe(true);
    expect(newIn2('2')).toBe(true);
    expect(newIn2('1.19')).toBe(false);
    expect(newIn2('1.0.0')).toBe(false); // what the 1.19 CLI's --version prints
    expect(newIn2('unknown')).toBe(false);
    expect(newIn2(null)).toBe(false);
  });

  it('expectedVerdict: the verdict follows the score, only while right answers are shown, never after a timeout', () => {
    const shown = testCase();
    expect(expectedVerdict(shown, questionById(1), { indices: [1] })).toBe('✓ Правильно');
    expect(expectedVerdict(shown, questionById(1), { indices: [0] })).toBe('✗ Неправильно');
    // A partly right answer with several answers counts as wrong; min_correct 2 lets two right ones count
    expect(expectedVerdict(shown, questionById(2), { indices: [0] })).toBe('✗ Неправильно');
    expect(expectedVerdict(shown, questionById(3), { indices: [0, 1] })).toBe('✓ Правильно');
    // A wrong typed answer
    expect(expectedVerdict(shown, questionById(4), { text: '25' })).toBe('✗ Неправильно');
    expect(expectedVerdict(testCase({ language: 'en' }), questionById(1), { indices: [1] })).toBe('✓ Correct');
    expect(expectedVerdict(testCase({ language: 'en' }), questionById(4), { text: '25' })).toBe('✗ Incorrect');
    expect(expectedVerdict(shown, questionById(10), null)).toBeNull(); // a timeout
    expect(expectedVerdict(testCase({}, { show_right_answer: false }), questionById(1), { indices: [1] })).toBeNull();
  });

  it('checkVerdict: the right words left of "Continue"; none after a timeout', () => {
    const matrixCase = testCase();
    const snap = emptySnap();
    snap.cont = { visible: true, disabled: false, text: TEXTS.uk.continue_button, box: { left: 600, right: 720, top: 400, bottom: 440 } };
    snap.verdict = { text: '✗ Неправильно', box: { left: 440, right: 588, top: 406, bottom: 434 } };
    const wrong = { indices: [0, 1] }; // question 2: one right and one wrong option
    expect(checkVerdict(matrixCase, questionById(2), wrong, snap).map((check) => check.pass)).toEqual([true, true]);
    // The words do not match the score
    expect(checkVerdict(matrixCase, questionById(2), questionById(2).right, snap)[0].pass).toBe(false);
    // Right of "Continue" is the wrong place
    snap.verdict.box = { left: 730, right: 878, top: 406, bottom: 434 };
    expect(checkVerdict(matrixCase, questionById(2), wrong, snap).map((check) => check.pass)).toEqual([true, false]);
    // No verdict shown although one is due
    snap.verdict = null;
    expect(checkVerdict(matrixCase, questionById(2), wrong, snap).map((check) => check.pass)).toEqual([false]);
    // After a timeout there must be none
    expect(checkVerdict(matrixCase, questionById(10), null, snap).map((check) => check.pass)).toEqual([true]);
    snap.verdict = { text: '✗ Неправильно', box: null };
    expect(checkVerdict(matrixCase, questionById(10), null, snap).map((check) => check.pass)).toEqual([false]);
  });

  it('checkNoVerdict and checkNoVerdictWhenHidden: no verdict on later screens or with right answers hidden', () => {
    const snap = emptySnap();
    expect(checkNoVerdict('the next question', snap).pass).toBe(true);
    snap.verdict = { text: '✓ Правильно', box: null };
    const left = checkNoVerdict('the next question', snap);
    expect(left.pass).toBe(false);
    expect(left.name).toBe('no verdict on the next question');
    expect(left.rule).toMatch(/^new in 2\.0/);
    const quiet = { advanced: true, sawContinue: false, sawMarks: false, sawExplanation: false, sawVerdict: false, millis: 300 };
    expect(checkNoVerdictWhenHidden(quiet).pass).toBe(true);
    expect(checkNoVerdictWhenHidden({ ...quiet, sawVerdict: true }).pass).toBe(false);
  });
});
