import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fetchWithRetry } from './api';
import { ConnectionBanner } from './ui';

// React's act() needs this flag outside a test framework that sets it.
(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let container: HTMLDivElement;
let root: Root;

beforeEach(() => {
  vi.useFakeTimers();
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
});

afterEach(() => {
  act(() => root.unmount());
  container.remove();
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe('ConnectionBanner', () => {
  it('shows while a request waits and hides when it is done', async () => {
    const fetchMock = vi.fn().mockRejectedValueOnce(new TypeError('offline')).mockResolvedValue(new Response('{}'));
    vi.stubGlobal('fetch', fetchMock);
    act(() => root.render(<ConnectionBanner text="Connection lost" />));
    expect(container.querySelector('#connection-banner')).toBeNull();

    let done = false;
    const request = fetchWithRetry('api/x').then(() => (done = true));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
    expect(container.querySelector('#connection-banner')?.textContent).toBe('Connection lost');

    await act(async () => {
      await vi.advanceTimersByTimeAsync(1000);
      await request;
    });
    expect(done).toBe(true);
    expect(container.querySelector('#connection-banner')).toBeNull();
  });

  it('shows at once when a request already waits before the banner is drawn', async () => {
    // The quiz page starts its first request before React draws the page
    const fetchMock = vi.fn().mockRejectedValueOnce(new TypeError('offline')).mockResolvedValue(new Response('{}'));
    vi.stubGlobal('fetch', fetchMock);
    const request = fetchWithRetry('api/x');
    await vi.advanceTimersByTimeAsync(0);

    act(() => root.render(<ConnectionBanner text="Connection lost" />));
    expect(container.querySelector('#connection-banner')?.textContent).toBe('Connection lost');

    await act(async () => {
      await vi.advanceTimersByTimeAsync(1000);
      await request;
    });
    expect(container.querySelector('#connection-banner')).toBeNull();
  });
});
