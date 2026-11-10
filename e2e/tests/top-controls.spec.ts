// The theme button (every page) and the student's name badge (quiz page) sit at the
// top of the page and scroll away with it, as in 1.19. There they were placed with
// position: absolute (index.html:51-66, 886-891; admin.html:1169-1176, 1224-1226;
// files.html:528-535, 720-722; live_stats.html:403-408, 432-434) or stood in the
// page header (csv_table.html:294). When they stayed fixed on the screen, they
// covered the text under them on a phone. At the top of the page they must not
// cover the title or the first controls.
import { mkdirSync, writeFileSync } from 'node:fs';
import path from 'node:path';
import type { Locator, Page } from '@playwright/test';
import { adminLogin } from '../fixtures/helpers';
import { defaultQuiz, expect, test, waitForQuestionNumber } from '../fixtures/quiz';

const PHONE = { width: 375, height: 667 };
const DESKTOP = { width: 1280, height: 800 };
const DISTANCE = 300;

interface Box {
  x: number;
  y: number;
  width: number;
  height: number;
}

async function boxOf(locator: Locator): Promise<Box> {
  const box = await locator.boundingBox();
  expect(box, `${locator} is on the page`).toBeTruthy();
  return box!;
}

function overlap(a: Box, b: Box): boolean {
  return a.x < b.x + b.width && b.x < a.x + a.width && a.y < b.y + b.height && b.y < a.y + a.height;
}

// At the top of the page no control covers any of `others` (or another control),
// on a 375 px phone and on a desktop. The page is left at the phone size.
async function expectNothingCovered(page: Page, controls: Locator[], others: Locator[]) {
  for (const size of [PHONE, DESKTOP]) {
    await page.setViewportSize(size);
    await page.evaluate(() => window.scrollTo(0, 0));
    const boxes = await Promise.all(controls.map(boxOf));
    for (const [index, control] of controls.entries()) {
      for (const other of [...others, ...controls.slice(index + 1)]) {
        expect(overlap(boxes[index], await boxOf(other)), `${control} covers ${other} at ${size.width} px`).toBe(false);
      }
    }
  }
  await page.setViewportSize(PHONE);
}

// The page scrolls down by 300 px: every control must go up the screen by the same
// 300 px, as the text does. A control fixed on the screen would not move.
async function expectScrollsAway(page: Page, controls: Locator[]) {
  await page.evaluate(() => window.scrollTo(0, 0));
  const before = await Promise.all(controls.map(boxOf));
  await page.evaluate((distance) => window.scrollTo(0, distance), DISTANCE);
  expect(await page.evaluate(() => window.scrollY), 'the page is long enough to scroll').toBe(DISTANCE);
  const after = await Promise.all(controls.map(boxOf));
  const moved = before.map((box, index) => Math.round(box.y - after[index].y));
  expect(moved).toEqual(controls.map(() => DISTANCE));
  await page.evaluate(() => window.scrollTo(0, 0));
}

// Live stats and the CSV table fill the window, and their tables scroll inside
// their own boxes (as in 1.19), so the page itself does not scroll. A tall block
// under the page lets the page scroll, to see whether the button moves with it.
async function makePageLonger(page: Page) {
  await page.evaluate(() => {
    const spacer = document.createElement('div');
    spacer.style.height = '2000px';
    document.body.appendChild(spacer);
  });
}

