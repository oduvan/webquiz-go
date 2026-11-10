import { useCallback, useEffect, useRef, useState } from 'react';
import { readBoot, type FilesBoot } from '../shared/boot';
import { ConfigEditor } from './ConfigEditor';
import { FileList } from './FileList';
import { FileViewerModal, type ViewTarget } from './FileViewerModal';
import { QuizEditorModal } from './QuizEditorModal';
import { TransferPanel } from './TransferPanel';
import { getWithSession, readJson, sendJson, type FileLists, type FileType } from './api';
import { filesText as T } from './texts';
import { inputClass, Message, primaryButton, type MessageType } from './ui';

const boot = readBoot<FilesBoot>({ is_trusted_ip: false, config_content: null, config_path: null, config_data: null });

type Tab = FileType | 'config';
const MESSAGE_MS = 5000;

interface Notice {
  id: number;
  text: string;
  type: MessageType;
}

export function FilesPage() {
  const [authed, setAuthed] = useState(false);
  const [masterKey, setMasterKey] = useState('');
  const [tab, setTab] = useState<Tab>('csv');
  const [search, setSearch] = useState('');
  const [files, setFiles] = useState<FileLists | null>(null);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [notices, setNotices] = useState<Notice[]>([]);
  const [viewing, setViewing] = useState<ViewTarget | null>(null);
  const [editing, setEditing] = useState<string | null>(null);
  const [transferEnabled, setTransferEnabled] = useState(false);
  const noticeId = useRef(0);
  const timers = useRef<number[]>([]);

  useEffect(() => () => timers.current.forEach((timer) => window.clearTimeout(timer)), []);

  const showMessage = useCallback((text: string, type: MessageType) => {
    const id = ++noticeId.current;
    setNotices((current) => [...current, { id, text, type }]);
    const timer = window.setTimeout(() => setNotices((current) => current.filter((item) => item.id !== id)), MESSAGE_MS);
    timers.current.push(timer);
  }, []);

  const loadFiles = useCallback(async () => {
    try {
      const response = await getWithSession('/api/files/list');
      const data = await readJson<FileLists & { error?: string }>(response);
      if (response.ok) {
        setFiles(data);
        // Forget selected files that do not exist anymore
        setSelected((current) => {
          const next = new Set(current);
          current.forEach((key) => {
            const slash = key.indexOf('/');
            const list = data[key.slice(0, slash) as FileType] || [];
            if (!list.some((file) => file.name === key.slice(slash + 1))) next.delete(key);
          });
          return next.size === current.size ? current : next;
        });
      } else {
        showMessage(data.error || T.failedToLoad, 'error');
      }
    } catch (error) {
      showMessage(T.errorLoading + (error as Error).message, 'error');
    }
  }, [showMessage]);

  const enter = useCallback(
    (message: string) => {
      setAuthed(true);
      showMessage(message, 'success');
    },
    [showMessage],
  );

  // Restore the session cookie, or fall back to the trusted IP
  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const response = await getWithSession('/api/admin/check-session');
        if (response.ok) {
          if (!cancelled) enter(T.sessionRestored);
          return;
        }
      } catch (e) {
        // Fall through to the trusted IP
      }
      if (!cancelled && boot.is_trusted_ip) enter(T.trustedIp);
    })();
    return () => {
      cancelled = true;
    };
  }, [enter]);

  useEffect(() => {
    if (authed) void loadFiles();
  }, [authed, loadFiles]);

  const login = async () => {
    const key = masterKey.trim();
    if (!key) {
      showMessage(T.enterMasterKey, 'error');
      return;
    }
    try {
      const response = await sendJson('/api/admin/auth', 'POST', { master_key: key });
      const data = await readJson<{ error?: string }>(response);
      if (response.ok) {
        enter(T.authOk);
      } else {
        showMessage(data.error || T.authFailed, 'error');
      }
    } catch (error) {
      showMessage(T.connectionError + (error as Error).message, 'error');
    }
  };

  const download = async (type: FileType, name: string) => {
    try {
      const response = await getWithSession(`/api/files/${type}/download/${encodeURIComponent(name)}`);
      if (response.ok) {
        const blob = await response.blob();
        const url = window.URL.createObjectURL(blob);
        const link = document.createElement('a');
        link.href = url;
        link.download = name;
        document.body.appendChild(link);
        link.click();
        document.body.removeChild(link);
        window.URL.revokeObjectURL(url);
        showMessage(T.downloaded(name), 'success');
      } else {
        const data = await readJson<{ error?: string }>(response);
        showMessage(T.downloadFailed(data.error ?? ''), 'error');
      }
    } catch (error) {
      showMessage(T.downloadError((error as Error).message), 'error');
    }
  };

  const toggleTransfer = (type: FileType, name: string, checked: boolean) => {
    setSelected((current) => {
      const next = new Set(current);
      if (checked) next.add(`${type}/${name}`);
      else next.delete(`${type}/${name}`);
      return next;
    });
  };

  const refresh = async () => {
    await loadFiles();
    showMessage(T.refreshed, 'success');
  };

  const showConfig = boot.config_path !== null;
  const tabs: { id: Tab; label: string }[] = [
    { id: 'csv', label: T.tabCsv },
    { id: 'logs', label: T.tabLogs },
    { id: 'quizzes', label: T.tabQuizzes },
    ...(showConfig ? [{ id: 'config' as Tab, label: T.tabConfig }] : []),
  ];
  const linkClass = 'text-blue-600 hover:underline';

  return (
    <div className="container mx-auto box-border w-full max-w-5xl p-4 max-md:p-3">
      {/* mr-14: the title keeps clear of the theme button at the top right */}
      <h1 className="mb-4 mr-14 mt-8 text-2xl font-bold max-md:text-xl">{T.title}</h1>

      {!authed && (
        <div id="auth-section" className="max-w-md">
          <div className="mb-3">
            <label htmlFor="master-key" className="mb-1 block font-bold">
              {T.masterKey}
            </label>
            <input
              type="password"
              id="master-key"
              value={masterKey}
              placeholder={T.masterKeyPlaceholder}
              onChange={(event) => setMasterKey(event.target.value)}
              onKeyDown={(event) => event.key === 'Enter' && void login()}
              className={inputClass + ' box-border w-full'}
            />
          </div>
          <button type="button" className={primaryButton} onClick={() => void login()}>
            {T.login}
          </button>
        </div>
      )}

      {authed && (
        <div id="files-panel">
          <div className="controls mb-3 flex flex-wrap gap-2">
            <input
              type="text"
              id="search-input"
              value={search}
              placeholder={T.searchPlaceholder}
              onChange={(event) => setSearch(event.target.value)}
              className={inputClass + ' min-w-0 flex-1'}
            />
            <button type="button" className={primaryButton + ' refresh-btn'} onClick={() => void refresh()}>
              {T.refresh}
            </button>
          </div>

          <TransferPanel selected={selected} onSent={() => setSelected(new Set())} onAvailable={setTransferEnabled} showMessage={showMessage} />

          <div className="tabs mb-3 flex flex-wrap gap-1 border-b border-[var(--border)]">
            {tabs.map((item) => (
              <button
                key={item.id}
                type="button"
                data-tab={item.id}
                id={item.id === 'config' ? 'config-tab-button' : undefined}
                className={
                  'tab cursor-pointer rounded-t border border-b-0 border-[var(--border)] px-4 py-2 ' +
                  (tab === item.id ? 'active bg-blue-600 text-white' : 'bg-[var(--card)] text-[var(--fg)]')
                }
                onClick={() => {
                  setTab(item.id);
                  setSearch('');
                }}
              >
                {item.label}
              </button>
            ))}
          </div>

          {showConfig && (
            // Stays mounted (hidden) so edits and the saved values survive a tab change
            <div hidden={tab !== 'config'}>
              <ConfigEditor path={boot.config_path ?? ''} content={boot.config_content} data={boot.config_data} />
            </div>
          )}
          {tab !== 'config' && (
            <div id={`${tab}-tab`} className="tab-content active">
              <FileList
                type={tab}
                files={files ? files[tab] : null}
                search={search}
                selected={selected}
                transferEnabled={transferEnabled}
                onToggleTransfer={toggleTransfer}
                onView={(type, name, mode) => setViewing({ type, name, mode })}
                onEdit={setEditing}
                onDownload={(type, name) => void download(type, name)}
              />
            </div>
          )}

          <div className="back-link mt-6 text-center">
            <a href="/admin/" className={linkClass}>
              {T.backToAdmin}
            </a>
            <span className="mx-2.5">|</span>
            <a href="/" className={linkClass}>
              {T.quizHome}
            </a>
            <span className="mx-2.5">|</span>
            <a href="/live-stats/" className={linkClass}>
              {T.liveStats}
            </a>
          </div>
        </div>
      )}

      <div id="message-area" className="fixed bottom-3 left-3 right-3 z-50 mx-auto max-w-md">
        {notices.map((item) => (
          <Message key={item.id} type={item.type}>
            {item.text}
          </Message>
        ))}
      </div>

      {viewing && <FileViewerModal target={viewing} onClose={() => setViewing(null)} />}
      {editing !== null && (
        <QuizEditorModal filename={editing} onClose={() => setEditing(null)} onSaved={loadFiles} showMessage={showMessage} />
      )}
    </div>
  );
}
