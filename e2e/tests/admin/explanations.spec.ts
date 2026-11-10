import { mkdirSync, writeFileSync } from 'node:fs';
import path from 'node:path';
import type { Locator, Page } from '@playwright/test';
import {
  adminLogin,
  expandQuestion,
  expect,
  openStartPopup,
  registerStudent,
  saveQuiz,
  savedQuiz,
  scriptClick,
  selectAndEditQuiz,
  test,
  type RunningServer,
} from './helpers';

// tests/test_selenium_admin_explanations.py: question explanations in the
// quiz editor (fold, preview, image insert, save) and in the start pop-up.

// A 1x1 PNG image
const PNG = Buffer.from(
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==',
  'base64',
);

function quiz(explanation: string | null = null, settings: Record<string, unknown> = {}) {
  const first: Record<string, unknown> = { question: 'Q1', options: ['A', 'B'], correct_answer: 0 };
  if (explanation !== null) first.explanation = explanation;
  return {
    'default.yaml': {
      title: 'Editor Explanations',
      questions: [first, { question: 'Q2', options: ['A', 'B'], correct_answer: 1 }],
      ...settings,
    },
  };
}

function writeImage(server: RunningServer, name: string) {
  const folder = path.join(server.dir, 'quizzes', 'imgs');
  mkdirSync(folder, { recursive: true });
  writeFileSync(path.join(folder, name), PNG);
}

function explanationField(item: Locator): Locator {
  return item.locator('textarea.question-explanation');
}

function previewBox(item: Locator): Locator {
  return item.locator('.explanation-preview');
}

async function unfoldExplanation(item: Locator) {
  await scriptClick(item.locator('.explanation-toggle-btn'));
  await expect(explanationField(item)).toBeVisible();
}

async function togglePreview(item: Locator) {
  await scriptClick(item.locator('.explanation-preview-btn'));
}

async function openFirstQuestion(page: Page, url: string): Promise<Locator> {
  await adminLogin(page, url);
  await selectAndEditQuiz(page);
  return expandQuestion(page, 0);
}

// Opens the image picker of the question's explanation and picks an image.
async function insertImage(page: Page, item: Locator, filename: string) {
  await scriptClick(item.locator('.explanation-image-btn'));
  const image = page.locator(`#image-grid .image-item[title="${filename}"]`);
  await expect(image).toBeVisible();
  await image.click();
  await expect(page.locator('#image-picker-modal')).toBeHidden();
}

// py: tests/test_selenium_admin_explanations.py::test_empty_explanation_is_folded
test('an empty explanation is folded', async ({ page, launch }) => {
  const server = await launch({ quizzes: quiz() });
  const item = await openFirstQuestion(page, server.url);

  const toggle = item.locator('.explanation-toggle-btn');
  await expect(toggle).toBeVisible();
  await expect(toggle).toContainText('Додати пояснення');
  await expect(explanationField(item)).toBeHidden();

  await unfoldExplanation(item);
  await expect(item.locator('.explanation-preview-btn')).toBeVisible();
});

// py: tests/test_selenium_admin_explanations.py::test_existing_explanation_is_unfolded
test('a written explanation opens unfolded', async ({ page, launch }) => {
  const server = await launch({ quizzes: quiz('Loaded *text*', { show_explanation: true }) });
  const item = await openFirstQuestion(page, server.url);

  await expect(explanationField(item)).toBeVisible();
  await expect(explanationField(item)).toHaveValue('Loaded *text*');
  await expect(previewBox(item)).toBeHidden();
  await expect(page.locator('#show-explanation-group')).toBeVisible();
  await expect(page.locator('#show-explanation')).toBeChecked();

  const second = await expandQuestion(page, 1);
  await expect(explanationField(second)).toBeHidden();
});

// py: tests/test_selenium_admin_explanations.py::test_preview_button_replaces_the_field
test('the preview takes the place of the field', async ({ page, launch }) => {
  const server = await launch({ quizzes: quiz() });
  const item = await openFirstQuestion(page, server.url);
  await unfoldExplanation(item);
  await explanationField(item).fill('**Bold** text');

  // No preview until the button is pressed
  await page.waitForTimeout(600);
  await expect(previewBox(item)).toBeHidden();

  await togglePreview(item);
  await expect(previewBox(item).locator('strong')).toHaveText('Bold');
  await expect(previewBox(item)).toBeVisible();
  await expect(explanationField(item)).toBeHidden();
  await expect(item.locator('.explanation-image-btn')).toBeHidden();

  await togglePreview(item);
  await expect(explanationField(item)).toBeVisible();
  await expect(previewBox(item)).toBeHidden();
  await expect(explanationField(item)).toHaveValue('**Bold** text');
});

