package i18n

import (
	"sort"
	"strings"
	"testing"
)

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// py: tests/test_language.py::test_uk_and_en_have_same_keys
func TestUkAndEnHaveSameKeys(t *testing.T) {
	uk, en := Get("uk"), Get("en")
	for key := range uk {
		if _, ok := en[key]; !ok {
			t.Errorf("missing in en: %s", key)
		}
	}
	for key := range en {
		if _, ok := uk[key]; !ok {
			t.Errorf("missing in uk: %s", key)
		}
	}
}

// py: tests/test_language.py::test_get_translations_uk
func TestGetTranslationsUk(t *testing.T) {
	if got := Get("uk")["loading"]; got != "Завантаження..." {
		t.Errorf("uk loading = %q", got)
	}
}

// py: tests/test_language.py::test_get_translations_en
func TestGetTranslationsEn(t *testing.T) {
	if got := Get("en")["loading"]; got != "Loading..." {
		t.Errorf("en loading = %q", got)
	}
}

// py: tests/test_language.py::test_get_translations_fallback
func TestGetTranslationsFallback(t *testing.T) {
	for _, lang := range []string{"fr", "", "ru", "UK"} {
		if got := Get(lang)["loading"]; got != "Завантаження..." {
			t.Errorf("Get(%q) should fall back to Ukrainian, loading = %q", lang, got)
		}
	}
}

// py: tests/test_language.py::test_no_empty_translations
func TestNoEmptyTranslations(t *testing.T) {
	for _, lang := range Languages {
		for key, value := range Get(lang) {
			if value == "" {
				t.Errorf("%s.%s is empty", lang, key)
			}
		}
	}
}

func TestLanguagesAndKeyCount(t *testing.T) {
	if len(Languages) != 2 || Languages[0] != "uk" || Languages[1] != "en" {
		t.Errorf("Languages = %v", Languages)
	}
	if Default != "uk" {
		t.Errorf("Default = %q", Default)
	}
	// translations.py has 91 keys in each language (70, and 21 for the manual
	// check of answers). 2.0 adds 2: the answer verdict.
	if n := len(Get("uk")); n != 93 {
		t.Errorf("uk has %d keys, want 93 (the 91 of translations.py and 2 new ones): %v", n, keys(Get("uk")))
	}
}

// The texts of the manual check: the student page and the review page (as in translations.py).
func TestManualCheckTexts(t *testing.T) {
	cases := map[string]map[string]string{
		"uk": {
			"pending_review_notice": "⏳ Відповідей чекає ручної перевірки: {n}. Сторінка оновиться сама.",
			"review_help_text":      "Відповіді, відкриті на інших сторінках перевірки: {n}. Можна допомогти: тоді ви бачите ту саму відповідь, і зараховується перше рішення.",
			"review_title":          "✍️ Перевірка відповідей",
			"review_help_button":    "🤝 Допомогти з перевіркою",
		},
		"en": {
			"pending_review_saved": "✍️ Your answer is saved. It will be checked manually; you will see the result at the end of the quiz.",
			"pending_review_row":   "⏳ Waiting for manual verification",
			"review_waiting_count": "Waiting for a check: {n}",
			"review_login_error":   "Wrong key",
		},
	}
	for lang, texts := range cases {
		for key, want := range texts {
			if got := Get(lang)[key]; got != want {
				t.Errorf("%s.%s = %q, want %q", lang, key, got, want)
			}
		}
	}
}

// The verdict next to the "Continue" button after an answer (new in 2.0).
func TestAnswerVerdictTexts(t *testing.T) {
	cases := map[string]map[string]string{
		"uk": {"answer_verdict_correct": "✓ Правильно", "answer_verdict_incorrect": "✗ Неправильно"},
		"en": {"answer_verdict_correct": "✓ Correct", "answer_verdict_incorrect": "✗ Incorrect"},
	}
	for lang, texts := range cases {
		for key, want := range texts {
			if got := Get(lang)[key]; got != want {
				t.Errorf("%s.%s = %q, want %q", lang, key, got, want)
			}
		}
	}
}

func TestGetReturnsACopy(t *testing.T) {
	Get("en")["loading"] = "changed"
	if Get("en")["loading"] != "Loading..." {
		t.Error("changing the map from Get must not change the texts")
	}
}

func TestTextsMatchPython(t *testing.T) {
	// A few texts with quotes, placeholders and emoji, copied from translations.py.
	cases := map[string]map[string]string{
		"uk": {
			"username_label":          "Ім'я користувача",
			"please_fill_field":       `Будь ласка, заповніть поле "{field}"`,
			"question_n_of_m":         "Питання {n} з {m}",
			"explanation_title":       "📖 Пояснення",
			"connection_lost":         "Немає з'єднання з сервером. Повторна спроба...",
			"server_update_not_found": "User not found",
			"answers_available_later": "ℹ️ Правильні відповіді будуть доступні після завершення тесту всіма учнями.",
		},
		"en": {
			"username_label":      "Username",
			"please_fill_field":   `Please fill in the "{field}" field`,
			"points_2_4":          "points",
			"server_empty_answer": "An empty answer is not accepted",
		},
	}
	for lang, texts := range cases {
		for key, want := range texts {
			if got := Get(lang)[key]; got != want {
				t.Errorf("%s.%s = %q, want %q", lang, key, got, want)
			}
		}
	}
}

// Students and review pages see "manual verification", never a "teacher"
// (test_texts_never_say_teacher of WebQuiz 1.x).
func TestManualCheckTextsNeverSayTeacher(t *testing.T) {
	for _, lang := range Languages {
		for key, text := range Get(lang) {
			if !strings.HasPrefix(key, "pending_review") && !strings.HasPrefix(key, "review_") {
				continue
			}
			lowered := strings.ToLower(text)
			for _, word := range []string{"teacher", "вчител", "учител"} {
				if strings.Contains(lowered, word) {
					t.Errorf("%s.%s says %q: %q", lang, key, word, text)
				}
			}
		}
	}
}
