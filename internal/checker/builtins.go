package checker

// The names a checker can use. Starlark has most Python builtins already;
// this file adds the missing ones (sum, round, pow, map, filter, isinstance,
// format), the math module with a few more functions, the helpers to_int,
// distance and direction_angle, and the internal functions that the
// rewrite step calls (_assert_, _fmt_, _iter_, _attr_). Some builtins are
// replaced to act like Python: str and repr quote like Python, int and float
// read text the Python way, and list, sorted, min and friends accept text.

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"strings"
	"unicode"
	"unicode/utf8"

	starlarkmath "go.starlark.net/lib/math"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
	"go.starlark.net/syntax"
)

// maxPowerBits limits the size of a**b and a*b for integers, so that
// 10 ** 10 ** 10 fails at once instead of running for a very long time.
const maxPowerBits = 1 << 20

const tooLargeNumber = "the number is too large"

// maxModPowWork limits pow(a, b, m): the work grows with the bits of b
// times the bits of m. 8000-bit numbers still take only milliseconds.
const maxModPowWork = 1 << 26

// maxRepeatBytes limits the size of text * n and list * n, which Starlark
// makes in one step, before the memory watch could stop the checker.
const maxRepeatBytes = MaxMemory / 2

// predeclared holds the names every checker can use. The values never
// change, so all checks share them.
var predeclared = newPredeclared()

func newPredeclared() starlark.StringDict {
	d := starlark.StringDict{
		"_assert_":        builtin("_assert_", assertBuiltin),
		"_fmt_":           builtin("_fmt_", fmtBuiltin),
		"_iter_":          builtin("_iter_", iterBuiltin),
		"_attr_":          builtin("_attr_", attrBuiltin),
		"_mul_":           builtin("_mul_", mulBuiltin),
		"_add_":           builtin("_add_", addBuiltin),
		"_iadd_":          builtin("_iadd_", iaddBuiltin),
		"_mod_":           builtin("_mod_", modBuiltin),
		"_cat_":           builtin("_cat_", catBuiltin),
		"_star_":          builtin("_star_", starBuiltin),
		"_cmp_":           builtin("_cmp_", cmpBuiltin),
		"range":           builtin("range", rangeBuiltin),
		"getattr":         builtin("getattr", getattrBuiltin),
		"math":            mathModule(),
		"to_int":          helperBuiltin("to_int", toInt),
		"distance":        helperBuiltin("distance", distance),
		"direction_angle": helperBuiltin("direction_angle", directionAngle),
		"sum":             builtin("sum", sumBuiltin),
		"round":           builtin("round", roundBuiltin),
		"pow":             builtin("pow", powBuiltin),
		"map":             builtin("map", mapBuiltin),
		"filter":          builtin("filter", filterBuiltin),
		"isinstance":      builtin("isinstance", isinstanceBuiltin),
		"format":          builtin("format", formatBuiltin),
		"str":             builtin("str", strBuiltin),
		"repr":            builtin("repr", reprBuiltin),
		"int":             builtin("int", intBuiltin),
		"float":           builtin("float", floatBuiltin),
		"type":            builtin("type", typeBuiltin),
		"print":           builtin("print", printBuiltin),
		"dict":            builtin("dict", dictBuiltin),
		"zip":             builtin("zip", zipBuiltin),
	}
	// These accept text like Python does: list("ab") is ["a", "b"].
	for name, textArgs := range map[string]func(int) bool{
		"list":      firstArg,
		"tuple":     firstArg,
		"set":       firstArg,
		"sorted":    firstArg,
		"reversed":  firstArg,
		"enumerate": firstArg,
		"any":       firstArg,
		"all":       firstArg,
		"min":       nil,
		"max":       nil,
	} {
		d[name] = textBuiltin(name, name, textArgs)
	}
	// Starlark has no frozen set: frozenset makes a normal set.
	d["frozenset"] = textBuiltin("frozenset", "set", firstArg)
	// The rewrite step calls these names. A checker cannot change them,
	// although it may assign to pow or set.
	d["_pow_"] = d["pow"]
	d["_set_"] = d["set"]
	return d
}

func firstArg(i int) bool { return i == 0 }

func builtin(name string, fn func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error)) *starlark.Builtin {
	return starlark.NewBuiltin(name, fn)
}

// isPredeclared tells the Starlark compiler which names come from outside.
// Names that are not defined anywhere count too: Python reports them only
// when the line runs ("name 'x' is not defined"), and so does the checker.
func isPredeclared(name string) bool {
	if _, ok := predeclared[name]; ok || name == "_answer_" || hidden[name] {
		return true
	}
	_, universal := starlark.Universe[name]
	return !universal
}

// hidden holds Starlark builtins that a checker cannot use: the name is not
// defined, as in 1.19. bytes(n) makes n bytes in one step, before any limit
// can stop it, and fail(*args) joins the text of all its arguments without
// a size check. Text like b'abc' still works.
var hidden = map[string]bool{"bytes": true, "fail": true}

const stateKey = "webquiz.checker"

func stateOf(thread *starlark.Thread) *runState {
	s, _ := thread.Local(stateKey).(*runState)
	return s
}

// meter counts work done inside a builtin, like the interpreter counts its
// own steps, so that a long builtin stops with the check.
type meter struct {
	thread *starlark.Thread
	state  *runState
}

func newMeter(thread *starlark.Thread) meter {
	return meter{thread: thread, state: stateOf(thread)}
}

// step counts one item and tells when the check must stop.
func (m meter) step() error { return m.add(1) }

// add counts n steps at once, for work done in one go, like copying a
// list, and tells when the check must stop.
func (m meter) add(n int) error {
	m.thread.Steps += uint64(n)
	if m.thread.Steps >= MaxSteps && m.state != nil {
		m.state.stop(m.thread, tooManySteps)
	}
	return m.err()
}

// err tells whether the check must stop, without counting a step.
func (m meter) err() error {
	if m.state != nil && m.state.isStopped.Load() {
		return errors.New(m.state.reason)
	}
	if m.thread.Steps >= MaxSteps {
		return errors.New(tooManySteps)
	}
	return nil
}

// iterate calls fn for every item of v. Text gives its characters.
func iterate(thread *starlark.Thread, v starlark.Value, fn func(starlark.Value) error) error {
	m := newMeter(thread)
	switch v := v.(type) {
	case starlark.String:
		for _, r := range string(v) {
			if err := m.step(); err != nil {
				return err
			}
			if err := fn(starlark.String(string(r))); err != nil {
				return err
			}
		}
		return nil
	case starlark.Iterable:
		iter := v.Iterate()
		defer iter.Done()
		var x starlark.Value
		for iter.Next(&x) {
			if err := m.step(); err != nil {
				return err
			}
			if err := fn(x); err != nil {
				return err
			}
		}
		// A metered value, like range(), ends the loop early when the
		// check must stop: then these are not all the items.
		return m.err()
	}
	return fmt.Errorf("'%s' object is not iterable", pyTypeName(v))
}

// knownLength returns how many items v gives, when that is known without
// going through it: the characters of a text, the length of a sequence
// like a list, a range or text.elems(), or the size of text.codepoints()
// and bytes.elems().
func knownLength(v starlark.Value) (int, bool) {
	switch v := v.(type) {
	case starlark.String:
		return utf8.RuneCountInString(string(v)), true
	case starlark.Sequence:
		return v.Len(), true
	case meteredIterable:
		return v.length, v.length >= 0
	}
	return 0, false
}

