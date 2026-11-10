// Ports of tests/test_selenium_multiple_choice.py: the student's view of single and
// multiple choice questions (selection, submit, feedback, min_correct, results, progress).
import {
  clickContinue,
  defaultQuiz,
  expect,
  expectSelected,
  findOptions,
  registerUser,
  submitAnswer,
  test,
  waitForContinue,
  waitForQuestionNumber,
} from '../../fixtures/quiz';

// ---- 1. Option rendering and interaction ----

// py: tests/test_selenium_multiple_choice.py::test_multiple_choice_renders_options
test('multiple choice renders options', async ({ page, launch }) => {
  const server = await launch({
    quizzes: defaultQuiz({
      title: 'Multiple Choice Rendering Test',
      questions: [{ question: 'Which are programming languages?', options: ['Python', 'HTML', 'JavaScript', 'CSS'], correct_answer: [0, 2] }],
    }),
  });
  await registerUser(page, server.url);
  await expect(findOptions(page)).toHaveCount(4);
  await expect(page.locator('.multiple-choice-hint')).toBeVisible();
});

// py: tests/test_selenium_multiple_choice.py::test_single_choice_renders_options
test('single choice renders options without the multiple choice hint', async ({ page, launch }) => {
  const server = await launch({
    quizzes: defaultQuiz({
      title: 'Single Choice Test',
      questions: [{ question: 'What is 2 + 2?', options: ['3', '4', '5', '6'], correct_answer: 1 }],
    }),
  });
  await registerUser(page, server.url);
  await expect(findOptions(page)).toHaveCount(4);
  await expect(page.locator('.multiple-choice-hint')).toHaveCount(0);
});

// py: tests/test_selenium_multiple_choice.py::test_select_multiple_options_and_submit
test('select multiple options and submit', async ({ page, launch }) => {
  const server = await launch({
    quizzes: defaultQuiz({
      title: 'Multiple Selection Test',
      questions: [{ question: 'Select all primary colors:', options: ['Red', 'Green', 'Blue', 'Yellow'], correct_answer: [0, 2] }],
    }),
  });
  await registerUser(page, server.url);
  const options = findOptions(page);
  await options.nth(0).click();
  await options.nth(2).click();
  await expectSelected(options.nth(0));
  await expectSelected(options.nth(2));
  await expectSelected(options.nth(1), false);
  await submitAnswer(page);
  await waitForContinue(page);
});

// py: tests/test_selenium_multiple_choice.py::test_unselect_and_reselect_options
test('unselect and reselect options', async ({ page, launch }) => {
  const server = await launch({
    quizzes: defaultQuiz({
      title: 'Option Toggle Test',
      questions: [{ question: 'Select options:', options: ['A', 'B', 'C', 'D'], correct_answer: [0, 1] }],
    }),
  });
  await registerUser(page, server.url);
  const first = findOptions(page).nth(0);
  await first.click();
  await expectSelected(first);
  await first.click();
  await expectSelected(first, false);
  await first.click();
  await expectSelected(first);
});

// py: tests/test_selenium_multiple_choice.py::test_multiple_choice_hint_displayed
test('multiple choice hint is shown', async ({ page, launch }) => {
  const server = await launch({
    quizzes: defaultQuiz({
      title: 'Hint Display Test',
      questions: [{ question: 'Which are fruits?', options: ['Apple', 'Carrot', 'Banana', 'Potato'], correct_answer: [0, 2] }],
    }),
  });
  await registerUser(page, server.url);
  await expect(page.locator('.multiple-choice-hint')).toBeVisible();
});

// ---- 2. Submitting answers ----

// py: tests/test_selenium_multiple_choice.py::test_submit_all_correct_checkboxes
test('all right options get green feedback', async ({ page, launch }) => {
  const server = await launch({
    quizzes: defaultQuiz({
      title: 'All Correct Test',
      show_right_answer: true,
      questions: [{ question: 'Which are programming languages?', options: ['Python', 'HTML', 'JavaScript', 'CSS'], correct_answer: [0, 2] }],
    }),
  });
  await registerUser(page, server.url);
  const options = findOptions(page);
  await options.nth(0).click();
  await options.nth(2).click();
  await submitAnswer(page);
  await waitForContinue(page);
  await expect(options.nth(0)).toHaveClass(/feedback-correct/);
  await expect(options.nth(2)).toHaveClass(/feedback-correct/);
});

