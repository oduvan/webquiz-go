import { test as base, expect, type BrowserContext, type Locator, type Page } from '@playwright/test';
import { startServer, type RunningServer, type StartOptions } from './server';

export { expect };
export type { RunningServer, StartOptions };

// The ported Selenium tests start a server with their own quiz inside the test,
// like `with custom_webquiz_server(...)` did. `launch` starts one; every server it
// started is stopped after the test.
export const test = base.extend<{ launch: (options?: StartOptions) => Promise<RunningServer> }>({
  launch: async ({}, use) => {
    const started: RunningServer[] = [];
    await use(async (options: StartOptions = {}) => {
      const server = await startServer(options);
      started.push(server);
      return server;
    });
    for (const server of started) {
      await server.stop();
    }
  },
});

type Quiz = Record<string, unknown>;

// One quiz file named default.yaml, so it starts at once.
export function defaultQuiz(quiz: Quiz): Record<string, Quiz> {
  return { 'default.yaml': quiz };
}

export const EN = { language: 'en' };

// ---- Registration (selenium_helpers.register_user) ----

// Opens the quiz, registers and waits until the first question is shown.
export async function registerUser(page: Page, url: string, username = 'TestStudent', fields: Record<string, string> = {}) {
  await page.goto(url + '/');
  await page.locator('#username').fill(username);
  for (const name of Object.keys(fields)) {
    await page.locator(`[data-field-name="${name}"]`).fill(fields[name]);
  }
  await page.locator('.register-btn').click();
  await expect(page.locator('#current-question-container .current-question')).toBeVisible();
}

// ---- Options (find_options, find_option_by_index, find_option_by_text) ----

export function findOptions(page: Page): Locator {
  return page.locator('.quiz-option');
}

export function optionByIndex(page: Page, index: number): Locator {
  return page.locator(`[data-option-index="${index}"]`);
}

// The first option whose text contains `text`, like Selenium's find_option_by_text.
// The locator points at that option by its index, so it stays the same option
// when feedback labels ("✓ Correct answer") appear in the options later.
export async function optionByText(page: Page, text: string): Promise<Locator> {
  const options = findOptions(page);
  await expect(options.first()).toBeVisible();
  const index = await options.evaluateAll(
    (elements, wanted) =>
      elements.findIndex((element) => {
        const label = element.querySelector('.option-text');
        return (label ? label.textContent || '' : '').indexOf(wanted) !== -1;
      }),
    text,
  );
  expect(index, `an option with the text "${text}"`).toBeGreaterThanOrEqual(0);
  return optionByIndex(page, index);
}

// The old page hid elements with the "hidden" class, and the Python tests read that
// class (`"hidden" in element.get_attribute("class")`). The ports read the element's
// own class too: Playwright's toBeHidden() would also pass when only a parent is hidden.
export const HIDDEN_CLASS = /(^|\s)hidden(\s|$)/;

// Reads the class once, without waiting, like a plain Selenium assert.
export async function hasHiddenClass(locator: Locator): Promise<boolean> {
  return HIDDEN_CLASS.test((await locator.getAttribute('class')) || '');
}

export async function expectSelected(option: Locator, selected = true) {
  if (selected) {
    await expect(option).toHaveClass(/(^|\s)selected(\s|$)/);
  } else {
    await expect(option).not.toHaveClass(/(^|\s)selected(\s|$)/);
  }
}

// ---- Waiting for questions ----

export async function waitForQuestionText(page: Page, text: string, timeout = 5000) {
  await expect(page.locator('.question-text')).toContainText(text, { timeout });
}

// Waits until question n of total is on the screen. It replaces the Selenium
// helper wait_for_question_transition, which slept 2.2 seconds.
export async function waitForQuestionNumber(page: Page, n: number, total: number) {
  await expect(page.locator('#progress-text')).toHaveText(new RegExp(`(^|\\D)${n}\\D+${total}(\\D|$)`));
  await expect(page.locator('#current-question-container .current-question')).toBeVisible();
}

export async function submitAnswer(page: Page) {
  await page.locator('#submit-answer-btn').click();
}

// Selenium's element_to_be_clickable: shown and enabled. "Continue" is enabled
// while it is still hidden, so both are checked.
export async function waitForContinue(page: Page, timeout?: number) {
  const button = page.locator('#continue-btn');
  await expect(button).toBeVisible({ timeout });
  await expect(button).toBeEnabled({ timeout });
}

export async function clickContinue(page: Page) {
  await page.locator('#continue-btn').click();
}

// Answers the open question with the option at `index`, then presses "Continue".
export async function answerAndContinue(page: Page, index = 0) {
  await optionByIndex(page, index).click();
  await submitAnswer(page);
  await clickContinue(page);
}

// ---- Server data ----

export async function cookieValue(context: BrowserContext, name: string): Promise<string> {
  const cookie = (await context.cookies()).find((item) => item.name === name);
  expect(cookie, `cookie ${name}`).toBeTruthy();
  return cookie!.value;
}

// Whether the page can be scrolled sideways (it must not on a phone).
export async function hasHorizontalScroll(page: Page): Promise<boolean> {
  return page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth);
}
