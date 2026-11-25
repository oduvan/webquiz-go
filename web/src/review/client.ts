import type { Translations } from '../shared/boot';

// The review page's logic: the flows of the Python version's
// templates/review.html. A teacher logs in (or comes from a trusted address),
// the page connects to /ws/review and shows one waiting answer at a time with
// two buttons, right and wrong. The server decides which answer each page
// gets; the page only shows the last review_state it got.

// What the review page shows of one answer (never the student's name).
export interface ReviewItem {
  review_id: string;
  question: unknown; // the question text as written in the quiz file
  image: unknown; // image path as written in the quiz file, or null
  answer: unknown; // the student's answer
  correct_value: unknown; // the sample answer for the teacher ("" when none)
  points: number;
}

export interface ReviewView {
  phase: 'starting' | 'login' | 'review';
  loginError: boolean;
  connectionLost: boolean; // the socket closed; the page connects again
  received: boolean; // a review_state came at least once
  pendingCount: number;
  hasManual: boolean;
  item: ReviewItem | null;
  buttonsEnabled: boolean; // off after a tap until the next state arrives
  helpEnabled: boolean;
  notice: string | null; // "another teacher has already checked this answer", for 4 seconds
}

// The parts of a socket the client uses (a fake one in the unit tests).
export interface SocketLike {
  readyState: number;
  onopen: ((event: unknown) => void) | null;
  onmessage: ((event: { data: unknown }) => void) | null;
  onclose: ((event: unknown) => void) | null;
  onerror: ((event: unknown) => void) | null;
  send(data: string): void;
}

const OPEN = 1; // WebSocket.OPEN
export const RECONNECT_DELAY_MS = 3000;
export const NOTICE_MS = 4000;
export const PING_INTERVAL_MS = 30000;

type Fetch = (url: string, init?: RequestInit) => Promise<{ ok: boolean }>;

// A message from the socket. Values come from JSON, so they are checked where used.
type Message = Record<string, any>;

export function fillCount(text: string | undefined, count: unknown): string {
  return (text || '').replace('{n}', String(count));
}

// The view after a review_state message.
export function applyReviewState(view: ReviewView, state: Message, T: Translations): ReviewView {
  const item: ReviewItem | null = state.item && typeof state.item === 'object' ? (state.item as ReviewItem) : null;
  return {
    ...view,
    received: true,
    pendingCount: Number(state.pending_count) || 0,
    hasManual: state.has_manual === true,
    item,
    // A new answer (or the same one again after a tap that did not count) can be decided
    buttonsEnabled: item !== null,
    // Every waiting answer is open on another page: the teacher may help with one
    helpEnabled: item === null,
    notice: state.already_decided === true ? T.review_already_decided : view.notice,
  };
}

// What the idle card says: nothing to check, no manual questions, or "help check".
export function idleContent(view: ReviewView, T: Translations): { text: string | null; helpText: string | null } {
  if (view.pendingCount > 0) {
    return { text: null, helpText: fillCount(T.review_help_text, view.pendingCount) };
  }
  return { text: view.hasManual ? T.review_idle : T.review_no_manual, helpText: null };
}