// tooManyItems tells whether a list of n items would be larger than
// maxRepeatBytes. An item takes about 16 bytes.
func tooManyItems(n int) bool { return tooLarge(n, 16) }

// tooLarge tells whether n items of itemBytes bytes each would be larger
// than maxRepeatBytes. It divides instead of multiplying, so that a huge
// n, like the length of range(2 ** 62), cannot overflow.
func tooLarge(n int, itemBytes uint64) bool {
	return n < 0 || itemBytes > 0 && uint64(n) > maxRepeatBytes/itemBytes
}

// items collects the items of v. Lists and tuples are used as they are. It
// refuses at once when the length is known and too large, else as soon as
// there are too many items.
func items(thread *starlark.Thread, v starlark.Value) (starlark.Tuple, error) {
	if n, ok := knownLength(v); ok && tooManyItems(n) {
		return nil, errors.New(tooMuchMemory)
	}
	switch v := v.(type) {
	case starlark.Tuple:
		return v, nil
	case *starlark.List:
		// The copy is quick, but it counts as work, like a loop would.
		if err := newMeter(thread).add(v.Len()); err != nil {
			return nil, err
		}
		out := make(starlark.Tuple, v.Len())
		for i := range out {
			out[i] = v.Index(i)
		}
		return out, nil
	}
	var out starlark.Tuple
	err := iterate(thread, v, func(x starlark.Value) error {
		if tooManyItems(len(out) + 1) {
			return errors.New(tooMuchMemory)
		}
		out = append(out, x)
		return nil
	})
	return out, err
}

// lockstep opens an iterator for every argument of zip() and map(), which
// take one item of each argument at a time. Text gives its characters.
// Nothing is copied: zip(range(10 ** 9), 'ab') reads only two items of the
// range. rows is the length of the shortest argument, or -1 when no length
// is known. The caller must call Done on the iterators.
func lockstep(args starlark.Tuple) (iters []starlark.Iterator, rows int, err error) {
	rows = -1
	for _, arg := range args {
		var it starlark.Iterator
		switch v := arg.(type) {
		case starlark.String:
			it = &charIterator{text: string(v)}
		case *starlark.List:
			it = &listIterator{list: v}
		case starlark.Iterable:
			it = v.Iterate()
		default:
			done(iters)
			return nil, 0, fmt.Errorf("'%s' object is not iterable", pyTypeName(arg))
		}
		iters = append(iters, it)
		if n, ok := knownLength(arg); ok && (rows < 0 || n < rows) {
			rows = n
		}
	}
	return iters, rows, nil
}

func done(iters []starlark.Iterator) {
	for _, it := range iters {
		it.Done()
	}
}

// charIterator gives the characters of a text, one at a time.
type charIterator struct{ text string }

func (it *charIterator) Next(p *starlark.Value) bool {
	if it.text == "" {
		return false
	}
	r, size := utf8.DecodeRuneInString(it.text)
	*p = starlark.String(string(r))
	it.text = it.text[size:]
	return true
}

func (it *charIterator) Done() {}

// listIterator goes through a list like Python's list iterator does: the
// function given to map() may change the list, and the loop sees the
// change. Starlark's own iterator would refuse the change.
type listIterator struct {
	list *starlark.List
	next int
}

func (it *listIterator) Next(p *starlark.Value) bool {
	if it.next >= it.list.Len() {
		return false
	}
	*p = it.list.Index(it.next)
	it.next++
	return true
}

func (it *listIterator) Done() {}

// zipBuiltin is zip(): a list of tuples, as long as the shortest argument.
// The size of the result is checked before it is made.
func zipBuiltin(thread *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if len(kwargs) > 0 {
		return nil, errors.New("zip does not accept keyword arguments")
	}
	iters, rows, err := lockstep(args)
	if err != nil {
		return nil, err
	}
	defer done(iters)
	// A row is a tuple: about 40 bytes, and 16 for each item.
	rowBytes := 40 + 16*uint64(len(args))
	if rows > 0 && tooLarge(rows, rowBytes) {
		return nil, errors.New(tooMuchMemory)
	}
	result := make([]starlark.Value, 0, max(rows, 0))
	m := newMeter(thread)
	for len(iters) > 0 {
		if err := m.step(); err != nil {
			return nil, err
		}
		if tooLarge(len(result)+1, rowBytes) {
			return nil, errors.New(tooMuchMemory)
		}
		row := make(starlark.Tuple, len(iters))
		for i, it := range iters {
			if !it.Next(&row[i]) {
				return unlessStopped(thread, starlark.NewList(result), nil)
			}
		}
		result = append(result, row)
	}
	return starlark.NewList(result), nil
}

// resultItemBytes is about how much memory these builtins take for each
// item of their result: a set keeps a hash table entry for every item,
// enumerate makes a tuple for every item. The builtin makes the whole
// result in one step, so the size is checked before.
var resultItemBytes = map[string]uint64{
	"set":       hashItemBytes,
	"frozenset": hashItemBytes,
	"enumerate": 72,
}

// hashItemBytes is about how much memory one item of a set or a dict
// takes, with the room that the hash table keeps free.
const hashItemBytes = 128

// checkHashItems refuses a call that would put too many items into one set
// or dict in one step: the items it already has, the items of all the
// arguments and the keyword arguments, added up.
func checkHashItems(existing int, args starlark.Tuple, kwargs []starlark.Tuple) error {
	total := existing + len(kwargs)
	if tooLarge(total, hashItemBytes) {
		return errors.New(tooMuchMemory)
	}
	for _, arg := range args {
		n, ok := knownLength(arg)
		if !ok {
			continue
		}
		// Both are small here, so the sum cannot overflow.
		if tooLarge(n, hashItemBytes) || tooLarge(total+n, hashItemBytes) {
			return errors.New(tooMuchMemory)
		}
		total += n
	}
	return nil
}

// dictBuiltin is dict(), after checkHashItems.
func dictBuiltin(thread *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if err := checkHashItems(0, args, kwargs); err != nil {
		return nil, err
	}
	v, err := starlark.Universe["dict"].(*starlark.Builtin).CallInternal(thread, args, kwargs)
	return unlessStopped(thread, v, err)
}

// hashMethod calls the Starlark dict or set method of the same name, like
// update() or union(), after checkHashItems. The items of the dict or set
// count too: union() copies them, and update() makes the table larger.
func hashMethod(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	existing := 0
	if receiver, ok := b.Receiver().(starlark.Sequence); ok {
		existing = receiver.Len()
	}
	if err := checkHashItems(existing, args, kwargs); err != nil {
		return nil, err
	}
	native, err := b.Receiver().(starlark.HasAttrs).Attr(b.Name())
	if err != nil {
		return nil, err
	}
	v, err := starlark.Call(thread, native, args, kwargs)
	return unlessStopped(thread, v, err)
}

// unlessStopped passes on the result of a Starlark builtin, unless the
// check was stopped while the builtin ran: a metered value, like range(),
// then gave the builtin only some of its items.
func unlessStopped(thread *starlark.Thread, v starlark.Value, err error) (starlark.Value, error) {
	if err != nil {
		return nil, err
	}
	if err := newMeter(thread).err(); err != nil {
		return nil, err
	}
	return v, nil
}

