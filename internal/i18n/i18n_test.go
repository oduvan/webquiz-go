package i18n

import (
	"sort"
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
	// translations.py has 70 keys in each language.
	if n := len(Get("uk")); n != 70 {
		t.Errorf("uk has %d keys, translations.py has 70: %v", n, keys(Get("uk")))
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
