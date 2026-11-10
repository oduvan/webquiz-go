import { useSyncExternalStore, type ClipboardEvent } from 'react';
import type { QuizBoot } from '../shared/boot';
import { plain } from '../shared/text';
import { Button, ConnectionBanner } from '../shared/ui';
import type { QuizController } from './controller';
import { QuestionCard } from './QuestionCard';
import { Results } from './Results';
import { SelectionRequired } from './SelectionRequired';
import type { MessageView, ViewState } from './types';

// Question text cannot be copied (the old page blocked the copy event too).
function blockCopy(event: ClipboardEvent) {
  event.preventDefault();
}

function hiddenUnless(visible: boolean, classes = ''): string {
  return (classes + (visible ? '' : ' hidden')).trim();
}

function Message({ id, message }: { id: string; message: MessageView | null }) {
  const classes = 'error-message' + (message && message.success ? ' success-message' : '');
  return (
    <div id={id} className={hiddenUnless(message !== null, classes)} role="alert">
      {message ? message.text : ''}
    </div>
  );
}

function Registration({ state, controller }: { state: ViewState; controller: QuizController }) {
  const T = controller.T;
  const boot = controller.boot;
  const registration = state.registration;
  return (
    <section id="registration" className={hiddenUnless(registration.visible, 'section px-3 md:px-0')}>
      <h2 className="mb-4 text-xl font-semibold">{T.register_title}</h2>
      <Message id="registration-error" message={registration.error ? { text: registration.error, success: false } : null} />
      <div className="registration-table mx-auto w-full max-w-md">
        <div className="registration-row mb-3">
          <label htmlFor="username" className="registration-label mb-1 block font-semibold">
            {boot.username_label}:
          </label>
          <input
            id="username"
            name="username"
            type="text"
            className="text-input"
            value={registration.username}
            onChange={(event) => controller.setRegistrationUsername(event.target.value)}
          />
        </div>
        {boot.registration_fields.map((field, index) => (
          <div className="registration-row mb-3" key={index}>
            <label htmlFor={'registration-field-' + index} className="registration-label mb-1 block font-semibold">
              {field.label}:
            </label>
            <input
              id={'registration-field-' + index}
              name={field.name}
              data-field-name={field.name}
              type="text"
              className="registration-field text-input"
              value={registration.fields[field.name] || ''}
              onChange={(event) => controller.setRegistrationField(field.name, event.target.value)}
            />
          </div>
        ))}
      </div>
      <div className="mt-4 text-center">
        <Button className="register-btn" disabled={registration.busy} onClick={() => void controller.registerUser()}>
          {T.register_button}
        </Button>
      </div>
    </section>
  );
}

function Waiting({ state, controller }: { state: ViewState; controller: QuizController }) {
  const T = controller.T;
  const waiting = state.waiting;
  return (
    <section id="waiting-approval" className={hiddenUnless(waiting.visible, 'section px-3 md:px-0')}>
      <h2 className="mb-4 text-xl font-semibold">{T.waiting_title}</h2>
      <p className="my-4 text-center">
        {T.waiting_description}
        <br />
        {T.waiting_hint}
      </p>
      <Message id="waiting-error" message={waiting.message} />
      {/* Drawn only while waiting, like the old page: before that, [data-field-name]
          finds only the registration form's fields */}
      {waiting.visible ? <WaitingFields state={state} controller={controller} /> : null}
      <div className="mt-5 flex flex-wrap justify-center">
        <Button className="save-registration-btn m-1" onClick={() => void controller.updateRegistrationData()}>
          {T.save_changes}
        </Button>
        <Button className="check-status-btn m-1" onClick={() => void controller.checkApprovalStatus()}>
          {T.check_status}
        </Button>
      </div>
    </section>
  );
}

function WaitingFields({ state, controller }: { state: ViewState; controller: QuizController }) {
  const boot = controller.boot;
  const waiting = state.waiting;
  return (
    <div id="waiting-registration-fields" className="registration-table mx-auto w-full max-w-md">
      <div className="registration-row mb-3">
        <label htmlFor="waiting-username" className="registration-label mb-1 block font-semibold">
          {boot.username_label}:
        </label>
        <input
          id="waiting-username"
          type="text"
          className="text-input"
          value={waiting.username}
          onChange={(event) => controller.setWaitingUsername(event.target.value)}
        />
      </div>
      {boot.registration_fields.map((field, index) => (
        <div className="registration-row mb-3" key={index}>
          <label htmlFor={'waiting-field-' + index} className="registration-label mb-1 block font-semibold">
            {field.label}:
          </label>
          <input
            id={'waiting-field-' + index}
            data-field-name={field.name}
            type="text"
            className="waiting-field text-input"
            value={waiting.fields[field.name] || ''}
            onChange={(event) => controller.setWaitingField(field.name, event.target.value)}
          />
        </div>
      ))}
    </div>
  );
}

