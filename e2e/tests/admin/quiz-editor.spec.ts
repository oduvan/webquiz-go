import { adminLogin, expect, questionItems, selectAndEditQuiz, test } from './helpers';

// tests/test_selenium_quiz_editor.py: collapsible questions in the quiz
// editor, their header (number, preview, indicators, drag handle) and
// "Save and continue".

const COLLAPSED = /(^|\s)collapsed(\s|$)/;

// py: tests/test_selenium_quiz_editor.py::test_questions_collapsed_when_editing_quiz
test('questions are collapsed when a quiz opens in the editor', async ({ page, launch }) => {
  const server = await launch({
    quizzes: {
      'test_quiz.yaml': {
        title: 'Collapsible Test Quiz',
        questions: [
          { question: 'Question 1 text here', options: ['A', 'B'], correct_answer: 0 },
          { question: 'Question 2 text here', options: ['C', 'D'], correct_answer: 1 },
          { question: 'Question 3 text here', options: ['E', 'F'], correct_answer: 0 },
        ],
      },
    },
  });
  await adminLogin(page, server.url);
  await selectAndEditQuiz(page);

  const items = questionItems(page);
  await expect(items).toHaveCount(3);
  for (let i = 0; i < 3; i++) {
    await expect(items.nth(i), `question ${i + 1} should be collapsed`).toHaveClass(COLLAPSED);
    // A collapsed question's body has no height
    const maxHeight = await items.nth(i).locator('.question-body').evaluate((body) => getComputedStyle(body).maxHeight);
    expect(['0px', '0'], `question ${i + 1} body max-height`).toContain(maxHeight);
  }
});

// py: tests/test_selenium_quiz_editor.py::test_click_header_toggles_collapse
test('a click on the question header folds and unfolds it', async ({ page, launch }) => {
  const server = await launch({
    quizzes: {
      'test_quiz.yaml': {
        title: 'Toggle Test Quiz',
        questions: [{ question: 'Toggle question text', options: ['Yes', 'No'], correct_answer: 0 }],
      },
    },
  });
  await adminLogin(page, server.url);
  await selectAndEditQuiz(page);

  const item = questionItems(page).first();
  const header = item.locator('.question-header');
  await expect(item).toHaveClass(COLLAPSED);

  await header.click();
  await expect(item, 'the question should be expanded after a click').not.toHaveClass(COLLAPSED);

  await header.click();
  await expect(item, 'the question should be collapsed after a second click').toHaveClass(COLLAPSED);
});

// py: tests/test_selenium_quiz_editor.py::test_new_question_is_expanded
test('a new question is expanded', async ({ page, launch }) => {
  const server = await launch({
    quizzes: {
      'test_quiz.yaml': {
        title: 'New Question Test',
        questions: [{ question: 'Existing question', options: ['A', 'B'], correct_answer: 0 }],
      },
    },
  });
  await adminLogin(page, server.url);
  await selectAndEditQuiz(page);

  await page.getByRole('button', { name: 'Додати Питання' }).click();

  const items = questionItems(page);
  await expect(items).toHaveCount(2);
  await expect(items.nth(0), 'the existing question stays collapsed').toHaveClass(COLLAPSED);
  await expect(items.nth(1), 'the new question is expanded').not.toHaveClass(COLLAPSED);
});

// py: tests/test_selenium_quiz_editor.py::test_question_preview_text_displayed
test('the collapsed header shows the question text', async ({ page, launch }) => {
  const server = await launch({
    quizzes: {
      'test_quiz.yaml': {
        title: 'Preview Test Quiz',
        questions: [{ question: 'What is the capital of Ukraine?', options: ['Kyiv', 'Lviv'], correct_answer: 0 }],
      },
    },
  });
  await adminLogin(page, server.url);
  await selectAndEditQuiz(page);

  // Selenium's .text is the text on the screen: the preview must be shown
  const preview = questionItems(page).first().locator('.question-preview');
  await expect(preview).toBeVisible();
  await expect(preview).toContainText('What is the capital of Ukraine?');
});

// py: tests/test_selenium_quiz_editor.py::test_file_image_indicators_displayed
test('the collapsed header shows image and file indicators', async ({ page, launch }) => {
  const server = await launch({
    quizzes: {
      'test_quiz.yaml': {
        title: 'Indicators Test Quiz',
        questions: [
          { question: 'Question with image', image: '/imgs/test.png', options: ['A', 'B'], correct_answer: 0 },
          { question: 'Question with file', file: 'data.xlsx', options: ['C', 'D'], correct_answer: 0 },
          {
            question: 'Question with both',
            image: '/imgs/pic.jpg',
            file: 'doc.pdf',
            options: ['E', 'F'],
            correct_answer: 0,
          },
        ],
      },
    },
  });
  await adminLogin(page, server.url);
  await selectAndEditQuiz(page);

  // The picture sign without its variation selector, as the Python test checked it
  const image = '\u{1F5BC}';
  const file = '📎';
  // Selenium's .text is the text on the screen: the signs must be shown
  const items = questionItems(page);
  const shownText = async (index: number) => {
    const indicators = items.nth(index).locator('.question-indicators');
    await expect(indicators).toBeVisible();
    return indicators.innerText();
  };
  const first = await shownText(0);
  expect(first, 'the first question has the image sign').toContain(image);
  const second = await shownText(1);
  expect(second, 'the second question has the file sign').toContain(file);
  const third = await shownText(2);
  expect(third, 'the third question has the image sign').toContain(image);
  expect(third, 'the third question has the file sign').toContain(file);
});

