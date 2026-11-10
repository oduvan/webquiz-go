import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { connectAdminSocket } from './socket';

// A WebSocket that the test opens and closes.
class FakeSocket {
  static made: FakeSocket[] = [];
  onmessage: ((event: { data: string }) => void) | null = null;
  onclose: (() => void) | null = null;
  onerror: ((event: unknown) => void) | null = null;
  closed = false;

  constructor(public url: string) {
    FakeSocket.made.push(this);
  }

  close() {
    if (this.closed) return;
    this.closed = true;
    if (this.onclose) this.onclose();
  }
}

describe('admin socket', () => {
  beforeEach(() => {
    FakeSocket.made = [];
    vi.useFakeTimers();
    vi.stubGlobal('WebSocket', FakeSocket);
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it('connects to /ws/admin and passes the messages on', () => {
    vi.spyOn(console, 'error').mockImplementation(() => undefined);
    const received: unknown[] = [];
    const stop = connectAdminSocket((data) => received.push(data));

    expect(FakeSocket.made).toHaveLength(1);
    expect(FakeSocket.made[0].url).toBe(`ws://${window.location.host}/ws/admin`);
    FakeSocket.made[0].onmessage!({ data: '{"type":"initial_state"}' });
    FakeSocket.made[0].onmessage!({ data: 'not JSON' }); // logged, not passed on
    expect(received).toEqual([{ type: 'initial_state' }]);
    stop();
  });

  it('connects again 5 seconds after the connection closed', () => {
    const stop = connectAdminSocket(() => undefined);

    FakeSocket.made[0].close(); // the server went away
    vi.advanceTimersByTime(4999);
    expect(FakeSocket.made).toHaveLength(1);
    vi.advanceTimersByTime(1);
    expect(FakeSocket.made).toHaveLength(2);

    // ... and again each time
    FakeSocket.made[1].close();
    vi.advanceTimersByTime(5000);
    expect(FakeSocket.made).toHaveLength(3);
    stop();
  });

  it('does not connect again after it was stopped', () => {
    const stop = connectAdminSocket(() => undefined);
    stop();
    expect(FakeSocket.made[0].closed).toBe(true);
    vi.advanceTimersByTime(20000);
    expect(FakeSocket.made).toHaveLength(1);
  });

  it('a stop while it waits to connect again cancels the new connection', () => {
    const stop = connectAdminSocket(() => undefined);
    FakeSocket.made[0].close();
    stop();
    vi.advanceTimersByTime(20000);
    expect(FakeSocket.made).toHaveLength(1);
  });
});