// textBuiltin wraps the Starlark builtin original so that its iterable
// arguments may be text. textArg tells which positional arguments are
// iterables; nil means "the only argument, when there is just one" (min and
// max).
func textBuiltin(name, original string, textArg func(int) bool) *starlark.Builtin {
	starlarkBuiltin := starlark.Universe[original].(*starlark.Builtin)
	itemBytes := resultItemBytes[name]
	return builtin(name, func(thread *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		converted := make(starlark.Tuple, len(args))
		for i, arg := range args {
			converted[i] = arg
			isIterable := textArg != nil && textArg(i) || textArg == nil && len(args) == 1
			if !isIterable {
				continue
			}
			if n, ok := knownLength(arg); ok && tooLarge(n, itemBytes) {
				return nil, errors.New(tooMuchMemory)
			}
			switch arg.(type) {
			case starlark.String, starlark.Iterable:
				collected, err := items(thread, arg)
				if err != nil {
					return nil, err
				}
				if tooLarge(len(collected), itemBytes) {
					return nil, errors.New(tooMuchMemory)
				}
				converted[i] = collected
			}
		}
		if name == "enumerate" && len(kwargs) == 1 && kwargs[0][0] == starlark.String("start") && len(converted) == 1 {
			// Python allows enumerate(x, start=1); Starlark only enumerate(x, 1).
			converted, kwargs = append(converted, kwargs[0][1]), nil
		}
		return starlarkBuiltin.CallInternal(thread, converted, kwargs)
	})
}

// assertBuiltin runs a Python assert: _assert_(condition, message) where
// message is a function, so that it is only built when the check fails.
func assertBuiltin(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var cond, message starlark.Value
	if err := starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 1, &cond, &message); err != nil {
		return nil, err
	}
	if cond.Truth() {
		return starlark.None, nil
	}
	if message == nil {
		return nil, errors.New("")
	}
	value, err := starlark.Call(thread, message, nil, nil)
	if err != nil {
		return nil, err
	}
	text, err := pyStr(value)
	if err != nil {
		return nil, err
	}
	return nil, errors.New(text)
}

// fmtBuiltin formats one f-string field: _fmt_(value, conversion, spec).
func fmtBuiltin(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var value starlark.Value
	var conversion, spec string
	if err := starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 3, &value, &conversion, &spec); err != nil {
		return nil, err
	}
	var converted string
	var err error
	switch conversion {
	case "r":
		converted, err = pyRepr(value)
	case "s":
		converted, err = pyStr(value)
	case "a":
		converted, err = pyASCII(value)
	}
	if err != nil {
		return nil, err
	}
	if conversion != "" {
		value = starlark.String(converted)
	}
	text, err := formatValue(value, spec)
	if err != nil {
		return nil, err
	}
	return starlark.String(text), nil
}

// compareOps are the operators that _cmp_ takes.
var compareOps = map[string]syntax.Token{
	"<": syntax.LT, ">": syntax.GT, "<=": syntax.LE, ">=": syntax.GE, "==": syntax.EQL, "!=": syntax.NEQ,
}

// cmpBuiltin is _cmp_(op, a, b): a comparison like Python's. Starlark puts
// NaN above every number and calls it equal to itself; in Python every
// comparison with NaN is false, except "!=".
func cmpBuiltin(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if len(args) != 3 || len(kwargs) != 0 {
		return nil, fmt.Errorf("%s: want 3 arguments", b.Name())
	}
	opText, _ := args[0].(starlark.String)
	op, ok := compareOps[string(opText)]
	if !ok {
		return nil, fmt.Errorf("%s: unknown operator %s", b.Name(), args[0])
	}
	if isNaN(args[1]) && isNumber(args[2]) || isNaN(args[2]) && isNumber(args[1]) {
		return starlark.Bool(op == syntax.NEQ), nil
	}
	result, err := starlark.Compare(op, args[1], args[2])
	if err != nil {
		return nil, err
	}
	return starlark.Bool(result), nil
}

func isNaN(v starlark.Value) bool {
	f, ok := v.(starlark.Float)
	return ok && math.IsNaN(float64(f))
}

// isNumber tells whether v is an int, a float or a bool, which Python
// counts as a number. Python compares NaN with these; with other values,
// like text or None, it raises an error.
func isNumber(v starlark.Value) bool {
	switch v.(type) {
	case starlark.Int, starlark.Float, starlark.Bool:
		return true
	}
	return false
}

// mulBuiltin is a * b, but refuses a text or list that would be too large.
func mulBuiltin(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var x, y starlark.Value
	if err := starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 2, &x, &y); err != nil {
		return nil, err
	}
	if repeatTooLarge(x, y) || repeatTooLarge(y, x) {
		return nil, errors.New(tooMuchMemory)
	}
	if xi, ok := x.(starlark.Int); ok {
		if yi, ok := y.(starlark.Int); ok && xi.BigInt().BitLen()+yi.BigInt().BitLen() > maxPowerBits {
			return nil, errors.New(tooLargeNumber)
		}
	}
	return starlark.Binary(syntax.STAR, x, y)
}

// sizeOf returns about how much memory a text, bytes, list or tuple takes.
// A list item takes about 16 bytes.
func sizeOf(v starlark.Value) (uint64, bool) {
	switch v := v.(type) {
	case starlark.String:
		return uint64(len(v)), true
	case starlark.Bytes:
		return uint64(len(v)), true
	case *starlark.List:
		return uint64(v.Len()) * 16, true
	case starlark.Tuple:
		return uint64(len(v)) * 16, true
	}
	return 0, false
}

// repeatTooLarge tells whether seq * count would be larger than
// maxRepeatBytes.
func repeatTooLarge(seq, count starlark.Value) bool {
	size, ok := sizeOf(seq)
	n, isInt := count.(starlark.Int)
	if !ok || !isInt || size == 0 || n.Sign() <= 0 {
		return false
	}
	times, ok := n.Uint64()
	return !ok || times > maxRepeatBytes/size
}

// twoArgs returns the two arguments of an operator builtin.
func twoArgs(b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, starlark.Value, error) {
	if len(args) != 2 || len(kwargs) != 0 {
		return nil, nil, fmt.Errorf("%s: want 2 arguments", b.Name())
	}
	return args[0], args[1], nil
}

// addBuiltin is a + b, but refuses a text or list that would be too large:
// s + s + s + ... in one line could otherwise use all memory in one step.
func addBuiltin(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	x, y, err := twoArgs(b, args, kwargs)
	if err != nil {
		return nil, err
	}
	if sx, ok := sizeOf(x); ok {
		if sy, ok := sizeOf(y); ok && sx+sy > maxRepeatBytes {
			return nil, errors.New(tooMuchMemory)
		}
	}
	return starlark.Binary(syntax.PLUS, x, y)
}

// iaddBuiltin is x += y. A list grows in place, as in Python, so other
// names for the same list see the new items; it also takes the characters
// of a text.
func iaddBuiltin(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	x, y, err := twoArgs(b, args, kwargs)
	if err != nil {
		return nil, err
	}
	list, ok := x.(*starlark.List)
	if !ok {
		return addBuiltin(thread, b, args, kwargs)
	}
	if err := extendList(thread, list, y); err != nil {
		return nil, err
	}
	return list, nil
}