function TestSection({ state, controller }: { state: ViewState; controller: QuizController }) {
  const T = controller.T;
  const test = state.test;
  const explanation = test.explanation;
  return (
    <section id="test-section" className={hiddenUnless(test.visible, 'section')}>
      <div id="quiz-timer-box" className={hiddenUnless(test.quizTimer.visible, 'timer-box quiz-timer-box')}>
        ⏱ <span className="timer-label">{T.time_left}</span>{' '}
        <span id="quiz-timer" className={test.quizTimer.warning ? 'timer-warning' : ''}>
          {test.quizTimer.text}
        </span>
      </div>
      <div id="progress-bar" className="progress my-3 md:my-5">
        <div id="progress-fill" style={{ width: test.progress.percent + '%' }} />
        <span id="progress-text">{test.progress.text}</span>
      </div>
      <Message id="test-error" message={test.error ? { text: test.error, success: false } : null} />
      <div id="current-question-container" className={test.containerHidden ? 'hidden' : ''} onCopy={blockCopy}>
        {test.question ? <QuestionCard key={test.question.key} view={test.question} controller={controller} /> : null}
      </div>
      {/* Outside #current-question-container: the explanation itself may be copied */}
      <div id="explanation-step" className={hiddenUnless(explanation.visible, 'explanation-step')}>
        <h3 className="explanation-title mb-2 text-xl font-semibold">{T.explanation_title}</h3>
        <div className="explanation-question" onCopy={blockCopy}>
          {explanation.question}
        </div>
        {/* Rendered on the server from Markdown: raw HTML is escaped and unsafe links removed there */}
        <div className="explanation-content" dangerouslySetInnerHTML={{ __html: explanation.html }} />
        <div className="button-container mt-4 flex justify-end">
          <Button id="explanation-next-btn" disabled={explanation.nextDisabled} onClick={() => void controller.nextAfterExplanation()}>
            {T.next_button}
          </Button>
        </div>
      </div>
      {/* flex-wrap: on a very narrow phone the verdict and "Continue" may take two lines */}
      <div className="button-container my-5 flex flex-wrap items-center px-3 md:px-0">
        <Button id="submit-answer-btn" className={test.submit.hidden ? 'hidden' : ''} disabled={test.submit.disabled} onClick={controller.submitClicked}>
          {T.submit_answer}
        </Button>
        {/* The verdict in words (new in 2.0): colors and labels alone did not say
            that a partly right answer counts as wrong */}
        {test.verdict ? (
          <span id="answer-verdict" role="status" className={'answer-verdict mr-3 text-lg font-bold ' + test.verdict}>
            {test.verdict === 'correct' ? T.answer_verdict_correct : T.answer_verdict_incorrect}
          </span>
        ) : null}
        <Button
          id="continue-btn"
          className={'ml-auto' + (test.continueButton.hidden ? ' hidden' : '')}
          disabled={test.continueButton.disabled}
          onClick={controller.continueClicked}
        >
          {T.continue_button}
        </Button>
      </div>
    </section>
  );
}

export function QuizPage({ boot, controller }: { boot: QuizBoot; controller: QuizController | null }) {
  if (boot.state === 'quiz_selection_required' || !controller) {
    return <SelectionRequired boot={boot} />;
  }
  return <RunningQuiz controller={controller} />;
}

function RunningQuiz({ controller }: { controller: QuizController }) {
  const state = useSyncExternalStore(controller.subscribe, controller.getSnapshot);
  const T = controller.T;
  const boot = controller.boot;
  return (
    <>
      <div className="connection-banner-holder">
        <ConnectionBanner text={T.connection_lost} />
      </div>
      <div id="user-info" className={hiddenUnless(state.username !== null, 'user-info')}>
        👤 <span id="username-display">{state.username}</span>
      </div>
      <main className="quiz-card mx-auto mt-14 w-full max-w-3xl py-4 md:mt-16 md:px-8 md:py-8">
        <h1 id="quiz-title" className="mb-4 px-3 text-2xl font-bold md:px-0">
          {plain(boot.title)}
        </h1>
        <section id="loading" className={hiddenUnless(state.loading, 'section py-10 text-center')}>
          <h2 className="text-xl font-semibold">{T.loading}</h2>
          <p className="muted mt-2 italic">{T.checking_data}</p>
        </section>
        <Registration state={state} controller={controller} />
        <Waiting state={state} controller={controller} />
        <TestSection state={state} controller={controller} />
        <section id="results" className={hiddenUnless(state.results.visible, 'section px-3 md:px-0')}>
          <div id="results-content">
            {state.results.content ? (
              <Results
                content={state.results.content}
                T={T}
                showFinalList={boot.show_final_list}
                showRightAnswer={boot.show_right_answer}
              />
            ) : null}
          </div>
        </section>
      </main>
      <footer className="muted mx-auto my-8 w-full max-w-3xl border-t px-3 pt-4 text-center text-xs" style={{ borderColor: 'var(--border)' }}>
        WebQuiz v{boot.version}
      </footer>
    </>
  );
}
