"""Write mistune_cases.json: how WebQuiz 1.19 renders explanation Markdown.

Run it with the Python 1.19 project's virtual environment, giving the project folder:

    cd /path/to/webquiz && source venv/bin/activate
    python -I /path/to/webquiz-go/internal/explanations/testdata/gen_mistune_cases.py /path/to/webquiz

The Go test TestRenderMatchesMistune reads the JSON file and checks that
explanations.Render gives the same HTML (it ignores line breaks next to tags).
"""

import json
import os
import sys

sys.path.insert(0, os.path.abspath(sys.argv[1] if len(sys.argv) > 1 else "."))

from webquiz.explanations import ADMIN_IMAGES_URL, STUDENT_IMAGES_URL, render_explanation  # noqa: E402

CASES = [
    "**b** text\n\n- one\n- two",
    "| a | b |\n|---|---|\n| 1 | 2 |",
    "<script>alert(1)</script>\n\n<img src=x onerror=alert(1)>",
    "see [x](javascript:alert) now",
    "[site](https://e.com)",
    "![a](/imgs/a.png)",
    "![a](</imgs/my pic.png>)",
    "![a](https://e.com/a.png)",
    "~~gone~~ *em* `code`",
    "# H1\n\n## H2\n\ntext",
    '[t](https://e.com "Ti&tle")',
    "a <b>bold</b> c",
    "1. one\n2. two",
    "```\ncode <x>\n```",
    "> quote",
    "line1\nline2",
    "line1  \nline2",
    "---",
    '![alt](/imgs/a.png "t")',
    "[x](vbscript:foo)",
    "[x](data:text/html,foo)",
    "[x](data:image/png;base64,AAA)",
    "![x](data:image/png;base64,AAA)",
    "![x](javascript:alert)",
    "<https://e.com>",
    "https://e.com",
    "[x](/relative/path)",
    "[x](#anchor)",
    '&amp; & < > "',
    "| a | b |\n|:--|--:|\n| 1 | 2 |",
    "| a | b | c |\n|:-:|---|:--|\n| 1 | 2 | 3 |",
    "",
    "   ",
    "[x](JAVASCRIPT:alert)",
    "[x](  javascript:alert  )",
    "[x](file:///etc/passwd)",
    "<!-- comment -->",
    "text <!-- c --> more",
    "a\\*b",
    "* a\n* b\n\n  para",
    "- [ ] task",
    "Setext\n===",
    "    indented code",
    "[ref]\n\n[ref]: https://e.com",
    "x ~~y~~ z",
    "**[link](https://e.com)**",
    "[x](https://e.com/a_b?c=1&d=2)",
    "&#65; &copy; &nbsp; \\& a\\<b",
    "`a&amp;b`",
    '[t](https://e.com "a &quot;q&quot;")',
    "![*em* `c` <b>h</b>](/imgs/a.png)",
    "[a <b>h</b> *e*](https://e.com)",
    "[x](%6Aavascript:alert)",
    "[x](JaVaScRiPt:alert)",
    "[x](<javascript:alert>)",
    "[x](mailto:a@b.c)",
    "<mailto:a@b.c>",
    "<a@b.c>",
    "[x](ftp://e.com)",
    "[x](e.com/a:b)",
    "[x](a:b)",
    "[x](<https://e.com/ü ö>)",
    "[x](https://e.com/ü)",
    "![a](/imgs/ü.png)",
    "![a](/imgs/a'b.png)",
    "![a](/imgs/a[1].png)",
    "![a](/imgs/a{1}.png)",
    "![a](/imgs/a|b.png)",
    '[x](https://e.com/"q")',
    "[x](https://e.com/50%off)",
    "*   one\n*   two",
    "- a\n\n- b",
    "Text with ~single~ tilde",
    "a~b~c",
    "~~a~~",
    "~~~a~~~",
    "1) one\n2) two",
    "3. three\n4. four",
    "```py\nx = 1\n```",
    "``` py extra\nx\n```",
    "<div>\n*x*\n</div>",
    "<div>block</div>\ntext after",
    "text\n<div>x</div>",
    "| a |\n|---|\n| <b>x</b> |",
    "a | b\n--|--\n1 | 2",
    "***bold italic***",
    "__u__ _e_",
    "[x]: https://e.com\n[y][x]",
    "![x][i]\n\n[i]: /imgs/r.png",
    "  - nested\n    - deeper",
    "> - q list",
    "Hard\\\nbreak",
    "<https://e.com/a_b>",
    "<javascript:alert(1)>",
    "x\n\n<!-- c -->\n\ny",
    "<?php echo 1; ?>",
    "<![CDATA[x]]>",
    "<br>",
    "a<br/>b",
    "Пояснення: **2 + 2 = 4**, бо *так*.",
    "![схема](/imgs/схема.png)",
    "![a](/imgs/pic(1).png) and ![b](/imgs/plain.png)",
    "![a](</imgs/my map.png>) ![b](</imgs/s_pic(1).png>)",
    "Formula: `x**2` and x\\_1",
    "[link](https://e.com/a b)",
    "[x](?q=1)",
    "[x](tel:+380)",
    "[x](sms:123)",
    "![x](javascript:alert \"t\")",
    "<img src=x>text",
    "a\n\n\n\nb",
    "Line with trailing spaces   ",
    "\tcode with tab",
    "1. a\n\n   b\n2. c",
]


def main():
    results = []
    for text in CASES:
        results.append({"markdown": text, "images_url": STUDENT_IMAGES_URL, "html": render_explanation(text)})
    for text in ["![a](/imgs/a.png)", "![a](</imgs/my pic.png>) <b>x</b>", "![a](https://e.com/a.png)"]:
        results.append({"markdown": text, "images_url": ADMIN_IMAGES_URL, "html": render_explanation(text, ADMIN_IMAGES_URL)})
    path = os.path.join(os.path.dirname(os.path.abspath(__file__)), "mistune_cases.json")
    with open(path, "w", encoding="utf-8") as f:
        json.dump(results, f, ensure_ascii=False, indent=1)
        f.write("\n")
    print(f"wrote {len(results)} cases to {path}")


if __name__ == "__main__":
    main()