// py: tests/test_selenium_multiple_choice.py::test_submit_partial_correct_fails
test('a missed right option is highlighted', async ({ page, launch }) => {
  const server = await launch({
    quizzes: defaultQuiz({
      title: 'Partial Correct Test',
      show_right_answer: true,
      questions: [{ question: 'Select both correct answers:', options: ['Correct1', 'Wrong1', 'Correct2', 'Wrong2'], correct_answer: [0, 2] }],
    }),
  });
  await registerUser(page, server.url);
  const options = findOptions(page);
  await options.nth(0).click();
  await submitAnswer(page);
  await waitForContinue(page);
  await expect(options.nth(2)).toHaveClass(/feedback-missed/);
});

// py: tests/test_selenium_multiple_choice.py::test_submit_with_one_wrong_checkbox_fails
test('a wrong selected option gets red feedback', async ({ page, launch }) => {
  const server = await launch({
    quizzes: defaultQuiz({
      title: 'Wrong Checkbox Test',
      show_right_answer: true,
      questions: [{ question: 'Select correct options:', options: ['Right1', 'Wrong', 'Right2', 'Wrong2'], correct_answer: [0, 2] }],
    }),
  });
  await registerUser(page, server.url);
  const options = findOptions(page);
  await options.nth(0).click();
  await options.nth(1).click();
  await options.nth(2).click();
  await submitAnswer(page);
  await waitForContinue(page);
  await expect(options.nth(1)).toHaveClass(/feedback-incorrect/);
});

// py: tests/test_selenium_multiple_choice.py::test_submit_empty_multiple_choice_blocked
test('submit is disabled without a selection', async ({ page, launch }) => {
  const server = await launch({
    quizzes: defaultQuiz({
      title: 'Empty Submit Test',
      questions: [{ question: 'Select at least one:', options: ['A', 'B', 'C'], correct_answer: [0, 1] }],
    }),
  });
  await registerUser(page, server.url);
  await expect(page.locator('#submit-answer-btn')).toBeDisabled();
});

// py: tests/test_selenium_multiple_choice.py::test_submit_button_enables_after_checkbox_selection
test('submit becomes enabled after an option is selected', async ({ page, launch }) => {
  const server = await launch({
    quizzes: defaultQuiz({
      title: 'Button Enable Test',
      questions: [{ question: 'Select options:', options: ['A', 'B', 'C'], correct_answer: [0, 1] }],
    }),
  });
  await registerUser(page, server.url);
  const submit = page.locator('#submit-answer-btn');
  await expect(submit).toBeDisabled();
  await findOptions(page).nth(0).click();
  await expect(submit).toBeEnabled();
});

// py: tests/test_selenium_multiple_choice.py::test_multiple_submissions_different_questions
test('single, multiple, then single choice in one quiz', async ({ page, launch }) => {
  const server = await launch({
    quizzes: defaultQuiz({
      title: 'Mixed Submission Test',
      questions: [
        { question: 'Single: What is 2+2?', options: ['3', '4', '5'], correct_answer: 1 },
        { question: 'Multiple: Select colors:', options: ['Red', 'Dog', 'Blue', 'Car'], correct_answer: [0, 2] },
        { question: 'Single: Capital of France?', options: ['London', 'Paris', 'Berlin'], correct_answer: 1 },
      ],
    }),
  });
  await registerUser(page, server.url);
  const options = findOptions(page);

  await expect(options).toHaveCount(3);
  await options.nth(1).click();
  await submitAnswer(page);
  await clickContinue(page);

  await waitForQuestionNumber(page, 2, 3);
  await expect(options).toHaveCount(4);
  await options.nth(0).click();
  await options.nth(2).click();
  await submitAnswer(page);
  await clickContinue(page);

  await waitForQuestionNumber(page, 3, 3);
  await expect(options).toHaveCount(3);
  await options.nth(1).click();
  await submitAnswer(page);
  await clickContinue(page);

  await expect(page.locator('#results')).toBeVisible();
});

// ---- 3. Visual feedback ----

