import { useRef, useState } from 'react';
import type { DownloadableQuiz, NetworkURL } from '../shared/boot';
import { plain } from '../shared/text';
import type { ShowMessage } from './messages';
import { T } from './texts';
import type { PendingUser, TransferChoice, TransferFile, TransferRequest } from './types';
import { fieldNameToLabel, formatFileSize } from './wizard';

// ---- Students waiting for approval ----

export function PendingUsersSection({
  visible,
  users,
  onApprove,
}: {
  visible: boolean;
  users: Record<string, PendingUser>;
  onApprove: (userId: string) => void;
}) {
  const ids = Object.keys(users);
  return (
    <div id="pending-approvals-section" className={'pending-approvals' + (visible ? '' : ' hidden')}>
      <h3>{T.pendingTitle}</h3>
      <div id="pending-users-list">
        {ids.length === 0 ? (
          <p className="empty-note">{T.noPendingUsers}</p>
        ) : (
          ids.map((userId) => {
            const user = users[userId];
            const fields = user.registration_fields && typeof user.registration_fields === 'object' ? user.registration_fields : {};
            return (
              <div className="pending-user-item" id={`pending-user-${userId}`} key={userId}>
                <div className="pending-user-info">
                  <div className="pending-user-name">{'👤 ' + plain(user.username)}</div>
                  {Object.keys(fields).map((name) => (
                    <div className="pending-user-data" key={name}>
                      {fieldNameToLabel(name) + ': ' + plain(fields[name])}
                    </div>
                  ))}
                </div>
                <button type="button" className="approve-btn" onClick={() => onApprove(userId)}>
                  {T.approve}
                </button>
              </div>
            );
          })
        )}
      </div>
    </div>
  );
}

// ---- Incoming file transfer requests ----

// The key of a file in the admin's choices: "type/name".
export function transferFileKey(file: TransferFile): string {
  return file.type + '/' + file.name;
}

// The admin's choice for a file, or the server's suggestion. An action the
// file does not offer falls back to the first one offered, the option a
// browser shows in that case.
export function transferChoiceFor(
  choices: Record<string, TransferChoice> | undefined,
  file: TransferFile,
): TransferChoice {
  const saved = choices ? choices[transferFileKey(file)] : undefined;
  const choice = saved ? { ...saved } : { action: file.default_action, new_name: file.suggested_name || '' };
  const actions = Array.isArray(file.actions) ? file.actions : [];
  if (actions.indexOf(choice.action) === -1 && actions.length > 0) choice.action = actions[0];
  return choice;
}

function TransferRequestItem({
  request,
  choices,
  error,
  busy,
  onChoice,
  onAnswer,
}: {
  request: TransferRequest;
  choices: Record<string, TransferChoice> | undefined;
  error: string | undefined;
  busy: 'accept' | 'reject' | undefined;
  onChoice: (requestId: string, file: TransferFile, choice: TransferChoice) => void;
  onAnswer: (requestId: string, action: 'accept' | 'reject') => void;
}) {
  const files = Array.isArray(request.files) ? request.files : [];
  return (
    <div className="transfer-request-item" data-request-id={request.id}>
      <div className="pending-user-name">{T.transferFrom(plain(request.from))}</div>
      {files.map((file) => {
        const choice = transferChoiceFor(choices, file);
        const describe = T.transferCases[file.case] || ((target: string) => target);
        return (
          <div className="transfer-file-row" data-type={file.type} data-name={file.name} key={transferFileKey(file)}>
            <div>
              <div className="transfer-file-name">{plain(file.name)}</div>
              <div className="transfer-file-meta">
                {(T.transferTypes[file.type] || plain(file.type)) + ', ' + formatFileSize(Number(file.size) || 0)}
              </div>
            </div>
            <div className={'transfer-case transfer-case-' + plain(file.case)}>{describe(plain(file.target))}</div>
            <div className="transfer-choice">
              <select
                aria-label={T.transferActionLabel}
                value={choice.action}
                disabled={busy !== undefined}
                onChange={(event) => onChoice(request.id, file, { action: event.target.value, new_name: choice.new_name })}
              >
                {(Array.isArray(file.actions) ? file.actions : []).map((action) => (
                  <option key={action} value={action}>
                    {T.transferActions[action] || action}
                  </option>
                ))}
              </select>
              <input
                type="text"
                className={'transfer-new-name' + (choice.action === 'rename' ? '' : ' hidden')}
                aria-label={T.transferNewNameLabel}
                value={choice.new_name}
                disabled={busy !== undefined}
                onChange={(event) => onChoice(request.id, file, { action: choice.action, new_name: event.target.value })}
              />
            </div>
          </div>
        );
      })}
      {error ? (
        <div className="transfer-error" role="alert">
          {error}
        </div>
      ) : null}
      <div className="transfer-actions">
        <button type="button" className="approve-btn" disabled={busy !== undefined} onClick={() => onAnswer(request.id, 'accept')}>
          {busy === 'accept' ? T.downloading : T.accept}
        </button>
        <button type="button" className="reject-btn" disabled={busy !== undefined} onClick={() => onAnswer(request.id, 'reject')}>
          {T.reject}
        </button>
      </div>
    </div>
  );
}

