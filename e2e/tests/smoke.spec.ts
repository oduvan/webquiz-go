import { expect, test } from '../fixtures/helpers';

test('server starts and serves the quiz page', async ({ server, request }) => {
  const response = await request.get(`${server.url}/`);
  expect(response.status()).toBe(200);
});
