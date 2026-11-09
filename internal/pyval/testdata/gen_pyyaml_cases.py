"""Write pyyaml_cases.json: how PyYAML's yaml.safe_load (WebQuiz 1.19) reads each YAML text.

Run it from the Python 1.19 project, with its virtual environment active:

    cd /path/to/webquiz && source venv/bin/activate
    python -I /path/to/webquiz-go/internal/pyval/testdata/gen_pyyaml_cases.py

The Go test TestLoadYAMLMatchesPyYAML reads the JSON file and checks that
pyval.LoadYAML gives the same values. JSON cannot tell 1 from 1.0 or a date
from a string, so every value is written with its Python type.
"""

import datetime
import json
import os

import yaml

CASES = [
    # Booleans: YAML 1.1 words (PyYAML) and what stays text
    "yes", "Yes", "YES", "no", "No", "NO", "on", "On", "ON", "off", "Off", "OFF",
    "true", "True", "TRUE", "false", "False", "FALSE", "y", "n", "Y", "N", "yES", "tRUE",
    # Integers
    "0", "-0", "+5", "12", "1_000", "0777", "0o17", "0x1F", "0X1F", "-0x1f", "0b101", "0b1_0",
    "08", "1:30", "-1:30", "190:20:30", "1:60", "1:5", "123456789012345678901234567890",
    # Floats
    "1.5", ".5", "1.", "-1.5", "+1.5", "1.5e+3", "1.5e3", "1e3", "1E+3", "1.0e-05", "0.1",
    ".inf", "-.inf", "+.Inf", ".INF", ".nan", ".NaN", ".NAN", "1:30.5", "1_0.5", "0.1_5",
    "09.5", "1e400", "1.0e+400",
    # Nulls
    "~", "null", "Null", "NULL", "", "nUll",
    # Dates and times
    "2024-01-15", "2024-1-5 10:00:00", "2001-12-14t21:59:43.10-05:00",
    "2001-12-14 21:59:43.10 Z", "2024-01-15x",
    # Quoted, block and tagged scalars
    "'yes'", '"no"', "'5'", '"1.5"', "|\n  yes\n", ">\n  5\n", "!!str 5", "!!str yes",
    "!!int '7'", "!!int 0x10", "!!float '1'", "!!float 5", "!!bool 'yes'", "!!bool 'On'",
    "!!null 'x'", "!!foo bar", "!!int abc", "!!bool maybe", "! foo",
    # The "value" tag (=) and merge keys
    "=", "- =", "=: x", "<<",
    # Collections
    "- a\n- yes\n- 1:30\n",
    "a: 1\na: 2\n",
    "base: &b {x: 1, y: 2}\nderived:\n  <<: *b\n  y: 3\n",
    "a: &a {x: 1}\nb: &b {x: 2, z: 3}\nc:\n  <<: [*a, *b]\n",
    "a: &a {x: 1}\nc:\n  <<: *a\n  <<: {x: 5, w: 6}\n",
    "1: one\nfalse: f\nnull: n\n1.5: x\n",
    "? [1, 2]\n: x\n",
    "x: &x 1\ny: *x\n",
    "questions:\n  - question: Q\n    options: [Yes, No]\n    correct_answer: 0\n    points: 1.5\n",
    "registration:\n  approve: yes\n  fields:\n    - Клас\n    - on\n",
    "time_limit: {whole_quiz: 15:00}\n",
    # Documents and syntax
    "--- a\n--- b\n",
    "--- a\n",
    "a: [1, 2",
    "\ufeffa: 1\n",
    "a: b\r\nc: d\r\n",
    "# only a comment\n",
    "a:\n  - b\n  -\n",
    "a: 'it''s'\n",
    "a: \"tab\\there\"\n",
]


def typed(value):
    if value is None:
        return {"type": "null"}
    if isinstance(value, bool):
        return {"type": "bool", "value": value}
    if isinstance(value, int):
        return {"type": "int", "value": str(value)}
    if isinstance(value, float):
        return {"type": "float", "value": repr(value)}
    if isinstance(value, str):
        return {"type": "str", "value": value}
    if isinstance(value, (datetime.date, datetime.datetime)):
        return {"type": "timestamp"}
    if isinstance(value, list):
        return {"type": "list", "items": [typed(item) for item in value]}
    if isinstance(value, dict):
        return {"type": "dict", "items": {key_text(key): typed(item) for key, item in value.items()}}
    raise TypeError(f"unexpected type {type(value)}")


def key_text(key):
    """The text a non-string key becomes in Go (the way json.dumps writes dict keys)."""
    if isinstance(key, str):
        return key
    if key is None or isinstance(key, (bool, int, float)):
        return json.dumps(key)
    return str(key)


def main():
    results = []
    for text in CASES:
        try:
            results.append({"yaml": text, "result": typed(yaml.safe_load(text))})
        except Exception as error:  # PyYAML raises ValueError too, for "!!int abc"
            results.append({"yaml": text, "error": type(error).__name__})
    path = os.path.join(os.path.dirname(os.path.abspath(__file__)), "pyyaml_cases.json")
    with open(path, "w", encoding="utf-8") as f:
        json.dump(results, f, ensure_ascii=False, indent=1)
        f.write("\n")
    print(f"wrote {len(results)} cases to {path}")


if __name__ == "__main__":
    main()
