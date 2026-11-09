import { describe, expect, it } from 'vitest';
import { formatPoints, roundPoints } from './points';

describe('roundPoints', () => {
  it('rounds to 2 decimals', () => {
    expect(roundPoints(0.1 + 0.2)).toBe(0.3);
    expect(roundPoints(1.005 * 100)).toBe(100.5);
    expect(roundPoints(1.256)).toBe(1.26);
  });
  it('treats junk as 0', () => {
    expect(roundPoints(undefined)).toBe(0);
    expect(roundPoints(null)).toBe(0);
    expect(roundPoints('abc')).toBe(0);
  });
  it('accepts numeric strings', () => {
    expect(roundPoints('2.5')).toBe(2.5);
  });
});

describe('formatPoints', () => {
  it('shows whole numbers without decimals', () => {
    expect(formatPoints(3)).toBe('3');
    expect(formatPoints(3.0)).toBe('3');
    expect(formatPoints(0)).toBe('0');
  });
  it('shows up to 2 decimals', () => {
    expect(formatPoints(0.1 + 0.2)).toBe('0.3');
    expect(formatPoints(1.25)).toBe('1.25');
    expect(formatPoints(0.5)).toBe('0.5');
  });
  it('treats junk as 0', () => {
    expect(formatPoints(undefined)).toBe('0');
    expect(formatPoints('x')).toBe('0');
  });
});
