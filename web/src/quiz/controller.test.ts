import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ClientQuestion, QuizBoot, Translations } from '../shared/boot';
import { QuizController } from './controller';

// Texts: a few real ones, every other key shows as "[key]".
const T = new Proxy(
  {
    question_n_of_m: 'Question {n} of {m}',
    please_fill_field: 'Fill "{field}"',
    submit_error: 'Error submitting answer: ',
  } as Record<string, string>,
  { get: (texts, key) => (String(key) in texts ? texts[String(key)] : '[' + String(key) + ']') },
) as Translations;

function choice(id: number, extra: Partial<ClientQuestion> = {}): ClientQuestion {
  return { id, points: 1, time_limit: 0, question: 'Q' + id + '?', options: ['A', 'B', 'C'], is_multiple_choice: false, ...extra } as ClientQuestion;
}

function makeBoot(overrides: Partial<QuizBoot> = {}): QuizBoot {
  return {
    state: 'quiz',
    language: 'en',
    translations: T,
    version: 'test',
    title: 'Quiz',
    questions: [choice(1), choice(2, { is_multiple_choice: true, min_correct: 2 })],
    username_label: 'Name',
    registration_fields: [{ label: 'Group', name: 'group' }],
    show_right_answer: true,
    show_final_list: true,
    quiz_time_limit: 0,
    quizzes: [],
    ...overrides,
  };
}

type Reply = { status?: number; json?: unknown; text?: string };
type Handler = (body: Record<string, unknown> | undefined, url: string) => Reply | Promise<Reply>;

// A fake server: the first route whose prefix starts the URL answers.
function fakeServer(routes: Record<string, Handler>) {
  const calls: Array<{ url: string; method: string; body: Record<string, unknown> | undefined }> = [];
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string, init: RequestInit = {}) => {
      const body = init.body ? JSON.parse(String(init.body)) : undefined;
      calls.push({ url, method: init.method || 'GET', body });
      for (const prefix of Object.keys(routes)) {
        if (url.indexOf(prefix) === 0) {
          const reply = await routes[prefix](body, url);
          const text = reply.text !== undefined ? reply.text : JSON.stringify(reply.json === undefined ? {} : reply.json);
          return new Response(text, { status: reply.status || 200 });
        }
      }
      return new Response('{"error": "not found"}', { status: 404 });
    }),
  );
  return calls;
}

const flush = () => vi.advanceTimersByTimeAsync(0);

const REGISTERED = { user_id: '123456', username: 'Anna', approved: true, requires_approval: false };

function clearCookie() {
  document.cookie = 'user_id=; expires=Thu, 01 Jan 1970 00:00:00 GMT; path=/';
}

beforeEach(() => {
  vi.useFakeTimers();
  vi.stubGlobal('scrollTo', vi.fn());
  clearCookie();
  localStorage.clear();
});

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
  clearCookie();
});

// Starts the page and registers Anna of group 10-A.
async function registered(boot: QuizBoot, routes: Record<string, Handler>) {
  const calls = fakeServer({ 'api/register': () => ({ json: REGISTERED }), 'api/question-start': () => ({ json: {} }), ...routes });
  const controller = new QuizController(boot);
  controller.start();
  await flush();
  controller.setRegistrationUsername('Anna');
  controller.setRegistrationField('group', '10-A');
  await controller.registerUser();
  await flush();
  return { controller, calls, state: () => controller.getSnapshot() };
}

