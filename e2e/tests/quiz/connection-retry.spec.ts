// Ports of tests/test_selenium_connection_retry.py: requests are retried while the
// connection is down. A fetch wrapper in the page plays the connection: with
// __networkDown every request fails, and __dropResponses lets the server handle a
// request but loses the response, the case where a retry must not record anything twice.
import type { Page } from '@playwright/test';
import { cookieValue, defaultQuiz, expect, findOptions, registerUser, test, waitForContinue, waitForQuestionText } from '../../fixtures/quiz';

const TWO_QUESTIONS = defaultQuiz({
  title: 'Retry Quiz',
  questions: [
    { question: 'First question', options: ['A', 'B'], correct_answer: 0 },
    { question: 'Second question', options: ['C', 'D'], correct_answer: 1 },
  ],
});

function fetchPatch(networkDown: boolean): string {
  return `
    window.__realFetch = window.__realFetch || window.fetch;
    window.__networkDown = ${JSON.stringify(networkDown)};
    window.__dropResponses = {};
    window.fetch = async function(url, options) {
      if (window.__networkDown) {
        throw new TypeError('Failed to fetch');
      }
      for (const part in window.__dropResponses) {
        if (String(url).includes(part) && window.__dropResponses[part] > 0) {
          window.__dropResponses[part]--;
          await window.__realFetch(url, options);
          throw new TypeError('Failed to fetch');
        }
      }
      return window.__realFetch(url, options);
    };
  `;
}

async function installFetchPatch(page: Page, networkDown = false) {
  await page.evaluate(fetchPatch(networkDown));
}

// The wrapper runs before the page's own scripts, for requests sent while the page loads.
async function installFetchPatchOnLoad(page: Page, networkDown = false) {
  await page.addInitScript(fetchPatch(networkDown));
}

const banner = (page: Page) => page.locator('#connection-banner');

// py: tests/test_selenium_connection_retry.py::test_lost_answer_response_is_retried_without_duplicate
test('a lost answer response is retried and counted once', async ({ page, context, request, launch }) => {
  const server = await launch({ quizzes: TWO_QUESTIONS });
  await registerUser(page, server.url);
  await installFetchPatch(page);
  await page.evaluate(() => ((window as unknown as { __dropResponses: Record<string, number> }).__dropResponses['api/submit-answer'] = 1));

  await findOptions(page).nth(0).click();
  await page.locator('#submit-answer-btn').click();

  // The retry gets the stored result and the page shows feedback as usual
  await waitForContinue(page, 10000);
  await page.locator('#continue-btn').click();
  await waitForQuestionText(page, 'Second question');

  await findOptions(page).nth(1).click();
  await page.locator('#submit-answer-btn').click();
  await page.locator('#continue-btn').click();
  await expect(page.locator('#results-content')).toBeVisible();
  await expect(banner(page)).toBeHidden({ timeout: 10000 });

  const userId = await cookieValue(context, 'user_id');
  const data = await (await request.get(`${server.url}/api/verify-user/${userId}`)).json();
  expect(data.final_results, 'the quiz should be completed').toBeTruthy();
  expect(data.final_results.total_count).toBe(2);
  expect(data.final_results.correct_count).toBe(2);
});

// py: tests/test_selenium_connection_retry.py::test_offline_submit_shows_banner_and_keeps_answer
test('offline: the banner shows and the chosen answer is kept and sent later', async ({ page, context, request, launch }) => {
  const server = await launch({ quizzes: TWO_QUESTIONS });
  await registerUser(page, server.url);
  await installFetchPatch(page);

  const options = findOptions(page);
  await options.nth(0).click();
  await page.evaluate(() => ((window as unknown as { __networkDown: boolean }).__networkDown = true));
  await page.locator('#submit-answer-btn').click();

  await expect(banner(page)).toBeVisible({ timeout: 5000 });
  await expect(page.locator('#submit-answer-btn')).toBeDisabled();

  // The answer is locked while the page retries
  await options.nth(1).click();
  await expect(options.nth(0)).toHaveClass(/(^|\s)selected(\s|$)/);
  await expect(options.nth(1)).not.toHaveClass(/(^|\s)selected(\s|$)/);

  await page.waitForTimeout(1500);
  await page.evaluate(() => ((window as unknown as { __networkDown: boolean }).__networkDown = false));
  await waitForContinue(page, 10000);
  await expect(banner(page)).toBeHidden();
  await expect(options.nth(0)).toHaveClass(/feedback-correct/);

  const userId = await cookieValue(context, 'user_id');
  const data = await (await request.get(`${server.url}/api/verify-user/${userId}`)).json();
  expect(data.next_question_index).toBe(1);
});

// py: tests/test_selenium_connection_retry.py::test_reload_while_offline_waits_for_server
test('a page loaded while offline waits instead of showing the registration form', async ({ page, launch }) => {
  const server = await launch({ quizzes: TWO_QUESTIONS });
  await registerUser(page, server.url);
  await installFetchPatchOnLoad(page, true);
  await page.reload();

  await expect(banner(page)).toBeVisible({ timeout: 5000 });
  await page.waitForTimeout(1500);
  await expect(page.locator('#loading')).toBeVisible();
  await expect(page.locator('#registration')).toBeHidden();

  await page.evaluate(() => ((window as unknown as { __networkDown: boolean }).__networkDown = false));
  await waitForQuestionText(page, 'First question', 10000);
  await expect(banner(page)).toBeHidden({ timeout: 10000 });
  await expect(page.locator('#registration')).toBeHidden();
});

// py: tests/test_selenium_connection_retry.py::test_lost_registration_response_starts_quiz
test('a lost registration response: the retry returns the same student', async ({ page, launch }) => {
  const server = await launch({ quizzes: TWO_QUESTIONS });
  await page.goto(server.url + '/');
  await page.locator('#username').fill('Anna');
  await installFetchPatch(page);
  await page.evaluate(() => ((window as unknown as { __dropResponses: Record<string, number> }).__dropResponses['api/register'] = 1));

  await page.locator('.register-btn').click();

  await waitForQuestionText(page, 'First question', 10000);
  await expect(page.locator('#registration-error')).toBeHidden();
  await expect(page.locator('#username-display')).toHaveText('Anna');
});
