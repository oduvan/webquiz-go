import { readFileSync } from 'node:fs';
import path from 'node:path';
import type { Page } from '@playwright/test';
import { root } from '../../fixtures/server';
import { adminLogin, adminSession, expect, quizOptions, selectMultipleQuizzes, test } from './helpers';

// Admin panel behavior of the old admin.html that no Python test covered:
// login, the quiz list actions, students waiting for approval, "show answers
// to everyone", quizzes to download and the footer banners.

const TWO_QUIZZES = {
  'quiz1.yaml': { title: 'Quiz 1', questions: [{ question: 'Q1?', options: ['A', 'B'], correct_answer: 0 }] },
  'quiz2.yaml': { title: 'Quiz 2', questions: [{ question: 'Q2?', options: ['C', 'D'], correct_answer: 1 }] },
};

function messages(page: Page, type: 'success' | 'error' | 'warning') {
  return page.locator(`#message-area .message.${type}`);
}

// Answers the next dialog (confirm or prompt) and returns its text.
function answerDialog(page: Page, accept: boolean, promptValue?: string): Promise<{ message: string; defaultValue: string }> {
  return new Promise((resolve) => {
    page.once('dialog', async (dialog) => {
      const seen = { message: dialog.message(), defaultValue: dialog.defaultValue() };
      // A prompt answered without text would give an empty name: keep its suggestion
      if (accept) await dialog.accept(promptValue !== undefined ? promptValue : dialog.defaultValue());
      else await dialog.dismiss();
      resolve(seen);
    });
  });
}

test.describe('login', () => {
  test('a wrong key shows the server message and keeps the form', async ({ page, launch }) => {
    const server = await launch();
    await page.goto(server.url + '/admin/');

    await page.getByRole('button', { name: 'Увійти' }).click();
    await expect(messages(page, 'error')).toContainText('Будь ласка, введіть ключ');

    await page.locator('#master-key').fill('wrong');
    await page.locator('#master-key').press('Enter');
    await expect(messages(page, 'error').last()).toContainText('Недійсний або відсутній головний ключ');
    await expect(page.locator('#admin-panel')).toBeHidden();
    await expect(page.locator('#master-key')).toBeVisible();
  });

  test('a saved session opens the panel after a reload', async ({ page, launch }) => {
    const server = await launch();
    await adminLogin(page, server.url);
    await expect(messages(page, 'success')).toContainText('Успішна автентифікація!');

    await page.reload();
    await expect(page.locator('#admin-panel')).toBeVisible();
    await expect(messages(page, 'success')).toContainText('Сесію відновлено');
    await expect(page.locator('#master-key')).toBeHidden();
  });

  test('a trusted address opens the panel without the key', async ({ page, launch }) => {
    const server = await launch({ config: { admin: { trusted_ips: ['127.0.0.1'] } } });
    await page.goto(server.url + '/admin/');
    await expect(page.locator('#admin-panel')).toBeVisible();
    await expect(messages(page, 'success')).toContainText('Авто-автентифікація з довіреного IP');
    await expect(quizOptions(page)).toHaveCount(1);
  });
});