describe('registration', () => {
  it('shows the form, filled with the values of last time', async () => {
    localStorage.setItem('webquiz_registration_data', JSON.stringify({ username: 'Anna', group: '10-A' }));
    fakeServer({});
    const controller = new QuizController(makeBoot());
    controller.start();
    await flush();
    const state = controller.getSnapshot();
    expect(state.loading).toBe(false);
    expect(state.registration.visible).toBe(true);
    expect(state.registration.username).toBe('Anna');
    expect(state.registration.fields.group).toBe('10-A');
  });

  it('registers with a token, shows the first question and starts its clock', async () => {
    const { calls, state } = await registered(makeBoot(), {});
    const register = calls.find((call) => call.url === 'api/register')!;
    expect(register.body).toMatchObject({ username: 'Anna', group: '10-A' });
    expect(String(register.body!.registration_token)).toMatch(/^[0-9a-f]{32}$/);
    expect(document.cookie).toContain('user_id=123456');
    expect(state().registration.visible).toBe(false);
    expect(state().test.visible).toBe(true);
    expect(state().test.question!.question.id).toBe(1);
    expect(state().test.progress.text).toBe('Question 1 of 2');
    expect(state().username).toBe('Anna');
    expect(calls.find((call) => call.url === 'api/question-start')!.body).toEqual({ user_id: '123456', question_id: 1 });
    expect(JSON.parse(localStorage.getItem('webquiz_registration_data')!)).toEqual({ username: 'Anna', group: '10-A' });
  });

  it('asks for the name and every field; errors go away after 5 seconds', async () => {
    const calls = fakeServer({});
    const controller = new QuizController(makeBoot());
    controller.start();
    await flush();
    await controller.registerUser();
    expect(controller.getSnapshot().registration.error).toBe('[please_enter_username]');
    controller.setRegistrationUsername('Anna');
    await controller.registerUser();
    expect(controller.getSnapshot().registration.error).toBe('Fill "Group"');
    await vi.advanceTimersByTimeAsync(5000);
    expect(controller.getSnapshot().registration.error).toBeNull();
    expect(calls.filter((call) => call.url === 'api/register')).toHaveLength(0);
  });

  it('shows the server error', async () => {
    fakeServer({ 'api/register': () => ({ status: 400, json: { error: 'Username already exists' } }) });
    const controller = new QuizController(makeBoot({ registration_fields: [] }));
    controller.start();
    await flush();
    controller.setRegistrationUsername('Anna');
    await controller.registerUser();
    expect(controller.getSnapshot().registration.error).toBe('Username already exists');
    expect(controller.getSnapshot().registration.busy).toBe(false);
  });
});

