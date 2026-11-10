import { memo, useEffect, useRef, useState, type DragEvent, type MouseEvent } from 'react';
import { T } from './texts';
import { explanationToggleText, indicatorsOf, type QuestionDraft, type QuestionType, type TimeLimitMode } from './wizard';

export interface CheckerTemplate {
  name: string;
  code: string;
}

// Where a picked image goes.
export type ImageTarget =
  | { kind: 'question-image'; questionKey: number }
  | { kind: 'option-text'; questionKey: number; optionKey: number }
  | { kind: 'explanation'; questionKey: number };

// The explanation preview of a question: the server's HTML, or empty text.
export type Preview = { html: string } | { empty: true };

// What a question can ask its editor to do.
export interface QuestionActions {
  update(key: number, patch: Partial<QuestionDraft>): void;
  toggleCollapse(key: number): void;
  move(key: number, direction: -1 | 1): void;
  remove(key: number): void;
  setType(key: number, type: QuestionType): void;
  addOption(key: number): void;
  removeOption(key: number, optionKey: number): void;
  setOptionText(key: number, optionKey: number, text: string): void;
  setCorrect(key: number, optionKey: number, checked: boolean): void;
  openImagePicker(target: ImageTarget): void;
  openFilePicker(key: number): void;
  openFullscreen(src: string): void;
  toggleExplanation(key: number): void;
  togglePreview(key: number): void;
  registerExplanation(key: number, element: HTMLTextAreaElement | null): void;
  dragStart(key: number, event: DragEvent<HTMLDivElement>): void;
  dragEnd(event: DragEvent<HTMLDivElement>): void;
  dragEnter(key: number, event: DragEvent<HTMLDivElement>): void;
  dragLeave(key: number): void;
  drop(key: number, event: DragEvent<HTMLDivElement>): void;
}

interface Props {
  question: QuestionDraft;
  index: number;
  count: number;
  timeLimitMode: TimeLimitMode;
  preview: Preview | undefined;
  templates: CheckerTemplate[];
  dragging: boolean;
  dragOver: boolean;
  moved: boolean;
  actions: QuestionActions;
}

function isImagePath(value: string): boolean {
  return value.trim().indexOf('/imgs/') === 0;
}

// The question picture under its field, for any value of the field (like
// the old page did for a loaded quiz). It shows once the picture has loaded,
// so a missing picture shows nothing.
function ImagePreview({ path, onOpen }: { path: string; onOpen: (src: string) => void }) {
  const [loaded, setLoaded] = useState('');
  const src = path.trim();
  return (
    <div className={'image-preview-container' + (src && loaded === src ? ' has-image' : '')}>
      {src ? (
        <img
          className="image-preview"
          src={src}
          alt={T.imagePreviewAlt}
          title={T.clickToEnlarge}
          onLoad={() => setLoaded(src)}
          onError={() => setLoaded('')}
          onClick={(event) => onOpen(event.currentTarget.src)}
        />
      ) : null}
    </div>
  );
}

// A small picture next to an option whose text is an image path.
function OptionImagePreview({ path, onOpen }: { path: string; onOpen: (src: string) => void }) {
  const [failed, setFailed] = useState('');
  const src = path.trim();
  if (!isImagePath(src) || failed === src) return null;
  return (
    <img
      className="option-image-preview"
      src={src}
      alt={T.optionPreviewAlt}
      title={T.clickToEnlarge}
      onError={() => setFailed(src)}
      onClick={(event) => onOpen(event.currentTarget.src)}
    />
  );
}

