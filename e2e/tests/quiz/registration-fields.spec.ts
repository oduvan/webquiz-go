// Ports of tests/test_selenium_registration_fields.py: extra registration fields from the config.
import { defaultQuiz, expect, test } from '../../fixtures/quiz';

// The old form was a table with "Label:" cells (the tests found them with
// //td[contains(text(), 'Grade:')]). The new form has a <label class="registration-label">
// with the same "Label:" text next to each field; the ports look for those labels.

// py: tests/test_selenium_registration_fields.py::test_registration_form_renders_extra_fields
test('the form shows the extra fields with their labels', async ({ page, launch }) => {
  const server = await launch({
    config: { registration: { fields: ['Grade', 'School'] } },
    quizzes: defaultQuiz({
      title: 'Registration Fields Test',
      questions: [{ question: 'Test question?', options: ['A', 'B'], correct_answer: 0 }],
    }),
  });
  await page.goto(server.url + '/');
  await expect(page.locator('#username')).toBeVisible();
  await expect(page.locator('.registration-field')).toHaveCount(2);
  await expect(page.locator('#registration .registration-label', { hasText: 'Grade:' })).toBeVisible();
  await expect(page.locator('#registration .registration-label', { hasText: 'School:' })).toBeVisible();
});

// py: tests/test_selenium_registration_fields.py::test_registration_with_all_fields_filled
test('registration with every field filled starts the quiz', async ({ page, launch }) => {
  const server = await launch({
    config: { registration: { fields: ['Grade', 'School'] } },
    quizzes: defaultQuiz({
      title: 'Registration Success Test',
      questions: [{ question: 'Test question?', options: ['A', 'B'], correct_answer: 0 }],
    }),
  });
  await page.goto(server.url + '/');
  await page.locator('#username').fill('TestStudent');
  const fields = page.locator('.registration-field');
  await fields.nth(0).fill('10');
  await fields.nth(1).fill('Central High School');
  await page.locator('.register-btn').click();
  await expect(page.locator('#test-section')).toBeVisible();
  await expect(page.locator('#user-info')).toContainText('TestStudent');
});

// py: tests/test_selenium_registration_fields.py::test_registration_missing_required_field
test('an empty field keeps the form and shows an error', async ({ page, launch }) => {
  const server = await launch({
    config: { registration: { fields: ['Grade'] } },
    quizzes: defaultQuiz({
      title: 'Validation Test',
      questions: [{ question: 'Test question?', options: ['A', 'B'], correct_answer: 0 }],
    }),
  });
  await page.goto(server.url + '/');
  await page.locator('#username').fill('IncompleteStudent');
  await page.locator('.register-btn').click();
  await expect(page.locator('#registration-error')).toBeVisible();
  await expect(page.locator('#registration')).toBeVisible();
  await expect(page.locator('#registration')).not.toHaveClass(/hidden/);
  // The message names the field, as the old page did
  await expect(page.locator('#registration-error')).toContainText('Grade');
});

// py: tests/test_selenium_registration_fields.py::test_multiple_users_with_registration_fields
test('two students register one after the other', async ({ page, context, launch }) => {
  const server = await launch({
    config: { registration: { fields: ['Grade'] } },
    quizzes: defaultQuiz({
      title: 'Multiple Users Test',
      questions: [{ question: 'Test question?', options: ['A', 'B'], correct_answer: 0 }],
    }),
  });
  await page.goto(server.url + '/');
  await page.locator('#username').fill('Student1');
  await page.locator('.registration-field').fill('9');
  await page.locator('.register-btn').click();
  await expect(page.locator('#test-section')).toBeVisible();

  await context.clearCookies(); // a new student
  await page.goto(server.url + '/');
  await page.locator('#username').fill('Student2');
  await page.locator('.registration-field').fill('10');
  await page.locator('.register-btn').click();
  await expect(page.locator('#test-section')).toBeVisible();
});

// py: tests/test_selenium_registration_fields.py::test_quiz_flow_with_registration_fields
test('a whole quiz after registration with fields', async ({ page, launch }) => {
  const server = await launch({
    config: { registration: { fields: ['Grade', 'School'] } },
    quizzes: defaultQuiz({
      title: 'Full Flow Test',
      questions: [
        { question: 'What is 1+1?', options: ['1', '2', '3'], correct_answer: 1 },
        { question: 'What is 2+2?', options: ['3', '4', '5'], correct_answer: 1 },
      ],
    }),
  });
  await page.goto(server.url + '/');
  await page.locator('#username').fill('QuizTaker');
  const fields = page.locator('.registration-field');
  await fields.nth(0).fill('11');
  await fields.nth(1).fill('North High');
  await page.locator('.register-btn').click();
  await expect(page.locator('#test-section')).toBeVisible();

  await page.locator('#option_1').click();
  await page.locator('#submit-answer-btn').click();
  await page.locator('#continue-btn').click();

  await expect(page.locator('.question-text')).toContainText('2+2', { timeout: 5000 });
  await page.locator('#option_1').click();
  await page.locator('#submit-answer-btn').click();
  await page.locator('#continue-btn').click();

  await expect(page.locator('#results')).toBeVisible();
});

// py: tests/test_selenium_registration_fields.py::test_registration_with_cyrillic_labels_selenium
test('Cyrillic field labels', async ({ page, launch }) => {
  const server = await launch({
    config: { registration: { fields: ['Клас', 'Школа'] } },
    quizzes: defaultQuiz({
      title: 'Cyrillic Test',
      questions: [{ question: 'Test?', options: ['A', 'B'], correct_answer: 0 }],
    }),
  });
  await page.goto(server.url + '/');
  await page.locator('#username').fill('УкраїнськийУчень');
  const fields = page.locator('.registration-field');
  await expect(fields).toHaveCount(2);
  await expect(page.locator('#registration .registration-label', { hasText: 'Клас:' })).toBeVisible();
  await expect(page.locator('#registration .registration-label', { hasText: 'Школа:' })).toBeVisible();
  await fields.nth(0).fill('8');
  await fields.nth(1).fill('Гімназія №7');
  await page.locator('.register-btn').click();
  await expect(page.locator('#test-section')).toBeVisible();
});
