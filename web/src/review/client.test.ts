import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { applyReviewState, idleContent, NOTICE_MS, RECONNECT_DELAY_MS, ReviewClient, type ReviewView, type SocketLike } from './client';

const T = {
  review_idle: 'No answers to check.',
  review_no_manual: 'No manual questions.',
  review_help_text: 'Open on other pages: {n}.',
  review_already_decided: 'Already checked.',
  review_waiting_count: 'Waiting: {n}',
};

class FakeSocket implements SocketLike {
  readyState = 0;
  sent: Array<Record<string, unknown>> = [];
  onopen: ((event: unknown) => void) | null = null;
  onmessage: ((event: { data: unknown }) => void) | null = null;
  onclose: ((event: unknown) => void) | null = null;
  onerror: ((event: unknown) => void) | null = null;
  send(data: string) {
    this.sent.push(JSON.parse(data));
  }
  open() {
    this.readyState = 1;
    this.onopen?.({});
  }
  receive(message: unknown) {
    this.onmessage?.({ data: JSON.stringify(message) });
  }
  close() {
    this.readyState = 3;
    this.onclose?.({});
  }
}

let sockets: FakeSocket[] = [];
let requests: Array<{ url: string; init?: RequestInit }> = [];
let replies: Record<string, boolean> = {};

function makeClient(trusted = false) {
  return new ReviewClient(
    T,
    trusted,
    'ws://test/ws/review',
    () => {
      const socket = new FakeSocket();
      sockets.push(socket);
      return socket;
    },
    async (url, init) => {
      requests.push({ url, init });
      return { ok: replies[url] === true };
    },
  );
}

const item = { review_id: '1', question: 'Why?', image: null, answer: 'Because', correct_value: 'Sample', points: 2 };

function state(patch: Record<string, unknown> = {}) {
  return { type: 'review_state', item: null, pending_count: 0, has_manual: true, already_decided: false, ...patch };
}

beforeEach(() => {
  vi.useFakeTimers();
  sockets = [];
  requests = [];
  replies = {};
});

afterEach(() => {
  vi.useRealTimers();
});

describe('starting the review page', () => {
  it('connects at once from a trusted address', async () => {
    const client = makeClient(true);
    await client.start();
    expect(client.getSnapshot().phase).toBe('review');
    expect(sockets.length).toBe(1);
    expect(requests.length).toBe(0);
    client.stop();
  });

  it('connects with the session of the admin panel', async () => {
    replies['/api/admin/check-session'] = true;
    const client = makeClient();
    await client.start();
    expect(client.getSnapshot().phase).toBe('review');
    expect(sockets.length).toBe(1);
    client.stop();
  });

  it('asks for the master key without a session, then logs in', async () => {
    const client = makeClient();
    await client.start();
    expect(client.getSnapshot().phase).toBe('login');
    expect(sockets.length).toBe(0);

    await client.login('wrong');
    expect(client.getSnapshot().loginError).toBe(true);
    expect(client.getSnapshot().phase).toBe('login');

    replies['/api/admin/auth'] = true;
    await client.login('test123');
    const auth = requests[requests.length - 1];
    expect(auth.url).toBe('/api/admin/auth');
    expect(JSON.parse(String(auth.init?.body))).toEqual({ master_key: 'test123' });
    expect(client.getSnapshot().phase).toBe('review');
    expect(client.getSnapshot().loginError).toBe(false);
    expect(sockets.length).toBe(1);
    client.stop();
  });
});

describe('review states', () => {
  async function connected() {
    const client = makeClient(true);
    await client.start();
    sockets[0].open();
    return client;
  }

  it('shows the answer and enables the buttons', async () => {
    const client = await connected();
    sockets[0].receive(state({ item, pending_count: 1 }));
    const view = client.getSnapshot();
    expect(view.item).toEqual(item);
    expect(view.pendingCount).toBe(1);
    expect(view.buttonsEnabled).toBe(true);
    client.stop();
  });

  it('sends one verdict and disables the buttons until the next state', async () => {
    const client = await connected();
    sockets[0].receive(state({ item, pending_count: 1 }));
    client.sendVerdict(true);
    client.sendVerdict(false); // a second tap does nothing
    expect(sockets[0].sent).toEqual([{ type: 'verdict', review_id: '1', is_correct: true }]);
    expect(client.getSnapshot().buttonsEnabled).toBe(false);

    // The same answer again (the verdict did not count): the buttons work again
    sockets[0].receive(state({ item, pending_count: 1 }));
    expect(client.getSnapshot().buttonsEnabled).toBe(true);
    client.stop();
  });

  it('offers help when every waiting answer is open on another page', async () => {
    const client = await connected();
    sockets[0].receive(state({ pending_count: 2 }));
    expect(idleContent(client.getSnapshot(), T)).toEqual({ text: null, helpText: 'Open on other pages: 2.' });
    expect(client.getSnapshot().helpEnabled).toBe(true);
    client.takeShared();
    expect(sockets[0].sent).toEqual([{ type: 'take_shared' }]);
    expect(client.getSnapshot().helpEnabled).toBe(false);
    client.stop();
  });

  it('says "another teacher checked it" for a while', async () => {
    const client = await connected();
    sockets[0].receive(state({ already_decided: true }));
    expect(client.getSnapshot().notice).toBe('Already checked.');
    vi.advanceTimersByTime(NOTICE_MS);
    expect(client.getSnapshot().notice).toBe(null);
    client.stop();
  });

  it('pings every 30 seconds', async () => {
    const client = await connected();
    vi.advanceTimersByTime(30000);
    expect(sockets[0].sent).toEqual([{ type: 'ping' }]);
    client.stop();
  });

  it('connects again after the connection drops', async () => {
    const client = await connected();
    sockets[0].receive(state({ item, pending_count: 1 }));
    sockets[0].close();
    expect(client.getSnapshot().connectionLost).toBe(true);
    expect(client.getSnapshot().buttonsEnabled).toBe(false);
    vi.advanceTimersByTime(RECONNECT_DELAY_MS);
    expect(sockets.length).toBe(2);
    sockets[1].open();
    expect(client.getSnapshot().connectionLost).toBe(false);
    client.stop();
  });

  it('asks for the key again when the server refuses the socket and the session is gone', async () => {
    replies['/api/admin/check-session'] = true;
    const client = makeClient();
    await client.start();
    replies['/api/admin/check-session'] = false;
    sockets[0].close(); // closed before it opened
    await vi.runOnlyPendingTimersAsync();
    expect(client.getSnapshot().phase).toBe('login');
    client.stop();
  });
});

describe('applyReviewState', () => {
  const empty: ReviewView = {
    phase: 'review',
    loginError: false,
    connectionLost: false,
    received: false,
    pendingCount: 0,
    hasManual: false,
    item: null,
    buttonsEnabled: false,
    helpEnabled: false,
    notice: null,
  };

  it('tells an empty queue from a quiz without manual questions', () => {
    const idle = applyReviewState(empty, state(), T);
    expect(idle.received).toBe(true);
    expect(idleContent(idle, T)).toEqual({ text: 'No answers to check.', helpText: null });
    const none = applyReviewState(empty, state({ has_manual: false }), T);
    expect(idleContent(none, T)).toEqual({ text: 'No manual questions.', helpText: null });
  });
});
