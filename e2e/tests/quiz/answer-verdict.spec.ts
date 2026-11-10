// The verdict next to "Continue" (new in 2.0). While right answers are shown, the page
// says in words whether the answer counts: "✓ Правильно" or "✗ Неправильно". 1.19 showed
// only colors and labels, so a partly right several-answers answer looked right.
import type { Page } from '@playwright/test';
import { EN, defaultQuiz, expect, hasHorizontalScroll, optionByIndex, registerUser, test, waitForContinue, waitForQuestionText } from '../../fixtures/quiz';

const CORRECT = '✓ Правильно';
const INCORRECT = '✗ Неправильно';

// Colors of the light and the dark theme
const GREEN = 'rgb(45, 115, 28)'; // #2d731c
const RED = 'rgb(220, 53, 69)'; // #dc3545
const DARK_GREEN = 'rgb(76, 175, 80)'; // #4caf50
const DARK_RED = 'rgb(255, 107, 107)'; // #ff6b6b

const verdict = (page: Page) => page.locator('#answer-verdict');

function quiz(questions: unknown[], settings: Record<string, unknown> = {}) {
  return defaultQuiz({ title: 'Verdict', show_right_answer: true, questions, ...settings });
}

function single(text: string, extra: Record<string, unknown> = {}) {
  return { question: text, options: ['Right', 'Wrong'], correct_answer: 0, ...extra };
}

const TYPED = {
  question: 'How many regions? Type a number.',
  checker: "assert to_int(user_answer) == 24, 'Must be 24'",
  correct_value: '24',
};

// Picks the options at these indexes, sends the answer and waits for "Continue".
async function answer(page: Page, ...indexes: number[]) {
  for (const index of indexes) {
    await optionByIndex(page, index).click();
  }
  await page.locator('#submit-answer-btn').click();
  await waitForContinue(page);
}

async function typeAnswer(page: Page, text: string) {
  await page.locator('#text-answer-input').fill(text);
  await page.locator('#submit-answer-btn').click();
  await waitForContinue(page);
}

async function style(page: Page, property: string): Promise<string> {
  return verdict(page).evaluate((element, name) => getComputedStyle(element).getPropertyValue(name), property);
}

async function boxes(page: Page) {
  const verdictBox = await verdict(page).boundingBox();
  const buttonBox = await page.locator('#continue-btn').boundingBox();
  expect(verdictBox, 'the verdict is on the screen').not.toBeNull();
  expect(buttonBox, '"Continue" is on the screen').not.toBeNull();
  return { verdictBox: verdictBox!, buttonBox: buttonBox! };
}

// The verdict is in the button row, left of "Continue"; the button stays on the right.
async function expectVerdict(page: Page, text: string, kind: 'correct' | 'incorrect') {
  await expect(verdict(page)).toHaveText(text);
  await expect(verdict(page)).toHaveClass(new RegExp(`(^|\\s)answer-verdict(\\s|$)`));
  await expect(verdict(page)).toHaveClass(new RegExp(`(^|\\s)${kind}(\\s|$)`));
  await expect(verdict(page)).toBeVisible();
  const { verdictBox, buttonBox } = await boxes(page);
  expect(verdictBox.x + verdictBox.width).toBeLessThanOrEqual(buttonBox.x);
  // One row: the two boxes share a line
  expect(verdictBox.y).toBeLessThan(buttonBox.y + buttonBox.height);
  expect(buttonBox.y).toBeLessThan(verdictBox.y + verdictBox.height);
  // "Continue" ends where the row's content ends
  const rowEnd = await page.locator('#continue-btn').evaluate((button) => {
    const row = button.parentElement!;
    const rect = row.getBoundingClientRect();
    return rect.right - parseFloat(getComputedStyle(row).paddingRight);
  });
  expect(Math.abs(buttonBox.x + buttonBox.width - rowEnd)).toBeLessThan(1);
}

// Notes it if #answer-verdict appears at any moment, also for a moment only.
async function watchVerdict(page: Page) {
  await page.evaluate(() => {
    const w = window as unknown as { __verdictSeen: boolean };
    w.__verdictSeen = false;
    new MutationObserver(() => {
      if (document.getElementById('answer-verdict')) w.__verdictSeen = true;
    }).observe(document.body, { childList: true, subtree: true });
  });
}

async function verdictWasSeen(page: Page): Promise<boolean> {
  return page.evaluate(() => (window as unknown as { __verdictSeen: boolean }).__verdictSeen);
}

test('a partly right several-answers answer says "✗ Неправильно"', async ({ page, launch }) => {
  const server = await launch({
    quizzes: quiz([{ question: 'Which numbers are even?', options: ['2', '4', '5'], correct_answer: [0, 1] }]),
  });
  await registerUser(page, server.url);
  await answer(page, 0);
  await expectVerdict(page, INCORRECT, 'incorrect');
  // The rest stays as in 1.19: the picked option is marked right, the missed one "also right"
  await expect(optionByIndex(page, 0)).toHaveClass(/(^|\s)feedback-correct(\s|$)/);
  await expect(optionByIndex(page, 0).locator('.feedback-label')).toHaveText('✓ Правильна відповідь');
  await expect(optionByIndex(page, 1)).toHaveClass(/(^|\s)feedback-missed(\s|$)/);
  await expect(optionByIndex(page, 1).locator('.feedback-label')).toHaveText('✓ Теж правильна відповідь');
});

