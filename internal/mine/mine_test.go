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
