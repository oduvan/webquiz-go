import { expect, test as base, type Locator, type Page } from '@playwright/test';
import { startServer, type RunningServer, type StartOptions } from './server';

export { expect };
export { startServer };
export type { RunningServer, StartOptions };

// Same cookie the browser gets from the login form: POST the key, the context keeps the session cookie.
export async function adminLogin(page: Page, url: string, key = 'test123') {
  const response = await page.request.post(`${url}/api/admin/auth`, { data: { master_key: key } });
  expect(response.status(), 'admin login').toBe(200);
  await page.goto(`${url}/admin/`);
}

// Opens the quiz page, fills the username (and optional extra fields by name) and registers.
export async function registerStudent(
  page: Page,
  url: string,
  name = 'TestStudent',
  extraFields: Record<string, string> = {},
) {
  await page.goto(`${url}/`);
  await page.locator('#username').fill(name);
  for (const [field, value] of Object.entries(extraFields)) {
    await page.locator(`[name="${field}"]`).fill(value);
  }
  await page.locator('.register-btn').click();
  await page.locator('#current-question-container').waitFor();
}

export async function waitForQuestionText(page: Page, text: string | RegExp): Promise<Locator> {
  const locator = page.locator('.question-text', { hasText: text });
  await expect(locator).toBeVisible();
  return locator;
}

export function options(page: Page): Locator {
  return page.locator('.option');
}

// A test that gets its own server: `server` is started before the test and stopped after it.
// Set options with test.use({ serverOptions: {...} }).
export const test = base.extend<{ serverOptions: StartOptions; server: RunningServer }>({
  serverOptions: [{}, { option: true }],
  server: async ({ serverOptions }, use) => {
    const server = await startServer(serverOptions);
    try {
      await use(server);
    } finally {
      await server.stop();
    }
  },
});
