// Live stats with an answer that waits for the teacher (manual_check): the cell shows
// ✍️ and counts as answered, not right; the teacher's verdict turns it green or red.
import { expect, test } from '../../fixtures/quiz';

test('a waiting answer shows ✍️, then the verdict', async ({ page, browser, request, launch }) => {
  const server = await launch({
    quizzes: {
      'manual.yaml': {
        title: 'Manual',
        questions: [
          { question: 'Why?', manual_check: true },
          { question: '2 + 2?', options: ['3', '4'], correct_answer: 1 },
        ],
      },
    },
  });
  const userId = (await (await request.post(server.url + '/api/register', { data: { username: 'Anna' } })).json()).user_id;
  await page.goto(server.url + '/live-stats/');
  await request.post(server.url + '/api/submit-answer', { data: { user_id: userId, question_id: 1, selected_answer: 'Because' } });

  const cell = page.locator(`#cell-${userId}-1`);
  await expect(cell).toHaveClass(/state-pending/);
  await expect(cell).toContainText('✍️');

  const teacher = await (await browser.newContext()).newPage();
  await teacher.request.post(server.url + '/api/admin/auth', { data: { master_key: 'test123' } });
  await teacher.goto(server.url + '/review/');
  await teacher.locator('#verdict-correct').click();
  await expect(cell).toHaveClass(/state-ok/);
  await expect(cell).toContainText('🟢');
  await teacher.context().close();
});