test.describe('quiz list', () => {
  test('starting and restarting a quiz', async ({ page, launch }) => {
    const server = await launch({ quizzes: TWO_QUIZZES });
    await adminLogin(page, server.url);
    await expect(page.locator('#current-quiz-display')).toHaveText('Поточний quiz: не вибрано');
    await expect(page.locator('#switch-btn')).toBeDisabled();

    await quizOptions(page).nth(1).click();
    await expect(page.locator('#switch-btn')).toHaveText('Переключитися на Вибраний Quiz');
    await page.locator('#switch-btn').click();
    await expect(page.locator('#start-quiz-name')).toHaveText('quiz2.yaml');
    await page.locator('#start-quiz-confirm').click();

    await expect(page.locator('#start-quiz-modal')).toBeHidden();
    await expect(messages(page, 'success').last()).toContainText('Successfully switched to "quiz2.yaml". CSV file: quiz2_0001.csv');
    await expect(page.locator('#current-quiz-display')).toHaveText('Поточний quiz: quiz2.yaml - Quiz 2');
    await expect(quizOptions(page).nth(1)).toHaveText('quiz2.yaml - Quiz 2 (поточний)');
    // The selection is cleared after a start
    await expect(page.locator('#switch-btn')).toBeDisabled();

    // The running quiz restarts; a bad value keeps the pop-up open with the server's message
    await quizOptions(page).nth(1).click();
    await expect(page.locator('#switch-btn')).toHaveText('Перезапустити Поточний Quiz');
    await page.locator('#switch-btn').click();
    await expect(page.locator('#start-quiz-name')).toHaveText('quiz2.yaml (перезапуск)');
    await page.locator('#start-time-limit-mode').selectOption('whole_quiz');
    await expect(page.locator('#start-time-limit-hint')).toBeHidden();
    await page.locator('#start-time-limit-seconds').fill('-5');
    await page.locator('#start-quiz-confirm').click();
    await expect(page.locator('#start-quiz-error')).toContainText("'time_limit.whole_quiz' must be a whole number of seconds");
    await expect(page.locator('#start-quiz-modal')).toBeVisible();
    await expect(page.locator('#switch-btn')).toHaveText('Restart Current Quiz');

    await page.locator('#start-time-limit-mode').selectOption('per_question');
    await expect(page.locator('#start-time-limit-hint')).toBeVisible();
    await page.locator('#start-time-limit-seconds').fill('30');
    await page.locator('#start-quiz-confirm').click();
    await expect(page.locator('#start-quiz-modal')).toBeHidden();
    await expect(messages(page, 'success').last()).toContainText('Successfully restarted "quiz2.yaml"');
  });

  test('a reloaded list sets the start button text again', async ({ page, launch }) => {
    const server = await launch({ quizzes: TWO_QUIZZES });
    await adminLogin(page, server.url);
    await quizOptions(page).nth(1).click();
    await page.locator('#switch-btn').click();
    await page.locator('#start-time-limit-mode').selectOption('whole_quiz');
    await page.locator('#start-time-limit-seconds').fill('-5');
    await page.locator('#start-quiz-confirm').click();
    await expect(page.locator('#start-quiz-error')).toBeVisible();
    await page.locator('#start-quiz-cancel').click();
    // The old page's text after a failed start
    await expect(page.locator('#switch-btn')).toHaveText('Switch to Selected Quiz');

    await page.locator('#single-quiz-actions .refresh-btn').click();
    await expect(page.locator('#switch-btn')).toHaveText('Переключитися на Вибраний Quiz');
    await expect(page.locator('#switch-btn')).toBeEnabled();
  });

  test('the refresh button reloads the list', async ({ page, request, launch }) => {
    const server = await launch({ quizzes: TWO_QUIZZES });
    await adminLogin(page, server.url);
    await adminSession(request, server.url);
    const created = await request.post(server.url + '/api/admin/create-quiz', {
      data: { filename: 'quiz3', mode: 'wizard', quiz_data: { questions: [{ question: 'Q', options: ['A', 'B'], correct_answer: 0 }] } },
    });
    expect(created.status()).toBe(200);

    await page.locator('#single-quiz-actions .refresh-btn').click();
    await expect(messages(page, 'success').last()).toHaveText('Список quiz оновлено');
    await expect(quizOptions(page)).toHaveText(['quiz1.yaml - Quiz 1', 'quiz2.yaml - Quiz 2', 'quiz3.yaml']);
  });

  test('duplicate asks for a name and opens the copy', async ({ page, launch }) => {
    const server = await launch({ quizzes: TWO_QUIZZES });
    await adminLogin(page, server.url);
    await quizOptions(page).nth(0).click();

    const dialog = answerDialog(page, true);
    await page.locator('#duplicate-btn').click();
    expect(await dialog).toEqual({ message: "Введіть ім'я для дубльованого quiz:", defaultValue: 'quiz1_copy' });

    await expect(messages(page, 'success').last()).toContainText('Quiz duplicated successfully as "quiz1_copy.yaml"');
    await expect(page.locator('#modal-title')).toHaveText('Редагувати Quiz: quiz1_copy.yaml');
    await expect(page.locator('#quiz-title')).toHaveValue('Quiz 1');
    await expect(quizOptions(page)).toHaveText(['quiz1.yaml - Quiz 1', 'quiz1_copy.yaml - Quiz 1', 'quiz2.yaml - Quiz 2']);
  });

  test('delete asks first and removes the selected quizzes', async ({ page, launch }) => {
    const server = await launch({
      quizzes: { ...TWO_QUIZZES, 'quiz3.yaml': { questions: [{ question: 'Q3?', options: ['E', 'F'], correct_answer: 0 }] } },
    });
    await adminLogin(page, server.url);

    // "No" keeps the quiz
    await quizOptions(page).nth(1).click();
    let dialog = answerDialog(page, false);
    await page.locator('#delete-btn').click();
    expect((await dialog).message).toBe('Ви впевнені, що хочете видалити "quiz2.yaml"? Ця дія не може бути скасована.');
    await expect(quizOptions(page)).toHaveCount(3);

    // Two at once
    await selectMultipleQuizzes(page, [1, 2]);
    dialog = answerDialog(page, true);
    await page.locator('#delete-btn').click();
    expect((await dialog).message).toBe('Ви впевнені, що хочете видалити 2 quiz(ів)? Ця дія не може бути скасована.');
    await expect(messages(page, 'success').last()).toHaveText('Успішно видалено 2 quiz(ів)');
    await expect(quizOptions(page)).toHaveText(['quiz1.yaml - Quiz 1']);
  });

  test('unite asks for a name and opens the united quiz', async ({ page, launch }) => {
    const server = await launch({ quizzes: TWO_QUIZZES });
    await adminLogin(page, server.url);
    await selectMultipleQuizzes(page, [0, 1]);

    const dialog = answerDialog(page, true);
    await page.locator('#unite-btn').click();
    expect(await dialog).toEqual({ message: "Введіть ім'я для об'єднаного quiz (2 quizzes):", defaultValue: 'quiz1_united' });

    await expect(messages(page, 'success').last()).toContainText('Quiz успішно об\'єднано в "quiz1_united.yaml". Загалом питань: 2');
    await expect(page.locator('#modal-title')).toHaveText('Редагувати Quiz: quiz1_united.yaml');
    await expect(page.locator('.question-item .question-preview')).toHaveText(['Q1?', 'Q2?']);
    // The list now has the united quiz, and the selection is cleared
    await page.locator('#cancel-editor-btn').click();
    await expect(quizOptions(page)).toHaveCount(3);
    await expect(page.locator('#single-quiz-actions')).toBeVisible();
  });

  test('a quiz is renamed by its file name in the editor', async ({ page, launch }) => {
    const server = await launch({ quizzes: TWO_QUIZZES });
    await adminLogin(page, server.url);
    await quizOptions(page).nth(1).click();
    await page.locator('#edit-btn').click();
    await expect(page.locator('#quiz-filename')).toHaveValue('quiz2');

    await page.locator('#quiz-filename').fill('renamed');
    await page.locator('#save-quiz-btn').click();

    await expect(page.locator('#quiz-editor-modal')).toBeHidden();
    await expect(messages(page, 'success').last()).toHaveText('Quiz "renamed.yaml" updated successfully');
    await expect(quizOptions(page)).toHaveText(['quiz1.yaml - Quiz 1', 'renamed.yaml - Quiz 2']);
  });
});