// extendList adds the items of more to list, in place. It checks the size
// before it copies anything, and it takes the characters of a text, as
// Python does.
func extendList(thread *starlark.Thread, list *starlark.List, more starlark.Value) error {
	if n, ok := knownLength(more); ok && tooManyItems(list.Len()+n) {
		return errors.New(tooMuchMemory)
	}
	elems, err := items(thread, more)
	if err != nil {
		return err
	}
	if tooManyItems(list.Len() + len(elems)) {
		return errors.New(tooMuchMemory)
	}
	extend, err := list.Attr("extend")
	if err != nil {
		return err
	}
	_, err = starlark.Call(thread, extend, starlark.Tuple{elems}, nil)
	return err
}

// extendMethod is list.extend(more), through extendList.
func extendMethod(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var more starlark.Value
	if err := starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 1, &more); err != nil {
		return nil, err
	}
	if err := extendList(thread, b.Receiver().(*starlark.List), more); err != nil {
		return nil, err
	}
	return starlark.None, nil
}

// starBuiltin is the x of f(*x). Starlark makes the arguments in one step,
// so a long x is refused first. Text gives its characters, as in Python.
func starBuiltin(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var v starlark.Value
	if err := starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 1, &v); err != nil {
		return nil, err
	}
	if n, ok := knownLength(v); ok && tooManyItems(n) {
		return nil, errors.New(tooMuchMemory)
	}
	switch v.(type) {
	case starlark.String:
		return items(thread, v)
	case starlark.Sequence:
		return v, nil
	case starlark.Iterable:
		// A lazy value of unknown length: collect it here, with the
		// size check, instead of in Starlark's call, which has none.
		return items(thread, v)
	}
	return v, nil
}

// modBuiltin is a % b: Python's text formatting for text, else the
// remainder.
func modBuiltin(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	x, y, err := twoArgs(b, args, kwargs)
	if err != nil {
		return nil, err
	}
	if template, ok := x.(starlark.String); ok {
		text, err := pyPercentFormat(string(template), y)
		if err != nil {
			return nil, err
		}
		return starlark.String(text), nil
	}
	return starlark.Binary(syntax.PERCENT, x, y)
}

// catBuiltin joins the parts of an f-string, refusing a text that would be
// too large.
func catBuiltin(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var out textBuilder
	for _, part := range args {
		text, ok := part.(starlark.String)
		if !ok {
			return nil, fmt.Errorf("%s: got %s, want string", b.Name(), part.Type())
		}
		if err := out.write(string(text)); err != nil {
			return nil, err
		}
	}
	return starlark.String(out.b.String()), nil
}

// iterBuiltin returns what a for loop goes over: the characters of a text,
// or the value itself.
func iterBuiltin(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var v starlark.Value
	if err := starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 1, &v); err != nil {
		return nil, err
	}
	if s, ok := v.(starlark.String); ok {
		// codepoints() gives the characters one by one, so a loop over a
		// long text does not build a list first.
		codepoints, err := s.Attr("codepoints")
		if err != nil {
			return nil, err
		}
		return starlark.Call(thread, codepoints, nil, nil)
	}
	return v, nil
}

// attrBuiltin returns x.name, through attr.
func attrBuiltin(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var x starlark.Value
	var name string
	if err := starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 2, &x, &name); err != nil {
		return nil, err
	}
	return attr(x, name)
}

// attr returns x.name. Text methods that can make a large result use this
// file's versions, which check the size first. When the Starlark value has
// no such method but Python has one, attr returns this file's version too.
func attr(x starlark.Value, name string) (starlark.Value, error) {
	if method := guardedMethods[x.Type()][name]; method != nil {
		return starlark.NewBuiltin(name, method).BindReceiver(x), nil
	}
	if obj, ok := x.(starlark.HasAttrs); ok {
		if v, err := obj.Attr(name); err == nil && v != nil {
			return v, nil
		}
	}
	if method := extraMethods[x.Type()][name]; method != nil {
		return starlark.NewBuiltin(name, method).BindReceiver(x), nil
	}
	return nil, fmt.Errorf("%s has no .%s field or method", x.Type(), name)
}

// getattrBuiltin is getattr(x, name[, default]), through attr.
func getattrBuiltin(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var x, fallback starlark.Value
	var name string
	if err := starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 2, &x, &name, &fallback); err != nil {
		return nil, err
	}
	v, err := attr(x, name)
	if err != nil && fallback != nil {
		return fallback, nil
	}
	return v, err
}

type builtinFunc = func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error)

// guardedMethods replace Starlark methods that can make a large result:
// they check the size before they make it. elems() and the like of text
// and bytes are metered.
var guardedMethods map[string]map[string]builtinFunc

func init() {
	guardedMethods = map[string]map[string]builtinFunc{
		"list": {"extend": extendMethod},
		"string": {
			"replace":        replaceMethod,
			"join":           joinMethod,
			"format":         formatMethod,
			"split":          splitMethod,
			"rsplit":         splitMethod,
			"splitlines":     splitlinesMethod,
			"elems":          meteredMethod,
			"codepoints":     meteredMethod,
			"elem_ords":      meteredMethod,
			"codepoint_ords": meteredMethod,
		},
		"bytes": {"elems": meteredMethod},
		"dict":  {"update": hashMethod},
		"set": {
			"union":                hashMethod,
			"update":               hashMethod,
			"intersection":         hashMethod,
			"symmetric_difference": hashMethod,
		},
	}
}

func replaceMethod(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var old, replacement string
	count := -1
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "old", &old, "new", &replacement, "count?", &count); err != nil {
		return nil, err
	}
	s := string(b.Receiver().(starlark.String))
	n := strings.Count(s, old)
	if count >= 0 && count < n {
		n = count
	}
	if int64(len(s))+int64(n)*(int64(len(replacement))-int64(len(old))) > maxRepeatBytes {
		return nil, errors.New(tooMuchMemory)
	}
	return starlark.String(strings.Replace(s, old, replacement, count)), nil
}

func joinMethod(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var iterable starlark.Value
	if err := starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 1, &iterable); err != nil {
		return nil, err
	}
	sep := string(b.Receiver().(starlark.String))
	parts, err := items(thread, iterable)
	if err != nil {
		return nil, err
	}
	texts := make([]string, len(parts))
	size := 0
	for i, part := range parts {
		text, ok := part.(starlark.String)
		if !ok {
			return nil, fmt.Errorf("sequence item %d: expected str instance, %s found", i, pyTypeName(part))
		}
		texts[i] = string(text)
		size += len(text)
		if i > 0 {
			size += len(sep)
		}
		if size > maxRepeatBytes {
			return nil, errors.New(tooMuchMemory)
		}
	}
	return starlark.String(strings.Join(texts, sep)), nil
}

func formatMethod(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	text, err := pyFormatString(string(b.Receiver().(starlark.String)), args, kwargs)
	if err != nil {
		return nil, err
	}
	return starlark.String(text), nil
}