// py: tests/test_selenium_multiple_choice.py::test_correct_checkboxes_turn_green
test('every right selected option turns green', async ({ page, launch }) => {
  const server = await launch({
    quizzes: defaultQuiz({
      title: 'Green Feedback Test',
      show_right_answer: true,
      questions: [{ question: 'Select all vowels:', options: ['A', 'B', 'E', 'F', 'I'], correct_answer: [0, 2, 4] }],
    }),
  });
  await registerUser(page, server.url);
  const options = findOptions(page);
  await options.nth(0).click();
  await options.nth(2).click();
  await options.nth(4).click();
  await submitAnswer(page);
  await waitForContinue(page);
  for (const index of [0, 2, 4]) {
    await expect(options.nth(index)).toHaveClass(/feedback-correct/);
  }
});

// py: tests/test_selenium_multiple_choice.py::test_incorrect_checkbox_turns_red
test('a wrong option turns red', async ({ page, launch }) => {
  const server = await launch({
    quizzes: defaultQuiz({
      title: 'Red Feedback Test',
      show_right_answer: true,
      questions: [{ question: 'Select correct:', options: ['Correct', 'Wrong', 'Correct2'], correct_answer: [0, 2] }],
    }),
  });
  await registerUser(page, server.url);
  const options = findOptions(page);
  await options.nth(0).click();
  await options.nth(1).click();
  await options.nth(2).click();
  await submitAnswer(page);
  await waitForContinue(page);
  await expect(options.nth(1)).toHaveClass(/feedback-incorrect/);
});

// py: tests/test_selenium_multiple_choice.py::test_missed_correct_answer_highlighted
test('the missed right answer is highlighted', async ({ page, launch }) => {
  const server = await launch({
    quizzes: defaultQuiz({
      title: 'Missed Answer Test',
      show_right_answer: true,
      questions: [{ question: 'Select all 3 correct:', options: ['C1', 'W1', 'C2', 'W2', 'C3'], correct_answer: [0, 2, 4] }],
    }),
  });
  await registerUser(page, server.url);
  const options = findOptions(page);
  await options.nth(0).click();
  await options.nth(2).click();
  await submitAnswer(page);
  await waitForContinue(page);
  await expect(options.nth(4)).toHaveClass(/feedback-missed/);
});

// py: tests/test_selenium_multiple_choice.py::test_no_feedback_when_show_right_answer_false
test('no feedback with show_right_answer false: straight to the results', async ({ page, launch }) => {
  const server = await launch({
    quizzes: defaultQuiz({
      title: 'No Feedback Test',
      show_right_answer: false,
      questions: [{ question: 'Select options:', options: ['A', 'B', 'C'], correct_answer: [0, 1] }],
    }),
  });
  await registerUser(page, server.url);
  const options = findOptions(page);
  await options.nth(0).click();
  await options.nth(2).click();
  await submitAnswer(page);
  await expect(page.locator('#results')).toBeVisible({ timeout: 3000 });
  await expect(page.locator('#results')).toContainText(/Результат|Result/);
});

// py: tests/test_selenium_multiple_choice.py::test_checkboxes_disabled_after_submit
test('options are disabled after the answer is sent', async ({ page, launch }) => {
  const server = await launch({
    quizzes: defaultQuiz({
      title: 'Disabled Test',
      questions: [{ question: 'Select:', options: ['A', 'B', 'C'], correct_answer: [0, 1] }],
    }),
  });
  await registerUser(page, server.url);
  const options = findOptions(page);
  await options.nth(0).click();
  await submitAnswer(page);
  await waitForContinue(page);
  for (let index = 0; index < 3; index++) {
    await expect(options.nth(index)).toHaveClass(/(^|\s)disabled(\s|$)/);
  }
});

// ---- 4. min_correct ----

// py: tests/test_selenium_multiple_choice.py::test_min_correct_hint_shows_requirement
test('the page tells how many answers are needed', async ({ page, launch }) => {
  const server = await launch({
    quizzes: defaultQuiz({
      title: 'Min Correct Hint Test',
      questions: [
        { question: 'Select at least 2 colors:', options: ['Red', 'Green', 'Blue', 'Yellow'], correct_answer: [0, 1, 2], min_correct: 2 },
      ],
    }),
  });
  await registerUser(page, server.url);
  // The Python test searched the page source for "2" and "least"/"minimum"; the
  // page shows them in the question text. The visible text is checked here.
  const text = (await page.locator('body').innerText()).toLowerCase();
  expect(text).toContain('2');
  expect(text.indexOf('least') !== -1 || text.indexOf('minimum') !== -1).toBe(true);
});

