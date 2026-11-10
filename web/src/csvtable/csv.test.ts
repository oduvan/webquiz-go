import { describe, expect, it } from 'vitest';
import { filterRows, parseCSV, parseTable, questionItems, userItems, usersCsvName } from './csv';

describe('parseCSV', () => {
  it('reads quoted commas, doubled quotes and line breaks', () => {
    const rows = parseCSV('a,b\r\n"x, y","say ""hi"""\r\n"two\nlines",z\r\n');
    expect(rows).toEqual([
      ['a', 'b'],
      ['x, y', 'say "hi"'],
      ['two\nlines', 'z'],
    ]);
  });
  it('accepts a last line without a break, LF endings, a BOM and empty lines', () => {
    expect(parseCSV('﻿a,b\n\n1,2')).toEqual([
      ['a', 'b'],
      ['1', '2'],
    ]);
  });
  it('keeps an empty last field', () => {
    expect(parseCSV('a,b\n1,')).toEqual([
      ['a', 'b'],
      ['1', ''],
    ]);
  });
});

describe('filters', () => {
  const table = parseTable('user_id,question,x\n2,B,1\n1,A,2\n1,B,3\n');
  it('lists questions and users sorted, with names from the users file', () => {
    expect(questionItems(table).map((i) => i.value)).toEqual(['A', 'B']);
    expect(userItems(table, { '1': 'Ann' }).map((i) => i.label)).toEqual(['2', 'Ann (1)']);
  });
  it('combines filters', () => {
    expect(filterRows(table, 'B', '1')).toEqual([['1', 'B', '3']]);
  });
  it('finds the users file of an answers file only', () => {
    expect(usersCsvName('q_0001.csv')).toBe('q_0001.users.csv');
    expect(usersCsvName('q_0001.users.csv')).toBe('');
    expect(usersCsvName('q.log')).toBe('');
  });
});
