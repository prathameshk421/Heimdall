package coverage

import (
	"errors"
	"path"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	domaincoverage "github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/coverage"
)

func TestParseCoberturaRefusesBadReports(t *testing.T) {
	cases := []struct {
		name string
		in   string
		line int
	}{
		{"empty input", "", 1},
		{"text only", "hello", 1},
		{"wrong root", "<report/>", 1},
		{"line outside a class", "<coverage>\n<line number=\"1\" hits=\"1\"/>\n</coverage>", 2},
		{"class without a filename", "<coverage>\n<class name=\"a\"/>\n</coverage>", 2},
		{"class with an empty filename", "<coverage>\n<class filename=\"\"/>\n</coverage>", 2},
		{"line without a number", "<coverage>\n<class filename=\"a.go\">\n<line hits=\"1\"/>\n</class>\n</coverage>", 3},
		{"line without hits", "<coverage>\n<class filename=\"a.go\">\n<line number=\"1\"/>\n</class>\n</coverage>", 3},
		{"line zero", "<coverage>\n<class filename=\"a.go\">\n<line number=\"0\" hits=\"1\"/>\n</class>\n</coverage>", 3},
		{"line above the cap", "<coverage>\n<class filename=\"a.go\">\n<line number=\"10000001\" hits=\"1\"/>\n</class>\n</coverage>", 3},
		{"hits below zero", "<coverage>\n<class filename=\"a.go\">\n<line number=\"1\" hits=\"-2\"/>\n</class>\n</coverage>", 3},
		{"hits above the cap", "<coverage>\n<class filename=\"a.go\">\n<line number=\"1\" hits=\"99999999999999\"/>\n</class>\n</coverage>", 3},
		{"line is not a number", "<coverage>\n<class filename=\"a.go\">\n<line number=\"x\" hits=\"1\"/>\n</class>\n</coverage>", 3},
		{"line is too big for an int", "<coverage>\n<class filename=\"a.go\">\n<line number=\"99999999999999999999\" hits=\"1\"/>\n</class>\n</coverage>", 3},
		{"filename escapes the root", "<coverage>\n<class filename=\"../a.go\"/>\n</coverage>", 2},
		{"unclosed root", "<coverage>", 1},
		{"unclosed element", "<coverage>\n<class filename=\"a.go\">\n</coverage>", 3},
		{"mismatched close", "<coverage>\n</class>\n</coverage>", 2},
		{"unterminated attribute", "<coverage>\n<class filename=\"a.go\n</coverage>", 3},
		{"unquoted attribute", "<coverage>\n<class filename=a.go/>\n</coverage>", 2},
		{"duplicate attribute", "<coverage>\n<class filename=\"a.go\" filename=\"b.go\"/>\n</coverage>", 2},
		{"unknown entity", "<coverage>\n<class filename=\"a&bogus;.go\"/>\n</coverage>", 2},
		{"class inside a class", "<coverage>\n<class filename=\"a.go\">\n<class filename=\"b.go\"/>\n</class>\n</coverage>", 3},
		{"text before the root", "x<coverage/>", 1},
		{"a second root", "<coverage/>\n<coverage/>", 2},
		{"text after the root", "<coverage/>\nx", 1},
		{"unterminated comment", "<coverage><!-- x</coverage>", 1},
		{"invalid utf-8", "<coverage>\n<class filename=\"a\xff.go\"/>\n</coverage>", 2},
		{"an undeclared character set", "<?xml version=\"1.0\" encoding=\"ISO-8859-1\"?>\n<coverage/>", 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ParseCobertura([]byte(c.in), domaincoverage.Options{MaxBytes: 1 << 20})
			require.ErrorIs(t, err, domaincoverage.ErrMalformed)
			require.Contains(t, err.Error(), "line "+strconv.Itoa(c.line)+":")
		})
	}
}

func TestParseCoberturaRefusesDeclarations(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"internal subset", "<!DOCTYPE coverage [<!ENTITY x \"y\">]><coverage/>"},
		{"entity bomb", "<!DOCTYPE c [<!ENTITY a \"aaaa\"><!ENTITY b \"&a;&a;&a;\">]><coverage><class filename=\"&b;\"/></coverage>"},
		{"empty internal subset", "<!DOCTYPE coverage []><coverage/>"},
		{"unterminated declaration", "<!DOCTYPE coverage SYSTEM \"x\""},
		{"entity declaration", "<coverage><!ENTITY x \"y\"><class filename=\"a.go\"/></coverage>"},
		{"entity declaration before the root", "<!ENTITY x \"y\"><coverage/>"},
		{"doctype inside the root", "<coverage><!DOCTYPE coverage SYSTEM \"x\"></coverage>"},
		{"doctype after the root", "<coverage/><!DOCTYPE coverage SYSTEM \"x\">"},
		{"two doctypes", "<!DOCTYPE coverage SYSTEM \"x\"><!DOCTYPE coverage SYSTEM \"x\"><coverage/>"},
		{"lower case doctype", "<!doctype coverage SYSTEM \"x\"><coverage/>"},
		{"element declaration", "<!ELEMENT coverage ANY><coverage/>"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ParseCobertura([]byte(c.in), domaincoverage.Options{MaxBytes: 1 << 20})
			require.ErrorIs(t, err, domaincoverage.ErrMalformed)
		})
	}
}

