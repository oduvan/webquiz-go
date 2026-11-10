// The quiz editor's data, without React: how a quiz file becomes the editor's
// fields (Python's loadQuizDataToWizard), how the fields become the quiz data
// that is saved (getWizardData), the points check (validateWizardPoints) and
// the start pop-up's settings (openStartQuizModal and getStartSettings).
// Field values are kept as the text of their input, like the old page read
// them from the DOM.

import { T } from './texts';

export type QuestionType = 'single' | 'multiple' | 'text';
export type TimeLimitMode = 'none' | 'per_question' | 'whole_quiz';
type Dict = Record<string, unknown>;

export interface OptionDraft {
  key: number; // stable React key
  text: string;
  correct: boolean; // the option's radio button or checkbox is ticked
}

export interface QuestionDraft {
  key: number; // stable React key, also the data-question-id
  collapsed: boolean;
  focus: boolean; // a new question: its text field gets the focus once it is drawn
  text: string;
  image: string;
  file: string;
  type: QuestionType;
  // The kind of the option inputs (radio buttons or checkboxes). It stays
  // while the question is a text question, as the old inputs did.
  choiceKind: 'single' | 'multiple';
  options: OptionDraft[];
  minCorrect: string;
  defaultValue: string;
  correctValue: string;
  checker: string;
  points: string;
  timeLimit: string;
  explanation: string;
  explanationOpen: boolean; // the explanation field is unfolded
  stick: boolean;
}

export interface QuizDraft {
  title: string;
  showRightAnswer: boolean;
  showAnswersOnCompletion: boolean;
  randomizeQuestions: boolean;
  showFinalList: boolean;
  showExplanation: boolean;
  timeLimitMode: TimeLimitMode;
  timeLimitSeconds: string;
  questions: QuestionDraft[];
}

let lastKey = 0;

export function nextKey(): number {
  lastKey += 1;
  return lastKey;
}

