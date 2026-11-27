// Ports of tests/test_selenium_manual_check.py (WebQuiz 1.x), the admin panel's side:
// the start pop-up note, the "manual check" block and the editor checkbox.
import type { APIRequestContext } from '@playwright/test';
import {
  adminLogin,
  expandQuestion,
  expect,
  openStartPopup,
  questionItems,
  registerStudent,
  savedQuiz,
  scriptClick,
  selectAndEditQuiz,
  test,
} from './helpers';

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

async function submit(request: APIRequestContext, url: string, userId: string, questionId: number, answer: unknown) {
  const response = await request.post(url + '/api/submit-answer', {
    data: { user_id: userId, question_id: questionId, selected_answer: answer },
  });
  expect(response.status()).toBe(200);
}

test('the start pop-up tells about the manual check', async ({ page, launch }) => {
  const server = await launch({ quizzes: quiz() });
  await openStartPopup(page, server.url);
  const note = page.locator('#start-manual-check-note');
  await expect(note).toBeVisible();
  await expect(page.locator('#start-manual-check-count')).toHaveText('Питань з ручною перевіркою: 1.');
  // The review page logs in like the other admin pages: the note says nothing about the key
  await expect(note).toContainText('Сторінка перевірки:');
  expect((await note.innerText()).toLowerCase()).not.toContain('ключ');
  expect((await note.innerText()).toLowerCase()).not.toMatch(/вчител|учител/);
  const links = note.locator('a');
  expect(await links.count()).toBeGreaterThan(0);
  for (const href of await links.evaluateAll((elements) => elements.map((element) => (element as HTMLAnchorElement).href))) {
    expect(href.endsWith('/review/')).toBe(true);
  }
});

test('the start pop-up has no note without manual questions', async ({ page, launch }) => {
  const server = await launch({
    quizzes: { 'q.yaml': { title: 'Plain', questions: [{ question: 'Q', options: ['a', 'b'], correct_answer: 0 }] } },
  });
  await openStartPopup(page, server.url);
  await expect(page.locator('#start-manual-check-note')).toBeHidden();
});

test('the admin panel shows the manual check status and links to the review page', async ({ page, request, launch }) => {
  const server = await launch({ quizzes: quiz() });
  await adminLogin(page, server.url);
  const section = page.locator('#manual-review-section');
  await expect(section).toBeVisible();
  await expect(section).toContainText('Ніхто не перевіряє');
  await expect(section.locator('a').first()).toHaveAttribute('href', /\/review\/$/);
  await expect(page.locator('.back-link a[href="/review/"]')).toBeVisible();

  await submit(request, server.url, await registerStudent(request, server.url), 1, 'Light scatters');
  await expect(page.locator('#manual-review-pending')).toHaveText('1');

  // A teacher opens the review page: the panel counts it
  const reviewer = await page.context().newPage();
  await reviewer.goto(server.url + '/review/');
  await expect(reviewer.locator('#item-card')).toBeVisible();
  await expect(page.locator('#manual-review-reviewers')).toHaveText('1');
  await expect(page.locator('#manual-review-no-reviewers')).toHaveText('Сторінка перевірки:');
  await reviewer.close();
  await expect(page.locator('#manual-review-reviewers')).toHaveText('0');
});

test('the admin panel has no manual check block for a quiz without manual questions', async ({ page, launch }) => {
  const server = await launch();
  await adminLogin(page, server.url);
  await expect(page.locator('#admin-panel')).toBeVisible();
  await expect(page.locator('#manual-review-section')).toBeHidden();
});

test('the editor saves manual_check and hides the checker code', async ({ page, request, launch }) => {
  const server = await launch({
    quizzes: {
      'q.yaml': {
        title: 'Editor',
        questions: [
          { question: 'Type it', correct_value: 'x', checker: "assert user_answer == 'x'" },
          { question: 'By hand', manual_check: true },
        ],
      },
    },
  });
  await adminLogin(page, server.url);
  await selectAndEditQuiz(page);
  // Folded questions: only the manual one carries the "✍️ вручну" marker
  const markers = questionItems(page).locator('.manual-check-indicator');
  await expect(markers).toHaveCount(1);
  await expect(questionItems(page).nth(1).locator('.manual-check-indicator')).toHaveText('✍️ вручну');
  await expect(questionItems(page).nth(1).locator('.manual-check-indicator')).toHaveAttribute('title', 'Ручна перевірка');
  const first = await expandQuestion(page, 0);
  const second = await expandQuestion(page, 1);
  await expect(first.locator('.manual-check-input')).not.toBeChecked();
  await expect(second.locator('.manual-check-input')).toBeChecked();
  await expect(second.locator('.checker-group')).toBeHidden();
  await expect(first.locator('.checker-group')).toBeVisible();

  // Manual check on for the first question: its checker code is hidden and not saved
  await scriptClick(first.locator('.manual-check-input'));
  await expect(first.locator('.checker-group')).toBeHidden();
  await expect(first.locator('.text-question-fields')).toContainText('Зразок відповіді');
  await expect(first.locator('.manual-check-input').locator('xpath=..')).toHaveText('✍️ Ручна перевірка');
  expect((await first.locator('.text-question-fields').innerText()).toLowerCase()).not.toMatch(/вчител|учител/);
  // The marker appears at once
  await expect(first.locator('.manual-check-indicator')).toHaveText('✍️ вручну');
  await expect(markers).toHaveCount(2);
  // ... and goes away at once when the question is no longer a text question
  await scriptClick(first.locator('.manual-check-input'));
  await expect(first.locator('.manual-check-indicator')).toHaveCount(0);
  await scriptClick(first.locator('.manual-check-input'));
  await expect(first.locator('.manual-check-indicator')).toHaveCount(1);
  await scriptClick(first.locator('.question-type-toggle[data-type="single"]'));
  await expect(first.locator('.manual-check-indicator')).toHaveCount(0);
  await scriptClick(first.locator('.question-type-toggle[data-type="text"]'));
  await expect(first.locator('.manual-check-indicator')).toHaveCount(1);
  await expect(questionItems(page)).toHaveCount(2);
  await scriptClick(page.locator('#save-continue-btn'));
  await expect(page.locator('#save-continue-btn')).toHaveText(/Збережено/);

  const saved = (await savedQuiz(request, server.url, 'q.yaml')) as { questions: Array<Record<string, unknown>> };
  expect(saved.questions[0].manual_check).toBe(true);
  expect('checker' in saved.questions[0]).toBe(false);
  expect(saved.questions[0].correct_value).toBe('x');
  expect(saved.questions[1].manual_check).toBe(true);
});
