import { useCallback, useEffect, useMemo, useState } from 'react';
import {
  filterRows,
  parseTable,
  questionItems,
  userItems,
  usernamesFromUsersCsv,
  usersCsvName,
  type FilterItem,
  type ParsedCsv,
} from './csv';
import { csvText as T } from './texts';

// The file name is the last part of the address: /files/csv/{filename}
const fileName = decodeURIComponent(window.location.pathname.split('/').pop() ?? '');
const pageParams = new URLSearchParams(window.location.search);
const embedded = pageParams.get('embedded') === '1';

function fetchCsv(name: string): Promise<Response> {
  return fetch(`/api/files/csv/view/${encodeURIComponent(name)}`, { credentials: 'same-origin' });
}

// Page address with the current filters, e.g. /files/csv/quiz_0001.csv?question=...&user=123456
function addressWithFilters(question: string, user: string, includeEmbedded: boolean): string {
  const params = new URLSearchParams();
  if (includeEmbedded && embedded) params.set('embedded', '1');
  if (question) params.set('question', question);
  if (user) params.set('user', user);
  const query = params.toString();
  return `/files/csv/${encodeURIComponent(fileName)}${query ? '?' + query : ''}`;
}

type Problem = { kind: 'login' } | { kind: 'error'; text: string } | null;

function FilterSelect(props: {
  id: string;
  groupId: string;
  label: string;
  allLabel: string;
  items: FilterItem[];
  value: string;
  onChange: (value: string) => void;
}) {
  const { id, groupId, label, allLabel, items, value, onChange } = props;
  const selected = items.some((item) => item.value === value) ? value : '';
  return (
    <div id={groupId} className="flex min-w-0 flex-[1_1_260px] flex-col gap-1">
      <label htmlFor={id} className="text-[13px] font-bold opacity-70">
        {label}
      </label>
      <select
        id={id}
        value={selected}
        onChange={(event) => onChange(event.target.value)}
        className={
          'w-full max-w-full rounded border bg-[var(--card)] px-2 py-1.5 text-sm text-[var(--fg)] ' +
          (selected ? 'border-blue-600 ring-1 ring-blue-600' : 'border-[var(--border)]')
        }
      >
        <option value="">{`${allLabel} (${items.length})`}</option>
        {items.map((item) => (
          // Line breaks in a question would be lost in the list anyway; show them as spaces
          <option key={item.value} value={item.value} title={item.label}>
            {item.label.replace(/\s+/g, ' ')}
          </option>
        ))}
      </select>
    </div>
  );
}

