package finding

import (
	"strings"
	"testing"
)

var filed = Location{File: "internal/api/refunds.go", Lines: [2]int{10, 20}}

func TestCheckRefutation(t *testing.T) {
	reason := "the caller checks the total before this line"
	cases := []struct {
		name      string
		reporters []string
		r         Refutation
		lines     int
		wantErr   string // substring; empty means accepted
	}{
		{"refuted with other code", []string{"r1"},
			Refutation{Refuter: "r2", Answer: RefuteRefuted, Location: Location{File: "internal/api/refunds.go", Lines: [2]int{1, 5}}, Reason: reason}, 40, ""},
		{"refuted cites the finding's own lines", []string{"r1"},
			Refutation{Refuter: "r2", Answer: RefuteRefuted, Location: Location{File: "internal/api/refunds.go", Lines: [2]int{15, 16}}, Reason: reason}, 40, "own lines"},
		{"refuted lines past the end of head", []string{"r1"},
			Refutation{Refuter: "r2", Answer: RefuteRefuted, Location: Location{File: "internal/api/refunds.go", Lines: [2]int{30, 41}}, Reason: reason}, 40, "outside"},
		{"refuted file absent at head", []string{"r1"},
			Refutation{Refuter: "r2", Answer: RefuteRefuted, Location: Location{File: "gone.go", Lines: [2]int{1, 2}}, Reason: reason}, 0, "does not exist"},
		{"refuted with no location", []string{"r1"},
			Refutation{Refuter: "r2", Answer: RefuteRefuted, Reason: reason}, 40, "needs a location"},
		{"reporter cannot refute", []string{"r1"},
			Refutation{Refuter: "r1", Answer: RefuteNotRefuted, Reason: reason}, 40, "cannot refute"},
		{"not_refuted needs a sentence", []string{"r1"},
			Refutation{Refuter: "r2", Answer: RefuteNotRefuted, Reason: "   "}, 40, "reason must be"},
		{"not_refuted accepted", []string{"r1"},
			Refutation{Refuter: "r2", Answer: RefuteNotRefuted, Reason: reason}, 40, ""},
		{"proved is left to the evidence stage", []string{"r1"},
			Refutation{Refuter: "r2", Answer: RefuteProved, Reason: reason}, 40, ""},
		{"unknown answer", []string{"r1"},
			Refutation{Refuter: "r2", Answer: "maybe", Reason: reason}, 40, "is not refuted"},
		{"reason too long", []string{"r1"},
			Refutation{Refuter: "r2", Answer: RefuteNotRefuted, Reason: strings.Repeat("x", MaxRefuteReasonChars+1)}, 40, "reason must be"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := CheckRefutation(filed, c.reporters, c.r, c.lines)
			switch {
			case c.wantErr == "" && err != nil:
				t.Fatalf("refused: %v", err)
			case c.wantErr != "" && (err == nil || !strings.Contains(err.Error(), c.wantErr)):
				t.Fatalf("err = %v, want it to contain %q", err, c.wantErr)
			}
		})
	}
}

// TestRefutedIsDerivedFromLaterEvidence: a refutation stands until evidence arrives after it.
func TestRefutedIsDerivedFromLaterEvidence(t *testing.T) {
	cases := []struct {
		refute, evidence int
		want             bool
	}{
		{0, 0, false}, // no refutation filed
		{0, 5, false}, // evidence without a refutation
		{3, 0, true},  // refuted, no evidence
		{3, 3, true},  // evidence at the refutation's own sequence does not reopen it
		{3, 4, false}, // evidence after it reopens
	}
	for _, c := range cases {
		if got := Refuted(c.refute, c.evidence); got != c.want {
			t.Errorf("Refuted(%d, %d) = %v, want %v", c.refute, c.evidence, got, c.want)
		}
	}
}