// py: tests/test_selenium_quiz_editor.py::test_drag_handle_present
test('every question has a drag handle', async ({ page, launch }) => {
  const server = await launch({
    quizzes: {
      'test_quiz.yaml': {
        title: 'Drag Handle Test Quiz',
        questions: [
          { question: 'Question 1', options: ['A', 'B'], correct_answer: 0 },
          { question: 'Question 2', options: ['C', 'D'], correct_answer: 0 },
        ],
      },
    },
  });
  await adminLogin(page, server.url);
  await selectAndEditQuiz(page);

  const items = questionItems(page);
  await expect(items).toHaveCount(2);
  for (let i = 0; i < 2; i++) {
    const handle = items.nth(i).locator('.drag-handle');
    await expect(handle, `question ${i + 1} drag handle`).toBeVisible();
    await expect(handle).toContainText('☰');
  }
});

// py: tests/test_selenium_quiz_editor.py::test_question_numbers_without_prefix
test('question numbers have no "Питання" prefix', async ({ page, launch }) => {
  const server = await launch({
    quizzes: {
      'test_quiz.yaml': {
        title: 'Numbers Test Quiz',
        questions: [
          { question: 'Question 1', options: ['A', 'B'], correct_answer: 0 },
          { question: 'Question 2', options: ['C', 'D'], correct_answer: 0 },
        ],
      },
    },
  });
  await adminLogin(page, server.url);
  await selectAndEditQuiz(page);

  // Selenium's .text is the text on the screen: the numbers must be shown
  const items = questionItems(page);
  for (const [index, text] of [
    [0, '1.'],
    [1, '2.'],
  ] as [number, string][]) {
    const number = items.nth(index).locator('.question-number');
    await expect(number).toBeVisible();
    await expect(number).toHaveText(text);
  }
});

// py: tests/test_selenium_quiz_editor.py::test_save_and_continue_keeps_editor_open
test('"Save and continue" saves a new quiz and keeps the editor open', async ({ page, launch }) => {
  const server = await launch({ config: { admin: { master_key: 'test123' } } });
  await adminLogin(page, server.url);

  await page.getByRole('button', { name: 'Створити Новий Quiz' }).click();
  await expect(page.locator('#quiz-editor-modal')).toBeVisible();

  await page.locator('#quiz-filename').fill('test_save_continue');
  await page.locator('#quiz-title').fill('Save and Continue Test');

  // The editor focuses the first question 300 ms after it opens
  await expect
    .poll(() => page.evaluate(() => (document.activeElement ? document.activeElement.classList.contains('question-text') : false)))
    .toBe(true);

  await page.getByRole('button', { name: 'Додати Питання' }).click();

  // The first question gets the text; the added one stays empty and is not saved
  const item = questionItems(page).first();
  await item.locator('.question-text').fill('Test question');
  const options = item.locator('.option-text');
  await options.nth(0).fill('Option A');
  await options.nth(1).fill('Option B');

  const button = page.locator('#save-continue-btn');
  await button.click();

  // The button shows that it saved, then comes back after 1.5 s
  await expect(button).toBeVisible();
  await expect(button).toContainText('Збережено');
  await expect(button, 'the button is enabled again after the save').toBeEnabled();
  await expect(button).toContainText('Продовжити');

  await expect(page.locator('#quiz-editor-modal'), 'the editor stays open').toBeVisible();
  // Now an edited quiz: the file name can be changed (a rename)
  await expect(page.locator('#quiz-filename')).toBeEnabled();
  await expect(page.locator('#modal-title')).toBeVisible();
  await expect(page.locator('#modal-title')).toContainText('Редагувати');
});

// py: tests/test_selenium_quiz_editor.py::test_save_and_continue_in_edit_mode
test('"Save and continue" works for an edited quiz', async ({ page, launch }) => {
  const server = await launch({
    quizzes: {
      'test_quiz.yaml': {
        title: 'Existing Quiz',
        questions: [{ question: 'Original question', options: ['A', 'B'], correct_answer: 0 }],
      },
    },
  });
  await adminLogin(page, server.url);
  await selectAndEditQuiz(page);

  await page.locator('#quiz-title').fill('Modified Title');

  const button = page.locator('#save-continue-btn');
  await button.click();
  await expect(button).toBeVisible();
  await expect(button).toContainText('Збережено');
  await expect(button, 'the button is enabled again after the save').toBeEnabled();

  await expect(page.locator('#quiz-editor-modal'), 'the editor stays open').toBeVisible();
  await expect(page.locator('#quiz-title'), 'the title is kept').toHaveValue('Modified Title');
});
