package reviewlearn

import (
	"math"
	"testing"
)

func TestListedKeepsOnlyMutedRulesSortedByName(t *testing.T) {
	got := Listed([]RuleTally{
		{"zeta", 0, 6}, {"alpha", 0, 5}, {"keep", 3, 9}, {"few", 0, 2},
	})
	if len(got) != 2 || got[0].Rule != "alpha" || got[1].Rule != "zeta" {
		t.Fatalf("Listed = %+v, want alpha then zeta", got)
	}
}

func TestWeightIsTheLaplaceShare(t *testing.T) {
	cases := map[RuleTally]float64{
		{}:                         0.5,
		{Confirmed: 1}:             2.0 / 3.0,
		{Dropped: 2}:               1.0 / 4.0,
		{Confirmed: 3, Dropped: 1}: 4.0 / 6.0,
	}
	for tally, want := range cases {
		if got := tally.Weight(); math.Abs(got-want) > 1e-12 {
			t.Errorf("%+v: Weight = %v, want %v", tally, got, want)
		}
	}
}

func TestRefuterRecordHidesUntilOverturnedMoreThanHalf(t *testing.T) {
	cases := []struct {
		name string
		r    RefuterRecord
		want bool
	}{
		{"two refutations, both overturned: not enough to judge", RefuterRecord{"s", 2, 2}, true},
		{"three, one overturned", RefuterRecord{"s", 3, 1}, true},
		{"four, two overturned: exactly half still hides", RefuterRecord{"s", 4, 2}, true},
		{"four, three overturned: an opinion", RefuterRecord{"s", 4, 3}, false},
	}
	for _, c := range cases {
		if got := c.r.Hides(); got != c.want {
			t.Errorf("%s: Hides = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestDismissalCategories(t *testing.T) {
	for _, d := range []Dismissal{DismissalWrong, DismissalOutOfScope, DismissalTradeOff, DismissalPreference} {
		if !d.Valid() {
			t.Errorf("%s should be valid", d)
		}
	}
	if Dismissal("because").Valid() {
		t.Error("an unknown category is valid")
	}
	if !DismissalTradeOff.ProposesConvention() || !DismissalPreference.ProposesConvention() {
		t.Error("trade-off and preference must propose a convention")
	}
	if DismissalWrong.ProposesConvention() || DismissalOutOfScope.ProposesConvention() {
		t.Error("wrong and out_of_scope must not propose a convention")
	}
}
