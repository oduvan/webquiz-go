// Ports of the live stats part of tests/test_selenium_html_escaping.py: question text
// and student names are shown as plain text on the live stats page.
import { expect, test } from '../../fixtures/quiz';

const TAG_QUESTION = 'What does 0 mean for `<vtx_band>` and `<vtx_channel>`?';
// A name that would run code if the page used it as HTML
const SCRIPT_USERNAME = '<img src=x onerror="window.injected=1">Bob';

const TAG_QUIZ = {
  'default.yaml': {
    title: 'Tags <in> title',
    questions: [{ question: TAG_QUESTION, options: ['Use <default> band', 'Turn <b>off</b> the VTX'], correct_answer: 0 }],
  },
};

// py: tests/test_selenium_html_escaping.py::test_live_stats_shows_tags_in_question_text
test('the live stats header shows tags in the question text', async ({ page, request, launch }) => {
  const server = await launch({ quizzes: TAG_QUIZ });
  await request.post(`${server.url}/api/register`, { data: { username: 'student1' } });

  await page.goto(server.url + '/live-stats/');
  const header = page.locator('th .question-text');
  await expect(header).toContainText(TAG_QUESTION, { timeout: 15000 });
  const th = page.locator('th', { has: page.locator('.question-text') }).first();
  expect(await th.getAttribute('title')).toContain(TAG_QUESTION); // the tooltip keeps the tags too
});

// py: tests/test_selenium_html_escaping.py::test_live_stats_shows_username_as_text
test('a student name with HTML is shown as text and never runs', async ({ page, request, launch }) => {
  const server = await launch({ quizzes: TAG_QUIZ });
  const response = await request.post(`${server.url}/api/register`, { data: { username: SCRIPT_USERNAME } });
  expect(response.status()).toBe(200);

  await page.goto(server.url + '/live-stats/');
  const name = page.locator('.username-text');
  await expect(name).toHaveText(SCRIPT_USERNAME, { timeout: 15000 });
  await expect(name.locator('img')).toHaveCount(0);
  expect(await page.evaluate(() => (window as unknown as { injected?: unknown }).injected === undefined)).toBe(true);
});