export function TransferRequestsSection({
  requests,
  choices,
  errors,
  busy,
  onChoice,
  onAnswer,
}: {
  requests: Record<string, TransferRequest>;
  choices: Record<string, Record<string, TransferChoice>>;
  errors: Record<string, string>;
  busy: Record<string, 'accept' | 'reject'>;
  onChoice: (requestId: string, file: TransferFile, choice: TransferChoice) => void;
  onAnswer: (requestId: string, action: 'accept' | 'reject') => void;
}) {
  const list = Object.keys(requests).map((id) => requests[id]);
  return (
    <div id="transfer-requests-section" className={'transfer-requests' + (list.length === 0 ? ' hidden' : '')}>
      <h3>{T.transferTitle}</h3>
      <p className="section-hint">{T.transferHint}</p>
      <div id="transfer-requests-list">
        {list.map((request) => (
          <TransferRequestItem
            key={request.id}
            request={request}
            choices={choices[request.id]}
            error={errors[request.id]}
            busy={busy[request.id]}
            onChoice={onChoice}
            onAnswer={onAnswer}
          />
        ))}
      </div>
    </div>
  );
}

// ---- Show the answers to everyone ----

export function ShowAnswersSection({
  visible,
  forced,
  onForce,
}: {
  visible: boolean;
  forced: boolean;
  onForce: () => void;
}) {
  return (
    <div id="show-answers-section" className={'show-answers-control' + (visible ? '' : ' hidden')}>
      <div className="show-answers-box">
        <p className="section-hint">{T.showAnswersHint}</p>
        <div id="answers-status" className="answers-status" style={{ display: forced ? 'block' : 'none' }}>
          {forced ? T.answersShownStatus : ''}
        </div>
        <button
          type="button"
          id="show-answers-btn"
          className={'show-answers-btn' + (forced ? ' forced' : '')}
          disabled={forced}
          onClick={onForce}
        >
          {forced ? T.answersAlreadyShown : T.showAnswers}
        </button>
      </div>
    </div>
  );
}

// ---- Quizzes to download (the "quizzes" list of the config) ----

