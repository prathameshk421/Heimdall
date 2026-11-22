package summary

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCleanMakesOneLine(t *testing.T) {
	require.Equal(t, "a b c", clean("  a\n\n b\t\rc  ", 100))
	require.Equal(t, "ab", clean("a\u200bb", 100), "a zero width space is removed")
	require.Equal(t, "evil", clean("\u202eevil\u2069", 100), "bidirectional overrides are removed")
	require.Equal(t, "a b", clean("a\xffb", 100), "a byte that is not UTF-8 becomes a space")
	require.Equal(t, "a [0m b", clean("a\x00\x1b[0m\x7fb", 100), "control characters become spaces")
	require.Equal(t, "abcd…", clean("abcdefghij", 5))
	require.Equal(t, "日本語…", clean("日本語テスト", 4), "a cut is by rune, never inside one")
	require.Empty(t, clean(" \n\t ", 10))
}

func TestEscapeTable(t *testing.T) {
	for in, want := range map[string]string{
		"plain words 1.2":      "plain words 1.2",
		"a < b && c > d":       "a &lt; b &amp;&amp; c &gt; d",
		"call `f()` now":       "call \\`f\\(\\)\\` now",
		"[x](y)":               "\\[x\\]\\(y\\)",
		"![x](y)":              "\\!\\[x\\]\\(y\\)",
		"*bold* _it_ ~~gone~~": "\\*bold\\* \\_it\\_ \\~\\~gone\\~\\~",
		"a | b":                "a \\| b",
		"$x$":                  "\\$x\\$",
		`back\slash`:           `back\\slash`,
		"@team":                "@" + zwsp + "team",
		"#12":                  `\#` + zwsp + "12",
		"# title":              `\# title`,
		"https://x.example":    "https:" + zwsp + "//x.example",
		"WWW.x.example":        "WWW" + zwsp + ".x.example",
	} {
		require.Equal(t, want, escape(in), in)
	}
}

func TestCodeSpanCannotBeClosed(t *testing.T) {
	for _, in := range []string{"a`b", "a``b", "`a`", "a```b`c", "plain.go:12", "`", "x\n`y"} {
		got := code(in, 100)
		fence := got[:strings.IndexFunc(got, func(r rune) bool { return r != '`' })]
		require.NotEmpty(t, fence)
		inner := strings.TrimSpace(got[len(fence) : len(got)-len(fence)])
		require.Equal(t, clean(in, 100), inner, "the content is kept as it is")
		longest := 0
		run := 0
		for _, r := range inner {
			if r == '`' {
				run++
				longest = max(longest, run)
			} else {
				run = 0
			}
		}
		require.Greater(t, len(fence), longest, "the fence is longer than any run of backticks inside: %q", got)
	}
	require.Empty(t, code("   ", 10))
}

func TestBreakWWWOnlyBeforeADot(t *testing.T) {
	require.Equal(t, "www"+zwsp+".a www"+zwsp+".", breakWWW("www.a www."))
	require.Equal(t, "awww b", breakWWW("awww b"))
	require.Equal(t, "xWWW"+zwsp+".y", breakWWW("xWWW.y"))
	require.Equal(t, "\u0130www"+zwsp+".example.com", breakWWW("\u0130www.example.com"))
	require.Equal(t, "\u0130\u0130WWW"+zwsp+".a", breakWWW("\u0130\u0130WWW.a"))
}
