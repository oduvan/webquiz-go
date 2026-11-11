// Package i18n holds the texts that students see, in Ukrainian and English.
//
// The texts are copied from webquiz/translations.py of WebQuiz 1.19, with the
// same keys. 2.0 adds a few keys of its own, marked "new in 2.0" in texts.go.
// Get gives the texts of one language; an unknown language gives
// the Ukrainian texts, as in the Python version. Languages lists the language
// codes the config may use, and Default is the language when none is set.
//
// Tray gives the texts of the tray menu of the desktop version (new in 2.0,
// tray.go). They are kept apart from the page texts, so the quiz page does
// not get them.
package i18n