// py: tests/test_selenium_multiple_choice.py::test_min_correct_submit_exact_minimum
test('exactly min_correct right answers is right', async ({ page, launch }) => {
  const server = await launch({
    quizzes: defaultQuiz({
      title: 'Exact Minimum Test',
      show_right_answer: true,
      questions: [{ question: 'Select at least 2:', options: ['C1', 'C2', 'C3', 'W1'], correct_answer: [0, 1, 2], min_correct: 2 }],
    }),
  });
  await registerUser(page, server.url);
  const options = findOptions(page);
  await options.nth(0).click();
  await options.nth(1).click();
  await submitAnswer(page);
  await waitForContinue(page);
  await expect(options.nth(0)).toHaveClass(/feedback-correct/);
  await expect(options.nth(1)).toHaveClass(/feedback-correct/);
});

// py: tests/test_selenium_multiple_choice.py::test_min_correct_submit_more_than_minimum
test('more than min_correct right answers can be sent', async ({ page, launch }) => {
  const server = await launch({
    quizzes: defaultQuiz({
      title: 'More Than Minimum Test',
      questions: [{ question: 'Select at least 2:', options: ['C1', 'C2', 'C3', 'W1'], correct_answer: [0, 1, 2], min_correct: 2 }],
    }),
  });
  await registerUser(page, server.url);
  const options = findOptions(page);
  await options.nth(0).click();
  await options.nth(1).click();
  await options.nth(2).click();
  await submitAnswer(page);
  await waitForContinue(page);
});

// py: tests/test_selenium_multiple_choice.py::test_min_correct_submit_less_than_minimum_fails
test('fewer than min_correct answers show the other right options', async ({ page, launch }) => {
  const server = await launch({
    quizzes: defaultQuiz({
      title: 'Less Than Minimum Test',
      show_right_answer: true,
      questions: [{ question: 'Select at least 2:', options: ['C1', 'C2', 'C3', 'W1'], correct_answer: [0, 1, 2], min_correct: 2 }],
    }),
  });
  await registerUser(page, server.url);
  const options = findOptions(page);
  await options.nth(0).click();
  await submitAnswer(page);
  await waitForContinue(page);
  const missed = await page.locator('.quiz-option.feedback-missed[data-option-index="1"], .quiz-option.feedback-missed[data-option-index="2"]').count();
  expect(missed).toBeGreaterThan(0);
});

// ---- 5. Whole quizzes ----

// py: tests/test_selenium_multiple_choice.py::test_complete_all_multiple_choice_quiz
test('a quiz of only multiple choice questions', async ({ page, launch }) => {
  const server = await launch({
    quizzes: defaultQuiz({
      title: 'All Multiple Choice Quiz',
      questions: [
        { question: 'Q1: Select colors:', options: ['Red', 'Dog', 'Blue', 'Car'], correct_answer: [0, 2] },
        { question: 'Q2: Select numbers:', options: ['One', 'Apple', 'Two', 'Three'], correct_answer: [0, 2, 3] },
        { question: 'Q3: Select animals:', options: ['Cat', 'Table', 'Dog', 'Chair'], correct_answer: [0, 2] },
      ],
    }),
  });
  await registerUser(page, server.url, 'CompleteJourney');
  const options = findOptions(page);
  await options.nth(0).click();
  await options.nth(2).click();
  await submitAnswer(page);
  await clickContinue(page);

  await waitForQuestionNumber(page, 2, 3);
  await options.nth(0).click();
  await options.nth(2).click();
  await options.nth(3).click();
  await submitAnswer(page);
  await clickContinue(page);

  await waitForQuestionNumber(page, 3, 3);
  await options.nth(0).click();
  await options.nth(2).click();
  await submitAnswer(page);
  await clickContinue(page);

  await expect(page.locator('#results')).toBeVisible();
  await expect(page.locator('#results')).toContainText(/Результат|Result/);
});

