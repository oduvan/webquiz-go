// The server sends the gzip copies of the page's scripts and styles to a browser that
// accepts gzip, and every browser asks for gzip by itself. The pages must load and
// work as before.
import type { Page, Response } from '@playwright/test';
import { adminLogin } from '../fixtures/helpers';
import { clickContinue, defaultQuiz, expect, optionByText, registerUser, submitAnswer, test } from '../fixtures/quiz';

// Keeps the answers to /assets/ requests, by address.
function recordAssets(page: Page): Map<string, Response> {
  const answers = new Map<string, Response>();
  page.on('response', (response) => {
    if (new URL(response.url()).pathname.indexOf('/assets/') === 0) answers.set(response.url(), response);
  });
  return answers;
}

// The full addresses of the page's main script and of its style sheets.
async function pageFiles(page: Page): Promise<string[]> {
  const script = await page.locator('script[type="module"][src]').getAttribute('src');
  const styles = await page
    .locator('link[rel="stylesheet"]')
    .evaluateAll((links) => links.map((link) => link.getAttribute('href') || ''));
  expect(script, 'the page has a main script').toBeTruthy();
  expect(styles.length, 'the page has style sheets').toBeGreaterThan(0);
  return [script!, ...styles].map((address) => new URL(address, page.url()).href);
}

async function expectGzipped(answers: Map<string, Response>, files: string[]) {
  for (const file of files) {
    const answer = answers.get(file);
    expect(answer, `${file} was loaded`).toBeTruthy();
    expect(answer!.status(), file).toBe(200);
    expect(await answer!.headerValue('content-encoding'), file).toBe('gzip');
  }
}

// The browser unpacked and read every style sheet: each one has rules.
async function expectStylesRead(page: Page) {
  const rules = await page.evaluate(() => Array.from(document.styleSheets, (sheet) => sheet.cssRules.length));
  expect(rules.length).toBeGreaterThan(0);
  for (const count of rules) expect(count).toBeGreaterThan(0);
}

test('the quiz page gets its script and styles gzipped and works', async ({ page, launch }) => {
  const server = await launch({
    quizzes: defaultQuiz({
      title: 'Gzip',
      questions: [{ question: 'What is 2 + 2?', options: ['3', '4', '5'], correct_answer: 1 }],
    }),
  });
  const answers = recordAssets(page);

  // The script drew the registration form and the first question
  await registerUser(page, server.url, 'Gzip student');
  await expectGzipped(answers, await pageFiles(page));
  await expectStylesRead(page);

  await (await optionByText(page, '4')).click();
  await submitAnswer(page);
  await clickContinue(page);
  await expect(page.locator('#results')).toContainText('1/1 (100%)');
});

test('the admin page gets its script and styles gzipped and works', async ({ page, launch }) => {
  const server = await launch();
  const answers = recordAssets(page);

  await adminLogin(page, server.url);
  await expect(page.locator('#admin-panel')).toBeVisible();
  await expect(page.locator('#quiz-select option').first()).toContainText('test_quiz.yaml');
  await expectGzipped(answers, await pageFiles(page));
  await expectStylesRead(page);
});
