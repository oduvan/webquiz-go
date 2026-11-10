// Ports of tests/test_selenium_explanations.py: the explanation step after an answer.
import { writeFileSync } from 'node:fs';
import path from 'node:path';
import type { Page } from '@playwright/test';
import { EN, expect, hasHorizontalScroll, registerUser, test, waitForQuestionText, type RunningServer } from '../../fixtures/quiz';

// A 1x1 PNG image
const PNG = Buffer.from(
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==',
  'base64',
);

function question(text: string, explanation?: string) {
  const data: Record<string, unknown> = { question: text, options: ['A', 'B'], correct_answer: 0 };
  if (explanation !== undefined) data.explanation = explanation;
  return data;
}

function quiz(questions: unknown[], settings: Record<string, unknown> = {}) {
  return { 'explain.yaml': { title: 'Explain', show_right_answer: true, show_explanation: true, questions, ...settings } };
}

// Puts a file into the quiz images folder (quizzes/imgs) of the test server.
function writeImage(server: RunningServer, name: string, content: string | Buffer) {
  writeFileSync(path.join(server.dir, 'quizzes', 'imgs', name), content);
}

async function answerAndContinue(page: Page) {
  await page.locator('[data-option-index="0"]').click();
  await page.locator('#submit-answer-btn').click();
  await page.locator('#continue-btn').click();
}

async function waitForStep(page: Page) {
  const step = page.locator('#explanation-step');
  await expect(step).toBeVisible({ timeout: 5000 });
  return step;
}

async function waitForProgress(page: Page, text: string) {
  await expect(page.locator('#progress-text')).toContainText(text, { timeout: 5000 });
}

// py: tests/test_selenium_explanations.py::test_explanation_step_between_questions
test('the explanation step comes between two questions', async ({ page, launch }) => {
  const server = await launch({
    config: EN,
    quizzes: quiz([question('Q1?', '**Because** of A'), question('Q2?'), question('Q3?')]),
  });
  await registerUser(page, server.url);
  await answerAndContinue(page);
  const step = await waitForStep(page);
  await expect(step.locator('.explanation-content strong')).toHaveText('Because');
  await expect(step.locator('.explanation-question')).toContainText('Q1?');
  await expect(page.locator('#current-question-container')).toBeHidden();
  await expect(page.locator('#continue-btn')).toBeHidden();
  await page.locator('#explanation-next-btn').click();
  await waitForProgress(page, 'Question 2 of 3');
  await waitForQuestionText(page, 'Q2?');
  await expect(page.locator('#explanation-step')).toBeHidden();
});

// py: tests/test_selenium_explanations.py::test_question_without_explanation_goes_straight_on
test('a question without explanation goes straight on', async ({ page, launch }) => {
  const server = await launch({ config: EN, quizzes: quiz([question('Q1?'), question('Q2?', 'Text')]) });
  await registerUser(page, server.url);
  await answerAndContinue(page);
  await waitForQuestionText(page, 'Q2?');
  await expect(page.locator('#explanation-step')).toBeHidden();
});

// py: tests/test_selenium_explanations.py::test_last_question_explanation_then_results
test('the last explanation, then the results', async ({ page, launch }) => {
  const server = await launch({ config: EN, quizzes: quiz([question('Only?', 'Last words')]) });
  await registerUser(page, server.url);
  await answerAndContinue(page);
  await expect(await waitForStep(page)).toContainText('Last words');
  await page.locator('#explanation-next-btn').click();
  await expect(page.locator('#results')).toContainText('Result', { timeout: 5000 });
  await expect(page.locator('#explanation-step')).toBeHidden();
});

// py: tests/test_selenium_explanations.py::test_no_step_when_right_answer_hidden
test('no step while right answers are hidden', async ({ page, launch }) => {
  const server = await launch({
    config: EN,
    quizzes: quiz([question('Q1?', 'Hidden'), question('Q2?')], { show_right_answer: false }),
  });
  await registerUser(page, server.url);
  await page.locator('[data-option-index="0"]').click();
  await page.locator('#submit-answer-btn').click();
  await waitForQuestionText(page, 'Q2?');
  await expect(page.locator('#explanation-step')).toBeHidden();
});

// py: tests/test_selenium_explanations.py::test_double_click_next_does_not_skip
test('a double click on Next does not skip a question', async ({ page, launch }) => {
  const server = await launch({
    config: EN,
    quizzes: quiz([question('Q1?', 'Read me'), question('Q2?'), question('Q3?')]),
  });
  await registerUser(page, server.url);
  await answerAndContinue(page);
  await waitForStep(page);
  await page.locator('#explanation-next-btn').dblclick();
  await waitForProgress(page, 'Question 2 of 3');
  await waitForQuestionText(page, 'Q2?');
  await page.waitForTimeout(1000); // a skipped question would show by now
  await expect(page.locator('#progress-text')).toContainText('Question 2 of 3');
});

