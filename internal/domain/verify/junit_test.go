package verify_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/verify"
)

// load reads one recorded sample from testdata/junit. The files are hand-written, and every expected value below
// is read off the sample by a person, never computed from the parser.
func load(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "junit", name))
	require.NoError(t, err)
	return b
}

func parse(t *testing.T, b []byte) verify.Report {
	t.Helper()
	rep, err := verify.ParseBytes(b)
	require.NoError(t, err)
	return rep
}

func names(cases []verify.Case) []string {
	out := make([]string, 0, len(cases))
	for _, c := range cases {
		out = append(out, c.Name)
	}
	return out
}

const refundsPkg = "github.com/aarush323/heimdall-fixture-pay/refunds"

// An assertion failure is a <failure> with the tool's message and the assertion text.
func TestParseAssertionFailure(t *testing.T) {
	rep := parse(t, load(t, "assertion_failure.xml"))
	want := verify.Report{Suites: []verify.Suite{{
		Name: refundsPkg,
		Cases: []verify.Case{
			{
				Suite:     refundsPkg,
				ClassName: refundsPkg,
				Name:      "TestPartialRefund",
				Status:    verify.StatusFail,
				Message:   "Failed",
				Text: "Error Trace:\trefunds_test.go:42\n" +
					"\tError:      \tNot equal: \n" +
					"\t            \texpected: 500\n" +
					"\t            \tactual  : 200\n" +
					"\tTest:       \tTestPartialRefund\n",
			},
			{Suite: refundsPkg, ClassName: refundsPkg, Name: "TestFullRefund", Status: verify.StatusPass},
		},
	}}}
	require.Equal(t, want, rep)
}

// A compile error is a <error>, and it carries the compiler output so a retry can act on it.
func TestParseCompileError(t *testing.T) {
	rep := parse(t, load(t, "compile_error.xml"))
	require.Len(t, rep.Suites, 1)
	require.Len(t, rep.Cases(), 1)
	c := rep.Cases()[0]
	require.Equal(t, "[build failed]", c.Name)
	require.Equal(t, verify.StatusError, c.Status)
	require.Equal(t, "Failed to compile refunds_test: # github.com/aarush323/heimdall-fixture-pay/refunds [build failed]", c.Message)
	require.Equal(t, "refunds_test.go:9:2: undefined: NewRefund\nFAIL\tgithub.com/aarush323/heimdall-fixture-pay/refunds [build failed]\n", c.Text)
}

func TestParseAllPass(t *testing.T) {
	rep := parse(t, load(t, "all_pass.xml"))
	require.Equal(t, []string{"github.com/aarush323/heimdall-fixture-pay/ledger"}, suiteNames(rep))
	require.Equal(t, []string{"TestBalance", "TestDeposit", "TestWithdraw"}, names(rep.Cases()))
	for _, c := range rep.Cases() {
		require.Equal(t, verify.StatusPass, c.Status, c.Name)
		require.Empty(t, c.Message, c.Name)
		require.Empty(t, c.Text, c.Name)
	}
}

func TestParseSkipped(t *testing.T) {
	rep := parse(t, load(t, "skipped.xml"))
	cases := rep.Cases()
	require.Len(t, cases, 2)
	require.Equal(t, verify.Case{
		Suite: refundsPkg, ClassName: refundsPkg, Name: "TestRefundLedger",
		Status: verify.StatusSkipped, Message: "skipping: needs the ledger service",
	}, cases[0])
	require.Equal(t, verify.StatusPass, cases[1].Status)
}

// A report with two suites keeps the document order and the suite each case came from.
func TestParseTwoSuites(t *testing.T) {
	rep := parse(t, load(t, "two_suites.xml"))
	require.Equal(t, []string{
		"github.com/aarush323/heimdall-fixture-pay/refunds",
		"github.com/aarush323/heimdall-fixture-pay/ledger",
	}, suiteNames(rep))
	require.Len(t, rep.Suites[0].Cases, 2)
	require.Len(t, rep.Suites[1].Cases, 1)
	cases := rep.Cases()
	require.Equal(t, []string{"TestPartialRefund", "TestFullRefund", "TestBalance"}, names(cases))
	require.Equal(t, verify.StatusFail, cases[0].Status)
	require.Equal(t, "unexpected status: got 200, want 500", cases[0].Text)
	require.Equal(t, "github.com/aarush323/heimdall-fixture-pay/ledger", cases[2].Suite)
}

