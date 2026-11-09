"""Write config_cases.json: how WebQuiz 1.19 checks webquiz.yaml and merges form updates into it.

Run it with the Python 1.19 project's virtual environment, giving the project folder:

    cd /path/to/webquiz && source venv/bin/activate
    python -I /path/to/webquiz-go/internal/config/testdata/gen_config_cases.py /path/to/webquiz

"validate" holds the errors of _validate_config_data for config data (the Go test
decodes it with UseNumber). "merge" holds partial updates (PUT /api/admin/config
with {"data": ...}): the original file, the update, and what PyYAML reads from the
file that ruamel.yaml wrote. The Go test checks that config.MergeYAML gives a file
that reads the same.
"""

import json
import os
import sys
from io import StringIO

sys.path.insert(0, os.path.abspath(sys.argv[1] if len(sys.argv) > 1 else "."))

import yaml  # noqa: E402
from ruamel.yaml import YAML as RuamelYAML  # noqa: E402

from webquiz.config import WebQuizConfig  # noqa: E402
from webquiz.server import TestingServer  # noqa: E402

VALIDATE = [
    None, {}, [], "text", 5,
    {"language": "uk"}, {"language": "en"}, {"language": "ru"}, {"language": "fr"}, {"language": None},
    {"language": 1},
    {"extra_answers_with_users_csv": True}, {"extra_answers_with_users_csv": "yes"},
    {"server": None}, {"server": []}, {"server": {}},
    {"server": {"host": "0.0.0.0", "port": 8080}}, {"server": {"host": 5}},
    {"server": {"port": "8080"}}, {"server": {"port": 0}}, {"server": {"port": 65536}},
    {"server": {"port": 65535}}, {"server": {"port": True}}, {"server": {"port": 8080.0}},
    {"server": {"include_ipv6": "x", "url_format": 5}},
    {"paths": "x"}, {"paths": {"quizzes_dir": 1, "logs_dir": None, "csv_dir": "d", "static_dir": []}},
    {"admin": 5}, {"admin": {"master_key": None}}, {"admin": {"master_key": 123}},
    {"admin": {"trusted_ips": "127.0.0.1"}}, {"admin": {"trusted_ips": ["127.0.0.1", 5]}},
    {"admin": {"trusted_ips": []}},
    {"registration": None}, {"registration": {"fields": "Grade"}},
    {"registration": {"fields": ["Grade", 1]}}, {"registration": {"disabled_fields": {}}},
    {"registration": {"disabled_fields": [None]}}, {"registration": {"approve": "yes"}},
    {"registration": {"approve": False, "username_label": 5}},
    {"registration": {"fields": ["Клас"], "disabled_fields": ["Школа"], "approve": True, "username_label": "Ім'я"}},
    {"quizzes": {}}, {"quizzes": None}, {"quizzes": [5]}, {"quizzes": [{}]},
    {"quizzes": [{"name": "a", "download_path": "b", "folder": "c"}]},
    {"quizzes": [{"name": 1, "download_path": None, "folder": []}]},
    {"quizzes": [{"name": "a", "download_path": "b", "folder": "c", "auth_header_token": 5}]},
    {"quizzes": [{"name": "a", "download_path": "b", "folder": "c", "auth_header_token": "t"}, "x"]},
    {"tunnel": 5, "checker_templates": 5},
    {"language": "ru", "server": {"port": 0}, "paths": [], "admin": {"trusted_ips": [1]},
     "registration": {"approve": 1}, "quizzes": [{"name": "a"}]},
]


def ruamel_merge(content, merge_data):
    """The partial update of admin_update_config in webquiz/server.py (1.19), unchanged."""
    rt_yaml = RuamelYAML()
    rt_yaml.preserve_quotes = True
    rt_yaml.allow_unicode = True
    rt_yaml.indent(mapping=2, sequence=4, offset=2)
    current_config = None
    try:
        if content.strip():
            current_config = rt_yaml.load(content)
    except Exception:
        current_config = None
    if not isinstance(current_config, dict):
        current_config = rt_yaml.load("{}\n") or {}
    for section_key, section_value in merge_data.items():
        if isinstance(section_value, dict) and isinstance(current_config.get(section_key), dict):
            for k, v in section_value.items():
                current_config[section_key][k] = v
        else:
            current_config[section_key] = section_value
    buf = StringIO()
    rt_yaml.dump(current_config, buf)
    return buf.getvalue()


