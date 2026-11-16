package testset

import (
	"errors"
	"slices"
	"strings"
)

// The three answers to a surviving mutant (TEST_AGENT_ARCH 6.3).
const (
	// AnswerWeak: the tests are weak. A new or tightened test must kill the mutant (Killed).
	AnswerWeak = "weak_tests"
	// AnswerEquivalent: the mutant does not change behaviour. One sentence says why.
	AnswerEquivalent = "equivalent"
	// AnswerUnspecified: no criterion decides this behaviour. The question goes to the ticket's assignee.
	AnswerUnspecified = "unspecified"
)

// Answer is the author's answer to one survivor found on Head.
type Answer struct {
	Survivor string // the mutant's id
	Head     string // the head commit the survivor was found on
	Kind     string // AnswerWeak, AnswerEquivalent or AnswerUnspecified
	Sentence string // one sentence; required for equivalent and unspecified
	Killed   bool   // the kill check: the mutant fails on the new test and passes on head
}

// ValidAnswer returns the problem with an answer, or nil. A weak-tests answer only counts when its test kills the mutant.
func ValidAnswer(a Answer) error {
	switch a.Kind {
	case AnswerWeak:
		if !a.Killed {
			return errors.New("weak_tests needs a new test that fails on the mutant and passes on head")
		}
	case AnswerEquivalent, AnswerUnspecified:
		if strings.TrimSpace(a.Sentence) == "" {
			return errors.New(a.Kind + " needs one sentence")
		}
	default:
		return errors.New("answer kind " + a.Kind + " is not weak_tests, equivalent or unspecified")
	}
	return nil
}

// OpenSurvivors returns the survivors on head that have no valid answer for head, in the order given. An answer for
// an older head does not count: a new head resets what must be answered.
func OpenSurvivors(head string, survivors []string, answers []Answer) []string {
	answered := map[string]bool{}
	for _, a := range answers {
		if a.Head == head && ValidAnswer(a) == nil {
			answered[a.Survivor] = true
		}
	}
	var open []string
	for _, s := range survivors {
		if !slices.Contains(open, s) && !answered[s] {
			open = append(open, s)
		}
	}
	return open
}
