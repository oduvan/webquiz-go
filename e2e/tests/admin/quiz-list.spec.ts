import { adminLogin, adminSession, expect, quizOptions, selectMultipleQuizzes, test } from './helpers';

// The browser tests of tests/test_admin_unite_quizzes.py: the quiz list
// shows single or multiple actions by selection, and marks the running quiz.

const TWO_QUIZZES = {
  'quiz1.yaml': {
    title: 'Quiz 1',
    questions: [{ question: 'Q1?', options: ['A', 'B'], correct_answer: 0 }],
  },
  'quiz2.yaml': {
    title: 'Quiz 2',
    questions: [{ question: 'Q2?', options: ['C', 'D'], correct_answer: 1 }],
  },
};

// py: tests/test_admin_unite_quizzes.py::test_single_select_shows_standard_buttons
test('one selected quiz shows the standard buttons', async ({ page, launch }) => {
  const server = await launch({ quizzes: TWO_QUIZZES });
  await adminLogin(page, server.url);

  await quizOptions(page).nth(0).click();

  await expect(page.locator('#single-quiz-actions')).toBeVisible();
  await expect(page.locator('#multi-quiz-actions')).toBeHidden();
  await expect(page.locator('#edit-btn')).toBeEnabled();
});

// py: tests/test_admin_unite_quizzes.py::test_multi_select_shows_unite_button
test('two selected quizzes show the unite button', async ({ page, launch }) => {
  const server = await launch({ quizzes: TWO_QUIZZES });
  await adminLogin(page, server.url);

  await selectMultipleQuizzes(page, [0, 1]);

  await expect(page.locator('#single-quiz-actions')).toBeHidden();
  await expect(page.locator('#multi-quiz-actions')).toBeVisible();
  await expect(page.locator('#unite-btn')).toBeVisible();
});

// py: tests/test_admin_unite_quizzes.py::test_current_quiz_marked_in_select
test('the running quiz is marked in the list', async ({ page, request, launch }) => {
  const server = await launch({ quizzes: TWO_QUIZZES });
  // Start a quiz through the API first, so there is a running quiz
  await adminSession(request, server.url);
  const response = await request.post(server.url + '/api/admin/switch-quiz', { data: { quiz_filename: 'quiz1.yaml' } });
  expect(response.status()).toBe(200);

  await adminLogin(page, server.url);

  const texts = await quizOptions(page).allTextContents();
  expect(texts.some((text) => text.indexOf('(поточний)') !== -1), `options: ${texts.join(' | ')}`).toBe(true);
});

// py: tests/test_admin_unite_quizzes.py::test_delete_button_disabled_for_current_quiz
test('the delete button is off for the running quiz', async ({ page, request, launch }) => {
  const server = await launch({
    quizzes: { 'quiz1.yaml': { title: 'Quiz 1', questions: [{ question: 'Q1?', options: ['A', 'B'], correct_answer: 0 }] } },
  });
  await adminSession(request, server.url);
  const response = await request.post(server.url + '/api/admin/switch-quiz', { data: { quiz_filename: 'quiz1.yaml' } });
  expect(response.status()).toBe(200);

  await adminLogin(page, server.url);
  // The only quiz is the running one
  await quizOptions(page).nth(0).click();

  await expect(page.locator('#delete-btn')).toBeDisabled();
});
