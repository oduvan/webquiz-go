// Ports of tests/test_user_journey_selenium.py: registration, a whole quiz, feedback,
// progress, theme, reload, question kinds, buttons, results and show_right_answer.
import {
  clickContinue,
  defaultQuiz,
  expect,
  expectSelected,
  findOptions,
  optionByIndex,
  optionByText,
  submitAnswer,
  test,
  waitForContinue,
  waitForQuestionText,
} from '../../fixtures/quiz';
import type { Page } from '@playwright/test';

async function register(page: Page, url: string, name: string) {
  await page.goto(url + '/');
  await page.locator('#username').fill(name);
  await page.locator('.register-btn').click();
  await expect(page.locator('#current-question-container .current-question')).toBeVisible();
}

// py: tests/test_user_journey_selenium.py::test_user_registration_complete_flow
test('registration shows the quiz and the name', async ({ page, launch }) => {
  const server = await launch({
    quizzes: defaultQuiz({
      title: 'Registration Test Quiz',
      questions: [{ question: 'Test registration question?', options: ['A', 'B', 'C'], correct_answer: 1 }],
    }),
  });
  await page.goto(server.url + '/');
  const username = page.locator('#username');
  await expect(username).toBeVisible();
  await expect(page.locator('.register-btn')).toBeVisible();
  await username.fill('TestUser123');
  await page.locator('.register-btn').click();
  await expect(page.locator('#test-section')).toBeVisible();
  await expect(page.locator('#user-info')).toContainText('TestUser123');
});

// py: tests/test_user_journey_selenium.py::test_registration_form_validation
test('an empty name keeps the registration form', async ({ page, launch }) => {
  const server = await launch();
  await page.goto(server.url + '/');
  const username = page.locator('#username');
  await expect(username).toBeVisible();
  await page.locator('.register-btn').click();
  await expect(username).toBeVisible();
  await expect(page.locator('#registration')).toBeVisible();
  await expect(page.locator('#registration')).not.toHaveClass(/hidden/);
});

// py: tests/test_user_journey_selenium.py::test_complete_quiz_journey
test('a whole quiz from registration to the results', async ({ page, launch }) => {
  const server = await launch({
    quizzes: defaultQuiz({
      title: 'Complete Journey Test',
      questions: [
        { question: 'What is 2 + 2?', options: ['3', '4', '5'], correct_answer: 1 },
        { question: 'What is the capital of France?', options: ['London', 'Paris', 'Berlin'], correct_answer: 1 },
      ],
    }),
  });
  await register(page, server.url, 'JourneyTester');
  await expect(page.locator('.question-text')).toHaveText('What is 2 + 2?');

  const four = await optionByText(page, '4');
  await four.click();
  await expectSelected(four);
  await submitAnswer(page);
  await waitForContinue(page);
  await expect(four).toHaveClass(/feedback-correct/);
  await clickContinue(page);

  await waitForQuestionText(page, 'capital');
  await (await optionByText(page, 'Paris')).click();
  await submitAnswer(page);
  await clickContinue(page);

  await expect(page.locator('#results')).toBeVisible();
  await expect(page.locator('#results')).toContainText('Результат:');
});

// py: tests/test_user_journey_selenium.py::test_question_selection_and_feedback
test('changing the selection, then feedback and disabled options', async ({ page, launch }) => {
  const server = await launch({
    quizzes: defaultQuiz({
      title: 'Feedback Test',
      questions: [{ question: 'Test question for feedback?', options: ['Wrong', 'Correct', 'Also Wrong'], correct_answer: 1 }],
    }),
  });
  await register(page, server.url, 'FeedbackTester');
  const wrong = await optionByText(page, 'Wrong');
  const correct = await optionByText(page, 'Correct');

  await wrong.click();
  await expectSelected(wrong);
  await expectSelected(correct, false);
  await correct.click();
  await expectSelected(correct);
  await expectSelected(wrong, false);

  await expect(page.locator('#submit-answer-btn')).toBeEnabled();
  await submitAnswer(page);
  await waitForContinue(page);
  await expect(correct).toHaveClass(/feedback-correct/);
  await expect(correct).toHaveClass(/(^|\s)disabled(\s|$)/);
});