// pytest tags each case with its module as classname and its own name; both are kept, and &gt; decodes.
func TestParsePytest(t *testing.T) {
	rep := parse(t, load(t, "pytest_failure.xml"))
	require.Equal(t, []string{"pytest"}, suiteNames(rep))
	cases := rep.Cases()
	require.Len(t, cases, 3)
	require.Equal(t, verify.Case{
		Suite: "pytest", ClassName: "tests.test_refunds", Name: "test_partial_refund",
		Status:  verify.StatusFail,
		Message: "assert 200 == 500",
		Text:    "def test_partial_refund():\n>       assert response.status_code == 500\nE       assert 200 == 500\n",
	}, cases[0])
	require.Equal(t, verify.StatusPass, cases[1].Status)
	require.Equal(t, verify.Case{
		Suite: "pytest", ClassName: "tests.test_refunds", Name: "test_idempotent",
		Status: verify.StatusSkipped, Message: "needs the ledger service",
	}, cases[2], "an unknown type attribute must not leak into the case")
}

// A root <testsuite> is as valid as a <testsuites> wrapper.
func TestParseRootTestsuite(t *testing.T) {
	rep := parse(t, load(t, "root_testsuite.xml"))
	require.Len(t, rep.Suites, 1)
	require.Equal(t, refundsPkg, rep.Suites[0].Name)
	require.Equal(t, "Error:      \tReceived unexpected error:\n\t            \t404 page not found\n\tTest:       \tTestPartialRefund", rep.Cases()[0].Text)
}

// A failure message and body are bounded: a 3000-character message and body each keep their first 2048 bytes.
func TestParseHugeMessageIsBounded(t *testing.T) {
	rep := parse(t, load(t, "huge_message.xml"))
	c := rep.Cases()[0]
	require.Equal(t, verify.StatusFail, c.Status)
	require.Len(t, c.Message, 2048)
	require.Equal(t, "message-start:"+strings.Repeat("m", 2034), c.Message)
	require.Len(t, c.Text, 2048)
	require.Equal(t, "text-start:"+strings.Repeat("t", 2037), c.Text)
}

// Truncation never splits a rune, so the result stays valid UTF-8.
func TestParseBoundedMessageKeepsUTF8(t *testing.T) {
	long := strings.Repeat("a", 2047) + "é"
	doc := `<?xml version="1.0"?><testsuite name="s"><testcase name="T"><failure message="` + long + `">x</failure></testcase></testsuite>`
	rep := parse(t, []byte(doc))
	require.Len(t, rep.Cases()[0].Message, 2047)
	require.True(t, utf8.ValidString(rep.Cases()[0].Message))
}

// A zero-test run is never green: an otherwise well-formed report with no cases is refused.
func TestParseEmptySuiteIsRefused(t *testing.T) {
	_, err := verify.ParseBytes(load(t, "empty_suite.xml"))
	require.ErrorIs(t, err, verify.ErrNoCases)
	require.Contains(t, err.Error(), "no test cases")
}

func TestParseRefusesBadReports(t *testing.T) {
	cases := map[string]struct {
		in   string
		want error
	}{
		"empty input":             {"", verify.ErrMalformed},
		"truncated tag":           {"<testsuite", verify.ErrMalformed},
		"unknown root":            {`<?xml version="1.0"?><report><testsuite name="s"><testcase name="T"/></testsuite></report>`, verify.ErrMalformed},
		"doctype":                 {`<!DOCTYPE testsuite><testsuite name="s"><testcase name="T"/></testsuite>`, verify.ErrMalformed},
		"unknown entity":          {`<testsuite name="s"><testcase name="T"><failure message="a &bogus; b"/></testcase></testsuite>`, verify.ErrMalformed},
		"testcase without name":   {`<testsuite name="s"><testcase classname="c" time="1"/></testsuite>`, verify.ErrMalformed},
		"stray text":              {`<testsuite name="s">oops<testcase name="T"/></testsuite>`, verify.ErrMalformed},
		"duplicate attribute":     {`<testsuite name="s"><testcase name="a" name="b"/></testsuite>`, verify.ErrMalformed},
		"nested testsuite":        {`<testsuite name="s"><testsuite name="inner"><testcase name="T"/></testsuite></testsuite>`, verify.ErrMalformed},
		"unclosed testsuites":     {`<testsuites><testsuite name="s"><testcase name="T"/></testsuite>`, verify.ErrMalformed},
		"closing tag mismatch":    {`<testsuite name="s"><testcase name="T"></testsuite></testsuite>`, verify.ErrMalformed},
		"attribute without value": {`<testsuite name><testcase name="T"/></testsuite>`, verify.ErrMalformed},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := verify.ParseBytes([]byte(c.in))
			require.ErrorIs(t, err, c.want)
		})
	}
}

