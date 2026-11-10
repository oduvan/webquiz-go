// Automatic students: the teacher's "students N" command starts them.
//
// They take the quiz that runs now, with no AI: open the quiz page, fill every
// registration field, wait for approval (pressing the check button every few
// seconds, up to 3 minutes), and answer every question. They find the right
// answer by matching the question on the screen with the quiz files on disk
// (answers.ts) and answer right about 70% of the time, after a random 2-8 s
// (less when a time limit is close). They click "Continue" and "Next" when
// those show and stop at the results. Their steps are recorded like every
// other student's steps (screenshots off).
//
// auto-01, auto-03, ... use a desktop browser; auto-02, auto-04, ... a phone.

import type { Classroom } from './classroom';
import { sleep } from './classroom';
import {
  baseName,
  chooseAnswer,
  fieldValue,
  findQuestion,
  loadQuizFiles,
  pickRunningQuiz,
  questionsLeft,
  seededRandom,
  seedOf,
  thinkSeconds,
  timerSeconds,
  type Answer,
  type ShownQuestion,
} from './answers';

/** What an automatic student reads from the quiz page. */
export interface AutoState {
  registration: boolean;
  waiting: boolean;
  test: boolean;
  results: boolean;
  answers_later: boolean;
  loading: boolean;
  question: string | null;
  image: string | null;
  options: { index: number; text: string; image: string | null; disabled: boolean }[];
  text_box: boolean;
  can_submit: boolean;
  submit_shown: boolean;
  can_continue: boolean;
  can_next: boolean;
  time_up: boolean;
  question_timer: string | null;
  quiz_timer: string | null;
  progress: string;
  test_error: string | null;
  registration_error: string | null;
  fields: { selector: string; label: string; name: string }[];
  questions: string[];
}

export const AUTO_STATE_JS = String.raw`() => {
  const shown = (el) => !!el && el.getClientRects().length > 0 && getComputedStyle(el).visibility !== 'hidden';
  const byId = (id) => document.getElementById(id);
  const text = (el) => (el ? (el.innerText || '').trim() : '');
  const box = byId('current-question-container');
  const inBox = shown(box);
  const q = inBox ? box.querySelector('.question-text') : null;
  const img = inBox ? box.querySelector('img.question-image') : null;
  const options = inBox ? [...box.querySelectorAll('.option')].map((o, i) => {
    const picture = o.querySelector('img');
    return {
      index: Number(o.getAttribute('data-option-index') || i),
      text: text(o.querySelector('.option-text')),
      image: picture ? picture.getAttribute('src') : null,
      disabled: o.classList.contains('disabled'),
    };
  }) : [];
  const textBox = byId('text-answer-input');
  const submit = byId('submit-answer-btn');
  const cont = byId('continue-btn');
  const next = byId('explanation-next-btn');
  const quizBox = byId('quiz-timer-box');
  const fields = [...document.querySelectorAll('#registration .registration-field')].map((field) => {
    const label = document.querySelector('label[for="' + field.id + '"]');
    return { selector: '#' + field.id, label: text(label).replace(/:$/, ''), name: field.getAttribute('data-field-name') || '' };
  });
  let questions = [];
  try {
    const boot = JSON.parse((byId('webquiz-boot') || {}).textContent || '{}');
    questions = (boot.questions || []).map((item) => (item.question == null ? '' : String(item.question)));
  } catch (e) { /* no boot data */ }
  const errorOf = (id) => { const el = byId(id); return shown(el) && text(el) ? text(el) : null; };
  return {
    registration: shown(byId('registration')),
    waiting: shown(byId('waiting-approval')),
    test: shown(byId('test-section')),
    results: shown(byId('results')),
    answers_later: shown(document.querySelector('#results .answers-later')),
    loading: shown(byId('loading')),
    question: q ? text(q) : (img ? '' : null),
    image: img ? img.getAttribute('src') : null,
    options,
    text_box: shown(textBox) && !textBox.disabled,
    can_submit: shown(submit) && !submit.disabled,
    submit_shown: shown(submit),
    can_continue: shown(cont) && !cont.disabled,
    can_next: shown(byId('explanation-step')) && !!next && !next.disabled,
    time_up: !!(box && box.querySelector('.time-up-message')),
    question_timer: inBox && byId('question-timer') ? byId('question-timer').textContent : null,
    quiz_timer: shown(quizBox) ? (byId('quiz-timer') || {}).textContent || null : null,
    progress: text(byId('progress-text')),
    test_error: errorOf('test-error'),
    registration_error: errorOf('registration-error'),
    fields,
    questions,
  };
}`;