// splitMethod checks how many parts split() or rsplit() would make (each
// takes about 32 bytes), then calls Starlark's method.
func splitMethod(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var sep starlark.Value = starlark.None
	maxsplit := -1
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "sep?", &sep, "maxsplit?", &maxsplit); err != nil {
		return nil, err
	}
	s := string(b.Receiver().(starlark.String))
	parts := 0
	if sepText, ok := sep.(starlark.String); ok && sepText != "" {
		parts = strings.Count(s, string(sepText)) + 1
	} else {
		inWord := false
		for _, r := range s {
			if unicode.IsSpace(r) {
				inWord = false
			} else if !inWord {
				inWord = true
				parts++
			}
		}
	}
	if maxsplit >= 0 && parts > maxsplit+1 {
		parts = maxsplit + 1
	}
	if uint64(parts)*32 > maxRepeatBytes {
		return nil, errors.New(tooMuchMemory)
	}
	return callTextMethod(thread, b, args, kwargs)
}

func splitlinesMethod(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	s := string(b.Receiver().(starlark.String))
	parts := strings.Count(s, "\n") + strings.Count(s, "\r") + 1
	if uint64(parts)*32 > maxRepeatBytes {
		return nil, errors.New(tooMuchMemory)
	}
	return callTextMethod(thread, b, args, kwargs)
}

// callTextMethod calls Starlark's own text method of the same name.
func callTextMethod(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	native, err := b.Receiver().(starlark.String).Attr(b.Name())
	if err != nil {
		return nil, err
	}
	return starlark.Call(thread, native, args, kwargs)
}

// extraMethods are Python methods that Starlark values do not have.
var extraMethods = map[string]map[string]builtinFunc{
	"string": {
		"isnumeric": textTest(unicode.IsNumber),
		"isdecimal": textTest(unicode.IsDigit),
		"isascii":   isASCII,
		"casefold":  textChange(func(s string) string { return strings.ReplaceAll(strings.ToLower(s), "ß", "ss") }),
		"swapcase":  textChange(swapCase),
		"zfill":     zfill,
		"ljust":     justify('<'),
		"rjust":     justify('>'),
		"center":    justify('^'),
	},
	"list": {
		"sort":    listSort,
		"reverse": listReverse,
		"count":   countItems,
		"copy":    listCopy,
	},
	"tuple": {"count": countItems},
	"dict": {
		"copy": func(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
			if err := starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 0); err != nil {
				return nil, err
			}
			d := starlark.NewDict(b.Receiver().(*starlark.Dict).Len())
			for _, item := range b.Receiver().(*starlark.Dict).Items() {
				if err := d.SetKey(item[0], item[1]); err != nil {
					return nil, err
				}
			}
			return d, nil
		},
	},
	"set": {
		"copy": func(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
			if err := starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 0); err != nil {
				return nil, err
			}
			return b.Receiver().(*starlark.Set).Union(starlark.NewSet(0).Iterate())
		},
	},
	"float": {"is_integer": func(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		if err := starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 0); err != nil {
			return nil, err
		}
		x := float64(b.Receiver().(starlark.Float))
		return starlark.Bool(!math.IsInf(x, 0) && x == math.Trunc(x)), nil
	}},
	"int": {"is_integer": func(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		return starlark.True, starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 0)
	}},
}

func textTest(ok func(rune) bool) builtinFunc {
	return func(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		if err := starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 0); err != nil {
			return nil, err
		}
		s := string(b.Receiver().(starlark.String))
		if s == "" {
			return starlark.False, nil
		}
		for _, r := range s {
			if !ok(r) {
				return starlark.False, nil
			}
		}
		return starlark.True, nil
	}
}

func isASCII(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if err := starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 0); err != nil {
		return nil, err
	}
	s := string(b.Receiver().(starlark.String))
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return starlark.False, nil
		}
	}
	return starlark.True, nil
}

func textChange(change func(string) string) builtinFunc {
	return func(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		if err := starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 0); err != nil {
			return nil, err
		}
		return starlark.String(change(string(b.Receiver().(starlark.String)))), nil
	}
}

func swapCase(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsUpper(r) {
			return unicode.ToLower(r)
		}
		return unicode.ToUpper(r)
	}, s)
}

func zfill(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var width int
	if err := starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 1, &width); err != nil {
		return nil, err
	}
	if width > maxRepeatBytes {
		return nil, errors.New(tooMuchMemory)
	}
	s := string(b.Receiver().(starlark.String))
	n := width - utf8.RuneCountInString(s)
	if n <= 0 {
		return starlark.String(s), nil
	}
	sign := ""
	if s != "" && (s[0] == '+' || s[0] == '-') {
		sign, s = s[:1], s[1:]
	}
	return starlark.String(sign + strings.Repeat("0", n) + s), nil
}

func justify(align byte) builtinFunc {
	return func(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var width int
		fill := " "
		if err := starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 1, &width, &fill); err != nil {
			return nil, err
		}
		if utf8.RuneCountInString(fill) != 1 {
			return nil, errors.New("The fill character must be exactly one character long")
		}
		if width > 0 && uint64(width)*uint64(len(fill)) > maxRepeatBytes {
			return nil, errors.New(tooMuchMemory)
		}
		s := string(b.Receiver().(starlark.String))
		n := width - utf8.RuneCountInString(s)
		if n <= 0 {
			return starlark.String(s), nil
		}
		left := 0
		switch align {
		case '>':
			left = n
		case '^':
			// Python's str.center puts the odd space on the left when width is odd.
			left = n/2 + (n & width & 1)
		}
		return starlark.String(strings.Repeat(fill, left) + s + strings.Repeat(fill, n-left)), nil
	}
}

func listSort(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if len(args) > 0 {
		return nil, errors.New("sort() takes no positional arguments")
	}
	list := b.Receiver().(*starlark.List)
	sorted, err := starlark.Call(thread, starlark.Universe["sorted"], starlark.Tuple{list}, kwargs)
	if err != nil {
		return nil, err
	}
	for i := 0; i < list.Len(); i++ {
		if err := list.SetIndex(i, sorted.(*starlark.List).Index(i)); err != nil {
			return nil, err
		}
	}
	return starlark.None, nil
}

func listReverse(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if err := starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 0); err != nil {
		return nil, err
	}
	list := b.Receiver().(*starlark.List)
	for i, j := 0, list.Len()-1; i < j; i, j = i+1, j-1 {
		a, z := list.Index(i), list.Index(j)
		if err := list.SetIndex(i, z); err != nil {
			return nil, err
		}
		if err := list.SetIndex(j, a); err != nil {
			return nil, err
		}
	}
	return starlark.None, nil
}

func countItems(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var x starlark.Value
	if err := starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 1, &x); err != nil {
		return nil, err
	}
	seq := b.Receiver().(starlark.Indexable)
	n := 0
	for i := 0; i < seq.Len(); i++ {
		eq, err := starlark.Equal(seq.Index(i), x)
		if err != nil {
			return nil, err
		}
		if eq {
			n++
		}
	}
	return starlark.MakeInt(n), nil
}

func listCopy(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if err := starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 0); err != nil {
		return nil, err
	}
	list := b.Receiver().(*starlark.List)
	elems := make([]starlark.Value, list.Len())
	for i := range elems {
		elems[i] = list.Index(i)
	}
	return starlark.NewList(elems), nil
}

