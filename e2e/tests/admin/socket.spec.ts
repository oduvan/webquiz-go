import type { Page, WebSocketRoute } from '@playwright/test';
import { adminLogin, expect, test } from './helpers';

// What the admin page does with the messages of its socket (/ws/admin): the
// SSH tunnel panel, file transfer requests from other servers and students'
// registration changes. Most tests talk to a fake socket, so they can send
// any message (a real tunnel and another server are not here).

type Message = Record<string, unknown>;

// The page's admin socket talks to the test instead of the server. The test
// sends the first message (initial_state) and any later ones.
async function fakeSocket(page: Page, initial: Message = {}) {
  const sockets: WebSocketRoute[] = [];
  await page.routeWebSocket('**/ws/admin', (socket) => {
    sockets.push(socket);
    socket.send(
      JSON.stringify({
        type: 'initial_state',
        pending_users: {},
        requires_approval: false,
        tunnel: null,
        transfer_requests: [],
        ...initial,
      }),
    );
  });
  return {
    send: async (message: Message) => {
      await expect.poll(() => sockets.length).toBeGreaterThan(0);
      sockets[sockets.length - 1].send(JSON.stringify(message));
    },
  };
}

function messages(page: Page, type: 'success' | 'error' | 'warning') {
  return page.locator(`#message-area .message.${type}`);
}

const TUNNEL = {
  configured: true,
  server: 'tunnel.example.com',
  socket_name: 'room-a',
  connected: false,
  url: null,
  error: null,
  keys_status: 'ok',
  public_key_content: null,
};

