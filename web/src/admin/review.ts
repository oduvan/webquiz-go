import type { NetworkURL } from '../shared/boot';

// The manual check (manual_check: true) in the admin panel: how many questions
// of a quiz the teacher checks, and where the review page is.

// The review_status of the admin socket ("review" in initial_state).
export interface ReviewStatus {
  has_manual: boolean; // the running quiz has questions the teacher checks
  pending_count: number; // answers that wait for a verdict
  reviewers: number; // open review pages
}

// The questions with manual_check: true in a quiz file (the parsed YAML).
export function manualQuestionCount(parsed: unknown): number {
  const quiz = typeof parsed === 'object' && parsed !== null ? (parsed as Record<string, unknown>) : {};
  const questions = Array.isArray(quiz.questions) ? quiz.questions : [];
  return questions.filter(
    (question) => typeof question === 'object' && question !== null && (question as Record<string, unknown>).manual_check === true,
  ).length;
}

// The addresses of the review page: one per network address (for a phone),
// else this computer's.
export function reviewPageUrls(urls: NetworkURL[], origin: string): string[] {
  const result = urls.map((entry) => {
    const base = entry.quiz_url.endsWith('/') ? entry.quiz_url : entry.quiz_url + '/';
    return base + 'review/';
  });
  if (result.length === 0) result.push(origin + '/review/');
  return result;
}

// The review_status of a socket message, or null when it has none.
export function reviewStatusOf(value: unknown): ReviewStatus | null {
  if (typeof value !== 'object' || value === null) return null;
  const status = value as Record<string, unknown>;
  return {
    has_manual: status.has_manual === true,
    pending_count: Number(status.pending_count) || 0,
    reviewers: Number(status.reviewers) || 0,
  };
}
