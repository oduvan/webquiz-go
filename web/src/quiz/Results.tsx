import type { Translations } from '../shared/boot';
import { formatPoints } from '../shared/points';
import { Button } from '../shared/ui';
import { answerParts, questionDisplay } from './format';
import type { ClientTestResult, FinalResults, ResultsContent, ServerTestResult } from './types';

// The results screen: the score, the points (when some question is not worth 1 point),
// a note while the right answers wait for the other students, and the list of answers.

function QuestionCell({ question, image, file, T }: { question: unknown; image: unknown; file: unknown; T: Translations }) {
  const display = questionDisplay(question, image, file, T);
  if (display.fallback !== null) return <>{display.fallback}</>;
  return (
    <>
      {display.text}
      {display.image ? (
        <div className="mt-1">
          <img src={display.image} alt="" className="results-question-image" />
        </div>
      ) : null}
      {display.file ? (
        <div className="mt-1">
          <a href={display.file.href} download className="question-file" style={{ margin: 0, padding: '5px 10px', fontSize: 12 }}>
            <span className="question-file-icon">📎</span>
            <span className="question-file-name">{display.file.name}</span>
          </a>
        </div>
      ) : null}
    </>
  );
}

function AnswerText({ answer }: { answer: unknown }) {
  const { multiple, parts } = answerParts(answer);
  const show = (part: { image?: string; text?: string }) =>
    part.image ? <img src={part.image} alt="" className="results-answer-image" /> : part.text;
  if (multiple) {
    return (
      <>
        {parts.map((part, index) => (
          <div key={index} className="my-0.5">
            {show(part)}
          </div>
        ))}
      </>
    );
  }
  return parts.length ? <>{show(parts[0])}</> : null;
}

function answerClass(isCorrect: boolean | null | undefined): string {
  if (isCorrect === true) return 'answer-correct';
  if (isCorrect === false) return 'answer-incorrect';
  return '';
}

function TimeUp({ T }: { T: Translations }) {
  return <span className="time-up-message">⏱ {T.time_is_up}</span>;
}

function Mark({ isCorrect }: { isCorrect: boolean }) {
  return (
    <span className={isCorrect ? 'correct' : 'incorrect'} style={{ fontSize: 18 }}>
      {isCorrect ? '✓' : '✗'}
    </span>
  );
}

