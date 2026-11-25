// Ports of tests/test_selenium_manual_check.py (WebQuiz 1.x), the teacher's side:
// the review page (/review/), where answers to manual_check questions are marked
// right or wrong.
import type { APIRequestContext } from '@playwright/test';
import { EN, expect, hasHorizontalScroll, test } from '../../fixtures/quiz';

function quiz() {
  return {
    'manual.yaml': {
      title: 'Manual',
      show_right_answer: true,
      questions: [
        { question: 'Why is the sky blue?', manual_check: true, correct_value: 'Scattering', points: 2 },
        { question: '2 + 2?', options: ['3', '4'], correct_answer: 1 },
      ],
    },
  };
}

async function registerStudent(request: APIRequestContext, url: string, name: string): Promise<string> {
  const response = await request.post(url + '/api/register', { data: { username: name } });
  expect(response.status()).toBe(200);
  return (await response.json()).user_id;
}

async function submit(request: APIRequestContext, url: string, userId: string, questionId: number, answer: unknown) {
  const response = await request.post(url + '/api/submit-answer', {
    data: { user_id: userId, question_id: questionId, selected_answer: answer },
  });
  expect(response.status()).toBe(200);
}

test('the review page asks for the master key and takes a verdict', async ({ page, request, launch }) => {
  const server = await launch({ config: EN, quizzes: quiz() });
  const userId = await registerStudent(request, server.url, 'Hidden Name');
  await submit(request, server.url, userId, 1, 'Light scatters');
  await submit(request, server.url, userId, 2, 1);

  await page.goto(server.url + '/review/');
  await expect(page).toHaveTitle('✍️ Answer review');
  const key = page.locator('#master-key-input');
  await expect(key).toBeVisible();
  await key.fill('wrong');
  await page.locator('#login-button').click();
  await expect(page.locator('#login-error')).toBeVisible();
  await key.fill('test123');
  await key.press('Enter');

  const card = page.locator('#item-card');
  await expect(card).toBeVisible();
  await expect(card).toContainText('Why is the sky blue?');
  await expect(card).toContainText('Light scatters');
  await expect(card).toContainText('Scattering');
  await expect(card).toContainText('Points: 2');
  expect(await page.content()).not.toContain('Hidden Name');
  await expect(page.locator('#pending-count')).toContainText('Waiting for a check: 1');

  await page.locator('#verdict-incorrect').click();
  const idle = page.locator('#idle');
  await expect(idle).toBeVisible();
  await expect(idle).toContainText('No answers to check');
  await expect(page.locator('#pending-count')).toContainText('Waiting for a check: 0');

  const results = (await (await request.get(server.url + '/api/verify-user/' + userId)).json()).final_results;
  expect(results.test_results[0].is_correct).toBe(false);
  expect(results.pending_count).toBe(0);
});

test('a logged-in admin goes straight to the answers; a new answer comes by itself', async ({ page, request, launch }) => {
  const server = await launch({ config: EN, quizzes: quiz() });
  const login = await page.request.post(server.url + '/api/admin/auth', { data: { master_key: 'test123' } });
  expect(login.status()).toBe(200);
  await page.goto(server.url + '/review/');
  await expect(page.locator('#idle')).toContainText('No answers to check');
  await expect(page.locator('#master-key-input')).toHaveCount(0);

  await submit(request, server.url, await registerStudent(request, server.url, 'Anna'), 1, 'Because');
  await expect(page.locator('#item-answer')).toHaveText('Because');
  await page.locator('#verdict-correct').click();
  await expect(page.locator('#idle')).toBeVisible();
});

test('two teachers: the second one can help with the same answer, the first verdict counts', async ({ page, browser, request, launch }) => {
  const server = await launch({ config: EN, quizzes: quiz() });
  await page.request.post(server.url + '/api/admin/auth', { data: { master_key: 'test123' } });
  await page.goto(server.url + '/review/');
  await expect(page.locator('#idle')).toBeVisible();
  await submit(request, server.url, await registerStudent(request, server.url, 'Anna'), 1, 'Because');
  await expect(page.locator('#item-answer')).toHaveText('Because');

  const other = await (await browser.newContext()).newPage();
  await other.request.post(server.url + '/api/admin/auth', { data: { master_key: 'test123' } });
  await other.goto(server.url + '/review/');
  // The only answer is open on the first page: the second page offers help
  await expect(other.locator('#help-text')).toContainText('Answers open on other teachers\' pages: 1');
  await other.locator('#help-button').click();
  await expect(other.locator('#item-answer')).toHaveText('Because');

  await page.locator('#verdict-correct').click();
  await expect(page.locator('#idle')).toBeVisible();
  // The other page moves on by itself
  await expect(other.locator('#idle')).toContainText('No answers to check');
  await other.context().close();
});

test('the review page fits a phone', async ({ page, request, launch }) => {
  await page.setViewportSize({ width: 375, height: 800 });
  const server = await launch({ config: { ...EN, admin: { trusted_ips: ['127.0.0.1'] } }, quizzes: quiz() });
  await submit(request, server.url, await registerStudent(request, server.url, 'Anna'), 1, 'A long answer '.repeat(20));
  await page.goto(server.url + '/review/');
  await expect(page.locator('#item-card')).toBeVisible();
  expect(await hasHorizontalScroll(page)).toBe(false);
  for (const id of ['#verdict-correct', '#verdict-incorrect']) {
    const button = page.locator(id);
    await expect(button).toBeVisible();
    const box = (await button.boundingBox())!;
    expect(box.x).toBeGreaterThanOrEqual(0);
    expect(box.x + box.width).toBeLessThanOrEqual(375);
    expect(box.height).toBeGreaterThanOrEqual(60); // big enough for a finger
  }
  // On a phone the two buttons stand one above the other
  const right = (await page.locator('#verdict-correct').boundingBox())!;
  const wrong = (await page.locator('#verdict-incorrect').boundingBox())!;
  expect(wrong.y).toBeGreaterThan(right.y);
});