// The size limit is inclusive, and one byte over it is refused before parsing.
func TestParseSizeLimit(t *testing.T) {
	open := `<testsuite name="s"><testcase name="T"/><system-out>`
	closing := `</system-out></testsuite>`
	pad := verify.MaxReportBytes - len(open) - len(closing)
	atLimit := open + strings.Repeat("x", pad) + closing
	require.Len(t, atLimit, verify.MaxReportBytes)
	require.Len(t, parse(t, []byte(atLimit)).Cases(), 1)

	over := atLimit[:len(atLimit)-len(closing)] + "x" + closing
	require.Len(t, over, verify.MaxReportBytes+1)
	_, err := verify.ParseBytes([]byte(over))
	require.ErrorIs(t, err, verify.ErrTooLarge)
}

// A reader that never ends is cut at the limit instead of being buffered whole.
func TestParseFromReaderStopsAtLimit(t *testing.T) {
	r := bytes.NewReader(append([]byte(`<testsuite name="s"><testcase name="T"/></testsuite>`), make([]byte, verify.MaxReportBytes)...))
	_, err := verify.Parse(r)
	require.ErrorIs(t, err, verify.ErrTooLarge)
}

func TestParseDecodesCharacterData(t *testing.T) {
	doc := `<?xml version="1.0"?><!-- a comment --><testsuite name="s &amp; t"><testcase classname="c" name="T">` +
		`<failure message="m &lt;&amp;&gt; &#65;&#x42;"><![CDATA[a & b <c>]]>tail &quot;q&quot;</failure></testcase></testsuite>`
	rep := parse(t, []byte(doc))
	require.Equal(t, "s & t", rep.Suites[0].Name)
	require.Equal(t, "m <&> AB", rep.Cases()[0].Message)
	require.Equal(t, "a & b <c>tail \"q\"", rep.Cases()[0].Text)
}

// error outranks failure, which outranks skipped, and the winner supplies the message.
func TestParseStatusPrecedence(t *testing.T) {
	cases := map[string]verify.Case{
		`<testcase name="T"><skipped message="s"/><failure message="f"/></testcase>`: {Name: "T", Status: verify.StatusFail, Message: "f"},
		`<testcase name="T"><failure message="f"/><error message="e"/></testcase>`:   {Name: "T", Status: verify.StatusError, Message: "e"},
		`<testcase name="T"><error message="e"/><failure message="f"/></testcase>`:   {Name: "T", Status: verify.StatusError, Message: "e"},
		`<testcase name="T"><skipped message="s"/></testcase>`:                       {Name: "T", Status: verify.StatusSkipped, Message: "s"},
		`<testcase name="T"><system-out>noise</system-out></testcase>`:               {Name: "T", Status: verify.StatusPass},
		`<testcase name="T"><failure message="f">   </failure></testcase>`:           {Name: "T", Status: verify.StatusFail, Message: "f", Text: "   "},
	}
	for in, want := range cases {
		t.Run(in, func(t *testing.T) {
			rep := parse(t, []byte(`<testsuite name="s">`+in+`</testsuite>`))
			require.Equal(t, maybeSuite(want, "s"), rep.Cases()[0])
		})
	}
}

func maybeSuite(c verify.Case, suite string) verify.Case {
	c.Suite = suite
	return c
}

func TestStatusValid(t *testing.T) {
	for _, s := range []verify.Status{verify.StatusPass, verify.StatusFail, verify.StatusError, verify.StatusSkipped} {
		require.True(t, s.Valid(), string(s))
	}
	require.False(t, verify.Status("").Valid())
	require.False(t, verify.Status("failure").Valid())
}

func suiteNames(r verify.Report) []string {
	out := make([]string, 0, len(r.Suites))
	for _, s := range r.Suites {
		out = append(out, s.Name)
	}
	return out
}

// A report that parses must never be empty, whatever the bytes were.
func FuzzParse(f *testing.F) {
	for _, name := range []string{"assertion_failure.xml", "compile_error.xml", "two_suites.xml", "pytest_failure.xml"} {
		b, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "junit", name))
		require.NoError(f, err)
		f.Add(b)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		rep, err := verify.ParseBytes(data)
		if err != nil {
			return
		}
		require.NotEmpty(t, rep.Cases())
	})
}
