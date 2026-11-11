package i18n

import "testing"

// The menu of the tray icon of the desktop version (new in 2.0).
func TestTrayTexts(t *testing.T) {
	cases := map[string]map[string]string{
		"uk": {
			"tray_open_admin":      "Відкрити панель адміністратора",
			"tray_open_live_stats": "Відкрити живу статистику",
			"tray_open_files":      "Відкрити менеджер файлів",
			"tray_quit":            "Вийти",
			"tray_error":           "Помилка: {error}",
			"tray_port_busy":       "порт {port} зайнятий іншою програмою. Можливо, WebQuiz уже запущено",
			"tray_data_folder":     "Папка з даними: {folder}",
			"tray_see_log":         "Подробиці є в журналі: {log}",
		},
		"en": {
			"tray_open_admin":      "Open admin panel",
			"tray_open_live_stats": "Open live stats",
			"tray_open_files":      "Open file manager",
			"tray_quit":            "Quit",
			"tray_error":           "Error: {error}",
			"tray_port_busy":       "port {port} is used by another program. Maybe WebQuiz is already running",
			"tray_data_folder":     "Data folder: {folder}",
			"tray_see_log":         "Details are in the log file: {log}",
		},
	}
	for lang, texts := range cases {
		got := Tray(lang)
		if len(got) != len(texts) {
			t.Errorf("%s has %d tray texts, want %d: %v", lang, len(got), len(texts), keys(got))
		}
		for key, want := range texts {
			if got[key] != want {
				t.Errorf("%s.%s = %q, want %q", lang, key, got[key], want)
			}
		}
	}
}

func TestTrayTextsFallBackToUkrainian(t *testing.T) {
	for _, lang := range []string{"", "fr", "EN"} {
		if got := Tray(lang)["tray_quit"]; got != "Вийти" {
			t.Errorf("Tray(%q) tray_quit = %q, want the Ukrainian text", lang, got)
		}
	}
}

// The tray texts are not page texts: the quiz page does not get them.
func TestTrayTextsAreNotPageTexts(t *testing.T) {
	for _, lang := range Languages {
		for key := range Tray(lang) {
			if _, ok := Get(lang)[key]; ok {
				t.Errorf("%s: %s is also a page text", lang, key)
			}
		}
	}
}

func TestTrayReturnsACopy(t *testing.T) {
	Tray("en")["tray_quit"] = "changed"
	if Tray("en")["tray_quit"] != "Quit" {
		t.Error("changing the map from Tray must not change the texts")
	}
}
