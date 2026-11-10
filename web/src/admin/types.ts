// Data the admin page gets from the server (replies and admin socket messages).

// One quiz file of GET /api/admin/list-quizzes. The title is as written in
// the file (any YAML value), or null.
export interface QuizEntry {
  filename: string;
  title: unknown;
}

// A student who waits for approval: registration fields by field name.
export interface PendingUser {
  username: string;
  registration_fields: Record<string, unknown>;
}

// One file of an incoming transfer request, with what the server suggests.
export interface TransferFile {
  type: string; // quizzes, logs, csv, images, attachments
  name: string;
  size: number;
  target: string; // the name it gets here
  case: string; // new, same, different, in_use
  actions: string[]; // save, replace, rename, skip
  default_action: string;
  suggested_name: string;
}

// An incoming file transfer request from another server.
export interface TransferRequest {
  id: string;
  from: string;
  files: TransferFile[];
  received?: string;
  status?: string;
}

// The admin's choice for one file.
export interface TransferChoice {
  action: string;
  new_name: string;
}

// The tunnel status of the admin socket (initial_state and tunnel_status).
export interface TunnelInfo {
  configured?: boolean;
  server?: string | null;
  socket_name?: string | null;
  connected?: boolean;
  url?: string | null;
  error?: string | null;
  keys_status?: string;
}