export function CsvTablePage() {
  const [table, setTable] = useState<ParsedCsv | null>(null);
  const [names, setNames] = useState<Record<string, string>>({});
  const [loading, setLoading] = useState(true);
  const [problem, setProblem] = useState<Problem>(null);
  const [question, setQuestion] = useState(pageParams.get('question') ?? '');
  const [user, setUser] = useState(pageParams.get('user') ?? '');

  useEffect(() => {
    document.title = `${fileName} - WebQuiz`;
    if (embedded) document.body.classList.add('embedded');
  }, []);

  const load = useCallback(async () => {
    setLoading(true);
    let response: Response;
    try {
      response = await fetchCsv(fileName);
    } catch (error) {
      setTable(null);
      setLoading(false);
      setProblem({ kind: 'error', text: T.couldNotLoad + (error as Error).message });
      return;
    }
    if (!response.ok) {
      setTable(null);
      setLoading(false);
      if (response.status === 401) {
        setProblem({ kind: 'login' });
      } else {
        let error = `HTTP ${response.status}`;
        try {
          error = (await response.json()).error || error;
        } catch (e) {
          // Keep the HTTP status as the error text
        }
        setProblem({ kind: 'error', text: T.couldNotLoad + error });
      }
      return;
    }
    const parsed = parseTable(await response.text());

    // A plain answers CSV has only user IDs. Its users CSV has the names.
    let found: Record<string, string> = {};
    const usersName = usersCsvName(fileName);
    if (parsed.usernameColumn < 0 && parsed.userIdColumn >= 0 && usersName) {
      try {
        const usersResponse = await fetchCsv(usersName);
        if (usersResponse.ok) found = usernamesFromUsersCsv(await usersResponse.text());
      } catch (e) {
        // Names are only a convenience; the IDs still work
      }
    }
    setProblem(null);
    setNames(found);
    setTable(parsed);
    setLoading(false);
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const questions = useMemo(() => (table ? questionItems(table) : []), [table]);
  const users = useMemo(() => (table ? userItems(table, names) : []), [table, names]);
  const showQuestion = table !== null && table.questionColumn >= 0;
  const showUser = table !== null && (table.userIdColumn >= 0 || table.usernameColumn >= 0);

  // A filter value that is not in the file is dropped, like the old page did
  const activeQuestion = showQuestion && questions.some((item) => item.value === question) ? question : '';
  const activeUser = showUser && users.some((item) => item.value === user) ? user : '';

  const visibleRows = useMemo(
    () => (table ? filterRows(table, activeQuestion, activeUser) : []),
    [table, activeQuestion, activeUser],
  );

  // Keep the filters in the address, so reloading the page keeps them
  useEffect(() => {
    if (!table) return;
    history.replaceState(null, '', addressWithFilters(activeQuestion, activeUser, true));
  }, [table, activeQuestion, activeUser]);

  const openInNewWindow = () => {
    window.open(addressWithFilters(activeQuestion, activeUser, false), '_blank');
  };

  const buttonClass = 'cursor-pointer whitespace-nowrap rounded px-3.5 py-2 text-sm text-white ';

  return (
    <div className="box-border flex h-screen flex-col gap-3 p-4 max-md:p-2.5">
      {!embedded && (
        <div id="page-header" className="flex flex-wrap items-center gap-3 pr-14">
          <a href="/files/" className="text-blue-600 hover:underline">
            {T.fileManager}
          </a>
          <h1 id="csv-title" className="m-0 min-w-0 flex-1 break-words text-[22px] max-md:order-3 max-md:basis-full max-md:text-lg">
            {fileName}
          </h1>
        </div>
      )}
      <div id="toolbar" className="flex flex-wrap items-end gap-x-4 gap-y-2.5">
        {showQuestion && (
          <FilterSelect
            id="question-filter"
            groupId="question-filter-group"
            label={T.question}
            allLabel={T.allQuestions}
            items={questions}
            value={question}
            onChange={setQuestion}
          />
        )}
        {showUser && (
          <FilterSelect
            id="user-filter"
            groupId="user-filter-group"
            label={T.user}
            allLabel={T.allUsers}
            items={users}
            value={user}
            onChange={setUser}
          />
        )}
        {/* The old page kept the hidden groups in the DOM; tests only ask whether they show */}
        {!showQuestion && <div id="question-filter-group" hidden />}
        {!showUser && <div id="user-filter-group" hidden />}
        <div className="flex w-full flex-wrap items-center gap-2 md:w-auto">
          <span id="row-count" className="whitespace-nowrap text-sm opacity-70">
            {table ? T.showing(visibleRows.length, table.rows.length) : ''}
          </span>
          <button id="reload-button" type="button" title={T.reloadTitle} className={buttonClass + 'bg-gray-500 hover:bg-gray-600'} onClick={() => void load()}>
            {T.reload}
          </button>
          {embedded && (
            <button id="open-new-window" type="button" title={T.openNewWindowTitle} className={buttonClass + 'bg-blue-600 hover:bg-blue-700'} onClick={openInNewWindow}>
              {T.openNewWindow}
            </button>
          )}
        </div>
      </div>
      {problem && (
        <div id="csv-message" className="message error rounded border border-red-300 bg-red-100 p-3 text-red-900 dark:border-red-800 dark:bg-red-950 dark:text-red-200">
          {problem.kind === 'login' ? (
            <>
              {T.loginBefore}
              <a href="/files/" className="font-bold underline">
                {T.loginLink}
              </a>
              {T.loginAfter}
            </>
          ) : (
            problem.text
          )}
        </div>
      )}
      {!problem && <div id="csv-message" hidden />}
      {/* The table box scrolls by itself, so the header row and the sideways scrollbar stay on screen */}
      <div id="table-container" className="min-h-0 flex-1 overflow-auto border border-[var(--border)] bg-[var(--card)]">
        {loading && !table ? (
          <div className="px-5 py-10 text-center opacity-70">{T.loading}</div>
        ) : table && table.header.length === 0 ? (
          <div className="px-5 py-10 text-center opacity-70">📊<br />{T.noData}</div>
        ) : table ? (
          <>
            <table className="csv-table w-full border-collapse">
              <thead>
                <tr>
                  {table.header.map((cell, index) => (
                    <th key={index} className="sticky top-0 border border-[var(--border)] bg-[var(--bg)] px-2.5 py-2 text-left max-md:px-2 max-md:py-1.5 max-md:text-xs">
                      {cell}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {visibleRows.map((row, rowIndex) => (
                  <tr key={rowIndex} className="even:bg-black/5 hover:bg-sky-100 dark:even:bg-white/5 dark:hover:bg-white/10">
                    {Array.from({ length: Math.max(table.header.length, row.length) }, (_, i) => (
                      <td key={i} className="whitespace-pre-line border border-[var(--border)] px-2.5 py-2 align-top max-md:px-2 max-md:py-1.5 max-md:text-xs">
                        {row[i] === undefined ? '' : row[i]}
                      </td>
                    ))}
                  </tr>
                ))}
              </tbody>
            </table>
            {visibleRows.length === 0 && <div className="px-5 py-10 text-center opacity-70">{T.noRowsMatch}</div>}
          </>
        ) : null}
      </div>
    </div>
  );
}