describe('answers', () => {
  it('a single choice: select, deselect, send, then feedback and Continue', async () => {
    const { controller, calls, state } = await registered(makeBoot(), {
      'api/submit-answer': () => ({ json: { is_correct: false, correct_answer: 0, time_taken: 1.5, test_completed: false } }),
    });
    controller.selectCurrentAnswer(0);
    expect(state().test.submit.disabled).toBe(false);
    controller.selectCurrentAnswer(0);
    expect(state().test.question!.selected).toEqual([]);
    expect(state().test.submit.disabled).toBe(true);
    controller.selectCurrentAnswer(1);
    controller.submitClicked();
    expect(state().test.submit.disabled).toBe(true);
    await flush();

    expect(calls.find((call) => call.url === 'api/submit-answer')!.body).toEqual({ user_id: '123456', question_id: 1, selected_answer: 1 });
    const view = state().test.question!;
    expect(view.feedback).toEqual({ 1: ['feedback-incorrect'], 0: ['feedback-correct'] });
    expect(view.optionsDisabled).toBe(true);
    expect(state().test.submit.hidden).toBe(true);
    expect(state().test.continueButton).toEqual({ hidden: false, disabled: false });

    controller.continueClicked();
    expect(state().test.continueButton.disabled).toBe(true);
    expect(state().test.question!.animation).toBe('question-fade-out');
    await vi.advanceTimersByTimeAsync(450);
    expect(state().test.question!.question.id).toBe(2);
    expect(state().test.question!.animation).toBe('question-fade-in');
    expect(calls.filter((call) => call.url === 'api/question-start').map((call) => call.body!.question_id)).toEqual([1, 2]);
  });

  it('a multiple choice answer is sent sorted; missed right options are shown', async () => {
    const boot = makeBoot({ questions: [choice(1, { is_multiple_choice: true, min_correct: 2 })] });
    const { controller, calls, state } = await registered(boot, {
      'api/submit-answer': () => ({ json: { is_correct: false, correct_answer: [0, 1], test_completed: true } }),
    });
    controller.selectCurrentAnswer(2);
    controller.selectCurrentAnswer(0);
    expect(state().test.question!.selected).toEqual([2, 0]);
    controller.submitClicked();
    await flush();
    expect(calls.find((call) => call.url === 'api/submit-answer')!.body!.selected_answer).toEqual([0, 2]);
    expect(state().test.question!.feedback).toEqual({ 2: ['feedback-incorrect'], 0: ['feedback-correct'], 1: ['feedback-missed'] });
  });

  it('the answer cannot change while it is being sent', async () => {
    let answer: (reply: Reply) => void = () => undefined;
    const { controller, state } = await registered(makeBoot(), {
      'api/submit-answer': () => new Promise<Reply>((resolve) => (answer = resolve)),
    });
    controller.selectCurrentAnswer(0);
    controller.submitClicked();
    await flush();
    controller.selectCurrentAnswer(1);
    expect(state().test.question!.selected).toEqual([0]);
    controller.submitClicked(); // a second click does nothing
    answer({ json: { is_correct: true, correct_answer: 0, test_completed: false } });
    await flush();
    expect(state().test.question!.feedback).toEqual({ 0: ['feedback-correct'] });
  });

  it('a refused answer shows the error and can be sent again', async () => {
    const { controller, state } = await registered(makeBoot(), {
      'api/submit-answer': () => ({ status: 409, json: { error: 'Already answered' } }),
    });
    controller.selectCurrentAnswer(0);
    controller.submitClicked();
    await flush();
    expect(state().test.error).toBe('Error submitting answer: Already answered');
    expect(state().test.submit.disabled).toBe(false);
    expect(state().test.question!.optionsDisabled).toBe(false);
  });

  it('without right answers the next question comes by itself after 100 ms', async () => {
    const { controller, state } = await registered(makeBoot({ show_right_answer: false }), {
      'api/submit-answer': () => ({ json: { time_taken: 1, test_completed: false } }),
    });
    controller.selectCurrentAnswer(0);
    controller.submitClicked();
    await flush();
    expect(state().test.question!.optionsDisabled).toBe(true);
    expect(state().test.question!.feedback).toEqual({});
    expect(state().test.submit.hidden).toBe(true);
    expect(state().test.continueButton.hidden).toBe(true);
    await vi.advanceTimersByTimeAsync(99);
    expect(state().test.question!.question.id).toBe(1);
    await vi.advanceTimersByTimeAsync(1 + 450);
    expect(state().test.question!.question.id).toBe(2);
  });

  it('a text answer: the starting text enables submit; a wrong answer shows the checker message', async () => {
    const text = { id: 1, points: 1, time_limit: 0, question: 'Type', type: 'text', default_value: '41' } as ClientQuestion;
    const { controller, calls, state } = await registered(makeBoot({ questions: [text] }), {
      'api/submit-answer': () => ({ json: { is_correct: false, is_text_question: true, correct_value: '42', checker_error: 'Not 42', test_completed: true } }),
    });
    expect(state().test.question!.defaultText).toBe('41');
    expect(state().test.submit.disabled).toBe(false);
    controller.setTextAnswer('  ');
    expect(state().test.submit.disabled).toBe(true);
    controller.setTextAnswer('40');
    controller.submitClicked();
    expect(state().test.question!.textDisabled).toBe(true);
    await flush();
    expect(calls.find((call) => call.url === 'api/submit-answer')!.body!.selected_answer).toBe('40');
    const view = state().test.question!;
    expect(view.textFeedback).toBe('feedback-incorrect');
    expect(view.checkerError).toBe('Not 42');
    expect(view.correctValue).toBe('42');
  });

  it('the last answer shows the results from the server', async () => {
    const final = { test_results: [], correct_count: 2, total_count: 2, percentage: 100 };
    const { controller, state } = await registered(makeBoot({ questions: [choice(1)] }), {
      'api/submit-answer': () => ({ json: { is_correct: true, correct_answer: 0, test_completed: true } }),
      'api/verify-user/': () => ({ json: { valid: true, test_completed: true, final_results: final } }),
    });
    controller.selectCurrentAnswer(0);
    controller.submitClicked();
    await flush();
    controller.continueClicked();
    await vi.advanceTimersByTimeAsync(450);
    expect(state().test.visible).toBe(false);
    expect(state().results.visible).toBe(true);
    expect(state().results.content).toEqual({ kind: 'server', final });
  });

  it("the page's own results are shown when the server's cannot be read", async () => {
    const { controller, state } = await registered(makeBoot({ questions: [choice(1)] }), {
      'api/submit-answer': () => ({ json: { is_correct: true, correct_answer: 0, time_taken: 2, test_completed: true } }),
      'api/verify-user/': () => ({ status: 500, text: 'oops' }),
    });
    controller.selectCurrentAnswer(1);
    controller.submitClicked();
    await flush();
    controller.continueClicked();
    await vi.advanceTimersByTimeAsync(450);
    const content = state().results.content!;
    expect(content.kind).toBe('client');
    expect(content.kind === 'client' && content.results[0]).toMatchObject({ questionId: 1, selectedAnswer: 'B', isCorrect: true, timeTaken: 2 });
  });
});