test.describe('tunnel panel', () => {
  test('shows the status, the public address and the connect button', async ({ page, launch }) => {
    const server = await launch();
    const socket = await fakeSocket(page, { tunnel: TUNNEL });
    await page.route('**/api/admin/tunnel/connect', (route) =>
      route.fulfill({ json: { success: true, url: 'https://tunnel.example.com/room-a/' } }),
    );
    await page.route('**/api/admin/tunnel/disconnect', (route) => route.fulfill({ json: { success: true } }));
    await adminLogin(page, server.url);

    await expect(page.locator('#tunnel-section')).toBeVisible();
    await expect(page.locator('#tunnel-server')).toHaveText('tunnel.example.com');
    await expect(page.locator('#tunnel-socket-name')).toHaveText('room-a');
    await expect(page.locator('#tunnel-public-key-section')).toBeVisible();
    await expect(page.locator('#tunnel-error')).toBeHidden();
    await expect(page.locator('#tunnel-url-item')).toHaveCount(0);

    const button = page.locator('#tunnel-connect-btn');
    await expect(button).toHaveText('Connect');
    await button.click();
    await expect(messages(page, 'success').last()).toHaveText('Tunnel connected! Public URL: https://tunnel.example.com/room-a/');
    // The button waits for the next status message
    await expect(button).toBeDisabled();

    // A status without a server name keeps the one shown
    await socket.send({
      type: 'tunnel_status',
      tunnel: { ...TUNNEL, server: null, connected: true, url: 'https://tunnel.example.com/room-a/' },
    });
    await expect(button).toHaveText('Disconnect');
    await expect(button).toBeEnabled();
    await expect(page.locator('#tunnel-server')).toHaveText('tunnel.example.com');
    // The public address comes first in the address list
    await expect(page.locator('#url-list > .url-item').first()).toHaveAttribute('id', 'tunnel-url-item');
    await expect(page.locator('#tunnel-url-item')).toContainText('🌐 Зовнішній URL:');
    await expect(page.locator('#tunnel-url-item a')).toHaveAttribute('href', 'https://tunnel.example.com/room-a/');

    await button.click();
    await expect(messages(page, 'success').last()).toHaveText('Tunnel disconnected');
    await socket.send({ type: 'tunnel_status', tunnel: { ...TUNNEL, error: 'Connection lost' } });
    await expect(button).toHaveText('Connect');
    await expect(page.locator('#tunnel-url-item')).toHaveCount(0);
    await expect(page.locator('#tunnel-error')).toHaveText('Error: Connection lost');

    // Without usable keys there is nothing to connect with
    await socket.send({ type: 'tunnel_status', tunnel: { ...TUNNEL, keys_status: 'invalid' } });
    await expect(button).toBeDisabled();
    await expect(page.locator('#tunnel-public-key-section')).toBeHidden();
    await expect(page.locator('#tunnel-socket-name-section')).toBeVisible();
    await socket.send({ type: 'tunnel_status', tunnel: { ...TUNNEL, socket_name: null } });
    await expect(page.locator('#tunnel-socket-name-section')).toBeHidden();
  });

  test('is hidden without a tunnel', async ({ page, launch }) => {
    const server = await launch();
    await adminLogin(page, server.url);
    await expect(page.locator('#url-list')).toBeVisible();
    await expect(page.locator('#tunnel-section')).toHaveCount(0);
  });

  test('copies the public key and reports a failed connection', async ({ page, context, launch }) => {
    // A tunnel server on this computer: the keys are made at start, and the
    // connection fails because nothing serves the tunnel there
    const server = await launch({
      config: { tunnel: { server: '127.0.0.1', public_key: 'keys/id_ed25519.pub', private_key: 'keys/id_ed25519' } },
    });
    await context.grantPermissions(['clipboard-read', 'clipboard-write'], { origin: server.url });
    await adminLogin(page, server.url);

    await expect(page.locator('#tunnel-section')).toBeVisible();
    await expect(page.locator('#tunnel-server')).toHaveText('127.0.0.1');
    const key = page.locator('#tunnel-public-key');
    await expect(key).toHaveValue(/^ssh-ed25519 /);

    const copy = page.locator('#copy-public-key-btn');
    await copy.click();
    await expect(copy).toHaveText('✅ Copied!');
    expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(await key.inputValue());
    await expect(copy).toHaveText('📋 Copy');

    // Fetching the tunnel config fails. Like Python, the server answers with
    // the reason and sends no status message, so only the message shows it.
    const button = page.locator('#tunnel-connect-btn');
    await expect(button).toBeEnabled();
    await button.click();
    await expect(messages(page, 'error').last()).toContainText('Tunnel operation failed: Failed to fetch config from server');
    await expect(button).toBeEnabled();
  });
});

const REQUEST = {
  id: 'r1',
  from: 'room-b',
  received: '2026-11-10T10:00:00',
  status: 'pending',
  files: [
    {
      type: 'quizzes',
      name: 'math.yaml',
      size: 2048,
      target: 'math.yaml',
      case: 'new',
      actions: ['save', 'rename', 'skip'],
      default_action: 'save',
      suggested_name: 'room-b_math.yaml',
    },
    {
      type: 'csv',
      name: 'math_0001.csv',
      size: 10,
      target: 'room-b_math_0001.csv',
      case: 'different',
      actions: ['replace', 'rename', 'skip'],
      default_action: 'rename',
      suggested_name: 'room-b_math_0001_2.csv',
    },
  ],
};

