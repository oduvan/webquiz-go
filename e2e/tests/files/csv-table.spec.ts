import { mkdirSync, writeFileSync } from 'node:fs';
import path from 'node:path';
import { expect, test, type Page } from '../../fixtures/helpers';

// Requests from the browser come from 127.0.0.1, so a trusted IP skips the login
const TRUSTED = { admin: { master_key: 'test123', trusted_ips: ['127.0.0.1'] } };

const MULTILINE_QUESTION = 'Name the capital,\nof France';

const ANSWERS_WITH_USERS = [
  ['user_id', 'username', 'group', 'question', 'selected_answer', 'correct_answer', 'is_correct', 'time_taken_seconds'],
  ['100001', 'Alice', 'A1', 'What is 2 + 2?', '4', '4', 'True', '3.1'],
  ['100001', 'Alice', 'A1', MULTILINE_QUESTION, 'Paris', 'Paris', 'True', '2.0'],
  ['100002', 'Bob', 'B2', 'What is 2 + 2?', '5', '4', 'False', '4.2'],
  ['100002', 'Bob', 'B2', MULTILINE_QUESTION, 'Rome', 'Paris', 'False', '6.0'],
  ['100003', 'Carol', 'A1', 'What is 2 + 2?', '4', '4', 'True', '1.0'],
];

const ANSWERS = [
  ['user_id', 'question', 'selected_answer', 'correct_answer', 'is_correct', 'time_taken_seconds'],
  ['100001', 'What is 2 + 2?', '4', '4', 'True', '3.1'],
  ['100002', 'What is 2 + 2?', '5', '4', 'False', '4.2'],
];

const USERS = [
  ['user_id', 'username', 'registered_at', 'total_questions_asked', 'correct_answers', 'earned_points', 'total_points', 'total_time'],
  ['100001', 'Alice', '2026-11-04T10:00:00', '1', '1', '1', '1', '0:03'],
  ['100002', 'Bob', '2026-11-04T10:00:05', '1', '0', '0', '1', '0:04'],
];

const WITH_USERS_NAME = 'math_0003.answers_with_users.csv';