// py: tests/test_user_journey_selenium.py::test_progress_bar_functionality
test('the progress bar and counter follow the questions', async ({ page, launch }) => {
  const server = await launch({
    quizzes: defaultQuiz({
      title: 'Progress Test',
      questions: [
        { question: 'Question 1?', options: ['A', 'B'], correct_answer: 0 },
        { question: 'Question 2?', options: ['X', 'Y'], correct_answer: 1 },
        { question: 'Question 3?', options: ['1', '2'], correct_answer: 0 },
      ],
    }),
  });
  await register(page, server.url, 'ProgressTester');
  const progress = page.locator('#progress-text');
  await expect(progress).toContainText('Питання 1 з 3');

  await optionByIndex(page, 0).click();
  await submitAnswer(page);
  await clickContinue(page);
  await expect(progress).toContainText('Питання 2 з 3', { timeout: 5000 });
  const width = (await page.locator('#progress-fill').getAttribute('style')) || '';
  expect(width.indexOf('66') !== -1 || width.indexOf('67') !== -1, `progress width ${width}`).toBe(true);

  await optionByIndex(page, 1).click();
  await submitAnswer(page);
  await clickContinue(page);
  await expect(progress).toContainText('Питання 3 з 3', { timeout: 5000 });
});

// py: tests/test_user_journey_selenium.py::test_theme_toggle_functionality
test('the theme button switches the theme and back', async ({ page, launch }) => {
  const server = await launch();
  await page.goto(server.url + '/');
  await expect(page.locator('#username')).toBeVisible();
  // The old page put data-theme on <body>; the new pages put it on <html>.
  const html = page.locator('html');
  const initial = await html.getAttribute('data-theme');
  await page.locator('.theme-toggle').click();
  await expect.poll(() => html.getAttribute('data-theme')).not.toBe(initial);
  await page.locator('.theme-toggle').click();
  await expect.poll(() => html.getAttribute('data-theme')).toBe(initial);
});

// py: tests/test_user_journey_selenium.py::test_session_persistence_on_reload
test('a reload continues the quiz of the same student', async ({ page, launch }) => {
  const server = await launch({
    quizzes: defaultQuiz({
      title: 'Session Test',
      questions: [
        { question: 'Session test question?', options: ['A', 'B', 'C'], correct_answer: 1 },
        { question: 'Second question?', options: ['X', 'Y', 'Z'], correct_answer: 0 },
      ],
    }),
  });
  await register(page, server.url, 'SessionTester');
  await (await optionByText(page, 'B')).click();
  await submitAnswer(page);
  await clickContinue(page);
  await waitForQuestionText(page, 'Second question?');

  await page.reload();
  // The quiz continues (or shows the results), without the registration form
  await expect(page.locator('#current-question-container .current-question, #results:not(.hidden)').first()).toBeVisible();
  await expect(page.locator('#registration')).toBeHidden();
  await expect(page.locator('#user-info')).toContainText('SessionTester');
});

// py: tests/test_user_journey_selenium.py::test_different_question_types
test('text, text with image and image-only questions', async ({ page, launch }) => {
  const server = await launch({
    quizzes: defaultQuiz({
      title: 'Mixed Question Types',
      questions: [
        { question: 'Text only question?', options: ['Text A', 'Text B'], correct_answer: 0 },
        { question: 'Question with image?', image: '/imgs/test.jpg', options: ['With image A', 'With image B'], correct_answer: 1 },
        { image: '/imgs/image-only.png', options: ['Image only A', 'Image only B'], correct_answer: 0 },
      ],
    }),
  });
  await register(page, server.url, 'TypeTester');
  await expect(page.locator('.question-text')).toHaveText('Text only question?');
  await (await optionByText(page, 'Text A')).click();
  await submitAnswer(page);
  await clickContinue(page);

  await waitForQuestionText(page, 'Question with image?');
  // The image file does not exist (as in the Python test), so only the element is checked.
  // Its address is relative, so it also works under the tunnel's path prefix.
  await expect(page.locator('.question-image')).toHaveAttribute('src', 'imgs/test.jpg');
  await (await optionByText(page, 'With image B')).click();
  await submitAnswer(page);
  await clickContinue(page);

  await expect(findOptions(page).filter({ hasText: 'Image only' }).first()).toBeVisible({ timeout: 5000 });
  await expect(page.locator('.question-text')).toHaveCount(0); // no text, only the image and options
  await expect(page.locator('.question-image')).toHaveAttribute('src', 'imgs/image-only.png');
  await (await optionByText(page, 'Image only A')).click();
  await submitAnswer(page);
  await clickContinue(page);

  await expect(page.locator('#results')).toContainText('3/3 (100%)');
  await expect(page.locator('#results')).toContainText('Результат:');
});

