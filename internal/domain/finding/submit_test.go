package finding

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type reviewCase struct {
	Brief           Brief    `json:"brief"`
	Submission      SubmitV2 `json:"submission"`
	ExpectedRefusal *Refusal `json:"expected_refusal"`
}

func loadReviewCase(t *testing.T, path string) reviewCase {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var c reviewCase
	if err := json.Unmarshal(b, &c); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return c
}

func TestGoldenReviewsValid(t *testing.T) {
	files, _ := filepath.Glob("../../../testdata/plan/review/valid/*.json")
	if len(files) == 0 {
		t.Fatal("no valid goldens")
	}
	for _, f := range files {
		c := loadReviewCase(t, f)
		if got := CheckSubmit(c.Brief, c.Submission); len(got) != 0 {
			t.Errorf("%s: refused with %+v", filepath.Base(f), got)
		}
	}
}

func TestGoldenReviewsRefused(t *testing.T) {
	files, _ := filepath.Glob("../../../testdata/plan/review/refused/*.json")
	if len(files) != 7 {
		t.Fatalf("want 7 refused goldens, found %d", len(files))
	}
	for _, f := range files {
		name := filepath.Base(f)
		t.Run(name, func(t *testing.T) {
			c := loadReviewCase(t, f)
			got := CheckSubmit(c.Brief, c.Submission)
			if len(got) != 1 {
				t.Fatalf("want exactly one refusal, got %d: %+v", len(got), got)
			}
			want, g := *c.ExpectedRefusal, got[0]
			if g.Field != want.Field || g.Problem != want.Problem {
				t.Fatalf("refusal = {%q %q}, want {%q %q}", g.Field, g.Problem, want.Field, want.Problem)
			}
			if wantEx := exampleText(want.Example); g.Example != wantEx {
				t.Fatalf("example = %q, want %q", g.Example, wantEx)
			}
		})
	}
}

// exampleText turns the golden's example into the string a refusal carries: null is no example.
func exampleText(s string) string {
	if strings.TrimSpace(s) == "" || s == "null" {
		return ""
	}
	return s
}