// One question of the editor. It is memoized: typing in one question redraws
// only that question, because the editor passes the same actions object and
// the same question objects for the others.
export const QuestionItem = memo(function QuestionItem({
  question,
  index,
  count,
  timeLimitMode,
  preview,
  templates,
  dragging,
  dragOver,
  moved,
  actions,
}: Props) {
  const key = question.key;
  const itemRef = useRef<HTMLDivElement>(null);
  const textRef = useRef<HTMLInputElement>(null);

  // A new question scrolls into view and its text field gets the focus
  useEffect(() => {
    if (!question.focus) return undefined;
    if (itemRef.current && itemRef.current.scrollIntoView) {
      itemRef.current.scrollIntoView({ behavior: 'smooth', block: 'nearest' });
    }
    const timer = setTimeout(() => {
      if (textRef.current) textRef.current.focus();
    }, 300);
    return () => clearTimeout(timer);
    // only when the question first appears, so no dependencies
  }, []);

  // A moved question scrolls to the middle of the editor
  useEffect(() => {
    if (!moved || !itemRef.current || !itemRef.current.scrollIntoView) return undefined;
    const timer = setTimeout(() => {
      if (itemRef.current) itemRef.current.scrollIntoView({ behavior: 'smooth', block: 'center', inline: 'nearest' });
    }, 50);
    return () => clearTimeout(timer);
  }, [moved]);

  const indicators = indicatorsOf(question);
  const previewing = preview !== undefined;
  const multiple = question.choiceKind === 'multiple';

  const onHeaderClick = (event: MouseEvent<HTMLDivElement>) => {
    // Buttons and the drag handle do not fold the question
    const target = event.target as HTMLElement;
    if (target.closest('.question-header-buttons') || target.closest('.drag-handle')) return;
    actions.toggleCollapse(key);
  };

  const classes = ['question-item'];
  if (question.collapsed) classes.push('collapsed');
  if (dragging) classes.push('dragging');
  if (dragOver) classes.push('drag-over');
  if (moved) classes.push('moved');

  return (
    <div
      ref={itemRef}
      className={classes.join(' ')}
      data-question-id={key}
      onMouseUp={(event) => {
        event.currentTarget.draggable = false;
      }}
      onDragStart={(event) => actions.dragStart(key, event)}
      onDragEnd={(event) => actions.dragEnd(event)}
      onDragOver={(event) => {
        event.preventDefault();
        event.dataTransfer.dropEffect = 'move';
      }}
      onDragEnter={(event) => actions.dragEnter(key, event)}
      onDragLeave={() => actions.dragLeave(key)}
      onDrop={(event) => actions.drop(key, event)}
    >
      <div className="question-header" onClick={onHeaderClick}>
        <div className="question-header-left">
          <span
            className="drag-handle"
            title={T.dragHandle}
            onMouseDown={() => {
              // Only the handle starts a drag, so text in the fields stays selectable
              if (itemRef.current) itemRef.current.draggable = true;
            }}
          >
            ☰
          </span>
          <span className="question-collapse-icon">▼</span>
          <span className="question-number">{index + 1}.</span>
          <span className="question-indicators">
            {indicators.stick ? (
              <span className="question-indicator" title={T.stickIndicator}>
                🔗
              </span>
            ) : null}
            {indicators.image ? (
              <span className="question-indicator" title={T.imageIndicator}>
                🖼️
              </span>
            ) : null}
            {indicators.file ? (
              <span className="question-indicator" title={T.fileIndicator}>
                📎
              </span>
            ) : null}
            {indicators.points !== null ? (
              <span
                className="question-indicator points-indicator question-points-badge"
                title={T.pointsIndicator(String(indicators.points))}
              >
                {'🏆 ' + indicators.points}
              </span>
            ) : null}
          </span>
          <span className="question-preview">{question.text.trim()}</span>
        </div>
        <div className="question-header-buttons">
          {index > 0 ? (
            <button type="button" title={T.moveUp} className="move-up-btn" onClick={() => actions.move(key, -1)}>
              ↑
            </button>
          ) : null}
          {index < count - 1 ? (
            <button type="button" title={T.moveDown} className="move-down-btn" onClick={() => actions.move(key, 1)}>
              ↓
            </button>
          ) : null}
          <button type="button" title={T.remove} className="btn-danger remove-question-btn" onClick={() => actions.remove(key)}>
            ×
          </button>
        </div>
      </div>

      <div className="question-body">
        <div className="form-group">
          <label>{T.questionTextLabel}</label>
          <input
            ref={textRef}
            type="text"
            className="question-text"
            placeholder={T.questionTextPlaceholder}
            value={question.text}
            onChange={(event) => actions.update(key, { text: event.target.value })}
          />
        </div>

        <div className="form-group">
          <label>{T.questionImageLabel}</label>
          <div className="field-with-button">
            <input
              type="text"
              className="question-image"
              placeholder={T.questionImagePlaceholder}
              value={question.image}
              onChange={(event) => actions.update(key, { image: event.target.value })}
            />
            <button
              type="button"
              className="picker-btn"
              title={T.selectImage}
              onClick={() => actions.openImagePicker({ kind: 'question-image', questionKey: key })}
            >
              📷
            </button>
          </div>
          <ImagePreview path={question.image} onOpen={actions.openFullscreen} />
        </div>

        <div className="form-group">
          <label>{T.questionFileLabel}</label>
          <div className="field-with-button">
            <input
              type="text"
              className="question-file"
              placeholder={T.questionFilePlaceholder}
              value={question.file}
              onChange={(event) => actions.update(key, { file: event.target.value })}
            />
            <button type="button" className="picker-btn" title={T.selectFile} onClick={() => actions.openFilePicker(key)}>
              📎
            </button>
          </div>
        </div>

        <div className="form-group">
          <label>{T.questionTypeLabel}</label>
          <div className="question-type-toggles">
            {(
              [
                ['single', T.typeSingle],
                ['multiple', T.typeMultiple],
                ['text', T.typeText],
              ] as [QuestionType, string][]
            ).map(([type, label]) => (
              <button
                key={type}
                type="button"
                className={'question-type-toggle' + (question.type === type ? ' active' : '')}
                data-type={type}
                onClick={() => actions.setType(key, type)}
              >
                {label}
              </button>
            ))}
          </div>
        </div>

        <div className="choice-question-fields" style={{ display: question.type === 'text' ? 'none' : 'block' }}>
          <div className="form-group">
            <label>{T.optionsLabel}</label>
            <div className="options-container">
              {question.options.map((option, optionIndex) => (
                <div className="option-item" key={option.key} data-option-index={optionIndex}>
                  <input
                    type={multiple ? 'checkbox' : 'radio'}
                    name={multiple ? `correct-${key}-multiple` : `correct-${key}`}
                    value={optionIndex}
                    checked={option.correct}
                    onChange={(event) => actions.setCorrect(key, option.key, event.target.checked)}
                  />
                  <input
                    type="text"
                    className="option-text"
                    placeholder={T.optionPlaceholder(optionIndex + 1)}
                    value={option.text}
                    onChange={(event) => actions.setOptionText(key, option.key, event.target.value)}
                  />
                  <OptionImagePreview path={option.text} onOpen={actions.openFullscreen} />
                  <button
                    type="button"
                    className="option-btn"
                    title={T.selectImage}
                    onClick={() => actions.openImagePicker({ kind: 'option-text', questionKey: key, optionKey: option.key })}
                  >
                    📷
                  </button>
                  <button type="button" className="option-btn remove-option-btn" onClick={() => actions.removeOption(key, option.key)}>
                    {T.removeOption}
                  </button>
                </div>
              ))}
            </div>
            <button type="button" className="add-option-btn" onClick={() => actions.addOption(key)}>
              {T.addOption}
            </button>
          </div>
          <div className="form-group min-correct-container" style={{ display: multiple ? 'block' : 'none' }}>
            <label htmlFor={`min-correct-${key}`}>{T.minCorrectLabel}</label>
            <input
              type="number"
              id={`min-correct-${key}`}
              className="min-correct-input"
              min="1"
              placeholder={T.minCorrectPlaceholder}
              value={question.minCorrect}
              onChange={(event) => actions.update(key, { minCorrect: event.target.value })}
            />
          </div>
        </div>

        <div className="text-question-fields" style={{ display: question.type === 'text' ? 'block' : 'none' }}>
          <div className="form-group">
            <label>{T.defaultValueLabel}</label>
            <textarea
              className="default-value-input code-field"
              placeholder={T.defaultValuePlaceholder}
              value={question.defaultValue}
              onChange={(event) => actions.update(key, { defaultValue: event.target.value })}
            />
          </div>
          <div className="form-group">
            <label>{T.correctValueLabel}</label>
            <textarea
              className="correct-value-input code-field"
              placeholder={T.correctValuePlaceholder}
              value={question.correctValue}
              onChange={(event) => actions.update(key, { correctValue: event.target.value })}
            />
          </div>
          <div className="form-group">
            <label>{T.checkerLabel}</label>
            <div className="checker-template-row">
              <select
                className="checker-template-select"
                value=""
                onChange={(event) => {
                  if (event.target.value) actions.update(key, { checker: event.target.value });
                }}
              >
                <option value="">{T.chooseTemplate}</option>
                {templates.map((template, templateIndex) => (
                  <option key={templateIndex} value={template.code}>
                    {template.name}
                  </option>
                ))}
              </select>
            </div>
            <textarea
              className="checker-input code-field checker-field"
              value={question.checker}
              onChange={(event) => actions.update(key, { checker: event.target.value })}
            />
          </div>
        </div>

        <div className="form-group">
          <label htmlFor={`points-${key}`} className="inline-label">
            {T.pointsLabel}
          </label>
          <input
            type="number"
            id={`points-${key}`}
            className="points-input"
            min="0.01"
            step="0.01"
            placeholder="1"
            value={question.points}
            onChange={(event) => actions.update(key, { points: event.target.value })}
          />
        </div>

        <div
          className="form-group question-time-limit-group"
          style={{ display: timeLimitMode === 'per_question' ? '' : 'none' }}
        >
          <label className="inline-label">{T.questionTimeLimitLabel}</label>
          <input
            type="number"
            className="question-time-limit-input"
            min="0"
            step="1"
            placeholder={T.questionTimeLimitPlaceholder}
            value={question.timeLimit}
            onChange={(event) => actions.update(key, { timeLimit: event.target.value })}
          />
          <small>{T.zeroMeansNoLimit}</small>
        </div>

        <div className={'form-group explanation-group' + (question.explanationOpen ? '' : ' folded')}>
          <button type="button" className="explanation-toggle-btn" onClick={() => actions.toggleExplanation(key)}>
            {explanationToggleText(question)}
          </button>
          <div className="explanation-body">
            <label>{T.explanationLabel}</label>
            <div className="explanation-tools">
              <button
                type="button"
                className={'explanation-image-btn' + (previewing ? ' hidden' : '')}
                onClick={() => actions.openImagePicker({ kind: 'explanation', questionKey: key })}
              >
                {T.insertImage}
              </button>
              <button type="button" className="explanation-preview-btn" onClick={() => actions.togglePreview(key)}>
                {previewing ? T.backToEditing : T.preview}
              </button>
            </div>
            <textarea
              ref={(element) => actions.registerExplanation(key, element)}
              className={'question-explanation' + (previewing ? ' hidden' : '')}
              placeholder={T.explanationPlaceholder}
              value={question.explanation}
              onChange={(event) => actions.update(key, { explanation: event.target.value })}
            />
            {preview && 'html' in preview ? (
              // Rendered by the server: raw HTML is escaped and unsafe links removed there
              <div className="explanation-preview explanation-content" dangerouslySetInnerHTML={{ __html: preview.html }} />
            ) : (
              <div className={'explanation-preview explanation-content' + (preview ? '' : ' hidden')}>
                {preview ? <p className="explanation-preview-empty">{T.emptyExplanation}</p> : null}
              </div>
            )}
          </div>
        </div>

        <div className="form-group">
          <label className="checkbox-label stick-label">
            <input
              type="checkbox"
              className="stick-to-previous-checkbox"
              checked={question.stick}
              onChange={(event) => actions.update(key, { stick: event.target.checked })}
            />
            <span>{T.stickLabel}</span>
          </label>
        </div>
      </div>
    </div>
  );
});
