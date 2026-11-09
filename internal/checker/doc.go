// Package checker runs the checker code of text questions.
//
// A teacher writes a checker in Python. The student's answer is in the
// variable user_answer. The answer is correct when the code ends without an
// error; the error text (usually an assert message) is shown to the student.
// WebQuiz 1.19 ran this code with Python's exec(). This package runs it with
// Starlark (go.starlark.net), a small Python-like language, after a rewrite
// step that turns Python-only syntax into Starlark.
//
// Use Check to run a checker, CheckExact for a question without a checker,
// and Validate to check the syntax when a quiz is saved or loaded.
//
// # What a checker can use
//
// Statements: assignments (also a = b = 0, x += 1 and x **= 2; type hints
// are ignored), if/elif/else, for, while, break, continue, pass, def (nested
// functions, default values, *args, **kwargs, recursion), return inside a
// def, and assert with or without a message. The message is only built when
// the assert fails, as in Python. The code may use if, for and while at the
// top level and may assign to user_answer.
//
// Expressions: numbers (also 1_000), text with any prefix (r, u, b, f),
// f-strings with conversions (!r, !s, !a), format specs (fill, align, sign,
// width, "," and "_", precision, types d f e g % s x o b c n) and nested
// fields, a ** b, chained comparisons like 0 < x < 10 (also with in, not in,
// is and is not; any comparison with NaN is false except "!=", as in
// Python), conditional expressions, lambda, list, dict and set
// comprehensions, generator expressions, set literals, and text joined by
// writing two literals side by side. A checker may assign to pow or set: the
// code that the rewrite adds uses its own names.
//
// Names: user_answer; math (Starlark's math module plus isclose, log10,
// log2, trunc, factorial, gcd, fmod, isfinite, isinf, isnan, inf, nan and
// tau; domain and range errors fail like in Python); the helpers to_int,
// distance and direction_angle (the same results and error texts as 1.19);
// and the builtins abs, all, any, bool, chr, dict, enumerate, filter, float,
// format, frozenset (it makes a normal set), int, isinstance, len, list, map,
// max, min, ord, pow, print, range, repr, reversed, round, set, sorted, str,
// sum, tuple, type and zip. int(), float(), str(), repr(), round(), pow() and
// sum() follow Python's rules: int and float read any Unicode decimal digit
// ("４２"), spaces around the number and "_" between digits, int reads at
// most 4300 digits like Python 3.11 and later, str of a float gives
// "1000000000000000.0" like Python, round goes to the even neighbour, and
// sum adds floats like Python 3.12. Text can be looped over and passed to
// list, sorted, set, min, max and the others, which see its characters.
// These Python methods work too, although Starlark has no such methods:
// list sort, reverse, count and copy; dict and set copy; text isnumeric,
// isdecimal, isascii, casefold, swapcase, zfill, ljust, rjust and center;
// float is_integer.
//
// # Known differences from Python
//
//   - These are not supported and give a syntax error: try, except,
//     finally, raise, class, import, from, with, global, nonlocal, del,
//     yield, async, await, decorators, ":=", "@", "...", else after for or
//     while, "*" in an assignment target and "/" among parameters.
//   - len() and indexing of text count bytes (UTF-8), not letters. For text
//     that is not plain ASCII, make a list of its letters first:
//     len(list(user_answer)) or list(user_answer)[0].
//     (len(user_answer.codepoints()) does not work: Starlark cannot count it.)
//   - "%" and .format() have no width or precision: "%.2f" % x fails. Use
//     an f-string or format(x, ".2f"). They also write values the Starlark
//     way, for example "%r" % "y" gives "y" in double quotes.
//   - Generator expressions are computed in full, also inside any() and
//     all(). map() and filter() return lists.
//   - "is" compares values like "==".
//   - Starlark does not count True and False as numbers outside sum():
//     n += c.isdigit() fails. Write n += 1 if c.isdigit() else 0, or use
//     sum().
//   - Text cannot be unpacked: a, b = user_answer fails. Write
//     a, b = list(user_answer).
//   - A tuple with a trailing comma and no brackets, like x = 1, is
//     refused when the quiz is saved. Write x = (1,).
//   - \N{...} escapes in text are refused when the quiz is saved.
//   - slice() does not exist; use a[1:5:2].
//   - In a chained comparison like a < f() < b the middle value is computed
//     twice.
//   - Some other Python methods are missing, for example text translate and
//     encode. Errors other than assert messages, unknown names, and the
//     errors of int(), float(), the helpers and math have Starlark's texts.
//   - A checker stops after Timeout (2 seconds), after MaxSteps steps, when
//     memory grows by more than MaxMemory, or when calls nest deeper than
//     1000. Text and lists made in one step (with "*", a format width,
//     zfill, ljust, rjust or center) may not be larger than half of
//     MaxMemory, the text of repr() and str() not larger than 4 MB, and
//     numbers made with "*" or "**" not longer than about a million bits.
//     The answer then counts as wrong. Python 1.19 had no such limits.
//   - The memory watch measures the memory of the whole program. A checker
//     that uses much memory can make another check that runs at the same
//     time fail too.
//
// Some code that failed in 1.19 works now. 1.19 ran exec() with separate
// globals and locals, so a function defined in a checker could not call
// another function of the checker, call itself or read user_answer, and a
// generator expression could not read variables of the checker: all of these
// failed with NameError. Here they work. print() did not exist in 1.19 and
// now does nothing. Starlark builtins such as getattr, hasattr, dir and hash
// are available too.
package checker
