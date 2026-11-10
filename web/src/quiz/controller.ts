import { fetchWithRetry } from '../shared/api';
import type { ClientQuestion, QuizBoot, Translations } from '../shared/boot';
import { plain } from '../shared/text';
import {
  applyQuestionOrder,
  fillText,
  formatTimer,
  generateRequestToken,
  progressPercent,
  quizImageUrls,
} from './format';
import { getCookie, loadRegistrationData, saveRegistrationData, setCookie } from './storage';
import type {
  ClientTestResult,
  FeedbackClass,
  FinalResults,
  MessageView,
  QuestionView,
  ResultsContent,
  Verdict,
  ViewState,
} from './types';

// The quiz page's logic: the flows of the Python version's page script
// (templates/index.html), step by step, with the same requests and timings.
// The controller keeps the page state; the React components only draw it and
// call the methods below. Comments name the old functions where it helps.

type Timer = ReturnType<typeof setTimeout>;
type MessageKind = 'registration' | 'waiting' | 'test';

const JSON_HEADERS = { 'Content-Type': 'application/json' };

function delay(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

// The text of a caught error, like `error.message` in the old page.
function messageOf(error: unknown): string {
  return String(error ? (error as { message?: unknown }).message : error);
}

// Reply fields the page reads. Values come from JSON, so they are checked where used.
type Reply = Record<string, any>;

export class QuizController {
  readonly boot: QuizBoot;
  readonly T: Translations;

  private state: ViewState;
  private listeners: Array<() => void> = [];
  private drawCount = 0;

  // The old page's variables.
  private questions: ClientQuestion[];
  private currentUserId: string | null = null;
  private currentQuestionIndex = 0;
  private selectedAnswer: number | null = null;
  private selectedAnswers: number[] = []; // in click order
  private textAnswer = '';
  private testResults: ClientTestResult[] = [];
  private answerLocked = false; // the answer is being sent and must not change
  private questionCountdown: { questionId: number; deadline: number; intervalId: Timer | null } | null = null;
  private quizCountdown: { deadline: number; intervalId: Timer | null } | null = null;
  private quizTimeUpStarted = false; // the whole-quiz time ran out and the quiz is being closed
  private pendingExplanation: { question: string; html: string } | null = null; // shown after "Continue"
  // The answer waits for this request, so the server always gets them in order.
  private questionStartRequest: Promise<void> = Promise.resolve();
  private checkingApproval = false; // one check at a time

  constructor(boot: QuizBoot) {
    this.boot = boot;
    this.T = boot.translations;
    this.questions = boot.questions.slice();
    const fields: Record<string, string> = {};
    boot.registration_fields.forEach((field) => (fields[field.name] = ''));
    this.state = {
      loading: true,
      registration: { visible: false, username: '', fields, error: null, busy: false },
      waiting: { visible: false, username: '', fields: { ...fields }, message: null },
      username: null,
      test: {
        visible: false,
        quizTimer: { visible: false, text: '', warning: false },
        progress: { percent: 0, text: '' },
        error: null,
        containerHidden: false,
        question: null,
        submit: { hidden: false, disabled: true },
        continueButton: { hidden: true, disabled: false },
        verdict: null,
        explanation: { visible: false, question: '', html: '', nextDisabled: false },
      },
      results: { visible: false, content: null },
    };
  }

  // ---- Store for React (useSyncExternalStore) ----

  subscribe = (listener: () => void) => {
    this.listeners.push(listener);
    return () => {
      this.listeners = this.listeners.filter((item) => item !== listener);
    };
  };

  getSnapshot = (): ViewState => this.state;

  private update(change: (state: ViewState) => ViewState) {
    this.state = change(this.state);
    this.listeners.slice().forEach((listener) => listener());
  }

  private setTest(patch: Partial<ViewState['test']>) {
    this.update((state) => ({ ...state, test: { ...state.test, ...patch } }));
  }

  private setQuestion(patch: Partial<QuestionView>) {
    this.update((state) =>
      state.test.question ? { ...state, test: { ...state.test, question: { ...state.test.question, ...patch } } } : state,
    );
  }

  private setSubmit(patch: Partial<ViewState['test']['submit']>) {
    this.setTest({ submit: { ...this.state.test.submit, ...patch } });
  }

  private setContinue(patch: Partial<ViewState['test']['continueButton']>) {
    this.setTest({ continueButton: { ...this.state.test.continueButton, ...patch } });
  }

  private setRegistration(patch: Partial<ViewState['registration']>) {
    this.update((state) => ({ ...state, registration: { ...state.registration, ...patch } }));
  }

  private setWaiting(patch: Partial<ViewState['waiting']>) {
    this.update((state) => ({ ...state, waiting: { ...state.waiting, ...patch } }));
  }

  // ---- Start ----

  start() {
    this.preloadQuizImages(); // while the student fills in the registration form
    void this.checkStoredUserId();
  }

  // All quiz images are loaded at once, so they show without delay during the quiz.
  private preloadQuizImages() {
    const urls = quizImageUrls(this.questions);
    urls.forEach((url) => {
      const image = new Image();
      image.src = url;
    });
    console.log('Preloading ' + urls.length + ' quiz images');
  }

  // A stored user id continues the quiz after a reload; without it the form is shown.
  private async checkStoredUserId() {
    const storedUserId = getCookie('user_id');
    if (!storedUserId) {
      this.hideLoadingShowRegistration();
      return;
    }
    try {
      const response = await fetchWithRetry('api/verify-user/' + storedUserId);
      const data: Reply = await response.json();
      if (response.ok && data.valid) {
        this.currentUserId = data.user_id;
        if (data.question_order) {
          this.questions = applyQuestionOrder(this.questions, data.question_order);
        }
        this.showUsername(data.username);
        if (data.requires_approval && !data.approved) {
          this.update((state) => ({ ...state, loading: false }));
          if (data.user_data) {
            this.showWaitingForApproval(data.user_data);
          }
        } else if (data.test_completed && data.final_results) {
          this.showCompletedTestResults(data.final_results);
        } else {
          this.currentQuestionIndex = data.next_question_index;
          this.update((state) => ({ ...state, loading: false }));
          await this.startTest();
          await this.notifyQuestionStart(this.questions[this.currentQuestionIndex].id);
        }
      } else {
        this.hideLoadingShowRegistration();
      }
    } catch (error) {
      this.hideLoadingShowRegistration();
    }
  }

  private hideLoadingShowRegistration() {
    const saved = loadRegistrationData();
    this.update((state) => {
      const registration = { ...state.registration, visible: true, fields: { ...state.registration.fields } };
      if (saved) {
        if (saved.username) registration.username = plain(saved.username);
        this.boot.registration_fields.forEach((field) => {
          if (saved[field.name]) registration.fields[field.name] = plain(saved[field.name]);
        });
      }
      return { ...state, loading: false, registration };
    });
  }

  private showUsername(username: unknown) {
    this.update((state) => ({ ...state, username: plain(username) }));
  }

  // ---- Messages ----

  private message(kind: MessageKind): MessageView | null {
    if (kind === 'registration') return this.state.registration.error ? { text: this.state.registration.error, success: false } : null;
    if (kind === 'test') return this.state.test.error ? { text: this.state.test.error, success: false } : null;
    return this.state.waiting.message;
  }

  private setMessage(kind: MessageKind, message: MessageView | null) {
    if (kind === 'registration') this.setRegistration({ error: message ? message.text : null });
    else if (kind === 'test') this.setTest({ error: message ? message.text : null });
    else this.setWaiting({ message });
  }

  // An error shows for 5 seconds.
  private showError(kind: MessageKind, text: string) {
    this.setMessage(kind, { text, success: false });
    setTimeout(() => this.hideError(kind), 5000);
  }

  private hideError(kind: MessageKind) {
    if (this.message(kind)) this.setMessage(kind, null);
  }

  // ---- Registration ----

  setRegistrationUsername = (value: string) => this.setRegistration({ username: value });

  setRegistrationField = (name: string, value: string) =>
    this.setRegistration({ fields: { ...this.state.registration.fields, [name]: value } });

  registerUser = async () => {
    if (this.state.registration.busy) return;
    const username = this.state.registration.username.trim();
    if (!username) {
      this.showError('registration', this.T.please_enter_username);
      return;
    }
    const registrationData: Record<string, string> = { username };
    for (const field of this.boot.registration_fields) {
      const value = (this.state.registration.fields[field.name] || '').trim();
      // The old page took the label from the form ("Label:") and removed the first ":".
      const label = (field.label + ':').replace(':', '').trim();
      if (!value) {
        this.showError('registration', fillText(this.T.please_fill_field, { field: label }));
        return;
      }
      registrationData[field.name] = value;
    }
    this.hideError('registration');

    // The button stays disabled while the request is on its way, so it is not sent twice.
    this.setRegistration({ busy: true });
    try {
      const response = await fetchWithRetry('api/register', {
        method: 'POST',
        headers: JSON_HEADERS,
        body: JSON.stringify({ ...registrationData, registration_token: generateRequestToken() }),
      });
      const data: Reply = await response.json();
      if (response.ok) {
        this.currentUserId = data.user_id;
        setCookie('user_id', String(data.user_id));
        saveRegistrationData(registrationData);
        this.showUsername(data.username);
        if (data.question_order) {
          this.questions = applyQuestionOrder(this.questions, data.question_order);
        }
        if (data.requires_approval && !data.approved) {
          this.setRegistration({ visible: false });
          this.showWaitingForApproval(registrationData);
        } else {
          this.currentQuestionIndex = 0; // a new student starts from the beginning
          this.setRegistration({ visible: false });
          await this.startTest();
          await this.notifyQuestionStart(this.questions[this.currentQuestionIndex].id);
        }
      } else {
        this.showError('registration', data.error || this.T.registration_error);
      }
    } catch (error) {
      this.showError('registration', this.T.error_prefix + messageOf(error));
    } finally {
      this.setRegistration({ busy: false });
    }
  };

  // ---- Waiting for approval ----

  private showWaitingForApproval(registrationData: Record<string, unknown>) {
    const fields: Record<string, string> = {};
    this.boot.registration_fields.forEach((field) => {
      fields[field.name] = plain(registrationData[field.name] || '');
    });
    this.setWaiting({ visible: true, username: plain(registrationData.username), fields });
  }

  setWaitingUsername = (value: string) => this.setWaiting({ username: value });

  setWaitingField = (name: string, value: string) =>
    this.setWaiting({ fields: { ...this.state.waiting.fields, [name]: value } });

  updateRegistrationData = async () => {
    const username = this.state.waiting.username.trim();
    if (!username) {
      this.showError('waiting', this.T.please_enter_username);
      return;
    }
    const updatedData: Record<string, unknown> = { user_id: this.currentUserId, username };
    this.boot.registration_fields.forEach((field) => {
      updatedData[field.name] = (this.state.waiting.fields[field.name] || '').trim();
    });
    this.hideError('waiting');
    try {
      const response = await fetchWithRetry('api/update-registration', {
        method: 'PUT',
        headers: JSON_HEADERS,
        body: JSON.stringify(updatedData),
      });
      const data: Reply = await response.json();
      if (response.ok) {
        // The success message shows for 3 seconds
        this.setMessage('waiting', { text: this.T.data_updated, success: true });
        setTimeout(() => this.hideError('waiting'), 3000);
        this.showUsername(updatedData.username);
        saveRegistrationData(updatedData);
      } else {
        this.showError('waiting', data.error || this.T.update_error);
      }
    } catch (error) {
      this.showError('waiting', this.T.error_prefix + messageOf(error));
    }
  };

  checkApprovalStatus = async () => {
    if (!this.currentUserId) {
      this.showError('waiting', 'User ID not found'); // the old page's text; the page always knows the id here
      return;
    }
    // One check at a time: while the connection is down, more clicks would start the quiz several times
    if (this.checkingApproval) return;
    this.checkingApproval = true;
    this.hideError('waiting');
    try {
      const response = await fetchWithRetry('api/verify-user/' + this.currentUserId);
      const data: Reply = await response.json();
      if (response.ok && data.valid) {
        if (data.approved) {
          if (data.question_order) {
            this.questions = applyQuestionOrder(this.questions, data.question_order);
          }
          this.setWaiting({ visible: false });
          this.currentQuestionIndex = 0;
          await this.startTest();
          await this.notifyQuestionStart(this.questions[this.currentQuestionIndex].id);
        } else {
          this.showError('waiting', this.T.not_approved_yet);
        }
      } else {
        this.showError('waiting', data.message || this.T.status_check_error);
      }
    } catch (error) {
      this.showError('waiting', this.T.error_prefix + messageOf(error));
    } finally {
      this.checkingApproval = false;
    }
  };

  // ---- Questions ----

  // Tells the server that a question is shown: its clock starts, and live stats show "think".
  // The reply has the remaining time, so a reload does not restart a countdown.
  private notifyQuestionStart(questionId: number): Promise<void> {
    this.questionStartRequest = fetchWithRetry('api/question-start', {
      method: 'POST',
      headers: JSON_HEADERS,
      body: JSON.stringify({ user_id: this.currentUserId, question_id: questionId }),
    })
      .then(async (response) => {
        if (response.ok) {
          this.applyRemainingTimes(questionId, await response.json());
        }
      })
      .catch((error) => {
        console.log('Failed to notify question start:', error);
      });
    return this.questionStartRequest;
  }

  private async startTest() {
    this.selectedAnswer = null;
    this.testResults = [];
    this.setTest({ visible: true });
    await this.showCurrentQuestion(false); // the first question has no animation
  }

  private async showCurrentQuestion(withAnimation = true) {
    if (this.currentQuestionIndex >= this.questions.length) {
      // The quiz is done: the results come from the server, the same as after a reload
      this.pendingExplanation = null;
      this.hideExplanationStep();
      this.setTest({ verdict: null });
      await this.fetchAndShowFinalResults();
      return;
    }

    const question = this.questions[this.currentQuestionIndex];
    const isText = question.type === 'text';
    this.selectedAnswer = null;
    this.selectedAnswers = [];
    this.textAnswer = question.type === 'text' ? plain(question.default_value || '') : '';

    this.drawCount++;
    this.setTest({
      progress: {
        percent: progressPercent(this.currentQuestionIndex, this.questions.length),
        text: fillText(this.T.question_n_of_m, { n: this.currentQuestionIndex + 1, m: this.questions.length }),
      },
      question: {
        key: this.drawCount,
        question,
        animation: withAnimation ? 'question-fade-in' : '',
        selected: [],
        feedback: {},
        optionsDisabled: false,
        defaultText: question.type === 'text' ? plain(question.default_value) : '',
        textDisabled: false,
        textFeedback: '',
        checkerError: null,
        correctValue: null,
        timeUp: false,
        timer: { text: '', warning: false, pending: true },
      },
      // A text question can be sent at once when it has a starting text
      submit: { hidden: false, disabled: isText ? !this.textAnswer.trim() : true },
      continueButton: { ...this.state.test.continueButton, hidden: true },
      verdict: null,
    });
  }

  setTextAnswer = (value: string) => {
    this.textAnswer = value;
    this.setSubmit({ disabled: !value.trim() });
  };

  selectCurrentAnswer = (optionIndex: number) => {
    const view = this.state.test.question;
    // No change once the answer is sent or shown
    if (this.answerLocked || !view || view.optionsDisabled) return;
    const question = this.questions[this.currentQuestionIndex];
    if (!question || question.type === 'text') return;

    if (question.is_multiple_choice) {
      if (this.selectedAnswers.indexOf(optionIndex) !== -1) {
        this.selectedAnswers = this.selectedAnswers.filter((index) => index !== optionIndex);
      } else {
        this.selectedAnswers = this.selectedAnswers.concat([optionIndex]);
      }
      this.setQuestion({ selected: this.selectedAnswers.slice() });
      this.setSubmit({ disabled: this.selectedAnswers.length === 0 });
    } else if (this.selectedAnswer === optionIndex) {
      // Clicking the selected option again deselects it
      this.selectedAnswer = null;
      this.setQuestion({ selected: [] });
      this.setSubmit({ disabled: true });
    } else {
      this.selectedAnswer = optionIndex;
      this.setQuestion({ selected: [optionIndex] });
      this.setSubmit({ disabled: false });
    }
  };

  submitClicked = () => {
    if (this.state.test.submit.disabled || this.state.test.submit.hidden) return;
    void this.submitCurrentAnswer();
  };

  // timedOut: the countdown reached zero, so an empty answer is sent ("time ran out").
  private async submitCurrentAnswer(options: { timedOut?: boolean } = {}) {
    const question = this.questions[this.currentQuestionIndex];
    const isText = question.type === 'text';
    const sendingTimeUp = options.timedOut === true;

    if (!sendingTimeUp) {
      if (isText) {
        if (!this.textAnswer.trim()) {
          this.showError('test', this.T.please_enter_answer);
          return;
        }
      } else if (question.is_multiple_choice) {
        if (this.selectedAnswers.length === 0) {
          this.showError('test', this.T.please_select_at_least_one);
          return;
        }
      } else if (this.selectedAnswer === null) {
        this.showError('test', this.T.please_select_answer);
        return;
      }
    }
    this.hideError('test');

    // Disabled at once against double clicks. The answer is locked: a retried
    // request sends the same answer, so it must not change.
    const viewKey = this.state.test.question ? this.state.test.question.key : -1;
    this.setSubmit({ disabled: true });
    this.answerLocked = true;
    if (isText) this.setQuestion({ textDisabled: true });

    // The answer is on its way, so the question countdown is over
    this.stopQuestionCountdown();

    let answerToSubmit: unknown;
    if (sendingTimeUp) {
      answerToSubmit = null;
    } else if (isText) {
      answerToSubmit = this.textAnswer;
    } else if (question.is_multiple_choice) {
      answerToSubmit = this.selectedAnswers.slice().sort((a, b) => a - b);
    } else {
      answerToSubmit = this.selectedAnswer;
    }

    try {
      // The question's clock starts with the question-start request: it goes first
      await this.questionStartRequest;
      const response = await fetchWithRetry('api/submit-answer', {
        method: 'POST',
        headers: JSON_HEADERS,
        body: JSON.stringify({ user_id: this.currentUserId, question_id: question.id, selected_answer: answerToSubmit }),
      });

      if (!response.ok) {
        const errorText = await response.text();
        let errorMessage = errorText;
        try {
          const errorData = JSON.parse(errorText);
          errorMessage = errorData.error || errorText;
        } catch (parseError) {
          // Not JSON: the raw text is the message
        }
        throw new Error(errorMessage);
      }

      const responseText = await response.text();
      let data: Reply;
      try {
        data = JSON.parse(responseText);
      } catch (parseError) {
        console.error('JSON Parse Error:', parseError);
        console.error('Response text:', responseText);
        throw new Error('Invalid JSON response: ' + responseText.substring(0, 100));
      }

      // Time ran out (the page sent an empty answer, or the answer arrived too late):
      // the server stored no answer, so what was selected is dropped
      const timedOut = data.timed_out === true;
      if (timedOut) {
        this.selectedAnswer = null;
        this.selectedAnswers = [];
        const view = this.state.test.question;
        if (view) {
          // The countdown stopped when the answer was sent; time is up, so it shows 0:00
          this.setQuestion({
            selected: [],
            timer: view.question.time_limit > 0 ? { ...view.timer, text: formatTimer(0), warning: true } : view.timer,
          });
        }
      }

      let selectedAnswerDisplay: string;
      if (timedOut) {
        selectedAnswerDisplay = '';
      } else if (isText) {
        selectedAnswerDisplay = this.textAnswer;
      } else if (question.is_multiple_choice) {
        selectedAnswerDisplay = this.selectedAnswers.map((index) => question.options[index]).join(' | ');
      } else {
        selectedAnswerDisplay = plain(question.options[this.selectedAnswer as number]);
      }

      // The explanation step comes after "Continue" (the server sends it only while right answers are shown)
      this.pendingExplanation =
        typeof data.explanation_html === 'string' && data.explanation_html
          ? { question: plain(question.question || ''), html: data.explanation_html }
          : null;

      this.testResults.push({
        questionId: question.id,
        question: question.question,
        image: question.image,
        file: question.file,
        selectedAnswer: selectedAnswerDisplay,
        isCorrect: data.is_correct !== undefined ? data.is_correct : null,
        timeTaken: data.time_taken || 0,
        isMultipleChoice: question.type !== 'text' && question.is_multiple_choice,
        isTextQuestion: isText,
        checkerError: data.checker_error,
        correctValue: data.correct_value,
        timedOut,
      });

      // The whole-quiz time is over: the server closed the quiz, so the results are shown
      if (timedOut && this.boot.quiz_time_limit > 0 && data.test_completed) {
        this.answerLocked = false;
        await this.finishQuizAfterTimeUp();
        return;
      }

      // The verdict next to "Continue" says in words whether the answer counts (new in 2.0).
      // The reply has is_correct only while right answers are shown. A timeout gets no
      // verdict: the question already says "Time is up".
      let verdict: Verdict | null = null;
      if (!timedOut && !sendingTimeUp && data.is_correct !== undefined) {
        verdict = data.is_correct ? 'correct' : 'incorrect';
      }

      this.showAnswerFeedback(data.is_correct, data.correct_answer, data.checker_error, data.correct_value, timedOut, verdict);
    } catch (error) {
      this.showError('test', this.T.submit_error + messageOf(error));
      // The student can send it again
      this.setSubmit({ disabled: false });
      if (isText && this.state.test.question && this.state.test.question.key === viewKey) {
        this.setQuestion({ textDisabled: false });
      }
    } finally {
      this.answerLocked = false;
    }
  }

  private showAnswerFeedback(
    isCorrect: unknown,
    correctAnswer: unknown,
    checkerError: unknown,
    correctValue: unknown,
    timedOut: boolean,
    verdict: Verdict | null,
  ) {
    const question = this.questions[this.currentQuestionIndex];
    const view = this.state.test.question;
    if (!view) return;

    if (timedOut) {
      this.showTimeUpMessage();
    }

    // No correctness in the reply (show_right_answer: false): no feedback, the next question comes by itself
    if (isCorrect === undefined) {
      if (question.type === 'text') {
        this.setQuestion({ textDisabled: true });
      } else {
        this.setQuestion({ optionsDisabled: true });
      }
      this.setSubmit({ hidden: true });
      // A short pause lets the student see the selection (or "Time is up")
      setTimeout(() => {
        void this.continueToNextQuestion(true);
      }, timedOut ? 1500 : 100);
      return;
    }

    if (question.type === 'text') {
      this.setQuestion({
        textDisabled: true,
        textFeedback: isCorrect ? 'feedback-correct' : 'feedback-incorrect',
        checkerError: !isCorrect && checkerError ? plain(checkerError) : null,
        correctValue: !isCorrect && correctValue ? plain(correctValue) : null,
      });
    } else {
      const feedback: Record<number, FeedbackClass[]> = {};
      const add = (index: number, name: FeedbackClass) => {
        feedback[index] = (feedback[index] || []).concat([name]);
      };
      // The option with the id option_<value>, as the old page found it
      const optionIndexOf = (value: unknown): number => {
        for (let i = 0; i < question.options.length; i++) {
          if (String(i) === String(value)) return i;
        }
        return -1;
      };
      const selected = this.state.test.question ? this.state.test.question.selected : [];

      if (question.is_multiple_choice) {
        const correctList: unknown[] = Array.isArray(correctAnswer) ? correctAnswer : [];
        this.selectedAnswers.forEach((index) => {
          add(index, correctList.indexOf(index) !== -1 ? 'feedback-correct' : 'feedback-incorrect');
        });
        // Every right option is shown, also the ones the student missed
        correctList.forEach((value) => {
          if (this.selectedAnswers.indexOf(value as number) === -1) {
            const index = optionIndexOf(value);
            if (index !== -1) add(index, 'feedback-missed');
          }
        });
      } else {
        if (selected.length > 0) {
          add(selected[0], isCorrect ? 'feedback-correct' : 'feedback-incorrect');
        }
        if (!isCorrect && correctAnswer !== undefined && correctAnswer !== null) {
          const index = optionIndexOf(correctAnswer);
          if (index !== -1) add(index, 'feedback-correct');
        }
      }
      this.setQuestion({ feedback, optionsDisabled: true });
    }

    this.setSubmit({ hidden: true });
    this.setContinue({ hidden: false, disabled: false });
    this.setTest({ verdict });
  }

  private showTimeUpMessage() {
    this.setQuestion({ timeUp: true });
  }

  continueClicked = () => {
    if (this.state.test.continueButton.disabled || this.state.test.continueButton.hidden) return;
    void this.continueToNextQuestion(true);
  };

  private async continueToNextQuestion(withAnimation = true) {
    // The answered question has an explanation: it comes before the next question
    if (this.pendingExplanation) {
      this.showExplanationStep();
      return;
    }

    // Disabled against double clicks during the animation
    this.setContinue({ disabled: true });

    // The fade-out keeps the answered question (and the disabled button) under the mouse for a moment
    if (withAnimation && this.state.test.question) {
      this.setQuestion({ animation: 'question-fade-out' });
      await delay(450);
    }

    this.currentQuestionIndex++;
    if (this.currentQuestionIndex >= this.questions.length) {
      await this.showCurrentQuestion(false); // the results, without animation
      return;
    }

    // The server hears about the question only when it is opened
    void this.notifyQuestionStart(this.questions[this.currentQuestionIndex].id);
    await this.showCurrentQuestion(withAnimation);
    if (withAnimation) {
      await delay(525); // the fade-in (500 ms) ends before anything else happens
    }
  }

  // ---- Explanation step ----

  private showExplanationStep() {
    const pending = this.pendingExplanation;
    if (!pending) return;
    this.setTest({
      explanation: { visible: true, question: pending.question, html: pending.html, nextDisabled: false },
      containerHidden: true,
      submit: { ...this.state.test.submit, hidden: true },
      continueButton: { ...this.state.test.continueButton, hidden: true },
      verdict: null,
    });
    window.scrollTo(0, 0);
  }

  private hideExplanationStep() {
    this.setTest({
      explanation: { ...this.state.test.explanation, visible: false },
      containerHidden: false,
    });
  }

  nextAfterExplanation = async () => {
    if (this.state.test.explanation.nextDisabled) return;
    // A double click must not skip a question
    this.setTest({ explanation: { ...this.state.test.explanation, nextDisabled: true } });
    this.pendingExplanation = null;
    // The step (with the disabled button) stays under the mouse for a moment, so the second click
    // of a double click cannot land on the next question's controls
    await delay(400);
    if (this.currentQuestionIndex >= this.questions.length || this.quizTimeUpStarted) {
      return; // the whole-quiz time ran out meanwhile and the results are shown
    }
    // The next question is drawn while the step still covers the answered one
    await this.continueToNextQuestion(false);
    this.hideExplanationStep();
  };

  // ---- Results ----

  private async fetchAndShowFinalResults() {
    this.stopAllCountdowns();
    try {
      // The results come from the server, the same as after a reload
      const response = await fetchWithRetry('api/verify-user/' + this.currentUserId);
      const data: Reply = await response.json();
      if (response.ok && data.valid && data.test_completed && data.final_results) {
        this.showCompletedTestResults(data.final_results);
      } else {
        this.showFinalResults();
      }
    } catch (error) {
      console.error('Error fetching final results from server:', error);
      this.showFinalResults();
    }
  }

  // The page's own results, when the server's cannot be read.
  private showFinalResults() {
    this.showResults({ kind: 'client', results: this.testResults.slice() });
  }

  private showCompletedTestResults(finalResults: FinalResults) {
    this.stopAllCountdowns();
    this.update((state) => ({ ...state, loading: false }));
    this.showResults({ kind: 'server', final: finalResults });
  }

  private showResults(content: ResultsContent) {
    this.update((state) => ({
      ...state,
      test: { ...state.test, visible: false },
      results: { visible: true, content },
    }));
  }

  // ---- Time limit countdowns ----

  private applyRemainingTimes(questionId: number, data: Reply) {
    if (data.remaining_seconds !== undefined) {
      this.startQuestionCountdown(questionId, data.remaining_seconds);
    }
    if (data.quiz_remaining_seconds !== undefined) {
      this.startQuizCountdown(data.quiz_remaining_seconds);
    }
  }

  private startQuestionCountdown(questionId: number, remainingSeconds: number) {
    this.stopQuestionCountdown();
    this.questionCountdown = { questionId, deadline: Date.now() + remainingSeconds * 1000, intervalId: null };
    this.tickQuestionCountdown();
    if (this.questionCountdown) {
      this.questionCountdown.intervalId = setInterval(() => this.tickQuestionCountdown(), 250);
    }
  }

  private stopQuestionCountdown() {
    if (this.questionCountdown && this.questionCountdown.intervalId) {
      clearInterval(this.questionCountdown.intervalId);
    }
    this.questionCountdown = null;
  }

  private tickQuestionCountdown() {
    if (!this.questionCountdown) return;
    const remaining = (this.questionCountdown.deadline - Date.now()) / 1000;
    // Shown on the question on the screen, if it has a timer. The page is drawn
    // again only when the shown time changes (about once a second, not every tick).
    const view = this.state.test.question;
    if (view && view.question.time_limit > 0) {
      const timer = { text: formatTimer(remaining), warning: remaining <= 10, pending: false };
      if (timer.text !== view.timer.text || timer.warning !== view.timer.warning || view.timer.pending) {
        this.setQuestion({ timer });
      }
    }
    if (remaining <= 0) {
      const questionId = this.questionCountdown.questionId;
      this.stopQuestionCountdown();
      this.onQuestionTimeUp(questionId);
    }
  }

  private startQuizCountdown(remainingSeconds: number) {
    if (this.quizCountdown && this.quizCountdown.intervalId) {
      clearInterval(this.quizCountdown.intervalId);
    }
    this.quizCountdown = { deadline: Date.now() + remainingSeconds * 1000, intervalId: null };
    this.setTest({ quizTimer: { ...this.state.test.quizTimer, visible: true } });
    this.tickQuizCountdown();
    if (this.quizCountdown) {
      this.quizCountdown.intervalId = setInterval(() => this.tickQuizCountdown(), 250);
    }
  }

  private stopAllCountdowns() {
    this.stopQuestionCountdown();
    if (this.quizCountdown && this.quizCountdown.intervalId) {
      clearInterval(this.quizCountdown.intervalId);
    }
    this.quizCountdown = null;
    this.setTest({ quizTimer: { ...this.state.test.quizTimer, visible: false } });
  }

  private tickQuizCountdown() {
    if (!this.quizCountdown) return;
    const remaining = (this.quizCountdown.deadline - Date.now()) / 1000;
    const shown = this.state.test.quizTimer;
    const text = formatTimer(remaining);
    const warning = remaining <= 10;
    // Drawn again only when the shown time changes
    if (text !== shown.text || warning !== shown.warning) {
      this.setTest({ quizTimer: { visible: shown.visible, text, warning } });
    }
    if (remaining <= 0) {
      if (this.quizCountdown.intervalId) clearInterval(this.quizCountdown.intervalId);
      this.quizCountdown = null;
      void this.onQuizTimeUp();
    }
  }

  private isQuestionAnswered(questionId: number): boolean {
    return this.testResults.some((result) => result.questionId === questionId);
  }

  // Per-question time is up: an empty answer is sent, unless an answer is already on its way.
  private onQuestionTimeUp(questionId: number) {
    const question = this.questions[this.currentQuestionIndex];
    if (!question || question.id !== questionId || this.answerLocked || this.isQuestionAnswered(questionId)) {
      return;
    }
    void this.submitCurrentAnswer({ timedOut: true });
  }

  // Whole-quiz time is up: an empty answer is sent, and the server closes the whole quiz.
  private async onQuizTimeUp() {
    if (this.quizTimeUpStarted) return;
    this.quizTimeUpStarted = true;
    this.stopQuestionCountdown();
    // Never two answers on their way: wait for an answer that is being sent
    while (this.answerLocked) {
      await delay(100);
    }
    let index = this.currentQuestionIndex;
    // On the "Continue" screen the open question already has an answer: the next one is used
    if (index < this.questions.length && this.isQuestionAnswered(this.questions[index].id)) {
      index++;
    }
    if (index < this.questions.length) {
      // Locked like a normal answer, so the student cannot send a second one
      this.answerLocked = true;
      this.setSubmit({ disabled: true });
      this.setQuestion({ optionsDisabled: true, textDisabled: true });
      try {
        await this.questionStartRequest;
        await fetchWithRetry('api/submit-answer', {
          method: 'POST',
          headers: JSON_HEADERS,
          body: JSON.stringify({ user_id: this.currentUserId, question_id: this.questions[index].id, selected_answer: null }),
        });
      } catch (error) {
        console.log('Failed to send the quiz time-up answer:', error);
      } finally {
        this.answerLocked = false;
      }
    }
    await this.finishQuizAfterTimeUp();
  }

  private async finishQuizAfterTimeUp() {
    this.quizTimeUpStarted = true;
    this.stopAllCountdowns();
    this.currentQuestionIndex = this.questions.length;
    await this.showCurrentQuestion(false); // shows the final results from the server
  }
}
