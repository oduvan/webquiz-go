// The quiz page on a phone (375 px): registration, a question with long text, image
// options and a timer, the feedback, and the results never make the page scroll sideways.
import { writeFileSync } from 'node:fs';
import path from 'node:path';
import { defaultQuiz, expect, hasHorizontalScroll, test } from '../../fixtures/quiz';

const PNG = Buffer.from(
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==',
  'base64',
);
const LONG_WORD = 'Pneumonoultramicroscopicsilicovolcanoconiosis_and_even_longer_than_that';

test('the quiz page fits a 375 px screen', async ({ page, launch }) => {
  await page.setViewportSize({ width: 375, height: 800 });
  const server = await launch({
    config: { registration: { fields: ['School with a long name'] } },
    quizzes: defaultQuiz({
      title: 'A quiz with a long title for a small phone screen',
      show_right_answer: true,
      time_limit: { per_question: 120 },
      questions: [
        {
          question: 'A long question ' + LONG_WORD + ' that must wrap on a phone?',
          options: ['Option ' + LONG_WORD, 'Short'],
          correct_answer: 1,
          points: 2,
        },
        { question: 'Which image?', options: ['/imgs/a.png', '/imgs/b.png'], correct_answer: 0 },
      ],
    }),
  });
  for (const name of ['a.png', 'b.png']) {
    writeFileSync(path.join(server.dir, 'quizzes', 'imgs', name), PNG);
  }

  await page.goto(server.url + '/');
  await expect(page.locator('#username')).toBeVisible();
  expect(await hasHorizontalScroll(page)).toBe(false);
  const usernameBox = await page.locator('#username').boundingBox();
  expect(usernameBox!.x + usernameBox!.width).toBeLessThanOrEqual(375);

  await page.locator('#username').fill('A student with a really long name');
  await page.locator('[data-field-name="school_with_a_long_name"]').fill('School 7');
  await page.locator('.register-btn').click();
  await expect(page.locator('#question-timer')).toHaveText(/^\d:\d\d$/);
  await expect(page.locator('.question-points-badge')).toBeVisible();
  expect(await hasHorizontalScroll(page)).toBe(false);

  await page.locator('#option_0').click();
  await page.locator('#submit-answer-btn').click();
  await expect(page.locator('#option_0')).toHaveClass(/feedback-incorrect/);
  expect(await hasHorizontalScroll(page)).toBe(false);
  await page.locator('#continue-btn').click();

  await expect(page.locator('.option-image').first()).toBeVisible();
  expect(await hasHorizontalScroll(page)).toBe(false);
  await page.locator('#option_0').click();
  await page.locator('#submit-answer-btn').click();
  await page.locator('#continue-btn').click();

  await expect(page.locator('.results-table')).toBeVisible();
  expect(await hasHorizontalScroll(page)).toBe(false);
  // On a phone every row is a card with the column names
  const firstCell = page.locator('.results-table td').first();
  expect(await firstCell.evaluate((element) => getComputedStyle(element, '::before').content)).toContain('Питання');
});