// Results from the server (verify-user's final_results), the same after a reload.
function ServerResults({ final, T, showFinalList, showRightAnswer }: { final: FinalResults; T: Translations; showFinalList: boolean; showRightAnswer: boolean }) {
  const results: ServerTestResult[] = final.test_results || [];
  const totalCount = final.total_count;
  const percentage = final.percentage;
  const earnedPoints = final.earned_points || 0;
  const totalPoints = final.total_points || totalCount;
  const pointsPercentage = final.points_percentage || percentage;
  // The right answers wait until every student is done
  const showWaitingMessage = (final.show_answers_on_completion || false) && !(final.all_completed || false);
  const hasCorrectAnswers = results.length > 0 && results[0].is_correct !== undefined;
  const hasCustomPoints = results.some((result) => (result.points || 1) !== 1);

  return (
    <>
      <p>
        <strong>{T.result_label}</strong> {final.correct_count}/{totalCount} ({percentage}%)
      </p>
      {hasCustomPoints ? (
        <p>
          <strong>{T.points_label}</strong> {formatPoints(earnedPoints)}/{formatPoints(totalPoints)} ({pointsPercentage}%)
        </p>
      ) : null}
      {showWaitingMessage ? (
        <div className="answers-later">
          <p className="mb-2">
            <strong>{T.answers_available_later}</strong>
          </p>
          <p className="mb-3 text-sm">{T.reload_later}</p>
          <Button className="reload-btn" onClick={() => window.location.reload()}>
            {T.reload_page}
          </Button>
        </div>
      ) : null}
      {showFinalList ? (
        <table className="results-table">
          <thead>
            <tr>
              <th>{T.question_header}</th>
              <th>{T.your_answer_header}</th>
              {hasCorrectAnswers ? <th style={{ textAlign: 'center' }}>{T.correct_header_alt}</th> : null}
              {hasCustomPoints ? <th style={{ textAlign: 'center' }}>{T.points_header}</th> : null}
            </tr>
          </thead>
          <tbody>
            {results.map((result, index) => (
              <tr key={index}>
                <td data-label={T.question_header}>
                  <QuestionCell question={result.question} image={result.image} file={result.file} T={T} />
                </td>
                <td data-label={T.your_answer_header}>
                  {result.timed_out ? (
                    <TimeUp T={T} />
                  ) : (
                    <span className={answerClass(result.is_correct)}>
                      <AnswerText answer={result.selected_answer} />
                    </span>
                  )}
                  {result.is_correct === false && result.correct_answer && (showRightAnswer || hasCorrectAnswers) ? (
                    <div className="correct-answer-hint">
                      {T.result_correct_header}
                      <AnswerText answer={result.correct_answer} />
                    </div>
                  ) : null}
                </td>
                {hasCorrectAnswers ? (
                  <td data-label={T.correct_header_alt} style={{ textAlign: 'center' }}>
                    {result.is_correct !== undefined ? <Mark isCorrect={result.is_correct} /> : <span className="muted">{T.unknown}</span>}
                  </td>
                ) : null}
                {hasCustomPoints ? (
                  <td data-label={T.points_header} style={{ textAlign: 'center' }}>
                    {formatPoints(result.earned_points !== undefined ? result.earned_points : result.is_correct ? result.points || 1 : 0)}/
                    {formatPoints(result.points || 1)}
                  </td>
                ) : null}
              </tr>
            ))}
          </tbody>
        </table>
      ) : null}
    </>
  );
}

// The page's own results, shown only when the server's results cannot be read.
function ClientResults({ results, T, showFinalList }: { results: ClientTestResult[]; T: Translations; showFinalList: boolean }) {
  const correctCount = results.filter((result) => result.isCorrect === true).length;
  const totalCount = results.length;
  const percentage = Math.round((correctCount / totalCount) * 100);
  // Correctness is unknown while the server hides it: then the "correct" column is left out
  const hasCorrectness = results.some((result) => result.isCorrect !== null);
  return (
    <>
      <p>
        <strong>{T.result_label}</strong> {correctCount}/{totalCount} ({percentage}%)
      </p>
      {showFinalList ? (
        <table className="results-table">
          <thead>
            <tr>
              <th>{T.question_header}</th>
              <th>{T.your_answer_header}</th>
              {hasCorrectness ? <th style={{ textAlign: 'center' }}>{T.correct_header}</th> : null}
            </tr>
          </thead>
          <tbody>
            {results.map((result, index) => (
              <tr key={index}>
                <td data-label={T.question_header}>
                  <QuestionCell question={result.question} image={result.image} file={result.file} T={T} />
                </td>
                <td data-label={T.your_answer_header}>
                  {result.timedOut ? (
                    <TimeUp T={T} />
                  ) : (
                    <span className={answerClass(result.isCorrect)}>
                      <AnswerText answer={result.selectedAnswer} />
                    </span>
                  )}
                </td>
                {hasCorrectness ? (
                  <td data-label={T.correct_header_alt} style={{ textAlign: 'center' }}>
                    {result.isCorrect !== null ? <Mark isCorrect={result.isCorrect} /> : <span className="muted">—</span>}
                  </td>
                ) : null}
              </tr>
            ))}
          </tbody>
        </table>
      ) : null}
    </>
  );
}

export function Results({ content, T, showFinalList, showRightAnswer }: { content: ResultsContent; T: Translations; showFinalList: boolean; showRightAnswer: boolean }) {
  if (content.kind === 'server') {
    return <ServerResults final={content.final} T={T} showFinalList={showFinalList} showRightAnswer={showRightAnswer} />;
  }
  return <ClientResults results={content.results} T={T} showFinalList={showFinalList} />;
}