// helperBuiltin makes a builtin from one of the helpers of helpers.go.
func helperBuiltin(name string, fn func(string) (*big.Int, error)) *starlark.Builtin {
	return builtin(name, func(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var s string
		if err := starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 1, &s); err != nil {
			return nil, err
		}
		n, err := fn(s)
		if err != nil {
			return nil, err
		}
		return starlark.MakeBigInt(n), nil
	})
}

func printBuiltin(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return starlark.None, nil
}

func strBuiltin(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var v starlark.Value = starlark.String("")
	if err := starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 0, &v); err != nil {
		return nil, err
	}
	text, err := pyStr(v)
	if err != nil {
		return nil, err
	}
	return starlark.String(text), nil
}

func reprBuiltin(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var v starlark.Value
	if err := starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 1, &v); err != nil {
		return nil, err
	}
	text, err := pyRepr(v)
	if err != nil {
		return nil, err
	}
	return starlark.String(text), nil
}

func formatBuiltin(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var v starlark.Value
	spec := ""
	if err := starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 1, &v, &spec); err != nil {
		return nil, err
	}
	text, err := formatValue(v, spec)
	if err != nil {
		return nil, err
	}
	return starlark.String(text), nil
}

func intBuiltin(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var x starlark.Value = starlark.MakeInt(0)
	var base starlark.Value
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "x?", &x, "base?", &base); err != nil {
		return nil, err
	}
	if base != nil {
		s, isText := x.(starlark.String)
		if !isText {
			return nil, errors.New("int() can't convert non-string with explicit base")
		}
		var b int
		if err := starlark.AsInt(base, &b); err != nil {
			return nil, fmt.Errorf("'%s' object cannot be interpreted as an integer", pyTypeName(base))
		}
		n, err := pyIntBase(string(s), b)
		if err != nil {
			return nil, err
		}
		return starlark.MakeBigInt(n), nil
	}
	switch x := x.(type) {
	case starlark.String:
		n, err := pyInt(string(x))
		if err != nil {
			return nil, err
		}
		return starlark.MakeBigInt(n), nil
	case starlark.Float:
		switch f := float64(x); {
		case math.IsInf(f, 0):
			return nil, errors.New("cannot convert float infinity to integer")
		case math.IsNaN(f):
			return nil, errors.New("cannot convert float NaN to integer")
		}
		return starlark.NumberToInt(x)
	case starlark.Int, starlark.Bool:
		return starlark.Call(thread, starlark.Universe["int"], starlark.Tuple{x}, nil)
	}
	return nil, fmt.Errorf("int() argument must be a string, a bytes-like object or a real number, not '%s'", pyTypeName(x))
}

func floatBuiltin(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var x starlark.Value = starlark.Float(0)
	if err := starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 0, &x); err != nil {
		return nil, err
	}
	switch x := x.(type) {
	case starlark.String:
		f, ok := parsePyFloat(string(x))
		if !ok {
			text, err := pyReprString(string(x))
			if err != nil {
				return nil, err
			}
			return nil, fmt.Errorf("could not convert string to float: %s", text)
		}
		return starlark.Float(f), nil
	case starlark.Float, starlark.Int, starlark.Bool:
		return starlark.Call(thread, starlark.Universe["float"], starlark.Tuple{x}, nil)
	}
	return nil, fmt.Errorf("float() argument must be a string or a real number, not '%s'", pyTypeName(x))
}

// typeValues maps Starlark type names to the builtins that stand for the
// Python types, so that type(1) == int works as in Python.
var typeValues = map[string]starlark.Value{}

func init() {
	for starlarkName, name := range map[string]string{
		"int": "int", "float": "float", "string": "str", "bool": "bool",
		"list": "list", "tuple": "tuple", "dict": "dict", "set": "set",
	} {
		if v, ok := predeclared[name]; ok {
			typeValues[starlarkName] = v
		} else {
			typeValues[starlarkName] = starlark.Universe[name]
		}
	}
}

func typeBuiltin(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var x starlark.Value
	if err := starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 1, &x); err != nil {
		return nil, err
	}
	if v, ok := typeValues[x.Type()]; ok {
		return v, nil
	}
	return starlark.String(x.Type()), nil
}

func isinstanceBuiltin(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var x, types starlark.Value
	if err := starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 2, &x, &types); err != nil {
		return nil, err
	}
	options, ok := types.(starlark.Tuple)
	if !ok {
		options = starlark.Tuple{types}
	}
	for _, option := range options {
		fn, ok := option.(*starlark.Builtin)
		if !ok {
			return nil, errors.New("isinstance() arg 2 must be a type, a tuple of types, or a union")
		}
		switch name := fn.Name(); name {
		case "int":
			if _, isBool := x.(starlark.Bool); isBool || x.Type() == "int" {
				return starlark.True, nil
			}
		case "str":
			if x.Type() == "string" {
				return starlark.True, nil
			}
		case "float", "bool", "list", "tuple", "dict", "set", "range":
			if x.Type() == name {
				return starlark.True, nil
			}
		case "frozenset":
			if x.Type() == "set" {
				return starlark.True, nil
			}
		default:
			return nil, errors.New("isinstance() arg 2 must be a type, a tuple of types, or a union")
		}
	}
	return starlark.False, nil
}

