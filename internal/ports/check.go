package ports

import (
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
)

// MaxUntrustedText bounds text that someone outside the system wrote (a PR body, a ticket description). Such text
// is read only through a tool and never inlined into an event or a prompt (TECH_SPEC 6.3), and the bound keeps a
// single adapter response from being unbounded.
const MaxUntrustedText = 64 << 10

// problems collects every field error of one Validate call, so the caller sees all of them at once.
type problems []error

// add records err under its field path when it is not nil.
func (p *problems) add(field string, err error) {
	if err != nil {
		*p = append(*p, fmt.Errorf("%s: %w", field, err))
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

// require records a field error when ok is false.
func (p *problems) require(field string, ok bool, msg string) {
	if !ok {
		*p = append(*p, fmt.Errorf("%s: %s", field, msg))
	}
}

// join returns every recorded problem, or nil when there are none.
func (p problems) join() error { return errors.Join(p...) }

// checkText requires a non-blank value of at most maxText bytes.
func checkText(s string, maxText int) error {
	if strings.TrimSpace(s) == "" {
		return errors.New("must not be blank")
	}
	if len(s) > maxText {
		return fmt.Errorf("must be at most %d bytes", maxText)
	}
	return nil
}

// checkRelPath requires a clean relative path inside the repository, the same rule the workflow envelope uses.
func checkRelPath(p string) error {
	if p == "" || path.IsAbs(p) || path.Clean(p) != p || p == ".." || strings.HasPrefix(p, "../") {
		return fmt.Errorf("%q is not a clean relative path inside the repository", p)
	}
	return nil
}

// checkSHA requires a git hash.
func checkSHA(s id.SHA) error {
	_, err := id.ParseSHA(string(s))
	return err
}

// checkAgentName requires a Switch-safe agent name.
func checkAgentName(a id.AgentName) error {
	_, err := id.ParseAgentName(string(a))
	return err
}

// checkRole requires one of the LLM roles.
func checkRole(r workflow.Role) error {
	_, err := workflow.ParseRole(string(r))
	return err
}

// checkActor requires an actor id.
func checkActor(a change.ActorID) error {
	_, err := change.ParseActorID(string(a))
	return err
}

// checkRepo requires owner/name.
func checkRepo(r change.Repo) error {
	_, err := change.ParseRepo(string(r))
	return err
}

// checkTicket requires a ticket key (not a pr- stream).
func checkTicket(c id.ChangeID) error {
	ch, err := id.ParseChangeID(string(c))
	if err != nil {
		return err
	}
	if ch.IsPR() {
		return fmt.Errorf("%s is a pull request stream, not a ticket key", c)
	}
	return nil
}

// checkBlob requires a blob reference.
func checkBlob(r workflow.BlobRef) error {
	_, err := workflow.ParseBlobRef(string(r))
	return err
}
