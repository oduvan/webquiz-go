// Ports of tests/test_selenium_decimal_points.py: decimal points in the badge and in the results.
import { defaultQuiz, expect, findOptions, optionByText, registerUser, test, waitForQuestionText } from '../../fixtures/quiz';

// py: tests/test_selenium_decimal_points.py::test_points_badge_shows_decimal_value
test('a 0.5 point question shows 0.5 in its badge', async ({ page, launch }) => {
  const server = await launch({
    quizzes: defaultQuiz({
      title: 'Decimal Points Badge',
      questions: [{ question: 'Worth half a point', options: ['A', 'B'], correct_answer: 0, points: 0.5 }],
    }),
  });
  await registerUser(page, server.url);
  const badge = page.locator('.question-points-badge');
  await expect(badge).toBeVisible();
  await expect(badge).toContainText('0.5');
});

// py: tests/test_selenium_decimal_points.py::test_no_badge_for_default_one_point
test('a 1 point question has no badge', async ({ page, launch }) => {
  const server = await launch({
    quizzes: defaultQuiz({
      title: 'Default Points',
      questions: [{ question: 'Plain question', options: ['A', 'B'], correct_answer: 0 }],
    }),
  });
  await registerUser(page, server.url);
  await expect(findOptions(page).first()).toBeVisible();
  await expect(page.locator('.question-points-badge')).toHaveCount(0);
});

// py: tests/test_selenium_decimal_points.py::test_final_score_shows_clean_decimal
test('0.1 + 0.2 points read 0.3 in the results', async ({ page, launch }) => {
  const server = await launch({
    quizzes: defaultQuiz({
      title: 'Inexact Points',
      show_right_answer: true,
      questions: [
        { question: 'First question?', options: ['Right', 'Wrong'], correct_answer: 0, points: 0.1 },
        { question: 'Second question?', options: ['Right', 'Wrong'], correct_answer: 0, points: 0.2 },
      ],
    }),
  });
  await registerUser(page, server.url);
  await (await optionByText(page, 'Right')).click();
  await page.locator('#submit-answer-btn').click();
  await page.locator('#continue-btn').click();

  await waitForQuestionText(page, 'Second question?', 10000);
  await (await optionByText(page, 'Right')).click();
  await page.locator('#submit-answer-btn').click();
  await page.locator('#continue-btn').click();

  const results = page.locator('#results');
  await expect(results).toContainText('2/2 (100%)', { timeout: 10000 });
  await expect(results).not.toContainText('0.30000000000000004');
  await expect(results).toContainText('0.3/0.3');
});