test.describe('students and answers', () => {
  test('students waiting for approval are listed and approved', async ({ page, request, launch }) => {
    const server = await launch({ config: { registration: { approve: true, fields: ['Group'] } } });
    const response = await request.post(server.url + '/api/register', { data: { username: 'Alice', group: '10-A' } });
    const alice = (await response.json()).user_id as string;

    await adminLogin(page, server.url);
    const items = page.locator('.pending-user-item');
    await expect(page.locator('#pending-approvals-section')).toBeVisible();
    await expect(items).toHaveCount(1);
    await expect(items.first().locator('.pending-user-name')).toHaveText('👤 Alice');
    await expect(items.first()).toContainText('Group: 10-A');

    // A new student shows up through the admin socket
    const bob = await request.post(server.url + '/api/register', { data: { username: 'Bob', group: '10-B' } });
    expect(bob.status()).toBe(200);
    await expect(items).toHaveCount(2);
    await expect(messages(page, 'success').last()).toHaveText('Новий користувач: Bob');

    await page.locator(`#pending-user-${alice} .approve-btn`).click();
    await expect(messages(page, 'success').last()).toHaveText('Користувача підтверджено!');
    await expect(items).toHaveCount(1);
    await expect(items.first().locator('.pending-user-name')).toHaveText('👤 Bob');
    const verify = await (await request.get(server.url + '/api/verify-user/' + alice)).json();
    expect(verify.approved).toBe(true);
  });

  test('without approval the waiting list stays hidden', async ({ page, launch }) => {
    const server = await launch();
    await adminLogin(page, server.url);
    await expect(page.locator('#pending-approvals-section')).toBeHidden();
    await expect(page.locator('#show-answers-section')).toBeHidden();
  });

  test('the answers can be opened for everyone', async ({ page, launch }) => {
    const server = await launch({
      quizzes: {
        'default.yaml': {
          show_right_answer: false,
          show_answers_on_completion: true,
          questions: [{ question: 'Q?', options: ['A', 'B'], correct_answer: 0 }],
        },
      },
    });
    await adminLogin(page, server.url);
    await expect(page.locator('#show-answers-section')).toBeVisible();
    await expect(page.locator('#answers-status')).toBeHidden();

    const dialog = answerDialog(page, true);
    await page.locator('#show-answers-btn').click();
    expect((await dialog).message).toContain('Ви впевнені, що хочете відкрити відповіді для всіх учнів?');

    await expect(messages(page, 'success').first()).toBeVisible();
    await expect(page.locator('#message-area')).toContainText('Відповіді відкрито! (0/0 учнів завершили тест)');
    await expect(page.locator('#answers-status')).toHaveText('✓ Відповіді відкрито для всіх учнів');
    await expect(page.locator('#show-answers-btn')).toBeDisabled();
    await expect(page.locator('#show-answers-btn')).toHaveText('✓ Відповіді Вже Відкриті');

    // A reload reads the state from the server
    await page.reload();
    await expect(page.locator('#show-answers-btn')).toBeDisabled();
  });
});

