package terminal

import (
	"strings"
	"unicode"
)

// Safe removes terminal control and Unicode formatting characters from text
// received from a configuration file or Git process.
func Safe(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return ' '
		}
		return r
	}, value)
}