test('a right answer says "✓ Правильно" in bold green', async ({ page, launch }) => {
  const server = await launch({ quizzes: quiz([single('Q1?')]) });
  await registerUser(page, server.url);
  await answer(page, 0);
  await expectVerdict(page, CORRECT, 'correct');
  expect(await style(page, 'color')).toBe(GREEN);
  expect(await style(page, 'font-weight')).toBe('700');
  expect(await style(page, 'font-size')).toBe('18px');
});

test('a wrong answer says "✗ Неправильно" in red', async ({ page, launch }) => {
  const server = await launch({ quizzes: quiz([single('Q1?')]) });
  await registerUser(page, server.url);
  await answer(page, 1);
  await expectVerdict(page, INCORRECT, 'incorrect');
  expect(await style(page, 'color')).toBe(RED);
  await expect(optionByIndex(page, 1)).toHaveClass(/(^|\s)feedback-incorrect(\s|$)/);
});

test('a right typed answer says "✓ Правильно"', async ({ page, launch }) => {
  const server = await launch({ quizzes: quiz([TYPED]) });
  await registerUser(page, server.url);
  await typeAnswer(page, '24');
  await expectVerdict(page, CORRECT, 'correct');
  await expect(page.locator('#text-answer-input')).toHaveClass(/(^|\s)feedback-correct(\s|$)/);
});

test('a wrong typed answer says "✗ Неправильно"; the right value is still shown', async ({ page, launch }) => {
  const server = await launch({ quizzes: quiz([TYPED]) });
  await registerUser(page, server.url);
  await typeAnswer(page, '25');
  await expectVerdict(page, INCORRECT, 'incorrect');
  await expect(page.locator('#text-answer-input')).toHaveClass(/(^|\s)feedback-incorrect(\s|$)/);
  await expect(page.locator('.correct-value-display')).toContainText('✓ Правильна відповідь: ');
  await expect(page.locator('.correct-value-display')).toContainText('24');
});

test('no verdict while right answers are hidden', async ({ page, launch }) => {
  const server = await launch({ quizzes: quiz([single('Q1?'), single('Q2?')], { show_right_answer: false }) });
  await registerUser(page, server.url);
  await watchVerdict(page);
  await optionByIndex(page, 0).click();
  await page.locator('#submit-answer-btn').click();
  await waitForQuestionText(page, 'Q2?'); // the next question comes by itself
  await optionByIndex(page, 1).click();
  await page.locator('#submit-answer-btn').click();
  await expect(page.locator('.results-table')).toBeVisible();
  expect(await verdictWasSeen(page)).toBe(false);
});

test('the start settings turn the verdict on, as they turn on the right answers', async ({ page, request, launch }) => {
  const server = await launch({
    quizzes: { 'q.yaml': { title: 'Verdict', show_right_answer: false, questions: [single('Q1?')] } },
  });
  expect((await request.post(server.url + '/api/admin/auth', { data: { master_key: 'test123' } })).status()).toBe(200);
  const switched = await request.post(server.url + '/api/admin/switch-quiz', {
    data: { quiz_filename: 'q.yaml', settings: { show_right_answer: true } },
  });
  expect(switched.status()).toBe(200);
  await registerUser(page, server.url);
  await answer(page, 0);
  await expectVerdict(page, CORRECT, 'correct');
});

test('no verdict when the time is up: the question says "Час вичерпано"', async ({ page, launch }) => {
  const server = await launch({ quizzes: quiz([single('Q1?'), single('Q2?')], { time_limit: { per_question: 2 } }) });
  await registerUser(page, server.url);
  await watchVerdict(page);
  await optionByIndex(page, 0).click(); // picked, but not sent
  await expect(page.locator('#current-question-container .time-up-message')).toContainText('Час вичерпано', { timeout: 8000 });
  await waitForContinue(page);
  await expect(verdict(page)).toHaveCount(0);
  expect(await verdictWasSeen(page)).toBe(false);
});

test('English: "✓ Correct" and "✗ Incorrect"', async ({ page, launch }) => {
  const server = await launch({ config: EN, quizzes: quiz([single('Q1?'), single('Q2?')]) });
  await registerUser(page, server.url);
  await answer(page, 0);
  await expectVerdict(page, '✓ Correct', 'correct');
  await page.locator('#continue-btn').click();
  await waitForQuestionText(page, 'Q2?');
  await answer(page, 1);
  await expectVerdict(page, '✗ Incorrect', 'incorrect');
});

