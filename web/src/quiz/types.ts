import type { ClientQuestion } from '../shared/boot';

// The classes an option gets when the right answers are shown.
export type FeedbackClass = 'feedback-correct' | 'feedback-incorrect' | 'feedback-missed';

// The countdown of a question with a per-question time limit.
export interface QuestionTimer {
  text: string; // "M:SS"
  warning: boolean; // 10 seconds or less are left
  pending: boolean; // no time from the server yet: the timer is not shown
}

// The question on the screen. Every new drawing of a question gets a new key,
// so its elements (and the fade-in animation) start fresh.
export interface QuestionView {
  key: number;
  question: ClientQuestion;
  animation: '' | 'question-fade-in' | 'question-fade-out';
  selected: number[]; // selected option indexes
  feedback: Record<number, FeedbackClass[]>; // option index -> feedback classes
  optionsDisabled: boolean; // answered, or the whole-quiz time ran out
  defaultText: string; // the starting text of the answer box
  textDisabled: boolean;
  textFeedback: '' | 'feedback-correct' | 'feedback-incorrect';
  checkerError: string | null; // shown under a wrong text answer
  correctValue: string | null; // shown under a wrong text answer
  timeUp: boolean; // shows "Time is up"
  timer: QuestionTimer;
}

// A result row of the results screen, as the server sends it (verify-user's final_results).
export interface ServerTestResult {
  question?: unknown;
  image?: unknown;
  file?: unknown;
  selected_answer?: unknown;
  correct_answer?: unknown; // left out while the right answers are hidden
  is_correct?: boolean; // left out while the right answers are hidden
  time_taken?: number;
  points?: number;
  earned_points?: number;
  checker_error?: string;
  timed_out?: boolean;
}

export interface FinalResults {
  test_results: ServerTestResult[];
  correct_count: number;
  total_count: number;
  percentage: number;
  earned_points?: number;
  total_points?: number;
  points_percentage?: number;
  total_time?: number;
  all_completed?: boolean;
  show_answers_on_completion?: boolean;
}

// A result row kept by the page itself. It is shown only when the server's
// results cannot be fetched.
export interface ClientTestResult {
  questionId: number;
  question: unknown;
  image: unknown;
  file: unknown;
  selectedAnswer: string;
  isCorrect: boolean | null; // null while the right answers are hidden
  timeTaken: number;
  isMultipleChoice: boolean;
  isTextQuestion: boolean;
  checkerError: unknown;
  correctValue: unknown;
  timedOut: boolean;
}

export type ResultsContent = { kind: 'server'; final: FinalResults } | { kind: 'client'; results: ClientTestResult[] };

export interface MessageView {
  text: string;
  success: boolean; // green "data updated" message instead of an error
}

export interface ViewState {
  loading: boolean;
  registration: {
    visible: boolean;
    username: string;
    fields: Record<string, string>; // field name -> value
    error: string | null;
    busy: boolean; // the request is on its way: the button is disabled
  };
  waiting: {
    visible: boolean;
    username: string;
    fields: Record<string, string>;
    message: MessageView | null;
  };
  username: string | null; // shown at the top once the student is known
  test: {
    visible: boolean;
    quizTimer: { visible: boolean; text: string; warning: boolean };
    progress: { percent: number; text: string };
    error: string | null;
    containerHidden: boolean; // the explanation step covers the question
    question: QuestionView | null;
    submit: { hidden: boolean; disabled: boolean };
    continueButton: { hidden: boolean; disabled: boolean };
    explanation: { visible: boolean; question: string; html: string; nextDisabled: boolean };
  };
  results: { visible: boolean; content: ResultsContent | null };
}
