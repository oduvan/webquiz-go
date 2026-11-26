// Ports of tests/test_selenium_manual_check.py (WebQuiz 1.x), the student's side:
// a text question that the teacher checks by hand (manual_check: true).
import type { Browser, Page } from '@playwright/test';
import { EN, expect, optionByIndex, registerUser, test, waitForQuestionText } from '../../fixtures/quiz';

function quiz(settings: Record<string, unknown> = {}) {
  return {
    'manual.yaml': {
      title: 'Manual',
      show_right_answer: true,
      questions: [
        { question: 'Why is the sky blue?', manual_check: true, correct_value: 'Scattering', points: 2 },
        { question: '2 + 2?', options: ['3', '4'], correct_answer: 1 },
      ],
      ...settings,
    },
  };
}

async function answerManualThenChoice(page: Page) {
  await waitForQuestionText(page, 'Why is the sky blue?');
  await page.locator('#text-answer-input').fill('Light scatters');
  await page.locator('#submit-answer-btn').click();
  const message = page.locator('#checker-feedback .pending-review-message');
  await expect(message).toBeVisible();
  await expect(message).toContainText('It will be checked manually');
  // The student page never mentions a teacher
  expect((await page.locator('body').innerText()).toLowerCase()).not.toContain('teacher');
  // No right or wrong yet
  await expect(page.locator('#answer-verdict')).toHaveCount(0);
  await page.locator('#continue-btn').click();
  await waitForQuestionText(page, '2 + 2?');
  await optionByIndex(page, 1).click();
  await page.locator('#submit-answer-btn').click();
  await page.locator('#continue-btn').click();
}

// Acts as a teacher in a browser of its own: opens the review page and gives a verdict.
async function teacherDecides(browser: Browser, url: string, verdict: 'correct' | 'incorrect') {
  const context = await browser.newContext();
  const page = await context.newPage();
  const login = await page.request.post(url + '/api/admin/auth', { data: { master_key: 'test123' } });
  expect(login.status()).toBe(200);
  await page.goto(url + '/review/');
  await expect(page.locator('#item-card')).toBeVisible();
  await page.locator('#verdict-' + verdict).click();
  await expect(page.locator('#idle')).toBeVisible();
  await context.close();
}

test('the results wait for the teacher and update by themselves', async ({ page, browser, launch }) => {
  const server = await launch({ config: EN, quizzes: quiz() });
  await registerUser(page, server.url, 'Student');
  await answerManualThenChoice(page);

  const notice = page.locator('#pending-review-notice');
  await expect(notice).toBeVisible({ timeout: 10000 });
  await expect(notice).toContainText('Answers waiting for manual verification: 1');
  const waitingRows = page.locator('.results-table tr.pending-review-row');
  await expect(waitingRows).toHaveCount(1);
  await expect(waitingRows.first()).toContainText('Waiting for manual verification');
  expect((await page.locator('#results').innerText()).toLowerCase()).not.toContain('teacher');

  await teacherDecides(browser, server.url, 'correct');

  const done = page.locator('#pending-review-done');
  await expect(done).toBeVisible({ timeout: 12000 });
  await expect(done).toContainText('All your answers have been verified');
  await expect(waitingRows).toHaveCount(0);
  await expect(page.locator('#results-content')).toContainText('2/2');
});

test('without right answers the page moves on after "it will be checked manually"', async ({ page, launch }) => {
  const server = await launch({ config: EN, quizzes: quiz({ show_right_answer: false }) });
  await registerUser(page, server.url, 'Student');
  await waitForQuestionText(page, 'Why is the sky blue?');
  await page.locator('#text-answer-input').fill('Light scatters');
  await page.locator('#submit-answer-btn').click();
  await expect(page.locator('#checker-feedback .pending-review-message')).toBeVisible();
  await expect(page.locator('#continue-btn')).toBeHidden();
  await waitForQuestionText(page, '2 + 2?');
});

test('a waiting answer on a phone: the results screen fits 375 px', async ({ page, launch }) => {
  await page.setViewportSize({ width: 375, height: 800 });
  const server = await launch({ config: EN, quizzes: quiz() });
  await registerUser(page, server.url, 'Student');
  await answerManualThenChoice(page);
  await expect(page.locator('#pending-review-notice')).toBeVisible({ timeout: 10000 });
  await expect(page.locator('.results-table tr.pending-review-row')).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
});