export function DownloadableQuizzesSection({
  quizzes,
  downloading,
  onDownload,
}: {
  quizzes: DownloadableQuiz[];
  downloading: Record<number, boolean>;
  onDownload: (index: number) => void;
}) {
  return (
    <div id="downloadable-quizzes-section" className={'downloadable-quizzes' + (quizzes.length > 0 ? '' : ' hidden')}>
      <div className="form-group">
        <label>{T.downloadableTitle}</label>
        <div id="downloadable-quiz-list" className="quiz-list">
          {quizzes.map((quiz, index) => (
            <div className="downloadable-quiz-item" key={index}>
              <div className="downloadable-quiz-info">
                <div className="downloadable-quiz-name">{plain(quiz.name)}</div>
                <div className="downloadable-quiz-url">{plain(quiz.download_path)}</div>
                <small className="muted">{T.folder(plain(quiz.folder))}</small>
              </div>
              <button
                type="button"
                className="download-btn"
                id={`download-btn-${index}`}
                disabled={Boolean(downloading[index])}
                onClick={() => onDownload(index)}
              >
                {downloading[index] ? T.downloadingQuiz : T.download}
              </button>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}

// ---- Addresses for other devices ----

export function NetworkInfoSection({ urls, tunnelURL }: { urls: NetworkURL[]; tunnelURL: string | null }) {
  return (
    <div className="network-info" id="network-info">
      <div id="access-urls" className="info-section">
        <strong>{T.accessURLs}</strong>
        <div id="url-list" className="url-list">
          {tunnelURL ? (
            <div id="tunnel-url-item" className="url-item tunnel-url-item">
              <div className="label">
                <strong>{T.publicURL}</strong>{' '}
                <a href={tunnelURL} target="_blank" rel="noopener">
                  {tunnelURL}
                </a>
              </div>
            </div>
          ) : null}
          {urls.length > 0 ? (
            urls.map((url, index) => (
              <div className="url-item" key={index}>
                <div className="label">
                  <a href={url.quiz_url} target="_blank" rel="noopener">
                    {url.quiz_url}
                  </a>
                </div>
              </div>
            ))
          ) : (
            <div className="url-item">
              <div className="label">{T.noInterfaces}</div>
              <div className="url">{T.noInterfacesHint}</div>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

// ---- SSH tunnel ----

export interface TunnelView {
  server: string; // kept from earlier messages when a message has none
  socketName: string;
  showKey: boolean;
  connected: boolean;
  error: string;
  keysOK: boolean;
}

export function TunnelSection({
  view,
  publicKey,
  busy,
  onToggle,
  showMessage,
}: {
  view: TunnelView;
  publicKey: string;
  busy: boolean;
  onToggle: () => void;
  showMessage: ShowMessage;
}) {
  const keyField = useRef<HTMLTextAreaElement>(null);
  const [copied, setCopied] = useState(false);

  const done = () => {
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  // The clipboard API needs a secure page (https or localhost); over plain
  // http on an IP address the old execCommand way still works.
  const copyPublicKey = () => {
    const field = keyField.current;
    if (!field) return;
    try {
      if (navigator.clipboard && window.isSecureContext) {
        navigator.clipboard.writeText(field.value).then(done, (error: unknown) => {
          showMessage(T.copyFailed(error instanceof Error ? error.message : String(error)), 'error');
        });
      } else {
        field.select();
        field.setSelectionRange(0, 99999); // for phones
        if (document.execCommand('copy')) {
          done();
          const selection = window.getSelection();
          if (selection) selection.removeAllRanges();
        } else {
          showMessage(T.copyManually, 'error');
        }
      }
    } catch (error) {
      showMessage(T.copyFailed(error instanceof Error ? error.message : String(error)), 'error');
    }
  };

  return (
    <div className="tunnel-info" id="tunnel-section">
      <h3>{T.tunnelTitle}</h3>
      <div className="info-section">
        <strong>{T.tunnelServer}</strong> <span id="tunnel-server">{view.server}</span>
      </div>
      <div id="tunnel-socket-name-section" className="info-section" style={{ display: view.socketName ? 'block' : 'none' }}>
        <strong>{T.tunnelSocketName}</strong> <span id="tunnel-socket-name">{view.socketName}</span>
      </div>
      <div id="tunnel-public-key-section" className="info-section" style={{ display: view.showKey ? 'block' : 'none' }}>
        <strong>{T.tunnelPublicKey}</strong>
        <div className="public-key-row">
          <textarea id="tunnel-public-key" ref={keyField} readOnly value={publicKey} />
          <button type="button" id="copy-public-key-btn" className={copied ? 'copied' : undefined} onClick={copyPublicKey}>
            {copied ? T.copied : T.copy}
          </button>
        </div>
      </div>
      <div className="info-section">
        <button
          type="button"
          id="tunnel-connect-btn"
          disabled={busy || (!view.connected && !view.keysOK)}
          onClick={onToggle}
        >
          {view.connected ? T.disconnect : T.connect}
        </button>
      </div>
      <div id="tunnel-error" className="tunnel-error" style={{ display: !view.connected && view.error ? 'block' : 'none' }}>
        {!view.connected && view.error ? T.tunnelError(view.error) : ''}
      </div>
    </div>
  );
}
