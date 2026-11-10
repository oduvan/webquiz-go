import { T } from './texts';
import type { StartSettingsDraft, TimeLimitMode } from './wizard';

export interface StartModalState {
  filename: string;
  restart: boolean; // the quiz is the one running now
  settings: StartSettingsDraft;
  error: string;
  busy: boolean;
}

// The pop-up shown before a quiz starts (#start-quiz-modal). The quiz file
// gives the values; changes apply to this run only and are not saved.
export function StartQuizModal({
  state,
  onChange,
  onConfirm,
  onClose,
}: {
  state: StartModalState;
  onChange: (settings: StartSettingsDraft) => void;
  onConfirm: () => void;
  onClose: () => void;
}) {
  const settings = state.settings;
  const set = (patch: Partial<StartSettingsDraft>) => onChange({ ...settings, ...patch });

  return (
    <div id="start-quiz-modal" className="modal open">
      <div className="modal-content start-quiz-content">
        <div className="modal-header">
          <h2>{T.startTitle}</h2>
          <button type="button" className="close" aria-label={T.close} onClick={onClose}>
            {T.close}
          </button>
        </div>
        <p id="start-quiz-name" className="start-quiz-name">
          {state.filename + (state.restart ? T.restartSuffix : '')}
        </p>
        <p className="start-quiz-hint">{T.startHint}</p>
        <div className="form-group">
          <label className="checkbox-label">
            <input
              type="checkbox"
              id="start-show-right-answer"
              checked={settings.showRightAnswer}
              onChange={(event) => set({ showRightAnswer: event.target.checked })}
            />
            {T.showRightAnswer}
          </label>
        </div>
        {/* "Show answers after everyone finishes" only matters when correct answers are hidden */}
        <div
          className="form-group"
          id="start-show-answers-on-completion-group"
          style={{ display: settings.showRightAnswer ? 'none' : 'block' }}
        >
          <label className="checkbox-label">
            <input
              type="checkbox"
              id="start-show-answers-on-completion"
              checked={settings.showAnswersOnCompletion}
              onChange={(event) => set({ showAnswersOnCompletion: event.target.checked })}
            />
            {T.showAnswersOnCompletion}
          </label>
        </div>
        <div className="form-group">
          <label className="checkbox-label">
            <input
              type="checkbox"
              id="start-randomize-questions"
              checked={settings.randomizeQuestions}
              onChange={(event) => set({ randomizeQuestions: event.target.checked })}
            />
            {T.randomizeQuestions}
          </label>
        </div>
        <div className="form-group">
          <label className="checkbox-label">
            <input
              type="checkbox"
              id="start-show-final-list"
              checked={settings.showFinalList}
              onChange={(event) => set({ showFinalList: event.target.checked })}
            />
            {T.showFinalList}
          </label>
        </div>
        {/* Explanations: offered only when the quiz has some, and they follow the correct answers */}
        <div
          className="form-group"
          id="start-show-explanation-group"
          style={{ display: settings.hasExplanations ? 'block' : 'none' }}
        >
          <label className="checkbox-label">
            <input
              type="checkbox"
              id="start-show-explanation"
              checked={settings.showExplanation}
              disabled={!settings.showRightAnswer}
              onChange={(event) => set({ showExplanation: event.target.checked })}
            />
            {T.showExplanation}
          </label>
          <small id="start-show-explanation-note" style={{ display: settings.showRightAnswer ? 'none' : 'block' }}>
            {T.explanationNeedsAnswers}
          </small>
        </div>
        <div className="form-group">
          <label htmlFor="start-time-limit-mode">{T.timeLimit}</label>
          <div className="time-limit-row">
            <select
              id="start-time-limit-mode"
              value={settings.timeLimitMode}
              onChange={(event) => set({ timeLimitMode: event.target.value as TimeLimitMode })}
            >
              <option value="none">{T.timeLimitNone}</option>
              <option value="per_question">{T.timeLimitPerQuestion}</option>
              <option value="whole_quiz">{T.timeLimitWholeQuiz}</option>
            </select>
            <span
              id="start-time-limit-seconds-group"
              className="time-limit-seconds-group"
              style={{ display: settings.timeLimitMode === 'none' ? 'none' : 'inline-flex' }}
            >
              <label htmlFor="start-time-limit-seconds">{T.seconds}</label>
              <input
                type="number"
                id="start-time-limit-seconds"
                min="0"
                step="1"
                value={settings.timeLimitSeconds}
                onChange={(event) => set({ timeLimitSeconds: event.target.value })}
              />
              <small>{T.zeroMeansNoLimit}</small>
            </span>
          </div>
          <small id="start-time-limit-hint" style={{ display: settings.timeLimitMode === 'per_question' ? 'block' : 'none' }}>
            {T.ownLimitsStay}
          </small>
        </div>
        <div id="start-quiz-error" className="start-quiz-error" style={{ display: state.error ? 'block' : 'none' }}>
          {state.error}
        </div>
        <div className="start-quiz-buttons">
          <button type="button" id="start-quiz-confirm" disabled={state.busy} onClick={onConfirm}>
            {T.start}
          </button>
          <button type="button" id="start-quiz-cancel" onClick={onClose}>
            {T.cancel}
          </button>
        </div>
      </div>
    </div>
  );
}
