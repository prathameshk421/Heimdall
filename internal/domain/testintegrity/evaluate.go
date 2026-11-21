package testintegrity

import (
	"cmp"
	"slices"
)

// Result is the list of violations the static rules found, sorted by path, rule and subject.
type Result struct {
	Violations []Violation
}

// Blocking returns the violations that make the test_integrity clause unmet.
func (r Result) Blocking() []Violation { return r.byLevel(LevelBlock) }

// Warnings returns the violations that are shown and never block.
func (r Result) Warnings() []Violation { return r.byLevel(LevelWarn) }

func (r Result) byLevel(l Level) []Violation {
	var out []Violation
	for _, v := range r.Violations {
		if v.Level == l {
			out = append(out, v)
		}
	}
	return out
}

// Evaluate runs the static rules (rules 1 to 3 of work package A6) over the facts. It is a pure function: the same
// facts give the same list. It never fails. A language it does not know only gets the file-level rules.
func Evaluate(f Facts, o Options) Result {
	pinned := pinnedViolations(f)
	approved := f.OwnerKnown && f.OwnerApproved
	var out []Violation
	out = append(out, pinned...)
	for _, c := range f.Files {
		out = append(out, weakened(c, wholeFilePinned(f.Pinned, c) && !approved)...)
	}
	out = append(out, mockViolations(f, o)...)
	return Result{Violations: dedupe(out)}
}

// dedupe drops a repeated id and sorts, so the output order does not depend on the order of the facts.
func dedupe(vs []Violation) []Violation {
	slices.SortStableFunc(vs, func(a, b Violation) int {
		return cmp.Or(cmp.Compare(a.Path, b.Path), cmp.Compare(a.Rule, b.Rule), cmp.Compare(a.Subject, b.Subject))
	})
	return slices.CompactFunc(vs, func(a, b Violation) bool { return a.ID == b.ID })
}

func newViolation(r Rule, l Level, path, subject, message string) Violation {
	id := string(r) + ":" + path
	if subject != "" {
		id += ":" + subject
	}
	return Violation{ID: id, Rule: r, Level: l, Path: path, Subject: subject, Message: message}
}
