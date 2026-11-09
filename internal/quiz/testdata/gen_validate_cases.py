"""Write validate_cases.json: the errors WebQuiz 1.19 gives for quiz data before a save.

Run it with the Python 1.19 project's virtual environment, giving the project folder:

    cd /path/to/webquiz && source venv/bin/activate
    python -I /path/to/webquiz-go/internal/quiz/testdata/gen_validate_cases.py /path/to/webquiz

The Go test TestValidateMatchesPython decodes each "data" with json.Decoder.UseNumber
(so 1 and 1.0 stay different, as in Python) and checks that quiz.Validate gives
the same errors. Cases where Python crashed instead of giving errors are left out.
"""

import json
import os
import sys

sys.path.insert(0, os.path.abspath(sys.argv[1] if len(sys.argv) > 1 else "."))

from webquiz.config import WebQuizConfig  # noqa: E402
from webquiz.server import TestingServer  # noqa: E402


def q(**fields):
    question = {"question": "Q", "options": ["A", "B", "C"], "correct_answer": 0}
    question.update(fields)
    return {k: v for k, v in question.items() if v is not DROP}


def text_q(**fields):
    question = {"question": "Q", "checker": ""}
    question.update(fields)
    return {k: v for k, v in question.items() if v is not DROP}


DROP = object()

