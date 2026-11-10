"""Run checker cases through the checker of WebQuiz 1.19 (Python) and write
python_results.json next to this file.

The Go test TestMatchesPythonResults reads the JSON file and checks that the
Go checker gives the same result for every case, and the same message when
Python failed with an AssertionError, NameError or ValueError.

Run it from the Python project:

    cd /home/user/webquiz && source venv/bin/activate && \
        PYTHONPATH=/home/user/webquiz python \
        /home/user/wt/task-2/internal/checker/testdata/gen_python_results.py

Cases marked "go_unsupported" use Python features that the Go checker
cannot run. The Go test only checks that they fail there without a crash.
"""

import builtins
import json
import os
import sys

import webquiz
import webquiz.server as server

# _execute_checker() returns only (correct, message). To know the kind of
# error, wrap the exec() that it calls: the module name is looked up before
# the builtin one.
last_error = {}


def recording_exec(code, exec_globals, exec_locals):
    last_error.clear()
    try:
        builtins.exec(code, exec_globals, exec_locals)
    except BaseException as error:
        last_error["type"] = type(error).__name__
        raise


server.exec = recording_exec

# Checkers in the style that teachers and AI tools write. Each case runs with
# every answer in "answers".
CASES = [
    # Exact text and numbers
    {"name": "exact match", "checker": "assert user_answer.strip() == '4', 'Expected 4'", "answers": ["4", " 4 \n", "5"]},
    {"name": "float number", "checker": "assert float(user_answer) == 42, 'Expected 42'", "answers": ["42", " 42.0 ", "4 2", "forty-two"]},
    {"name": "keyword", "checker": "assert 'keyword' in user_answer.lower(), 'Must contain keyword'", "answers": ["The KEYWORD is here", "nothing"]},
    {"name": "math sqrt", "checker": "result = float(user_answer.strip())\nassert abs(result - math.sqrt(16)) < 0.01, f'Expected 4, got {result}'", "answers": ["4", "4.001", "5", "abc"]},
    {"name": "sqrt without math (1.19 docs example)", "checker": "result = float(user_answer)\nassert abs(result - sqrt(16)) < 0.01, f'Expected 4, got {result}'", "answers": ["4"]},
    {"name": "range with comma", "checker": "x = float(user_answer.replace(',', '.'))\nassert 0.49 < x < 0.51, f\"Значення {x} поза межами\"", "answers": ["0.5", "0,5", "0.6", "1e-1"]},
    {"name": "int error", "checker": "assert int(user_answer) > 0", "answers": ["5", " 7\n", "abc", "3.5"]},
    {"name": "str of float", "checker": "assert str(float(user_answer)) == '1500000.0', str(float(user_answer))", "answers": ["1500000", "1.5e6", "15"]},
    # Helpers
    {"name": "to_int", "checker": "assert to_int(user_answer) == 42, 'Відповідь: 42'", "answers": ["42", " 42 ", "abc", "42.0"]},
    {"name": "distance", "checker": "assert distance(user_answer) == 2000, 'Неправильна відстань'", "answers": ["2км", "2000m", "2 km", "1.5km", "abc"]},
    {"name": "direction_angle", "checker": "assert direction_angle(user_answer) == 2030, f'Кут {user_answer} неправильний'", "answers": ["20-30", "20 - 30", "2030", "20-30-40"]},
    # f-strings
    {"name": "f-string precision", "checker": "x = float(user_answer)\nassert abs(x - 3.14159) < 1e-3, f'Expected 3.142, got {x:.3f}'", "answers": ["3.1416", "3.2"]},
    {"name": "f-string width and align", "checker": "n = int(user_answer)\nassert n == 7, f'[{n:>5}] != [{7:<5}] {n:^7}|'", "answers": ["7", "12"]},
    {"name": "f-string grouping", "checker": "v = int(user_answer)\nassert v == 1000000, f'got {v:,} expected {1000000:_}'", "answers": ["1000000", "1234567"]},
    {"name": "f-string percent", "checker": "p = float(user_answer)\nassert p == 0.25, f'got {p:.1%}, expected {0.25:%}'", "answers": ["0.25", "0.333"]},
    {"name": "f-string repr", "checker": "assert user_answer.strip() == 'yes', f\"got {user_answer!r}, want {'yes'!r}\"", "answers": ["yes", "no", "it's"]},
    {"name": "f-string float forms", "checker": "x = float(user_answer)\nassert x == 12345.678, f'{x:e} {x:g} {x:.3g} {x:10.2f}|'", "answers": ["12345.678", "0.5"]},
    {"name": "f-string big float", "checker": "x = float(user_answer)\nassert x < 1e6, f'too big: {x}'", "answers": ["10", "2000000", "1e20"]},
    {"name": "nested f-string", "checker": "w = user_answer.strip()\nassert w == 'ok', f\"got {f'<{w}>'} and {w.upper()!r}\"", "answers": ["ok", "no"]},
    {"name": "dict in f-string", "checker": "d = {'k': user_answer.strip()}\nassert d['k'] == 'v', f\"got {d['k']}\"", "answers": ["v", "w"]},
    {"name": "triple-quoted f-string", "checker": "x = user_answer.strip()\nassert x == 'a', f'''Expected 'a'\ngot {x!r}'''", "answers": ["a", "b"]},
    # ** and comparisons
    {"name": "power", "checker": "n = int(user_answer)\nassert n ** 2 == 144, f'{n}**2 = {n ** 2}'", "answers": ["12", "-12", "11"]},
    {"name": "power precedence", "checker": "assert int(user_answer) == -2 ** 2 + 2 ** 3 ** 2, 'Expected 508'", "answers": ["508", "516"]},
    {"name": "word count range", "checker": "assert 1 <= len(user_answer.split()) <= 3, 'Від 1 до 3 слів'", "answers": ["one", "one two three", "a b c d"]},
    {"name": "is None", "checker": "v = None if not user_answer.strip() else user_answer.strip()\nassert v is not None, 'Empty answer'\nassert v == 'ok', f'{v!r} is not ok'", "answers": ["ok", " ", "nope"]},
    {"name": "set literal", "checker": "assert user_answer.strip().lower() in {'yes', 'так', 'y'}, 'Say yes'", "answers": ["Так", "no"]},
    {"name": "division and modulo", "checker": "n = int(user_answer)\nassert n // 3 == -2 and n % 3 == 1, f'{n // 3} {n % 3}'", "answers": ["-5", "-4"]},
    {"name": "division by zero", "checker": "x = float(user_answer)\nassert 1 / x < 1, 'too small'", "answers": ["2", "0"]},
    # Statements
    {"name": "words", "checker": "words = user_answer.lower().split()\nassert 'python' in words and len(words) >= 2, \"Need 'python' and another word\"", "answers": ["I like Python", "python"]},
    {"name": "loop over parts", "checker": "total = 0\nfor part in user_answer.split(','):\n    total += int(part)\nassert total == 10, f'Sum is {total}'", "answers": ["1,2,3,4", "5, 5", "1,2", "1,,2"]},
    {"name": "helper function", "checker": "def norm(s):\n    return s.strip().lower().replace('ё', 'е')\nassert norm(user_answer) == 'еж', f'got {norm(user_answer)!r}'", "answers": ["Ёж", " еж ", "уж"]},
    {"name": "while loop", "checker": "n = int(user_answer)\nsteps = 0\nwhile n != 1:\n    n = n // 2 if n % 2 == 0 else 3 * n + 1\n    steps += 1\nassert steps == 7, f'steps={steps}'", "answers": ["3", "6"]},
    {"name": "one-line if", "checker": "n = int(user_answer)\nif n > 10: assert n % 2 == 0, 'Large numbers must be even'\nassert n > 0, 'Must be positive'", "answers": ["12", "13", "-1"]},
    {"name": "assert on several lines", "checker": "assert (\n    user_answer.strip().lower()\n    == 'kyiv'\n), 'Wrong city'", "answers": ["KYIV", "Lviv"]},
    {"name": "assert in parentheses", "checker": "assert(user_answer.strip() == '42')", "answers": ["42", "24"]},
    {"name": "assert of a tuple always passes", "checker": "assert(user_answer == '42', 'Wrong')", "answers": ["7"]},
    {"name": "assert without message", "checker": "assert user_answer == 'x'", "answers": ["x", "y"]},
    {"name": "multi-line expected text", "checker": "expected = '''first line\nsecond line'''\nassert user_answer.strip() == expected, 'Expected two lines'", "answers": ["first line\nsecond line", "first line second line"]},
    {"name": "joined strings", "checker": "msg = ('Відповідь має '\n       'бути 42')\nassert to_int(user_answer) == 42, msg", "answers": ["42", "41"]},
    {"name": "chained assignment", "checker": "count = total = 0\nfor w in user_answer.split():\n    count += 1\n    total += len(w)\nassert count == 2 and total == 6, f'{count} words, {total} letters'", "answers": ["abc def", "ab cd"]},
    {"name": "type hints", "checker": "def parse(s: str) -> float:\n    return float(s.replace(',', '.'))\nassert parse(user_answer) == 2.5, 'Expected 2.5'", "answers": ["2,5", "2.6"]},
    {"name": "top-level return", "checker": "return distance(user_answer) == 540", "answers": ["540km"]},
    # Comprehensions, text and builtins
    {"name": "comprehension", "checker": "nums = [int(x) for x in user_answer.split()]\nassert sorted(nums) == [1, 2, 3], f'got {sorted(nums)}'", "answers": ["3 1 2", "1 2 2"]},
    {"name": "comprehension with range", "checker": "vals = [float(v) for v in user_answer.split(';') if 0 < float(v) < 10]\nassert len(vals) == 3, f'{len(vals)} values in range'", "answers": ["1;2;3", "1;20;3;-4", "0.5;9.5;5"]},
    {"name": "list of strings in message", "checker": "items = sorted(user_answer.split())\nassert items == ['a', 'b'], f'got {items}'", "answers": ["b a", "b c"]},
    {"name": "all digits", "checker": "assert all(c.isdigit() for c in user_answer.strip()), 'Only digits, please'", "answers": ["12345", "12a45"]},
    {"name": "sum of digits", "checker": "assert sum(int(d) for d in user_answer.strip()) == 10, 'Digits must add up to 10'", "answers": ["1234", "55", "99"]},
    {"name": "count capitals", "checker": "assert sum(c.isupper() for c in user_answer) == 2, 'Need 2 capitals'", "answers": ["HeLlo", "hello"]},
    {"name": "loop over letters", "checker": "vowels = 0\nfor ch in user_answer.lower():\n    if ch in 'aeiou':\n        vowels += 1\nassert vowels == 3, f'{vowels} vowels'", "answers": ["banana", "sky"]},
    {"name": "anagram", "checker": "assert sorted(user_answer.strip().lower()) == sorted('listen'), 'Not an anagram'", "answers": ["Silent", "lists"]},
    {"name": "enumerate letters", "checker": "letters = user_answer.strip()\nassert all(ord(c) == ord('a') + i for i, c in enumerate(letters)), 'Expected abc...'", "answers": ["abcd", "abd"]},
    {"name": "zip pairs", "checker": "pairs = list(zip(user_answer.split(), ['1', '2']))\nassert pairs == [('a', '1'), ('b', '2')], f'{pairs}'", "answers": ["a b", "b a"]},
    {"name": "sorted with key", "checker": "words = user_answer.split()\nassert sorted(words, key=len, reverse=True)[0] == 'longest', 'Wrong longest word'", "answers": ["a longest bb", "aaaaaaaaa longest"]},
    {"name": "map and filter", "checker": "nums = list(map(int, user_answer.split(',')))\nevens = list(filter(lambda n: n % 2 == 0, nums))\nassert evens == [2, 4], f'evens={evens}'", "answers": ["1,2,3,4", "2,4,6"]},
    {"name": "min and max", "checker": "nums = [int(x) for x in user_answer.split()]\nassert max(nums) - min(nums) == 5, f'range is {max(nums) - min(nums)}'", "answers": ["1 6", "2 10 4"]},
    {"name": "round to 2 digits", "checker": "assert round(float(user_answer), 2) == 3.14, f'{round(float(user_answer), 2)}'", "answers": ["3.14159", "3.145", "3.15"]},
    {"name": "round half to even", "checker": "assert round(float(user_answer)) == 2, f'round gives {round(float(user_answer))}'", "answers": ["2.5", "1.5", "2.6"]},
    {"name": "pow with modulus", "checker": "assert pow(int(user_answer), 2, 7) == 2, 'x*x mod 7 must be 2'", "answers": ["3", "4", "5"]},
    {"name": "sum of floats", "checker": "vals = [float(v) for v in user_answer.split()]\nassert sum(vals) == 1.0, f'sum={sum(vals)}'", "answers": ["0.1 0.2 0.7", "0.1 0.1 0.1 0.1 0.1 0.1 0.1 0.1 0.1 0.1", "0.5 0.6"]},
    {"name": "isinstance", "checker": "v = int(user_answer) if user_answer.strip().lstrip('-').isdigit() else user_answer\nassert isinstance(v, int), 'Not an integer'", "answers": ["-12", "1.5"]},
    {"name": "format", "checker": "assert format(float(user_answer), '.2f') == '3.14', 'Round to 2 decimals'", "answers": ["3.14159", "3.1"]},
    {"name": "dict lookup", "checker": "capitals = {'france': 'paris', 'ukraine': 'kyiv'}\nassert capitals.get('ukraine') == user_answer.strip().lower(), 'Wrong capital'", "answers": ["Kyiv", "Lviv"]},
    {"name": "math trigonometry", "checker": "a = math.radians(float(user_answer))\nassert math.isclose(math.sin(a), 0.5, abs_tol=1e-6), f'sin={math.sin(a):.4f}'", "answers": ["30", "150", "45"]},
    {"name": "math floor", "checker": "x = float(user_answer)\nassert math.floor(x * 100) / 100 == 3.14, f'{math.floor(x * 100) / 100}'", "answers": ["3.14159", "3.149", "3.2"]},
    {"name": "math domain error", "checker": "assert math.sqrt(float(user_answer)) != 5", "answers": ["4", "-1"]},
    {"name": "Cyrillic case", "checker": "assert user_answer.strip().lower() == 'київ', 'Відповідь: Київ'", "answers": ["Київ", "КИЇВ", "Lviv"]},
    {"name": "length of a Latin word", "checker": "assert len(user_answer.strip()) == 4, 'Чотири літери'", "answers": ["Kyiv", "Lviv!"]},
    {"name": "list sort method", "checker": "nums = [int(x) for x in user_answer.split()]\nnums.sort()\nassert nums == [1, 2, 3], f'sorted: {nums}'", "answers": ["3 2 1", "3 3 1"]},
    {"name": "isnumeric", "checker": "assert user_answer.strip().isnumeric(), 'Digits only'", "answers": ["123", "12a"]},
    {"name": "zfill", "checker": "assert user_answer.strip().zfill(4) == '0042', 'Expected 42'", "answers": ["42", "420"]},
    {"name": "is_integer", "checker": "assert float(user_answer).is_integer(), 'Whole number please'", "answers": ["4.0", "4.5"]},
    {"name": "format method without spec", "checker": "x = float(user_answer)\nassert x == 1, 'got {}'.format(x)", "answers": ["1", "2"]},
    {"name": "percent format without precision", "checker": "x = int(user_answer)\nassert x == 1, 'got %d' % x", "answers": ["1", "2"]},
    {"name": "format method with precision", "checker": "x = float(user_answer)\nassert x == 1, 'got {:.2f}'.format(x)", "answers": ["1", "2"]},
    {"name": "percent format with precision", "checker": "x = float(user_answer)\nassert x == 1, 'got %.2f' % x", "answers": ["1", "2"]},
    {"name": "format method with names and specs", "checker": "x = float(user_answer)\nassert x == 2.5, 'got {val:>8.3f} ({0!r})'.format(user_answer, val=x)",
     "answers": ["2.5", "3.14159", " 7 "]},
    {"name": "percent format with flags", "checker": "n = int(user_answer)\nassert n == 42, 'got %05d, %+.1e, %-6s|, %x' % (n, n, user_answer.strip(), n)",
     "answers": ["42", "255", "-7"]},
    {"name": "percent format with a mapping", "checker": "d = {'a': user_answer.strip(), 'n': len(user_answer)}\nassert d['a'] == 'ok', '%(a)s has %(n)d letters' % d",
     "answers": ["ok", "nope"]},
    # Text read as numbers: like Python's int() and float(), the helpers take
    # any Unicode decimal digit, Unicode spaces and "_" between digits.
    {"name": "to_int with Unicode digits", "checker": "assert to_int(user_answer) == 42, f'got {to_int(user_answer)}'",
     "answers": ["４２", "٤٢", " 42　", "\x1c42", "+4_2", "1_000", "4__2", "42²"]},
    {"name": "int() text forms", "checker": "n = int(user_answer)\nassert n == 7, f'{n}'",
     "answers": ["७", " +7 ", "0_7", "\x1c7", "7_", "Ⅶ", "1٣"]},
    {"name": "int() digit limit", "checker": "n = int(user_answer)\nassert n > 0", "answers": ["9" * 4300, "9" * 4301]},
    {"name": "distance with Unicode digits", "checker": "assert distance(user_answer) == 1500, f'got {distance(user_answer)}'",
     "answers": ["１.５km", "1_500m", "1.5E3", "+1500м", "１５００", "2\x1ckm", "1.5e3km", "inf"]},
    {"name": "direction_angle with Unicode digits", "checker": "assert direction_angle(user_answer) == 2030, 'wrong angle'",
     "answers": ["２０-３０", "20-3_0", " 20 -　30 ", "20-٣٠", "+20-+30"]},
    {"name": "float() text forms", "checker": "x = float(user_answer)\nassert x > 0, f'{x}'",
     "answers": ["１.５", "Infinity", "-inf", "+nan", "+1E-3", "1_0e1_0", " 1.5 ", "\x1c1.5", "١٫٥", "in_f", ".5_0", "1e5_"]},
    # Python features the Go checker cannot run
    {"name": "try and except", "checker": "try:\n    v = float(user_answer)\nexcept ValueError:\n    v = None\nassert v == 1.5, 'Expected 1.5'",
     "answers": ["1.5", "abc"], "go_unsupported": "try/except is not supported"},
    {"name": "raise", "checker": "if user_answer.strip() != '42':\n    raise ValueError('Wrong answer')", "answers": ["42", "41"],
     "go_unsupported": "raise is not supported"},
    {"name": "class", "checker": "class Answer:\n    pass\nassert user_answer", "answers": ["x"], "go_unsupported": "class is not supported"},
    {"name": "import", "checker": "import math\nassert math.sqrt(float(user_answer)) == 3", "answers": ["9"], "go_unsupported": "import is not supported"},
    {"name": "length of a Cyrillic word", "checker": "assert len(user_answer.strip()) == 4, 'Чотири літери'", "answers": ["Київ"],
     "go_unsupported": "len() counts bytes, so non-ASCII text needs .codepoints()"},
]


def main():
    results = []
    for case in CASES:
        for answer in case["answers"]:
            correct, error = server.TestingServer._execute_checker(None, answer, {"checker": case["checker"]})
            entry = {
                "name": case["name"],
                "checker": case["checker"],
                "answer": answer,
                "correct": correct,
                "error": error,
                "error_type": None if correct else last_error.get("type"),
            }
            if "go_unsupported" in case:
                entry["go_unsupported"] = case["go_unsupported"]
            results.append(entry)
    output = {
        "comment": "Generated by gen_python_results.py from the WebQuiz 1.19 checker. Do not edit.",
        "webquiz_version": webquiz.__version__,
        "python_version": sys.version.split()[0],
        "cases": results,
    }
    path = os.path.join(os.path.dirname(os.path.abspath(__file__)), "python_results.json")
    with open(path, "w", encoding="utf-8") as f:
        json.dump(output, f, ensure_ascii=False, indent=1)
        f.write("\n")
    print(f"wrote {len(results)} results to {path}")


if __name__ == "__main__":
    main()