// mapBuiltin is map(), which gives a list here. It stops at the shortest
// argument, as in Python, and reads the arguments one item at a time.
func mapBuiltin(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if len(kwargs) > 0 || len(args) < 2 {
		return nil, errors.New("map() must have at least two arguments.")
	}
	iters, rows, err := lockstep(args[1:])
	if err != nil {
		return nil, err
	}
	defer done(iters)
	// Every call gets one item of each argument: all of them together may
	// not be more than a list could hold.
	argBytes := 16 * uint64(len(iters))
	if rows > 0 && tooLarge(rows, argBytes) {
		return nil, errors.New(tooMuchMemory)
	}
	var out []starlark.Value
	m := newMeter(thread)
	for {
		if err := m.step(); err != nil {
			return nil, err
		}
		if tooLarge(len(out)+1, argBytes) {
			return nil, errors.New(tooMuchMemory)
		}
		call := make(starlark.Tuple, len(iters))
		for i, it := range iters {
			if !it.Next(&call[i]) {
				return unlessStopped(thread, starlark.NewList(out), nil)
			}
		}
		v, err := starlark.Call(thread, args[0], call, nil)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
}

func filterBuiltin(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var fn, seq starlark.Value
	if err := starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 2, &fn, &seq); err != nil {
		return nil, err
	}
	var out []starlark.Value
	err := iterate(thread, seq, func(x starlark.Value) error {
		keep := x
		if fn != starlark.None {
			v, err := starlark.Call(thread, fn, starlark.Tuple{x}, nil)
			if err != nil {
				return err
			}
			keep = v
		}
		if keep.Truth() {
			out = append(out, x)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return starlark.NewList(out), nil
}

// sumBuiltin is Python's sum(), with the compensated float sum of
// Python 3.12+ so that sum([0.1] * 10) == 1.0.
func sumBuiltin(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var seq starlark.Value
	var start starlark.Value = starlark.MakeInt(0)
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "iterable", &seq, "start?", &start); err != nil {
		return nil, err
	}
	switch start.(type) {
	case starlark.String:
		return nil, errors.New("sum() can't sum strings [use ''.join(seq) instead]")
	case starlark.Bytes:
		return nil, errors.New("sum() can't sum bytes [use b''.join(seq) instead]")
	}
	acc := start
	var compensation float64
	_, inFloat := acc.(starlark.Float)
	err := iterate(thread, seq, func(x starlark.Value) error {
		if bv, ok := x.(starlark.Bool); ok {
			x = starlark.MakeInt(boolInt(bv))
		}
		if bv, ok := acc.(starlark.Bool); ok {
			acc = starlark.MakeInt(boolInt(bv))
		}
		if f, ok := acc.(starlark.Float); ok && inFloat {
			if y, ok := floatOf(x); ok {
				total := float64(f) + y
				if math.Abs(float64(f)) >= math.Abs(y) {
					compensation += (float64(f) - total) + y
				} else {
					compensation += (y - total) + float64(f)
				}
				acc = starlark.Float(total)
				return nil
			}
			acc = starlark.Float(addCompensation(float64(f), compensation))
			compensation, inFloat = 0, false
		}
		next, err := starlark.Binary(syntax.PLUS, acc, x)
		if err != nil {
			return fmt.Errorf("unsupported operand type(s) for +: '%s' and '%s'", pyTypeName(acc), pyTypeName(x))
		}
		acc = next
		_, inFloat = acc.(starlark.Float)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if f, ok := acc.(starlark.Float); ok && inFloat {
		acc = starlark.Float(addCompensation(float64(f), compensation))
	}
	return acc, nil
}

func boolInt(b starlark.Bool) int {
	if b {
		return 1
	}
	return 0
}

func addCompensation(total, compensation float64) float64 {
	if compensation != 0 && !math.IsInf(compensation, 0) && !math.IsNaN(compensation) {
		return total + compensation
	}
	return total
}

// floatOf returns a float or a small int as a float64.
func floatOf(x starlark.Value) (float64, bool) {
	switch x := x.(type) {
	case starlark.Float:
		return float64(x), true
	case starlark.Int:
		if n, ok := x.Int64(); ok {
			return float64(n), true
		}
	}
	return 0, false
}

func roundBuiltin(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var x, ndigits starlark.Value
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "number", &x, "ndigits?", &ndigits); err != nil {
		return nil, err
	}
	if bv, ok := x.(starlark.Bool); ok {
		x = starlark.MakeInt(boolInt(bv))
	}
	if ndigits == starlark.None {
		ndigits = nil
	}
	digits := 0
	if ndigits != nil {
		if err := starlark.AsInt(ndigits, &digits); err != nil {
			return nil, fmt.Errorf("'%s' object cannot be interpreted as an integer", pyTypeName(ndigits))
		}
	}
	switch x := x.(type) {
	case starlark.Int:
		if digits >= 0 {
			return x, nil
		}
		// 10**k is more than twice |x| when k > bits/3 + 2: then x rounds to 0,
		// and 10**k itself is never computed.
		if digits < -(x.BigInt().BitLen()/3 + 2) {
			return starlark.MakeInt(0), nil
		}
		return starlark.MakeBigInt(roundRat(new(big.Rat).SetInt(x.BigInt()), digits).Num()), nil
	case starlark.Float:
		f := float64(x)
		if ndigits == nil {
			switch {
			case math.IsInf(f, 0):
				return nil, errors.New("cannot convert float infinity to integer")
			case math.IsNaN(f):
				return nil, errors.New("cannot convert float NaN to integer")
			}
			return starlark.NumberToInt(starlark.Float(math.RoundToEven(f)))
		}
		if math.IsInf(f, 0) || math.IsNaN(f) || f == 0 || digits > 400 {
			return x, nil
		}
		if digits < -400 {
			return starlark.Float(math.Copysign(0, f)), nil
		}
		rounded, _ := roundRat(new(big.Rat).SetFloat64(f), digits).Float64()
		return starlark.Float(math.Copysign(rounded, f)), nil
	}
	return nil, fmt.Errorf("type %s doesn't define __round__ method", pyTypeName(x))
}

// roundRat rounds r to the given number of decimal digits (negative means
// tens, hundreds, ...), with ties to even, as Python's round() does.
func roundRat(r *big.Rat, digits int) *big.Rat {
	scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(abs(digits))), nil)
	scaled := new(big.Rat).Set(r)
	if digits >= 0 {
		scaled.Mul(scaled, new(big.Rat).SetInt(scale))
	} else {
		scaled.Quo(scaled, new(big.Rat).SetInt(scale))
	}
	// floor(scaled + 1/2), then fix exact ties to the even neighbour.
	half := new(big.Rat).Add(scaled, big.NewRat(1, 2))
	n := new(big.Int).Div(half.Num(), half.Denom())
	if new(big.Rat).SetInt(n).Cmp(half) == 0 && n.Bit(0) == 1 {
		n.Sub(n, big.NewInt(1))
	}
	out := new(big.Rat).SetInt(n)
	if digits >= 0 {
		return out.Quo(out, new(big.Rat).SetInt(scale))
	}
	return out.Mul(out, new(big.Rat).SetInt(scale))
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// powBuiltin is Python's pow(base, exp, mod) and the ** operator.
func powBuiltin(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var base, exp, mod starlark.Value
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "base", &base, "exp", &exp, "mod?", &mod); err != nil {
		return nil, err
	}
	base, exp = boolAsInt(base), boolAsInt(exp)
	if mod != nil && mod != starlark.None {
		return modPow(base, exp, boolAsInt(mod))
	}
	bi, baseIsInt := base.(starlark.Int)
	ei, expIsInt := exp.(starlark.Int)
	if baseIsInt && expIsInt && ei.Sign() >= 0 {
		bits := bi.BigInt().BitLen()
		e, ok := ei.Int64()
		if !ok || bits > 1 && float64(bits-1)*float64(e) > maxPowerBits {
			return nil, errors.New(tooLargeNumber)
		}
		return starlark.MakeBigInt(new(big.Int).Exp(bi.BigInt(), ei.BigInt(), nil)), nil
	}
	x, ok1 := starlark.AsFloat(base)
	y, ok2 := starlark.AsFloat(exp)
	if !ok1 || !ok2 {
		return nil, fmt.Errorf("unsupported operand type(s) for ** or pow(): '%s' and '%s'", pyTypeName(base), pyTypeName(exp))
	}
	if x == 0 && y < 0 {
		return nil, errors.New("0.0 cannot be raised to a negative power")
	}
	if x < 0 && y != math.Trunc(y) && !math.IsInf(y, 0) {
		return nil, errors.New("a negative number cannot be raised to a fractional power")
	}
	result := math.Pow(x, y)
	if math.IsInf(result, 0) && !math.IsInf(x, 0) && !math.IsInf(y, 0) {
		return nil, errors.New("(34, 'Numerical result out of range')")
	}
	return starlark.Float(result), nil
}

func boolAsInt(v starlark.Value) starlark.Value {
	if bv, ok := v.(starlark.Bool); ok {
		return starlark.MakeInt(boolInt(bv))
	}
	return v
}

func modPow(base, exp, mod starlark.Value) (starlark.Value, error) {
	bi, ok1 := base.(starlark.Int)
	ei, ok2 := exp.(starlark.Int)
	mi, ok3 := mod.(starlark.Int)
	if !ok1 || !ok2 || !ok3 {
		return nil, errors.New("pow() 3rd argument not allowed unless all arguments are integers")
	}
	m := mi.BigInt()
	if m.Sign() == 0 {
		return nil, errors.New("pow() 3rd argument cannot be 0")
	}
	absM := new(big.Int).Abs(m)
	e := ei.BigInt()
	if e.BitLen()*absM.BitLen() > maxModPowWork {
		return nil, errors.New(tooLargeNumber)
	}
	x := new(big.Int).Mod(bi.BigInt(), absM)
	if e.Sign() < 0 {
		if x.ModInverse(x, absM) == nil {
			return nil, errors.New("base is not invertible for the given modulus")
		}
		e = new(big.Int).Neg(e)
	}
	result := new(big.Int).Exp(x, e, absM)
	if m.Sign() < 0 && result.Sign() != 0 {
		result.Add(result, m)
	}
	return starlark.MakeBigInt(result), nil
}

// mathModule is go.starlark.net's math module with Python's errors and a few
// more functions that Python's math module has.
func mathModule() *starlarkstruct.Module {
	members := starlark.StringDict{}
	for name, v := range starlarkmath.Module.Members {
		members[name] = v
		if fn, ok := v.(*starlark.Builtin); ok && name != "floor" && name != "ceil" {
			members[name] = mathErrors(fn)
		}
	}
	unary := func(name string, fn func(float64) float64) {
		members[name] = mathErrors(builtin(name, func(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
			var x starlark.Value
			if err := starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 1, &x); err != nil {
				return nil, err
			}
			f, ok := starlark.AsFloat(x)
			if !ok {
				return nil, fmt.Errorf("must be real number, not %s", pyTypeName(x))
			}
			return starlark.Float(fn(f)), nil
		}))
	}
	unary("log10", math.Log10)
	unary("log2", math.Log2)
	members["fmod"] = members["mod"]
	members["inf"] = starlark.Float(math.Inf(1))
	members["nan"] = starlark.Float(math.NaN())
	members["tau"] = starlark.Float(2 * math.Pi)
	members["isclose"] = builtin("isclose", mathIsClose)
	members["isfinite"] = floatTest("isfinite", func(f float64) bool { return !math.IsInf(f, 0) && !math.IsNaN(f) })
	members["isinf"] = floatTest("isinf", func(f float64) bool { return math.IsInf(f, 0) })
	members["isnan"] = floatTest("isnan", math.IsNaN)
	members["trunc"] = builtin("trunc", func(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var x starlark.Value
		if err := starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 1, &x); err != nil {
			return nil, err
		}
		switch x := boolAsInt(x).(type) {
		case starlark.Int:
			return x, nil
		case starlark.Float:
			if math.IsInf(float64(x), 0) || math.IsNaN(float64(x)) {
				return nil, errors.New("cannot convert float infinity or NaN to integer")
			}
			return starlark.NumberToInt(x)
		}
		return nil, fmt.Errorf("type %s doesn't define __trunc__ method", pyTypeName(x))
	})
	members["factorial"] = builtin("factorial", func(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var n int
		if err := starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 1, &n); err != nil {
			return nil, err
		}
		if n < 0 {
			return nil, errors.New("factorial() not defined for negative values")
		}
		if n > 20000 {
			return nil, errors.New(tooLargeNumber)
		}
		return starlark.MakeBigInt(new(big.Int).MulRange(1, int64(n))), nil
	})
	members["gcd"] = builtin("gcd", func(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		if len(kwargs) > 0 {
			return nil, errors.New("gcd() takes no keyword arguments")
		}
		result := new(big.Int)
		for _, arg := range args {
			n, ok := boolAsInt(arg).(starlark.Int)
			if !ok {
				return nil, fmt.Errorf("'%s' object cannot be interpreted as an integer", pyTypeName(arg))
			}
			result.GCD(nil, nil, result, new(big.Int).Abs(n.BigInt()))
		}
		return starlark.MakeBigInt(result), nil
	})
	module := &starlarkstruct.Module{Name: "math", Members: members}
	module.Freeze()
	return module
}

