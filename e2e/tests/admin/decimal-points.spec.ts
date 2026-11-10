import {
  adminLogin,
  clickSave,
  errorMessages,
  expandQuestion,
  expect,
  quizContent,
  saveQuiz,
  selectAndEditQuiz,
  setPoints,
  test,
} from './helpers';

// tests/test_selenium_admin_decimal_points.py: decimal question points in the
// quiz editor (0.5, 0.25), the check of validateWizardPoints, and the trophy
// sign in the collapsed header.

const QUIZ = {
  'default.yaml': {
    title: 'Editor Points Quiz',
    questions: [
      { question: 'Q1', options: ['A', 'B'], correct_answer: 0 },
      { question: 'Q2', options: ['A', 'B'], correct_answer: 1 },
    ],
  },
};

// py: tests/test_selenium_admin_decimal_points.py::test_points_input_accepts_decimal_values
test('the points field takes decimal values', async ({ page, launch }) => {
  const server = await launch({ quizzes: QUIZ });
  await adminLogin(page, server.url);
  await selectAndEditQuiz(page);

  const field = await setPoints(page, 0, '0.25');

  await expect(field, 'the field holds the decimal value').toHaveValue('0.25');
  await expect(field, 'the field steps by 0.01').toHaveAttribute('step', '0.01');
  // A browser marks a number field invalid when the value breaks step or min
  expect(await field.evaluate((input) => (input as HTMLInputElement).checkValidity()), '0.25 is valid').toBe(true);
});

// py: tests/test_selenium_admin_decimal_points.py::test_editor_saves_decimal_points
test('the editor saves decimal points', async ({ page, request, launch }) => {
  const server = await launch({ quizzes: QUIZ });
  await adminLogin(page, server.url);
  await selectAndEditQuiz(page);

  await setPoints(page, 0, '0.5');
  await setPoints(page, 1, '0.25');
  await saveQuiz(page);

  const content = await quizContent(request, server.url);
  expect(content, '0.5 was not saved').toContain('points: 0.5');
  expect(content, '0.25 was not saved').toContain('points: 0.25');
});

// py: tests/test_selenium_admin_decimal_points.py::test_decimal_points_reload_into_editor
test('decimal points of a quiz file show in the editor', async ({ page, launch }) => {
  const server = await launch({
    quizzes: {
      'default.yaml': {
        title: 'Stored Points Quiz',
        questions: [
          { question: 'Q1', options: ['A', 'B'], correct_answer: 0, points: 0.25 },
          { question: 'Q2', options: ['A', 'B'], correct_answer: 1, points: 2.5 },
        ],
      },
    },
  });
  await adminLogin(page, server.url);
  await selectAndEditQuiz(page);

  await expect((await expandQuestion(page, 0)).locator('.points-input')).toHaveValue('0.25');
  await expect((await expandQuestion(page, 1)).locator('.points-input')).toHaveValue('2.5');
});

// py: tests/test_selenium_admin_decimal_points.py::test_more_than_two_decimals_shows_error
test('more than two decimals give a message instead of a save', async ({ page, request, launch }) => {
  const server = await launch({ quizzes: QUIZ });
  await adminLogin(page, server.url);
  await selectAndEditQuiz(page);

  await setPoints(page, 0, '0.255');
  await clickSave(page);

  await expect(page.locator('#message-area .message.error').first()).toBeVisible();
  const message = (await errorMessages(page)).join(' ');
  expect(message, 'the message names the question').toContain('Питання 1');
  expect(message, 'the message names the 2 decimal limit').toContain('2 знак');

  // The editor stays open so the value can be corrected
  await expect(page.locator('#quiz-editor-modal')).toBeVisible();

  // Nothing was saved
  expect(await quizContent(request, server.url)).not.toContain('points:');
});

// py: tests/test_selenium_admin_decimal_points.py::test_zero_points_shows_error
test('zero points give a message that names the question', async ({ page, launch }) => {
  const server = await launch({ quizzes: QUIZ });
  await adminLogin(page, server.url);
  await selectAndEditQuiz(page);

  await setPoints(page, 0, '0');
  await clickSave(page);

  await expect(page.locator('#message-area .message.error').first()).toBeVisible();
  expect((await errorMessages(page)).join(' '), 'the message names the question').toContain('Питання 1');
  await expect(page.locator('#quiz-editor-modal'), 'the editor stays open').toBeVisible();
});

// py: tests/test_selenium_admin_decimal_points.py::test_two_decimals_still_saves
test('two decimals save without a message', async ({ page, request, launch }) => {
  const server = await launch({ quizzes: QUIZ });
  await adminLogin(page, server.url);
  await selectAndEditQuiz(page);

  await setPoints(page, 0, '0.25');
  await saveQuiz(page);

  expect(await errorMessages(page), 'no error expected').toEqual([]);
  expect(await quizContent(request, server.url), '0.25 should be saved').toContain('points: 0.25');
});

// py: tests/test_selenium_admin_decimal_points.py::test_trophy_indicator_shows_for_half_point
test('a half point question shows the trophy sign', async ({ page, launch }) => {
  const server = await launch({
    quizzes: {
      'default.yaml': {
        title: 'Indicator Quiz',
        questions: [{ question: 'Q1', options: ['A', 'B'], correct_answer: 0, points: 0.5 }],
      },
    },
  });
  await adminLogin(page, server.url);
  await selectAndEditQuiz(page);

  const indicator = page.locator('.points-indicator');
  await expect(indicator, 'the trophy sign is shown for a 0.5 point question').toBeAttached();
  expect(await indicator.innerHTML(), 'the sign shows 0.5').toContain('0.5');
});
