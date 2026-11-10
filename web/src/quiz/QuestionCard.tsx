import type { KeyboardEvent } from 'react';
import type { ChoiceQuestion, Translations } from '../shared/boot';
import { plain } from '../shared/text';
import type { QuizController } from './controller';
import { fileName, isImagePath, makeRelativePath, pointsBadgeText } from './format';
import type { FeedbackClass, QuestionView } from './types';

// The label an answered option shows; one label per option, the last class wins as in the old CSS.
function feedbackLabel(classes: FeedbackClass[], T: Translations): string | null {
  if (classes.indexOf('feedback-missed') !== -1) return T.also_correct_label;
  if (classes.indexOf('feedback-incorrect') !== -1) return T.your_answer_label;
  if (classes.indexOf('feedback-correct') !== -1) return T.correct_answer_label;
  return null;
}

function Options({ question, view, controller }: { question: ChoiceQuestion; view: QuestionView; controller: QuizController }) {
  const T = controller.T;
  const multiple = question.is_multiple_choice;
  return (
    <div className="options mt-3" role={multiple ? 'group' : 'radiogroup'}>
      {multiple ? <p className="multiple-choice-hint muted mb-2 px-3 text-sm md:px-0">{T.multiple_choice_hint}</p> : null}
      {question.options.map((option, index) => {
        const image = isImagePath(option);
        const selected = view.selected.indexOf(index) !== -1;
        const feedback = view.feedback[index] || [];
        const classes = ['option', 'quiz-option'];
        if (image) classes.push('image-option');
        if (selected) classes.push('selected');
        feedback.forEach((name) => classes.push(name));
        if (view.optionsDisabled) classes.push('disabled');
        const label = feedbackLabel(feedback, T);
        const choose = () => controller.selectCurrentAnswer(index);
        const onKeyDown = (event: KeyboardEvent) => {
          if (event.key === 'Enter' || event.key === ' ') {
            event.preventDefault();
            choose();
          }
        };
        return (
          <div
            key={index}
            id={'option_' + index}
            data-option-index={index}
            className={classes.join(' ')}
            role={multiple ? 'checkbox' : 'radio'}
            aria-checked={selected}
            aria-disabled={view.optionsDisabled || undefined}
            tabIndex={view.optionsDisabled ? -1 : 0}
            onClick={choose}
            onKeyDown={onKeyDown}
          >
            <span className="option-marker" aria-hidden="true" />
            {image ? (
              <div className="option-content">
                <img src={makeRelativePath(option)} alt={String(index + 1)} className="option-image" />
              </div>
            ) : (
              <span className="option-text">{plain(option)}</span>
            )}
            {label ? <span className="feedback-label">{label}</span> : null}
          </div>
        );
      })}
    </div>
  );
}

function TextAnswer({ view, controller }: { view: QuestionView; controller: QuizController }) {
  const T = controller.T;
  const classes = ['text-answer-input'];
  if (view.textFeedback) classes.push(view.textFeedback);
  return (
    <div className="text-input-container mt-4 px-3 md:px-0">
      <label htmlFor="text-answer-input" className="muted mb-2 block text-sm">
        {T.enter_answer_label}
      </label>
      <textarea
        id="text-answer-input"
        className={classes.join(' ')}
        placeholder={T.enter_answer_placeholder}
        defaultValue={view.defaultText}
        disabled={view.textDisabled}
        onChange={(event) => controller.setTextAnswer(event.target.value)}
      />
      <div id="checker-feedback">
        {view.checkerError ? <div className="checker-error">⚠️ {view.checkerError}</div> : null}
        {view.correctValue ? (
          <div className="correct-value-display">
            {T.correct_answer_value}
            <pre>{view.correctValue}</pre>
          </div>
        ) : null}
      </div>
    </div>
  );
}

// The question on the screen. It is drawn again (new key) for every question.
export function QuestionCard({ view, controller }: { view: QuestionView; controller: QuizController }) {
  const T = controller.T;
  const question = view.question;
  const points = question.points || 1;
  const classes = ['current-question', 'active-question', 'py-4', 'md:p-6'];
  if (view.animation) classes.push(view.animation);
  return (
    <div className={classes.join(' ')}>
      {question.time_limit > 0 ? (
        <div className={'timer-box question-timer-box mx-3 md:mx-0' + (view.timer.pending ? ' timer-pending' : '')}>
          ⏱ {T.time_left}{' '}
          <span id="question-timer" className={view.timer.warning ? 'timer-warning' : ''}>
            {view.timer.text}
          </span>
        </div>
      ) : null}
      {points !== 1 ? <div className="question-points-badge">{pointsBadgeText(points, T)}</div> : null}
      {question.question ? (
        <h3 className={'question-text px-3 text-lg font-semibold md:px-0' + (points !== 1 ? ' pr-24 md:pr-24' : '')}>
          {plain(question.question)}
        </h3>
      ) : null}
      {question.image ? (
        <a href={makeRelativePath(question.image)} target="_blank" rel="noopener noreferrer">
          {/* The file name as alt text: a missing image still shows as a box with its name */}
          <img src={makeRelativePath(question.image)} alt={fileName(question.image)} className="question-image quiz-question-img" />
        </a>
      ) : null}
      {question.file ? (
        <div className="px-3 md:px-0">
          <a href={makeRelativePath(question.file)} download className="question-file">
            <span className="question-file-icon">📎</span>
            <span className="question-file-name">{fileName(question.file)}</span>
          </a>
        </div>
      ) : null}
      {question.type === 'text' ? (
        <TextAnswer view={view} controller={controller} />
      ) : (
        <Options question={question} view={view} controller={controller} />
      )}
      {view.timeUp ? <div className="time-up-message px-3 md:px-0">⏱ {T.time_is_up}</div> : null}
    </div>
  );
}
