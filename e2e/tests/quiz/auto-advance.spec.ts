// Ports of tests/test_auto_advance.py: with show_right_answer: false the next question
// comes by itself after an answer; with true the student presses "Continue".
import { defaultQuiz, expect, optionByText, test, waitForContinue, waitForQuestionText } from '../../fixtures/quiz';
import type { Page } from '@playwright/test';

async function register(page: Page, url: string, name: string) {
  await page.goto(url + '/');
  await page.locator('#username').fill(name);
  await page.locator('.register-btn').click();
  await expect(page.locator('#current-question-container .current-question')).toBeVisible();
}

async function answer(page: Page, text: string) {
  await (await optionByText(page, text)).click();
  await page.locator('#submit-answer-btn').click();
}

// py: tests/test_auto_advance.py::test_auto_advance_with_show_right_answer_false
test('the next question comes by itself', async ({ page, launch }) => {
  const server = await launch({
    quizzes: defaultQuiz({
      title: 'Auto Advance Quiz',
      show_right_answer: false,
      questions: [
        { question: 'Question 1?', options: ['A', 'B', 'C'], correct_answer: 1 },
        { question: 'Question 2?', options: ['X', 'Y', 'Z'], correct_answer: 0 },
      ],
    }),
  });
  await register(page, server.url, 'AutoAdvanceTester');
  await expect(page.locator('.question-text')).toHaveText('Question 1?');
  await answer(page, 'A');
  await expect(page.locator('#continue-btn')).toBeHidden();

  await waitForQuestionText(page, 'Question 2?');
  await expect(page.locator('.question-text')).toContainText('Question 2?');
  await answer(page, 'X');
  await expect(page.locator('#results')).toBeVisible({ timeout: 3000 });
  await expect(page.locator('#results')).toContainText('Результат:');
});

// py: tests/test_auto_advance.py::test_manual_continue_with_show_right_answer_true
test('with show_right_answer true the student presses Continue', async ({ page, launch }) => {
  const server = await launch({
    quizzes: defaultQuiz({
      title: 'Manual Continue Quiz',
      show_right_answer: true,
      questions: [
        { question: 'Question 1?', options: ['A', 'B', 'C'], correct_answer: 1 },
        { question: 'Question 2?', options: ['X', 'Y', 'Z'], correct_answer: 0 },
      ],
    }),
  });
  await register(page, server.url, 'ManualContinueTester');
  await expect(page.locator('.question-text')).toHaveText('Question 1?');
  await answer(page, 'A');
  await waitForContinue(page, 3000);
  await expect(page.locator('#continue-btn')).not.toHaveClass(/hidden/);
  await expect(page.locator('.quiz-option[class*="feedback-"]').first()).toBeVisible();

  await page.waitForTimeout(1500); // no auto-advance meanwhile
  await expect(page.locator('.question-text')).toContainText('Question 1?');

  await page.locator('#continue-btn').click();
  await waitForQuestionText(page, 'Question 2?');
});

// py: tests/test_auto_advance.py::test_auto_advance_multiple_questions
test('three questions one after the other', async ({ page, launch }) => {
  const server = await launch({
    quizzes: defaultQuiz({
      title: 'Multi Question Auto Advance',
      show_right_answer: false,
      questions: [
        { question: 'Q1?', options: ['A', 'B'], correct_answer: 0 },
        { question: 'Q2?', options: ['C', 'D'], correct_answer: 1 },
        { question: 'Q3?', options: ['E', 'F'], correct_answer: 0 },
      ],
    }),
  });
  await register(page, server.url, 'MultiAutoTester');
  await expect(page.locator('.question-text')).toHaveText('Q1?');
  await answer(page, 'A');
  await waitForQuestionText(page, 'Q2?');
  await answer(page, 'C');
  await waitForQuestionText(page, 'Q3?');
  await answer(page, 'E');
  await expect(page.locator('#results')).toBeVisible({ timeout: 3000 });
  await expect(page.locator('#results')).toContainText('Результат:');
});

// py: tests/test_auto_advance.py::test_auto_advance_no_visual_feedback
test('a wrong answer moves on without feedback', async ({ page, launch }) => {
  const server = await launch({
    quizzes: defaultQuiz({
      title: 'No Feedback Auto Advance',
      show_right_answer: false,
      questions: [
        { question: 'Question?', options: ['Wrong', 'Right'], correct_answer: 1 },
        { question: 'Next?', options: ['A', 'B'], correct_answer: 0 },
      ],
    }),
  });
  await register(page, server.url, 'NoFeedbackTester');
  await optionByText(page, 'Right'); // both options are there
  await answer(page, 'Wrong');
  await waitForQuestionText(page, 'Next?');
  await expect(page.locator('.question-text')).toContainText('Next?');
});

// py: tests/test_auto_advance.py::test_auto_advance_to_results_last_question
test('the last question goes straight to the results', async ({ page, launch }) => {
  const server = await launch({
    quizzes: defaultQuiz({
      title: 'Last Question Auto Advance',
      show_right_answer: false,
      questions: [{ question: 'Only Question?', options: ['A', 'B'], correct_answer: 0 }],
    }),
  });
  await register(page, server.url, 'LastQuestionTester');
  await answer(page, 'A');
  await expect(page.locator('#results')).toBeVisible({ timeout: 3000 });
  await expect(page.locator('#results')).toContainText('Результат:');
  await expect(page.locator('#continue-btn')).toBeHidden();
});

// py: tests/test_auto_advance.py::test_button_state_during_auto_advance
test('the next question starts with a disabled submit button', async ({ page, launch }) => {
  const server = await launch({
    quizzes: defaultQuiz({
      title: 'Button State Test',
      show_right_answer: false,
      questions: [
        { question: 'First?', options: ['A', 'B'], correct_answer: 0 },
        { question: 'Second?', options: ['C', 'D'], correct_answer: 1 },
      ],
    }),
  });
  await register(page, server.url, 'ButtonStateTester');
  await answer(page, 'A');
  await waitForQuestionText(page, 'Second?');
  const submit = page.locator('#submit-answer-btn');
  await expect(submit).toBeVisible();
  await expect(submit).not.toHaveClass(/hidden/);
  await expect(submit).toBeDisabled();
});
