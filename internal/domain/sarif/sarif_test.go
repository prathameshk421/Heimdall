package sarif

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// The lens of each profile row in testdata/plan/sarif/profile.yaml.
var rowLens = map[string]string{"patterns": "security", "migrations": "data", "api-break": "contract"}

func readJSON(t *testing.T, path string, v any) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}

// TestGoldenPairs: New over each pair's base and head reproduces its new.json exactly.
func TestGoldenPairs(t *testing.T) {
	pairs, _ := filepath.Glob("../../../testdata/plan/sarif/pair*")
	if len(pairs) != 6 {
		t.Fatalf("want 6 pairs, found %d", len(pairs))
	}
	for _, dir := range pairs {
		t.Run(filepath.Base(dir), func(t *testing.T) {
			var in struct {
				Check string `json:"check"`
			}
			readJSON(t, filepath.Join(dir, "input.json"), &in)
			lens, ok := rowLens[in.Check]
			if !ok {
				t.Fatalf("no lens for check %q", in.Check)
			}
			base := parseFile(t, filepath.Join(dir, "base.sarif"), in.Check, lens)
			head := parseFile(t, filepath.Join(dir, "head.sarif"), in.Check, lens)
			var want []Result
			readJSON(t, filepath.Join(dir, "new.json"), &want)
			if got := New(base, head); !reflect.DeepEqual(got, want) {
				t.Fatalf("New = %+v\nwant %+v", got, want)
			}
		})
	}
}

func parseFile(t *testing.T, path, check, lens string) []Result {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	res, err := Parse(b, check, lens)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return res
}

func TestParseRefusesWhatCannotBeFingerprinted(t *testing.T) {
	cases := map[string]string{
		"not json":               `{`,
		"no rule":                `{"runs":[{"results":[{"level":"error","message":{"text":"x"},"locations":[]}]}]}`,
		"unknown level":          `{"runs":[{"results":[{"ruleId":"r","level":"fatal","message":{"text":"x"},"locations":[]}]}]}`,
		"no snippet, no message": `{"runs":[{"results":[{"ruleId":"r","level":"note","message":{"text":""},"locations":[{"physicalLocation":{"artifactLocation":{"uri":"a.go"},"region":{"startLine":1,"endLine":1}}}]}]}]}`,
		"backwards region":       `{"runs":[{"results":[{"ruleId":"r","level":"note","message":{"text":"x"},"locations":[{"physicalLocation":{"artifactLocation":{"uri":"a.go"},"region":{"startLine":5,"endLine":2,"snippet":{"text":"x"}}}}]}]}]}`,
	}
	for name, in := range cases {
		if _, err := Parse([]byte(in), "patterns", "security"); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestFingerprintIgnoresLineNumbersAndIndentation(t *testing.T) {
	a := fingerprint("r", "a.go", "\treturn  x +\n\t\ty")
	b := fingerprint("r", "a.go", "return x + y")
	if a != b {
		t.Fatalf("collapsed text differs: %s vs %s", a, b)
	}
}

func FuzzParse(f *testing.F) {
	for _, s := range []string{`{}`, `{"runs":[]}`, `{"runs":[{"results":[{"ruleId":"r","level":"note","message":{"text":"m"},"locations":[{"physicalLocation":{"artifactLocation":{"uri":"a"},"region":{"startLine":1,"endLine":2,"snippet":{"text":"t"}}}}]}]}]}`} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		res, err := Parse(data, "c", "l")
		if err != nil {
			return
		}
		for _, r := range res {
			if r.Rule == "" || (r.Level != "error" && r.Level != "warning" && r.Level != "note") {
				t.Fatalf("accepted an invalid result %+v", r)
			}
		}
	})
}

// golangci-lint (v2.15) writes a region with only startLine and startColumn, and no snippet, which SARIF 2.1.0
// allows: endLine defaults to startLine and the snippet is optional. Such a result is read, its range is the one line,
// and its fingerprint comes from the message (live, Nov 21: the lint check on PR #18 was "no valid SARIF", D192).
func TestAResultWithOnlyAStartLineAndNoSnippetIsRead(t *testing.T) {
	data := []byte(`{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"golangci-lint"}},"results":[` +
		`{"ruleId":"errcheck","level":"error","message":{"text":"Error return value of ` + "`s.refunds.record`" + ` is not checked"},` +
		`"locations":[{"physicalLocation":{"artifactLocation":{"uri":"internal/payments/refund.go","index":0},"region":{"startLine":71,"startColumn":18}}}]}]}]}`)
	got, err := Parse(data, "golangci-lint", "static")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(got) != 1 || got[0].File != "internal/payments/refund.go" || got[0].Lines != [2]int{71, 71} || got[0].Finger == "" {
		t.Fatalf("got %+v", got)
	}
	moved := []byte(string(data[:0]) + `{"runs":[{"results":[{"ruleId":"errcheck","level":"error","message":{"text":"Error return value of ` +
		"`s.refunds.record`" + ` is not checked"},"locations":[{"physicalLocation":{"artifactLocation":{"uri":"internal/payments/refund.go"},"region":{"startLine":90}}}]}]}]}`)
	again, err := Parse(moved, "golangci-lint", "static")
	if err != nil || len(again) != 1 || again[0].Finger != got[0].Finger {
		t.Fatalf("the same message on a moved line keeps its fingerprint: %+v %v", again, err)
	}
	bad := []byte(`{"runs":[{"results":[{"ruleId":"r","level":"error","message":{"text":"m"},"locations":[{"physicalLocation":{"artifactLocation":{"uri":"a.go"},"region":{"startLine":9,"endLine":3}}}]}]}]}`)
	if _, err := Parse(bad, "c", "static"); err == nil {
		t.Fatal("an endLine before the startLine is still refused")
	}
}
