import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fetchWithRetry, onConnectionWaiting } from './api';

function response(status: number): Response {
  return new Response('{}', { status });
}

beforeEach(() => {
  vi.useFakeTimers();
});
afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe('fetchWithRetry', () => {
  it('returns a good response at once', async () => {
    const fetchMock = vi.fn().mockResolvedValue(response(200));
    vi.stubGlobal('fetch', fetchMock);
    const res = await fetchWithRetry('api/x');
    expect(res.status).toBe(200);
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it('does not retry a 400 or a 500', async () => {
    for (const status of [400, 404, 409, 500]) {
      const fetchMock = vi.fn().mockResolvedValue(response(status));
      vi.stubGlobal('fetch', fetchMock);
      const res = await fetchWithRetry('api/x');
      expect(res.status).toBe(status);
      expect(fetchMock).toHaveBeenCalledTimes(1);
    }
  });

  it('retries after 1 s, 2 s, 4 s, then every 5 s on network errors', async () => {
    const fetchMock = vi.fn().mockRejectedValue(new TypeError('offline'));
    vi.stubGlobal('fetch', fetchMock);
    let result: Response | undefined;
    const promise = fetchWithRetry('api/x').then((r) => (result = r));

    await vi.advanceTimersByTimeAsync(0);
    expect(fetchMock).toHaveBeenCalledTimes(1);
    await vi.advanceTimersByTimeAsync(999);
    expect(fetchMock).toHaveBeenCalledTimes(1);
    await vi.advanceTimersByTimeAsync(1);
    expect(fetchMock).toHaveBeenCalledTimes(2);
    await vi.advanceTimersByTimeAsync(2000);
    expect(fetchMock).toHaveBeenCalledTimes(3);
    await vi.advanceTimersByTimeAsync(4000);
    expect(fetchMock).toHaveBeenCalledTimes(4);
    await vi.advanceTimersByTimeAsync(5000);
    expect(fetchMock).toHaveBeenCalledTimes(5);
    await vi.advanceTimersByTimeAsync(5000);
    expect(fetchMock).toHaveBeenCalledTimes(6);

    fetchMock.mockResolvedValue(response(200));
    await vi.advanceTimersByTimeAsync(5000);
    await promise;
    expect(result?.status).toBe(200);
  });

  it.each([502, 503, 504])('retries on %i', async (status) => {
    const fetchMock = vi.fn().mockResolvedValueOnce(response(status)).mockResolvedValueOnce(response(200));
    vi.stubGlobal('fetch', fetchMock);
    const promise = fetchWithRetry('api/x');
    await vi.advanceTimersByTimeAsync(1000);
    const res = await promise;
    expect(res.status).toBe(200);
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it('aborts a request after 15 s and retries', async () => {
    const signals: AbortSignal[] = [];
    const fetchMock = vi.fn((_url: string, init?: RequestInit) => {
      const signal = init!.signal as AbortSignal;
      signals.push(signal);
      if (signals.length > 1) return Promise.resolve(response(200));
      return new Promise<Response>((_resolve, reject) => {
        signal.addEventListener('abort', () => reject(new DOMException('aborted', 'AbortError')));
      });
    });
    vi.stubGlobal('fetch', fetchMock);
    const promise = fetchWithRetry('api/x');
    await vi.advanceTimersByTimeAsync(14999);
    expect(signals[0].aborted).toBe(false);
    await vi.advanceTimersByTimeAsync(1);
    expect(signals[0].aborted).toBe(true);
    await vi.advanceTimersByTimeAsync(1000);
    const res = await promise;
    expect(res.status).toBe(200);
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it('passes the options through', async () => {
    const fetchMock = vi.fn().mockResolvedValue(response(200));
    vi.stubGlobal('fetch', fetchMock);
    await fetchWithRetry('api/x', { method: 'POST', body: '{}' });
    expect(fetchMock.mock.calls[0][1].method).toBe('POST');
    expect(fetchMock.mock.calls[0][1].body).toBe('{}');
  });

  it('tells the listener when requests wait and when they are done', async () => {
    const states: boolean[] = [];
    const off = onConnectionWaiting((waiting) => states.push(waiting));
    const fetchMock = vi
      .fn()
      .mockRejectedValueOnce(new TypeError('offline'))
      .mockRejectedValueOnce(new TypeError('offline'))
      .mockResolvedValue(response(200));
    vi.stubGlobal('fetch', fetchMock);
    const a = fetchWithRetry('api/a');
    const b = fetchWithRetry('api/b');
    await vi.advanceTimersByTimeAsync(0);
    expect(states).toEqual([true]); // two waiting requests, one banner
    await vi.advanceTimersByTimeAsync(1000);
    await Promise.all([a, b]);
    expect(states).toEqual([true, false]);
    off();
  });

  it('does not touch the listener when nothing fails', async () => {
    const states: boolean[] = [];
    const off = onConnectionWaiting((waiting) => states.push(waiting));
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(response(200)));
    await fetchWithRetry('api/x');
    expect(states).toEqual([]);
    off();
  });
});