describe('explanations', () => {
  it('come after Continue; Next moves on once, even when pressed twice', async () => {
    const boot = makeBoot({ questions: [choice(1), choice(2), choice(3)] });
    const { controller, state } = await registered(boot, {
      'api/submit-answer': (body) =>
        ({ json: body!.question_id === 1 ? { is_correct: true, correct_answer: 0, explanation_html: '<p>Because</p>', test_completed: false } : { is_correct: true, correct_answer: 0, test_completed: false } }),
    });
    controller.selectCurrentAnswer(0);
    controller.submitClicked();
    await flush();
    controller.continueClicked();
    const explanation = state().test.explanation;
    expect(explanation).toEqual({ visible: true, question: 'Q1?', html: '<p>Because</p>', nextDisabled: false });
    expect(state().test.containerHidden).toBe(true);
    expect(state().test.continueButton.hidden).toBe(true);

    void controller.nextAfterExplanation();
    void controller.nextAfterExplanation();
    expect(state().test.explanation.nextDisabled).toBe(true);
    await vi.advanceTimersByTimeAsync(399);
    expect(state().test.question!.question.id).toBe(1);
    await vi.advanceTimersByTimeAsync(1);
    expect(state().test.question!.question.id).toBe(2);
    expect(state().test.explanation.visible).toBe(false);
    expect(state().test.containerHidden).toBe(false);
    await vi.advanceTimersByTimeAsync(2000);
    expect(state().test.question!.question.id).toBe(2);
  });
});