const POLL = 0.3;
/** How long a student waits for the teacher's approval, and for a quiz to start. */
const WAIT_LIMIT = 180;
const CHECK_EVERY = 5;

export interface AutoOptions {
  quizzesDir: string;
  /** Set to true to stop every automatic student. */
  stop: { done: boolean };
  /** A clock() value after which the students stop. */
  deadline: number;
  rightRate?: number;
}

const clock = () => performance.now() / 1000;

function label(answer: Answer, shown: ShownQuestion, index: number): string {
  if (answer.kind === 'text') return answer.value;
  const option = shown.options[index];
  if (!option) return String(index + 1);
  return option.text || (option.image ? `image ${baseName(option.image)}` : String(index + 1));
}

/** The class of automatic students of one run: numbering goes on from call to call. */
export class AutoClass {
  private count = 0;
  readonly running: Promise<void>[] = [];
  readonly names: string[] = [];

  constructor(
    private classroom: Classroom,
    private options: AutoOptions,
  ) {}

  /** Start `count` students at once. Returns their names and devices. */
  start(count: number): { name: string; device: string }[] {
    const started: { name: string; device: string }[] = [];
    for (let i = 0; i < count; i++) {
      this.count += 1;
      const name = `auto-${String(this.count).padStart(2, '0')}`;
      const device = this.count % 2 === 1 ? 'desktop' : 'phone';
      started.push({ name, device });
      this.names.push(name);
      this.running.push(
        autoStudent(this.classroom, name, device, this.count, this.options).catch((error) => {
          this.classroom.log('note', { student: name, detail: `automatic student stopped: ${error instanceof Error ? error.message : error}` });
        }),
      );
    }
    return started;
  }

  /** Wait until every student is done, at most `seconds`. Returns true when all are done. */
  async finish(seconds: number): Promise<boolean> {
    let done = false;
    const all = Promise.all(this.running).then(() => {
      done = true;
    });
    await Promise.race([all, sleep(seconds)]);
    return done;
  }
}