func TestParseCoberturaReadsNamespacedDocuments(t *testing.T) {
	in := `<coverage xmlns="urn:x" xmlns:q="urn:q"><class filename="a.go" q:filename="b.go"><line number="1" hits="1" q:hits="9"/></class></coverage>`
	got, err := ParseCobertura([]byte(in), domaincoverage.Options{MaxBytes: 1 << 20})
	require.NoError(t, err)
	require.Equal(t, map[string]domaincoverage.Lines{"a.go": {1: 1}}, got.Files)
}

func TestParseCoberturaNestingLimit(t *testing.T) {
	nest := func(n int) []byte {
		return []byte("<coverage>" + strings.Repeat("<x>", n-1) + strings.Repeat("</x>", n-1) + "</coverage>")
	}
	_, err := ParseCobertura(nest(maxDepth), domaincoverage.Options{MaxBytes: 1 << 20})
	require.NoError(t, err)

	_, err = ParseCobertura(nest(maxDepth+1), domaincoverage.Options{MaxBytes: 1 << 20})
	require.ErrorIs(t, err, domaincoverage.ErrMalformed)
	require.Contains(t, err.Error(), "nested deeper")
}

func TestParseCoberturaSizeLimit(t *testing.T) {
	in := []byte(`<coverage><class filename="a.go"><line number="1" hits="1"/></class></coverage>`)
	atLimit, err := ParseCobertura(in, domaincoverage.Options{MaxBytes: len(in)})
	require.NoError(t, err)
	require.NotEmpty(t, atLimit.Files)

	_, err = ParseCobertura(in, domaincoverage.Options{MaxBytes: len(in) - 1})
	require.ErrorIs(t, err, domaincoverage.ErrTooLarge)

	// The limit is checked before the document is read: input that is not XML is still too large.
	_, err = ParseCobertura([]byte("not xml at all"), domaincoverage.Options{MaxBytes: 3})
	require.ErrorIs(t, err, domaincoverage.ErrTooLarge)
}

func TestParseCoberturaOptionMistakes(t *testing.T) {
	_, err := ParseCobertura(nil, domaincoverage.Options{})
	require.Error(t, err)
	require.NotErrorIs(t, err, domaincoverage.ErrMalformed)
	require.NotErrorIs(t, err, domaincoverage.ErrTooLarge)

	_, err = ParseCobertura(nil, domaincoverage.Options{MaxBytes: 1 << 20, Prefix: "../app"})
	require.ErrorContains(t, err, "prefix")
}

func FuzzParseCobertura(f *testing.F) {
	f.Add([]byte(`<coverage><class filename="a.go"><line number="1" hits="1"/></class></coverage>`))
	f.Add([]byte(""))
	f.Add([]byte(`<?xml version="1.0"?><!DOCTYPE coverage SYSTEM "x"><coverage/>`))
	f.Add([]byte(`<!DOCTYPE c [<!ENTITY a "b">]><coverage/>`))
	f.Add([]byte(`<coverage><class filename="../a.go"/></coverage>`))
	f.Add([]byte(`<coverage><class filename="C:\work\a.go"><line number="1" hits="1"/></class></coverage>`))
	f.Fuzz(func(t *testing.T, data []byte) {
		res, err := ParseCobertura(data, domaincoverage.Options{MaxBytes: 1 << 20})
		if err != nil {
			require.True(t, errors.Is(err, domaincoverage.ErrMalformed) || errors.Is(err, domaincoverage.ErrTooLarge), "unexpected error: %v", err)
			return
		}
		require.NotNil(t, res.Files)
		for file, lines := range res.Files {
			require.NotEmpty(t, file)
			require.NotContains(t, file, "\\")
			require.False(t, strings.HasPrefix(file, "/"))
			require.Equal(t, path.Clean(file), file)
			require.False(t, file == ".." || strings.HasPrefix(file, "../"))
			require.NotEmpty(t, lines)
			for number, hits := range lines {
				require.GreaterOrEqual(t, number, 1)
				require.LessOrEqual(t, number, domaincoverage.MaxLine)
				require.GreaterOrEqual(t, hits, 0)
			}
		}
	})
}
