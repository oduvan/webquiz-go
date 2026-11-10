import { useCallback, useEffect, useRef, useState } from 'react';
import type { AdminBoot } from '../shared/boot';
import { plain } from '../shared/text';
import { errorText, get, post, put, quizURL, type Json } from './api';
import { useShowMessage } from './messages';
import {
  DownloadableQuizzesSection,
  NetworkInfoSection,
  PendingUsersSection,
  ShowAnswersSection,
  TransferRequestsSection,
  TunnelSection,
  transferChoiceFor,
  transferFileKey,
  type TunnelView,
} from './Panels';
import { QuizEditor, type EditorStart } from './QuizEditor';
import { connectAdminSocket } from './socket';
import { StartQuizModal, type StartModalState } from './StartQuizModal';
import { T } from './texts';
import type { PendingUser, QuizEntry, TransferChoice, TransferFile, TransferRequest, TunnelInfo } from './types';
import { documentationURL, isNewer, latestReleaseURL, releasePageURL, versionFromTag } from './version';
import { draftFromQuiz, newQuizDraft, nextKey, startSettingsFromQuiz, startSettingsPayload } from './wizard';

function without<V>(map: Record<string, V>, key: string): Record<string, V> {
  if (!(key in map)) return map;
  const next = { ...map };
  delete next[key];
  return next;
}

function requestsById(list: unknown): Record<string, TransferRequest> {
  const result: Record<string, TransferRequest> = {};
  if (Array.isArray(list)) {
    list.forEach((request: TransferRequest) => {
      if (request && typeof request.id === 'string') result[request.id] = request;
    });
  }
  return result;
}

// The text of a quiz in the list: "file - title", and " (поточний)" for the running quiz.
function quizLabel(quiz: QuizEntry, current: string | null): string {
  let label = quiz.filename;
  if (quiz.title) label += ' - ' + plain(quiz.title);
  if (quiz.filename === current) label += T.currentMarker;
  return label;
}