/** One automatic student, from opening the page to the results. */
export async function autoStudent(
  classroom: Classroom,
  name: string,
  device: string,
  number: number,
  options: AutoOptions,
): Promise<void> {
  await classroom.addStudent(name, device, 'auto');
  const random = seededRandom(seedOf(name));
  const quiet = { screenshot: false };
  const say = (detail: string) => classroom.log('note', { student: name, detail });
  const over = () => options.stop.done || clock() > options.deadline;

  await classroom.command(name, 'open', null, quiet);
  const seen = new Set<string>();
  let waitingSince: number | null = null;
  let lastCheck = 0;
  let emptySince: number | null = null;
  let registrations = 0;
  let reloadsForAnswers = 0;
  let lastResultsReload = 0;
  let errors = 0;

  while (!over()) {
    const state = await classroom.evaluate<AutoState>(name, AUTO_STATE_JS);
    if (!state) {
      await sleep(1);
      continue;
    }

    if (state.results) {
      // "The right answers will be shown later": reload a few times, like the page asks
      if (state.answers_later && reloadsForAnswers < 3) {
        if (lastResultsReload === 0) {
          lastResultsReload = clock();
          await classroom.command(name, 'look', null, quiet);
        }
        if (clock() - lastResultsReload >= 20) {
          reloadsForAnswers += 1;
          lastResultsReload = clock();
          await classroom.command(name, 'reload', null, { screenshot: false, settle: 1.0 });
        }
        await sleep(1);
        continue;
      }
      await sleep(1.0);
      await classroom.command(name, 'look', null, quiet);
      say('reached the results');
      return;
    }

    if (state.registration && !state.waiting && !state.test) {
      if (registrations >= 3) {
        say('gave up: could not register');
        await classroom.command(name, 'look', null, quiet);
        return;
      }
      registrations += 1;
      const username = registrations === 1 ? name : `${name}-${registrations}`;
      await classroom.command(name, 'type', username, { screenshot: false, settle: 0.2, selector: '#username' });
      for (const field of state.fields) {
        const value = fieldValue(field.label || field.name, name, number);
        await classroom.command(name, 'type', value, { screenshot: false, settle: 0.1, selector: field.selector });
      }
      await classroom.clickSelector(name, '.register-btn', false);
      await sleep(1);
      waitingSince = null;
      continue;
    }

    if (state.waiting) {
      waitingSince = waitingSince ?? clock();
      if (clock() - waitingSince > WAIT_LIMIT) {
        say('gave up: not approved in 3 minutes');
        await classroom.command(name, 'look', null, quiet);
        return;
      }
      if (clock() - lastCheck >= CHECK_EVERY + random() * 2) {
        lastCheck = clock();
        await classroom.clickSelector(name, '.check-status-btn', false);
      }
      await sleep(POLL);
      continue;
    }

    if (!state.test) {
      // No quiz yet (the teacher has not started one) or still loading: look again later
      emptySince = emptySince ?? clock();
      if (clock() - emptySince > WAIT_LIMIT) {
        say('gave up: no quiz to take');
        await classroom.command(name, 'look', null, quiet);
        return;
      }
      await sleep(3);
      if (!state.loading) await classroom.command(name, 'reload', null, { screenshot: false, settle: 1.0 });
      continue;
    }
    emptySince = null;

    if (state.test_error) {
      // The answer was not taken (for example the teacher restarted the quiz): reload
      errors += 1;
      if (errors > 4) {
        say(`gave up after errors: ${state.test_error}`);
        await classroom.command(name, 'look', null, quiet);
        return;
      }
      await sleep(2);
      await classroom.command(name, 'reload', null, { screenshot: false, settle: 1.0 });
      continue;
    }

    if (state.can_next) {
      await sleep(1 + random() * 2);
      await classroom.clickSelector(name, '#explanation-next-btn', false);
      continue;
    }
    if (state.can_continue) {
      await sleep(0.5 + random());
      await classroom.clickSelector(name, '#continue-btn', false);
      continue;
    }

    const key = `${state.progress}\u0000${state.question}\u0000${state.image}`;
    if (state.question !== null && state.submit_shown && !state.time_up && !seen.has(key)) {
      const shownAt = clock();
      seen.add(key);
      const shown: ShownQuestion = {
        text: state.question,
        image: state.image,
        options: state.options.map((option) => ({ text: option.text, image: option.image })),
        isText: state.options.length === 0,
      };
      const files = loadQuizFiles(options.quizzesDir);
      const running = pickRunningQuiz(files, state.questions);
      const found = findQuestion(files, shown, running);
      const { answer, meant } = chooseAnswer(found, shown, random, options.rightRate ?? 0.7);
      const think = thinkSeconds(
        random,
        timerSeconds(state.question_timer),
        timerSeconds(state.quiz_timer),
        questionsLeft(state.progress),
      );
      say(
        `question "${(state.question || baseName(state.image)).slice(0, 60)}": ` +
          (found ? `found in ${found.file.file}` : 'not found in the quiz files') +
          `, answers ${meant} after ${think} s`,
      );
      const left = shownAt + think - clock();
      if (left > 0) await sleep(left);
      if (over()) break;
      if (answer.kind === 'text') {
        await classroom.command(name, 'type', answer.value, { screenshot: false, settle: 0.2, selector: '#text-answer-input' });
      } else {
        for (const index of answer.indices) {
          await classroom.command(name, 'click', label(answer, shown, index), {
            screenshot: false,
            settle: 0.2,
            selector: `#current-question-container .option[data-option-index="${index}"]`,
          });
        }
      }
      await classroom.clickSelector(name, '#submit-answer-btn', false);
      continue;
    }
    await sleep(POLL);
  }
  await classroom.command(name, 'look', null, quiet);
}
