package finding

import (
	"errors"
	"fmt"
	"strings"
)

// Answers to a refute task (REVIEW_ARCH section 6).
const (
	RefuteRefuted    = "refuted"
	RefuteNotRefuted = "not_refuted"
	RefuteProved     = "proved"
)

// MaxRefuteReasonChars bounds the one sentence a refutation carries.
const MaxRefuteReasonChars = 300

// Refutation is one seat's answer to one unproven finding. Refuted needs a location at head that is not the finding's
// own lines; not_refuted needs a sentence on where the seat looked; proved is answered with evidence, which the
// evidence stage checks.
type Refutation struct {
	Refuter  string   `json:"refuter"`
	Answer   string   `json:"answer"`
	Location Location `json:"location,omitempty"`
	Reason   string   `json:"reason"`
}

// CheckRefutation applies the refutation rules to r against the finding at filed, filed by reporters. headLines is the
// number of lines the refuted file has at head (0 when the file is absent). It returns every problem, joined.
func CheckRefutation(filed Location, reporters []string, r Refutation, headLines int) error {
	var errs []error
	if r.Refuter == "" {
		errs = append(errs, errors.New("refuter must be named"))
	}
	for _, p := range reporters {
		if p == r.Refuter {
			errs = append(errs, fmt.Errorf("refuter %s reported this finding and cannot refute it", r.Refuter))
		}
	}
	if n := len([]rune(strings.TrimSpace(r.Reason))); n == 0 || n > MaxRefuteReasonChars {
		errs = append(errs, fmt.Errorf("reason must be 1 to %d characters, got %d", MaxRefuteReasonChars, n))
	}
	switch r.Answer {
	case RefuteRefuted:
		errs = append(errs, refutedLocationProblems(filed, r.Location, headLines)...)
	case RefuteNotRefuted, RefuteProved:
	default:
		errs = append(errs, fmt.Errorf("answer %q is not refuted, not_refuted or proved", r.Answer))
	}
	return errors.Join(errs...)
}

// refutedLocationProblems: the cited location exists at head and is not the finding's own lines.
func refutedLocationProblems(filed, loc Location, headLines int) []error {
	switch {
	case CheckRepoPath(loc.File) != nil:
		return []error{fmt.Errorf("refuted needs a location: %w", CheckRepoPath(loc.File))}
	case headLines == 0:
		return []error{fmt.Errorf("location file %s does not exist at head", loc.File)}
	case loc.Lines[0] < 1 || loc.Lines[1] < loc.Lines[0] || loc.Lines[1] > headLines:
		return []error{fmt.Errorf("location lines %v are outside %s, which has %d lines at head", loc.Lines, loc.File, headLines)}
	case loc.File == filed.File && loc.Lines[0] <= filed.Lines[1] && filed.Lines[0] <= loc.Lines[1]:
		return []error{errors.New("location is the finding's own lines; a refutation must cite other code")}
	}
	return nil
}

// Refuted is the derived "is refuted" of a finding: a refutation was filed and no evidence came after it. Sequence
// numbers are the event log's; 0 means none was filed. Nothing stores a state for it (C3).
func Refuted(refuteSeq, latestEvidenceSeq int) bool {
	return refuteSeq > 0 && latestEvidenceSeq <= refuteSeq
}
