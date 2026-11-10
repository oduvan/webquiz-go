import type { QuizBoot } from '../shared/boot';

// Shown at "/" when there are several quiz files and none is default.yaml.
// The texts are the ones of Python's templates/quiz_selection_required.html,
// which was English only and had no translations.
export function SelectionRequired({ boot }: { boot: QuizBoot }) {
  return (
    <main className="quiz-card mx-auto mt-14 w-full max-w-3xl px-3 py-6 text-center md:mt-16 md:px-8">
      <h1 className="mb-4 text-2xl font-bold">🎯 WebQuiz System</h1>
      <div className="selection-warning">
        <h3 className="mb-2 text-lg font-semibold">⚠️ Quiz Selection Required</h3>
        <p>Multiple quiz files are available, but no default quiz is set. An administrator must select which quiz to use.</p>
      </div>
      <h3 className="mb-2 text-lg font-semibold">📋 Available Quiz Files:</h3>
      <div className="quiz-list mb-5 rounded-md p-4 text-left" style={{ background: 'var(--quiz-question-bg)' }}>
        <ul className="list-disc pl-6">
          {boot.quizzes.map((quiz, index) => (
            <li key={index}>{quiz.title ? quiz.filename + ' - ' + quiz.title : quiz.filename}</li>
          ))}
        </ul>
      </div>
      <p className="mb-3">To select a quiz, please use the admin panel:</p>
      <a href="/admin/" className="admin-button inline-block rounded-md bg-blue-600 px-6 py-3 text-white hover:bg-blue-700">
        🔧 Go to Admin Panel
      </a>
      <p className="muted mt-8 text-sm">
        If you are an administrator and need to set up the master key,
        <br />
        please configure the <code>admin.master_key</code> in your server configuration.
      </p>
    </main>
  );
}
