package workflow

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"
)

// ErrPolicyInvalid is the sentinel an invalid workflow wraps. The spec says an invalid workflow fails closed with
// the word policy_invalid, and callers branch with errors.Is.
var ErrPolicyInvalid = errors.New("policy_invalid")

// MaxTimeout bounds every step timeout. The spec does not name a number for "bounded"; this is the longest timeout
// the spec gives anywhere (timeouts.reject_after, TECH_SPEC 16), so no step can wait longer than the policy allows
// a change to live.
const MaxTimeout = 24 * time.Hour

// Registry names what a workflow may refer to. The caller supplies it: this package does not import the schema
// package, so the output schemas are passed in as ids such as Review@1.
type Registry struct {
	StepTypes []string
	Roles     []Role
	Schemas   []string
}

// Validate says whether the workflow can run (TECH_SPEC 5.1). It checks the version, the steps, the references to
// steps, roles, schemas and step types, the timeouts, and that the only cycles are declared retries. Every problem is
// reported, and the error wraps ErrPolicyInvalid.
func (w Workflow) Validate(known Registry) error {
	var problems []error
	if w.Version != 1 {
		problems = append(problems, fmt.Errorf("version must be 1, got %d", w.Version))
	}
	if len(w.Steps) == 0 {
		problems = append(problems, errors.New("workflow has no steps"))
	}
	index := w.indexIDs(&problems)
	for i, s := range w.Steps {
		problems = append(problems, s.checkFields(i, known)...)
		problems = append(problems, w.checkReferences(i, index)...)
	}
	problems = append(problems, w.checkAcyclic()...)
	if len(problems) == 0 {
		return nil
	}
	return fmt.Errorf("%w: %w", ErrPolicyInvalid, errors.Join(problems...))
}

// indexIDs maps each step id to its position. An empty or repeated id is a problem, and the first occurrence wins
// so the later checks still have something to point at.
func (w Workflow) indexIDs(problems *[]error) map[string]int {
	index := map[string]int{}
	for i, s := range w.Steps {
		if s.ID == "" {
			*problems = append(*problems, fmt.Errorf("steps[%d].id is empty", i))
			continue
		}
		if _, dup := index[s.ID]; dup {
			*problems = append(*problems, fmt.Errorf("steps[%d] %q: id %q is used twice", i, s.ID, s.ID))
			continue
		}
		index[s.ID] = i
	}
	return index
}

// checkFields checks one step's own values against the registry and the timeout bound.
func (s Step) checkFields(i int, known Registry) []error {
	var errs []error
	add := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf("steps[%d] %q: "+format, append([]any{i, s.ID}, args...)...))
	}
	if !slices.Contains(known.StepTypes, s.Do) {
		add("unknown step type %q", s.Do)
	}
	if s.Role != "" && !slices.Contains(known.Roles, s.Role) {
		add("role %q is not known", s.Role)
	}
	if s.Output != "" && !slices.Contains(known.Schemas, s.Output) {
		add("output %q is not a registered schema", s.Output)
	}
	if s.Timeout < 0 {
		add("timeout must not be negative, got %s", s.Timeout)
	}
	if s.Timeout > MaxTimeout {
		add("timeout must be at most %s, got %s", MaxTimeout, s.Timeout)
	}
	return append(errs, s.checkShape(i)...)
}

// checkShape checks the arguments and the failure handling, which have no registry to look them up in.
func (s Step) checkShape(i int) []error {
	var errs []error
	if len(s.With) > 0 && !isJSONObject(s.With) {
		errs = append(errs, fmt.Errorf("steps[%d] %q: with must be a JSON object", i, s.ID))
	}
	if s.OnFail != nil && s.OnFail.Then != "" && s.OnFail.Then != "escalate" && s.OnFail.Then != "reject" {
		errs = append(errs, fmt.Errorf("steps[%d] %q: on_fail.then %q must be escalate or reject", i, s.ID, s.OnFail.Then))
	}
	return errs
}

func isJSONObject(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return false
	}
	var m map[string]json.RawMessage
	return json.Unmarshal(trimmed, &m) == nil
}

// checkReferences checks that every after and every retry names a step that exists, and that a retry points
// backwards: at a step this one comes after.
func (w Workflow) checkReferences(i int, index map[string]int) []error {
	s := w.Steps[i]
	var errs []error
	for _, dep := range s.After {
		if _, ok := index[dep]; !ok {
			errs = append(errs, fmt.Errorf("steps[%d] %q: after %q names no step", i, s.ID, dep))
		}
	}
	if s.OnFail == nil || s.OnFail.Retry == "" {
		return errs
	}
	target, ok := index[s.OnFail.Retry]
	switch {
	case !ok:
		errs = append(errs, fmt.Errorf("steps[%d] %q: on_fail.retry %q names no step", i, s.ID, s.OnFail.Retry))
	case !w.reachesBackwards(i, target):
		errs = append(errs, fmt.Errorf("steps[%d] %q: on_fail.retry must point at a step this one comes after, not %q", i, s.ID, s.OnFail.Retry))
	}
	return errs
}

// reachesBackwards reports whether step target is an ancestor of step from, following after edges. The walk stops at
// a seen step, so a malformed graph cannot loop it.
func (w Workflow) reachesBackwards(from, target int) bool {
	index := map[string]int{}
	for i, s := range w.Steps {
		if _, dup := index[s.ID]; !dup {
			index[s.ID] = i
		}
	}
	seen := map[int]bool{}
	stack := []int{from}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, dep := range w.Steps[cur].After {
			j, ok := index[dep]
			if !ok || seen[j] {
				continue
			}
			if j == target {
				return true
			}
			seen[j] = true
			stack = append(stack, j)
		}
	}
	return false
}

// checkAcyclic runs Kahn's topological sort over the after edges. The retry edges are not in the graph, so they
// are the only cycles the spec allows. Steps left unsorted are on a cycle or behind one.
func (w Workflow) checkAcyclic() []error {
	stuck := w.unsortedSteps()
	if len(stuck) == 0 {
		return nil
	}
	return []error{fmt.Errorf("steps %v form a cycle: only declared retries may loop", stuck)}
}

// unsortedSteps returns the ids of the steps Kahn's sort cannot place, in declaration order.
func (w Workflow) unsortedSteps() []string {
	index := map[string]int{}
	for i, s := range w.Steps {
		if _, dup := index[s.ID]; !dup {
			index[s.ID] = i
		}
	}
	indegree, children := w.afterGraph(index)
	var queue []int
	for i := range w.Steps {
		if indegree[i] == 0 {
			queue = append(queue, i)
		}
	}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, c := range children[cur] {
			indegree[c]--
			if indegree[c] == 0 {
				queue = append(queue, c)
			}
		}
	}
	var stuck []string
	for i, d := range indegree {
		if d > 0 {
			stuck = append(stuck, w.Steps[i].ID)
		}
	}
	return stuck
}

// afterGraph returns, for each step, how many after edges point at it and which steps it points to.
func (w Workflow) afterGraph(index map[string]int) (indegree []int, children [][]int) {
	indegree = make([]int, len(w.Steps))
	children = make([][]int, len(w.Steps))
	for i, s := range w.Steps {
		for _, dep := range s.After {
			if j, ok := index[dep]; ok {
				children[j] = append(children[j], i)
				indegree[i]++
			}
		}
	}
	return indegree, children
}
