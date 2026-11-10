// Flows of the old quiz page that the Python browser tests did not cover: approval
// with "Check", answers shown after everyone finished, checker messages, image
// preloading, the "choose a quiz" page, copy blocking, double-click guards and the
// saved registration values.
import type { APIRequestContext } from '@playwright/test';
import { writeFileSync } from 'node:fs';
import path from 'node:path';
import { cookieValue, defaultQuiz, EN, expect, registerUser, test, waitForQuestionNumber } from '../../fixtures/quiz';

const PNG = Buffer.from(
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==',
  'base64',
);

async function adminRequest(request: APIRequestContext, url: string) {
  expect((await request.post(`${url}/api/admin/auth`, { data: { master_key: 'test123' } })).status()).toBe(200);
}

test('approval: the student waits, saves changes, checks, then starts', async ({ page, context, request, launch }) => {
  const server = await launch({
    config: { ...EN, registration: { approve: true, fields: ['Group'] } },
    quizzes: defaultQuiz({ title: 'Approval', questions: [{ question: 'Q1?', options: ['A', 'B'], correct_answer: 0 }] }),
  });
  await page.goto(server.url + '/');
  await page.locator('#username').fill('Anna');
  await page.locator('[data-field-name="group"]').fill('10-A');
  await page.locator('.register-btn').click();

  await expect(page.locator('#waiting-approval')).toBeVisible();
  await expect(page.locator('#registration')).toBeHidden();
  await expect(page.locator('#waiting-username')).toHaveValue('Anna');
  await expect(page.locator('.waiting-field[data-field-name="group"]')).toHaveValue('10-A');
  await expect(page.locator('#user-info')).toContainText('Anna');

  await page.locator('#waiting-username').fill('Anna K');
  await page.locator('.save-registration-btn').click();
  await expect(page.locator('#waiting-error')).toHaveText('✓ Data updated successfully!');
  await expect(page.locator('#username-display')).toHaveText('Anna K');

  await page.locator('.check-status-btn').click();
  await expect(page.locator('#waiting-error')).toHaveText('You have not been approved yet. Please wait.');

  const userId = await cookieValue(context, 'user_id');
  await adminRequest(request, server.url);
  expect((await request.put(`${server.url}/api/admin/approve-user`, { data: { user_id: userId } })).status()).toBe(200);

  await page.locator('.check-status-btn').click();
  await expect(page.locator('.question-text')).toHaveText('Q1?');
  await expect(page.locator('#waiting-approval')).toBeHidden();

  // A reload of an approved student continues the quiz
  await page.reload();
  await expect(page.locator('.question-text')).toHaveText('Q1?');
});

test('a waiting student keeps waiting after a reload', async ({ page, launch }) => {
  const server = await launch({
    config: { ...EN, registration: { approve: true } },
    quizzes: defaultQuiz({ title: 'Approval', questions: [{ question: 'Q1?', options: ['A', 'B'], correct_answer: 0 }] }),
  });
  await page.goto(server.url + '/');
  await page.locator('#username').fill('Bob');
  await page.locator('.register-btn').click();
  await expect(page.locator('#waiting-approval')).toBeVisible();
  await page.reload();
  await expect(page.locator('#waiting-approval')).toBeVisible();
  await expect(page.locator('#waiting-username')).toHaveValue('Bob');
  await expect(page.locator('#loading')).toBeHidden();
});

test('answers shown on completion: the student waits for the class, then reloads', async ({ page, request, launch }) => {
  const server = await launch({
    config: EN,
    quizzes: defaultQuiz({
      title: 'Later',
      show_right_answer: false,
      show_answers_on_completion: true,
      questions: [{ question: 'Q1?', options: ['Right', 'Wrong'], correct_answer: 0 }],
    }),
  });
  // Another student has not finished yet
  const other = (await (await request.post(`${server.url}/api/register`, { data: { username: 'Other' } })).json()).user_id;

  await registerUser(page, server.url, 'Anna');
  await page.locator('#option_1').click();
  await page.locator('#submit-answer-btn').click();
  const results = page.locator('#results');
  await expect(results).toContainText('Correct answers will be available after all students complete the quiz.');
  await expect(results).toContainText('Reload the page later to see the correct answers.');
  await expect(page.locator('.results-table th')).not.toContainText(['Correct']);

  await request.post(`${server.url}/api/submit-answer`, { data: { user_id: other, question_id: 1, selected_answer: 0 } });
  await page.locator('.reload-btn').click();
  await expect(page.locator('.results-table')).toContainText('✗');
  await expect(page.locator('.results-table th', { hasText: 'Correct' })).toHaveCount(1);
  await expect(results).toContainText('Correct: Right');
  await expect(results).not.toContainText('Correct answers will be available');
});