// py: tests/test_selenium_multiple_choice.py::test_mixed_single_and_multiple_choice_quiz
test('single and multiple choice questions in turn', async ({ page, launch }) => {
  const server = await launch({
    quizzes: defaultQuiz({
      title: 'Mixed Quiz',
      questions: [
        { question: 'Single: 2+2=?', options: ['3', '4', '5'], correct_answer: 1 },
        { question: 'Multiple: Colors?', options: ['Red', 'Car', 'Blue'], correct_answer: [0, 2] },
      ],
    }),
  });
  await registerUser(page, server.url, 'MixedQuiz');
  const options = findOptions(page);
  expect(await options.count()).toBeGreaterThan(0);
  await options.nth(1).click();
  await submitAnswer(page);
  await clickContinue(page);

  await waitForQuestionNumber(page, 2, 2);
  expect(await options.count()).toBeGreaterThan(0);
  await options.nth(0).click();
  await options.nth(2).click();
  await submitAnswer(page);
  await clickContinue(page);

  await expect(page.locator('#results')).toBeVisible();
});

// py: tests/test_selenium_multiple_choice.py::test_perfect_score_multiple_choice
test('all right: 100%', async ({ page, launch }) => {
  const server = await launch({
    quizzes: defaultQuiz({
      title: 'Perfect Score Test',
      questions: [
        { question: 'Q1:', options: ['A', 'B', 'C'], correct_answer: [0, 1] },
        { question: 'Q2:', options: ['X', 'Y', 'Z'], correct_answer: [1, 2] },
      ],
    }),
  });
  await registerUser(page, server.url, 'PerfectScore');
  const options = findOptions(page);
  await options.nth(0).click();
  await options.nth(1).click();
  await submitAnswer(page);
  await clickContinue(page);

  await waitForQuestionNumber(page, 2, 2);
  await options.nth(1).click();
  await options.nth(2).click();
  await submitAnswer(page);
  await clickContinue(page);

  await expect(page.locator('#results')).toContainText(/2\/2|100%/);
});

// py: tests/test_selenium_multiple_choice.py::test_zero_score_multiple_choice
test('all wrong: 0%', async ({ page, launch }) => {
  const server = await launch({
    quizzes: defaultQuiz({
      title: 'Zero Score Test',
      questions: [
        { question: 'Q1:', options: ['Correct1', 'Correct2', 'Wrong1', 'Wrong2'], correct_answer: [0, 1] },
        { question: 'Q2:', options: ['Wrong1', 'Correct1', 'Correct2'], correct_answer: [1, 2] },
      ],
    }),
  });
  await registerUser(page, server.url, 'ZeroScore');
  const options = findOptions(page);
  await options.nth(2).click();
  await options.nth(3).click();
  await submitAnswer(page);
  await clickContinue(page);

  await waitForQuestionNumber(page, 2, 2);
  await options.nth(0).click();
  await submitAnswer(page);
  await clickContinue(page);

  await expect(page.locator('#results')).toContainText(/0\/2|0%/);
});

// py: tests/test_selenium_multiple_choice.py::test_partial_score_mixed_quiz
test('some right, some wrong: 2/3', async ({ page, launch }) => {
  const server = await launch({
    quizzes: defaultQuiz({
      title: 'Partial Score Test',
      questions: [
        { question: 'Q1: Correct', options: ['A', 'B', 'C'], correct_answer: [0, 2] },
        { question: 'Q2: Wrong', options: ['X', 'Y', 'Z'], correct_answer: [0, 1] },
        { question: 'Q3: Correct', options: ['M', 'N', 'O'], correct_answer: [1, 2] },
      ],
    }),
  });
  await registerUser(page, server.url, 'PartialScore');
  const options = findOptions(page);
  await options.nth(0).click();
  await options.nth(2).click();
  await submitAnswer(page);
  await clickContinue(page);

  await waitForQuestionNumber(page, 2, 3);
  await options.nth(2).click();
  await submitAnswer(page);
  await clickContinue(page);

  await waitForQuestionNumber(page, 3, 3);
  await options.nth(1).click();
  await options.nth(2).click();
  await submitAnswer(page);
  await clickContinue(page);

  await expect(page.locator('#results')).toContainText('2/3');
});

// ---- 6. Results ----

