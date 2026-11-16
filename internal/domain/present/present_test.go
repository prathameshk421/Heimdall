package present

import (
	"reflect"
	"testing"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
)

func TestScopeOf(t *testing.T) {
	changed := map[string][]Range{"a.go": {{10, 12}, {40, 40}}}
	cases := []struct {
		name   string
		loc    finding.Location
		caused bool
		want   Scope
	}{
		{"overlaps a changed range", finding.Location{File: "a.go", Lines: [2]int{11, 11}}, false, ScopeChanged},
		{"touches the edge of a changed range", finding.Location{File: "a.go", Lines: [2]int{5, 10}}, false, ScopeChanged},
		{"unchanged, linked by the graph", finding.Location{File: "a.go", Lines: [2]int{20, 25}}, true, ScopeCaused},
		{"unchanged and not linked", finding.Location{File: "a.go", Lines: [2]int{20, 25}}, false, ScopePreExisting},
		{"same lines, another file", finding.Location{File: "b.go", Lines: [2]int{10, 12}}, false, ScopePreExisting},
		{"changed overrides caused", finding.Location{File: "a.go", Lines: [2]int{40, 41}}, true, ScopeChanged},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ScopeOf(c.loc, changed, c.caused); got != c.want {
				t.Fatalf("ScopeOf = %s, want %s", got, c.want)
			}
		})
	}
}

func TestArrange(t *testing.T) {
	item := func(id string, blocking bool, sev finding.Severity, tier finding.Tier, sc Scope) Item {
		return Item{ID: id, Blocking: blocking, Severity: sev, Tier: tier, Scope: sc}
	}
	items := []Item{
		item("old", true, finding.SeverityCritical, "E4", ScopePreExisting),
		item("low", false, finding.SeverityLow, "E4", ScopeChanged),
		item("blockE3", true, finding.SeverityMedium, "E3", ScopeChanged),
		item("highE4", false, finding.SeverityHigh, "E4", ScopeCaused),
		item("highE2", false, finding.SeverityHigh, "E2", ScopeChanged),
	}
	p := Arrange(items, 3)
	ids := func(xs []Item) []string {
		var out []string
		for _, x := range xs {
			out = append(out, x.ID)
		}
		return out
	}
	if got, want := ids(p.Comments), []string{"blockE3", "highE2", "highE4"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("comments = %v, want %v", got, want)
	}
	if got, want := ids(p.Collapsed), []string{"low"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("collapsed = %v, want %v", got, want)
	}
	if got, want := ids(p.Noticed), []string{"old"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("noticed = %v, want %v", got, want)
	}
}

func TestArrangeEmptyAndUnderCap(t *testing.T) {
	if p := Arrange(nil, 10); len(p.Comments)+len(p.Collapsed)+len(p.Noticed) != 0 {
		t.Fatalf("empty input gave %+v", p)
	}
	p := Arrange([]Item{{ID: "x", Scope: ScopeChanged, Severity: finding.SeverityLow, Tier: "E1"}}, 10)
	if len(p.Comments) != 1 || len(p.Collapsed) != 0 {
		t.Fatalf("under the cap: %+v", p)
	}
}
