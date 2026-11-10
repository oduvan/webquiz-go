// The live stats page on a phone (375 px): the page itself never scrolls sideways;
// the table scrolls inside its own box.
import { defaultQuiz, expect, hasHorizontalScroll, test } from '../../fixtures/quiz';

test('the live stats page fits a 375 px screen', async ({ page, request, launch }) => {
  await page.setViewportSize({ width: 375, height: 700 });
  const questions = Array.from({ length: 10 }, (_value, i) => ({
    question: 'A fairly long question number ' + (i + 1) + '?',
    options: ['A', 'B'],
    correct_answer: 0,
  }));
  const server = await launch({ quizzes: defaultQuiz({ title: 'Phone', questions }) });
  for (const name of ['Anna', 'A student with a rather long name']) {
    await request.post(`${server.url}/api/register`, { data: { username: name } });
  }

  await page.goto(server.url + '/live-stats/');
  await expect(page.locator('#statsBody tr')).toHaveCount(2);
  expect(await hasHorizontalScroll(page)).toBe(false);

  // The table is wider than the phone and scrolls inside its box
  const wrapper = page.locator('.table-wrapper');
  expect(await wrapper.evaluate((element) => element.scrollWidth > element.clientWidth)).toBe(true);
  // The filter buttons stay on the screen
  for (const id of ['#filterAll', '#filterInProgress', '#filterCompleted']) {
    const box = await page.locator(id).boundingBox();
    expect(box).toBeTruthy();
    expect(box!.x).toBeGreaterThanOrEqual(0);
    expect(box!.x + box!.width).toBeLessThanOrEqual(375);
  }
});

// The project's rule: 768 px and less is the phone layout (the old page used
// max-width: 768px); 769 px gets the wide layout.
test('768 px is still the phone layout of live stats, 769 px the wide one', async ({ page, request, launch }) => {
  const server = await launch({
    quizzes: defaultQuiz({ title: 'Edge', questions: [{ question: 'Q?', options: ['A', 'B'], correct_answer: 0 }] }),
  });
  await request.post(`${server.url}/api/register`, { data: { username: 'Anna' } });
  const style = (selector: string, property: string) =>
    page.locator(selector).first().evaluate((element, name) => getComputedStyle(element).getPropertyValue(name), property);

  await page.setViewportSize({ width: 768, height: 1024 });
  await page.goto(server.url + '/live-stats/');
  await expect(page.locator('#statsBody tr')).toHaveCount(1);
  expect(await style('.live-page', 'padding-left')).toBe('10px');
  expect(await style('.filter-button', 'font-size')).toBe('14px');
  expect(await style('.stats-table td', 'padding-left')).toBe('4px');
  expect(await style('.state-cell', 'font-size')).toBe('12px');

  await page.setViewportSize({ width: 769, height: 1024 });
  expect(await style('.live-page', 'padding-left')).toBe('20px');
  expect(await style('.filter-button', 'font-size')).toBe('16px');
  expect(await style('.stats-table td', 'padding-left')).toBe('12px');
  expect(await style('.state-cell', 'font-size')).toBe('14px');
});
