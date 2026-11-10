// The live stats page follows the class over its WebSocket: new students, answers,
// the two groups (in progress, completed) and a quiz switch. These are not ports of
// Python browser tests (Python tested the socket messages only); they check that the
// page uses those messages like the old page did.
import type { APIRequestContext } from '@playwright/test';
import { expect, test } from '../../fixtures/quiz';

const QUIZZES = {
  'a.yaml': {
    title: 'Quiz A',
    questions: [
      { question: 'First?', options: ['A', 'B'], correct_answer: 0 },
      { question: 'Second?', options: ['A', 'B'], correct_answer: 0, points: 2 },
    ],
  },
  'b.yaml': {
    title: 'Quiz B',
    questions: [{ question: 'Only B?', options: ['X', 'Y'], correct_answer: 1, points: 0.5 }],
  },
};

async function register(request: APIRequestContext, url: string, username: string): Promise<string> {
  const response = await request.post(`${url}/api/register`, { data: { username } });
  expect(response.status()).toBe(200);
  return (await response.json()).user_id;
}

async function answer(request: APIRequestContext, url: string, userId: string, questionId: number, selected: number) {
  await request.post(`${url}/api/question-start`, { data: { user_id: userId, question_id: questionId } });
  const response = await request.post(`${url}/api/submit-answer`, {
    data: { user_id: userId, question_id: questionId, selected_answer: selected },
  });
  expect(response.status()).toBe(200);
}

async function switchQuiz(request: APIRequestContext, url: string, filename: string) {
  expect((await request.post(`${url}/api/admin/auth`, { data: { master_key: 'test123' } })).status()).toBe(200);
  expect((await request.post(`${url}/api/admin/switch-quiz`, { data: { quiz_filename: filename } })).status()).toBe(200);
}

test('students, answers and groups update without a reload', async ({ page, request, launch }) => {
  const server = await launch({ quizzes: QUIZZES });
  await switchQuiz(request, server.url, 'a.yaml');
  await page.goto(server.url + '/live-stats/');
  await expect(page.locator('#noDataMessage')).toHaveText('Waiting for users to join the quiz...');
  await expect(page.locator('#countAll')).toHaveText('0');

  const anna = await register(request, server.url, 'Anna');
  const row = page.locator('#user-' + anna);
  await expect(row.locator('.username-text')).toHaveText('Anna');
  await expect(page.locator(`#cell-${anna}-1`)).toHaveClass(/state-think/);
  await expect(page.locator(`#cell-${anna}-2`)).toHaveClass(/state-empty/);
  await expect(row.locator('.user-stats')).toHaveText('0/3');

  await answer(request, server.url, anna, 1, 0);
  await expect(page.locator(`#cell-${anna}-1`)).toHaveClass(/state-ok/);
  await expect(page.locator(`#cell-${anna}-1 .state-icon`)).toHaveText('🟢');
  await expect(page.locator(`#cell-${anna}-1 .time-display`)).toHaveText(/^\d+s$/);
  await expect(row.locator('.user-stats')).toHaveText('1/3');
  await expect(page.locator('th .question-stats').first()).toHaveText('1/1');

  const bob = await register(request, server.url, 'Bob');
  await expect(page.locator('#countAll')).toHaveText('2');
  await expect(page.locator('#countInProgress')).toHaveText('2');

  // Anna finishes (with a wrong answer) and moves to the completed group
  await answer(request, server.url, anna, 2, 1);
  await expect(page.locator(`#cell-${anna}-2`)).toHaveClass(/state-fail/);
  await expect(page.locator('#countCompleted')).toHaveText('1');
  await expect(page.locator('#countInProgress')).toHaveText('1');

  await page.locator('#filterCompleted').click();
  await expect(page.locator('#filterCompleted')).toHaveClass(/active/);
  await expect(page.locator('#user-' + anna)).toBeVisible();
  await expect(page.locator('#user-' + bob)).toHaveCount(0);

  await page.locator('#filterInProgress').click();
  await expect(page.locator('#user-' + bob)).toBeVisible();
  await expect(page.locator('#user-' + anna)).toHaveCount(0);

  await answer(request, server.url, bob, 1, 0);
  await answer(request, server.url, bob, 2, 0);
  await expect(page.locator('#noDataMessage')).toHaveText('No users in this category yet.');

  await page.locator('#filterAll').click();
  await expect(page.locator('#statsBody tr')).toHaveCount(2);
});

test('a quiz switch empties the table and shows the new questions', async ({ page, request, launch }) => {
  const server = await launch({ quizzes: QUIZZES });
  await switchQuiz(request, server.url, 'a.yaml');
  await register(request, server.url, 'Anna');
  await page.goto(server.url + '/live-stats/');
  await expect(page.locator('.username-text')).toHaveText('Anna');

  await switchQuiz(request, server.url, 'b.yaml');
  await expect(page.locator('#noDataMessage')).toHaveText('Waiting for users to join the quiz...');

  const carl = await register(request, server.url, 'Carl');
  await expect(page.locator('th .question-text')).toHaveCount(1);
  await expect(page.locator('th .question-text')).toContainText('Only B?');
  await expect(page.locator('#user-' + carl + ' .user-stats')).toHaveText('0/0.5'); // the new quiz's total
});

test('the page says when the connection to the server is lost', async ({ page, launch }) => {
  const server = await launch({ quizzes: QUIZZES });
  await page.goto(server.url + '/live-stats/');
  await expect(page.locator('#noDataMessage')).toBeVisible();
  await expect(page.locator('#connectionStatus')).toBeHidden();

  await server.stop();
  await expect(page.locator('#connectionStatus')).toBeVisible();
  await expect(page.locator('#connectionStatus')).toHaveText(/Reconnecting in \d+s\.\.\. \(\d\/5\)/);
});