// py: tests/test_selenium_explanations.py::test_whole_quiz_time_up_during_step_shows_results
test('whole-quiz time up during the step shows the results', async ({ page, launch }) => {
  const server = await launch({
    config: EN,
    quizzes: quiz([question('Q1?', 'Slow reading'), question('Q2?')], { time_limit: { whole_quiz: 5 } }),
    env: { WEBQUIZ_TIME_LIMIT_GRACE_SECONDS: '0' },
  });
  await registerUser(page, server.url);
  await answerAndContinue(page);
  await waitForStep(page);
  await expect(page.locator('#results')).toContainText('Result', { timeout: 15000 });
  await expect(page.locator('#explanation-step')).toBeHidden();
});

// py: tests/test_selenium_explanations.py::test_explanation_fits_phone_width
test('a wide explanation fits a phone screen', async ({ page, launch }) => {
  await page.setViewportSize({ width: 375, height: 800 });
  const table = '| a | b | c | d | e | f |\n|---|---|---|---|---|---|\n' + '| long cell text | '.repeat(6) + '\n';
  const explanation = table + '\n![wide](/imgs/wide.svg)\n';
  const server = await launch({ config: EN, quizzes: quiz([question('Q1?', explanation), question('Q2?')]) });
  writeImage(
    server,
    'wide.svg',
    '<svg xmlns="http://www.w3.org/2000/svg" width="1500" height="300"><rect width="1500" height="300"/></svg>',
  );
  await registerUser(page, server.url);
  await answerAndContinue(page);
  const image = (await waitForStep(page)).locator('.explanation-content img');
  await expect.poll(() => image.evaluate((element: HTMLImageElement) => element.naturalWidth), { timeout: 5000 }).toBe(1500);
  expect(await hasHorizontalScroll(page)).toBe(false);
});

// py: tests/test_selenium_explanations.py::test_explanation_image_is_loaded
test('an image in the explanation is loaded', async ({ page, launch }) => {
  const server = await launch({ config: EN, quizzes: quiz([question('Q1?', 'Look: ![dot](/imgs/dot.png)'), question('Q2?')]) });
  writeImage(server, 'dot.png', PNG);
  await registerUser(page, server.url);
  await answerAndContinue(page);
  const image = (await waitForStep(page)).locator('.explanation-content img');
  await expect.poll(() => image.evaluate((element: HTMLImageElement) => element.naturalWidth), { timeout: 5000 }).toBeGreaterThan(0);
});

// py: tests/test_selenium_explanations.py::test_double_click_next_does_not_answer_the_next_question
test('the second click of a double click does not answer the next question', async ({ page, launch }) => {
  const longExplanation = [1, 2, 3, 4, 5].map((n) => `Paragraph ${n} of the explanation.`).join('\n\n');
  const textQuestion = { question: 'Q2: type a number', checker: 'assert to_int(user_answer) == 42', default_value: '41' };
  const server = await launch({ config: EN, quizzes: quiz([question('Q1?', longExplanation), textQuestion]) });
  await registerUser(page, server.url);
  await answerAndContinue(page);
  await waitForStep(page);
  await page.locator('#explanation-next-btn').dblclick();
  await waitForProgress(page, 'Question 2 of 2');
  await expect(page.locator('#text-answer-input')).toBeVisible({ timeout: 5000 });
  await page.waitForTimeout(1000); // a wrongly sent answer would come back by now
  await expect(page.locator('#text-answer-input')).toBeEnabled();
  await expect(page.locator('#submit-answer-btn')).toBeVisible();
  await expect(page.locator('#continue-btn')).toBeHidden();
});

// py: tests/test_selenium_explanations.py::test_question_text_on_the_step_cannot_be_copied
test('the question on the step cannot be copied, the explanation can', async ({ page, launch }) => {
  const server = await launch({ config: EN, quizzes: quiz([question('Q1?', 'Free to copy'), question('Q2?')]) });
  await registerUser(page, server.url);
  await answerAndContinue(page);
  const step = await waitForStep(page);
  const heading = step.locator('.explanation-question');
  const content = step.locator('.explanation-content');
  const copyBlocked = (element: Element) => {
    const event = new ClipboardEvent('copy', { bubbles: true, cancelable: true });
    element.dispatchEvent(event);
    return event.defaultPrevented;
  };
  expect(await heading.evaluate((element) => getComputedStyle(element).userSelect)).toBe('none');
  expect(await heading.evaluate(copyBlocked)).toBe(true);
  expect(await content.evaluate(copyBlocked)).toBe(false);
});

// py: tests/test_selenium_explanations.py::test_next_button_is_on_the_right
test('Next is on the right side of the step', async ({ page, launch }) => {
  const server = await launch({ config: EN, quizzes: quiz([question('Q1?', 'Text'), question('Q2?')]) });
  await registerUser(page, server.url);
  await answerAndContinue(page);
  const step = await waitForStep(page);
  const stepBox = await step.boundingBox();
  const buttonBox = await page.locator('#explanation-next-btn').boundingBox();
  expect(stepBox && buttonBox).toBeTruthy();
  expect(buttonBox!.x + buttonBox!.width).toBeGreaterThan(stepBox!.x + stepBox!.width - 60);
  expect(buttonBox!.x).toBeGreaterThan(stepBox!.x + stepBox!.width / 2);
});