test.describe('file transfer requests', () => {
  test('a request is accepted with the chosen actions', async ({ page, launch }) => {
    const server = await launch();
    const socket = await fakeSocket(page, { transfer_requests: [REQUEST] });
    let sent: unknown = null;
    await page.route('**/api/admin/transfer/r1/accept', async (route) => {
      sent = route.request().postDataJSON();
      await route.fulfill({
        json: {
          success: true,
          from: 'room-b',
          saved: [{ type: 'csv', name: 'math_0001.csv', action: 'rename', saved_as: 'mine.csv' }],
          skipped: [{ type: 'quizzes', name: 'math.yaml', action: 'skip' }],
          failed: [],
        },
      });
    });
    await adminLogin(page, server.url);

    const item = page.locator('.transfer-request-item[data-request-id="r1"]');
    await expect(page.locator('#transfer-requests-section')).toBeVisible();
    await expect(item.locator('.pending-user-name')).toHaveText('🖥️ Від: room-b');
    const rows = item.locator('.transfer-file-row');
    await expect(rows).toHaveCount(2);

    const quizRow = rows.nth(0);
    await expect(quizRow).toHaveAttribute('data-type', 'quizzes');
    await expect(quizRow.locator('.transfer-file-name')).toHaveText('math.yaml');
    await expect(quizRow.locator('.transfer-file-meta')).toHaveText('тест, 2 KB');
    await expect(quizRow.locator('.transfer-case')).toHaveText('🆕 Новий файл: math.yaml');
    await expect(quizRow.locator('select')).toHaveValue('save');
    await expect(quizRow.locator('.transfer-new-name')).toBeHidden();

    const csvRow = rows.nth(1);
    await expect(csvRow.locator('.transfer-file-meta')).toHaveText('CSV, 10 B');
    await expect(csvRow.locator('.transfer-case')).toHaveText('⚠️ Вже є інший файл room-b_math_0001.csv');
    await expect(csvRow.locator('.transfer-case')).toHaveClass(/transfer-case-different/);
    await expect(csvRow.locator('select')).toHaveValue('rename');
    await expect(csvRow.locator('.transfer-new-name')).toHaveValue('room-b_math_0001_2.csv');

    await quizRow.locator('select').selectOption('skip');
    await csvRow.locator('.transfer-new-name').fill(' mine.csv ');

    // Another request arrives; the choices made so far stay
    await socket.send({ type: 'transfer_request', request: { ...REQUEST, id: 'r2', from: 'room-c' } });
    await expect(page.locator('.transfer-request-item')).toHaveCount(2);
    await expect(messages(page, 'success').last()).toHaveText('Новий запит на файли від room-c');
    await expect(quizRow.locator('select')).toHaveValue('skip');

    await item.getByRole('button', { name: '✓ Прийняти' }).click();
    await expect(page.locator('#message-area')).toContainText('Отримано файлів: 1 (mine.csv)');
    await expect(page.locator('#message-area')).toContainText('Пропущено файлів: 1');
    await expect(item).toHaveCount(0);
    expect(sent).toEqual({
      files: [
        { type: 'quizzes', name: 'math.yaml', action: 'skip' },
        { type: 'csv', name: 'math_0001.csv', action: 'rename', new_name: 'mine.csv' },
      ],
    });
  });

  test('refused choices keep the request with the reason, and reject removes it', async ({ page, launch }) => {
    const server = await launch();
    await fakeSocket(page, { transfer_requests: [REQUEST] });
    await page.route('**/api/admin/transfer/r1/accept', (route) =>
      route.fulfill({ status: 400, json: { error: 'File name already used: room-b_math_0001_2.csv' } }),
    );
    await page.route('**/api/admin/transfer', (route) =>
      route.fulfill({ json: { available: true, name: 'room-a', outgoing: [], incoming: [REQUEST] } }),
    );
    let rejectBody: string | null = null;
    await page.route('**/api/admin/transfer/r1/reject', async (route) => {
      rejectBody = route.request().postData();
      await route.fulfill({ json: { success: true } });
    });
    await adminLogin(page, server.url);

    const item = page.locator('.transfer-request-item[data-request-id="r1"]');
    const accept = item.getByRole('button', { name: '✓ Прийняти' });
    await accept.click();
    await expect(messages(page, 'error').last()).toHaveText('File name already used: room-b_math_0001_2.csv');
    await expect(item.locator('.transfer-error[role="alert"]')).toHaveText('File name already used: room-b_math_0001_2.csv');
    await expect(accept).toBeEnabled();
    await expect(item.locator('select').first()).toBeEnabled();

    await item.getByRole('button', { name: '✗ Відхилити' }).click();
    await expect(messages(page, 'success').last()).toHaveText('Запит відхилено');
    await expect(page.locator('#transfer-requests-section')).toBeHidden();
    expect(rejectBody).toBe('{}');
  });

  test('socket messages add and remove requests and report results', async ({ page, launch }) => {
    const server = await launch();
    const socket = await fakeSocket(page);
    await adminLogin(page, server.url);
    await expect(page.locator('#transfer-requests-section')).toBeHidden();

    await socket.send({ type: 'transfer_request', request: REQUEST });
    await expect(page.locator('#transfer-requests-section')).toBeVisible();
    await expect(messages(page, 'success').last()).toHaveText('Новий запит на файли від room-b');

    await socket.send({ type: 'transfer_request_removed', id: 'r1' });
    await expect(page.locator('#transfer-requests-section')).toBeHidden();

    const offer = { id: 'o1', to: 'room-c', files: [], created: '2026-11-10T10:00:00' };
    await socket.send({ type: 'transfer_result', offer: { ...offer, status: 'accepted' } });
    await expect(messages(page, 'success').last()).toHaveText('Сервер room-c прийняв файли');
    await socket.send({ type: 'transfer_result', offer: { ...offer, status: 'failed' } });
    await expect(messages(page, 'error').last()).toHaveText('Сервер room-c не зміг отримати файли');
    await socket.send({ type: 'transfer_result', offer: { ...offer, status: 'rejected' } });
    await expect(messages(page, 'error').last()).toHaveText('Сервер room-c відхилив файли');
  });
});