// py: tests/test_selenium_multiple_choice.py::test_results_show_multiple_answers_with_pipe
test('results list every selected answer', async ({ page, launch }) => {
  const server = await launch({
    quizzes: defaultQuiz({
      title: 'Results Pipe Test',
      show_right_answer: true,
      questions: [{ question: 'Select three:', options: ['Apple', 'Banana', 'Cherry', 'Date'], correct_answer: [0, 1, 2] }],
    }),
  });
  await registerUser(page, server.url, 'PipeTest');
  const options = findOptions(page);
  await options.nth(0).click();
  await options.nth(1).click();
  await options.nth(2).click();
  await submitAnswer(page);
  await clickContinue(page);

  const content = page.locator('#results-content');
  await expect(content).not.toHaveText('');
  const html = await content.innerHTML();
  for (const value of ['Apple', 'Banana', 'Cherry']) {
    expect(html).toContain(value);
  }
});

// py: tests/test_selenium_multiple_choice.py::test_results_show_correct_answers_multiple_choice
test('results show the right answers', async ({ page, launch }) => {
  const server = await launch({
    quizzes: defaultQuiz({
      title: 'Correct Answers Display Test',
      show_right_answer: true,
      questions: [{ question: 'Select correct:', options: ['Opt1', 'Opt2', 'Opt3', 'Opt4'], correct_answer: [0, 2] }],
    }),
  });
  await registerUser(page, server.url, 'CorrectAnswers');
  const options = findOptions(page);
  await options.nth(1).click();
  await options.nth(3).click();
  await submitAnswer(page);
  await clickContinue(page);

  await expect(page.locator('#results')).toBeVisible();
  await expect(page.locator('#results')).toContainText(/Opt1|Opt3/);
});

// py: tests/test_selenium_multiple_choice.py::test_results_checkmark_and_x_for_multiple_choice
test('results mark right answers with ✓ and wrong ones with ✗', async ({ page, launch }) => {
  const server = await launch({
    quizzes: defaultQuiz({
      title: 'Checkmark Test',
      questions: [
        { question: 'Q1 Correct:', options: ['A', 'B', 'C'], correct_answer: [0, 1] },
        { question: 'Q2 Wrong:', options: ['X', 'Y', 'Z'], correct_answer: [0, 1] },
      ],
    }),
  });
  await registerUser(page, server.url, 'CheckmarkTest');
  const options = findOptions(page);
  await options.nth(0).click();
  await options.nth(1).click();
  await submitAnswer(page);
  await clickContinue(page);

  await waitForQuestionNumber(page, 2, 2);
  await options.nth(2).click();
  await submitAnswer(page);
  await clickContinue(page);

  const results = page.locator('#results');
  await expect(results).toContainText(/✓|✔/);
  await expect(results).toContainText(/✗|✘|×/);
});

// py: tests/test_selenium_multiple_choice.py::test_results_table_displays_all_questions
test('the results table lists every question', async ({ page, launch }) => {
  const server = await launch({
    quizzes: defaultQuiz({
      title: 'Results Table Test',
      questions: [
        { question: 'Single question', options: ['A', 'B'], correct_answer: 0 },
        { question: 'Multiple question', options: ['X', 'Y', 'Z'], correct_answer: [0, 1] },
      ],
    }),
  });
  await registerUser(page, server.url, 'ResultsTable');
  const options = findOptions(page);
  await options.nth(0).click();
  await submitAnswer(page);
  await clickContinue(page);

  await waitForQuestionNumber(page, 2, 2);
  await options.nth(0).click();
  await options.nth(1).click();
  await submitAnswer(page);
  await clickContinue(page);

  const table = page.locator('.results-table');
  await expect(table).toBeVisible();
  const html = await table.innerHTML();
  expect(html).toContain('Single question');
  expect(html).toContain('Multiple question');
});

// ---- 7. Progress ----

// py: tests/test_selenium_multiple_choice.py::test_progress_bar_advances_after_multiple_choice
test('the progress bar grows after an answer', async ({ page, launch }) => {
  const server = await launch({
    quizzes: defaultQuiz({
      title: 'Progress Bar Test',
      questions: [
        { question: 'Q1', options: ['A', 'B'], correct_answer: [0] },
        { question: 'Q2', options: ['X', 'Y'], correct_answer: [0] },
        { question: 'Q3', options: ['M', 'N'], correct_answer: [0] },
      ],
    }),
  });
  await registerUser(page, server.url, 'ProgressBar');
  const fill = page.locator('#progress-fill');
  const initialWidth = await fill.getAttribute('style');

  const options = findOptions(page);
  await options.nth(0).click();
  await submitAnswer(page);
  await clickContinue(page);

  await waitForQuestionNumber(page, 2, 3);
  expect(await fill.getAttribute('style')).not.toBe(initialWidth);
});