const NO_LIST_QUIZ = defaultQuiz({
  title: 'No list',
  show_right_answer: true,
  show_final_list: false,
  questions: [
    { question: 'First question?', options: ['Right', 'Wrong'], correct_answer: 0, points: 2 },
    { question: 'Second question?', options: ['Right', 'Wrong'], correct_answer: 0 },
  ],
});

test('show_final_list false: the results show only the score, no list of questions', async ({ page, launch }) => {
  const server = await launch({ config: EN, quizzes: NO_LIST_QUIZ });
  await registerUser(page, server.url);
  await page.locator('#option_0').click();
  await page.locator('#submit-answer-btn').click();
  await page.locator('#continue-btn').click();
  await expect(page.locator('.question-text')).toHaveText('Second question?');
  await page.locator('#option_1').click();
  await page.locator('#submit-answer-btn').click();
  await page.locator('#continue-btn').click();

  const results = page.locator('#results');
  await expect(results).toContainText('Result: 1/2 (50%)');
  await expect(results).toContainText('Points: 2/3 (67%)');
  await expect(page.locator('.results-table')).toHaveCount(0);
  await expect(results).not.toContainText('First question?');

  // The same after a reload, when the results come straight from the server
  await page.reload();
  await expect(results).toContainText('Result: 1/2 (50%)');
  await expect(page.locator('.results-table')).toHaveCount(0);
});

test("show_final_list false also applies to the page's own results", async ({ page, launch }) => {
  const server = await launch({ config: EN, quizzes: NO_LIST_QUIZ });
  await registerUser(page, server.url);
  await page.locator('#option_0').click();
  await page.locator('#submit-answer-btn').click();
  await page.locator('#continue-btn').click();
  await expect(page.locator('.question-text')).toHaveText('Second question?');
  await page.locator('#option_1').click();
  await page.locator('#submit-answer-btn').click();
  await expect(page.locator('#continue-btn')).toBeVisible();
  // The server's results cannot be read: the page shows what it counted itself
  await page.route('**/api/verify-user/**', (route) => route.fulfill({ status: 500, body: 'oops' }));
  await page.locator('#continue-btn').click();

  await expect(page.locator('#results')).toContainText('Result: 1/2 (50%)');
  await expect(page.locator('.results-table')).toHaveCount(0);
});

test('a text answer: submit follows the text, a wrong answer shows the checker message', async ({ page, launch }) => {
  const server = await launch({
    config: EN,
    quizzes: defaultQuiz({
      title: 'Text',
      questions: [
        { question: 'Type 42', checker: "assert user_answer.strip() == '42', 'The answer is not 42'", correct_value: '42', default_value: '' },
      ],
    }),
  });
  await registerUser(page, server.url);
  const submit = page.locator('#submit-answer-btn');
  const input = page.locator('#text-answer-input');
  await expect(submit).toBeDisabled();
  await input.fill('41');
  await expect(submit).toBeEnabled();
  await input.fill('   ');
  await expect(submit).toBeDisabled();
  await input.fill('41');
  await submit.click();

  await expect(page.locator('#checker-feedback .checker-error')).toContainText('The answer is not 42');
  await expect(page.locator('#checker-feedback .correct-value-display')).toContainText('42');
  await expect(input).toBeDisabled();
  await expect(input).toHaveClass(/feedback-incorrect/);
  await page.locator('#continue-btn').click();
  await expect(page.locator('#results')).toContainText('Result: 0/1 (0%)');
});

test('quiz images are loaded while the student registers', async ({ page, launch }) => {
  const server = await launch({
    quizzes: defaultQuiz({
      title: 'Images',
      questions: [
        { question: 'Q1?', image: '/imgs/q.png', options: ['A', 'B'], correct_answer: 0 },
        { question: 'Q2?', options: ['/imgs/a.png', '/imgs/b.png'], correct_answer: 0 },
      ],
    }),
  });
  for (const name of ['q.png', 'a.png', 'b.png']) {
    writeFileSync(path.join(server.dir, 'quizzes', 'imgs', name), PNG);
  }
  const requested: string[] = [];
  page.on('request', (request) => requested.push(request.url()));
  await page.goto(server.url + '/');
  await expect(page.locator('#username')).toBeVisible();
  for (const name of ['q.png', 'a.png', 'b.png']) {
    await expect.poll(() => requested.indexOf(`${server.url}/imgs/${name}`) !== -1).toBe(true);
  }
});

