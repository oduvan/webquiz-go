// Reads CSV text the way Python's csv module writes it: fields in quotes
// may hold commas, doubled quotes and line breaks; lines end with \r\n or \n.
export function parseCSV(input: string): string[][] {
  let text = input;
  if (text.charCodeAt(0) === 0xfeff) {
    text = text.slice(1); // byte order mark
  }
  const rows: string[][] = [];
  let row: string[] = [];
  let field = '';
  let inQuotes = false;
  for (let i = 0; i < text.length; i++) {
    const char = text[i];
    if (inQuotes) {
      if (char === '"' && text[i + 1] === '"') {
        field += '"';
        i++;
      } else if (char === '"') {
        inQuotes = false;
      } else {
        field += char;
      }
    } else if (char === '"') {
      inQuotes = true;
    } else if (char === ',') {
      row.push(field);
      field = '';
    } else if (char === '\r' || char === '\n') {
      if (char === '\r' && text[i + 1] === '\n') {
        i++;
      }
      row.push(field);
      rows.push(row);
      row = [];
      field = '';
    } else {
      field += char;
    }
  }
  // The last line may have no line break at the end
  if (field !== '' || row.length > 0) {
    row.push(field);
    rows.push(row);
  }
  // Drop empty lines
  return rows.filter((r) => !(r.length === 1 && r[0] === ''));
}

export interface ParsedCsv {
  header: string[];
  rows: string[][];
  questionColumn: number;
  userIdColumn: number;
  usernameColumn: number;
}

export function parseTable(text: string): ParsedCsv {
  const all = parseCSV(text);
  const header = all.length > 0 ? all[0] : [];
  return {
    header,
    rows: all.slice(1),
    questionColumn: header.indexOf('question'),
    userIdColumn: header.indexOf('user_id'),
    usernameColumn: header.indexOf('username'),
  };
}

export interface FilterItem {
  value: string;
  label: string;
}

export function compareLabels(a: FilterItem, b: FilterItem): number {
  return a.label.localeCompare(b.label, undefined, { numeric: true, sensitivity: 'base' });
}

// user_id -> username from a users CSV text (empty when the columns are missing)
export function usernamesFromUsersCsv(text: string): Record<string, string> {
  const rows = parseCSV(text);
  const names: Record<string, string> = {};
  if (rows.length < 2) return names;
  const idColumn = rows[0].indexOf('user_id');
  const nameColumn = rows[0].indexOf('username');
  if (idColumn < 0 || nameColumn < 0) return names;
  rows.slice(1).forEach((row) => {
    if (row[idColumn]) names[row[idColumn]] = row[nameColumn] || '';
  });
  return names;
}

export function userKey(table: ParsedCsv, row: string[]): string {
  return table.userIdColumn >= 0 ? row[table.userIdColumn] : row[table.usernameColumn];
}

export function userLabel(table: ParsedCsv, row: string[], names: Record<string, string>): string {
  const userId = table.userIdColumn >= 0 ? row[table.userIdColumn] : '';
  let username = table.usernameColumn >= 0 ? row[table.usernameColumn] : '';
  if (!username && userId && names[userId]) {
    username = names[userId];
  }
  if (username && userId) {
    return `${username} (${userId})`;
  }
  return username || userId;
}

export function questionItems(table: ParsedCsv): FilterItem[] {
  if (table.questionColumn < 0) return [];
  const seen = new Set<string>();
  const items: FilterItem[] = [];
  table.rows.forEach((row) => {
    const question = row[table.questionColumn];
    if (question !== undefined && question !== '' && !seen.has(question)) {
      seen.add(question);
      items.push({ value: question, label: question });
    }
  });
  return items.sort(compareLabels);
}

export function userItems(table: ParsedCsv, names: Record<string, string>): FilterItem[] {
  if (table.userIdColumn < 0 && table.usernameColumn < 0) return [];
  const seen = new Set<string>();
  const items: FilterItem[] = [];
  table.rows.forEach((row) => {
    const key = userKey(table, row);
    if (key !== undefined && key !== '' && !seen.has(key)) {
      seen.add(key);
      items.push({ value: key, label: userLabel(table, row, names) });
    }
  });
  return items.sort(compareLabels);
}

export function filterRows(table: ParsedCsv, question: string, user: string): string[][] {
  return table.rows.filter((row) => {
    if (question && row[table.questionColumn] !== question) return false;
    if (user && userKey(table, row) !== user) return false;
    return true;
  });
}

// The users CSV that belongs to a plain answers CSV ("" for other files)
export function usersCsvName(fileName: string): string {
  if (!/\.csv$/i.test(fileName) || /\.users\.csv$/i.test(fileName)) return '';
  return fileName.replace(/\.csv$/i, '.users.csv');
}