// py: tests/test_user_journey_selenium.py::test_button_state_management
test('submit and continue buttons change with the answer', async ({ page, launch }) => {
  const server = await launch({
    quizzes: defaultQuiz({
      title: 'Button Test',
      questions: [{ question: 'Button test question?', options: ['Option 1', 'Option 2'], correct_answer: 0 }],
    }),
  });
  await register(page, server.url, 'ButtonTester');
  const submit = page.locator('#submit-answer-btn');
  const next = page.locator('#continue-btn');
  await expect(submit).toBeDisabled();
  await expect(next).toBeHidden();

  await (await optionByText(page, 'Option 1')).click();
  await expect(submit).toBeEnabled();
  await submit.click();
  await waitForContinue(page);
  await expect(submit).toBeHidden();
  await expect(next).toBeVisible();
});

// py: tests/test_user_journey_selenium.py::test_results_display_accuracy
test('results count one right and one wrong answer', async ({ page, launch }) => {
  const server = await launch({
    quizzes: defaultQuiz({
      title: 'Results Accuracy Test',
      questions: [
        { question: 'Correct answer question?', options: ['Wrong', 'Right', 'Wrong2'], correct_answer: 1 },
        { question: 'Incorrect answer question?', options: ['Wrong', 'Right', 'Wrong2'], correct_answer: 1 },
      ],
    }),
  });
  await register(page, server.url, 'ResultsTester');
  await (await optionByText(page, 'Right')).click();
  await submitAnswer(page);
  await clickContinue(page);

  await waitForQuestionText(page, 'Incorrect answer question?');
  await (await optionByText(page, 'Wrong')).click();
  await submitAnswer(page);
  await clickContinue(page);

  const results = page.locator('#results');
  await expect(results).toContainText('1/2 (50%)');
  await expect(results).toContainText('Результат:');
  await expect(page.locator('.results-table')).toBeVisible();
  await expect(results).toContainText('✓');
  await expect(results).toContainText('✗');
});

// py: tests/test_user_journey_selenium.py::test_browser_navigation_behavior
test('back and forward keep the quiz working', async ({ page, launch }) => {
  const server = await launch({
    quizzes: defaultQuiz({
      title: 'Navigation Test',
      questions: [{ question: 'Navigation test?', options: ['A', 'B'], correct_answer: 0 }],
    }),
  });
  await register(page, server.url, 'NavTester');
  await page.goBack();
  if ((await page.locator('#current-question-container').count()) === 0) {
    await page.goForward();
  }
  await expect(page.locator('#current-question-container .current-question')).toBeVisible();

  await (await optionByText(page, 'A')).click();
  await submitAnswer(page);
  await clickContinue(page);
  await expect(page.locator('#results')).toBeVisible();
});

// py: tests/test_user_journey_selenium.py::test_show_right_answer_true_visual_feedback
test('show_right_answer true marks the wrong and the right answer', async ({ page, launch }) => {
  const server = await launch({
    quizzes: defaultQuiz({
      title: 'Show Answers Quiz',
      show_right_answer: true,
      questions: [{ question: 'What is 2 + 2?', options: ['3', '4', '5', '6'], correct_answer: 1 }],
    }),
  });
  await register(page, server.url, 'ShowAnswersTester');
  const wrong = await optionByText(page, '3');
  const right = await optionByText(page, '4');
  await wrong.click();
  await submitAnswer(page);
  await waitForContinue(page);
  await expect(wrong).toHaveClass(/feedback-incorrect/);
  await expect(right).toHaveClass(/feedback-correct/);

  await clickContinue(page);
  const results = page.locator('#results');
  await expect(results).toBeVisible();
  await expect(results).toContainText('Правильна:');
  await expect(results).toContainText('4');
});