test.describe('quizzes to download', () => {
  test('a configured quiz archive downloads after a confirmation', async ({ page, launch }) => {
    const server = await launch({
      config: { quizzes: [{ name: 'Remote set', download_path: 'https://example.com/set.zip', folder: 'data' }] },
    });
    let sent: unknown = null;
    await page.route('**/api/admin/download-quiz', async (route) => {
      sent = route.request().postDataJSON();
      await route.fulfill({ json: { success: true, message: 'Quiz downloaded' } });
    });
    await adminLogin(page, server.url);

    const item = page.locator('.downloadable-quiz-item');
    await expect(item).toContainText('Remote set');
    await expect(item).toContainText('https://example.com/set.zip');
    await expect(item).toContainText('Папка: data');

    const dialog = answerDialog(page, true);
    await page.locator('#download-btn-0').click();
    expect((await dialog).message).toContain('Завантажити quiz "Remote set"?');
    await expect(messages(page, 'success').last()).toHaveText('✅ Quiz downloaded');
    expect(sent).toEqual({ name: 'Remote set', download_path: 'https://example.com/set.zip', folder: 'data' });
    await expect(page.locator('#download-btn-0')).toHaveText('📥 Завантажити');
  });

  test('a failed download shows the server message', async ({ page, launch }) => {
    const server = await launch({
      config: { quizzes: [{ name: 'Remote set', download_path: 'https://example.com/set.zip', folder: 'data' }] },
    });
    await page.route('**/api/admin/download-quiz', (route) => route.fulfill({ status: 500, json: { error: 'Network error: refused' } }));
    await adminLogin(page, server.url);

    answerDialog(page, true);
    await page.locator('#download-btn-0').click();
    await expect(messages(page, 'error').last()).toHaveText('❌ Помилка завантаження: Network error: refused');
  });

  test('without configured quizzes the section is hidden', async ({ page, launch }) => {
    const server = await launch();
    await adminLogin(page, server.url);
    await expect(page.locator('#downloadable-quizzes-section')).toBeHidden();
  });
});

