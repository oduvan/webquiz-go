import { readFileSync } from 'node:fs';
import path from 'node:path';
import { parse } from 'yaml';
import type { APIRequestContext } from '@playwright/test';
import { expect, openStartPopup, registerStudent, test } from './helpers';

// tests/test_selenium_admin_start_settings.py: the pop-up shown when the admin
// starts a quiz. The quiz file gives its values; changes apply to this run only.

function quiz(settings: Record<string, unknown> = {}) {
  return {
    'q.yaml': {
      title: 'Start Pop-up',
      questions: [
        { question: 'Q1?', options: ['A', 'B'], correct_answer: 0 },
        { question: 'Q2?', options: ['A', 'B'], correct_answer: 1 },
      ],
      ...settings,
    },
  };
}

// The boot data of the quiz page. Python's page had `let showFinalList = ...;`
// in its script; the Go page has the same values in its boot JSON.
async function quizPageBoot(request: APIRequestContext, url: string): Promise<Record<string, unknown>> {
  const html = await (await request.get(url + '/')).text();
  const match = /<script id="webquiz-boot" type="application\/json">([\s\S]*?)<\/script>/.exec(html);
  expect(match, 'the quiz page has its boot data').not.toBeNull();
  return JSON.parse(match![1]);
}

// py: tests/test_selenium_admin_start_settings.py::test_start_popup_shows_quiz_file_values
test('the start pop-up shows the values of the quiz file', async ({ page, launch }) => {
  const server = await launch({
    quizzes: quiz({
      show_right_answer: false,
      show_answers_on_completion: true,
      randomize_questions: true,
      show_final_list: false,
      time_limit: { per_question: 45 },
    }),
  });
  await openStartPopup(page, server.url);
  await expect(page.locator('#start-time-limit-seconds')).toHaveValue('45');

  await expect(page.locator('#start-show-right-answer')).not.toBeChecked();
  await expect(page.locator('#start-show-answers-on-completion')).toBeVisible();
  await expect(page.locator('#start-show-answers-on-completion')).toBeChecked();
  await expect(page.locator('#start-randomize-questions')).toBeChecked();
  await expect(page.locator('#start-show-final-list')).not.toBeChecked();
  await expect(page.locator('#start-time-limit-mode')).toHaveValue('per_question');
});

// py: tests/test_selenium_admin_start_settings.py::test_start_popup_changes_apply_only_to_the_running_quiz
test('changes in the start pop-up apply only to the running quiz', async ({ page, request, launch }) => {
  const server = await launch({ quizzes: quiz({ show_final_list: true, time_limit: { per_question: 45 } }) });
  const quizPath = path.join(server.dir, 'quizzes', 'q.yaml');
  const before = readFileSync(quizPath, 'utf-8');

  await openStartPopup(page, server.url);
  const seconds = page.locator('#start-time-limit-seconds');
  await expect(seconds).toHaveValue('45');
  await seconds.fill('20');
  await page.locator('#start-show-final-list').click(); // turn it off
  await page.locator('#start-quiz-confirm').click();
  await expect(page.locator('#start-quiz-modal')).toBeHidden();

  const userId = await registerStudent(request, server.url);
  const started = await (
    await request.post(server.url + '/api/question-start', { data: { user_id: userId, question_id: 1 } })
  ).json();
  expect(started.remaining_seconds).toBeGreaterThan(19);
  expect(started.remaining_seconds).toBeLessThanOrEqual(20);
  expect((await quizPageBoot(request, server.url)).show_final_list).toBe(false);

  // The quiz file is not changed
  expect(readFileSync(quizPath, 'utf-8')).toBe(before);
  expect((parse(before) as Record<string, unknown>).time_limit).toEqual({ per_question: 45 });
});

// py: tests/test_selenium_admin_start_settings.py::test_start_popup_cancel_does_not_restart_the_quiz
test('cancel in the start pop-up does not restart the quiz', async ({ page, request, launch }) => {
  const server = await launch({ quizzes: quiz() });
  const userId = await registerStudent(request, server.url);

  await openStartPopup(page, server.url);
  await page.locator('#start-quiz-cancel').click();
  await expect(page.locator('#start-quiz-modal')).toBeHidden();

  // A restart would clear all students
  const verify = await (await request.get(server.url + '/api/verify-user/' + userId)).json();
  expect(verify.valid).toBe(true);
});

// py: tests/test_selenium_admin_start_settings.py::test_start_popup_fits_mobile_width
test('the start pop-up fits a phone screen', async ({ page, launch }) => {
  await page.setViewportSize({ width: 375, height: 800 });
  const server = await launch({ quizzes: quiz({ time_limit: { whole_quiz: 600 } }) });
  await openStartPopup(page, server.url);
  await expect(page.locator('#start-quiz-confirm')).toBeAttached();

  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  await expect(page.locator('#start-quiz-confirm')).toBeVisible();
});
