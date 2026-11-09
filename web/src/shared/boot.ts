const BOOT_MARKER = '__WEBQUIZ_BOOT__';

// Reads the JSON that the Go server puts into <script id="webquiz-boot">.
// In the Vite dev server the script still holds the raw marker, so the page's
// own dev fallback is returned instead.
export function readBoot<T>(devFallback: T): T {
  const el = document.getElementById('webquiz-boot');
  const text = el && el.textContent ? el.textContent.trim() : '';
  if (text === '' || text === BOOT_MARKER) {
    return devFallback;
  }
  return JSON.parse(text) as T;
}

// The boot data of each page. These types mirror the Go structs in
// internal/server/pages.go (QuizBoot, AdminBoot, FilesBoot, LiveStatsBoot,
// CSVTableBoot); change both together. Values marked "as written in the
// quiz file" come straight from YAML: show them with plain(), never as HTML.

export type Translations = Record<string, string>;

// A question on the quiz page, without its answers.
interface QuestionBase {
  id: number;
  points: number; // rounded to 2 decimals, whole numbers without a fraction
  time_limit: number; // effective seconds for this question, 0 = no limit
  question?: unknown; // the question text as written in the quiz file; only when set
  image?: unknown; // image path as written in the quiz file (for example "/imgs/a.png"); only when set
  file?: string; // attachment download path "/attach/<name>"; only when set
}

export interface ChoiceQuestion extends QuestionBase {
  type?: undefined;
  options: unknown[]; // as written in the quiz file; an option that starts with "/" is an image path
  is_multiple_choice: boolean;
  min_correct?: unknown; // only for multiple choice: how many right options are needed
}

export interface TextQuestion extends QuestionBase {
  type: 'text';
  default_value: unknown; // the starting text of the answer box, as written in the quiz file
}

export type ClientQuestion = ChoiceQuestion | TextQuestion;

export interface RegistrationField {
  label: string; // shown to the student
  name: string; // the key of the value in the registration request
}

export interface QuizChoice {
  filename: string;
  title: string | null; // the quiz title as text, null when the file has none
}

// The quiz page (static_dir/index.html, served at "/").
export interface QuizBoot {
  // "quiz_selection_required": several quiz files and no default.yaml; the
  // page lists `quizzes` and asks the admin to choose one in the admin panel.
  state: 'quiz' | 'quiz_selection_required';
  language: 'uk' | 'en';
  translations: Translations;
  version: string;
  title: string;
  questions: ClientQuestion[];
  username_label: string;
  registration_fields: RegistrationField[];
  show_right_answer: boolean;
  show_final_list: boolean;
  quiz_time_limit: number; // whole-quiz seconds, 0 = no limit
  quizzes: QuizChoice[];
}

export interface NetworkURL {
  label: string; // "Network Access (<ip>)"
  quiz_url: string;
}

export interface DownloadableQuiz {
  name: string;
  download_path: string;
  folder: string;
}

// The admin page (/admin/).
export interface AdminBoot {
  is_trusted_ip: boolean; // log in without the master key
  network_info: { urls: NetworkURL[] };
  downloadable_quizzes: DownloadableQuiz[];
  version: string;
  tunnel_public_key: string; // "" without a tunnel
  repo_url: string; // https://github.com/oduvan/webquiz-go: releases and the "new version" check
  pages_url: string; // the online documentation
}

// The values of the config form.
export interface ConfigFormData {
  language: unknown; // as written in the config file ("uk" when missing)
  extra_answers_with_users_csv: boolean;
  registration: Record<string, unknown>; // the "registration" section as written
}

// The file manager (/files/). The config values are null when the server runs
// without a config file; config_data is also null when the file is not valid YAML.
export interface FilesBoot {
  is_trusted_ip: boolean;
  config_content: string | null;
  config_path: string | null;
  config_data: ConfigFormData | null;
}

// The live stats page (/live-stats/): everything comes over its WebSocket.
export type LiveStatsBoot = Record<string, never>;

// The CSV table page (/files/csv/<name>): it reads the file name from its address.
export type CsvTableBoot = Record<string, never>;