describe('time limits', () => {
  it('a question countdown sends an empty answer at zero', async () => {
    const boot = makeBoot({ questions: [choice(1, { time_limit: 5 }), choice(2)] });
    const { calls, state } = await registered(boot, {
      'api/question-start': () => ({ json: { remaining_seconds: 5 } }),
      'api/submit-answer': () => ({ json: { timed_out: true, is_correct: false, correct_answer: 0, test_completed: false } }),
    });
    expect(state().test.question!.timer).toEqual({ text: '0:05', warning: true, pending: false });
    await vi.advanceTimersByTimeAsync(4000);
    expect(state().test.question!.timer.text).toBe('0:01');
    await vi.advanceTimersByTimeAsync(1000);
    await flush();
    expect(calls.find((call) => call.url === 'api/submit-answer')!.body).toEqual({ user_id: '123456', question_id: 1, selected_answer: null });
    const view = state().test.question!;
    expect(view.timeUp).toBe(true);
    expect(view.timer.text).toBe('0:00');
    expect(view.feedback).toEqual({ 0: ['feedback-correct'] });
    expect(state().test.continueButton.hidden).toBe(false);
  });

  it('a timed-out answer without right answers moves on after 1.5 seconds', async () => {
    const boot = makeBoot({ show_right_answer: false, questions: [choice(1, { time_limit: 2 }), choice(2)] });
    const { state } = await registered(boot, {
      'api/question-start': (body) => ({ json: body!.question_id === 1 ? { remaining_seconds: 2 } : {} }),
      'api/submit-answer': () => ({ json: { timed_out: true, test_completed: false } }),
    });
    await vi.advanceTimersByTimeAsync(2000);
    await flush();
    expect(state().test.question!.timeUp).toBe(true);
    await vi.advanceTimersByTimeAsync(1499);
    expect(state().test.question!.question.id).toBe(1);
    await vi.advanceTimersByTimeAsync(1 + 450);
    expect(state().test.question!.question.id).toBe(2);
  });

  it('the whole-quiz countdown closes the quiz at zero', async () => {
    const final = { test_results: [], correct_count: 0, total_count: 2, percentage: 0 };
    const { calls, state } = await registered(makeBoot({ quiz_time_limit: 10 }), {
      'api/question-start': () => ({ json: { quiz_remaining_seconds: 10 } }),
      'api/submit-answer': () => ({ json: { timed_out: true, test_completed: true } }),
      'api/verify-user/': () => ({ json: { valid: true, test_completed: true, final_results: final } }),
    });
    expect(state().test.quizTimer).toEqual({ visible: true, text: '0:10', warning: true });
    await vi.advanceTimersByTimeAsync(10000);
    await flush();
    expect(calls.filter((call) => call.url === 'api/submit-answer').map((call) => call.body)).toEqual([
      { user_id: '123456', question_id: 1, selected_answer: null },
    ]);
    expect(state().results.visible).toBe(true);
    expect(state().test.quizTimer.visible).toBe(false);
  });

  it('whole-quiz time up on the "Continue" screen answers the next question', async () => {
    const final = { test_results: [], correct_count: 1, total_count: 2, percentage: 50 };
    const { controller, calls, state } = await registered(makeBoot({ quiz_time_limit: 10 }), {
      'api/question-start': () => ({ json: { quiz_remaining_seconds: 10 } }),
      'api/submit-answer': (body) => ({ json: body!.selected_answer === null ? { timed_out: true, test_completed: true } : { is_correct: true, correct_answer: 0, test_completed: false } }),
      'api/verify-user/': () => ({ json: { valid: true, test_completed: true, final_results: final } }),
    });
    controller.selectCurrentAnswer(0);
    controller.submitClicked();
    await flush();
    expect(state().test.continueButton.hidden).toBe(false);
    await vi.advanceTimersByTimeAsync(10000);
    await flush();
    const answers = calls.filter((call) => call.url === 'api/submit-answer').map((call) => call.body);
    expect(answers[1]).toEqual({ user_id: '123456', question_id: 2, selected_answer: null });
    expect(state().results.visible).toBe(true);
  });
});

