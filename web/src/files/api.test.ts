import { describe, expect, it } from 'vitest';
import { canTransfer, csvTableUrl, formatFileSize } from './api';

describe('file helpers', () => {
  it('formats sizes like the old page', () => {
    expect(formatFileSize(0)).toBe('0 B');
    expect(formatFileSize(1536)).toBe('1.5 KB');
    expect(formatFileSize(1048576)).toBe('1 MB');
  });
  it('knows which files can be sent', () => {
    expect(canTransfer('quizzes', 'a.YAML')).toBe(true);
    expect(canTransfer('csv', 'a.csv')).toBe(true);
    expect(canTransfer('logs', 'a.csv')).toBe(false);
    expect(canTransfer('csv', '.hidden.csv')).toBe(false);
  });
  it('encodes the table address', () => {
    expect(csvTableUrl('a b.csv')).toBe('/files/csv/a%20b.csv');
  });
});
