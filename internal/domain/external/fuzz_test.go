package external_test

import (
	"testing"
	"unicode/utf8"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/external"
)

// FuzzIngestBody: no body, for any of the four tools, makes the parser panic or give an invalid claim.
// Run with go test -run=^$ -fuzz=FuzzIngestBody -fuzztime=30s.
func FuzzIngestBody(f *testing.F) {
	for _, s := range []string{
		"", "<!--", "<details>", "</details>", "<details><details>", "```suggestion", "![", "[![a](b)](c)", "_x_ | _y_",
		"**High Severity**", "**logic:**", "<!-- DESCRIPTION START -->", "<sub>", "\x00\xff", "_⚠️ Potential issue_ | _🟠 Major_\n\n**t.**",
	} {
		f.Add(s)
	}
	logins := allSources()
	f.Fuzz(func(t *testing.T, body string) {
		for _, login := range logins {
			c := inline(1, "a.go", 3)
			c.UserLogin, c.Body = login, body
			got := newPR().Ingest(c)
			if got.Outcome == external.OutcomeCandidate {
				n := utf8.RuneCountInString(got.Candidate.Claim)
				if !utf8.ValidString(got.Candidate.Claim) || n < 10 || n > 600 || !got.Candidate.Severity.Valid() {
					t.Fatalf("bad candidate %+v", got.Candidate)
				}
			}
		}
	})
}

// FuzzParsePatch: no patch makes the parser panic, and a parsed patch maps every line without a panic.
func FuzzParsePatch(f *testing.F) {
	f.Add("@@ -3,3 +3,5 @@\n a\n b\n+x\n+y\n c\n")
	f.Add("@@ -0,0 +1 @@\n+a")
	f.Add("@@ @@")
	f.Fuzz(func(_ *testing.T, patch string) {
		hunks, err := external.ParsePatch(patch)
		if err != nil {
			return
		}
		pr := newPR()
		pr.Diffs = external.Diffs{older: {{OldPath: "a.go", NewPath: "a.go", Hunks: hunks}}}
		for line := 1; line < 40; line++ {
			pr.Ingest(oldComment("a.go", line))
		}
	})
}
