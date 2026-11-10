# Test harness: a classroom with real browsers

The harness runs a real WebQuiz server and lets a class of students take a quiz in real browsers at the same time:

- a **scripted teacher** starts the quiz through the start pop-up (with the scenario's settings);
- **scripted students** (no AI) act at exact times, for the timing edges: an answer one second before the limit, an answer that arrives inside or after the 5 s grace because the Wi-Fi was off, a reload, a closed tab;
- **AI agent students** (small models, e.g. Haiku) take the quiz like people would and report, step by step, what they did and what they saw;
- an **AI agent teacher** (in a teacher scenario) uses the admin panel, the file manager and live stats like a teacher would, through `harness/teacher.ts`;
- **automatic students** (no AI) join when the agent teacher runs `students N`, and take whatever quiz runs at that moment.

It is not part of the normal test suite. It lives in `harness/` with its own `package.json`, so `go test` and the Playwright specs in `e2e/` never start it. Only the `harness/*.test.ts` files (the pure functions: checks, element lists, answer lookup, the matrix cases and rules) run, with `npm --prefix harness test`.

The harness also has a **settings matrix** (`matrix.ts`, see [below](#settings-matrix)): scripted students go through many combinations of quiz settings and config, and every screen is checked against what WebQuiz 1.19 shows.

## The three records

Every run keeps three records of the same lesson, and the analysis checks that they tell the same story:

1. **The agent's report.** For each command: `STEP 7 | DID: click 56 | SAW: "✓ Правильна відповідь"`. Facts only, then a few NOTES.
2. **The driver's record** (`events.jsonl`, `shots/`). The driver runs every command, so it records each step itself: start and end time, command, the screen text after it, a screenshot, and every `/api/` request the page sent with the reply. It also records every live stats message, and what each quiz page was told when it loaded (its questions, settings and texts).
3. **The server's data**: the answers and users CSV files, the server log, and the live stats messages.

The step number joins records 1 and 2. The `user_id` and question id in the requests join them to the CSV rows and log lines.

What a disagreement means:

| Disagreement | Meaning |
|---|---|
| Agent's report vs driver's record | The agent misread or misclicked. Noise, not an app bug. |
| Screen vs server data (score, correct/wrong, "time is up") | A real bug. |
| A request was accepted but no answer is stored | A lost answer. |
| An answer is stored but nothing sent it | A phantom answer (only the server's own timeouts are expected). |
| Live stats vs stored answers | A live stats bug. |
| The screen vs the quiz settings (list of questions, hidden answers, explanations, language) | A bug, judged by the rules of WebQuiz 1.19. |
| A phone page wider than the screen | A layout bug (the report gives the screenshot). |

## Running it

```bash
make build                                   # the Go server; gives bin/webquiz (e2e/.bin/webquiz works too)
npm --prefix harness ci                      # Playwright, tsx, Vitest
npx --prefix harness playwright install chromium   # skip if a shared browser is set in PLAYWRIGHT_BROWSERS_PATH

# Scripted students only (no AI):
npx tsx harness/run.ts time-edges --no-agents

# A class of 100 students (scripted only):
npx tsx harness/run.ts big-class --class-size 100 --no-agents
```

`WEBQUIZ_BINARY=/path/to/webquiz` picks another server binary.

With AI agent students:

1. `npx tsx harness/run.ts normal-lesson` and wait for `READY <run_dir>`.
2. Start one agent per name in the scenario's `agents` (see `harness/scenarios.ts`), each with its prompt: `<run_dir>/prompts/<name>.md` is `harness/AGENT_PROMPT.md` with `{name}`, `{persona}` and `<repository folder>` filled in. Agents only run `npx tsx harness/student.ts <name> <command>`.
3. Save each agent's final message as `<run_dir>/agents/<name>.md`, then create the file `<run_dir>/agents_done`.
4. The run waits for the server's own time checks, stops, and writes `<run_dir>/report.md` and `<run_dir>/timelines/<name>.md`.

With an AI agent teacher:

1. `npx tsx harness/run.ts teacher-first-lesson` and wait for `READY <run_dir>`. The server starts with the scenario's config and quiz files; no quiz has been started yet, and no address is trusted, so the teacher logs in with the master key (`harness`).
2. Start one agent with its prompt `<run_dir>/prompts/<name>.md`: `harness/AGENT_TEACHER_PROMPT.md` with `{name}` (the name in the scenario's `teacher_agents`), `{master_key}` (`harness`), `{task}` (the scenario's task list) and `<repository folder>` filled in. The agent only runs `npx tsx harness/teacher.ts <name> <command>`.
3. Save the agent's final message as `<run_dir>/agents/<name>.md`, then create `<run_dir>/agents_done`.
4. The run gives the automatic students up to 150 s more to finish, waits for the server's time checks, stops, and writes `report.md` and the timelines (one per teacher and per student).

To check a run again after adding agent reports: `npx tsx harness/checks.ts <run_dir>`.

Runs are saved in `harness/runs/` (not committed). The server uses port 8199 and the agents' API port 8198, away from the test ports.

## The teacher tool

`npx tsx harness/teacher.ts NAME <command>` (run it without a command for the list). The commands:

| Command | What it does |
|---|---|
| `open admin` / `open files` / `open live-stats` / `open quiz` | Opens that page in the current tab |
| `look` | Shows the screen again |
| `click N` | Clicks element N |
| `fill N "text"` | Puts the text into field N (replaces what is there; `\n` is a new line) |
| `replace N "old" "new"` | Changes one part of the text in field N (for long texts such as YAML) |
| `select N "option"` | Chooses an option by its text or value; several for a list where several can be chosen |
| `check N` / `uncheck N` | Turns a checkbox on or off (check also chooses a radio button) |
| `press KEY` | Presses a key: Enter, Escape, Tab, ... |
| `wait S` | Waits up to 90 s |
| `reload` | Reloads the page |
| `close` | Closes the tab and goes back to the one before |
| `students N` | Starts N automatic students (see below); returns at once |

Every command prints `STEP n`, the time, `PAGE` (the address), `RESULT` (and `ERROR`), `SCREEN` (the visible text) and `ELEMENTS`: a numbered list of everything that can be used, for example:

```
[3] button "💾 Зберегти Quiz"
[7] input "Назва Quiz (необов'язково):" = "Біологія"
[9] checkbox "Показувати правильні відповіді" = on
[12] select "Quiz interface language:" = "English" (options: "Ukrainian (Українська)", "English", "Russian (Русский)")
[14] button "✓ Підтвердити" (at: 👤 auto-02 Клас: 7-В)
```

- A label comes from the element's text, its `<label>`, `aria-label`, placeholder, or the text just before it. A button with only a sign (like `📋`) also shows its title, id or class. When several elements have the same name, `at:` names the row or the question they belong to.
- When a pop-up covers the page, only the pop-up is listed (and the messages above it). A page inside a frame (the CSV table in the file manager's pop-up) is listed too.
- The listed elements are marked with `data-harness-n`, so the numbers are valid until the next command. An element that is gone gives an error that tells the agent to run `look`.
- Dialogs (alert, confirm, prompt) are accepted (a prompt with its suggested text) and shown as `DIALOG:` lines under RESULT. Downloads are saved in `<run_dir>/downloads/` and shown as `DOWNLOAD:` with the name and size. A link or button that opens a new tab moves the teacher there (`NEW TAB:`); `close` goes back.
- The teacher's device is desktop or phone (an iPhone 13), set per teacher in the scenario.

The driver records a teacher step like a student step (a `step` event with the command, arguments, result, screen and elements, a screenshot, and every `/api/` request and reply), so `checks.ts` compares the teacher's report with it.

## Automatic students

`students N` starts N students named `auto-01`, `auto-02`, ... (numbering goes on with the next call). Odd numbers use a desktop browser, even numbers a phone; the timeline shows the device. With no AI they:

- open the quiz page (and look again every few seconds while no quiz runs), fill every registration field with a value that fits its label, and register;
- wait for approval, pressing "Check" every 5-7 s, for up to 3 minutes;
- find each question in the quiz files on disk (`<run_dir>/quizzes`) by its text (or its picture), and answer right about 70% of the time (one answer, several answers, or the text answer's `correct_value`);
- think a random 2-8 s per question, less when a time limit is close;
- click "Continue" and the explanation's "Next" when they show, and stop at the results (when the results say the correct answers come later, they reload up to 3 times, 20 s apart).

Their steps are recorded like other students' steps, without screenshots.

## Scenarios

Student scenarios (scripted teacher, scripted students, AI agent students):

| Name | What it covers |
|---|---|
| `normal-lesson` | A normal lesson: limit changed in the start pop-up, mixed question types, different speeds, a reload, a student who leaves |
| `time-edges` | Answers just before the limit, inside and after the grace, a picked but unsent option, no answer, a reload, a closed tab, a question with no limit |
| `whole-quiz-wifi` | Whole-quiz limit chosen in the pop-up: students finish early, run out of time, lose Wi-Fi when time runs out, close the tab, reload a lot |
| `approval-whole-quiz` | Approval mode with a whole-quiz limit: a scripted teacher approves students in the admin panel at planned times; students press "Check" early, 40 s after approval, or are never approved. Waiting must never use up quiz time |
| `big-class` | A big class at once (40 scripted students, `--class-size N` for more) plus agents, random question order, 30 s per question; some reload, leave, or lose Wi-Fi. The report adds the server's reply times and checks the server's time per answer against the browser's |
| `rich-quiz` | A question picture and picture options, a file to download, explanations, points 2 and 0.5, a several-answers question with `min_correct`, a checker message, registration fields with a disabled one and a username label, `show_answers_on_completion`, 40 s per question. Most scripted students and one agent use phones |
| `english-approval` | English texts, approval, random order with two questions that stick together, no final list, correct answers hidden, 2 minutes for the whole quiz. Most scripted students and one agent use phones |

Teacher scenarios (one AI agent teacher, automatic students on demand; each task takes about 40-60 commands):

| Name | The teacher's task |
|---|---|
| `teacher-first-lesson` | Log in, create a quiz with three question types, start it with 30 s per question, `students 5`, watch live stats, open the answers CSV as a table and filter it to one student |
| `teacher-approval` | Turn on approval and add the field "Клас" in the config form, start "Географія" with 2 minutes for the whole quiz and "show answers after everyone finishes", `students 4`, approve them, show the answers before everyone has finished, look at live stats |
| `teacher-editor` | Add an explanation (bold text and a list) and preview it, 0.5 points, a question that sticks to the previous one, a picture from the image list, an attachment; duplicate and rename, unite two quizzes, delete the copy; start with explanations on, `students 2` |
| `teacher-settings-en` | Switch the language to English and hide a registration field in the config form; break a quiz file's YAML, validate, fix and save it; start it and look at the student page |
| `teacher-phone` | On a phone: log in, start a quiz with 45 s per question, `students 3`, watch live stats, download the answers CSV |

A scenario can add pictures (`files.images`, made in code by `pngs.ts`) and attachments (`files.attachments`) next to its quizzes, and set any config value (`config`).

## What the report checks

For each quiz run that had students (every start or restart of a quiz begins a new run with new CSV files; a teacher scenario can have several):

- each question stored once per student, timed-out answers failed, answers within the limit plus the grace;
- every accepted request stored, and nothing stored that no request sent (except the server's own timeouts);
- the results screen's score, live stats and the server's time per answer agree with the stored answers;
- the planned outcomes of the scripted students;
- the results screen follows the settings: the list of questions only with `show_final_list`, the note "answers later" only while not everyone has finished, the "correct" column only when the reply has the correct answers;
- correct answers stay hidden until every counted student (approved students, in approval mode) has finished, or the teacher pressed the button;
- the users CSV keeps every registration field typed (a disabled field is neither shown nor stored);
- explanations only with `show_right_answer` and `show_explanation`, never after the whole-quiz time ran out, and the explanation step shows;
- a text answer's checker message shows on the screen;
- the quiz page is in the configured language;
- questions with `stick_to_the_previous` stay right after the one before in a random order.

For the whole run: no phone page is wider than its screen (a full-page screenshot `shots/<name>-<step>-wide.png` is kept), and each agent's report (students and teachers) agrees with the driver's record, with the NOTES listed. A teacher scenario's report also lists the teacher's failed commands, downloads and the admin requests the server got, and the quiz runs.

## Settings matrix

`matrix.ts` is a scripted tool (no AI). It takes students through many combinations of quiz settings and config, takes a screenshot of every screen, and checks that each screen shows what WebQuiz 1.19 (the Python version) shows for that combination. It also tours the teacher's pages.

```bash
WEBQUIZ_BINARY=/path/to/webquiz harness/node_modules/.bin/tsx harness/matrix.ts            # everything
WEBQUIZ_BINARY=/path/to/webquiz harness/node_modules/.bin/tsx harness/matrix.ts --quick    # desktop with the light theme only
WEBQUIZ_BINARY=/path/to/webquiz harness/node_modules/.bin/tsx harness/matrix.ts --case pair-03
WEBQUIZ_BINARY=/path/to/webquiz harness/node_modules/.bin/tsx harness/matrix.ts --case teacher   # only the teacher pages
```

Run it from the repository folder. Options: `--case NAME` runs one case (the names are printed at the start), `--quick` runs only desktop with the light theme (and one teacher tour), `--no-teacher` skips the teacher tour, `--jobs N` sets how many runs go at the same time (default 3), `--binary PATH` names the binary (else `WEBQUIZ_BINARY`, then `e2e/.bin/webquiz`, then `bin/webquiz`). Every run starts its own server on a free port (never 8198 or 8199). The full matrix takes about 6 minutes with 3 jobs.

To check the checks themselves, run the student cases against a WebQuiz 1.19 server: `WEBQUIZ_BINARY=<python venv>/bin/webquiz harness/node_modules/.bin/tsx harness/matrix.ts --no-teacher`. The Python CLI takes the same `--config` and `--port` options, and every student check must pass there. (The teacher tour finds its way with the Go pages' element ids, so it is for the Go version only.)

**The combinations.** The factors are `show_right_answer`, `show_explanation`, `show_final_list`, `show_answers_on_completion`, `randomize_questions` (each on or off), the time limit (none, 15 s per question, 60 s for the whole quiz), where the settings come from (the quiz file, or the start settings of `POST /api/admin/switch-quiz`; then the quiz file holds the opposite values, so ignored start settings show up as failures), `registration.approve`, `language` (uk, en) and the registration fields (none, or two fields plus a disabled one and a custom username label). A small greedy all-pairs generator makes the cases `pair-01`, `pair-02`, ...: every pair of values of any two factors is in at least one case. Two fixed cases are added: `defaults` (the 1.19 default of everything: no settings in the quiz file, nothing in the config) and `everything-on`. Each case runs on a desktop with the light theme and on a phone (Playwright's "iPhone 13") with the dark theme; the two fixed cases also run on the other two pairs.

**The quiz** has ten questions (one answer with an explanation; several answers, 2 points; several answers with `min_correct: 2`; a text answer with a checker, 0.5 points; a text answer without a checker; a question image; image options; a file attachment; a question that sticks to the previous one; a question with its own 8 s limit). The images and the attachment are made in code.

**The students.** The main student registers, waits for approval when it is on (the teacher approves through the admin API), and answers by a fixed plan with some right and some wrong answers. In per-question mode it lets question 10 run out of time; in whole-quiz mode it stops at question 8 and lets the quiz time run out. A second student registers at the start and finishes through the API after the main student, so `show_answers_on_completion` first hides the answers and then, after a reload, shows them.

**The checks.** Each rule of 1.19 is a small pure function in `matrix-rules.ts` that names the Python file and line it comes from. The checks cover the registration form and the waiting screen, every question before the answer (progress, points badge, countdowns, hints, images, attachment), the feedback after the answer (or the page moving on by itself), the explanation step, the time-up screen, the results (score, points, table, marks, hints, "answers later" note), the question order of both students, the theme, and no sideways scrolling on the phone. The stored answers and users CSV files must agree with the results screen.

**The teacher tour** (desktop and phone, light and dark, uk and en): admin login, the admin panel, the start pop-up, the quiz editor (one screenshot per question type and the explanation preview), live stats with a finished and an unfinished student, the file manager (CSV files, logs, quizzes, config form and YAML) and the CSV table page. Each page must load without a JS error. Console messages that 1.19 shows too (the 401 of the session check before the login) are listed but marked as expected. Requests to other hosts never leave the machine: the GitHub "latest release" question gets the running version as its answer.

**Output** goes to `harness/runs/<stamp>-matrix/`:

| Path | What it holds |
|---|---|
| `report.md` | Per run, every check with PASS or FAIL, the expected and the actual value and the 1.19 rule; at the top the failed checks and the console and page errors |
| `manifest.json` | One entry per screenshot: `{case, device, theme, language, screen, caption, file, checks?}`, for a gallery page |
| `shots/<case>/<device>-<theme>/NNN-<screen>.jpg` | The screenshots (JPEG, quality 75; desktop at scale 1, the phone at its own scale) |
| `shots/teacher/<device>-<theme>-<language>/` | The teacher tour |
| `servers/<case>-<device>-<theme>/` | Each run's server folder: config, quiz, CSV files, logs, `server.log` |

## Files

| File | What it does |
|---|---|
| `classroom.ts` | The browser driver: one browser session per participant, the student commands, the record |
| `student.ts` | The command-line tool the agent students use |
| `teacher.ts` | The command-line tool the agent teacher uses |
| `teacher-desk.ts` | The driver side of the teacher tool: tabs, dialogs, downloads, the numbered elements |
| `elements.ts` | How the elements of a page are found (in the page) and shown as text; no Playwright needed |
| `auto.ts` | Automatic students (`students N`) |
| `answers.ts` | How automatic students find the right answer in the quiz files |
| `scripted.ts` | Scripted students and the scripted teacher |
| `scenarios.ts` | Scenario quizzes, settings, student plans, agent personas, teacher tasks |
| `pngs.ts` | Small PNG pictures made in code |
| `run.ts` | Runs a scenario end to end |
| `checks.ts` | The checks, the report and the timelines |
| `steps.ts` | How a recorded student step is shown as text (to the agents and in the checks) |
| `AGENT_PROMPT.md` | The instructions for an AI agent student |
| `AGENT_TEACHER_PROMPT.md` | The instructions for an AI agent teacher |
| `matrix.ts` | The settings matrix: plans the runs, runs them, writes `report.md` and `manifest.json` |
| `matrix-cases.ts` | The factors, the all-pairs generator and the cases |
| `matrix-quiz.ts` | The matrix quiz, the students' answers, the images and the attachment |
| `matrix-rules.ts` | What 1.19 shows for a case (pure rule functions with the Python file and line) and the check functions |
| `matrix-texts.ts` | The student page texts of 1.19 (uk and en), copied from `translations.py` |
| `matrix-page.ts` | Server start, admin API, browser sessions, the page reader, screenshots |
| `matrix-student.ts` | One case on one device and theme: the teacher, the main student, the second student, the data checks |
| `matrix-teacher.ts` | The teacher tour |
| `matrix-report.ts` | The run record, `report.md` and `manifest.json` |
| `*.test.ts` | Vitest tests of the pure functions (no server, browser or AI): checks, element lists, answer lookup, matrix cases and rules |

A suspected bug is only a bug once a scripted student or a normal test reproduces it. That test then stays in the test suite (Go or `e2e/`).
