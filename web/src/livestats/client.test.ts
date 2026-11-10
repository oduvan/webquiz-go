import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { LiveStatsClient, type SocketLike } from './client';

class FakeSocket implements SocketLike {
  readyState = 0;
  sent: string[] = [];
  onopen: ((event: unknown) => void) | null = null;
  onmessage: ((event: { data: unknown }) => void) | null = null;
  onclose: ((event: unknown) => void) | null = null;
  onerror: ((event: unknown) => void) | null = null;
  send(data: string) {
    this.sent.push(data);
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

function startClient() {
  const client = new LiveStatsClient('ws://test/ws/live-stats', () => {
    const socket = new FakeSocket();
    sockets.push(socket);
    return socket;
  });
  client.start();
  return client;
}

beforeEach(() => {
  vi.useFakeTimers();
  sockets = [];
});

afterEach(() => {
  vi.useRealTimers();
});

const questions = [
  { id: 1, question: 'Q1', points: 2 },
  { id: 2, question: 'Q2' },
];

describe('LiveStatsClient', () => {
  it('reads the initial state', () => {
    const client = startClient();
    sockets[0].open();
    sockets[0].receive({
      type: 'initial_state',
      users: { u1: 'Anna' },
      live_stats: { u1: { 1: { state: 'ok', time_taken: 2 } } },
      completed_users: { u1: false },
      completion_times: {},
      questions,
      total_questions: 2,
      total_points: 3,
    });
    const view = client.getSnapshot();
    expect(view.users).toEqual({ u1: 'Anna' });
    expect(view.liveStats.u1[1]).toEqual({ state: 'ok', time_taken: 2 });
    expect(view.totalPoints).toBe(3);
    expect(view.status.visible).toBe(false);
  });

  it('counts the total points itself when the server sends none', () => {
    const client = startClient();
    sockets[0].receive({ type: 'initial_state', users: {}, live_stats: {}, questions });
    expect(client.getSnapshot().totalPoints).toBe(3);
  });

  it('adds new students, updates cells and marks finished students', () => {
    const client = startClient();
    sockets[0].receive({ type: 'initial_state', users: {}, live_stats: {}, questions, total_points: 3 });
    sockets[0].receive({ type: 'user_registered', user_id: '123456', username: 'Bob', question_id: 1, state: 'think', time_taken: null });
    expect(client.getSnapshot().users).toEqual({ '123456': 'Bob' });
    expect(client.getSnapshot().liveStats['123456'][1]).toEqual({ state: 'think', time_taken: null });
    expect(client.getSnapshot().completedUsers['123456']).toBe(false);

    sockets[0].receive({ type: 'state_update', user_id: '123456', question_id: 1, state: 'timeout', time_taken: 30, completed: false });
    sockets[0].receive({ type: 'state_update', user_id: '123456', question_id: 2, state: 'ok', time_taken: 4, completed: true, completed_at: '2026-11-10T10:00:00' });
    const view = client.getSnapshot();
    expect(view.liveStats['123456']).toEqual({ 1: { state: 'timeout', time_taken: 30 }, 2: { state: 'ok', time_taken: 4 } });
    expect(view.completedUsers['123456']).toBe(true);
    expect(view.completionTimes['123456']).toBe('2026-11-10T10:00:00');
  });

  it('empties the table on a quiz switch and takes the new questions', () => {
    const client = startClient();
    sockets[0].receive({ type: 'initial_state', users: { u1: 'Anna' }, live_stats: {}, questions, total_points: 3 });
    sockets[0].receive({ type: 'quiz_switched', current_quiz: 'b.yaml', questions: [{ id: 1, question: 'B1', points: 0.5 }], total_questions: 1 });
    const view = client.getSnapshot();
    expect(view.users).toEqual({});
    expect(view.liveStats).toEqual({});
    expect(view.questions).toHaveLength(1);
    expect(view.totalPoints).toBe(0.5);
  });

  it('ignores pong and unknown messages', () => {
    const client = startClient();
    const before = client.getSnapshot();
    sockets[0].receive({ type: 'pong' });
    sockets[0].receive({ type: 'something new' });
    sockets[0].onmessage?.({ data: 'not json' });
    expect(client.getSnapshot()).toBe(before);
  });

  it('sends a ping every 30 seconds while connected', () => {
    startClient();
    sockets[0].open();
    vi.advanceTimersByTime(30000);
    expect(sockets[0].sent).toEqual(['{"type":"ping"}']);
  });

  it('reconnects after 1, 2, 4, 8 and 16 seconds, then gives up', () => {
    const client = startClient();
    sockets[0].open();
    sockets[0].close();
    expect(client.getSnapshot().status).toEqual({ visible: true, kind: 'connecting', text: '🔄 Reconnecting in 1s... (1/5)' });
    const delays = [1000, 2000, 4000, 8000, 16000];
    delays.forEach((delay, attempt) => {
      vi.advanceTimersByTime(delay - 1);
      expect(sockets).toHaveLength(attempt + 1);
      vi.advanceTimersByTime(1);
      expect(sockets).toHaveLength(attempt + 2);
      sockets[attempt + 1].close();
    });
    expect(client.getSnapshot().status).toEqual({
      visible: true,
      kind: 'disconnected',
      text: '🔴 Failed to reconnect. Refresh page to try again.',
    });
  });

  it('a connection that opens again starts the attempts from the beginning', () => {
    const client = startClient();
    sockets[0].close();
    vi.advanceTimersByTime(1000);
    sockets[1].open();
    expect(client.getSnapshot().status.visible).toBe(false);
    sockets[1].close();
    expect(client.getSnapshot().status.text).toBe('🔄 Reconnecting in 1s... (1/5)');
  });

  it('switches the filter', () => {
    const client = startClient();
    client.setFilter('completed');
    expect(client.getSnapshot().filter).toBe('completed');
  });
});
