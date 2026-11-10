// Unit tests for the classroom harness checks (harness/checks.ts).
//
// They use small fake records only: no server, browser or AI agents.

import { describe, expect, it } from 'vitest';
import {
  checkAgentAlignment,
  checkAnswerTimes,
  checkExpectations,
  checkLiveStats,
  checkScreenScores,
  checkSubmitsMatchRows,
  checkTimeAccuracy,
  checkTimeoutRows,
  checkUniqueAnswers,
  pairKey,
  parseAgentReport,
  parseScreenScore,
  type AnswerRow,
  type DriverStep,
} from './checks';

function row(over: Partial<AnswerRow> = {}): AnswerRow {
  return { user_id: 'u1', question: 'Q1?', selected_answer: 'A', is_correct: true, time_taken: 3.0, ...over };
}

const pairs = (...items: [string, string][]) => new Set(items.map(([user, question]) => pairKey(user, question)));

describe('agent reports', () => {
  // py: tests/test_harness_checks.py::test_parse_agent_report_reads_step_lines_and_quotes
  it('reads step lines and quotes', () => {
    const text = `Some intro the agent wrote.
STEP 1 | DID: open | SAW: "Математика"; "Ім'я користувача"
STEP 2 | DID: type Олена | SAW: "Олена"
step 3 | did: click 56 | saw: "✓ Правильна відповідь"
NOTES: the timer showed 0:28
`;
    const steps = parseAgentReport(text);
    expect(steps.map((s) => s.step)).toEqual([1, 2, 3]);
    expect(steps[0].did).toBe('open');
    expect(steps[0].quotes).toEqual(['Математика', "Ім'я користувача"]);
    expect(steps[2].quotes).toEqual(['✓ Правильна відповідь']);
  });

  // py: tests/test_harness_checks.py::test_agent_alignment_finds_wrong_quote_and_wrong_action
  it('finds a wrong quote and a wrong action', () => {
    const driverSteps = new Map<number, DriverStep>([
      [1, { cmd: 'open', arg: null, screen: "Математика\nІм'я користувача" }],
      [2, { cmd: 'click', arg: '56', screen: '✓ Правильна відповідь\nПродовжити' }],
    ]);
    const agentSteps = [
      { step: 1, did: 'open', quotes: ['Математика'] },
      { step: 2, did: 'click 54', quotes: ['Неправильна відповідь'] },
      { step: 9, did: 'look', quotes: [] },
    ];
    const problems = checkAgentAlignment(agentSteps, driverSteps);
    expect(problems.some((p) => p.includes('step 2') && p.includes('click 54'))).toBe(true);
    expect(problems.some((p) => p.includes('step 2') && p.includes('Неправильна відповідь'))).toBe(true);
    expect(problems.some((p) => p.includes('step 9'))).toBe(true);
    expect(problems.some((p) => p.includes('step 1'))).toBe(false);
  });

  // py: tests/test_harness_checks.py::test_agent_alignment_ignores_spaces_and_case_in_quotes
  it('ignores spaces and case in quotes', () => {
    const driverSteps = new Map<number, DriverStep>([[1, { cmd: 'look', arg: null, screen: '⏱ Залишилось часу   0:28' }]]);
    expect(checkAgentAlignment([{ step: 1, did: 'look', quotes: ['залишилось часу 0:28'] }], driverSteps)).toEqual([]);
  });

  // py: tests/test_harness_checks.py::test_parse_agent_report_ignores_punctuation_left_by_nested_quotes
  it('ignores punctuation left by nested quotes', () => {
    const line =
      'STEP 13 | DID: type "6" | SAW: "TYPE INTO: [textarea] Введіть відповідь... = "6""; "✍️ Введіть вашу відповідь:"';
    const quotes = parseAgentReport(line)[0].quotes;
    expect(quotes.map((q) => q.trim())).not.toContain(';');
    expect(quotes).toContain('✍️ Введіть вашу відповідь:');
  });

  // py: tests/test_harness_checks.py::test_parse_agent_report_reads_backslash_escaped_quotes
  it('reads backslash escaped quotes', () => {
    const line = 'STEP 20 | DID: type "100" | SAW: "✍️ Введіть вашу відповідь:"; "Введіть відповідь... = \\"100\\""';
    const quotes = parseAgentReport(line)[0].quotes;
    expect(quotes[1]).toContain('Введіть відповідь... = "100');
    expect(quotes[1]).not.toContain('\\');
  });
});

