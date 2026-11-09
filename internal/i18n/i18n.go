package i18n

import "maps"

// Default is the language used when the config sets none or an unknown one.
const Default = "uk"

// Languages are the language codes the config may set.
var Languages = []string{"uk", "en"}

var texts = map[string]map[string]string{
	"uk": ukTexts,
	"en": enTexts,
}

// Get returns the texts of a language, keyed like translations.py. An unknown
// language gives the Ukrainian texts. The map is a copy: the caller may keep
// or change it.
func Get(lang string) map[string]string {
	found, ok := texts[lang]
	if !ok {
		found = texts[Default]
	}
	return maps.Clone(found)
}