test('the verdict is gone on the next question', async ({ page, launch }) => {
  const server = await launch({ quizzes: quiz([single('Q1?'), single('Q2?')]) });
  await registerUser(page, server.url);
  await answer(page, 0);
  await expectVerdict(page, CORRECT, 'correct');
  await page.locator('#continue-btn').click();
  await waitForQuestionText(page, 'Q2?');
  await expect(verdict(page)).toHaveCount(0);
  await expect(page.locator('#submit-answer-btn')).toBeVisible();
  // The next answer gets its own verdict
  await answer(page, 1);
  await expectVerdict(page, INCORRECT, 'incorrect');
});

test('the explanation step does not show the verdict', async ({ page, launch }) => {
  const server = await launch({
    quizzes: quiz([single('Q1?', { explanation: 'Because of **this**' }), single('Q2?')], { show_explanation: true }),
  });
  await registerUser(page, server.url);
  await answer(page, 0);
  await expectVerdict(page, CORRECT, 'correct');
  await page.locator('#continue-btn').click();
  await expect(page.locator('#explanation-step')).toBeVisible();
  await expect(verdict(page)).toHaveCount(0);
  await page.locator('#explanation-next-btn').click();
  await waitForQuestionText(page, 'Q2?');
  await expect(verdict(page)).toHaveCount(0);
});

test('a reload does not show the verdict again', async ({ page, launch }) => {
  const server = await launch({ quizzes: quiz([single('Q1?'), single('Q2?')]) });
  await registerUser(page, server.url);
  await answer(page, 1);
  await expectVerdict(page, INCORRECT, 'incorrect');
  await page.reload();
  await waitForQuestionText(page, 'Q2?');
  await expect(verdict(page)).toHaveCount(0);
});

test('a retried answer shows the same verdict', async ({ page, launch }) => {
  const server = await launch({ quizzes: quiz([single('Q1?'), single('Q2?')]) });
  await registerUser(page, server.url);
  // The server stores the first answer, but its reply is lost: the page sends it again
  // and gets the stored result
  await page.evaluate(() => {
    const w = window as unknown as { __dropped: number };
    const realFetch = window.fetch.bind(window);
    w.__dropped = 0;
    window.fetch = async (url: RequestInfo | URL, options?: RequestInit) => {
      if (String(url).includes('api/submit-answer') && w.__dropped === 0) {
        w.__dropped++;
        await realFetch(url, options);
        throw new TypeError('Failed to fetch');
      }
      return realFetch(url, options);
    };
  });
  await answer(page, 1);
  expect(await page.evaluate(() => (window as unknown as { __dropped: number }).__dropped)).toBe(1);
  await expectVerdict(page, INCORRECT, 'incorrect');
});

test('dark theme: the verdict uses the dark colors', async ({ page, launch }) => {
  await page.addInitScript(() => localStorage.setItem('theme', 'dark'));
  const server = await launch({ quizzes: quiz([single('Q1?'), single('Q2?')]) });
  await registerUser(page, server.url);
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
  await answer(page, 0);
  await expectVerdict(page, CORRECT, 'correct');
  expect(await style(page, 'color')).toBe(DARK_GREEN);
  await page.locator('#continue-btn').click();
  await waitForQuestionText(page, 'Q2?');
  await answer(page, 1);
  await expectVerdict(page, INCORRECT, 'incorrect');
  expect(await style(page, 'color')).toBe(DARK_RED);
});

// The longest texts: "✗ Неправильно" + "Продовжити" and "✗ Incorrect" + "Continue".
for (const [language, wrong] of [
  ['uk', INCORRECT],
  ['en', '✗ Incorrect'],
] as const) {
  test(`phone (${language}): the verdict and "Continue" share one row at 375 px and fit 320 px`, async ({ page, launch }) => {
    await page.setViewportSize({ width: 375, height: 800 });
    const server = await launch({ config: { language }, quizzes: quiz([single('Q1?')]) });
    await registerUser(page, server.url);
    await answer(page, 1);
    await expectVerdict(page, wrong, 'incorrect');
    expect(await hasHorizontalScroll(page)).toBe(false);
    let { verdictBox, buttonBox } = await boxes(page);
    expect(verdictBox.x).toBeGreaterThanOrEqual(0);
    expect(buttonBox.x + buttonBox.width).toBeLessThanOrEqual(375);

    // A very narrow screen: the two may wrap, but nothing goes past the edge
    await page.setViewportSize({ width: 320, height: 800 });
    await expect(verdict(page)).toBeVisible();
    await expect(page.locator('#continue-btn')).toBeVisible();
    expect(await hasHorizontalScroll(page)).toBe(false);
    ({ verdictBox, buttonBox } = await boxes(page));
    expect(verdictBox.x).toBeGreaterThanOrEqual(0);
    expect(verdictBox.x + verdictBox.width).toBeLessThanOrEqual(320);
    expect(buttonBox.x).toBeGreaterThanOrEqual(0);
    expect(buttonBox.x + buttonBox.width).toBeLessThanOrEqual(320);
  });
}
