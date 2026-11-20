package coverage

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseCoberturaGolden(t *testing.T) {
	got, err := ParseCobertura(load(t, "testdata/cobertura/simple.xml"), Options{
		MaxBytes: 1 << 20,
		Prefix:   "/home/runner/work/app",
	})
	require.NoError(t, err)
	require.Equal(t, map[string]Lines{
		"app/service.py": {1: 1, 2: 0, 3: 4, 5: 3},
		"app/util.py":    {10: 2},
		"app/api.py":     {20: 0},
	}, got.Files)
}

func TestParseCoberturaRefusesBadReports(t *testing.T) {
	cases := []struct {
		name string
		in   string
		line int
	}{
		{"empty input", "", 1},
		{"wrong root", "<report/>", 1},
		{"line outside a class", "<coverage>\n<line number=\"1\" hits=\"1\"/>\n</coverage>", 2},
		{"class without a filename", "<coverage>\n<class name=\"a\"/>\n</coverage>", 2},
		{"line without a number", "<coverage>\n<class filename=\"a.go\">\n<line hits=\"1\"/>\n</class>\n</coverage>", 3},
		{"line without hits", "<coverage>\n<class filename=\"a.go\">\n<line number=\"1\"/>\n</class>\n</coverage>", 3},
		{"line zero", "<coverage>\n<class filename=\"a.go\">\n<line number=\"0\" hits=\"1\"/>\n</class>\n</coverage>", 3},
		{"hits below zero", "<coverage>\n<class filename=\"a.go\">\n<line number=\"1\" hits=\"-2\"/>\n</class>\n</coverage>", 3},
		{"line is not a number", "<coverage>\n<class filename=\"a.go\">\n<line number=\"x\" hits=\"1\"/>\n</class>\n</coverage>", 3},
		{"filename escapes the root", "<coverage>\n<class filename=\"../a.go\"/>\n</coverage>", 2},
		{"unclosed root", "<coverage>", 1},
		{"mismatched close", "<coverage>\n</class>\n</coverage>", 2},
		{"unterminated attribute", "<coverage>\n<class filename=\"a.go\n</coverage>", 3},
		{"unquoted attribute", "<coverage>\n<class filename=a.go/>\n</coverage>", 2},
		{"duplicate attribute", "<coverage>\n<class filename=\"a.go\" filename=\"b.go\"/>\n</coverage>", 2},
		{"unknown entity", "<coverage>\n<class filename=\"a&bogus;.go\"/>\n</coverage>", 2},
		{"internal document type subset", "<!DOCTYPE coverage [<!ENTITY x \"y\">]><coverage/>", 1},
		{"unterminated document type declaration", "<!DOCTYPE coverage SYSTEM \"x\"", 1},
		{"unterminated comment", "<coverage><!-- x</coverage>", 1},
		{"stray declaration", "<coverage><!ENTITY x \"y\"><class filename=\"a.go\"/></coverage>", 1},
		{"class inside a class", "<coverage>\n<class filename=\"a.go\">\n<class filename=\"b.go\"/>\n</class>\n</coverage>", 3},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ParseCobertura([]byte(c.in), Options{MaxBytes: 1 << 20})
			require.ErrorIs(t, err, ErrMalformed)
			require.Contains(t, err.Error(), "line "+strconv.Itoa(c.line)+":")
		})
	}
}

func TestParseCoberturaDecodesEntities(t *testing.T) {
	in := `<coverage><class filename="a&amp;b&#46;go"><line number="1" hits="1"/></class></coverage>`
	got, err := ParseCobertura([]byte(in), Options{MaxBytes: 1 << 20})
	require.NoError(t, err)
	require.Equal(t, map[string]Lines{"a&b.go": {1: 1}}, got.Files)
}

func TestParseCoberturaSkipsPrologAndText(t *testing.T) {
	in := "<?xml version=\"1.0\"?>\n<!-- generated -->\n<coverage>\n  <sources><source>x</source></sources>\n" +
		"  <packages><package><classes><class filename=\"a.go\"><lines>\n" +
		"    <line number=\"2\" hits=\"1\"/>\n" +
		"  </lines></class></classes></package></packages>\n</coverage>\n"
	got, err := ParseCobertura([]byte(in), Options{MaxBytes: 1 << 20})
	require.NoError(t, err)
	require.Equal(t, map[string]Lines{"a.go": {2: 1}}, got.Files)
}
