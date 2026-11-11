package i18n

import "maps"

// The texts of the tray menu of the desktop version (new in 2.0). The tray is
// not a page, so Get does not return these texts and the quiz page does not
// get them. Keep both languages with the same keys. {error}, {port},
// {folder} and {log} are filled in by internal/desktop.
var trayTexts = map[string]map[string]string{
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

// Tray returns the texts of the tray menu of the desktop version in a
// language (new in 2.0). An unknown language gives the Ukrainian texts, as
// with Get. The map is a copy.
func Tray(lang string) map[string]string {
	found, ok := trayTexts[lang]
	if !ok {
		found = trayTexts[Default]
	}
	return maps.Clone(found)
}
