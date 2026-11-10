// Calls of the file manager page. Each helper returns the status and the parsed answer.
export interface FileInfo {
  name: string;
  size: number;
  modified: string;
}

export type FileType = 'csv' | 'logs' | 'quizzes';
export type FileLists = Record<FileType, FileInfo[]>;

export interface OutgoingOffer {
  id: string;
  to: string;
  files: { type: string; name: string }[];
  status: string;
}

export interface TransferState {
  available: boolean;
  name: string | null;
  outgoing: OutgoingOffer[];
}

// Reads an answer as JSON; an answer that is not JSON gives an empty object.
export async function readJson<T = Record<string, unknown>>(response: Response): Promise<T> {
  try {
    return (await response.json()) as T;
  } catch (e) {
    return {} as T;
  }
}

export function sendJson(url: string, method: string, body: unknown): Promise<Response> {
  return fetch(url, {
    method,
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
    credentials: 'same-origin',
  });
}

export function getWithSession(url: string): Promise<Response> {
  return fetch(url, { credentials: 'same-origin' });
}

// Address of the CSV table viewer page for a file
export function csvTableUrl(filename: string): string {
  return `/files/csv/${encodeURIComponent(filename)}`;
}

// Must match FILE_TYPES in the transfer code of the server
const TRANSFER_EXTENSIONS: Record<string, string[]> = { quizzes: ['.yaml', '.yml'], logs: ['.log'], csv: ['.csv'] };

export function canTransfer(type: string, filename: string): boolean {
  const lower = filename.toLowerCase();
  return (TRANSFER_EXTENSIONS[type] || []).some((ext) => lower.endsWith(ext)) && !filename.startsWith('.');
}

export function formatFileSize(bytes: number): string {
  if (bytes === 0) return '0 B';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
}

export function formatDate(isoDate: string): string {
  return new Date(isoDate).toLocaleString();
}