describe('a stored student', () => {
  it('continues at the next question after a reload', async () => {
    document.cookie = 'user_id=123456;path=/';
    const calls = fakeServer({
      'api/verify-user/123456': () => ({ json: { valid: true, user_id: '123456', username: 'Anna', approved: true, next_question_index: 1, test_completed: false } }),
      'api/question-start': () => ({ json: {} }),
    });
    const controller = new QuizController(makeBoot());
    controller.start();
    await flush();
    const state = controller.getSnapshot();
    expect(state.loading).toBe(false);
    expect(state.registration.visible).toBe(false);
    expect(state.test.question!.question.id).toBe(2);
    expect(state.username).toBe('Anna');
    expect(calls.find((call) => call.url === 'api/question-start')!.body!.question_id).toBe(2);
  });

  it('follows the stored random question order', async () => {
    document.cookie = 'user_id=123456;path=/';
    fakeServer({
      'api/verify-user/123456': () => ({ json: { valid: true, user_id: '123456', username: 'Anna', approved: true, next_question_index: 0, test_completed: false, question_order: [2, 1] } }),
      'api/question-start': () => ({ json: {} }),
    });
    const controller = new QuizController(makeBoot());
    controller.start();
    await flush();
    expect(controller.getSnapshot().test.question!.question.id).toBe(2);
  });

  it('who waits for approval sees the waiting form, saves changes and checks', async () => {
    document.cookie = 'user_id=123456;path=/';
    let approved = false;
    const calls = fakeServer({
      'api/verify-user/123456': () => ({
        json: approved
          ? { valid: true, user_id: '123456', username: 'Anna K', approved: true, next_question_index: 0, test_completed: false }
          : { valid: true, user_id: '123456', username: 'Anna', approved: false, requires_approval: true, user_data: { username: 'Anna', group: '10-A' } },
      }),
      'api/update-registration': () => ({ json: { success: true } }),
      'api/question-start': () => ({ json: {} }),
    });
    const controller = new QuizController(makeBoot());
    controller.start();
    await flush();
    const state = () => controller.getSnapshot();
    expect(state().waiting).toMatchObject({ visible: true, username: 'Anna', fields: { group: '10-A' } });
    expect(state().loading).toBe(false);

    controller.setWaitingUsername('Anna K');
    await controller.updateRegistrationData();
    expect(calls.find((call) => call.url === 'api/update-registration')!.body).toEqual({ user_id: '123456', username: 'Anna K', group: '10-A' });
    expect(state().waiting.message).toEqual({ text: '[data_updated]', success: true });
    expect(state().username).toBe('Anna K');
    await vi.advanceTimersByTimeAsync(3000);
    expect(state().waiting.message).toBeNull();

    await controller.checkApprovalStatus();
    expect(state().waiting.message).toEqual({ text: '[not_approved_yet]', success: false });

    approved = true;
    await controller.checkApprovalStatus();
    await flush();
    expect(state().waiting.visible).toBe(false);
    expect(state().test.question!.question.id).toBe(1);
  });

  it('who finished sees the results', async () => {
    document.cookie = 'user_id=123456;path=/';
    const final = { test_results: [], correct_count: 1, total_count: 2, percentage: 50 };
    fakeServer({ 'api/verify-user/123456': () => ({ json: { valid: true, user_id: '123456', username: 'Anna', approved: true, test_completed: true, final_results: final } }) });
    const controller = new QuizController(makeBoot());
    controller.start();
    await flush();
    expect(controller.getSnapshot().results).toEqual({ visible: true, content: { kind: 'server', final } });
    expect(controller.getSnapshot().loading).toBe(false);
  });

  it('who is unknown gets the registration form', async () => {
    document.cookie = 'user_id=999999;path=/';
    fakeServer({ 'api/verify-user/': () => ({ json: { valid: false, message: 'User ID not found' } }) });
    const controller = new QuizController(makeBoot());
    controller.start();
    await flush();
    expect(controller.getSnapshot().registration.visible).toBe(true);
  });
});

describe('image preloading', () => {
  it('loads every question image and image option when the page starts', async () => {
    const sources: string[] = [];
    vi.stubGlobal(
      'Image',
      class {
        set src(value: string) {
          sources.push(value);
        }
      },
    );
    fakeServer({});
    const boot = makeBoot({ questions: [choice(1, { image: '/imgs/q.png' }), choice(2, { options: ['/imgs/a.png', 'B'] })] });
    new QuizController(boot).start();
    expect(sources).toEqual(['imgs/q.png', 'imgs/a.png']);
  });
});
