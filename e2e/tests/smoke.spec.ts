import { hasServerSource } from '../fixtures/server';
import { expect, test } from '../fixtures/helpers';

test.skip(!hasServerSource, 'cmd/webquiz/main.go does not exist yet');

test('server starts and serves the quiz page', async ({ server, request }) => {
  const response = await request.get(`${server.url}/`);
  expect(response.status()).toBe(200);
});