// py: tests/test_selenium_admin_explanations.py::test_editor_show_explanation_appears_with_text
test('the "show explanations" option appears with some text', async ({ page, launch }) => {
  const server = await launch({ quizzes: quiz() });
  const item = await openFirstQuestion(page, server.url);

  const group = page.locator('#show-explanation-group');
  await expect(group).toBeHidden();
  await unfoldExplanation(item);
  await explanationField(item).pressSequentially('Because');
  await expect(group).toBeVisible();
  await explanationField(item).clear();
  await explanationField(item).pressSequentially(' ');
  await expect(group).toBeHidden();
});

// py: tests/test_selenium_admin_explanations.py::test_editor_saves_explanation_and_option
test('the editor saves the explanation and the option', async ({ page, request, launch }) => {
  const server = await launch({ quizzes: quiz() });
  const item = await openFirstQuestion(page, server.url);
  await unfoldExplanation(item);
  await explanationField(item).fill('Because **A**');

  const checkbox = page.locator('#show-explanation');
  await expect(checkbox).toBeVisible();
  if (!(await checkbox.isChecked())) await checkbox.click();
  await saveQuiz(page);

  const saved = await savedQuiz(request, server.url);
  const questions = saved.questions as Record<string, unknown>[];
  expect(questions[0].explanation).toBe('Because **A**');
  expect('explanation' in questions[1]).toBe(false);
  expect(saved.show_explanation).toBe(true);
});

// py: tests/test_selenium_admin_explanations.py::test_editor_inserts_image_markdown
test('the editor inserts the Markdown of a picked image', async ({ page, launch }) => {
  const server = await launch({ quizzes: quiz() });
  writeImage(server, 'pic.png');
  const item = await openFirstQuestion(page, server.url);
  await unfoldExplanation(item);

  await scriptClick(item.locator('.explanation-image-btn'));
  const image = page.locator('#image-grid .image-item').first();
  await expect(image).toBeVisible();
  await image.click();
  await expect(explanationField(item)).toHaveValue(/!\[\]\(\/imgs\/pic\.png\)/);

  // The preview shows the image (it loads)
  await togglePreview(item);
  const previewImage = previewBox(item).locator('img');
  await expect(previewImage).toBeAttached();
  await expect.poll(() => previewImage.evaluate((img) => (img as HTMLImageElement).naturalWidth)).toBeGreaterThan(0);
});

// py: tests/test_selenium_admin_explanations.py::test_start_popup_explanation_checkbox
test('the start pop-up offers explanations only with correct answers', async ({ page, launch }) => {
  const server = await launch({ quizzes: quiz('Why', { show_explanation: true }) });
  await openStartPopup(page, server.url);

  const checkbox = page.locator('#start-show-explanation');
  await expect(checkbox).toBeVisible();
  await expect(checkbox).toBeChecked();
  await expect(checkbox).toBeEnabled();

  await page.locator('#start-show-right-answer').click(); // untick "show correct answers"
  await expect(checkbox).toBeDisabled();
  await expect(page.locator('#start-show-explanation-note')).toBeVisible();
});

// py: tests/test_selenium_admin_explanations.py::test_start_popup_hides_explanation_checkbox_without_explanations
test('the start pop-up hides the explanation option for a quiz without explanations', async ({ page, launch }) => {
  const server = await launch({ quizzes: quiz() });
  await openStartPopup(page, server.url);

  await expect(page.locator('#start-time-limit-mode')).toBeVisible();
  await expect(page.locator('#start-show-explanation-group')).toBeHidden();
});

// py: tests/test_selenium_admin_explanations.py::test_start_popup_sends_show_explanation
test('the start pop-up turns explanations off for the run', async ({ page, request, launch }) => {
  const server = await launch({ quizzes: quiz('Why', { show_explanation: true, show_right_answer: true }) });
  await openStartPopup(page, server.url);

  const checkbox = page.locator('#start-show-explanation');
  await expect(checkbox).toBeVisible();
  await expect(checkbox).toBeChecked();
  await checkbox.click();
  await page.locator('#start-quiz-confirm').click();
  await expect(page.locator('#start-quiz-modal')).toBeHidden();

  const userId = await registerStudent(request, server.url);
  const reply = await (
    await request.post(server.url + '/api/submit-answer', { data: { user_id: userId, question_id: 1, selected_answer: 0 } })
  ).json();
  expect('explanation_html' in reply).toBe(false);
});

// py: tests/test_selenium_admin_explanations.py::test_editor_uses_angle_brackets_for_unusual_image_names
// The Python test called the page's global insertExplanationImage() with three
// paths. The new page has no globals, so the same three images are picked with
// the image picker, which inserts them the same way.
test('names with spaces or brackets get angle brackets in Markdown', async ({ page, launch }) => {
  const server = await launch({ quizzes: quiz() });
  for (const name of ['pic(1).png', 'my pic.png', 'plain.png']) writeImage(server, name);
  const item = await openFirstQuestion(page, server.url);
  await unfoldExplanation(item);

  for (const name of ['pic(1).png', 'my pic.png', 'plain.png']) {
    await insertImage(page, item, name);
  }

  await expect(explanationField(item)).toHaveValue('![](</imgs/pic(1).png>)![](</imgs/my pic.png>)![](/imgs/plain.png)');
});