// py: tests/test_selenium_multiple_choice.py::test_question_counter_multiple_choice
test('the question counter moves on', async ({ page, launch }) => {
  const server = await launch({
    quizzes: defaultQuiz({
      title: 'Counter Test',
      questions: [
        { question: 'Q1', options: ['A', 'B'], correct_answer: [0] },
        { question: 'Q2', options: ['X', 'Y'], correct_answer: [0] },
        { question: 'Q3', options: ['M', 'N'], correct_answer: [0] },
      ],
    }),
  });
  await registerUser(page, server.url, 'Counter');
  const progress = page.locator('#progress-text');
  await expect(progress).toContainText('1');
  await expect(progress).toContainText('3');

  await findOptions(page).nth(0).click();
  await submitAnswer(page);
  await clickContinue(page);

  await waitForQuestionNumber(page, 2, 3);
  await expect(progress).toContainText('2');
  await expect(progress).toContainText('3');
});

// py: tests/test_selenium_multiple_choice.py::test_continue_button_works_multiple_choice
test('continue opens the next question', async ({ page, launch }) => {
  const server = await launch({
    quizzes: defaultQuiz({
      title: 'Continue Button Test',
      questions: [
        { question: 'First Q', options: ['A', 'B'], correct_answer: [0] },
        { question: 'Second Q', options: ['X', 'Y'], correct_answer: [0] },
      ],
    }),
  });
  await registerUser(page, server.url, 'Continue');
  await findOptions(page).nth(0).click();
  await submitAnswer(page);
  await clickContinue(page);
  await expect(page.locator('.question-text')).toContainText('Second', { timeout: 5000 });
});

// ---- 8. Edge cases ----

// py: tests/test_selenium_multiple_choice.py::test_many_checkboxes_all_visible
test('twelve options are all shown', async ({ page, launch }) => {
  const options = Array.from({ length: 12 }, (_value, i) => 'Option' + i);
  const server = await launch({
    quizzes: defaultQuiz({
      title: 'Many Options Test',
      questions: [{ question: 'Select from many options:', options, correct_answer: [0, 5, 10] }],
    }),
  });
  await registerUser(page, server.url, 'ManyOptions');
  const shown = findOptions(page);
  await expect(shown).toHaveCount(12);
  for (let index = 0; index < 12; index++) {
    await expect(shown.nth(index)).toBeVisible();
  }
});

// py: tests/test_selenium_multiple_choice.py::test_select_all_options_when_all_correct
test('every option can be selected when all are right', async ({ page, launch }) => {
  const server = await launch({
    quizzes: defaultQuiz({
      title: 'All Correct Test',
      questions: [{ question: 'All are correct:', options: ['C1', 'C2', 'C3', 'C4'], correct_answer: [0, 1, 2, 3] }],
    }),
  });
  await registerUser(page, server.url, 'AllCorrect');
  const options = findOptions(page);
  for (let index = 0; index < 4; index++) {
    await options.nth(index).click();
  }
  for (let index = 0; index < 4; index++) {
    await expectSelected(options.nth(index));
  }
  await submitAnswer(page);
  await waitForContinue(page);
});

// py: tests/test_selenium_multiple_choice.py::test_rapid_option_clicking_stable
test('fast clicking keeps a correct selection', async ({ page, launch }) => {
  const server = await launch({
    quizzes: defaultQuiz({
      title: 'Rapid Click Test',
      questions: [{ question: 'Click rapidly:', options: ['A', 'B', 'C', 'D'], correct_answer: [0, 1] }],
    }),
  });
  await registerUser(page, server.url, 'RapidClick');
  const options = findOptions(page);
  for (let i = 0; i < 5; i++) {
    await options.nth(0).click();
    await page.waitForTimeout(50);
  }
  await expectSelected(options.nth(0)); // an odd number of clicks
  await options.nth(0).click();
  await expectSelected(options.nth(0), false);
  await options.nth(0).click();
  await options.nth(1).click();
  await expect(page.locator('#submit-answer-btn')).toBeEnabled();
});
