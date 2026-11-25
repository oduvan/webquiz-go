import { useState, useSyncExternalStore, type KeyboardEvent } from 'react';
import { formatPoints } from '../shared/points';
import { plain } from '../shared/text';
import { fillCount, idleContent, type ReviewClient } from './client';

// The review page, made for a phone: one waiting answer at a time with two big
// buttons. Element ids are the ones of the Python page, so both versions can
// be tested the same way.

function LoginForm({ client }: { client: ReviewClient }) {
  const T = client.T;
  const view = useSyncExternalStore(client.subscribe, client.getSnapshot);
  const [key, setKey] = useState('');
  const onKeyDown = (event: KeyboardEvent) => {
    if (event.key === 'Enter') void client.login(key);
  };
  return (
    <div id="login-section" className="review-card login-form">
      <div id="login-title" className="login-title">
        {T.review_login_title}
      </div>
      {view.loginError ? (
        <div id="login-error" className="review-login-error">
          {T.review_login_error}
        </div>
      ) : null}
      <label htmlFor="master-key-input" id="master-key-label" className="review-label">
        {T.review_master_key}
      </label>
      <input
        type="password"
        id="master-key-input"
        autoComplete="current-password"
        autoFocus
        value={key}
        onChange={(event) => setKey(event.target.value)}
        onKeyDown={onKeyDown}
      />
      <button type="button" id="login-button" onClick={() => void client.login(key)}>
        {T.review_login_button}
      </button>
    </div>
  );
}

function Review({ client }: { client: ReviewClient }) {
  const T = client.T;
  const view = useSyncExternalStore(client.subscribe, client.getSnapshot);
  const item = view.item;
  const idle = idleContent(view, T);
  const sample = item ? plain(item.correct_value) : '';
  return (
    <div id="review-section">
      {view.connectionLost ? (
        <div id="connection-banner" className="review-connection-banner" role="status">
          {T.connection_lost}
        </div>
      ) : null}
      <div className="review-status-bar">
        <span id="pending-count">{view.received ? fillCount(T.review_waiting_count, view.pendingCount) : ''}</span>
      </div>
      {view.notice ? (
        <div id="notice" className="review-notice" role="status">
          {view.notice}
        </div>
      ) : null}

      {view.received && !item ? (
        <div id="idle" className="review-card review-idle">
          {idle.text !== null ? <div id="idle-text">{idle.text}</div> : null}
          {idle.helpText !== null ? (
            <div id="help-block">
              <p id="help-text">{idle.helpText}</p>
              <button
                type="button"
                id="help-button"
                className="review-help-button"
                disabled={!view.helpEnabled}
                onClick={client.takeShared}
              >
                {T.review_help_button}
              </button>
            </div>
          ) : null}
        </div>
      ) : null}

      {item ? (
        <div id="item-card" className="review-card" key={item.review_id}>
          <div className="review-label" id="question-label">
            {T.review_question}
          </div>
          <div className="review-question-text" id="item-question">
            {plain(item.question)}
          </div>
          {item.image ? <img id="item-image" className="review-question-image" src={plain(item.image)} alt="" /> : null}
          <div className="review-label" id="answer-label">
            {T.review_student_answer}
          </div>
          <div className="review-answer-text" id="item-answer">
            {plain(item.answer)}
          </div>
          {sample ? (
            <div id="sample-block">
              <div className="review-label" id="sample-label">
                {T.review_sample_answer}
              </div>
              <div className="review-sample-text" id="item-sample">
                {sample}
              </div>
            </div>
          ) : null}
          <div className="review-points" id="item-points">
            {fillCount(T.review_points, formatPoints(Number(item.points) || 0))}
          </div>
          <div className="review-verdict-buttons">
            <button type="button" id="verdict-correct" disabled={!view.buttonsEnabled} onClick={() => client.sendVerdict(true)}>
              {T.review_correct}
            </button>
            <button
              type="button"
              id="verdict-incorrect"
              disabled={!view.buttonsEnabled}
              onClick={() => client.sendVerdict(false)}
            >
              {T.review_incorrect}
            </button>
          </div>
        </div>
      ) : null}
    </div>
  );
}

export function ReviewPage({ client }: { client: ReviewClient }) {
  const view = useSyncExternalStore(client.subscribe, client.getSnapshot);
  return (
    <div className="review-page">
      <h1 id="review-title">{client.T.review_title}</h1>
      {view.phase === 'login' ? <LoginForm client={client} /> : null}
      {view.phase === 'review' ? <Review client={client} /> : null}
    </div>
  );
}