CASES = [
    None, [], "text", 5,
    {}, {"questions": None}, {"questions": {}}, {"questions": "x"}, {"questions": []},
    {"questions": [5]},
    {"questions": [q()]},
    {"questions": [q(question=DROP)]},
    {"questions": [q(question=DROP, image="/imgs/a.png")]},
    {"questions": [q(question="", image="")]},
    {"questions": [q(question=0)]},
    {"questions": [q(question=None, image=None)]},
    {"questions": [{"question": "Q"}]},
    {"questions": [q(options=DROP)]},
    {"questions": [q(correct_answer=DROP)]},
    {"questions": [q(options="AB")]},
    {"questions": [q(options={"a": 1, "b": 2}, correct_answer=1)]},
    {"questions": [q(options=["A"])]},
    {"questions": [q(options=["A", 1])]},
    {"questions": [q(options=["A", None])]},
    {"questions": [q(correct_answer=3)]},
    {"questions": [q(correct_answer=-1)]},
    {"questions": [q(correct_answer=True)]},
    {"questions": [q(correct_answer=1.0)]},
    {"questions": [q(correct_answer="0")]},
    {"questions": [q(correct_answer=None)]},
    {"questions": [q(correct_answer={"a": 1})]},
    {"questions": [q(correct_answer=[])]},
    {"questions": [q(correct_answer=[0, "1"])]},
    {"questions": [q(correct_answer=[0, 1.0])]},
    {"questions": [q(correct_answer=[0, 3])]},
    {"questions": [q(correct_answer=[-1, 0])]},
    {"questions": [q(correct_answer=[0, 0])]},
    {"questions": [q(correct_answer=[1, True])]},
    {"questions": [q(correct_answer=[0, 2])]},
    {"questions": [q(correct_answer=[0, 2], min_correct=1)]},
    {"questions": [q(correct_answer=[0, 2], min_correct=2)]},
    {"questions": [q(correct_answer=[0, 2], min_correct=0)]},
    {"questions": [q(correct_answer=[0, 2], min_correct=3)]},
    {"questions": [q(correct_answer=[0, 2], min_correct=1.5)]},
    {"questions": [q(correct_answer=[0, 2], min_correct="1")]},
    {"questions": [q(correct_answer=[0, 2], min_correct=True)]},
    {"questions": [q(correct_answer=[0, 2], min_correct=False)]},
    {"questions": [q(correct_answer=[0, 2], min_correct=None)]},
    {"questions": [q(correct_answer=1, min_correct=1)]},
    {"questions": [q(correct_answer=DROP, min_correct=1)]},
    {"questions": [q(points=1)]},
    {"questions": [q(points=0)]},
    {"questions": [q(points=-1)]},
    {"questions": [q(points=1.5)]},
    {"questions": [q(points=1.25)]},
    {"questions": [q(points=1.255)]},
    {"questions": [q(points=0.1)]},
    {"questions": [q(points=0.125)]},
    {"questions": [q(points=2.675)]},
    {"questions": [q(points=0.001)]},
    {"questions": [q(points=100)]},
    {"questions": [q(points=1e2)]},
    {"questions": [q(points=True)]},
    {"questions": [q(points="2")]},
    {"questions": [q(points=None)]},
    {"questions": [q(points=[1])]},
    {"questions": [q(stick_to_the_previous=True)]},
    {"questions": [q(stick_to_the_previous=False)]},
    {"questions": [q(), q(stick_to_the_previous=True)]},
    {"questions": [q(), q(stick_to_the_previous="yes")]},
    {"questions": [q(), q(stick_to_the_previous=1)]},
    {"questions": [q(), q(stick_to_the_previous=None)]},
    {"questions": [q()], "show_right_answer": "no"},
    {"questions": [q()], "randomize_questions": 1},
    {"questions": [q()], "show_answers_on_completion": None},
    {"questions": [q()], "show_final_list": "x"},
    {"questions": [q()], "show_explanation": "yes"},
    {"questions": [q()], "show_right_answer": False, "randomize_questions": True,
     "show_answers_on_completion": True, "show_final_list": False, "show_explanation": True},
    {"questions": [q()], "title": 5},
    {"questions": [q()], "title": None},
    {"questions": [q()], "title": "Тест"},
    {"questions": [q(explanation=5)]},
    {"questions": [q(explanation=None)]},
    {"questions": [q(explanation=["x"])]},
    {"questions": [q(explanation="**Why**")]},
    {"questions": [q()], "time_limit": {"per_question": 30}},
    {"questions": [q()], "time_limit": {"whole_quiz": 1.5}},
    {"questions": [q()], "time_limit": {"minutes": 1}},
    {"questions": [q()], "time_limit": 30},
    {"questions": [q(time_limit=-5)]},
    {"questions": [q(time_limit=10)]},
    {"questions": [text_q()]},
    {"questions": [text_q(checker=5)]},
    {"questions": [text_q(checker=None)]},
    {"questions": [text_q(default_value=5, correct_value=6)]},
    {"questions": [text_q(default_value="a", correct_value="b")]},
    {"questions": [text_q(checker="assert user_answer.strip() == '5', 'no'")]},
    {"questions": [text_q(checker="if True print(1)")]},
    {"questions": [text_q(checker="x = (1,\ny = 2")]},
    {"questions": [text_q(question=DROP)]},
    {"questions": [text_q(options=["A"], correct_answer=5, min_correct=0)]},
    {"questions": [text_q(points=0)]},
    {"questions": [q(question=DROP, options=["A"], correct_answer=[], points=0, stick_to_the_previous=True),
                   "x", q(correct_answer=[0, 0], min_correct=5)],
     "show_right_answer": "no", "title": 1, "time_limit": {}},
]


def main():
    server = TestingServer(WebQuizConfig())
    results = []
    for data in CASES:
        errors = []
        try:
            valid = server._validate_quiz_data(data, errors)
        except Exception as error:  # Python crashed: the case says nothing about the rules
            print(f"skipped {data!r}: {type(error).__name__}: {error}")
            continue
        results.append({"data": data, "valid": valid, "errors": errors})
    path = os.path.join(os.path.dirname(os.path.abspath(__file__)), "validate_cases.json")
    with open(path, "w", encoding="utf-8") as f:
        json.dump(results, f, ensure_ascii=False, indent=1)
        f.write("\n")
    print(f"wrote {len(results)} cases to {path}")


if __name__ == "__main__":
    main()