test('several quiz files and no default.yaml: the page asks to choose a quiz', async ({ page, launch }) => {
  const server = await launch({
    quizzes: {
      'a.yaml': { title: 'Quiz <A>', questions: [{ question: 'Q?', options: ['A', 'B'], correct_answer: 0 }] },
      'b.yaml': { questions: [{ question: 'Q?', options: ['A', 'B'], correct_answer: 0 }] },
    },
  });
  await page.goto(server.url + '/');
  expect(await page.title()).toBe('WebQuiz - Admin Selection Required');
  await expect(page.locator('body')).toContainText('Quiz Selection Required');
  const items = page.locator('.quiz-list li');
  await expect(items).toHaveText(['a.yaml - Quiz <A>', 'b.yaml']);
  await expect(page.locator('a[href="/admin/"]')).toBeVisible();
  await expect(page.locator('#username')).toHaveCount(0);
});

test('question text cannot be copied, the answer box can', async ({ page, launch }) => {
  const server = await launch({
    quizzes: defaultQuiz({ title: 'Copy', questions: [{ question: 'Secret question?', checker: '', correct_value: 'x', default_value: '' }] }),
  });
  await registerUser(page, server.url);
  const copyBlocked = (element: Element) => {
    const event = new ClipboardEvent('copy', { bubbles: true, cancelable: true });
    element.dispatchEvent(event);
    return event.defaultPrevented;
  };
  expect(await page.locator('.question-text').evaluate(copyBlocked)).toBe(true);
  expect(await page.locator('.current-question').evaluate((element) => getComputedStyle(element).userSelect)).toBe('none');
  expect(await page.locator('#text-answer-input').evaluate((element) => getComputedStyle(element).userSelect)).toBe('text');
});

test('double clicks register, answer and continue only once', async ({ page, launch }) => {
  const server = await launch({
    quizzes: defaultQuiz({
      title: 'Double',
      questions: [
        { question: 'Q1?', options: ['A', 'B'], correct_answer: 0 },
        { question: 'Q2?', options: ['A', 'B'], correct_answer: 0 },
        { question: 'Q3?', options: ['A', 'B'], correct_answer: 0 },
      ],
    }),
  });
  const posts: string[] = [];
  page.on('request', (request) => {
    if (request.method() === 'POST') posts.push(new URL(request.url()).pathname);
  });
  await page.goto(server.url + '/');
  await page.locator('#username').fill('Double');
  await page.locator('.register-btn').dblclick();
  await expect(page.locator('.question-text')).toHaveText('Q1?');

  await page.locator('#option_0').click();
  await page.locator('#submit-answer-btn').dblclick();
  await expect(page.locator('#continue-btn')).toBeVisible();
  await page.locator('#continue-btn').dblclick();
  await waitForQuestionNumber(page, 2, 3);
  await page.waitForTimeout(1000); // a second "Continue" would have opened question 3 by now
  await expect(page.locator('.question-text')).toHaveText('Q2?');

  expect(posts.filter((p) => p === '/api/register')).toHaveLength(1);
  expect(posts.filter((p) => p === '/api/submit-answer')).toHaveLength(1);
});

test('the registration form is filled with the values of last time', async ({ page, context, launch }) => {
  const server = await launch({
    config: { registration: { fields: ['Group'] } },
    quizzes: defaultQuiz({ title: 'Again', questions: [{ question: 'Q1?', options: ['A', 'B'], correct_answer: 0 }] }),
  });
  await registerUser(page, server.url, 'Anna', { group: '10-A' });
  await context.clearCookies();
  await page.goto(server.url + '/');
  await expect(page.locator('#username')).toHaveValue('Anna');
  await expect(page.locator('[data-field-name="group"]')).toHaveValue('10-A');
});

test('the whole-quiz timer turns red in the last 10 seconds', async ({ page, launch }) => {
  const server = await launch({
    config: EN,
    quizzes: { 'timed.yaml': { title: 'Timed', time_limit: { whole_quiz: 12 }, questions: [{ question: 'Q?', options: ['A', 'B'], correct_answer: 0 }] } },
  });
  await registerUser(page, server.url);
  const timer = page.locator('#quiz-timer');
  await expect(page.locator('#quiz-timer-box')).toContainText('Time left');
  await expect(timer).toHaveText(/^0:1[0-2]$/);
  await expect(timer).not.toHaveClass(/timer-warning/);
  await expect(timer).toHaveClass(/timer-warning/, { timeout: 5000 });
});
