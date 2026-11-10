// Ports of tests/test_selenium_time_limits.py: countdowns on the quiz page.
// The live stats part (test_live_stats_shows_timeout_icon) is in tests/livestats/time-limits.spec.ts.
import { EN, expect, hasHorizontalScroll, registerUser, test, waitForQuestionText } from '../../fixtures/quiz';

function question(text = 'Q?', extra: Record<string, unknown> = {}) {
  return { question: text, options: ['A', 'B'], correct_answer: 0, ...extra };
}

function timedQuiz(timeLimit: Record<string, number>, questions?: unknown[], settings: Record<string, unknown> = {}) {
  return { 'timed.yaml': { title: 'Timed', time_limit: timeLimit, questions: questions || [question()], ...settings } };
}

function timerSeconds(text: string): number {
  const [minutes, seconds] = text.trim().split(':');
  return Number(minutes) * 60 + Number(seconds);
}

// Every submit-answer request reaches the server `ms` milliseconds after it is sent (slow Wi-Fi).
function slowSubmits(ms: number): string {
  return `
    const originalFetch = window.fetch;
    window.fetch = (url, options) => String(url).includes('submit-answer')
      ? new Promise(resolve => setTimeout(resolve, ${ms})).then(() => originalFetch(url, options))
      : originalFetch(url, options);
  `;
}

const TIMER_FORMAT = /^\d:\d\d$/;

// py: tests/test_selenium_time_limits.py::test_question_countdown_is_shown
test('the question countdown is shown', async ({ page, launch }) => {
  const server = await launch({ config: EN, quizzes: timedQuiz({ per_question: 5 }) });
  await registerUser(page, server.url);
  const timer = page.locator('#question-timer');
  await expect(timer).toHaveText(TIMER_FORMAT, { timeout: 5000 });
  await expect(timer).toBeVisible();
  await expect(page.locator('#quiz-timer-box')).toBeHidden(); // no whole-quiz limit
});

// py: tests/test_selenium_time_limits.py::test_time_up_shows_message_and_continue
test('time up shows "Time is up" and Continue', async ({ page, launch }) => {
  const server = await launch({
    config: EN,
    quizzes: timedQuiz({ per_question: 2 }, [question('Q1?'), question('Q2?')], { show_right_answer: true }),
  });
  await registerUser(page, server.url);
  const message = page.locator('#current-question-container .time-up-message');
  await expect(message).toBeVisible({ timeout: 6000 });
  await expect(message).toContainText('Time is up');
  await expect(page.locator('#continue-btn')).toBeVisible({ timeout: 5000 });
});

// py: tests/test_selenium_time_limits.py::test_time_up_auto_advances_without_right_answer
test('time up moves on by itself when right answers are hidden', async ({ page, launch }) => {
  const server = await launch({
    config: EN,
    quizzes: timedQuiz({ per_question: 2 }, [question('First?'), question('Second?')], { show_right_answer: false }),
  });
  await registerUser(page, server.url);
  await waitForQuestionText(page, 'First?');
  await waitForQuestionText(page, 'Second?', 8000);
});

// py: tests/test_selenium_time_limits.py::test_whole_quiz_time_up_shows_results
test('whole-quiz time up shows the results', async ({ page, launch }) => {
  const server = await launch({
    config: EN,
    quizzes: timedQuiz({ whole_quiz: 3 }, [question('Q1?'), question('Q2?'), question('Q3?')]),
  });
  await registerUser(page, server.url);
  await expect(page.locator('#quiz-timer')).toBeVisible();
  await expect(page.locator('.results-table .time-up-message')).toHaveCount(3, { timeout: 10000 });
});

// py: tests/test_selenium_time_limits.py::test_reload_keeps_counting_down
test('a reload keeps counting down', async ({ page, launch }) => {
  const server = await launch({ config: EN, quizzes: timedQuiz({ per_question: 20 }) });
  await registerUser(page, server.url);
  await expect(page.locator('#question-timer')).toBeAttached();
  await page.waitForTimeout(4000);
  await page.reload();
  const timer = page.locator('#question-timer');
  await expect(timer).toHaveText(TIMER_FORMAT, { timeout: 5000 });
  expect(timerSeconds(await timer.innerText())).toBeLessThanOrEqual(17);
});

// py: tests/test_selenium_time_limits.py::test_timer_fits_mobile_width
test('the timer fits a phone screen', async ({ page, launch }) => {
  await page.setViewportSize({ width: 375, height: 800 });
  const server = await launch({
    config: EN,
    quizzes: timedQuiz({ whole_quiz: 300 }, [question('A fairly long question text for a phone?')]),
  });
  await registerUser(page, server.url);
  await expect(page.locator('#quiz-timer')).toHaveText(TIMER_FORMAT);
  expect(await hasHorizontalScroll(page)).toBe(false);
});

// py: tests/test_selenium_time_limits.py::test_whole_quiz_time_up_locks_the_answer_while_sending
test('the answer is locked while the whole-quiz time-up answer is sent', async ({ page, launch }) => {
  const server = await launch({ config: EN, quizzes: timedQuiz({ whole_quiz: 3 }, [question('Q1?'), question('Q2?')]) });
  await registerUser(page, server.url);
  await expect(page.locator('#quiz-timer')).toBeVisible();
  await page.locator('#option_0').click(); // selected, but not submitted
  await page.evaluate(slowSubmits(3000)); // slow network: each answer takes 3 seconds
  await expect(page.locator('#quiz-timer')).toHaveText('0:00', { timeout: 6000 });
  await page.waitForTimeout(500); // the empty answer is now on its way

  await expect(page.locator('#submit-answer-btn')).toBeDisabled();
  await expect(page.locator('#option_1')).toHaveClass(/(^|\s)disabled(\s|$)/);
  await expect(page.locator('.results-table .time-up-message')).toHaveCount(2, { timeout: 10000 });
});

// py: tests/test_selenium_time_limits.py::test_timer_shows_zero_when_a_late_answer_comes_back_timed_out
test('a late answer that comes back timed out shows 0:00', async ({ page, launch }) => {
  // An answer sent with seconds left but delivered late comes back as timed out. The
  // countdown stopped when the answer was sent; it must then show 0:00, not the seconds
  // that were left at sending, next to "Time is up".
  const server = await launch({
    config: EN,
    quizzes: timedQuiz({ per_question: 4 }, [question('Q1?'), question('Q2?')], { show_right_answer: true }),
    env: { WEBQUIZ_TIME_LIMIT_GRACE_SECONDS: '0.5', WEBQUIZ_TIME_LIMIT_CHECK_INTERVAL: '0.2' },
  });
  await registerUser(page, server.url);
  await expect(page.locator('#question-timer')).toBeAttached();
  await page.evaluate(slowSubmits(4000)); // every answer reaches the server 4 seconds after it is sent
  await page.waitForTimeout(1500);
  await page.locator('#option_0').click();
  await page.locator('#submit-answer-btn').click(); // about 2 seconds left

  await expect(page.locator('#current-question-container .time-up-message')).toBeVisible({ timeout: 12000 });
  await expect(page.locator('#question-timer')).toHaveText('0:00');
});