function isDict(value: unknown): value is Dict {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

// Python's page put values into inputs as text. JavaScript's `value || ''`
// is kept where the old page used it, so 0 and false become empty fields.
function text(value: unknown): string {
  return value === null || value === undefined || value === false || value === 0 || value === '' ? '' : String(value);
}

const FLOAT_PATTERN = /^-?(?:\d+(?:\.\d+)?|\.\d+)(?:[eE][+-]?\d+)?$/;

// The value a number input keeps: a browser drops text that is not a valid
// number ("abc", "1.") and shows an empty field.
export function numberValue(value: unknown): string {
  if (value === null || value === undefined) return '';
  const textValue = String(value);
  return FLOAT_PATTERN.test(textValue) ? textValue : '';
}

// Points may be decimals with at most 2 places. Rounding removes binary float
// noise (0.1 + 0.2) and keeps the value a number, so it saves as a YAML number.
export function roundPoints(value: unknown): number {
  return Math.round(Number(value) * 100) / 100;
}

export function newOption(textValue = '', correct = false): OptionDraft {
  return { key: nextKey(), text: textValue, correct };
}

// A question added with "Add question": two empty options, the first one
// marked correct, and sample values for the text question fields.
export function newQuestion(): QuestionDraft {
  return {
    key: nextKey(),
    collapsed: false,
    focus: true,
    text: '',
    image: '',
    file: '',
    type: 'single',
    choiceKind: 'single',
    options: [newOption('', true), newOption('', false)],
    minCorrect: '',
    defaultValue: T.newDefaultValue,
    correctValue: T.newCorrectValue,
    checker: T.newChecker,
    points: '1',
    timeLimit: '',
    explanation: '',
    explanationOpen: false,
    stick: false,
  };
}

// A new quiz: the default settings, no time limit and one empty question.
export function newQuizDraft(): QuizDraft {
  return {
    title: '',
    showRightAnswer: true,
    showAnswersOnCompletion: false,
    randomizeQuestions: false,
    showFinalList: true,
    showExplanation: false,
    timeLimitMode: 'none',
    timeLimitSeconds: '60',
    questions: [newQuestion()],
  };
}

// The time limit of a quiz file: the mode is the first of per_question and
// whole_quiz that the file has, and seconds is its value (60 without a limit).
function timeLimitOf(quizData: Dict): { mode: TimeLimitMode; seconds: string } {
  const limit = typeof quizData.time_limit === 'object' && quizData.time_limit !== null ? (quizData.time_limit as Dict) : {};
  let mode: TimeLimitMode = 'none';
  if (limit.per_question !== undefined) {
    mode = 'per_question';
  } else if (limit.whole_quiz !== undefined) {
    mode = 'whole_quiz';
  }
  return { mode, seconds: mode === 'none' ? '60' : numberValue(limit[mode]) };
}

function questionFromFile(value: unknown): QuestionDraft {
  const question = isDict(value) ? value : {};
  const isText = 'checker' in question;
  const isMultiple = !isText && Array.isArray(question.correct_answer);
  const correctAnswers: unknown[] = isMultiple ? (question.correct_answer as unknown[]) : [question.correct_answer || 0];

  let options: OptionDraft[] = [];
  if (!isText && Array.isArray(question.options)) {
    options = question.options.map((option, index) =>
      newOption(option === null || option === undefined ? '' : String(option), correctAnswers.indexOf(index) !== -1),
    );
  } else if (!isText) {
    options = [newOption('', true), newOption('', false)];
  }

  const explanation = typeof question.explanation === 'string' ? question.explanation : '';
  const timeLimit = question.time_limit;
  return {
    key: nextKey(),
    collapsed: true,
    focus: false,
    text: text(question.question),
    image: text(question.image),
    file: text(question.file),
    type: isText ? 'text' : isMultiple ? 'multiple' : 'single',
    choiceKind: isMultiple ? 'multiple' : 'single',
    options,
    minCorrect: numberValue(question.min_correct || ''),
    defaultValue: text(question.default_value),
    correctValue: text(question.correct_value),
    checker: question.checker === null || question.checker === undefined ? '' : String(question.checker),
    points: numberValue(question.points || 1),
    timeLimit: timeLimit === undefined || timeLimit === null ? '' : numberValue(timeLimit),
    explanation,
    // Folded while empty; a written explanation opens unfolded
    explanationOpen: explanation.trim() !== '',
    stick: Boolean(question.stick_to_the_previous),
  };
}

// The editor fields for a quiz file (the parsed YAML). Every question starts
// collapsed. "Show answers after everyone finishes" is cleared when correct
// answers are shown, as the old page did when it hid that option.
export function draftFromQuiz(parsed: unknown): QuizDraft {
  const quizData = isDict(parsed) ? parsed : {};
  const showRightAnswer = quizData.show_right_answer !== false;
  const { mode, seconds } = timeLimitOf(quizData);
  return {
    title: text(quizData.title),
    showRightAnswer,
    showAnswersOnCompletion: showRightAnswer ? false : quizData.show_answers_on_completion === true,
    randomizeQuestions: quizData.randomize_questions === true,
    showFinalList: quizData.show_final_list !== false,
    showExplanation: quizData.show_explanation === true,
    timeLimitMode: mode,
    timeLimitSeconds: seconds,
    questions: Array.isArray(quizData.questions) ? quizData.questions.map(questionFromFile) : [],
  };
}

export function hasAnyExplanation(questions: QuestionDraft[]): boolean {
  return questions.some((question) => question.explanation.trim() !== '');
}

// What the collapsed header shows next to the question number.
export interface Indicators {
  stick: boolean;
  image: boolean;
  file: boolean;
  points: number | null; // shown with a trophy when it is not 1
}

export function indicatorsOf(question: QuestionDraft): Indicators {
  const points = roundPoints(parseFloat(question.points) || 1);
  return {
    stick: question.stick,
    image: question.image.trim() !== '',
    file: question.file.trim() !== '',
    points: points !== 1 ? points : null,
  };
}

// The text of the explanation fold button.
export function explanationToggleText(question: QuestionDraft): string {
  if (question.explanationOpen) return T.explanationOpen;
  return question.explanation.trim() !== '' ? T.explanationFolded : T.addExplanation;
}

// Checks every question's points field and returns the message for the first
// bad value, or null. Points must be a number greater than 0 with at most 2
// decimal places: the rule the server enforces, checked here so the admin
// gets a message that names the question.
export function validateWizardPoints(questions: QuestionDraft[]): string | null {
  for (let i = 0; i < questions.length; i++) {
    const raw = questions[i].points.trim();
    if (!raw) continue;
    const points = parseFloat(raw);
    if (isNaN(points)) return T.pointsNotNumber(i + 1);
    if (points <= 0) return T.pointsNotPositive(i + 1);
    if (roundPoints(points) !== points) return T.pointsTooManyDecimals(i + 1);
  }
  return null;
}

function questionData(question: QuestionDraft): Dict | null {
  const questionText = question.text.trim();
  const questionImage = question.image.trim();
  const questionFile = question.file.trim();

  // A question needs text or an image; an empty one is left out
  if (!questionText && !questionImage) return null;

  let data: Dict;
  if (question.type === 'text') {
    // A text question is known by its checker or correct value; it has no type field
    data = {};
    if (question.defaultValue) data.default_value = question.defaultValue;
    if (question.correctValue) data.correct_value = question.correctValue;
    if (question.checker.trim()) data.checker = question.checker;
  } else {
    const options = question.options.map((option) => option.text.trim());
    // A choice question with an empty option is left out
    if (!options.every((option) => option) || options.length === 0) return null;
    data = { options };
    if (question.type === 'multiple') {
      const correct: number[] = [];
      question.options.forEach((option, index) => {
        if (option.correct) correct.push(index);
      });
      if (correct.length > 0) {
        data.correct_answer = correct;
        if (question.minCorrect.trim()) {
          const minCorrect = parseInt(question.minCorrect, 10);
          if (minCorrect > 0 && minCorrect <= correct.length) data.min_correct = minCorrect;
        }
      } else {
        data.correct_answer = [0]; // the first option when none is ticked
      }
    } else {
      const index = question.options.findIndex((option) => option.correct);
      data.correct_answer = index === -1 ? 0 : index;
    }
  }

  if (questionText) data.question = questionText;
  if (questionImage) data.image = questionImage;
  if (questionFile) data.file = questionFile;

  // Points only when they differ from the default 1. validateWizardPoints
  // blocks bad values before a save; this guard only keeps them out.
  if (question.points.trim()) {
    const points = roundPoints(parseFloat(question.points));
    if (!isNaN(points) && points > 0 && points !== 1) data.points = points;
  }
  if (question.stick) data.stick_to_the_previous = true;
  if (question.explanation.trim() !== '') data.explanation = question.explanation;
  // The question's own time limit is kept in every mode; it is used in per-question mode
  if (question.timeLimit.trim() !== '') data.time_limit = Number(question.timeLimit);
  return data;
}

// The quiz data the editor saves (the "quiz_data" of create-quiz and of
// PUT /api/admin/quiz/{filename}).
export function wizardData(draft: QuizDraft): Dict {
  const questions: Dict[] = [];
  draft.questions.forEach((question) => {
    const data = questionData(question);
    if (data) questions.push(data);
  });
  const quizData: Dict = { questions };
  const title = draft.title.trim();
  if (title) quizData.title = title;
  quizData.show_right_answer = draft.showRightAnswer;
  quizData.randomize_questions = draft.randomizeQuestions;
  quizData.show_answers_on_completion = draft.showAnswersOnCompletion;
  quizData.show_final_list = draft.showFinalList;
  // show_explanation only while some question has an explanation
  if (hasAnyExplanation(draft.questions)) quizData.show_explanation = draft.showExplanation;
  if (draft.timeLimitMode !== 'none') {
    const seconds = draft.timeLimitSeconds.trim();
    quizData.time_limit = { [draft.timeLimitMode]: seconds === '' ? 0 : Number(seconds) };
  }
  return quizData;
}

// The Markdown for an image in an explanation. Names with spaces or brackets
// need angle brackets in Markdown (the server reads both forms).
export function explanationImageMarkdown(imagePath: string): string {
  const name = imagePath.indexOf('/imgs/') === 0 ? imagePath.substring('/imgs/'.length) : imagePath;
  return /[\s()<>]/.test(name) ? `![](</imgs/${name}>)` : `![](/imgs/${name})`;
}

// Puts `insert` in place of the selection [start, end) and returns the new
// text with the caret after the inserted part.
export function insertAt(value: string, start: number, end: number, insert: string): { value: string; caret: number } {
  return { value: value.slice(0, start) + insert + value.slice(end), caret: start + insert.length };
}

// ---- Start pop-up ----

export interface StartSettingsDraft {
  showRightAnswer: boolean;
  showAnswersOnCompletion: boolean;
  randomizeQuestions: boolean;
  showFinalList: boolean;
  showExplanation: boolean;
  hasExplanations: boolean; // the explanation option is offered only when the quiz has explanations
  timeLimitMode: TimeLimitMode;
  timeLimitSeconds: string;
}

// The pop-up's values come from the quiz file (null when it could not be read).
export function startSettingsFromQuiz(parsed: unknown): StartSettingsDraft {
  const quizData = typeof parsed === 'object' && parsed !== null ? (parsed as Dict) : {};
  const questions = Array.isArray(quizData.questions) ? quizData.questions : [];
  const { mode, seconds } = timeLimitOf(quizData);
  return {
    showRightAnswer: quizData.show_right_answer !== false,
    showAnswersOnCompletion: quizData.show_answers_on_completion === true,
    randomizeQuestions: quizData.randomize_questions === true,
    showFinalList: quizData.show_final_list !== false,
    showExplanation: quizData.show_explanation === true,
    hasExplanations: questions.some(
      (question) =>
        isDict(question) && typeof question.explanation === 'string' && question.explanation.trim() !== '',
    ),
    timeLimitMode: mode,
    timeLimitSeconds: seconds,
  };
}

// The "settings" of POST /api/admin/switch-quiz. time_limit null turns the
// time limit off for this run.
export function startSettingsPayload(settings: StartSettingsDraft): Dict {
  const seconds = settings.timeLimitSeconds.trim();
  const payload: Dict = {
    show_right_answer: settings.showRightAnswer,
    show_answers_on_completion: settings.showAnswersOnCompletion,
    randomize_questions: settings.randomizeQuestions,
    show_final_list: settings.showFinalList,
    time_limit:
      settings.timeLimitMode === 'none' ? null : { [settings.timeLimitMode]: seconds === '' ? 0 : Number(seconds) },
  };
  if (settings.hasExplanations) payload.show_explanation = settings.showExplanation;
  return payload;
}

// ---- Small helpers of the other panels ----

// "моля_парола" -> "Моля Парола": the label of a registration field.
export function fieldNameToLabel(fieldName: string): string {
  return fieldName
    .split('_')
    .map((word) => word.charAt(0).toUpperCase() + word.slice(1))
    .join(' ');
}

export function formatFileSize(bytes: number): string {
  if (bytes === 0) return '0 B';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i];
}
