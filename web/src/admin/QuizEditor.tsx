import { useCallback, useEffect, useMemo, useRef, useState, type DragEvent } from 'react';
import { errorText, get, post, quizURL, request } from './api';
import { useShowMessage } from './messages';
import { FilePicker, FullscreenImage, ImagePicker, type FileEntry, type ImageEntry } from './Pickers';
import { QuestionItem, type CheckerTemplate, type ImageTarget, type Preview, type QuestionActions } from './QuestionItem';
import { T } from './texts';
import {
  explanationImageMarkdown,
  hasAnyExplanation,
  insertAt,
  newOption,
  newQuestion,
  validateWizardPoints,
  wizardData,
  type QuestionDraft,
  type QuizDraft,
  type TimeLimitMode,
} from './wizard';

export interface EditorStart {
  mode: 'create' | 'edit';
  editingName: string; // the quiz file being edited ("" for a new quiz)
  filename: string; // the file name field
  draft: QuizDraft;
}

// The checker templates are read once per page, like the old page's cache.
let templatesCache: CheckerTemplate[] | null = null;
let templatesRequest: Promise<CheckerTemplate[]> | null = null;

function loadCheckerTemplates(): Promise<CheckerTemplate[]> {
  if (templatesCache) return Promise.resolve(templatesCache);
  if (!templatesRequest) {
    templatesRequest = get('/api/admin/list-checker-templates')
      .then((reply) => (reply.ok && Array.isArray(reply.data.templates) ? (reply.data.templates as CheckerTemplate[]) : []))
      .catch((error) => {
        console.error('Failed to load checker templates:', error);
        return [] as CheckerTemplate[];
      })
      .then((templates) => {
        templatesCache = templates;
        return templates;
      });
  }
  return templatesRequest;
}

type SaveState = 'idle' | 'saving' | 'saved' | 'failed';