export function AdminApp({ boot }: { boot: AdminBoot }) {
  const showMessage = useShowMessage();
  const [loggedIn, setLoggedIn] = useState(false);
  const [masterKey, setMasterKey] = useState('');

  // Quiz list
  const [quizzes, setQuizzes] = useState<QuizEntry[] | null>(null);
  const [currentQuiz, setCurrentQuiz] = useState<string | null>(null);
  const [selected, setSelected] = useState<string[]>([]);
  const [switchBusy, setSwitchBusy] = useState<'restart' | 'switch' | null>(null);
  const [switchText, setSwitchText] = useState<string | null>(null); // the old page's text after a failed start
  const [uniting, setUniting] = useState(false);
  const [showAnswersVisible, setShowAnswersVisible] = useState(false);
  const [answersForced, setAnswersForced] = useState(false);
  const [startModal, setStartModal] = useState<StartModalState | null>(null);
  const [editor, setEditor] = useState<{ id: number; start: EditorStart } | null>(null);
  const [downloading, setDownloading] = useState<Record<number, boolean>>({});

  // From the admin socket
  const [pendingVisible, setPendingVisible] = useState(false);
  const [pendingUsers, setPendingUsers] = useState<Record<string, PendingUser>>({});
  const [transfers, setTransfers] = useState<Record<string, TransferRequest>>({});
  const [transferChoices, setTransferChoices] = useState<Record<string, Record<string, TransferChoice>>>({});
  const [transferErrors, setTransferErrors] = useState<Record<string, string>>({});
  const [transferBusy, setTransferBusy] = useState<Record<string, 'accept' | 'reject'>>({});
  const [tunnel, setTunnel] = useState<(TunnelView & { url: string }) | null>(null);
  const [tunnelBusy, setTunnelBusy] = useState(false);

  // Footer
  const [update, setUpdate] = useState<{ tag: string; version: string } | null>(null);
  const [restartVersion, setRestartVersion] = useState<string | null>(null);

  const loadQuizzes = useCallback(async () => {
    try {
      const reply = await get('/api/admin/list-quizzes');
      if (reply.ok) {
        const list: QuizEntry[] = Array.isArray(reply.data.quizzes) ? reply.data.quizzes : [];
        setCurrentQuiz(typeof reply.data.current_quiz === 'string' ? reply.data.current_quiz : null);
        setQuizzes(list);
        // A new list sets the start button's text again, as the old updateButtonVisibility did
        setSwitchText(null);
        // A selected quiz that is gone is not selected any more
        setSelected((names) => names.filter((name) => list.some((quiz) => quiz.filename === name)));
        if (reply.data.force_all_completed !== undefined) setAnswersForced(Boolean(reply.data.force_all_completed));
        setShowAnswersVisible(reply.data.show_answers_on_completion === true);
      } else {
        showMessage(reply.data.error || T.loadQuizzesFailed, 'error');
      }
    } catch (error) {
      showMessage(T.loadQuizzesError(errorText(error)), 'error');
    }
  }, [showMessage]);

  // The "new version" check asks GitHub; it fails silently
  const checkForUpdates = useCallback(async () => {
    const url = latestReleaseURL(boot.repo_url);
    if (!boot.version || !url) return;
    try {
      const response = await fetch(url, { headers: { Accept: 'application/vnd.github+json' } });
      if (!response.ok) return;
      const data = await response.json();
      const tag = data && typeof data.tag_name === 'string' ? data.tag_name : '';
      if (tag && isNewer(versionFromTag(tag), boot.version)) {
        setUpdate({ tag, version: versionFromTag(tag) });
      }
    } catch (error) {
      // no message: the admin may be offline
    }
  }, [boot.repo_url, boot.version]);

  const showAdminPanel = useCallback(
    (message: string) => {
      setLoggedIn(true);
      showMessage(message, 'success');
      void loadQuizzes();
      void checkForUpdates();
    },
    [showMessage, loadQuizzes, checkForUpdates],
  );

  // A saved session or a trusted address opens the panel without the key
  const started = useRef(false);
  useEffect(() => {
    if (started.current) return;
    started.current = true;
    (async () => {
      try {
        const response = await fetch('/api/admin/check-session', { credentials: 'same-origin' });
        if (response.ok) {
          showAdminPanel(T.sessionRestored);
          return;
        }
      } catch (error) {
        // fall back to the trusted address
      }
      if (boot.is_trusted_ip) showAdminPanel(T.trustedIPLogin);
    })();
  }, [boot.is_trusted_ip, showAdminPanel]);

  // "Restart required": the program file on disk is a newer version (after
  // 2 seconds, then every 5 minutes)
  useEffect(() => {
    const check = async () => {
      try {
        const response = await fetch('/api/admin/version-check', { credentials: 'same-origin' });
        if (!response.ok) return;
        const data = await response.json();
        if (data.restart_required) setRestartVersion(plain(data.file_version));
      } catch (error) {
        // no message
      }
    };
    const first = setTimeout(check, 2000);
    const every = setInterval(check, 300000);
    return () => {
      clearTimeout(first);
      clearInterval(every);
    };
  }, []);

  const applyTunnel = useCallback((info: TunnelInfo) => {
    setTunnelBusy(false);
    setTunnel((previous) => {
      if (info.configured === false) return null;
      return {
        server: info.server ? String(info.server) : previous ? previous.server : '',
        socketName: info.socket_name ? String(info.socket_name) : '',
        showKey: Boolean(info.configured) && info.keys_status === 'ok',
        connected: Boolean(info.connected),
        url: info.connected && info.url ? String(info.url) : '',
        error: info.error ? String(info.error) : '',
        keysOK: info.keys_status === 'ok',
      };
    });
  }, []);

  const handleSocketMessage = (data: Json) => {
    switch (data.type) {
      case 'initial_state':
        if (data.requires_approval) {
          setPendingVisible(true);
          setPendingUsers(data.pending_users || {});
        }
        if (data.tunnel) applyTunnel(data.tunnel);
        setTransfers(requestsById(data.transfer_requests));
        break;
      case 'transfer_request':
        setTransfers((current) => ({ ...current, [data.request.id]: data.request }));
        showMessage(T.newTransferRequest(plain(data.request.from)), 'success');
        break;
      case 'transfer_request_removed':
        setTransfers((current) => without(current, data.id));
        break;
      case 'transfer_result': {
        const status = plain(data.offer.status);
        showMessage(
          T.transferResult(plain(data.offer.to), T.transferResults[status] || status),
          status === 'accepted' ? 'success' : 'error',
        );
        break;
      }
      case 'tunnel_status':
        if (data.tunnel) applyTunnel(data.tunnel);
        break;
      case 'new_registration':
        setPendingUsers((current) => ({
          ...current,
          [data.user_id]: { username: data.username, registration_fields: data.registration_fields || {} },
        }));
        setPendingVisible(true);
        showMessage(T.newUser(plain(data.username)), 'success');
        break;
      case 'registration_updated':
        setPendingUsers((current) => ({
          ...current,
          [data.user_id]: { username: data.username, registration_fields: data.registration_fields || {} },
        }));
        showMessage(T.userUpdated(plain(data.username)), 'success');
        break;
      case 'user_approved':
        setPendingUsers((current) => without(current, data.user_id));
        break;
      case 'answers_forced':
        setAnswersForced(Boolean(data.forced));
        if (data.message) showMessage(data.message, 'success');
        break;
      default:
        break;
    }
  };
  const socketHandler = useRef(handleSocketMessage);
  socketHandler.current = handleSocketMessage;

  useEffect(() => {
    if (!loggedIn) return undefined;
    return connectAdminSocket((data) => socketHandler.current(data));
  }, [loggedIn]);

  // ---- Login ----

  const authenticate = async () => {
    const key = masterKey.trim();
    if (!key) {
      showMessage(T.enterKey, 'error');
      return;
    }
    try {
      const reply = await post('/api/admin/auth', { master_key: key });
      if (reply.ok) {
        showAdminPanel(T.loginSuccess);
      } else {
        showMessage(reply.data.error || T.loginFailed, 'error');
      }
    } catch (error) {
      showMessage(T.connectionError(errorText(error)), 'error');
    }
  };

  // ---- Quiz list actions ----

  const refreshQuizzes = async () => {
    await loadQuizzes();
    showMessage(T.listRefreshed, 'success');
  };

  const openStartQuizModal = async (filename: string) => {
    // The values come from the quiz file
    let parsed: unknown = {};
    try {
      const reply = await get(quizURL(filename));
      if (reply.ok && reply.data.parsed && typeof reply.data.parsed === 'object') parsed = reply.data.parsed;
    } catch (error) {
      console.log('Could not load quiz settings:', error);
    }
    setStartModal({
      filename,
      restart: filename === currentQuiz,
      settings: startSettingsFromQuiz(parsed),
      error: '',
      busy: false,
    });
  };

  const switchQuiz = () => {
    if (selected.length !== 1) {
      showMessage(T.selectOneQuiz, 'warning');
      return;
    }
    void openStartQuizModal(selected[0]);
  };

  const confirmStartQuiz = async () => {
    if (!startModal) return;
    const filename = startModal.filename;
    const restart = filename === currentQuiz;
    setStartModal((modal) => modal && { ...modal, busy: true });
    setSwitchBusy(restart ? 'restart' : 'switch');
    const failed = (message: string) => {
      // The pop-up stays open so the admin can fix the value
      setStartModal((modal) => modal && { ...modal, busy: false, error: message });
      setSwitchBusy(null);
      setSwitchText(restart ? T.restartCurrentQuizEn : T.switchToSelectedQuizEn);
    };
    try {
      const reply = await post('/api/admin/switch-quiz', {
        quiz_filename: filename,
        settings: startSettingsPayload(startModal.settings),
      });
      if (reply.ok) {
        setStartModal(null);
        showMessage(T.switched(restart, filename, reply.data.csv_file), 'success');
        setCurrentQuiz(filename);
        await loadQuizzes();
        setSelected([]);
        setSwitchText(null);
        setSwitchBusy(null);
      } else {
        failed(reply.data.error || T.switchFailed(restart));
      }
    } catch (error) {
      failed(T.switchError(restart, errorText(error)));
    }
  };

  const createNewQuiz = () => {
    setEditor({ id: nextKey(), start: { mode: 'create', editingName: '', filename: '', draft: newQuizDraft() } });
  };

  const editQuiz = async (filename?: string) => {
    if (!filename && selected.length !== 1) {
      showMessage(T.selectOneToEdit, 'warning');
      return;
    }
    const name = filename || selected[0];
    try {
      const reply = await get(quizURL(name));
      if (reply.ok) {
        // A file that is not valid YAML opens with no questions
        const draft = reply.data.parsed ? draftFromQuiz(reply.data.parsed) : { ...newQuizDraft(), questions: [] };
        setEditor({ id: nextKey(), start: { mode: 'edit', editingName: name, filename: name.replace('.yaml', ''), draft } });
      } else {
        showMessage(reply.data.error || T.loadQuizzesFailed, 'error');
      }
    } catch (error) {
      showMessage(T.loadQuizzesError(errorText(error)), 'error');
    }
  };

  const duplicateQuiz = async () => {
    if (selected.length !== 1) {
      showMessage(T.selectOneToDuplicate, 'warning');
      return;
    }
    const original = selected[0];
    const newName = window.prompt(T.duplicatePrompt, original.replace('.yaml', '_copy'));
    if (!newName) return;
    try {
      const got = await get(quizURL(original));
      if (!got.ok) {
        showMessage(got.data.error || T.loadOriginalFailed, 'error');
        return;
      }
      const created = await post('/api/admin/create-quiz', { filename: newName, mode: 'text', content: got.data.content });
      if (created.ok) {
        showMessage(T.duplicated(created.data.filename), 'success');
        await loadQuizzes();
        // The copy opens in the editor
        await editQuiz(created.data.filename);
      } else {
        showMessage(created.data.error || T.duplicateFailed, 'error');
      }
    } catch (error) {
      showMessage(T.duplicateError(errorText(error)), 'error');
    }
  };

  const deleteQuizzes = async () => {
    if (selected.length === 0) {
      showMessage(T.selectToDelete, 'warning');
      return;
    }
    if (currentQuiz !== null && selected.indexOf(currentQuiz) !== -1) {
      showMessage(T.cannotDeleteCurrent, 'error');
      return;
    }
    const question = selected.length === 1 ? T.confirmDeleteOne(selected[0]) : T.confirmDeleteMany(selected.length);
    if (!window.confirm(question + T.cannotBeUndone)) return;

    // One after the other
    const errors: string[] = [];
    let deleted = 0;
    for (const name of selected) {
      try {
        const response = await fetch(quizURL(name), { method: 'DELETE', credentials: 'same-origin' });
        if (response.ok) {
          deleted++;
        } else {
          const data = await response.json();
          errors.push(`${name}: ${data.error || T.genericError}`);
        }
      } catch (error) {
        errors.push(`${name}: ${errorText(error)}`);
      }
    }
    if (errors.length === 0) {
      showMessage(T.deleted(deleted), 'success');
    } else if (deleted > 0) {
      showMessage(T.deletedWithErrors(deleted, errors.join(', ')), 'warning');
    } else {
      showMessage(T.deleteFailed(errors.join(', ')), 'error');
    }
    setSelected([]);
    await loadQuizzes();
  };

  const uniteQuizzes = async () => {
    if (selected.length < 2) {
      showMessage(T.selectTwoToUnite, 'warning');
      return;
    }
    const newName = window.prompt(T.unitePrompt(selected.length), selected[0].replace('.yaml', '_united'));
    if (!newName) return;
    setUniting(true);
    try {
      const reply = await post('/api/admin/unite-quizzes', { quiz_filenames: selected, new_name: newName });
      if (reply.ok) {
        let message = T.united(reply.data.filename, reply.data.total_questions);
        if (reply.data.warning) message += `. ${reply.data.warning}`;
        showMessage(message, 'success');
        setSelected([]);
        await loadQuizzes();
        // The united quiz opens in the editor
        await editQuiz(reply.data.filename);
      } else {
        showMessage(reply.data.error || T.uniteFailed, 'error');
      }
    } catch (error) {
      showMessage(T.uniteError(errorText(error)), 'error');
    } finally {
      setUniting(false);
    }
  };

  // ---- Panels ----

  const approveUser = async (userId: string) => {
    try {
      const reply = await put('/api/admin/approve-user', { user_id: userId });
      if (reply.ok) {
        showMessage(T.approved, 'success');
        setPendingUsers((current) => without(current, userId));
      } else {
        showMessage(reply.data.error || T.approveFailed, 'error');
      }
    } catch (error) {
      showMessage(T.connectionError(errorText(error)), 'error');
    }
  };

  const forceShowAnswers = async () => {
    if (!window.confirm(T.confirmShowAnswers)) return;
    try {
      const reply = await post('/api/admin/force-show-answers');
      if (reply.ok) {
        showMessage(T.answersShown(reply.data.completed_count, reply.data.total_count), 'success');
        setAnswersForced(true);
      } else {
        showMessage(reply.data.error || T.showAnswersFailed, 'error');
      }
    } catch (error) {
      showMessage(T.connectionError(errorText(error)), 'error');
    }
  };

  const downloadQuiz = async (index: number) => {
    const quiz = boot.downloadable_quizzes[index];
    if (!quiz) return;
    if (!window.confirm(T.confirmDownload(plain(quiz.name), plain(quiz.download_path)))) return;
    setDownloading((current) => ({ ...current, [index]: true }));
    try {
      const reply = await post('/api/admin/download-quiz', {
        name: quiz.name,
        download_path: quiz.download_path,
        folder: quiz.folder,
      });
      if (reply.ok && reply.data.success) {
        showMessage(T.downloaded(reply.data.message), 'success');
        await loadQuizzes();
      } else {
        showMessage(T.downloadFailed(reply.data.error), 'error');
      }
    } catch (error) {
      console.error('Download quiz error:', error);
      showMessage(T.downloadNetworkError(errorText(error)), 'error');
    } finally {
      setDownloading((current) => ({ ...current, [index]: false }));
    }
  };

  const toggleTunnel = async () => {
    const connected = Boolean(tunnel && tunnel.connected);
    setTunnelBusy(true);
    try {
      const reply = await post(connected ? '/api/admin/tunnel/disconnect' : '/api/admin/tunnel/connect');
      if (!reply.ok) throw new Error(reply.data.error || T.operationFailed);
      // The button comes back with the next tunnel status message
      showMessage(connected ? T.tunnelDisconnected : T.tunnelConnected(reply.data.url), 'success');
    } catch (error) {
      showMessage(T.tunnelFailed(errorText(error)), 'error');
      setTunnelBusy(false);
    }
  };

  const setTransferChoice = (requestId: string, file: TransferFile, choice: TransferChoice) => {
    setTransferChoices((current) => ({
      ...current,
      [requestId]: { ...(current[requestId] || {}), [transferFileKey(file)]: choice },
    }));
  };

  const reloadTransfers = async () => {
    try {
      const reply = await get('/api/admin/transfer');
      if (reply.ok) setTransfers(requestsById(reply.data.incoming));
    } catch (error) {
      // keep the current list
    }
  };

  const answerTransfer = async (requestId: string, action: 'accept' | 'reject') => {
    const request = transfers[requestId];
    if (!request) return;
    setTransferBusy((current) => ({ ...current, [requestId]: action }));
    const files = (Array.isArray(request.files) ? request.files : []).map((file) => {
      const choice = transferChoiceFor(transferChoices[requestId], file);
      const answer: Record<string, string> = { type: file.type, name: file.name, action: choice.action };
      if (choice.action === 'rename') answer.new_name = choice.new_name.trim();
      return answer;
    });

    let keepRequest = false;
    try {
      const reply = await post(
        `/api/admin/transfer/${encodeURIComponent(requestId)}/${action}`,
        action === 'accept' ? { files } : '{}',
      );
      if (!reply.ok) {
        const message = reply.data.error || T.transferFailed;
        showMessage(message, 'error');
        // Invalid choices: nothing was downloaded and the request is still open
        keepRequest = reply.status === 400;
        setTransferErrors((current) => ({ ...current, [requestId]: message }));
      } else if (action === 'reject') {
        showMessage(T.transferRejected, 'success');
      } else {
        const saved: Json[] = Array.isArray(reply.data.saved) ? reply.data.saved : [];
        const skipped: Json[] = Array.isArray(reply.data.skipped) ? reply.data.skipped : [];
        const failed: Json[] = Array.isArray(reply.data.failed) ? reply.data.failed : [];
        if (saved.length > 0) {
          showMessage(T.filesReceived(saved.length, saved.map((file) => plain(file.saved_as)).join(', ')), 'success');
        }
        if (skipped.length > 0) showMessage(T.filesSkipped(skipped.length), 'success');
        failed.forEach((file) => showMessage(T.fileFailed(plain(file.name), plain(file.error)), 'error'));
      }
    } catch (error) {
      const message = T.connectionError(errorText(error));
      showMessage(message, 'error');
      keepRequest = true;
      setTransferErrors((current) => ({ ...current, [requestId]: message }));
    }

    if (keepRequest) {
      await reloadTransfers();
    } else {
      setTransfers((current) => without(current, requestId));
      setTransferChoices((current) => without(current, requestId));
      setTransferErrors((current) => without(current, requestId));
    }
    setTransferBusy((current) => without(current, requestId));
  };

  // ---- Drawing ----

  const count = selected.length;
  const single = count === 1 ? selected[0] : null;
  let switchLabel = single !== null && single === currentQuiz ? T.restartQuiz : T.switchQuiz;
  if (switchText !== null) switchLabel = switchText;
  if (switchBusy) switchLabel = switchBusy === 'restart' ? T.restarting : T.switching;

  const currentEntry = quizzes && currentQuiz ? quizzes.find((quiz) => quiz.filename === currentQuiz) : undefined;
  const hasCurrent = Boolean(quizzes && quizzes.length > 0 && currentQuiz);

  return (
    <div className="admin-page">
      <div className="container">
        <h1>{T.pageTitle}</h1>

        <form
          id="auth-section"
          className={loggedIn ? 'hidden' : undefined}
          onSubmit={(event) => {
            event.preventDefault();
            void authenticate();
          }}
        >
          <div className="form-group">
            <label htmlFor="master-key">{T.keyLabel}</label>
            <input
              type="password"
              id="master-key"
              placeholder={T.keyPlaceholder}
              autoComplete="current-password"
              value={masterKey}
              onChange={(event) => setMasterKey(event.target.value)}
            />
          </div>
          <div className="actions">
            <button type="submit" id="login-btn">
              {T.login}
            </button>
          </div>
        </form>

        {loggedIn ? (
          <div id="admin-panel">
            <div className="form-group">
              <label htmlFor="quiz-select">{T.quizFilesLabel}</label>
              <select
                id="quiz-select"
                className="quiz-select"
                multiple
                size={8}
                value={selected}
                onChange={(event) => {
                  setSelected(Array.from(event.target.selectedOptions).map((option) => option.value));
                  setSwitchText(null);
                }}
              >
                {quizzes !== null && quizzes.length === 0 ? <option disabled>{T.noQuizFiles}</option> : null}
                {(quizzes || []).map((quiz) => (
                  <option
                    key={quiz.filename}
                    value={quiz.filename}
                    className={quiz.filename === currentQuiz ? 'current-quiz' : undefined}
                  >
                    {quizLabel(quiz, currentQuiz)}
                  </option>
                ))}
              </select>
              <div className="selection-hint">{T.selectionHint}</div>
              <div id="current-quiz-display" className={'current-quiz-display' + (hasCurrent ? '' : ' no-quiz')}>
                {hasCurrent && currentQuiz
                  ? T.currentQuiz(currentQuiz, currentEntry && currentEntry.title ? plain(currentEntry.title) : '')
                  : T.noCurrentQuiz}
              </div>
            </div>

            <div className={'actions' + (count >= 2 ? ' hidden' : '')} id="single-quiz-actions">
              <button type="button" id="switch-btn" disabled={count !== 1 || switchBusy !== null} onClick={switchQuiz}>
                {switchLabel}
              </button>
              <button type="button" className="refresh-btn" onClick={() => void refreshQuizzes()}>
                {T.refresh}
              </button>
              <button type="button" id="edit-btn" disabled={count !== 1} onClick={() => void editQuiz()}>
                {T.edit}
              </button>
              <button type="button" id="duplicate-btn" disabled={count !== 1} onClick={() => void duplicateQuiz()}>
                {T.duplicate}
              </button>
            </div>

            <div className={'actions' + (count >= 2 ? '' : ' hidden')} id="multi-quiz-actions">
              <button type="button" id="unite-btn" className="btn-success" disabled={uniting} onClick={() => void uniteQuizzes()}>
                {uniting ? T.uniting : T.unite}
              </button>
              <button type="button" className="refresh-btn" onClick={() => void refreshQuizzes()}>
                {T.refresh}
              </button>
            </div>

            <div className="quiz-actions">
              <button type="button" id="create-quiz-btn" onClick={createNewQuiz}>
                {T.createQuiz}
              </button>
              <button
                type="button"
                id="delete-btn"
                className="btn-danger"
                disabled={count === 0 || (currentQuiz !== null && selected.indexOf(currentQuiz) !== -1)}
                onClick={() => void deleteQuizzes()}
              >
                {T.deleteQuiz}
              </button>
            </div>

            <PendingUsersSection visible={pendingVisible} users={pendingUsers} onApprove={(id) => void approveUser(id)} />

            <TransferRequestsSection
              requests={transfers}
              choices={transferChoices}
              errors={transferErrors}
              busy={transferBusy}
              onChoice={setTransferChoice}
              onAnswer={(id, action) => void answerTransfer(id, action)}
            />

            <ShowAnswersSection
              visible={showAnswersVisible}
              forced={answersForced}
              onForce={() => void forceShowAnswers()}
            />

            <DownloadableQuizzesSection
              quizzes={boot.downloadable_quizzes}
              downloading={downloading}
              onDownload={(index) => void downloadQuiz(index)}
            />

            <NetworkInfoSection urls={boot.network_info.urls} tunnelURL={tunnel && tunnel.connected ? tunnel.url : null} />

            {tunnel ? (
              <TunnelSection
                view={tunnel}
                publicKey={boot.tunnel_public_key}
                busy={tunnelBusy}
                onToggle={() => void toggleTunnel()}
                showMessage={showMessage}
              />
            ) : null}

            <div className="back-link">
              <a href="/">{T.backToQuiz}</a>
              <span className="separator">|</span>
              <a href="/files/">{T.fileManager}</a>
              <span className="separator">|</span>
              <a href="/live-stats/">{T.liveStats}</a>
            </div>

            <div className="admin-footer">
              <div className="footer-version" id="footer-version">
                {T.versionPrefix}
                <span id="version-number">{boot.version || 'unknown'}</span>
                <span id="footer-docs-link" className="footer-docs-link">
                  {boot.version ? (
                    <>
                      {'| '}
                      <a href={documentationURL(boot.repo_url, boot.version)} target="_blank" rel="noopener">
                        {T.documentation}
                      </a>
                    </>
                  ) : null}
                </span>
              </div>
              <div id="footer-update" className={'footer-update' + (update && !restartVersion ? '' : ' hidden')}>
                {update ? (
                  <>
                    {T.updateAvailable}
                    <a href={releasePageURL(boot.repo_url, update.tag)} target="_blank" rel="noopener">
                      {'v' + update.version}
                    </a>
                  </>
                ) : null}
              </div>
              <div id="footer-restart" className={'footer-restart' + (restartVersion ? '' : ' hidden')}>
                {T.restartRequiredStart}
                <span id="new-version-number">{restartVersion || ''}</span>
                {T.restartRequiredEnd}
              </div>
            </div>
          </div>
        ) : null}
      </div>

      {startModal ? (
        <StartQuizModal
          state={startModal}
          onChange={(settings) => setStartModal((modal) => modal && { ...modal, settings })}
          onConfirm={() => void confirmStartQuiz()}
          onClose={() => setStartModal(null)}
        />
      ) : null}

      {editor ? (
        <QuizEditor
          key={editor.id}
          start={editor.start}
          onClose={() => setEditor(null)}
          onListChanged={() => void loadQuizzes()}
        />
      ) : null}
    </div>
  );
}