test('registration changes from the socket update the waiting list', async ({ page, launch }) => {
  const server = await launch();
  const socket = await fakeSocket(page, {
    requires_approval: true,
    pending_users: { '123456': { username: 'Alice', registration_fields: { group: '10-A' } } },
  });
  await adminLogin(page, server.url);

  const item = page.locator('#pending-user-123456');
  await expect(item.locator('.pending-user-name')).toHaveText('👤 Alice');
  await expect(item.locator('.pending-user-data')).toHaveText('Group: 10-A');

  await socket.send({ type: 'registration_updated', user_id: '123456', username: 'Alicia', registration_fields: { group: '10-B' } });
  await expect(messages(page, 'success').last()).toHaveText('Користувач Alicia оновив дані');
  await expect(item.locator('.pending-user-data')).toHaveText('Group: 10-B');

  await socket.send({ type: 'user_approved', user_id: '123456' });
  await expect(item).toHaveCount(0);
  await expect(page.locator('#pending-users-list')).toHaveText('Немає користувачів, що очікують підтвердження');

  await socket.send({ type: 'answers_forced', forced: true, message: 'Відповіді тепер доступні для всіх учнів' });
  await expect(messages(page, 'success').last()).toHaveText('Відповіді тепер доступні для всіх учнів');
});

test('the admin socket connects again after the server closed it', async ({ page, launch }) => {
  const server = await launch();
  const sockets: WebSocketRoute[] = [];
  await page.routeWebSocket('**/ws/admin', (socket) => {
    sockets.push(socket);
    // Each connection gets its own first message, so the test can tell them apart
    const username = sockets.length === 1 ? 'Alice' : 'Bob';
    socket.send(
      JSON.stringify({
        type: 'initial_state',
        requires_approval: true,
        pending_users: { '100001': { username, registration_fields: {} } },
        tunnel: null,
        transfer_requests: [],
      }),
    );
  });
  await adminLogin(page, server.url);
  await expect(page.locator('.pending-user-name')).toHaveText(['👤 Alice']);

  await sockets[0].close();

  // 5 seconds later the page connects again and shows the new state
  await expect.poll(() => sockets.length, { timeout: 15000 }).toBe(2);
  await expect(page.locator('.pending-user-name')).toHaveText(['👤 Bob']);
});
