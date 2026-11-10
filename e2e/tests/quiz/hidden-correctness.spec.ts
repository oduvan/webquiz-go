// Ports of tests/test_selenium_hidden_correctness.py: the results table leaves out the
// "Correct" column while the server hides which answers were right.
import { EN, expect, registerUser, test, waitForQuestionText } from '../../fixtures/quiz';
import type { Page } from '@playwright/test';

function quiz(settings: Record<string, unknown>) {
  return {
    'q.yaml': {
      title: 'Hidden',
      questions: [
        { question: 'First?', options: ['Right', 'Wrong'], correct_answer: 0 },
        { question: 'Second?', options: ['Right', 'Wrong'], correct_answer: 0 },
      ],
      ...settings,
    },
  };
}

async function answer(page: Page, index: number) {
  await page.locator(`#option_${index}`).click({ timeout: 5000 });
  await page.locator('#submit-answer-btn').click({ timeout: 5000 });
}

async function headers(page: Page): Promise<string[]> {
  const table = page.locator('.results-table');
  await expect(table).toBeVisible({ timeout: 10000 });
  return (await table.locator('th').allInnerTexts()).map((text) => text.trim());
}

// py: tests/test_selenium_hidden_correctness.py::test_hidden_correctness_leaves_out_the_correct_column
test('hidden correctness leaves out the Correct column', async ({ page, launch }) => {
  const server = await launch({ config: EN, quizzes: quiz({ show_right_answer: false }) });
  await registerUser(page, server.url);
  await waitForQuestionText(page, 'First?');
  await answer(page, 0);
  await waitForQuestionText(page, 'Second?');
  await answer(page, 1);

  expect(await headers(page)).not.toContain('Correct');
  const table = page.locator('.results-table');
  await expect(table).not.toContainText('unknown');
  await expect(table).toContainText('Right');
  await expect(table).toContainText('Wrong');
  await expect(page.locator('#results')).toContainText('Result: 1/2 (50%)');
});

// py: tests/test_selenium_hidden_correctness.py::test_shown_correctness_keeps_the_correct_column
test('shown correctness keeps the Correct column', async ({ page, launch }) => {
  const server = await launch({ config: EN, quizzes: quiz({ show_right_answer: true }) });
  await registerUser(page, server.url);
  await waitForQuestionText(page, 'First?');
  await answer(page, 0);
  await page.locator('#continue-btn').click({ timeout: 5000 });
  await waitForQuestionText(page, 'Second?');
  await answer(page, 1);
  await page.locator('#continue-btn').click({ timeout: 5000 });

  expect(await headers(page)).toContain('Correct');
  const table = page.locator('.results-table');
  await expect(table).toContainText('✓');
  await expect(table).toContainText('✗');
});
