import { adminLogin, expandQuestion, expect, saveQuiz, savedQuiz, selectAndEditQuiz, test } from './helpers';

// tests/test_selenium_admin_time_limits.py: the time limit fields of the
// quiz editor (quiz-wide mode and seconds, and a question's own limit).

function timedQuiz(timeLimit: Record<string, number> | null) {
  const data: Record<string, unknown> = {
    title: 'Editor Time Limits',
    questions: [
      { question: 'Q1', options: ['A', 'B'], correct_answer: 0, time_limit: 10 },
      { question: 'Q2', options: ['A', 'B'], correct_answer: 1 },
    ],
  };
  if (timeLimit !== null) data.time_limit = timeLimit;
  return { 'default.yaml': data };
}

type Question = Record<string, unknown>;

// py: tests/test_selenium_admin_time_limits.py::test_editor_loads_and_saves_per_question_time_limit
test('the editor loads and saves per-question time limits', async ({ page, request, launch }) => {
  const server = await launch({ quizzes: timedQuiz({ per_question: 30 }) });
  await adminLogin(page, server.url);
  await selectAndEditQuiz(page);

  await expect(page.locator('#time-limit-mode')).toHaveValue('per_question');
  await expect(page.locator('#time-limit-seconds')).toHaveValue('30');
  const item = await expandQuestion(page, 0);
  const questionField = item.locator('.question-time-limit-input');
  await expect(questionField).toHaveValue('10');
  await expect(questionField).toBeVisible();

  await saveQuiz(page);

  const quiz = await savedQuiz(request, server.url);
  expect(quiz.time_limit).toEqual({ per_question: 30 });
  const questions = quiz.questions as Question[];
  expect(questions[0].time_limit).toBe(10);
  expect('time_limit' in questions[1]).toBe(false);
});

// py: tests/test_selenium_admin_time_limits.py::test_editor_hides_question_field_outside_per_question_mode
test('the question field is hidden outside per-question mode but kept', async ({ page, request, launch }) => {
  const server = await launch({ quizzes: timedQuiz({ per_question: 30 }) });
  await adminLogin(page, server.url);
  await selectAndEditQuiz(page);

  await page.locator('#time-limit-mode').selectOption('whole_quiz');
  await page.locator('#time-limit-seconds').fill('600');

  const item = await expandQuestion(page, 0);
  await expect(item.locator('.question-time-limit-group')).toBeHidden();

  await saveQuiz(page);

  const quiz = await savedQuiz(request, server.url);
  expect(quiz.time_limit).toEqual({ whole_quiz: 600 });
  // kept while hidden
  expect((quiz.questions as Question[])[0].time_limit).toBe(10);
});

// py: tests/test_selenium_admin_time_limits.py::test_editor_sets_none_removes_time_limit
test('"no time limit" removes the quiz time limit', async ({ page, request, launch }) => {
  const server = await launch({ quizzes: timedQuiz({ whole_quiz: 900 }) });
  await adminLogin(page, server.url);
  await selectAndEditQuiz(page);

  await page.locator('#time-limit-mode').selectOption('none');
  await expect(page.locator('#time-limit-seconds')).toBeHidden();

  await saveQuiz(page);

  expect('time_limit' in (await savedQuiz(request, server.url))).toBe(false);
});
