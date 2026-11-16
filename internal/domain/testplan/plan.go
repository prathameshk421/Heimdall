package testplan

import (
	"fmt"
	"slices"
	"strings"
)

// Plan is TestPlan@1 (TEST_AGENT_ARCH 4.1).
type Plan struct {
	Ticket        string      `json:"ticket"`
	Behaviours    []Behaviour `json:"behaviours"`
	UIHandles     []string    `json:"ui_handles"`
	ExpectedPaths []string    `json:"expected_paths"`
	Assumptions   []string    `json:"assumptions"`
}

// Behaviour is one thing the tests will check: the criterion it serves, its kind, its case and its oracle.
type Behaviour struct {
	ID     string `json:"id"`
	AC     string `json:"ac"`
	Kind   string `json:"kind"`
	Case   string `json:"case"`
	Given  string `json:"given"`
	When   string `json:"when,omitempty"`
	Then   string `json:"then"`
	Oracle string `json:"oracle"`
}

// Input is what the server knows: the criteria of the ticket and the kinds of the repository profile.
type Input struct {
	Criteria      []string `json:"criteria"`
	DeclaredKinds []string `json:"declared_kinds"`
}

// Refusal says what is wrong, where, and the smallest valid replacement (AGENT_INTERACTION_ARCH 6.2).
type Refusal struct {
	Field   string `json:"field"`
	Problem string `json:"problem"`
	Example string `json:"example"`
}

// oracles is the closed set of oracles (4.2).
func oracles() []string {
	return []string{"exact", "property", "metamorphic", "differential", "regression_seed"}
}

// CheckPlan applies the rules of 4.2 and returns every refusal, in rule order. An empty result accepts the plan.
func CheckPlan(in Input, p Plan) []Refusal {
	var out []Refusal
	out = append(out, criteriaCovered(in, p)...)
	out = append(out, negativeCovered(in, p)...)
	out = append(out, oraclesKnown(p)...)
	out = append(out, ruleOraclesProperty(p)...)
	out = append(out, kindsDeclared(in, p)...)
	return out
}

// nextID is the id of a behaviour that a refusal's example may add.
func nextID(p Plan) string { return fmt.Sprintf("B%d", len(p.Behaviours)+1) }

// example builds a behaviour as JSON text, the form the goldens use.
func example(id, ac, kind, c, oracle string) string {
	return fmt.Sprintf(`{"id": %q, "ac": %q, "kind": %q, "case": %q, "given": "...", "when": "...", "then": "...", "oracle": %q}`,
		id, ac, kind, c, oracle)
}

func criteriaCovered(in Input, p Plan) []Refusal {
	var out []Refusal
	for _, ac := range in.Criteria {
		if !slices.ContainsFunc(p.Behaviours, func(b Behaviour) bool { return b.AC == ac }) {
			out = append(out, Refusal{"behaviours", fmt.Sprintf("criterion %s has no behaviour", ac),
				example(nextID(p), ac, "api", "negative", "exact")})
		}
	}
	return out
}

func negativeCovered(in Input, p Plan) []Refusal {
	for _, b := range p.Behaviours {
		if b.Case == "negative" || b.Case == "boundary" {
			return nil
		}
	}
	if len(in.Criteria) == 0 {
		return nil
	}
	last := in.Criteria[len(in.Criteria)-1]
	return []Refusal{{"behaviours",
		fmt.Sprintf("no negative or boundary behaviour for any criterion (%s)", strings.Join(in.Criteria, ", ")),
		example(nextID(p), last, "api", "negative", "exact")}}
}

func oraclesKnown(p Plan) []Refusal {
	var out []Refusal
	for i, b := range p.Behaviours {
		if !slices.Contains(oracles(), b.Oracle) {
			out = append(out, Refusal{fmt.Sprintf("behaviours[%d].oracle", i),
				fmt.Sprintf("%q is not one of %s", b.Oracle, strings.Join(oracles(), ", ")),
				fmt.Sprintf(`"oracle": %q`, oracles()[0])})
		}
	}
	return out
}

func ruleOraclesProperty(p Plan) []Refusal {
	var out []Refusal
	for i, b := range p.Behaviours {
		if b.Case == "rule" && b.Oracle != "property" && b.Oracle != "metamorphic" {
			out = append(out, Refusal{fmt.Sprintf("behaviours[%d].oracle", i),
				fmt.Sprintf("a behaviour with case rule needs oracle property or metamorphic, not %s", b.Oracle),
				`"oracle": "property"`})
		}
	}
	return out
}

func kindsDeclared(in Input, p Plan) []Refusal {
	var out []Refusal
	for i, b := range p.Behaviours {
		if !slices.Contains(in.DeclaredKinds, b.Kind) {
			out = append(out, Refusal{fmt.Sprintf("behaviours[%d].kind", i),
				fmt.Sprintf("kind %q is not declared in the profile; declared: %s", b.Kind, strings.Join(in.DeclaredKinds, ", ")),
				fmt.Sprintf(`"kind": %q`, in.DeclaredKinds[0])})
		}
	}
	return out
}
