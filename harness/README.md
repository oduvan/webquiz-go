# Test harness: a classroom with real browsers

The harness runs a real WebQuiz server and lets a class of students take a quiz in real browsers at the same time:

- a **scripted teacher** starts the quiz through the start pop-up (with the scenario's settings);
- **scripted students** (no AI) act at exact times, for the timing edges: an answer one second before the limit, an answer that arrives inside or after the 5 s grace because the Wi-Fi was off, a reload, a closed tab;
- **AI agent students** (small models, e.g. Haiku) take the quiz like people would and report, step by step, what they did and what they saw.

It is not part of the normal test suite. It lives in `harness/` with its own `package.json`, so `go test` and the Playwright specs in `e2e/` never start it. Only `harness/checks.test.ts` and `harness/matrix.test.ts` (the pure check functions) run, with `npm --prefix harness test`.

The harness also has a **settings matrix** (`matrix.ts`, see [below](#settings-matrix)): scripted students go through many combinations of quiz settings and config, and every screen is checked against what WebQuiz 1.19 shows.

## The three records

Every run keeps three records of the same lesson, and the analysis checks that they tell the same story:

1. **The agent's report.** For each command: `STEP 7 | DID: click 56 | SAW: "✓ Правильна відповідь"`. Facts only.
2. **The driver's record** (`events.jsonl`, `shots/`). The driver runs every command, so it records each step itself: start and end time, command, the screen text after it, a screenshot, and every `/api/` request the page sent with the reply. It also records every live stats message.
3. **The server's data**: the answers CSV, the server log, and the live stats messages.

The step number joins records 1 and 2. The `user_id` and question id in the requests join them to the CSV rows and log lines.

What a disagreement means:

| Disagreement | Meaning |
|---|---|
| Agent's report vs driver's record | The agent misread or misclicked. Noise, not an app bug. |
| Screen vs server data (score, correct/wrong, "time is up") | A real bug. |
| A request was accepted but no answer is stored | A lost answer. |
| An answer is stored but nothing sent it | A phantom answer (only the server's own timeouts are expected). |
| Live stats vs stored answers | A live stats bug. |

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

With AI agent students:

1. `npx tsx harness/run.ts normal-lesson` and wait for `READY <run_dir>`.
2. Start one agent per name in the scenario's `agents` (see `harness/scenarios.ts`), each with `harness/AGENT_PROMPT.md`, `{name}`, `{persona}` and `<repository folder>` filled in. Agents only run `npx tsx harness/student.ts <name> <command>`.
3. Save each agent's final message as `<run_dir>/agents/<name>.md`, then create the file `<run_dir>/agents_done`.
4. The run waits for the server's own time checks, stops, and writes `<run_dir>/report.md` and `<run_dir>/timelines/<student>.md`.

To check a run again after adding agent reports: `npx tsx harness/checks.ts <run_dir>`.

Runs are saved in `harness/runs/` (not committed). The server uses port 8199 and the agents' API port 8198, away from the test ports.

## Scenarios

| Name | What it covers |
|---|---|
| `normal-lesson` | A normal lesson: limit changed in the start pop-up, mixed question types, different speeds, a reload, a student who leaves |
| `time-edges` | Answers just before the limit, inside and after the grace, a picked but unsent option, no answer, a reload, a closed tab, a question with no limit |
| `whole-quiz-wifi` | Whole-quiz limit chosen in the pop-up: students finish early, run out of time, lose Wi-Fi when time runs out, close the tab, reload a lot |
| `approval-whole-quiz` | Approval mode with a whole-quiz limit: a scripted teacher approves students in the admin panel at planned times; students press "Check" early, 40 s after approval, or are never approved. Waiting must never use up quiz time |
| `big-class` | A big class at once (40 scripted students, `--class-size N` for more) plus agents, random question order, 30 s per question; some reload, leave, or lose Wi-Fi. The report adds the server's reply times and checks the server's time per answer against the browser's |

## Settings matrix

`matrix.ts` is a scripted tool (no AI). It takes students through many combinations of quiz settings and config, takes a screenshot of every screen, and checks that each screen shows what WebQuiz 1.19 (the Python version) shows for that combination. It also tours the teacher's pages.

```bash
WEBQUIZ_BINARY=/path/to/webquiz harness/node_modules/.bin/tsx harness/matrix.ts            # everything
WEBQUIZ_BINARY=/path/to/webquiz harness/node_modules/.bin/tsx harness/matrix.ts --quick    # desktop with the light theme only
WEBQUIZ_BINARY=/path/to/webquiz harness/node_modules/.bin/tsx harness/matrix.ts --case pair-03
WEBQUIZ_BINARY=/path/to/webquiz harness/node_modules/.bin/tsx harness/matrix.ts --case teacher   # only the teacher pages
```

Run it from the repository folder. Options: `--case NAME` runs one case (the names are printed at the start), `--quick` runs only desktop with the light theme (and one teacher tour), `--no-teacher` skips the teacher tour, `--jobs N` sets how many runs go at the same time (default 3), `--binary PATH` names the binary (else `WEBQUIZ_BINARY`, then `e2e/.bin/webquiz`, then `bin/webquiz`). Every run starts its own server on a free port (never 8198 or 8199). The full matrix takes about 10 minutes with 3 jobs.

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
| `classroom.ts` | The browser driver: one browser session per student, the commands, the record |
| `student.ts` | The command-line tool the agents use |
| `scripted.ts` | Scripted students and the scripted teacher |
| `scenarios.ts` | Scenario quizzes, start settings, student plans, agent personas |
| `run.ts` | Runs a scenario end to end |
| `checks.ts` | The checks, the report and the timelines |
| `steps.ts` | How a recorded step is shown as text (to the agents and in the checks); needs no Playwright, so the checks' tests run without it |
| `AGENT_PROMPT.md` | The instructions for an AI agent student |
| `checks.test.ts` | Vitest tests of the check functions (no server, browser or AI) |
| `matrix.ts` | The settings matrix: plans the runs, runs them, writes `report.md` and `manifest.json` |
| `matrix-cases.ts` | The factors, the all-pairs generator and the cases |
| `matrix-quiz.ts` | The matrix quiz, the students' answers, the images and the attachment |
| `matrix-rules.ts` | What 1.19 shows for a case (pure rule functions with the Python file and line) and the check functions |
| `matrix-texts.ts` | The student page texts of 1.19 (uk and en), copied from `translations.py` |
| `matrix-page.ts` | Server start, admin API, browser sessions, the page reader, screenshots |
| `matrix-student.ts` | One case on one device and theme: the teacher, the main student, the second student, the data checks |
| `matrix-teacher.ts` | The teacher tour |
| `matrix-report.ts` | The run record, `report.md` and `manifest.json` |
| `matrix.test.ts` | Vitest tests of the all-pairs cases and the rules |

A suspected bug is only a bug once a scripted student or a normal test reproduces it. That test then stays in the test suite (Go or `e2e/`).
