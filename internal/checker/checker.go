package checker

import (
	"errors"
	"fmt"
	"regexp"
	"runtime/metrics"
	"strings"
	"sync"
	"time"

	"go.starlark.net/resolve"
	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
)

// Timeout is the longest time a checker may run. A checker that runs
// longer is stopped and the answer counts as wrong.
var Timeout = 2 * time.Second

// MaxSteps limits the work of a checker, counted in Starlark steps (a
// loop round or a function call costs about 10). It stops endless loops on
// a fast computer before Timeout does.
const MaxSteps = 100_000_000

// MaxMemory stops a checker when the program's memory grows by more than
// this while the checker runs, as with a text that doubles in a loop.
const MaxMemory = 128 << 20

const (
	// maxCallDepth is the deepest a checker may nest calls, like Python's
	// recursion limit.
	maxCallDepth = 1000
	// limitCheckEvery is how many steps pass between checks of the call depth.
	limitCheckEvery = 1000
	// memoryCheckEvery is how often the memory is checked while a checker runs.
	memoryCheckEvery = 5 * time.Millisecond
)

// The error texts of checkers that were stopped.
const (
	tooManySteps  = "the checker took too many steps"
	tooMuchMemory = "the checker used too much memory"
	tooDeep       = "maximum recursion depth exceeded"
)

func timeoutTextFor(limit time.Duration) string {
	return fmt.Sprintf("the checker took more than %v", limit)
}

// Result is the outcome of a checker. The answer is correct when the
// checker ends without an error. Error is the text of the error: the assert
// message, or what went wrong.
type Result struct {
	Correct bool
	Error   string
}

// SyntaxError tells why checker code cannot run. Line is the line of the
// teacher's code, starting at 1.
type SyntaxError struct {
	Msg  string
	Line int
}

func (e *SyntaxError) Error() string {
	return fmt.Sprintf("%s (line %d)", e.Msg, e.Line)
}

// CheckExact is the rule for a question without checker code: the answer
// must equal the correct value, with whitespace at both ends ignored.
func CheckExact(answer, correctValue string) bool {
	return pyStrip(answer) == pyStrip(correctValue)
}

// Validate tells whether checker code can run. It returns nil when the code
// compiles. Names that are not defined are no error here: as in Python,
// they fail when the line runs.
func Validate(code string) *SyntaxError {
	_, err := compile(code)
	return err
}

// Check runs checker code with the student's answer in user_answer. The
// caller handles an empty checker with CheckExact; Check("", answer) counts
// as correct.
func Check(code, answer string) Result {
	if code == "" {
		return Result{Correct: true}
	}
	prog, err := compile(code)
	if err != nil {
		return Result{Error: err.Error()}
	}
	return run(prog, answer)
}

var fileOptions = &syntax.FileOptions{
	While:           true,
	Set:             true,
	Recursion:       true,
	TopLevelControl: true,
	GlobalReassign:  true,
}

// compile rewrites Python checker code to Starlark and compiles it.
func compile(code string) (*starlark.Program, *SyntaxError) {
	src, synErr := rewrite(code)
	if synErr != nil {
		return nil, synErr
	}
	_, prog, err := starlark.SourceProgramOptions(fileOptions, "checker", src, isPredeclared)
	if err != nil {
		return nil, starlarkSyntaxError(err, lineCount(code))
	}
	return prog, nil
}

// starlarkSyntaxError turns a Starlark compile error into a SyntaxError on
// the teacher's line. The generated code has one extra line at the top,
// the "def" of the wrapper function.
func starlarkSyntaxError(err error, lines int) *SyntaxError {
	var pos syntax.Position
	msg := err.Error()
	var syntaxErr syntax.Error
	var resolveErrs resolve.ErrorList
	switch {
	case errors.As(err, &syntaxErr):
		pos, msg = syntaxErr.Pos, syntaxErr.Msg
	case errors.As(err, &resolveErrs):
		pos, msg = resolveErrs[0].Pos, resolveErrs[0].Msg
	}
	line := min(max(int(pos.Line)-1, 1), lines)
	return &SyntaxError{Msg: msg, Line: line}
}

// runState lets a running check be stopped from outside: by the timer, the
// memory watch or the step limit.
type runState struct {
	once    sync.Once
	stopped chan struct{}
	reason  string
}

func (s *runState) stop(thread *starlark.Thread, reason string) {
	s.once.Do(func() {
		s.reason = reason
		close(s.stopped)
		thread.Cancel(reason)
	})
}

// run executes a compiled checker.
func run(prog *starlark.Program, answer string) (result Result) {
	state := &runState{stopped: make(chan struct{})}
	thread := &starlark.Thread{Name: "checker", Print: func(*starlark.Thread, string) {}}
	thread.SetLocal(stateKey, state)
	thread.SetMaxExecutionSteps(limitCheckEvery)
	thread.OnMaxSteps = func(th *starlark.Thread) {
		switch {
		case th.Steps >= MaxSteps:
			state.stop(th, tooManySteps)
		case th.CallStackDepth() > maxCallDepth:
			state.stop(th, tooDeep)
		default:
			th.SetMaxExecutionSteps(th.Steps + limitCheckEvery)
		}
	}
	done := make(chan struct{})
	defer close(done)
	go watch(thread, state, Timeout, done)

	globals := make(starlark.StringDict, len(predeclared)+1)
	for name, v := range predeclared {
		globals[name] = v
	}
	globals["_answer_"] = starlark.String(answer)
	defer func() {
		if r := recover(); r != nil {
			result = Result{Error: fmt.Sprintf("the checker failed: %v", r)}
		}
	}()
	if _, err := prog.Init(thread, globals); err != nil {
		return Result{Error: errorText(err)}
	}
	return Result{Correct: true}
}

// watch stops the check when it runs longer than limit or when the memory
// grows too much.
func watch(thread *starlark.Thread, state *runState, limit time.Duration, done <-chan struct{}) {
	timer := time.NewTimer(limit)
	defer timer.Stop()
	ticker := time.NewTicker(memoryCheckEvery)
	defer ticker.Stop()
	start := heapBytes()
	for {
		select {
		case <-done:
			return
		case <-state.stopped:
			return
		case <-timer.C:
			state.stop(thread, timeoutTextFor(limit))
			return
		case <-ticker.C:
			if heapBytes() > start+MaxMemory {
				state.stop(thread, tooMuchMemory)
				return
			}
		}
	}
}

// heapBytes returns the memory held by Go objects, without stopping the
// program.
func heapBytes() uint64 {
	sample := []metrics.Sample{{Name: "/memory/classes/heap/objects:bytes"}}
	metrics.Read(sample)
	return sample[0].Value.Uint64()
}

var (
	undefinedName  = regexp.MustCompile(`^internal error: predeclared variable (\S+) is uninitialized$`)
	unassignedName = regexp.MustCompile(`^(?:local|global) variable (\S+) referenced before assignment$`)
)

// errorText returns the error message of a failed check without Starlark's
// file, line and traceback. Errors about unknown names read like Python's.
func errorText(err error) string {
	msg := err.Error()
	var evalErr *starlark.EvalError
	if errors.As(err, &evalErr) {
		msg = evalErr.Msg
	}
	msg = strings.TrimPrefix(msg, "Starlark computation cancelled: ")
	for _, pattern := range []*regexp.Regexp{undefinedName, unassignedName} {
		if m := pattern.FindStringSubmatch(msg); m != nil {
			return fmt.Sprintf("name '%s' is not defined", m[1])
		}
	}
	return msg
}