// mathErrors makes a math function fail like Python's: "math domain error"
// when the result is not a number, "math range error" when it is too large.
func mathErrors(fn *starlark.Builtin) *starlark.Builtin {
	// At a pole the function goes to infinity: Python calls that a domain
	// error, not a range error. pow(0, -1) is a pole too.
	pole := map[string]bool{"log": true, "log10": true, "log2": true, "atanh": true}[fn.Name()]
	return builtin(fn.Name(), func(thread *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		result, err := fn.CallInternal(thread, args, kwargs)
		if err != nil {
			return nil, err
		}
		f, ok := result.(starlark.Float)
		if !ok {
			return result, nil
		}
		finiteArgs, nanArg, zeroBase := true, false, false
		for i, arg := range args {
			if x, ok := starlark.AsFloat(arg); ok {
				finiteArgs = finiteArgs && !math.IsInf(x, 0) && !math.IsNaN(x)
				nanArg = nanArg || math.IsNaN(x)
				zeroBase = zeroBase || i == 0 && x == 0
			}
		}
		atPole := pole || fn.Name() == "pow" && zeroBase
		switch {
		case math.IsNaN(float64(f)) && !nanArg:
			return nil, errors.New("math domain error")
		case math.IsInf(float64(f), 0) && finiteArgs && atPole:
			return nil, errors.New("math domain error")
		case math.IsInf(float64(f), 0) && finiteArgs:
			return nil, errors.New("math range error")
		}
		return result, nil
	})
}

func floatTest(name string, test func(float64) bool) *starlark.Builtin {
	return builtin(name, func(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var x starlark.Value
		if err := starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 1, &x); err != nil {
			return nil, err
		}
		f, ok := starlark.AsFloat(x)
		if !ok {
			return nil, fmt.Errorf("must be real number, not %s", pyTypeName(x))
		}
		return starlark.Bool(test(f)), nil
	})
}

// mathIsClose is Python's math.isclose(a, b, *, rel_tol=1e-09, abs_tol=0.0).
func mathIsClose(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var a, c starlark.Value
	var relTol, absTol starlark.Value = starlark.Float(1e-9), starlark.Float(0)
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "a", &a, "b", &c, "rel_tol?", &relTol, "abs_tol?", &absTol); err != nil {
		return nil, err
	}
	x, ok1 := starlark.AsFloat(a)
	y, ok2 := starlark.AsFloat(c)
	rel, ok3 := starlark.AsFloat(relTol)
	abso, ok4 := starlark.AsFloat(absTol)
	if !ok1 || !ok2 || !ok3 || !ok4 {
		return nil, errors.New("must be real number")
	}
	if rel < 0 || abso < 0 {
		return nil, errors.New("tolerances must be non-negative")
	}
	if x == y {
		return starlark.True, nil
	}
	if math.IsInf(x, 0) || math.IsInf(y, 0) {
		return starlark.False, nil
	}
	diff := math.Abs(y - x)
	return starlark.Bool(diff <= math.Abs(rel*y) || diff <= math.Abs(rel*x) || diff <= abso), nil
}
