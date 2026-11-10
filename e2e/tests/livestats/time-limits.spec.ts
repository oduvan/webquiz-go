// Port of the live stats part of tests/test_selenium_time_limits.py.
import { EN, expect, test } from '../../fixtures/quiz';

// py: tests/test_selenium_time_limits.py::test_live_stats_shows_timeout_icon
test('a timed-out question shows ⏱ and counts as answered', async ({ page, request, launch }) => {
  const server = await launch({
    config: EN,
    quizzes: {
      'timed.yaml': {
        title: 'Timed',
        time_limit: { per_question: 30 },
        questions: [
          { question: 'Q1?', options: ['A', 'B'], correct_answer: 0 },
          { question: 'Q2?', options: ['A', 'B'], correct_answer: 0 },
        ],
      },
    },
  });
  const userId = (await (await request.post(`${server.url}/api/register`, { data: { username: 'slowstudent' } })).json()).user_id;
  await request.post(`${server.url}/api/question-start`, { data: { user_id: userId, question_id: 1 } });
  const response = await request.post(`${server.url}/api/submit-answer`, {
    data: { user_id: userId, question_id: 1, selected_answer: null },
  });
  expect((await response.json()).timed_out).toBe(true);

  await page.goto(server.url + '/live-stats/');
  await expect(page.locator('body')).toContainText('slowstudent', { timeout: 15000 });
  const body = await page.locator('body').innerText();
  expect(body).toContain('⏱');
  expect(body).toContain('0/1'); // the timed-out question counts as answered (and failed)
});
