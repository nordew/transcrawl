package storage

import "strings"

var cyrillicToLatin = map[rune]string{
	'а': "a", 'б': "b", 'в': "v", 'г': "h", 'ґ': "g", 'д': "d",
	'е': "e", 'є': "ie", 'ё': "e", 'ж': "zh", 'з': "z", 'и': "y",
	'і': "i", 'ї': "i", 'й': "i", 'к': "k", 'л': "l", 'м': "m",
	'н': "n", 'о': "o", 'п': "p", 'р': "r", 'с': "s", 'т': "t",
	'у': "u", 'ф': "f", 'х': "kh", 'ц': "ts", 'ч': "ch", 'ш': "sh",
	'щ': "shch", 'ъ': "", 'ы': "y", 'ь': "", 'э': "e", 'ю': "iu",
	'я': "ia",
}

func transliterate(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if latin, ok := cyrillicToLatin[r]; ok {
			b.WriteString(latin)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}
