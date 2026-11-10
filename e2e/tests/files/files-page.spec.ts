import { readFileSync } from 'node:fs';
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
