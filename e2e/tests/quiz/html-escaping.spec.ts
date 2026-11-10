// Ports of tests/test_selenium_html_escaping.py (quiz page and admin parts): text from
// quizzes and from students is shown as plain text, never read as HTML. The live stats
// part is in tests/livestats/html-escaping.spec.ts.
import { adminLogin } from '../../fixtures/helpers';
import { expect, registerUser, test } from '../../fixtures/quiz';

const TAG_QUESTION = 'What does 0 mean for `<vtx_band>` and `<vtx_channel>`?';
const TAG_OPTION_A = 'Use <default> band';
const TAG_OPTION_B = 'Turn <b>off</b> the VTX';
// A name that would run code if the page used it as HTML
const SCRIPT_USERNAME = '<img src=x onerror="window.injected=1">Bob';

const TAG_QUIZ = {
  'default.yaml': {
    title: 'Tags <in> title',
    questions: [{ question: TAG_QUESTION, options: [TAG_OPTION_A, TAG_OPTION_B], correct_answer: 0 }],
  },
};

// py: tests/test_selenium_html_escaping.py::test_quiz_page_shows_tags_in_question_options_and_results
test('the quiz page shows tags in the question, the options and the results', async ({ page, launch }) => {
  const server = await launch({ quizzes: TAG_QUIZ });
  await page.goto(server.url + '/');
  expect(await page.title()).toContain('Tags <in> title');
  await expect(page.locator('h1')).toHaveText('Tags <in> title');

  await page.locator('#username').fill('student1');
  await page.locator('.register-btn').click();

  await expect(page.locator('.question-text')).toHaveText(TAG_QUESTION);
  const optionA = page.locator('[data-option-index="0"]');
  const optionB = page.locator('[data-option-index="1"]');
  // The old options held only their text; the new ones keep it in .option-text
  await expect(optionA.locator('.option-text')).toHaveText(TAG_OPTION_A);
  await expect(optionB.locator('.option-text')).toHaveText(TAG_OPTION_B);
  await expect(optionB.locator('b')).toHaveCount(0);

  // The wrong option, so the results also show the right one
  await optionB.click();
  await page.locator('#submit-answer-btn').click();
  await page.locator('#continue-btn').click();

  const results = page.locator('#results-content');
  await expect(results).toBeVisible();
  await expect(results).toContainText(TAG_QUESTION);
  await expect(results).toContainText(TAG_OPTION_B);
  await expect(results).toContainText(TAG_OPTION_A);
});

// py: tests/test_selenium_html_escaping.py::test_quiz_page_text_question_keeps_tags
test('a text question keeps tags in the starting text and the right answer', async ({ page, launch }) => {
  const server = await launch({
    quizzes: {
      'default.yaml': {
        title: 'Text quiz',
        questions: [
          { question: 'Type the command', default_value: 'vtx <band> </textarea><b>x</b>', correct_value: 'vtx <band> <channel>', checker: '' },
        ],
      },
    },
  });
  await registerUser(page, server.url, 'student1');
  const textarea = page.locator('#text-answer-input');
  await expect(textarea).toHaveValue('vtx <band> </textarea><b>x</b>');

  await page.locator('#submit-answer-btn').click();
  await expect(page.locator('#checker-feedback .correct-value-display')).toContainText('vtx <band> <channel>', { timeout: 10000 });
});

// py: tests/test_selenium_html_escaping.py::test_waiting_form_keeps_quotes_and_tags_in_name
test('the waiting form keeps quotes and tags in the name', async ({ page, launch }) => {
  const username = 'O"Brien <x>';
  const server = await launch({ config: { registration: { approve: true } }, quizzes: TAG_QUIZ });
  await page.goto(server.url + '/');
  await page.locator('#username').fill(username);
  await page.locator('.register-btn').click();
  await expect(page.locator('#waiting-username')).toHaveValue(username);
});

// py: tests/test_selenium_html_escaping.py::test_admin_pending_users_show_name_and_fields_as_text
// The admin page keeps the old page's classes (.pending-user-item, .pending-user-name).
test('the admin approval list shows the name and fields as text', async ({ page, request, launch }) => {
  const groupValue = '<i>10-A</i>';
  const server = await launch({ config: { registration: { approve: true, fields: ['Group'] } }, quizzes: TAG_QUIZ });
  const response = await request.post(`${server.url}/api/register`, { data: { username: SCRIPT_USERNAME, group: groupValue } });
  expect(response.status()).toBe(200);

  await adminLogin(page, server.url);
  const name = page.locator('.pending-user-name').first();
  await expect(name).toBeAttached({ timeout: 15000 });
  const item = page.locator('.pending-user-item', { has: page.locator('.pending-user-name') }).first();

  // Selenium's .text reads only text on the screen, so the text must be visible too
  await expect(name).toBeVisible();
  await expect(item).toBeVisible();
  await expect(name).toContainText(SCRIPT_USERNAME);
  await expect(item).toContainText(`Group: ${groupValue}`);
  await expect(item.locator('img')).toHaveCount(0);
  await expect(item.locator('i')).toHaveCount(0);
  expect(await page.evaluate(() => (window as unknown as { injected?: unknown }).injected === undefined)).toBe(true);
});
