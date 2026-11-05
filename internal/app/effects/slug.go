package effects

import (
	"errors"
	"strings"
	"unicode/utf8"
)

// maxSlug is the longest slug of a branch name.
const maxSlug = 40

// Slug turns a ticket title into the slug of a branch name: lower case ASCII letters and digits, with runs of any other
// byte collapsed into one hyphen. It has no leading or trailing hyphen and at most maxSlug characters. A title with no
// ASCII letter or digit has no slug, and an error.
func Slug(title string) (string, error) {
	var b strings.Builder
	for i := 0; i < len(title); i++ {
		c := title[i]
		switch {
		case c >= 'A' && c <= 'Z':
			b.WriteByte(c + ('a' - 'A'))
		case (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9'):
			b.WriteByte(c)
		default:
			// Every other byte, including each byte of a non-ASCII rune, ends a word.
			if s := b.String(); s != "" && s[len(s)-1] != '-' {
				b.WriteByte('-')
			}
		}
	}
	s := strings.TrimRight(b.String(), "-")
	if len(s) > maxSlug {
		s = strings.TrimRight(s[:maxSlug], "-")
	}
	if s == "" {
		return "", errors.New("title has no ASCII letter or digit")
	}
	return s, nil
}

// truncate shortens s to at most n bytes without cutting a UTF-8 sequence.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}
