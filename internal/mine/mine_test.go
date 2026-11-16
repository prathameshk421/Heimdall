package mine

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

const golden = "../../testdata/plan/mine/"

func readCommits(t *testing.T) []Commit {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(golden, "history.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var out []Commit
	dec := json.NewDecoder(bytes.NewReader(b))
	for dec.More() {
		var c Commit
		if err := dec.Decode(&c); err != nil {
			t.Fatal(err)
		}
		out = append(out, c)
	}
	return out
}

func mustRead(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(golden, name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestGoldenRulesAndSARIFByteForByte: the history gives rules.json, and rules plus change give sarif.json, exactly.
func TestGoldenRulesAndSARIFByteForByte(t *testing.T) {
	rules := Mine(readCommits(t), DefaultLimits())
	gotRules, err := RulesJSON(rules)
	if err != nil {
		t.Fatal(err)
	}
	if want := mustRead(t, "rules.json"); !bytes.Equal(gotRules, want) {
		t.Fatalf("rules.json differs:\n%s", gotRules)
	}
	var change struct {
		Files []string `json:"files"`
	}
	if err := json.Unmarshal(mustRead(t, "change.json"), &change); err != nil {
		t.Fatal(err)
	}
	gotSARIF, err := SARIFJSON(Companions(rules, change.Files))
	if err != nil {
		t.Fatal(err)
	}
	if want := mustRead(t, "sarif.json"); !bytes.Equal(gotSARIF, want) {
		t.Fatalf("sarif.json differs:\n%s", gotSARIF)
	}
}

// TestMergesAndLargeCommitsAreNotCounted: a merge and a 31-file commit add nothing, neither to support nor to commits(A).
func TestMergesAndLargeCommitsAreNotCounted(t *testing.T) {
	var commits []Commit
	for i := 0; i < 5; i++ {
		commits = append(commits, Commit{Hash: "p", Files: []string{"a.go", "b.go"}})
	}
	big := make([]string, 31)
	for i := range big {
		big[i] = "f" + string(rune('a'+i%26)) + string(rune('0'+i/26))
	}
	big = append(big, "a.go", "b.go")
	commits = append(commits,
		Commit{Hash: "merge", Parents: []string{"x", "y"}, Files: []string{"a.go", "c.go"}},
		Commit{Hash: "big", Files: big})
	for _, r := range Mine(commits, DefaultLimits()) {
		if r.Antecedent == "a.go" && (r.Consequent == "c.go" || r.AntecedentCommits != 5) {
			t.Fatalf("merge or large commit was counted: %+v", r)
		}
	}
	rules := Mine(commits, DefaultLimits())
	if len(rules) != 2 {
		t.Fatalf("want a.go<=>b.go only (2 rules), got %+v", rules)
	}
}
