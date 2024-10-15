package utils

import (
	"strings"
	"unicode"
)

func RemoveSpacesAndNewlines(s string) string {
	// Use strings.Map to remove non-printable characters
	cleanText := strings.Map(func(r rune) rune {
		if r <= unicode.MaxASCII {
			return r
		}
		return -1
	}, s)
	return strings.Join(strings.Fields(cleanText), "")
}

// TODO: create a go workspace, and, in a specific project with the Vision Ecosystem,
// put this, in a `utils` package.
func ParseTicker(ticker string) string {
	return strings.TrimLeft(strings.ToUpper(ticker), "$")
}
