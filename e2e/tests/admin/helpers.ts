import { expect, test as base, type APIRequestContext, type Locator, type Page } from '@playwright/test';
import { parse } from 'yaml';
import { startServer, type RunningServer, type StartOptions } from '../../fixtures/server';

export { expect };
export type { RunningServer, StartOptions };

// The admin tests start a server with their own quizzes inside the test, like
// `with custom_webquiz_server(...)` did. `launch` starts one; every server it
// started is stopped after the test.
export const test = base.extend<{ launch: (options?: StartOptions) => Promise<RunningServer> }>({
  // Playwright reads a fixture's needs from its first argument, which must be an object pattern
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

type Dict = Record<string, unknown>;

// ---- Selenium helpers of test_selenium_quiz_editor.py ----

// admin_login: types the master key into the login form and waits for the
// panel and its quiz list (the old helper slept 0.5 s for the list).
export async function adminLogin(page: Page, url: string, masterKey = 'test123') {
  await page.goto(url + '/admin/');
  await page.locator('#master-key').fill(masterKey);
  await page.getByRole('button', { name: 'Увійти' }).click();
  await expect(page.locator('#admin-panel')).toBeVisible();
  await expect(page.locator('#quiz-select option').first()).toBeAttached();
}

export function quizOptions(page: Page): Locator {
  return page.locator('#quiz-select option');
}

// select_and_edit_quiz: selects the quiz at `index` and opens it in the editor
// (the old helper slept 1 s for the questions to load).
export async function selectAndEditQuiz(page: Page, index = 0) {
  await quizOptions(page).nth(index).click();
  await expect(page.locator('#edit-btn')).toBeEnabled();
  await page.locator('#edit-btn').click();
  await expect(page.locator('#quiz-editor-modal')).toBeVisible();
  await expect(page.locator('#modal-title')).toContainText('Редагувати');
}

// select_multiple_quizzes: the first click selects, the next ones add with Ctrl (Cmd on macOS).
export async function selectMultipleQuizzes(page: Page, indices: number[]) {
  for (let i = 0; i < indices.length; i++) {
    await quizOptions(page)
      .nth(indices[i])
      .click(i === 0 ? {} : { modifiers: ['ControlOrMeta'] });
  }
}

// ---- Helpers of test_selenium_admin_decimal_points.py ----

export function questionItems(page: Page): Locator {
  return page.locator('.question-item');
}

// A click by script, like the Selenium helpers did: the editor's sticky
// action bar can sit over the element.
export async function scriptClick(locator: Locator) {
  await locator.evaluate((element) => {
    element.scrollIntoView({ block: 'center' });
    (element as HTMLElement).click();
  });
}

// expand_question: unfolds the question at `index` (the editor collapses them all).
export async function expandQuestion(page: Page, index: number): Promise<Locator> {
  const item = questionItems(page).nth(index);
  if (/(^|\s)collapsed(\s|$)/.test((await item.getAttribute('class')) || '')) {
    await scriptClick(item.locator('.question-header'));
    await expect(item).not.toHaveClass(/(^|\s)collapsed(\s|$)/);
  }
  return item;
}

// set_points: types a points value into the question's points field.
export async function setPoints(page: Page, index: number, value: string): Promise<Locator> {
  const item = await expandQuestion(page, index);
  const field = item.locator('.points-input');
  await field.fill(value);
  return field;
}

// click_save: presses "Save quiz" without waiting.
export async function clickSave(page: Page) {
  await scriptClick(page.getByRole('button', { name: 'Зберегти Quiz' }));
}

// save_quiz: presses "Save quiz" and waits until the editor closes.
export async function saveQuiz(page: Page) {
  await clickSave(page);
  await expect(page.locator('#quiz-editor-modal')).toBeHidden();
}

// error_messages: the texts of the error messages shown now.
export async function errorMessages(page: Page): Promise<string[]> {
  return page.locator('#message-area .message.error').allTextContents();
}

// ---- conftest.py helpers ----

// get_admin_session: a request context with the admin session cookie.
export async function adminSession(request: APIRequestContext, url: string, masterKey = 'test123') {
  const response = await request.post(url + '/api/admin/auth', { data: { master_key: masterKey } });
  expect(response.status(), 'admin login').toBe(200);
  return request;
}

// The text of a quiz file as the server reads it (GET /api/admin/quiz/{name}).
export async function quizContent(request: APIRequestContext, url: string, filename = 'default.yaml'): Promise<string> {
  await adminSession(request, url);
  const response = await request.get(url + '/api/admin/quiz/' + encodeURIComponent(filename));
  expect(response.status()).toBe(200);
  return (await response.json()).content as string;
}

// saved_quiz: the quiz file, parsed.
export async function savedQuiz(request: APIRequestContext, url: string, filename = 'default.yaml'): Promise<Dict> {
  return parse(await quizContent(request, url, filename)) as Dict;
}

// register_student: registers through the API and returns the user id.
export async function registerStudent(request: APIRequestContext, url: string, username = 'Student'): Promise<string> {
  const response = await request.post(url + '/api/register', { data: { username } });
  expect(response.status(), await response.text()).toBe(200);
  return (await response.json()).user_id as string;
}

// ---- Helpers of test_selenium_admin_start_settings.py ----

// open_start_popup: logs in, selects the first quiz and presses the start button.
export async function openStartPopup(page: Page, url: string): Promise<Locator> {
  await adminLogin(page, url);
  await quizOptions(page).first().click();
  await expect(page.locator('#switch-btn')).toBeEnabled();
  await page.locator('#switch-btn').click();
  const modal = page.locator('#start-quiz-modal');
  await expect(modal).toBeVisible();
  return modal;
}
