package testplan

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

type goldenCase struct {
	Input           Input    `json:"input"`
	Plan            Plan     `json:"plan"`
	ExpectedRefusal *Refusal `json:"expected_refusal"`
}

func load(t *testing.T, path string) goldenCase {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var c goldenCase
	if err := json.Unmarshal(b, &c); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return c
}

func TestGoldenValidPlansAccepted(t *testing.T) {
	files, _ := filepath.Glob("../../../testdata/plan/testplan/valid/*.json")
	if len(files) != 2 {
		t.Fatalf("want 2 valid goldens, found %d", len(files))
	}
	for _, f := range files {
		c := load(t, f)
		if got := CheckPlan(c.Input, c.Plan); len(got) != 0 {
			t.Errorf("%s: refused with %+v", filepath.Base(f), got)
		}
	}
}

// TestGoldenRefusedPlans: each refused case breaks exactly one rule, and CheckPlan says exactly the refusal written.
func TestGoldenRefusedPlans(t *testing.T) {
	files, _ := filepath.Glob("../../../testdata/plan/testplan/refused/*.json")
	if len(files) != 5 {
		t.Fatalf("want 5 refused goldens, found %d", len(files))
	}
	for _, f := range files {
		t.Run(filepath.Base(f), func(t *testing.T) {
			c := load(t, f)
			got := CheckPlan(c.Input, c.Plan)
			if len(got) != 1 {
				t.Fatalf("want exactly one refusal, got %+v", got)
			}
			if want := *c.ExpectedRefusal; !reflect.DeepEqual(got[0], want) {
				t.Fatalf("refusal\n got  %+v\n want %+v", got[0], want)
			}
		})
	}
}

// FuzzDecodePlan: decoding and checking an arbitrary document never panics.
func FuzzDecodePlan(f *testing.F) {
	f.Add([]byte(`{"ticket":"K","behaviours":[{"id":"B1","ac":"AC1","kind":"api","case":"negative","given":"g","then":"t","oracle":"exact"}]}`))
	f.Add([]byte(`{}`))
	f.Fuzz(func(_ *testing.T, data []byte) {
		var p Plan
		if err := json.Unmarshal(data, &p); err != nil {
			return
		}
		_ = CheckPlan(Input{Criteria: []string{"AC1"}, DeclaredKinds: []string{"api"}}, p)
	})
}
