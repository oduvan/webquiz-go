import { describe, expect, it } from 'vitest';
import {
  formatTime,
  groupUsers,
  imageFilename,
  isAnsweredState,
  normalizeStatsData,
  questionStatistics,
  stateIcon,
  totalPointsOf,
  userStatistics,
} from './stats';

const questions = [
  { id: 1, question: 'Q1', points: 0.1 },
  { id: 2, question: 'Q2', points: 0.2 },
  { id: 3, question: 'Q3' },
];

describe('cells', () => {
  it('counts right, wrong and timed-out answers as answered', () => {
    expect(['ok', 'fail', 'timeout'].map(isAnsweredState)).toEqual([true, true, true]);
    expect(['think', 'empty', undefined].map(isAnsweredState)).toEqual([false, false, false]);
  });
  it('turns old text cells into objects', () => {
    expect(normalizeStatsData({ u1: { 1: 'ok', 2: { state: 'fail', time_taken: 3 } } })).toEqual({
      u1: { 1: { state: 'ok', time_taken: null }, 2: { state: 'fail', time_taken: 3 } },
    });
  });
  it('shows an icon per state', () => {
    expect(['think', 'ok', 'fail', 'timeout', 'empty'].map(stateIcon)).toEqual(['🟡', '🟢', '🔴', '⏱', '⚪']);
  });
  it('writes seconds and minutes', () => {
    expect(formatTime(null)).toBeNull();
    expect(formatTime(undefined)).toBeNull();
    expect(formatTime(4.6)).toBe('5s');
    expect(formatTime(125.4)).toBe('2m 5s');
  });
});

describe('statistics', () => {
  const liveStats = {
    a: { 1: { state: 'ok', time_taken: 1 }, 2: { state: 'ok', time_taken: 1 }, 3: { state: 'fail', time_taken: 1 } },
    b: { 1: { state: 'timeout', time_taken: 30 }, 2: { state: 'think', time_taken: null } },
  };
  it('counts points per student without float noise', () => {
    const stats = userStatistics(liveStats, questions);
    expect(stats.a).toEqual({ correct_count: 2, total_answered: 3, earned_points: 0.3, attempted_points: 1.3 });
    expect(stats.b).toEqual({ correct_count: 0, total_answered: 1, earned_points: 0, attempted_points: 0.1 });
  });
  it('counts right answers and answers per question', () => {
    expect(questionStatistics(liveStats, questions)).toEqual({
      1: { correct_count: 1, total_attempts: 2 },
      2: { correct_count: 1, total_attempts: 1 },
      3: { correct_count: 0, total_attempts: 1 },
    });
  });
  it('adds the points of all questions', () => {
    expect(totalPointsOf(questions)).toBe(1.3);
    expect(totalPointsOf([])).toBe(0);
  });
});

describe('groupUsers', () => {
  it('splits students and puts the latest finisher first', () => {
    const users = { '300000': 'C', '100000': 'A', '200000': 'B', '400000': 'D' };
    const completed = { '100000': true, '200000': true, '400000': true };
    const times = { '100000': '2026-11-10T10:00:00', '200000': '2026-11-10T10:05:00' };
    const groups = groupUsers(users, completed, times);
    // The key order of a JavaScript object: number-like keys ascending, as in the old page
    expect(groups.all).toEqual(['100000', '200000', '300000', '400000']);
    expect(groups.inProgress).toEqual(['300000']);
    expect(groups.completed).toEqual(['200000', '100000', '400000']);
  });
});

describe('imageFilename', () => {
  it('drops the folder and the extension', () => {
    expect(imageFilename('/imgs/solar.system.png')).toBe('solar.system');
  });
});