EXAMPLE = open(os.path.join(sys.argv[1] if len(sys.argv) > 1 else ".", "webquiz", "server_config.yaml.example"),
               encoding="utf-8").read()

FORM = {"language": "en", "extra_answers_with_users_csv": True,
        "registration": {"approve": True, "username_label": "Ім'я", "fields": ["Клас", "Школа"], "disabled_fields": []}}

MERGE = [
    (EXAMPLE, FORM),
    (EXAMPLE, {"server": {"port": 9000}, "admin": {"trusted_ips": ["127.0.0.1", "10.0.0.5"]}}),
    ("", FORM),
    ("   \n", {"language": "uk"}),
    ("# only a comment\n", {"language": "uk"}),
    ("- a\n- b\n", {"language": "en"}),
    ("just text\n", {"language": "en"}),
    ("a: [1, 2\n", {"language": "en"}),
    ("language: uk   # the language\n\nregistration:\n  approve: false   # need approval?\n"
     "  # a comment before fields\n  fields:\n    - Class    # first\n    # between\n    - School\n"
     "  username_label: \"Name\"\n\n# trailing comment\n",
     {"language": "en", "registration": {"approve": True, "fields": ["A", "B"], "disabled_fields": ["C"],
                                         "username_label": ""}}),
    ("registration:\n  approve: false\nserver:\n  port: 8080\n", {"registration": {"approve": True}, "server": {"port": 9000}}),
    ("language: uk\nregistration: null\n", {"registration": {"approve": True, "fields": []}}),
    ("language: uk\nregistration:\n# approve: true\nserver:\n  port: 1\n", {"registration": {"approve": True}}),
    ("server: {port: 8080}\n", {"server": {"host": "1.2.3.4"}}),
    ("registration:\n  fields: [A, B]\n", {"registration": {"fields": ["X"]}}),
    ("admin:\n  trusted_ips:\n  - 127.0.0.1\n  - 10.0.0.1\nregistration:\n  fields:\n  - A\n",
     {"registration": {"fields": ["X", "Y"]}}),
    ("language: uk", {"language": "en"}),
    ("registration:\n  approve: false", {"registration": {"username_label": "Ім'я", "fields": ["1", "a: b", "#x", "", "true", "null", "@x", "- y", "multi\nline"]}}),
    ("language: uk\n", {"registration": {"approve": True}, "server": None, "quizzes": [{"name": "Q", "download_path": "https://e.com/q.zip", "folder": "q/"}]}),
    ("language: 'uk'\nextra_answers_with_users_csv: false\n", {"language": "en", "extra_answers_with_users_csv": True}),
    ("registration:\n  approve: false\n  fields:\n    - A\n  username_label: x\nlanguage: uk\n",
     {"registration": "replaced"}),
    ("registration:\n  approve: false\n", {"registration": {"nested": {"a": 1}, "list": [{"x": 1}, {"y": [1, 2]}]}}),
    ("server:\n  host: \"0.0.0.0\"  # all\n  port: 8080       # port\n", {"server": {"host": "127.0.0.1", "port": 8081}}),
    ("checker_templates:\n  - name: Int\n    code: |\n      assert int(user_answer) == 5\n      # inside the code\n\nlanguage: uk\n",
     {"checker_templates": [{"name": "New", "code": "assert True"}]}),
    ("registration:\n  username_label: |\n    long\n    label\n  approve: true\n", {"registration": {"username_label": "short"}}),
    ("language: uk\r\nregistration:\r\n  approve: false\r\n", {"registration": {"approve": True}, "language": "en"}),
    ("registration:\n  approve: false\nregistration:\n  approve: true\n", {"language": "en"}),
]


def main():
    server = TestingServer(WebQuizConfig())
    validate = []
    for data in VALIDATE:
        errors = []
        valid = server._validate_config_data(data, errors)
        validate.append({"data": data, "valid": valid, "errors": errors})
    merge = []
    for original, data in MERGE:
        output = ruamel_merge(original.replace("\r\n", "\n"), data)  # the server read the file in text mode
        merge.append({"original": original, "data": data, "ruamel_output": output, "parsed": yaml.safe_load(output)})
    path = os.path.join(os.path.dirname(os.path.abspath(__file__)), "config_cases.json")
    with open(path, "w", encoding="utf-8") as f:
        json.dump({"validate": validate, "merge": merge}, f, ensure_ascii=False, indent=1)
        f.write("\n")
    print(f"wrote {len(validate)} validate and {len(merge)} merge cases to {path}")


if __name__ == "__main__":
    main()
