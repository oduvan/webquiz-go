import { roundPoints } from '../shared/points';
import { plain } from '../shared/text';

// A question as the live stats socket sends it: the whole question of the quiz file.
export interface LiveQuestion {
  id?: unknown;
  question?: unknown;
  image?: unknown;
  file?: unknown;
  points?: unknown;
  [key: string]: unknown;
}

// One cell: "think", "ok", "fail" or "timeout", and the seconds the answer took.
export interface CellState {
  state: string;
  time_taken: number | null;
}

// user id -> question id -> cell
export type LiveStats = Record<string, Record<string, CellState>>;

export interface UserStatistics {
  correct_count: number;
  total_answered: number;
  earned_points: number;
  attempted_points: number;
}

export interface QuestionStatistics {
  correct_count: number;
  total_attempts: number;
}

// A question is answered when it is right, wrong, or its time ran out ("timeout" counts as wrong).
export function isAnsweredState(state: unknown): boolean {
  return state === 'ok' || state === 'fail' || state === 'timeout';
}

// Old servers sent only the state text; it becomes {state, time_taken: null}.
export function normalizeStatsData(liveStats: Record<string, Record<string, unknown>>): LiveStats {
  const normalized: LiveStats = {};
  Object.keys(liveStats).forEach((userId) => {
    normalized[userId] = {};
    Object.keys(liveStats[userId]).forEach((questionId) => {
      const value = liveStats[userId][questionId];
      normalized[userId][questionId] =
        typeof value === 'string' ? { state: value, time_taken: null } : (value as CellState);
    });
  });
  return normalized;
}

// The points of a question: points as written in the quiz file, 1 when not set.
export function pointsOf(question: LiveQuestion | undefined): number {
  if (!question) return 1;
  return Number(question.points) || 1;
}

function findQuestion(questions: LiveQuestion[], questionId: string | number): LiveQuestion | undefined {
  const id = parseInt(String(questionId), 10);
  for (let i = 0; i < questions.length; i++) {
    if (questions[i].id === id) return questions[i];
  }
  return undefined;
}

// The score of every student: right answers and points, counted from the cells.
export function userStatistics(liveStats: LiveStats, questions: LiveQuestion[]): Record<string, UserStatistics> {
  const result: Record<string, UserStatistics> = {};
  Object.keys(liveStats).forEach((userId) => {
    let correctCount = 0;
    let totalAnswered = 0;
    let earnedPoints = 0;
    let attemptedPoints = 0;
    Object.keys(liveStats[userId]).forEach((questionId) => {
      const state = liveStats[userId][questionId].state;
      const points = pointsOf(findQuestion(questions, questionId));
      if (isAnsweredState(state)) {
        totalAnswered++;
        attemptedPoints += points;
        if (state === 'ok') {
          correctCount++;
          earnedPoints += points;
        }
      }
    });
    result[userId] = {
      correct_count: correctCount,
      total_answered: totalAnswered,
      earned_points: roundPoints(earnedPoints),
      attempted_points: roundPoints(attemptedPoints),
    };
  });
  return result;
}

// Right answers and answers of every question (question ids 1..N).
export function questionStatistics(liveStats: LiveStats, questions: LiveQuestion[]): Record<number, QuestionStatistics> {
  const result: Record<number, QuestionStatistics> = {};
  for (let id = 1; id <= questions.length; id++) {
    result[id] = { correct_count: 0, total_attempts: 0 };
  }
  Object.keys(liveStats).forEach((userId) => {
    Object.keys(liveStats[userId]).forEach((questionId) => {
      const state = liveStats[userId][questionId].state;
      if (isAnsweredState(state)) {
        const id = parseInt(questionId, 10);
        if (!result[id]) result[id] = { correct_count: 0, total_attempts: 0 };
        result[id].total_attempts++;
        if (state === 'ok') result[id].correct_count++;
      }
    });
  });
  return result;
}

// The students in progress and the ones who finished (newest first).
// The order of all students is the key order of the users object, as in the old page.
export function groupUsers(
  users: Record<string, unknown>,
  completedUsers: Record<string, boolean>,
  completionTimes: Record<string, string>,
): { all: string[]; inProgress: string[]; completed: string[] } {
  const all = Object.keys(users);
  const inProgress: string[] = [];
  const completed: string[] = [];
  all.forEach((userId) => (completedUsers[userId] ? completed : inProgress).push(userId));
  completed.sort((a, b) => {
    const timeA = completionTimes[a];
    const timeB = completionTimes[b];
    if (timeA && timeB) return new Date(timeB).getTime() - new Date(timeA).getTime();
    if (timeA) return -1;
    if (timeB) return 1;
    return 0;
  });
  return { all, inProgress, completed };
}

// "12s", "2m 5s"; null when the student still thinks.
export function formatTime(seconds: number | null | undefined): string | null {
  if (seconds === null || seconds === undefined) return null;
  if (seconds < 60) return Math.round(seconds) + 's';
  return Math.floor(seconds / 60) + 'm ' + Math.round(seconds % 60) + 's';
}

export function stateIcon(state: string): string {
  switch (state) {
    case 'think':
      return '🟡';
    case 'ok':
      return '🟢';
    case 'fail':
      return '🔴';
    case 'timeout':
      return '⏱';
    default:
      return '⚪';
  }
}

// The name shown for an image-only question: the file name without its extension.
export function imageFilename(path: unknown): string {
  const parts = plain(path).split('/');
  return parts[parts.length - 1].split('.').slice(0, -1).join('.');
}

// The sum of the question points, for a total the server did not send.
export function totalPointsOf(questions: LiveQuestion[]): number {
  return roundPoints(questions.reduce((sum, question) => sum + pointsOf(question), 0));
}
