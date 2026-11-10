import { mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import path from 'node:path';
import { expect, test } from '../../fixtures/helpers';

const TRUSTED = { admin: { master_key: 'test123', trusted_ips: ['127.0.0.1'] } };

test.describe('File manager', () => {
  test.use({ serverOptions: { config: TRUSTED } });

  test('config form save changes the file and keeps disabled fields', async ({ page, server }) => {
    await page.goto(`${server.url}/files/`);
    await page.locator('#config-tab-button').click();

    await page.locator('#cfg-language').selectOption('en');
    await page.locator('#cfg-reg-approve').check();
    await page.locator('#cfg-reg-username-label').fill('Student name');
    await page.getByRole('button', { name: '+ Add Field' }).click();
    await page.getByRole('button', { name: '+ Add Field' }).click();
    const rows = page.locator('#cfg-reg-fields-list .config-list-item');
    await rows.nth(0).locator('input[type="text"]').fill('group');
    await rows.nth(1).locator('input[type="text"]').fill('team');
    await rows.nth(1).locator('input[type="checkbox"]').uncheck();
    await page.getByRole('button', { name: /Save Configuration/ }).click();

    await expect(page.locator('#config-message-area .message.success')).toBeVisible();
    const saved = readFileSync(path.join(server.dir, 'webquiz.yaml'), 'utf-8');
    expect(saved).toMatch(/language: en/);
    expect(saved).toMatch(/approve: true/);
    expect(saved).toContain('Student name');
    expect(saved).toMatch(/fields:\s*\n\s*- group/);
    expect(saved).toMatch(/disabled_fields:\s*\n\s*- team/);
    // The form shows the saved values again, the disabled field stays unchecked
    await expect(rows).toHaveCount(2);
    await expect(rows.nth(1).locator('input[type="checkbox"]')).not.toBeChecked();
  });

  test('transfer checkboxes are absent while transfer is not available', async ({ page, server }) => {
    await page.goto(`${server.url}/files/`);
    await expect(page.locator('#csv-files')).toBeVisible();
    await page.locator('.tab[data-tab="quizzes"]').click();
    await expect(page.locator('#quizzes-files .file-item[data-name="test_quiz.yaml"]')).toBeVisible();
    await expect(page.locator('#transfer-panel')).toHaveCount(0);
    await expect(page.locator('.transfer-checkbox')).toHaveCount(0);
  });

  // Python's test_send_panel_hidden_until_tunnel_connected checked both states of
  // the panel. The test above and TestSendPanelHiddenUntilTunnelConnected (Go)
  // check the hidden state; this one checks that the panel and the checkboxes
  // appear once GET /api/admin/transfer says "available". A real "available"
  // needs a connected tunnel, so the browser gets a made-up state with the
  // JSON shape of transfer.State.
  test('send panel and checkboxes appear when transfer is available, and Send posts the chosen files', async ({ page, server }) => {
    mkdirSync(path.join(server.dir, 'data'), { recursive: true });
    writeFileSync(path.join(server.dir, 'data', 'math_0001.csv'), 'user_id,question\r\n100001,What is 2 + 2?\r\n', 'utf-8');
    await page.route('**/api/admin/transfer', (route) =>
      route.fulfill({ json: { available: true, name: 'room-a', outgoing: [], incoming: [] } }),
    );
    let sendBody: unknown = null;
    await page.route('**/api/admin/transfer/send', async (route) => {
      sendBody = route.request().postDataJSON();
      await route.fulfill({
        json: {
          success: true,
          offer: {
            id: 'o1',
            to: 'room-b',
            files: [
              { type: 'csv', name: 'math_0001.csv', size: 37, sha256: '' },
              { type: 'quizzes', name: 'test_quiz.yaml', size: 120, sha256: '' },
            ],
            created: '2026-11-10T10:00:00',
            status: 'waiting',
          },
        },
      });
    });

    await page.goto(`${server.url}/files/`);
    await expect(page.locator('#transfer-panel')).toBeVisible();
    await expect(page.locator('#transfer-own-name')).toHaveText('room-a');
    await expect(page.locator('#transfer-selected-count')).toHaveText('0 files selected');

    const csvItem = page.locator('#csv-files .file-item[data-name="math_0001.csv"]');
    await csvItem.locator('.transfer-checkbox').check();
    await page.locator('.tab[data-tab="quizzes"]').click();
    const quizItem = page.locator('#quizzes-files .file-item[data-name="test_quiz.yaml"]');
    await expect(quizItem.locator('.transfer-checkbox')).toBeVisible();
    await quizItem.locator('.transfer-checkbox').check();
    await expect(page.locator('#transfer-selected-count')).toHaveText('2 files selected');

    await page.locator('#transfer-target').fill('room-b');
    await page.locator('#transfer-send-btn').click();

    await expect(page.locator('#message-area .message.success', { hasText: 'Request sent to room-b.' })).toBeVisible();
    expect(sendBody).toEqual({
      to: 'room-b',
      files: [
        { type: 'csv', name: 'math_0001.csv' },
        { type: 'quizzes', name: 'test_quiz.yaml' },
      ],
    });
    // A sent offer clears the selection
    await expect(page.locator('#transfer-selected-count')).toHaveText('0 files selected');
    await expect(quizItem.locator('.transfer-checkbox')).not.toBeChecked();
  });

  test('the Text button of a CSV file shows the file text', async ({ page, server }) => {
    const text = 'user_id,question,selected_answer\r\n100001,"Name the capital, of France",Paris\r\n';
    mkdirSync(path.join(server.dir, 'data'), { recursive: true });
    writeFileSync(path.join(server.dir, 'data', 'geo_0001.csv'), text, 'utf-8');

    await page.goto(`${server.url}/files/`);
    const item = page.locator('#csv-files .file-item[data-name="geo_0001.csv"]');
    await item.getByRole('button', { name: '📄 Text' }).click();

    const viewer = page.locator('#file-viewer-modal');
    await expect(viewer).toBeVisible();
    await expect(viewer.locator('#file-viewer-title')).toHaveText('geo_0001.csv');
    await expect(viewer.locator('#file-content')).toContainText('user_id,question,selected_answer');
    await expect(viewer.locator('#file-content')).toContainText('100001,"Name the capital, of France",Paris');
    // Text view: no table frame
    await expect(viewer.locator('#csv-table-frame')).toHaveCount(0);

    await viewer.getByRole('button', { name: 'Close' }).click();
    await expect(viewer).toHaveCount(0);
  });

  test('config tab keeps the saved values and unsaved edits across tab changes', async ({ page, server }) => {
    await page.goto(`${server.url}/files/`);
    await page.locator('#config-tab-button').click();
    await page.locator('#cfg-reg-username-label').fill('First label');
    await page.getByRole('button', { name: /Save Configuration/ }).click();
    await expect(page.locator('#config-message-area .message.success')).toBeVisible();

    // Away and back: the form shows the saved value
    await page.locator('.tab[data-tab="logs"]').click();
    await page.locator('#config-tab-button').click();
    await expect(page.locator('#cfg-reg-username-label')).toHaveValue('First label');

    // An unsaved edit also survives a tab change
    await page.locator('#cfg-reg-approve').check();
    await page.locator('.tab[data-tab="csv"]').click();
    await page.locator('#config-tab-button').click();
    await expect(page.locator('#cfg-reg-approve')).toBeChecked();

    // A second save keeps the first change
    await page.getByRole('button', { name: /Save Configuration/ }).click();
    await expect(page.locator('#config-message-area .message.success')).toBeVisible();
    const saved = readFileSync(path.join(server.dir, 'webquiz.yaml'), 'utf-8');
    expect(saved).toContain('First label');
    expect(saved).toMatch(/approve: true/);
  });

  test('config YAML view saves its text and the form follows', async ({ page, server }) => {
    await page.goto(`${server.url}/files/`);
    await page.locator('#config-tab-button').click();
    await page.locator('.config-view-toggle [data-view="yaml"]').click();
    const editor = page.locator('#config-editor');
    const text = await editor.inputValue();
    await editor.fill(text + '\nlanguage: en\n');
    await page.getByRole('button', { name: /Save Configuration/ }).click();
    await expect(page.locator('#config-message-area .message.success')).toBeVisible();
    expect(readFileSync(path.join(server.dir, 'webquiz.yaml'), 'utf-8')).toMatch(/language: en/);
    await page.locator('.config-view-toggle [data-view="form"]').click();
    await expect(page.locator('#cfg-language')).toHaveValue('en');
  });

  test('switching Form and YAML asks before dropping unsaved changes', async ({ page, server }) => {
    await page.goto(`${server.url}/files/`);
    await page.locator('#config-tab-button').click();
    await page.locator('#cfg-reg-username-label').fill('Changed');
    const messages: string[] = [];
    page.on('dialog', async (dialog) => {
      messages.push(dialog.message());
      await dialog.dismiss();
    });
    await page.locator('.config-view-toggle [data-view="yaml"]').click();
    await expect.poll(() => messages.length).toBe(1);
    expect(messages[0]).toContain('unsaved changes');
    await expect(page.locator('#cfg-reg-username-label')).toBeVisible();
    page.removeAllListeners('dialog');
    page.on('dialog', (dialog) => void dialog.accept());
    await page.locator('.config-view-toggle [data-view="yaml"]').click();
    await expect(page.locator('#config-editor')).toBeVisible();
  });

  test('YAML validate button reports a valid quiz and a broken one', async ({ page, server }) => {
    await page.goto(`${server.url}/files/`);
    await page.locator('.tab[data-tab="quizzes"]').click();
    await page.locator('#quizzes-files .file-item[data-name="test_quiz.yaml"]').getByRole('button', { name: /Edit/ }).click();

    const editor = page.locator('#quiz-editor-content');
    await expect(editor).toHaveValue(/What is 2 \+ 2\?/);
    await page.getByRole('button', { name: /Validate YAML/ }).click();
    await expect(page.locator('#quiz-editor-message-area .message.success')).toContainText('Valid YAML! Found 1 questions.');

    await editor.fill('title: Broken\nquestions:\n  - question: Only text\n');
    await page.getByRole('button', { name: /Validate YAML/ }).click();
    await expect(page.locator('#quiz-editor-message-area .message.error')).toContainText('Validation errors');
  });

  test('the panel opens for a trusted address and again after a reload', async ({ page, server }) => {
    await page.goto(`${server.url}/files/`);
    await expect(page.locator('#files-panel')).toBeVisible();
    await page.reload();
    await expect(page.locator('#files-panel')).toBeVisible();
  });
});

test.describe('File manager login', () => {
  test('wrong key is refused, right key opens the panel', async ({ page, server }) => {
    await page.goto(`${server.url}/files/`);
    await page.locator('#master-key').fill('wrong');
    await page.getByRole('button', { name: 'Login' }).click();
    await expect(page.locator('#message-area .message.error')).toBeVisible();
    await expect(page.locator('#files-panel')).toHaveCount(0);

    await page.locator('#master-key').fill('test123');
    await page.locator('#master-key').press('Enter');
    await expect(page.locator('#files-panel')).toBeVisible();
    await expect(page.locator('#transfer-panel')).toHaveCount(0);
  });
});
