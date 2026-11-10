// Review Focus 2: the tunnel serves the quiz at {base_url}/{socket_name}/, so the page
// must load its assets, API calls and images with relative addresses. A small proxy
// serves the quiz under /abc123/ and strips the prefix, like the tunnel server.
import { writeFileSync } from 'node:fs';
import path from 'node:path';
import { startPrefixProxy } from '../../fixtures/prefix-proxy';
import { defaultQuiz, EN, expect, test } from '../../fixtures/quiz';

// A 1x1 PNG image
const PNG = Buffer.from(
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==',
  'base64',
);

test('the whole quiz works under /abc123/, like behind the tunnel', async ({ page, launch }) => {
  const server = await launch({
    config: EN,
    quizzes: defaultQuiz({
      title: 'Behind the tunnel',
      show_right_answer: true,
      show_explanation: true,
      questions: [
        {
          question: 'Which picture?',
          image: '/imgs/question.png',
          file: 'data.txt',
          options: ['/imgs/a.png', '/imgs/b.png'],
          correct_answer: 0,
          explanation: 'Like this: ![dot](/imgs/question.png)',
        },
        { question: 'Type 42', checker: "assert user_answer.strip() == '42', 'Not 42'", default_value: '' },
      ],
    }),
  });
  for (const name of ['question.png', 'a.png', 'b.png']) {
    writeFileSync(path.join(server.dir, 'quizzes', 'imgs', name), PNG);
  }
  const proxy = await startPrefixProxy(server.url, 'abc123');
  try {
    const failed: string[] = [];
    page.on('requestfailed', (request) => failed.push('failed ' + request.url()));
    page.on('response', (response) => {
      if (response.status() >= 400) failed.push(response.status() + ' ' + response.url());
    });
    const images: string[] = [];
    page.on('request', (request) => {
      if (request.url().indexOf('/imgs/') !== -1) images.push(request.url());
    });

    await page.goto(proxy.url);
    // The page's scripts and styles came through the prefix: the form is drawn
    await expect(page.locator('#username')).toBeVisible();
    // The images are loaded while the student registers (preloading), under the prefix
    await expect.poll(() => images.filter((url) => url.indexOf('/abc123/imgs/') !== -1).length).toBeGreaterThanOrEqual(3);

    await page.locator('#username').fill('Remote student');
    await page.locator('.register-btn').click();
    await expect(page.locator('.question-text')).toHaveText('Which picture?');

    const naturalWidth = (selector: string) =>
      page.locator(selector).first().evaluate((element: HTMLImageElement) => element.naturalWidth);
    await expect.poll(() => naturalWidth('.question-image')).toBeGreaterThan(0);
    await expect.poll(() => naturalWidth('.option-image')).toBeGreaterThan(0);
    // The attachment link stays inside the prefix (the download itself is another task's handler)
    const fileHref = await page.locator('.current-question .question-file').evaluate((link: HTMLAnchorElement) => link.href);
    expect(fileHref).toBe(proxy.url + 'attach/data.txt');

    // A reload continues the same student under the prefix
    await page.reload();
    await expect(page.locator('#username-display')).toHaveText('Remote student');
    await expect(page.locator('.question-text')).toHaveText('Which picture?');

    await page.locator('#option_0').click();
    await page.locator('#submit-answer-btn').click();
    await expect(page.locator('#option_0')).toHaveClass(/feedback-correct/);
    await page.locator('#continue-btn').click();

    // The explanation's image address (made relative by the server) also works
    await expect(page.locator('#explanation-step')).toBeVisible();
    await expect.poll(() => naturalWidth('.explanation-content img')).toBeGreaterThan(0);
    await page.locator('#explanation-next-btn').click();

    await expect(page.locator('.question-text')).toHaveText('Type 42');
    await page.locator('#text-answer-input').fill('41');
    await page.locator('#submit-answer-btn').click();
    await expect(page.locator('#checker-feedback .checker-error')).toContainText('Not 42');
    await page.locator('#continue-btn').click();

    await expect(page.locator('#results')).toContainText('Result: 1/2 (50%)');
    await expect.poll(() => naturalWidth('.results-question-image')).toBeGreaterThan(0);

    expect(failed).toEqual([]);
    expect(proxy.outside.filter((url) => url !== '/favicon.ico')).toEqual([]);
  } finally {
    await proxy.close();
  }
});
