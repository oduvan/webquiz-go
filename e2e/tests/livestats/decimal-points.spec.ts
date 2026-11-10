// Ports of tests/test_selenium_live_stats_decimal_points.py: clean decimal points on the
// live stats page, for the student's score and for a question's points.
import { defaultQuiz, expect, test } from '../../fixtures/quiz';

// py: tests/test_selenium_live_stats_decimal_points.py::test_live_stats_shows_clean_decimal_score
test('a 0.1 + 0.2 score reads 0.3', async ({ page, request, launch }) => {
  const server = await launch({
    quizzes: defaultQuiz({
      title: 'Live Decimal Quiz',
      questions: [
        { question: 'Q1', options: ['A', 'B'], correct_answer: 0, points: 0.1 },
        { question: 'Q2', options: ['A', 'B'], correct_answer: 0, points: 0.2 },
        { question: 'Q3', options: ['A', 'B'], correct_answer: 0, points: 0.7 },
      ],
    }),
  });
  const userId = (await (await request.post(`${server.url}/api/register`, { data: { username: 'livedecimal' } })).json()).user_id;
  // Two right (0.1 + 0.2), one wrong
  for (const [questionId, answer] of [
    [1, 0],
    [2, 0],
    [3, 1],
  ]) {
    await request.post(`${server.url}/api/submit-answer`, {
      data: { user_id: userId, question_id: questionId, selected_answer: answer },
    });
  }

  await page.goto(server.url + '/live-stats/');
  await expect(page.locator('body')).toContainText('livedecimal', { timeout: 15000 });
  const body = await page.locator('body').innerText();
  expect(body).not.toContain('0.30000000000000004');
  expect(body).toContain('0.3/1');
});

// py: tests/test_selenium_live_stats_decimal_points.py::test_live_stats_shows_decimal_question_points
test('a 0.5 point question shows 0.5 in its header', async ({ page, request, launch }) => {
  const server = await launch({
    quizzes: defaultQuiz({
      title: 'Live Indicator Quiz',
      questions: [{ question: 'Half point question', options: ['A', 'B'], correct_answer: 0, points: 0.5 }],
    }),
  });
  await request.post(`${server.url}/api/register`, { data: { username: 'indicatoruser' } });

  await page.goto(server.url + '/live-stats/');
  const indicator = page.locator('.points-indicator').first();
  await expect(indicator).toBeAttached({ timeout: 15000 });
  expect(await indicator.innerHTML()).toContain('0.5');
});
