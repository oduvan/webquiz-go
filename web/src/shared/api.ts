// Requests that fail because the connection dropped are repeated until they get
// through. A repeated answer or registration is safe: the server returns the stored
// result if the first attempt got through.
export const RETRY_DELAYS_MS = [1000, 2000, 4000, 5000]; // The last delay repeats
export const REQUEST_TIMEOUT_MS = 15000;
const RETRY_STATUSES = [502, 503, 504]; // A proxy or the tunnel cannot reach the server

type WaitingListener = (waiting: boolean) => void;

const listeners: WaitingListener[] = [];
let requestsWaiting = 0;

// The listener gets true when the first request starts waiting for the connection
// and false when the last one is done. Returns a function that removes the listener.
export function onConnectionWaiting(listener: WaitingListener): () => void {
  listeners.push(listener);
  return () => {
    const index = listeners.indexOf(listener);
    if (index !== -1) listeners.splice(index, 1);
  };
}

function changeWaiting(change: number) {
  const before = requestsWaiting > 0;
  requestsWaiting += change;
  const after = requestsWaiting > 0;
  if (before !== after) {
    listeners.slice().forEach((listener) => listener(after));
  }
}

export async function fetchWithRetry(url: string, options: RequestInit = {}): Promise<Response> {
  let attempt = 0;
  let waiting = false;
  try {
    for (;;) {
      const controller = new AbortController();
      const timer = setTimeout(() => controller.abort(), REQUEST_TIMEOUT_MS);
      try {
        const response = await fetch(url, { ...options, signal: controller.signal });
        if (RETRY_STATUSES.indexOf(response.status) === -1) {
          return response;
        }
      } catch (error) {
        console.log('Request to ' + url + ' failed, retrying:', error);
      } finally {
        clearTimeout(timer);
      }

      if (!waiting) {
        waiting = true;
        changeWaiting(1);
      }
      const delay = RETRY_DELAYS_MS[Math.min(attempt, RETRY_DELAYS_MS.length - 1)];
      attempt++;
      await new Promise((resolve) => setTimeout(resolve, delay));
    }
  } finally {
    if (waiting) {
      changeWaiting(-1);
    }
  }
}