test.describe('footer', () => {
  async function bootData(page: Page): Promise<{ version: string; repo_url: string }> {
    return page.evaluate(() => JSON.parse(document.getElementById('webquiz-boot')!.textContent || '{}'));
  }

  test('a newer release on GitHub shows the update banner', async ({ page, launch }) => {
    const server = await launch();
    let asked = '';
    await page.route('https://api.github.com/repos/**/releases/latest', async (route) => {
      asked = route.request().url();
      await route.fulfill({ json: { tag_name: 'v99.0' } });
    });
    await adminLogin(page, server.url);
    const boot = await bootData(page);

    await expect(page.locator('#version-number')).toHaveText(boot.version);
    await expect(page.locator('#footer-docs-link a')).toHaveAttribute(
      'href',
      `${boot.repo_url}/releases/download/v${boot.version}/webquiz-documentation-uk.pdf`,
    );
    const update = page.locator('#footer-update');
    await expect(update).toBeVisible();
    await expect(update).toContainText('⬆️ Update available: v99.0');
    await expect(update.locator('a')).toHaveAttribute('href', `${boot.repo_url}/releases/tag/v99.0`);
    // The address comes from the repository of the boot data
    expect(asked).toBe(boot.repo_url.replace('https://github.com/', 'https://api.github.com/repos/') + '/releases/latest');
  });

  test('the same release on GitHub shows no banner', async ({ page, launch }) => {
    const server = await launch();
    const version = readFileSync(path.join(root, 'VERSION'), 'utf-8').trim();
    let answered = false;
    await page.route('https://api.github.com/repos/**/releases/latest', async (route) => {
      answered = true;
      await route.fulfill({ json: { tag_name: 'v' + version } });
    });
    await adminLogin(page, server.url);
    await expect.poll(() => answered).toBe(true);
    await expect(page.locator('#footer-update')).toBeHidden();
  });

  test('a replaced program file shows "restart required"', async ({ page, launch }) => {
    const server = await launch();
    await page.route('https://api.github.com/repos/**/releases/latest', (route) => route.fulfill({ json: { tag_name: 'v99.0' } }));
    await page.route('**/api/admin/version-check', (route) =>
      route.fulfill({ json: { running_version: '2.0', file_version: '99.1', restart_required: true } }),
    );
    await adminLogin(page, server.url);

    // The first check runs 2 seconds after the page opens
    await expect(page.locator('#footer-restart')).toBeVisible();
    await expect(page.locator('#footer-restart')).toHaveText('New version installed (v99.1), restart required');
    await expect(page.locator('#new-version-number')).toHaveText('99.1');
    await expect(page.locator('#footer-update')).toBeHidden();
  });
});

test.describe('phones', () => {
  test('the panel and the editor fit a phone screen', async ({ page, launch }) => {
    await page.setViewportSize({ width: 375, height: 800 });
    const server = await launch({
      config: { registration: { approve: true, fields: ['A very long registration field name'] } },
      quizzes: {
        'a_very_long_quiz_file_name_that_does_not_fit_on_a_phone_screen.yaml': {
          title: 'A quiz title that is long as well, longer than the screen of a phone',
          questions: [{ question: 'Q?', options: ['A', 'B'], correct_answer: 0, explanation: 'Why' }],
        },
      },
    });
    await registerLongNamedStudent(page, server.url);
    await adminLogin(page, server.url);
    await expect(page.locator('.pending-user-item')).toHaveCount(1);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);

    await quizOptions(page).first().click();
    await page.locator('#edit-btn').click();
    await expect(page.locator('#quiz-editor-modal')).toBeVisible();
    await page.locator('.question-item .question-header').click();
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
    const box = await page.locator('#quiz-editor-modal .modal-content').boundingBox();
    expect(box && box.x >= 0 && box.x + box.width <= 375).toBe(true);
    await expect(page.locator('#save-continue-btn')).toBeVisible();
  });
});

// Registers a student with a long name and a long field value.
async function registerLongNamedStudent(page: Page, url: string) {
  const response = await page.request.post(url + '/api/register', {
    data: { username: 'Олександра-Анастасія Довгопрізвищенко', a_very_long_registration_field_name: 'x'.repeat(60) },
  });
  expect(response.status()).toBe(200);
}