// py: tests/test_user_journey_selenium.py::test_show_right_answer_false_visual_feedback
test('show_right_answer false: no right answer in the results', async ({ page, launch }) => {
  const server = await launch({
    quizzes: defaultQuiz({
      title: 'Hide Answers Quiz',
      show_right_answer: false,
      questions: [{ question: 'What is 3 + 3?', options: ['5', '6', '7', '8'], correct_answer: 1 }],
    }),
  });
  await register(page, server.url, 'HideAnswersTester');
  await (await optionByText(page, '5')).click();
  await submitAnswer(page);
  await expect(page.locator('#results')).toBeVisible({ timeout: 3000 });
  await expect(page.locator('#results-content')).not.toBeEmpty();
  expect(await page.locator('#results-content').innerHTML()).not.toContain('Правильна:');
  await expect(page.locator('#results')).toContainText(/0\/1|0%/);
});

// py: tests/test_user_journey_selenium.py::test_show_right_answer_false_correct_answer_visual_feedback
test('show_right_answer false and a right answer: score, no hints', async ({ page, launch }) => {
  const server = await launch({
    quizzes: defaultQuiz({
      title: 'Hide Answers Quiz',
      show_right_answer: false,
      questions: [{ question: 'What is 4 + 4?', options: ['6', '7', '8', '9'], correct_answer: 2 }],
    }),
  });
  await register(page, server.url, 'CorrectAnswerTester');
  await (await optionByText(page, '8')).click();
  await submitAnswer(page);
  await expect(page.locator('#results')).toBeVisible({ timeout: 3000 });
  await expect(page.locator('#results')).toContainText(/1\/1|100%/);
  expect(await page.locator('#results-content').innerHTML()).not.toContain('Правильна:');
});

// py: tests/test_user_journey_selenium.py::test_show_right_answer_multi_question_journey
test('show_right_answer false through three questions', async ({ page, launch }) => {
  const server = await launch({
    quizzes: defaultQuiz({
      title: 'Multi Question Hide Answers Quiz',
      show_right_answer: false,
      questions: [
        { question: 'What is 1 + 1?', options: ['1', '2', '3', '4'], correct_answer: 1 },
        { question: 'What is 2 * 3?', options: ['4', '5', '6', '7'], correct_answer: 2 },
        { question: 'What is 10 / 2?', options: ['3', '4', '5', '6'], correct_answer: 2 },
      ],
    }),
  });
  await register(page, server.url, 'MultiQuestionTester');
  await (await optionByText(page, '2')).click();
  await submitAnswer(page);

  await waitForQuestionText(page, 'What is 2 * 3?');
  await (await optionByText(page, '4')).click();
  await submitAnswer(page);

  await waitForQuestionText(page, 'What is 10 / 2?');
  await (await optionByText(page, '3')).click();
  await submitAnswer(page);

  const results = page.locator('#results');
  await expect(results).toBeVisible({ timeout: 10000 });
  await expect(results).toContainText('1/3');
  await expect(results).toContainText('33%');
  expect(await page.locator('#results-content').innerHTML()).not.toContain('Правильна:');
  await expect(page.locator('.results-table')).toBeVisible();
  // The Python test also found "✓" and "✗" in the page source, but those came from the
  // old page's inline script, not from the results: while right answers are hidden the
  // table has no correctness column (test_hidden_correctness_leaves_out_the_correct_column).
  // The table lists the student's own answers instead.
  const table = page.locator('.results-table');
  await expect(table).toContainText('What is 1 + 1?');
  await expect(table).toContainText('What is 10 / 2?');
  await expect(table).not.toContainText('✓');
});