// Writes a CSV file the way the server does (quoted fields, CRLF line ends).
function csvLine(row: string[]): string {
  return row.map((cell) => (/[",\r\n]/.test(cell) ? `"${cell.replace(/"/g, '""')}"` : cell)).join(',');
}

function writeCsv(dir: string, name: string, rows: string[][]) {
  const csvDir = path.join(dir, 'data');
  mkdirSync(csvDir, { recursive: true });
  writeFileSync(path.join(csvDir, name), rows.map(csvLine).join('\r\n') + '\r\n', 'utf-8');
}

async function openTable(page: Page, url: string, name: string, query = '') {
  await page.goto(`${url}/files/csv/${encodeURIComponent(name)}${query}`);
  await waitForTable(page);
}

async function waitForTable(page: Page) {
  await page.locator('#table-container tbody tr').first().waitFor();
}

// The table body as a list of rows, each a list of cell texts.
async function tableRows(page: Page): Promise<string[][]> {
  return page.locator('#table-container tbody tr').evaluateAll((trs) =>
    trs.map((tr) => Array.from(tr.querySelectorAll('td')).map((td) => td.textContent ?? '')),
  );
}

async function optionTexts(page: Page, selectId: string): Promise<string[]> {
  return page.locator(`#${selectId} option`).evaluateAll((options) => options.map((o) => (o.textContent ?? '').trim()));
}

test.describe('CSV table viewer', () => {
  test.use({ serverOptions: { config: TRUSTED } });

  // py: tests/test_selenium_csv_table_viewer.py::test_filter_by_question
  test('filter by question', async ({ page, server }) => {
    writeCsv(server.dir, WITH_USERS_NAME, ANSWERS_WITH_USERS);
    await openTable(page, server.url, WITH_USERS_NAME);

    await page.locator('#question-filter').selectOption({ label: 'What is 2 + 2?' });

    await expect(page.locator('#row-count')).toHaveText('Showing 3 of 5 rows');
    const rows = await tableRows(page);
    expect(rows).toHaveLength(3);
    expect(rows.every((row) => row[3] === 'What is 2 + 2?')).toBe(true);
  });

  // py: tests/test_selenium_csv_table_viewer.py::test_filter_by_user
  test('filter by user', async ({ page, server }) => {
    writeCsv(server.dir, WITH_USERS_NAME, ANSWERS_WITH_USERS);
    await openTable(page, server.url, WITH_USERS_NAME);

    expect(await optionTexts(page, 'user-filter')).toEqual([
      'All users (3)',
      'Alice (100001)',
      'Bob (100002)',
      'Carol (100003)',
    ]);

    await page.locator('#user-filter').selectOption({ label: 'Bob (100002)' });

    await expect(page.locator('#row-count')).toHaveText('Showing 2 of 5 rows');
    const rows = await tableRows(page);
    expect(rows).toHaveLength(2);
    expect(rows.every((row) => row[1] === 'Bob')).toBe(true);
  });

  // py: tests/test_selenium_csv_table_viewer.py::test_filters_combine
  test('filters combine', async ({ page, server }) => {
    writeCsv(server.dir, WITH_USERS_NAME, ANSWERS_WITH_USERS);
    await openTable(page, server.url, WITH_USERS_NAME);

    await page.locator('#question-filter').selectOption({ label: 'What is 2 + 2?' });
    await page.locator('#user-filter').selectOption({ label: 'Bob (100002)' });

    await expect(page.locator('#table-container tbody tr')).toHaveCount(1);
    const rows = await tableRows(page);
    expect(rows[0][1]).toBe('Bob');
    expect(rows[0][4]).toBe('5');
  });

  // py: tests/test_selenium_csv_table_viewer.py::test_filters_are_kept_in_page_address
  test('filters are kept in the page address', async ({ page, server }) => {
    writeCsv(server.dir, WITH_USERS_NAME, ANSWERS_WITH_USERS);
    await openTable(page, server.url, WITH_USERS_NAME, '?question=' + encodeURIComponent('What is 2 + 2?') + '&user=100002');

    await expect(page.locator('#question-filter')).toHaveValue('What is 2 + 2?');
    await expect(page.locator('#user-filter')).toHaveValue('100002');
    expect(await tableRows(page)).toHaveLength(1);

    await page.locator('#user-filter').selectOption({ label: 'Alice (100001)' });

    await expect.poll(() => page.url()).toContain('user=100001');
    expect(page.url()).toContain('question=');
  });

  // py: tests/test_selenium_csv_table_viewer.py::test_quoted_field_with_line_break_stays_one_cell
  test('quoted field with a line break stays one cell', async ({ page, server }) => {
    writeCsv(server.dir, WITH_USERS_NAME, ANSWERS_WITH_USERS);
    await openTable(page, server.url, WITH_USERS_NAME);

    const rows = await tableRows(page);
    expect(rows).toHaveLength(5);
    expect(rows.every((row) => row.length === 8)).toBe(true);
    expect(rows[1][3]).toBe(MULTILINE_QUESTION);
    expect(await optionTexts(page, 'question-filter')).toContain('What is 2 + 2?');
  });

  // py: tests/test_selenium_csv_table_viewer.py::test_answers_csv_takes_user_names_from_users_csv
  test('answers CSV takes user names from the users CSV', async ({ page, server }) => {
    writeCsv(server.dir, 'math_0001.csv', ANSWERS);
    writeCsv(server.dir, 'math_0001.users.csv', USERS);
    await openTable(page, server.url, 'math_0001.csv');

    await expect.poll(() => optionTexts(page, 'user-filter')).toContain('Alice (100001)');
    expect(await optionTexts(page, 'user-filter')).toContain('Bob (100002)');
  });

  // py: tests/test_selenium_csv_table_viewer.py::test_answers_csv_without_users_csv_shows_user_ids
  test('answers CSV without a users CSV shows user IDs', async ({ page, server }) => {
    writeCsv(server.dir, 'math_0001.csv', ANSWERS);
    await openTable(page, server.url, 'math_0001.csv');

    expect(await optionTexts(page, 'user-filter')).toEqual(['All users (2)', '100001', '100002']);
  });

  // py: tests/test_selenium_csv_table_viewer.py::test_users_csv_has_no_question_filter
  test('users CSV has no question filter', async ({ page, server }) => {
    writeCsv(server.dir, 'math_0001.users.csv', USERS);
    await openTable(page, server.url, 'math_0001.users.csv');

    await expect(page.locator('#question-filter-group')).toBeHidden();
    await expect(page.locator('#user-filter-group')).toBeVisible();
  });

  // py: tests/test_selenium_csv_table_viewer.py::test_popup_table_opens_in_new_window_with_same_filters
  test('pop-up table opens in a new window with the same filters', async ({ page, server, context }) => {
    writeCsv(server.dir, WITH_USERS_NAME, ANSWERS_WITH_USERS);
    await page.goto(`${server.url}/files/`);

    const item = page.locator(`#csv-files .file-item[data-name="${WITH_USERS_NAME}"]`);
    await item.getByRole('button', { name: /Table/ }).click();

    await expect(page.locator('#csv-table-frame')).toBeVisible();
    const frame = page.frameLocator('#csv-table-frame');
    await expect(frame.locator('#table-container tbody tr')).toHaveCount(5);

    await frame.locator('#user-filter').selectOption({ label: 'Bob (100002)' });
    await expect(frame.locator('#table-container tbody tr')).toHaveCount(2);

    const [popup] = await Promise.all([context.waitForEvent('page'), frame.locator('#open-new-window').click()]);

    await popup.waitForURL(/\/files\/csv\//);
    expect(popup.url()).toContain('user=100002');
    await waitForTable(popup);
    const rows = await tableRows(popup);
    expect(rows).toHaveLength(2);
    expect(rows.every((row) => row[1] === 'Bob')).toBe(true);
    // In its own window the page has no "open in new window" button
    await expect(popup.locator('#open-new-window')).toHaveCount(0);
  });

  // py: tests/test_selenium_csv_table_viewer.py::test_file_list_link_opens_table_in_new_window
  test('file list link opens the table in a new window', async ({ page, server, context }) => {
    writeCsv(server.dir, WITH_USERS_NAME, ANSWERS_WITH_USERS);
    await page.goto(`${server.url}/files/`);

    const link = page.locator(`#csv-files .file-item[data-name="${WITH_USERS_NAME}"] a.btn-new-window`);
    await expect(link).toHaveAttribute('target', '_blank');
    await expect(link).toHaveAttribute('href', `/files/csv/${encodeURIComponent(WITH_USERS_NAME)}`);
    expect(await link.evaluate((a) => (a as HTMLAnchorElement).href)).toBe(`${server.url}/files/csv/${encodeURIComponent(WITH_USERS_NAME)}`);

    const [popup] = await Promise.all([context.waitForEvent('page'), link.click()]);
    await waitForTable(popup);
    expect(await tableRows(popup)).toHaveLength(5);
  });

  // py: tests/test_selenium_csv_table_viewer.py::test_page_fits_phone_screen
  test('page fits a phone screen', async ({ page, server }) => {
    writeCsv(server.dir, WITH_USERS_NAME, ANSWERS_WITH_USERS);
    await page.setViewportSize({ width: 375, height: 800 });
    await openTable(page, server.url, WITH_USERS_NAME);

    const widths = await page.evaluate(() => ({
      page: document.documentElement.scrollWidth,
      view: document.documentElement.clientWidth,
    }));
    expect(widths.page, `Page is ${widths.page}px wide in a ${widths.view}px window`).toBeLessThanOrEqual(widths.view);
  });
});

test.describe('CSV table viewer without a session', () => {
  // py: tests/test_selenium_csv_table_viewer.py::test_page_asks_to_log_in_without_session
  test('page asks to log in without a session', async ({ page, server }) => {
    writeCsv(server.dir, WITH_USERS_NAME, ANSWERS_WITH_USERS);
    await page.goto(`${server.url}/files/csv/${encodeURIComponent(WITH_USERS_NAME)}`);

    const message = page.locator('#csv-message');
    await expect(message).toContainText(/log in/i);
    await expect(message.locator('a')).toHaveAttribute('href', '/files/');
    expect(await tableRows(page)).toHaveLength(0);
  });
});
