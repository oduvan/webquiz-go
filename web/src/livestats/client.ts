import { roundPoints } from '../shared/points';
import { normalizeStatsData, totalPointsOf, type LiveQuestion, type LiveStats } from './stats';
import { TEXTS } from './texts';

// The live stats page's connection, translated from the LiveStatsClient class of
// Python's templates/live_stats.html: the same messages and the same reconnect rules.

export type Filter = 'all' | 'inProgress' | 'completed';

export interface LiveView {
  status: { visible: boolean; kind: 'connected' | 'disconnected' | 'connecting'; text: string };
  filter: Filter;
  users: Record<string, string>; // user id -> name
  liveStats: LiveStats;
  completedUsers: Record<string, boolean>;
  completionTimes: Record<string, string>;
  questions: LiveQuestion[];
  totalPoints: number;
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

// A message from the socket. Values come from JSON, so they are checked where used.
type Message = Record<string, any>;

export class LiveStatsClient {
  readonly maxReconnectAttempts = 5;
  private readonly reconnectDelay = 1000;
  private reconnectAttempts = 0;
  private socket: SocketLike | null = null;
  private listeners: Array<() => void> = [];
  private state: LiveView = {
    status: { visible: false, kind: 'connecting', text: '' },
    filter: 'all',
    users: {},
    liveStats: {},
    completedUsers: {},
    completionTimes: {},
    questions: [],
    totalPoints: 0,
  };

  constructor(
    private readonly url: string,
    private readonly openSocket: (url: string) => SocketLike = (address) => new WebSocket(address) as unknown as SocketLike,
  ) {}

  subscribe = (listener: () => void) => {
    this.listeners.push(listener);
    return () => {
      this.listeners = this.listeners.filter((item) => item !== listener);
    };
  };

  getSnapshot = (): LiveView => this.state;

  private update(patch: Partial<LiveView>) {
    this.state = { ...this.state, ...patch };
    this.listeners.slice().forEach((listener) => listener());
  }

  start() {
    this.connect();
    // A ping every 30 seconds keeps the connection alive
    setInterval(() => {
      if (this.socket && this.socket.readyState === OPEN) {
        this.socket.send(JSON.stringify({ type: 'ping' }));
      }
    }, 30000);
  }

  setFilter = (filter: Filter) => this.update({ filter });

  private connect() {
    this.hideStatus();
    try {
      const socket = this.openSocket(this.url);
      this.socket = socket;
      socket.onopen = () => {
        this.hideStatus();
        this.reconnectAttempts = 0;
        console.log('WebSocket connected');
      };
      socket.onmessage = (event) => {
        try {
          this.handleMessage(JSON.parse(String(event.data)));
        } catch (error) {
          console.error('Error parsing WebSocket message:', error);
        }
      };
      socket.onclose = () => {
        this.updateStatus('disconnected', TEXTS.disconnected);
        console.log('WebSocket disconnected');
        this.scheduleReconnect();
      };
      socket.onerror = (error) => {
        console.error('WebSocket error:', error);
        this.updateStatus('disconnected', TEXTS.connectionError);
      };
    } catch (error) {
      console.error('Error creating WebSocket:', error);
      this.updateStatus('disconnected', TEXTS.failedToConnect);
      this.scheduleReconnect();
    }
  }

  // Up to 5 attempts, after 1, 2, 4, 8 and 16 seconds.
  private scheduleReconnect() {
    if (this.reconnectAttempts < this.maxReconnectAttempts) {
      this.reconnectAttempts++;
      const delay = this.reconnectDelay * Math.pow(2, this.reconnectAttempts - 1);
      this.updateStatus('connecting', TEXTS.reconnecting(delay / 1000, this.reconnectAttempts, this.maxReconnectAttempts));
      setTimeout(() => this.connect(), delay);
    } else {
      this.updateStatus('disconnected', TEXTS.failedToReconnect);
    }
  }

  private updateStatus(kind: LiveView['status']['kind'], text: string) {
    this.update({ status: { visible: true, kind, text } });
  }

  private hideStatus() {
    if (this.state.status.visible) this.update({ status: { ...this.state.status, visible: false } });
  }

  handleMessage(data: Message) {
    switch (data.type) {
      case 'initial_state':
        this.handleInitialState(data);
        break;
      case 'user_registered':
        this.handleUserRegistered(data);
        break;
      case 'state_update':
        this.handleStateUpdate(data);
        break;
      case 'quiz_switched':
        this.handleQuizSwitched(data);
        break;
      case 'pong':
        break; // the answer to the keep-alive ping
      default:
        console.log('Unknown message type:', data.type);
    }
  }

  private handleInitialState(data: Message) {
    const questions: LiveQuestion[] = data.questions || [];
    this.update({
      users: { ...(data.users || {}) },
      liveStats: normalizeStatsData(data.live_stats || {}),
      completedUsers: { ...(data.completed_users || {}) },
      completionTimes: { ...(data.completion_times || {}) },
      questions,
      totalPoints: data.total_points ? roundPoints(data.total_points) : totalPointsOf(questions),
    });
  }

  private setCell(userId: string, questionId: string, state: string, timeTaken: number | null): LiveStats {
    const liveStats = { ...this.state.liveStats };
    liveStats[userId] = { ...(liveStats[userId] || {}) };
    liveStats[userId][questionId] = { state, time_taken: timeTaken };
    return liveStats;
  }

  private handleUserRegistered(data: Message) {
    const userId = String(data.user_id);
    this.update({
      users: { ...this.state.users, [userId]: data.username },
      liveStats: this.setCell(userId, String(data.question_id), data.state, data.time_taken),
      completedUsers: { ...this.state.completedUsers, [userId]: false }, // a new student has not finished
    });
  }

  private handleStateUpdate(data: Message) {
    const userId = String(data.user_id);
    const completed = data.completed || false;
    const completionTimes = { ...this.state.completionTimes };
    if (completed && data.completed_at) {
      completionTimes[userId] = data.completed_at;
    }
    this.update({
      liveStats: this.setCell(userId, String(data.question_id), data.state, data.time_taken),
      completedUsers: { ...this.state.completedUsers, [userId]: completed },
      completionTimes,
    });
  }

  // A new run: no students yet. The message has no total points, so they are
  // counted from the new questions (the old page kept the previous quiz's total).
  private handleQuizSwitched(data: Message) {
    const questions: LiveQuestion[] = data.questions || [];
    this.update({ users: {}, liveStats: {}, completedUsers: {}, questions, totalPoints: totalPointsOf(questions) });
  }
}
