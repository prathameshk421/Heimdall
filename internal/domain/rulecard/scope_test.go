package rulecard

import "testing"

func TestOnlyAnAcceptedPersonCardReachesE3(t *testing.T) {
	cases := []struct {
		name string
		c    Card
		want bool
	}{
		{"accepted person card", Card{Scope: ScopePerson, Status: StatusActive}, true},
		{"unscoped card is a person card", Card{Status: StatusActive}, true},
		{"person card not yet accepted", Card{Scope: ScopePerson, Status: StatusProposed}, false},
		{"lens card, accepted", Card{Scope: ScopeLens, Lens: "security", Status: StatusActive}, false},
		{"team card, accepted", Card{Scope: ScopeTeam, Status: StatusActive}, false},
		{"codebase card, accepted", Card{Scope: ScopeCodebase, Status: StatusActive}, false},
	}
	for _, c := range cases {
		if got := c.c.CanReachE3(); got != c.want {
			t.Errorf("%s: CanReachE3 = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestValidateScopeAndLens(t *testing.T) {
	ok := Card{Status: StatusActive, Scope: ScopeLens, Lens: "security"}
	if err := ok.Validate(); err != nil {
		t.Fatalf("valid lens card refused: %v", err)
	}
	if err := (Card{Status: StatusActive, Scope: ScopeLens}).Validate(); err == nil {
		t.Error("a lens card without a lens was accepted")
	}
	if err := (Card{Status: StatusActive, Scope: "global"}).Validate(); err == nil {
		t.Error("an unknown scope was accepted")
	}
}
