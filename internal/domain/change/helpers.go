package change

import (
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
)

// maxRefLen bounds free text such as reasons and references; longer text belongs in a blob.
const maxRefLen = 256

// dateLayout is the calendar form of a pack date (TECH_SPEC 6.3).
const dateLayout = "2006-01-02"

var (
	reACID   = regexp.MustCompile(`^AC[1-9][0-9]{0,2}$`)
	reBranch = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)
)

// problems collects every field error of one Validate call, so the caller sees all of them at once.
type problems []error

// add records err under its field path when it is not nil.
func (p *problems) add(field string, err error) {
	if err != nil {
		*p = append(*p, fmt.Errorf("%s: %w", field, err))
	}
}

// require records a field error when ok is false.
func (p *problems) require(field string, ok bool, msg string) {
	if !ok {
		*p = append(*p, fmt.Errorf("%s: %s", field, msg))
	}
}

// each applies check to every item and records each failure under field[i].
func (p *problems) each[T any](field string, items []T, check func(T) error) {
	for i, item := range items {
		if err := check(item); err != nil {
			*p = append(*p, fmt.Errorf("%s[%d]: %w", field, i, err))
		}
	}
}

// join returns every recorded problem, or nil when there are none.
func (p problems) join() error { return errors.Join(p...) }

// oneOf reports whether v is one of choices. The choices are passed in, never kept in a package variable.
func oneOf[T comparable](v T, choices ...T) bool { return slices.Contains(choices, v) }

// checkEnum is the error for a value outside its enum.
func checkEnum[T ~string](v T, ok bool) error {
	if ok {
		return nil
	}
	return fmt.Errorf("%q is not an allowed value", string(v))
}

// checkText requires a non-blank value no longer than maxRefLen bytes.
func checkText(s string) error {
	if strings.TrimSpace(s) == "" {
		return errors.New("must not be blank")
	}
	if len(s) > maxRefLen {
		return fmt.Errorf("must be at most %d bytes", maxRefLen)
	}
	return nil
}

// checkAtLeastOne requires n to be 1 or more: a count of entries, or a number that starts at 1.
func checkAtLeastOne(n int) error {
	if n < 1 {
		return fmt.Errorf("has %d, needs at least 1", n)
	}
	return nil
}

// checkTicket accepts a ticket key such as PAY-142 and refuses a pr- stream id (D146).
func checkTicket(s id.ChangeID) error {
	c, err := id.ParseChangeID(string(s))
	if err != nil {
		return err
	}
	if c.IsPR() {
		return fmt.Errorf("%s is a pull request stream, not a ticket key", s)
	}
	return nil
}

// checkRepo requires owner/name.
func checkRepo(r Repo) error {
	_, err := ParseRepo(string(r))
	return err
}

// checkBranch accepts a branch name made of safe characters; the VCS adapter applies git's full rules.
func checkBranch(s string) error {
	if !reBranch.MatchString(s) || strings.Contains(s, "..") || strings.HasSuffix(s, "/") {
		return fmt.Errorf("%q is not a safe branch name", s)
	}
	return nil
}

// checkSHA requires a git hash.
func checkSHA(s id.SHA) error {
	_, err := id.ParseSHA(string(s))
	return err
}

// checkActor requires an actor id.
func checkActor(a ActorID) error {
	_, err := ParseActorID(string(a))
	return err
}

// checkTask requires a task id (tsk_ and a ULID).
func checkTask(t id.TaskID) error {
	_, err := id.ParseTaskID(string(t))
	return err
}

// checkAgent requires an agent name (TECH_SPEC 3.1).
func checkAgent(a id.AgentName) error {
	_, err := id.ParseAgentName(string(a))
	return err
}

// checkRole requires one of the LLM roles.
func checkRole(r workflow.Role) error {
	_, err := workflow.ParseRole(string(r))
	return err
}

// checkBlob requires a blob reference (blob:sha256:<64 hex>).
func checkBlob(r workflow.BlobRef) error {
	_, err := workflow.ParseBlobRef(string(r))
	return err
}

// checkRunID requires a run id (run_ and a ULID).
func checkRunID(r id.RunID) error {
	_, err := id.ParseRunID(string(r))
	return err
}

// checkDate requires a calendar date written as 2006-01-02.
func checkDate(s string) error {
	if _, err := time.Parse(dateLayout, s); err != nil {
		return fmt.Errorf("%q is not a date written as 2006-01-02", s)
	}
	return nil
}

// checkACMap checks the acceptance-criterion keys (AC1..n) and that each maps to at least one test reference.
// Keys are visited in order, so the same map always produces the same errors.
func checkACMap(m map[string][]string) error {
	var errs []error
	for _, k := range slices.Sorted(maps.Keys(m)) {
		if !reACID.MatchString(k) {
			errs = append(errs, fmt.Errorf("key %q must look like AC2", k))
		}
		if len(m[k]) == 0 {
			errs = append(errs, fmt.Errorf("%s maps to no test", k))
		}
	}
	return errors.Join(errs...)
}