export class ReviewClient {
  private socket: SocketLike | null = null;
  private listeners: Array<() => void> = [];
  private reconnectTimer: ReturnType<typeof setTimeout> | undefined;
  private noticeTimer: ReturnType<typeof setTimeout> | undefined;
  private pingTimer: ReturnType<typeof setInterval> | undefined;
  private state: ReviewView = {
    phase: 'starting',
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

  constructor(
    readonly T: Translations,
    private readonly trusted: boolean,
    private readonly url: string,
    private readonly openSocket: (url: string) => SocketLike = (address) => new WebSocket(address) as unknown as SocketLike,
    private readonly request: Fetch = (address, init) => fetch(address, init),
  ) {}

  subscribe = (listener: () => void) => {
    this.listeners.push(listener);
    return () => {
      this.listeners = this.listeners.filter((item) => item !== listener);
    };
  };

  getSnapshot = (): ReviewView => this.state;

  private update(patch: Partial<ReviewView>) {
    this.state = { ...this.state, ...patch };
    this.listeners.slice().forEach((listener) => listener());
  }

  // A trusted address goes straight to the answers; otherwise an admin session
  // from the admin panel is enough, and without one the master key form shows.
  async start() {
    // Keep the connection alive through phone sleep and proxies
    this.pingTimer = setInterval(() => {
      if (this.socket && this.socket.readyState === OPEN) {
        this.socket.send(JSON.stringify({ type: 'ping' }));
      }
    }, PING_INTERVAL_MS);
    if (this.trusted) {
      this.openReview();
      return;
    }
    if (await this.hasSession()) {
      this.openReview();
      return;
    }
    this.update({ phase: 'login' });
  }

  stop() {
    clearInterval(this.pingTimer);
    clearTimeout(this.reconnectTimer);
    clearTimeout(this.noticeTimer);
    const socket = this.socket;
    this.socket = null;
    if (socket) {
      socket.onclose = null;
      (socket as unknown as { close?: () => void }).close?.();
    }
  }

  private async hasSession(): Promise<boolean> {
    try {
      const response = await this.request('/api/admin/check-session', { credentials: 'same-origin' });
      return response.ok;
    } catch (error) {
      console.log('Session check failed:', error);
      return false;
    }
  }

  login = async (masterKey: string) => {
    this.update({ loginError: false });
    try {
      const response = await this.request('/api/admin/auth', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ master_key: masterKey }),
        credentials: 'same-origin',
      });
      if (response.ok) {
        this.openReview();
        return;
      }
    } catch (error) {
      console.log('Login failed:', error);
    }
    this.update({ loginError: true });
  };

  private openReview() {
    this.update({ phase: 'review', loginError: false });
    this.connect();
  }

  private connect() {
    const socket = this.openSocket(this.url);
    this.socket = socket;
    let opened = false;
    socket.onopen = () => {
      opened = true;
      this.update({ connectionLost: false });
    };
    socket.onmessage = (event) => {
      let data: Message;
      try {
        data = JSON.parse(String(event.data));
      } catch (error) {
        return;
      }
      if (data && data.type === 'review_state') this.receiveState(data);
    };
    socket.onerror = () => {};
    socket.onclose = () => {
      if (this.socket !== socket) return;
      this.socket = null;
      this.update({ buttonsEnabled: false });
      if (!opened) {
        // Refused before it opened: the session may have ended (a server restart)
        void this.hasSession().then((ok) => {
          if (!ok && !this.trusted) {
            this.update({ phase: 'login' });
          } else {
            this.scheduleReconnect();
          }
        });
        return;
      }
      this.scheduleReconnect();
    };
  }

  private scheduleReconnect() {
    this.update({ connectionLost: true });
    clearTimeout(this.reconnectTimer);
    this.reconnectTimer = setTimeout(() => this.connect(), RECONNECT_DELAY_MS);
  }

  private receiveState(data: Message) {
    const next = applyReviewState(this.state, data, this.T);
    if (data.already_decided === true) {
      clearTimeout(this.noticeTimer);
      this.noticeTimer = setTimeout(() => this.update({ notice: null }), NOTICE_MS);
    }
    this.update(next);
  }

  // Until the next state arrives, a second tap does nothing.
  sendVerdict = (isCorrect: boolean) => {
    const item = this.state.item;
    if (!item || !this.state.buttonsEnabled || !this.socket || this.socket.readyState !== OPEN) return;
    this.update({ buttonsEnabled: false });
    this.socket.send(JSON.stringify({ type: 'verdict', review_id: item.review_id, is_correct: isCorrect }));
  };

  takeShared = () => {
    if (!this.socket || this.socket.readyState !== OPEN) return;
    this.update({ helpEnabled: false });
    this.socket.send(JSON.stringify({ type: 'take_shared' }));
  };
}