describe('server data', () => {
  // py: tests/test_harness_checks.py::test_unique_answers
  it('finds an answer stored twice', () => {
    expect(checkUniqueAnswers([row(), row({ question: 'Q2?' })])).toEqual([]);
    expect(checkUniqueAnswers([row(), row({ selected_answer: 'B' })]).length).toBeGreaterThan(0);
  });

  // py: tests/test_harness_checks.py::test_timeout_rows_must_be_failed
  it('timeout rows must be failed', () => {
    expect(checkTimeoutRows([row({ selected_answer: '', is_correct: false })])).toEqual([]);
    expect(checkTimeoutRows([row({ selected_answer: '', is_correct: true })]).length).toBeGreaterThan(0);
  });

  // py: tests/test_harness_checks.py::test_answer_times_respect_limit_and_grace
  it('answer times respect the limit and the grace', () => {
    const limits = { 'Q1?': 20, 'Q2?': 0 };
    const rows = [
      row({ question: 'Q1?', time_taken: 24.0 }), // inside the 5 s grace
      row({ question: 'Q2?', time_taken: 300.0 }), // no limit
      row({ question: 'Q1?', user_id: 'u2', selected_answer: '', is_correct: false, time_taken: 40.0 }), // timeouts are skipped
    ];
    expect(checkAnswerTimes(rows, limits, 5)).toEqual([]);
    expect(checkAnswerTimes([row({ question: 'Q1?', time_taken: 27.5 })], limits, 5).length).toBeGreaterThan(0);
  });

  // py: tests/test_harness_checks.py::test_submits_match_rows
  it('submits match rows', () => {
    const rows = [row({ question: 'Q1?', is_correct: true }), row({ question: 'Q2?', selected_answer: '', is_correct: false })];
    const submits = [
      { user_id: 'u1', question: 'Q1?', status: 200, response: { is_correct: true } },
      { user_id: 'u1', question: 'Q2?', status: 200, response: { timed_out: true } },
    ];
    expect(checkSubmitsMatchRows(submits, rows, pairs())).toEqual([]);

    // A stored row nobody sent, and not written by the background check
    const extra = [...rows, row({ question: 'Q3?', selected_answer: '', is_correct: false })];
    expect(checkSubmitsMatchRows(submits, extra, pairs()).length).toBeGreaterThan(0);
    expect(checkSubmitsMatchRows(submits, extra, pairs(['u1', 'Q3?']))).toEqual([]);

    // An accepted answer with no row (lost answer)
    expect(
      checkSubmitsMatchRows([...submits, { user_id: 'u1', question: 'Q4?', status: 200, response: {} }], rows, pairs()).length,
    ).toBeGreaterThan(0);

    // The screen was told "correct", the data says wrong
    const wrong = [row({ question: 'Q1?', is_correct: false }), rows[1]];
    expect(checkSubmitsMatchRows(submits, wrong, pairs()).length).toBeGreaterThan(0);

    // The screen was told "time is up", the data has an answer
    const answered = [rows[0], row({ question: 'Q2?', selected_answer: 'B', is_correct: false })];
    expect(checkSubmitsMatchRows(submits, answered, pairs()).length).toBeGreaterThan(0);
  });

  // py: tests/test_harness_checks.py::test_parse_screen_score
  it('reads the score from a screen', () => {
    expect(parseScreenScore('Математика\nРезультат: 2/3 (67%)\nПитання')).toEqual([2, 3]);
    expect(parseScreenScore('Result: 5/5 (100%)')).toEqual([5, 5]);
    expect(parseScreenScore('Питання 2 з 3')).toBeNull();
  });

  // py: tests/test_harness_checks.py::test_screen_scores_match_rows
  it('screen scores match rows', () => {
    const rows = [row({ question: 'Q1?' }), row({ question: 'Q2?', is_correct: false }), row({ question: 'Q3?' })];
    expect(checkScreenScores({ u1: [2, 3] }, rows, 3)).toEqual([]);
    expect(checkScreenScores({ u1: [3, 3] }, rows, 3).length).toBeGreaterThan(0);
  });

  // py: tests/test_harness_checks.py::test_live_stats_final_states_match_rows
  it('live stats final states match rows', () => {
    const rows = [
      row({ question: 'Q1?' }),
      row({ question: 'Q2?', is_correct: false }),
      row({ question: 'Q3?', selected_answer: '', is_correct: false }),
    ];
    const states = new Map([
      [pairKey('u1', 'Q1?'), 'ok'],
      [pairKey('u1', 'Q2?'), 'fail'],
      [pairKey('u1', 'Q3?'), 'timeout'],
    ]);
    expect(checkLiveStats(states, rows)).toEqual([]);
    expect(checkLiveStats(new Map([...states, [pairKey('u1', 'Q3?'), 'fail']]), rows).length).toBeGreaterThan(0);
    expect(checkLiveStats(new Map([...states, [pairKey('u1', 'Q2?'), 'think']]), rows).length).toBeGreaterThan(0);
  });

  // py: tests/test_harness_checks.py::test_expectations_compare_the_planned_outcome_with_the_stored_answer
  it('compares the planned outcome with the stored answer', () => {
    const questionText = { '1': 'Q1?', '2': 'Q2?', '3': 'Q3?' };
    const userOf = { e02: 'u2' };
    const rows = [
      row({ user_id: 'u2', question: 'Q1?', is_correct: true }),
      row({ user_id: 'u2', question: 'Q2?', selected_answer: '', is_correct: false }),
    ];
    const good = { e02: { '1': 'correct', '2': 'timeout', '3': 'missing' } };
    expect(checkExpectations(good, rows, userOf, questionText)).toEqual([]);

    const problems = checkExpectations({ e02: { '1': 'timeout', '2': 'wrong', '3': 'correct' } }, rows, userOf, questionText);
    expect(problems).toHaveLength(3);
    expect(problems.some((p) => p.includes('Q1?') && p.includes('timeout') && p.includes('correct'))).toBe(true);

    expect(checkExpectations({ nobody: { '1': 'correct' } }, rows, userOf, questionText).length).toBeGreaterThan(0);
  });

  // py: tests/test_harness_checks.py::test_time_accuracy_compares_server_time_with_browser_time
  it('compares server time with browser time', () => {
    const rows = [
      row({ question: 'Q1?', time_taken: 5.2 }),
      row({ question: 'Q2?', time_taken: 9.0 }),
      row({ question: 'Q3?', selected_answer: '', is_correct: false, time_taken: 30.0 }), // timeouts are skipped
    ];
    const measured = new Map([
      [pairKey('u1', 'Q1?'), 5.0],
      [pairKey('u1', 'Q2?'), 6.0],
      [pairKey('u1', 'Q3?'), 3.0],
    ]);
    const problems = checkTimeAccuracy(rows, measured, 1.5);
    expect(problems).toHaveLength(1);
    expect(problems[0]).toContain('Q2?');
    expect(problems[0]).toContain('9.0');
    expect(problems[0]).toContain('6.0');
    expect(checkTimeAccuracy(rows.slice(0, 1), measured, 1.5)).toEqual([]);
  });
});
