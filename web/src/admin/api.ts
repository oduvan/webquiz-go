// Requests of the admin page. They go out once, like the old page's fetch()
// calls (no retries: the admin sees the error and can try again). The admin
// session cookie goes with every request.

// Replies are read the way the old page read them: field by field, as JSON.
export type Json = any;

export interface Reply {
  ok: boolean;
  status: number;
  data: Json;
}

// Sends a request and reads the JSON reply. A reply that is not JSON throws,
// as response.json() did on the old page.
export async function request(method: string, url: string, body?: unknown): Promise<Reply> {
  const options: RequestInit = { method, credentials: 'same-origin' };
  if (body !== undefined) {
    options.headers = { 'Content-Type': 'application/json' };
    options.body = typeof body === 'string' ? body : JSON.stringify(body);
  }
  const response = await fetch(url, options);
  const data = await response.json();
  return { ok: response.ok, status: response.status, data };
}

export function get(url: string): Promise<Reply> {
  return request('GET', url);
}

export function post(url: string, body?: unknown): Promise<Reply> {
  return request('POST', url, body);
}

export function put(url: string, body?: unknown): Promise<Reply> {
  return request('PUT', url, body);
}

export function errorText(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

// The path of a quiz file in the admin API. The name is used as it is, like
// the old page did (`/api/admin/quiz/${filename}`), but encoded so that a name
// with "#", "?" or "%" reaches the server whole.
export function quizURL(filename: string): string {
  return '/api/admin/quiz/' + encodeURIComponent(filename);
}