for (const language of ['uk', 'en']) {
  test(`quiz page (${language}): the theme button and the name badge scroll away and cover no title`, async ({ page, launch }) => {
    await page.setViewportSize(PHONE);
    // Many registration fields and questions: the registration form and the results are long
    const fields = Array.from({ length: 10 }, (_value, index) => `Field ${index + 1}`);
    const questions = Array.from({ length: 8 }, (_value, index) => ({
      question: `Question number ${index + 1}: how much is ${index} + 1?`,
      options: [String(index), String(index + 1)],
      correct_answer: 1,
    }));
    const server = await launch({
      config: { language, registration: { fields } },
      quizzes: defaultQuiz({ title: 'A quiz with a long title that takes two lines on a phone', show_right_answer: false, questions }),
    });
    const toggle = page.locator('.theme-toggle');
    const badge = page.locator('#user-info');
    const title = page.locator('#quiz-title');

    // Registration: the theme button only (the badge comes after the registration)
    await page.goto(server.url + '/');
    await expect(page.locator('#username')).toBeVisible();
    await expectNothingCovered(page, [toggle], [title]);
    await expectScrollsAway(page, [toggle]);

    await page.locator('#username').fill('A student with a rather long name');
    for (let index = 1; index <= fields.length; index++) {
      await page.locator(`[data-field-name="field_${index}"]`).fill('7');
    }
    await page.locator('.register-btn').click();
    for (let number = 1; number <= questions.length; number++) {
      await waitForQuestionNumber(page, number, questions.length);
      await page.locator('#option_1').click();
      await page.locator('#submit-answer-btn').click();
    }

    // Results: the name badge and the theme button
    await expect(page.locator('.results-table')).toBeVisible();
    await expect(badge).toBeVisible();
    await expectNothingCovered(page, [toggle, badge], [title]);
    await expectScrollsAway(page, [toggle, badge]);
  });
}

test('admin panel: the theme button scrolls away and covers no title', async ({ page, launch }) => {
  await page.setViewportSize(PHONE);
  const server = await launch();
  await adminLogin(page, server.url);
  await expect(page.locator('#admin-panel')).toBeVisible();
  const toggle = page.locator('.theme-toggle');
  await expectNothingCovered(page, [toggle], [page.locator('h1')]);
  await expectScrollsAway(page, [toggle]);
});

test('file manager: the theme button scrolls away and covers no title', async ({ page, launch }) => {
  await page.setViewportSize(PHONE);
  const server = await launch();
  // Many CSV files make a long list
  const dataDir = path.join(server.dir, 'data');
  mkdirSync(dataDir, { recursive: true });
  for (let number = 1; number <= 25; number++) {
    writeFileSync(path.join(dataDir, `lesson_${String(number).padStart(4, '0')}.csv`), 'user_id,question\r\n100001,Q\r\n');
  }
  await adminLogin(page, server.url);
  await page.goto(server.url + '/files/');
  await expect(page.getByText('lesson_0025.csv')).toBeVisible();
  const toggle = page.locator('.theme-toggle');
  await expectNothingCovered(page, [toggle], [page.locator('h1')]);
  await expectScrollsAway(page, [toggle]);
});

test('live stats: the theme button scrolls away and covers nothing at the top', async ({ page, request, launch }) => {
  await page.setViewportSize(PHONE);
  const server = await launch();
  await request.post(`${server.url}/api/register`, { data: { username: 'Anna' } });
  await page.goto(server.url + '/live-stats/');
  await expect(page.locator('#statsBody tr')).toHaveCount(1);
  const toggle = page.locator('.theme-toggle');
  await expectNothingCovered(page, [toggle], [page.locator('.filter-bar'), page.locator('.stats-container')]);
  await makePageLonger(page);
  await expectScrollsAway(page, [toggle]);
});

test('CSV table: the theme button scrolls away and covers no title', async ({ page, launch }) => {
  await page.setViewportSize(PHONE);
  const server = await launch();
  const dataDir = path.join(server.dir, 'data');
  mkdirSync(dataDir, { recursive: true });
  const name = 'lesson_0001.csv';
  writeFileSync(
    path.join(dataDir, name),
    'user_id,question,selected_answer,correct_answer,is_correct,time_taken_seconds\r\n100001,What is 2 + 2?,4,4,True,3.1\r\n',
  );
  await adminLogin(page, server.url);
  await page.goto(`${server.url}/files/csv/${name}`);
  await page.locator('#table-container tbody tr').first().waitFor();
  const toggle = page.locator('.theme-toggle');
  await expectNothingCovered(page, [toggle], [page.locator('#csv-title'), page.locator('#page-header a')]);
  await makePageLonger(page);
  await expectScrollsAway(page, [toggle]);
});
