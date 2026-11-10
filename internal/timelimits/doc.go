// Package timelimits reads and checks the time limit settings of a quiz.
//
// It is a port of webquiz/time_limits.py. A quiz can limit time in one of
// two modes:
//
//   - per_question: a default number of seconds for every question.
//     A question can set its own time_limit.
//   - whole_quiz: one number of seconds for the whole quiz. Question values
//     are ignored in this mode.
//
// All values are whole seconds, and 0 always means "no limit". This package
// only reads and checks the settings; the server keeps the clocks.
//
//   - Settings is the quiz-level limit; NoLimit means none.
//   - Validate checks a quiz before it is saved; Parse reads it when the quiz
//     loads (a broken value means no limits plus a warning for the log).
//   - QuestionLimits gives the effective seconds for every question id.
//   - IsEmptyAnswer tells "no answer given" (sent when the time is up).
//   - GraceSeconds and CheckInterval read WEBQUIZ_TIME_LIMIT_GRACE_SECONDS
//     and WEBQUIZ_TIME_LIMIT_CHECK_INTERVAL (tests shorten them).
//
// Where the Python code walked dict keys in file order, this package sorts
// them, so several unknown keys are reported in alphabetical order.
package timelimits