// The quiz editor pop-up (#quiz-editor-modal): the wizard for one quiz file.
// "Save" closes it, "Save and continue" keeps it open (a new quiz then
// becomes an edited one).
export function QuizEditor({
  start,
  onClose,
  onListChanged,
}: {
  start: EditorStart;
  onClose: () => void;
  onListChanged: () => void;
}) {
  const showMessage = useShowMessage();
  const [mode, setMode] = useState(start.mode);
  const [editingName, setEditingName] = useState(start.editingName);
  const [filename, setFilename] = useState(start.filename);
  const [draft, setDraft] = useState<QuizDraft>(start.draft);
  const [saveState, setSaveState] = useState<SaveState>('idle');
  const [templates, setTemplates] = useState<CheckerTemplate[]>(templatesCache || []);
  const [previews, setPreviews] = useState<Record<number, Preview>>({});
  const [imagePicker, setImagePicker] = useState<{ target: ImageTarget; images: ImageEntry[] } | null>(null);
  const [filePicker, setFilePicker] = useState<{ questionKey: number; files: FileEntry[] } | null>(null);
  const [fullscreen, setFullscreen] = useState<string | null>(null);
  const [dragging, setDragging] = useState<number | null>(null);
  const [dragOver, setDragOver] = useState<number | null>(null);
  const [moved, setMoved] = useState<number | null>(null);

  const explanationFields = useRef<Record<number, HTMLTextAreaElement | null>>({});
  const timers = useRef<ReturnType<typeof setTimeout>[]>([]);
  const draggedKey = useRef<number | null>(null);
  // The latest values for the actions below, which are made once
  const draftRef = useRef(draft);
  draftRef.current = draft;
  const previewsRef = useRef(previews);
  previewsRef.current = previews;

  useEffect(
    () => () => {
      timers.current.forEach((timer) => clearTimeout(timer));
    },
    [],
  );

  const later = useCallback((action: () => void, ms: number) => {
    timers.current.push(setTimeout(action, ms));
  }, []);

  // Text questions offer the configured checker templates
  const hasTextQuestion = draft.questions.some((question) => question.type === 'text');
  useEffect(() => {
    if (!hasTextQuestion) return;
    let active = true;
    loadCheckerTemplates().then((list) => {
      if (active) setTemplates(list);
    });
    return () => {
      active = false;
    };
  }, [hasTextQuestion]);

  const setQuestions = useCallback((change: (questions: QuestionDraft[]) => QuestionDraft[]) => {
    setDraft((current) => ({ ...current, questions: change(current.questions) }));
  }, []);

  const changeQuestion = useCallback(
    (key: number, change: (question: QuestionDraft) => QuestionDraft) => {
      setQuestions((questions) => questions.map((question) => (question.key === key ? change(question) : question)));
    },
    [setQuestions],
  );

  const focusExplanation = useCallback((key: number, caret?: number) => {
    // after React has drawn the change
    setTimeout(() => {
      const field = explanationFields.current[key];
      if (!field) return;
      if (caret !== undefined) {
        field.selectionStart = caret;
        field.selectionEnd = caret;
      }
      field.focus();
    }, 0);
  }, []);

  const openImagePicker = useCallback(
    async (target: ImageTarget) => {
      try {
        const reply = await get('/api/admin/list-images');
        if (reply.ok) {
          setImagePicker({ target, images: Array.isArray(reply.data.images) ? reply.data.images : [] });
        } else {
          showMessage(reply.data.error || T.loadImagesFailed, 'error');
        }
      } catch (error) {
        showMessage(T.loadImagesError(errorText(error)), 'error');
      }
    },
    [showMessage],
  );

  const openFilePicker = useCallback(
    async (questionKey: number) => {
      try {
        const reply = await get('/api/admin/list-files');
        if (reply.ok) {
          setFilePicker({ questionKey, files: Array.isArray(reply.data.files) ? reply.data.files : [] });
        } else {
          showMessage(reply.data.error || T.loadFilesFailed, 'error');
        }
      } catch (error) {
        showMessage(T.loadFilesError(errorText(error)), 'error');
      }
    },
    [showMessage],
  );

  const togglePreview = useCallback(
    async (key: number) => {
      if (previewsRef.current[key] !== undefined) {
        setPreviews((current) => {
          const next = { ...current };
          delete next[key];
          return next;
        });
        focusExplanation(key);
        return;
      }
      const question = draftRef.current.questions.find((item) => item.key === key);
      const markdown = question ? question.explanation : '';
      let preview: Preview = { empty: true };
      if (markdown.trim() !== '') {
        try {
          const reply = await post('/api/admin/preview-explanation', { markdown });
          if (!reply.ok) {
            showMessage(reply.data.error || T.previewFailed, 'error');
            return;
          }
          preview = { html: String(reply.data.html) };
        } catch (error) {
          showMessage(T.previewError(errorText(error)), 'error');
          return;
        }
      }
      setPreviews((current) => ({ ...current, [key]: preview }));
    },
    [focusExplanation, showMessage],
  );

  const actions = useMemo<QuestionActions>(
    () => ({
      update: (key, patch) => changeQuestion(key, (question) => ({ ...question, ...patch })),
      toggleCollapse: (key) => changeQuestion(key, (question) => ({ ...question, collapsed: !question.collapsed })),
      move: (key, direction) => {
        const questions = draftRef.current.questions;
        const index = questions.findIndex((question) => question.key === key);
        if (index === -1 || index + direction < 0 || index + direction >= questions.length) return;
        setQuestions((list) => {
          const from = list.findIndex((question) => question.key === key);
          const to = from + direction;
          if (from === -1 || to < 0 || to >= list.length) return list;
          const next = list.slice();
          next[from] = list[to];
          next[to] = list[from];
          return next;
        });
        // a short highlight shows where the question went
        setMoved(key);
        later(() => setMoved((current) => (current === key ? null : current)), 650);
      },
      remove: (key) => {
        if (!window.confirm(T.confirmRemoveQuestion)) return;
        setQuestions((questions) => questions.filter((question) => question.key !== key));
      },
      setType: (key, type) =>
        changeQuestion(key, (question) => {
          if (type === 'text') return { ...question, type };
          if (type === 'multiple') return { ...question, type, choiceKind: 'multiple' };
          // Checkboxes become radio buttons with the first option marked, as before
          const options =
            question.choiceKind === 'multiple'
              ? question.options.map((option, index) => ({ ...option, correct: index === 0 }))
              : question.options;
          return { ...question, type, choiceKind: 'single', options };
        }),
      addOption: (key) => changeQuestion(key, (question) => ({ ...question, options: question.options.concat(newOption()) })),
      removeOption: (key, optionKey) => {
        const question = draftRef.current.questions.find((item) => item.key === key);
        if (question && question.options.length <= 2) {
          showMessage(T.twoOptionsNeeded, 'warning');
          return;
        }
        changeQuestion(key, (item) => ({ ...item, options: item.options.filter((option) => option.key !== optionKey) }));
      },
      setOptionText: (key, optionKey, text) =>
        changeQuestion(key, (question) => ({
          ...question,
          options: question.options.map((option) => (option.key === optionKey ? { ...option, text } : option)),
        })),
      setCorrect: (key, optionKey, checked) =>
        changeQuestion(key, (question) => ({
          ...question,
          options: question.options.map((option) => {
            if (question.choiceKind === 'multiple') {
              return option.key === optionKey ? { ...option, correct: checked } : option;
            }
            return { ...option, correct: option.key === optionKey };
          }),
        })),
      openImagePicker: (target) => {
        void openImagePicker(target);
      },
      openFilePicker: (key) => {
        void openFilePicker(key);
      },
      openFullscreen: (src) => {
        if (src) setFullscreen(src);
      },
      toggleExplanation: (key) => {
        const question = draftRef.current.questions.find((item) => item.key === key);
        changeQuestion(key, (item) => ({ ...item, explanationOpen: !item.explanationOpen }));
        if (question && !question.explanationOpen) focusExplanation(key);
      },
      togglePreview: (key) => {
        void togglePreview(key);
      },
      registerExplanation: (key, element) => {
        explanationFields.current[key] = element;
      },
      dragStart: (key, event) => {
        draggedKey.current = key;
        setDragging(key);
        event.dataTransfer.effectAllowed = 'move';
        event.dataTransfer.setData('text/plain', String(key));
      },
      dragEnd: (event: DragEvent<HTMLDivElement>) => {
        event.currentTarget.draggable = false;
        draggedKey.current = null;
        setDragging(null);
        setDragOver(null);
      },
      dragEnter: (key, event) => {
        event.preventDefault();
        if (draggedKey.current !== key) setDragOver(key);
      },
      dragLeave: (key) => setDragOver((current) => (current === key ? null : current)),
      drop: (key, event) => {
        event.preventDefault();
        setDragOver(null);
        const from = draggedKey.current;
        if (from === null || from === key) return;
        setQuestions((questions) => {
          const fromIndex = questions.findIndex((question) => question.key === from);
          const toIndex = questions.findIndex((question) => question.key === key);
          if (fromIndex === -1 || toIndex === -1) return questions;
          // Dropped on a question below it, it goes after that question; above, before it
          const next = questions.slice();
          const [item] = next.splice(fromIndex, 1);
          next.splice(toIndex, 0, item);
          return next;
        });
      },
    }),
    [changeQuestion, setQuestions, later, showMessage, openImagePicker, openFilePicker, togglePreview, focusExplanation],
  );

  const selectImage = (path: string) => {
    if (!imagePicker) return;
    const target = imagePicker.target;
    const question = draftRef.current.questions.find((item) => item.key === target.questionKey);
    if (!question) {
      showMessage(T.fieldNotFound, 'error');
      return;
    }
    if (target.kind === 'explanation') {
      const field = explanationFields.current[question.key];
      const value = question.explanation;
      const start = field ? field.selectionStart : value.length;
      const end = field ? field.selectionEnd : value.length;
      const result = insertAt(value, start, end, explanationImageMarkdown(path));
      changeQuestion(question.key, (item) => ({ ...item, explanation: result.value }));
      focusExplanation(question.key, result.caret);
      setImagePicker(null);
      showMessage(T.imageInserted(path), 'success');
      return;
    }
    if (target.kind === 'question-image') {
      changeQuestion(question.key, (item) => ({ ...item, image: path }));
    } else {
      if (!question.options.some((option) => option.key === target.optionKey)) {
        showMessage(T.fieldNotFound, 'error');
        return;
      }
      actions.setOptionText(question.key, target.optionKey, path);
    }
    setImagePicker(null);
    showMessage(T.imageSelected(path), 'success');
  };

  const selectFile = (path: string) => {
    if (!filePicker) return;
    const name = path.replace(/^\/attach\//, '');
    if (!draftRef.current.questions.some((question) => question.key === filePicker.questionKey)) {
      showMessage(T.questionNotFound, 'error');
      return;
    }
    changeQuestion(filePicker.questionKey, (question) => ({ ...question, file: name }));
    setFilePicker(null);
    showMessage(T.fileSelected(name), 'success');
  };

  const addQuestion = () => setQuestions((questions) => questions.concat(newQuestion()));

  // Save, and close the editor unless keepOpen.
  const save = async (keepOpen: boolean) => {
    const name = filename.trim();
    if (!name) {
      showMessage(T.enterFilename, 'error');
      return;
    }
    const pointsError = validateWizardPoints(draft.questions);
    if (pointsError) {
      showMessage(pointsError, 'error');
      return;
    }
    const quizData = wizardData(draft);
    if ((quizData.questions as unknown[]).length === 0) {
      showMessage(T.addOneQuestion, 'error');
      return;
    }
    if (keepOpen) setSaveState('saving');

    let success = false;
    try {
      const reply =
        mode === 'edit'
          ? await request('PUT', quizURL(editingName), { filename: name, mode: 'wizard', quiz_data: quizData })
          : await request('POST', '/api/admin/create-quiz', { filename: name, mode: 'wizard', quiz_data: quizData });
      if (reply.ok) {
        success = true;
        showMessage(reply.data.message, 'success');
        if (!keepOpen) {
          onClose();
        } else if (mode !== 'edit') {
          // A new quiz is now edited: the next save updates its file
          setMode('edit');
          setEditingName(typeof reply.data.filename === 'string' ? reply.data.filename : name + '.yaml');
        } else if (reply.data.renamed) {
          setEditingName(reply.data.filename);
          setFilename(String(reply.data.filename).replace('.yaml', ''));
        }
        onListChanged();
      } else {
        showMessage(reply.data.error || T.saveFailed, 'error');
      }
    } catch (error) {
      showMessage(T.saveQuizError(errorText(error)), 'error');
    }

    if (keepOpen) {
      setSaveState(success ? 'saved' : 'failed');
      later(() => setSaveState('idle'), 1500);
    }
  };

  const setSetting = (patch: Partial<QuizDraft>) => setDraft((current) => ({ ...current, ...patch }));
  const showExplanationOption = hasAnyExplanation(draft.questions);
  const saveContinueText =
    saveState === 'saving' ? T.saving : saveState === 'saved' ? T.saved : saveState === 'failed' ? T.saveError : T.saveAndContinue;

  return (
    <>
      <div id="quiz-editor-modal" className="modal open">
        <div className="modal-content editor-content-box">
          <div className="modal-header">
            <h2 id="modal-title">{mode === 'edit' ? T.editQuizTitle(editingName) : T.newQuizTitle}</h2>
            <button type="button" className="close" aria-label={T.close} onClick={onClose}>
              {T.close}
            </button>
          </div>

          <div className="form-group">
            <label htmlFor="quiz-filename">{T.filenameLabel}</label>
            <input
              type="text"
              id="quiz-filename"
              placeholder={T.filenamePlaceholder}
              value={filename}
              onChange={(event) => setFilename(event.target.value)}
            />
          </div>

          <div className="editor-content">
            <div id="wizard-form">
              <div className="form-group">
                <label htmlFor="quiz-title">{T.quizTitleLabel}</label>
                <input
                  type="text"
                  id="quiz-title"
                  placeholder={T.quizTitlePlaceholder}
                  value={draft.title}
                  onChange={(event) => setSetting({ title: event.target.value })}
                />
              </div>
              <div className="form-group">
                <label className="checkbox-label">
                  <input
                    type="checkbox"
                    id="show-right-answer"
                    checked={draft.showRightAnswer}
                    onChange={(event) =>
                      // "Show answers after everyone finishes" only matters while answers are hidden
                      setSetting(
                        event.target.checked
                          ? { showRightAnswer: true, showAnswersOnCompletion: false }
                          : { showRightAnswer: false },
                      )
                    }
                  />
                  {T.showRightAnswer}
                </label>
              </div>
              <div
                className="form-group"
                id="show-answers-on-completion-group"
                style={{ display: draft.showRightAnswer ? 'none' : 'block' }}
              >
                <label className="checkbox-label">
                  <input
                    type="checkbox"
                    id="show-answers-on-completion"
                    checked={draft.showAnswersOnCompletion}
                    onChange={(event) => setSetting({ showAnswersOnCompletion: event.target.checked })}
                  />
                  {T.showAnswersOnCompletion}
                </label>
              </div>
              <div className="form-group">
                <label className="checkbox-label">
                  <input
                    type="checkbox"
                    id="randomize-questions"
                    checked={draft.randomizeQuestions}
                    onChange={(event) => setSetting({ randomizeQuestions: event.target.checked })}
                  />
                  {T.randomizeQuestions}
                </label>
              </div>
              <div className="form-group">
                <label className="checkbox-label">
                  <input
                    type="checkbox"
                    id="show-final-list"
                    checked={draft.showFinalList}
                    onChange={(event) => setSetting({ showFinalList: event.target.checked })}
                  />
                  {T.showFinalList}
                </label>
              </div>
              <div
                className="form-group"
                id="show-explanation-group"
                style={{ display: showExplanationOption ? 'block' : 'none' }}
              >
                <label className="checkbox-label">
                  <input
                    type="checkbox"
                    id="show-explanation"
                    checked={draft.showExplanation}
                    onChange={(event) => setSetting({ showExplanation: event.target.checked })}
                  />
                  {T.showExplanation}
                </label>
              </div>
              <div className="form-group">
                <label htmlFor="time-limit-mode">{T.timeLimit}</label>
                <div className="time-limit-row">
                  <select
                    id="time-limit-mode"
                    value={draft.timeLimitMode}
                    onChange={(event) => setSetting({ timeLimitMode: event.target.value as TimeLimitMode })}
                  >
                    <option value="none">{T.timeLimitNone}</option>
                    <option value="per_question">{T.timeLimitPerQuestion}</option>
                    <option value="whole_quiz">{T.timeLimitWholeQuiz}</option>
                  </select>
                  <span
                    id="time-limit-seconds-group"
                    className="time-limit-seconds-group"
                    style={{ display: draft.timeLimitMode === 'none' ? 'none' : 'inline-flex' }}
                  >
                    <label htmlFor="time-limit-seconds">{T.timeLimitSeconds}</label>
                    <input
                      type="number"
                      id="time-limit-seconds"
                      min="0"
                      step="1"
                      value={draft.timeLimitSeconds}
                      onChange={(event) => setSetting({ timeLimitSeconds: event.target.value })}
                    />
                    <small>{T.zeroMeansNoLimit}</small>
                  </span>
                </div>
              </div>
              <div className="form-group">
                <div id="questions-container">
                  {draft.questions.map((question, index) => (
                    <QuestionItem
                      key={question.key}
                      question={question}
                      index={index}
                      count={draft.questions.length}
                      timeLimitMode={draft.timeLimitMode}
                      preview={previews[question.key]}
                      templates={templates}
                      dragging={dragging === question.key}
                      dragOver={dragOver === question.key}
                      moved={moved === question.key}
                      actions={actions}
                    />
                  ))}
                </div>
                <button type="button" id="add-question-btn" onClick={addQuestion}>
                  {T.addQuestion}
                </button>
              </div>
            </div>
          </div>

          <div className="actions-sticky">
            <button type="button" id="save-quiz-btn" onClick={() => void save(false)}>
              {T.saveQuiz}
            </button>
            <button
              type="button"
              id="save-continue-btn"
              className={saveState === 'saved' ? 'save-success' : saveState === 'failed' ? 'save-failed' : undefined}
              disabled={saveState !== 'idle'}
              onClick={() => void save(true)}
            >
              {saveContinueText}
            </button>
            <button type="button" id="cancel-editor-btn" onClick={onClose}>
              {T.cancel}
            </button>
          </div>
        </div>
      </div>

      {imagePicker ? (
        <ImagePicker images={imagePicker.images} onSelect={selectImage} onClose={() => setImagePicker(null)} />
      ) : null}
      {filePicker ? <FilePicker files={filePicker.files} onSelect={selectFile} onClose={() => setFilePicker(null)} /> : null}
      {fullscreen ? <FullscreenImage src={fullscreen} onClose={() => setFullscreen(null)} /> : null}
    </>
  );
}
