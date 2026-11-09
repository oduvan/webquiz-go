// Package quiz reads, checks and scores quizzes.
//
// It is a port of the quiz parts of webquiz/server.py (WebQuiz 1.19):
//
//   - Load and Parse read a quiz file like load_questions_from_file: every
//     question gets its "id" (1, 2, ...), the settings get their defaults,
//     time limits and explanations are read. A Question is the question map
//     as written in the file, so "missing" and "empty" stay different, as in
//     Python. Load fails where starting the quiz failed in Python (a file
//     that is not a quiz, a choice question without options or
//     correct_answer, points that are not a number, a title that is not text).
//   - Validate is _validate_quiz_data: the checks run before a quiz is saved.
//     It takes data from a quiz file (pyval.LoadYAML) or from a JSON body
//     decoded with UseNumber, and gives the same messages as Python.
//   - CheckAnswer is _validate_answer, FormatAnswer is _format_answer_text,
//     RandomOrder is generate_random_question_order (sticky questions stay
//     with the one before them), NormalizePoints and SumPoints round points
//     to 2 decimals, FormatPoints writes them like str() did.
//
// Checker code (text questions) runs in internal/checker. This package does
// not import it; the server connects them once at start, before it handles
// requests (for example in an init function):
//
//	quiz.ValidateChecker = func(code string) (string, int, bool) { ... }
//	quiz.RunChecker = func(code, answer string) (bool, string) { ... }
package quiz
