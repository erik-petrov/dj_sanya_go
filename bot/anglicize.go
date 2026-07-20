package bot

import (
	"strings"
	"unicode"
)

// GigaAM transcribes Russian only, so spoken English song titles come out as
// Cyrillic phonetics ("бохемиан рапсоди"). anglicize transliterates that back
// into Latin ("bohemian rapsodi") — imperfect, but close enough for YouTube's
// fuzzy search. Used automatically when a Cyrillic query finds nothing, and on
// demand via an "англ"/"инглиш" marker in a voice command.
var translit = map[rune]string{
	'а': "a", 'б': "b", 'в': "v", 'г': "g", 'д': "d", 'е': "e", 'ё': "yo",
	'ж': "zh", 'з': "z", 'и': "i", 'й': "y", 'к': "k", 'л': "l", 'м': "m",
	'н': "n", 'о': "o", 'п': "p", 'р': "r", 'с': "s", 'т': "t", 'у': "u",
	'ф': "f", 'х': "h", 'ц': "ts", 'ч': "ch", 'ш': "sh", 'щ': "sch",
	'ъ': "", 'ы': "y", 'ь': "", 'э': "e", 'ю': "yu", 'я': "ya",
}

func hasCyrillic(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Cyrillic, r) {
			return true
		}
	}
	return false
}

func anglicize(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if lat, ok := translit[r]; ok {
			b.WriteString(lat)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}
