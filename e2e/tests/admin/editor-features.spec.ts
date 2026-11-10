import { mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import path from 'node:path';
import type { Locator, Page } from '@playwright/test';
import { parse } from 'yaml';
import {
  adminLogin,
  expandQuestion,
  expect,
  questionItems,
  saveQuiz,
  savedQuiz,
  selectAndEditQuiz,
  test,
  type RunningServer,
} from './helpers';

// The quiz editor's operations that no Python test covered: options and
// correct answers, question types and checker templates, moving and removing
// questions, the image and file pickers, the sticky flag and the quiz settings.

type Dict = Record<string, unknown>;

const PNG = Buffer.from(
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==',
  'base64',
);

function messages(page: Page, type: 'success' | 'error' | 'warning') {
  return page.locator(`#message-area .message.${type}`);
}

function writeQuizFile(server: RunningServer, folder: 'imgs' | 'attach', name: string, data: Buffer | string) {
  const dir = path.join(server.dir, 'quizzes', folder);
  mkdirSync(dir, { recursive: true });
  writeFileSync(path.join(dir, name), data);
}

function quizWith(questions: Dict[], settings: Dict = {}) {
  return { 'default.yaml': { title: 'Editor', questions, ...settings } };
}

function choice(text: string, extra: Dict = {}): Dict {
  return { question: text, options: ['A', 'B', 'C'], correct_answer: 0, ...extra };
}

async function correctMarks(item: Locator): Promise<boolean[]> {
  return item.locator('.option-item input:not([type="text"])').evaluateAll((inputs) =>
    inputs.map((input) => (input as HTMLInputElement).checked),
  );
}

async function inputKinds(item: Locator): Promise<string[]> {
  return item.locator('.option-item input:not([type="text"])').evaluateAll((inputs) =>
    inputs.map((input) => (input as HTMLInputElement).type),
  );
}

function answerNextDialog(page: Page, accept: boolean) {
  page.once('dialog', (dialog) => (accept ? dialog.accept() : dialog.dismiss()));
}

test('a new quiz is built with options, answers, points and a checker template', async ({ page, request, launch }) => {
  const server = await launch({
    config: { checker_templates: [{ name: 'Range', code: 'assert 1 <= to_int(user_answer) <= 9' }] },
  });
  await adminLogin(page, server.url);
  await page.locator('#create-quiz-btn').click();
  await expect(page.locator('#modal-title')).toHaveText('Створити Новий Quiz');
  await page.locator('#quiz-filename').fill('built');

  const first = questionItems(page).first();
  await expect(first).not.toHaveClass(/collapsed/);
  await first.locator('.question-text').fill('Pick two');
  const options = first.locator('.option-text');
  await options.nth(0).fill('A');
  await options.nth(1).fill('B');
  await expect(options.nth(1)).toHaveAttribute('placeholder', 'Варіант 2');

  // At least two options stay
  await first.locator('.remove-option-btn').first().click();
  await expect(messages(page, 'warning').last()).toHaveText('Питання має мати принаймні 2 варіанти');
  await expect(options).toHaveCount(2);
  await first.getByRole('button', { name: '+ Додати Варіант' }).click();
  await expect(options).toHaveCount(3);
  await options.nth(2).fill('C');

  // Several correct answers
  await first.locator('.question-type-toggle[data-type="multiple"]').click();
  expect(await inputKinds(first)).toEqual(['checkbox', 'checkbox', 'checkbox']);
  await first.locator('.option-item').nth(2).locator('input[type="checkbox"]').check();
  await expect(first.locator('.min-correct-container')).toBeVisible();
  await first.locator('.min-correct-input').fill('1');
  await first.locator('.points-input').fill('2');
  await expect(first.locator('.points-indicator')).toHaveText('🏆 2');

  // A text question with a template
  await page.getByRole('button', { name: 'Додати Питання' }).click();
  const second = questionItems(page).nth(1);
  await second.locator('.question-text').fill('Type a digit');
  await second.locator('.question-type-toggle[data-type="text"]').click();
  await expect(second.locator('.choice-question-fields')).toBeHidden();
  await expect(second.locator('.text-question-fields')).toBeVisible();
  await expect(second.locator('.default-value-input')).toHaveValue('0');
  await expect(second.locator('.correct-value-input')).toHaveValue('1000');
  await expect(second.locator('.checker-template-select option')).toHaveText(['-- Оберіть шаблон --', 'Range']);
  await second.locator('.checker-template-select').selectOption({ label: 'Range' });
  await expect(second.locator('.checker-input')).toHaveValue('assert 1 <= to_int(user_answer) <= 9');
  await expect(second.locator('.checker-template-select')).toHaveValue('');

  await saveQuiz(page);
  await expect(messages(page, 'success').last()).toHaveText('Quiz "built.yaml" created successfully');

  const saved = await savedQuiz(request, server.url, 'built.yaml');
  expect(saved.questions).toEqual([
    { question: 'Pick two', options: ['A', 'B', 'C'], correct_answer: [0, 2], min_correct: 1, points: 2 },
    {
      question: 'Type a digit',
      default_value: '0',
      correct_value: '1000',
      checker: 'assert 1 <= to_int(user_answer) <= 9',
    },
  ]);
});

test('switching question types keeps the answers like the old editor', async ({ page, launch }) => {
  const server = await launch({ quizzes: quizWith([choice('Q', { correct_answer: [1, 2], min_correct: 2 })]) });
  await adminLogin(page, server.url);
  await selectAndEditQuiz(page);
  const item = await expandQuestion(page, 0);

  expect(await inputKinds(item)).toEqual(['checkbox', 'checkbox', 'checkbox']);
  expect(await correctMarks(item)).toEqual([false, true, true]);
  await expect(item.locator('.min-correct-input')).toHaveValue('2');

  // Checkboxes become radio buttons with the first option marked
  await item.locator('.question-type-toggle[data-type="single"]').click();
  expect(await inputKinds(item)).toEqual(['radio', 'radio', 'radio']);
  expect(await correctMarks(item)).toEqual([true, false, false]);
  await expect(item.locator('.min-correct-container')).toBeHidden();
  await item.locator('.option-item').nth(1).locator('input[type="radio"]').check();
  expect(await correctMarks(item)).toEqual([false, true, false]);

  // Radio buttons become checkboxes with their marks
  await item.locator('.question-type-toggle[data-type="multiple"]').click();
  expect(await correctMarks(item)).toEqual([false, true, false]);

  // A text question keeps the options aside
  await item.locator('.question-type-toggle[data-type="text"]').click();
  await expect(item.locator('.choice-question-fields')).toBeHidden();
  await item.locator('.question-type-toggle[data-type="single"]').click();
  expect(await correctMarks(item)).toEqual([true, false, false]);
  await expect(item.locator('.question-type-toggle.active')).toHaveAttribute('data-type', 'single');
});

test('save checks the file name and the questions first', async ({ page, launch }) => {
  const server = await launch();
  await adminLogin(page, server.url);
  await page.locator('#create-quiz-btn').click();

  await page.locator('#save-quiz-btn').click();
  await expect(messages(page, 'error').last()).toHaveText("Будь ласка, введіть ім'я файлу");

  await page.locator('#quiz-filename').fill('empty');
  await page.locator('#save-quiz-btn').click();
  await expect(messages(page, 'error').last()).toHaveText('Будь ласка, додайте принаймні одне питання');

  // The server refuses a name that is taken
  const item = questionItems(page).first();
  await item.locator('.question-text').fill('Q');
  await item.locator('.option-text').nth(0).fill('A');
  await item.locator('.option-text').nth(1).fill('B');
  await page.locator('#quiz-filename').fill('test_quiz');
  await page.locator('#save-continue-btn').click();
  await expect(messages(page, 'error').last()).toHaveText('Quiz file already exists');
  await expect(page.locator('#save-continue-btn')).toHaveText('❌ Помилка');
  await expect(page.locator('#save-continue-btn')).toHaveText('💾 Зберегти і Продовжити');
  await expect(page.locator('#quiz-editor-modal')).toBeVisible();
});

test('questions move with the arrows and the drag handle', async ({ page, request, launch }) => {
  const server = await launch({ quizzes: quizWith([choice('Q-A'), choice('Q-B'), choice('Q-C')]) });
  await adminLogin(page, server.url);
  await selectAndEditQuiz(page);

  const items = questionItems(page);
  const previews = page.locator('.question-item .question-preview');
  await expect(items.nth(0).locator('.move-up-btn')).toHaveCount(0);
  await expect(items.nth(2).locator('.move-down-btn')).toHaveCount(0);

  await items.nth(0).locator('.move-down-btn').click();
  await expect(previews).toHaveText(['Q-B', 'Q-A', 'Q-C']);
  await expect(page.locator('.question-item .question-number')).toHaveText(['1.', '2.', '3.']);
  // Moving does not unfold the question
  await expect(items.nth(1)).toHaveClass(/collapsed/);

  await items.nth(2).locator('.move-up-btn').click();
  await expect(previews).toHaveText(['Q-B', 'Q-C', 'Q-A']);

  // Dragged by its handle onto the first question, the last one goes before it
  await items.nth(2).locator('.drag-handle').dragTo(items.nth(0).locator('.question-header'));
  await expect(previews).toHaveText(['Q-A', 'Q-B', 'Q-C']);

  // Dragged onto a question below it, a question goes after that one
  await items.nth(0).locator('.drag-handle').dragTo(items.nth(1).locator('.question-header'));
  await expect(previews).toHaveText(['Q-B', 'Q-A', 'Q-C']);

  await saveQuiz(page);
  const saved = await savedQuiz(request, server.url);
  expect((saved.questions as Dict[]).map((question) => question.question)).toEqual(['Q-B', 'Q-A', 'Q-C']);
});

test('a question is removed after a confirmation', async ({ page, launch }) => {
  const server = await launch({ quizzes: quizWith([choice('Q1', { explanation: 'Why' }), choice('Q2')]) });
  await adminLogin(page, server.url);
  await selectAndEditQuiz(page);
  await expect(page.locator('#show-explanation-group')).toBeVisible();

  answerNextDialog(page, false);
  await questionItems(page).first().locator('.remove-question-btn').click();
  await expect(questionItems(page)).toHaveCount(2);

  answerNextDialog(page, true);
  await questionItems(page).first().locator('.remove-question-btn').click();
  await expect(questionItems(page)).toHaveCount(1);
  await expect(page.locator('.question-item .question-preview')).toHaveText(['Q2']);
  await expect(page.locator('.question-item .question-number')).toHaveText(['1.']);
  // No explanation left: the option goes away
  await expect(page.locator('#show-explanation-group')).toBeHidden();
});

test('the image and file pickers fill the fields', async ({ page, request, launch }) => {
  const server = await launch({ quizzes: quizWith([choice('Q1')]) });
  await adminLogin(page, server.url);
  await selectAndEditQuiz(page);
  const item = await expandQuestion(page, 0);
  const imageButton = item.locator('.form-group', { has: page.locator('.question-image') }).locator('.picker-btn');
  const fileButton = item.locator('.form-group', { has: page.locator('.question-file') }).locator('.picker-btn');

  // Empty folders
  await imageButton.click();
  await expect(page.locator('#image-grid')).toContainText('Зображення не знайдені в директорії quizzes/imgs');
  await page.locator('#image-picker-modal .close').click();
  await expect(page.locator('#image-picker-modal')).toHaveCount(0);
  await fileButton.click();
  await expect(page.locator('#file-list')).toContainText('Файли не знайдені в директорії quizzes/attach');
  await page.locator('#file-picker-modal .close').click();

  writeQuizFile(server, 'imgs', 'pic.png', PNG);
  writeQuizFile(server, 'attach', 'data.xlsx', 'hello');

  await imageButton.click();
  await expect(page.locator('#image-grid .image-item .image-filename')).toHaveText(['pic.png']);
  await page.locator('#image-grid .image-item').click();
  await expect(item.locator('.question-image')).toHaveValue('/imgs/pic.png');
  await expect(messages(page, 'success').last()).toHaveText('Зображення вибрано: /imgs/pic.png');
  await expect(item.locator('.image-preview-container')).toHaveClass(/has-image/);

  // The preview opens at full size; Escape closes it
  await item.locator('.image-preview').click();
  await expect(page.locator('#image-fullscreen-modal')).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(page.locator('#image-fullscreen-modal')).toHaveCount(0);

  await fileButton.click();
  await expect(page.locator('#file-list .file-item')).toContainText('data.xlsx');
  await expect(page.locator('#file-list .file-item-size')).toHaveText('5 B');
  await page.locator('#file-list .file-item').click();
  await expect(item.locator('.question-file')).toHaveValue('data.xlsx');
  await expect(messages(page, 'success').last()).toHaveText('Файл вибрано: data.xlsx');

  // An option can be an image too
  await item.locator('.option-item').nth(1).getByTitle('Select image').click();
  await page.locator('#image-grid .image-item').click();
  await expect(item.locator('.option-text').nth(1)).toHaveValue('/imgs/pic.png');
  await expect(item.locator('.option-item').nth(1).locator('.option-image-preview')).toBeVisible();

  const indicators = (await item.locator('.question-indicators').textContent()) || '';
  expect(indicators).toContain('\u{1F5BC}');
  expect(indicators).toContain('📎');

  await saveQuiz(page);
  const question = ((await savedQuiz(request, server.url)).questions as Dict[])[0];
  expect(question).toMatchObject({ image: '/imgs/pic.png', file: 'data.xlsx', options: ['A', '/imgs/pic.png', 'C'] });
});

test('the sticky flag shows the link sign and is saved', async ({ page, request, launch }) => {
  const server = await launch({ quizzes: quizWith([choice('Q1'), choice('Q2')]) });
  await adminLogin(page, server.url);
  await selectAndEditQuiz(page);

  const item = await expandQuestion(page, 1);
  await item.locator('.stick-to-previous-checkbox').check();
  await expect(item.locator('.question-indicators')).toContainText('🔗');

  await saveQuiz(page);
  const questions = (await savedQuiz(request, server.url)).questions as Dict[];
  expect(questions[1].stick_to_the_previous).toBe(true);
  expect('stick_to_the_previous' in questions[0]).toBe(false);
});

test('the quiz settings are saved', async ({ page, request, launch }) => {
  const server = await launch({ quizzes: quizWith([choice('Q1')]) });
  await adminLogin(page, server.url);
  await selectAndEditQuiz(page);

  await page.locator('#quiz-title').fill('New title');
  await page.locator('#randomize-questions').check();
  await page.locator('#show-final-list').uncheck();

  // "Show answers after everyone finishes" is offered only while answers are hidden
  await expect(page.locator('#show-answers-on-completion-group')).toBeHidden();
  await page.locator('#show-right-answer').uncheck();
  await expect(page.locator('#show-answers-on-completion-group')).toBeVisible();
  await page.locator('#show-answers-on-completion').check();
  await page.locator('#show-right-answer').check();
  await expect(page.locator('#show-answers-on-completion-group')).toBeHidden();
  await page.locator('#show-right-answer').uncheck();
  await expect(page.locator('#show-answers-on-completion'), 'showing the answers cleared it').not.toBeChecked();
  await page.locator('#show-answers-on-completion').check();

  await saveQuiz(page);
  const saved = await savedQuiz(request, server.url);
  expect(saved).toMatchObject({
    title: 'New title',
    randomize_questions: true,
    show_final_list: false,
    show_right_answer: false,
    show_answers_on_completion: true,
  });
});

test('a save keeps the quiz description and the keys the editor does not write', async ({ page, launch }) => {
  const server = await launch({
    quizzes: {
      'default.yaml': {
        title: 'Rivers',
        description: 'A quiz about rivers',
        questions: [
          { question: 'Q1', options: ['A', 'B'], correct_answer: 0, hint: 'Think of water' },
          { question: 'T', checker: 'assert True', note: { level: 2 } },
        ],
      },
    },
  });
  await adminLogin(page, server.url);
  await selectAndEditQuiz(page);
  await page.locator('#quiz-title').fill('Rivers and lakes');
  await saveQuiz(page);

  // The file the server wrote
  const onDisk = parse(readFileSync(path.join(server.dir, 'quizzes', 'default.yaml'), 'utf-8')) as Dict;
  expect(onDisk.title).toBe('Rivers and lakes');
  expect(onDisk.description).toBe('A quiz about rivers');
  const questions = onDisk.questions as Dict[];
  expect(questions[0]).toEqual({ question: 'Q1', options: ['A', 'B'], correct_answer: 0, hint: 'Think of water' });
  expect(questions[1]).toEqual({ question: 'T', checker: 'assert True', note: { level: 2 } });
});
