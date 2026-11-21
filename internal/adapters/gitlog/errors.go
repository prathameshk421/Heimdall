package gitlog

import (
	"errors"
	"fmt"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

// ErrIntegrity marks a stored log that failed verification: a bad MAC, a broken hash chain, a head that does not
// match its log, or a missing or malformed file. The store refuses to read or extend such a log. Callers branch on it
// with errors.Is and read the detail with errors.As (*IntegrityError).
var ErrIntegrity = errors.New("state log failed verification")

// ErrTooLarge marks a size limit of the store (event, change file, effects file, or an API reply). Callers branch on
// it with errors.Is and read the numbers with errors.As (*LimitError).
var ErrTooLarge = errors.New("state size limit exceeded")

// ErrEffectKeyReused is returned when an effect key is already stored with different content. A deterministic key
// must always name the same action (TECH_SPEC 2.4), as in the Postgres store.
var ErrEffectKeyReused = errors.New("effect key already committed with different content")

// IntegrityError says which change failed verification and why. Reason is a fixed phrase. It never holds the key, an
// expected MAC or any file content.
type IntegrityError struct {
	Change id.ChangeID
	Reason string
	// Cause is the lower-level error, for example change.ErrChainBroken. It is nil for a MAC failure.
	Cause error
}

// Error describes the failure without any secret.
func (e *IntegrityError) Error() string {
	return fmt.Sprintf("state log of %s failed verification: %s", e.Change, e.Reason)
}

// Unwrap lets errors.Is match ErrIntegrity and the cause.
func (e *IntegrityError) Unwrap() []error {
	if e.Cause == nil {
		return []error{ErrIntegrity}
	}
	return []error{ErrIntegrity, e.Cause}
}

// LimitError says which limit was passed. What names the thing, Max is the limit and Got is the size seen (0 when the
// reader stopped at the limit and did not count further).
type LimitError struct {
	What string
	Max  int64
	Got  int64
}

// Error describes the limit.
func (e *LimitError) Error() string {
	if e.Got > 0 {
		return fmt.Sprintf("%s is %d long, the limit is %d", e.What, e.Got, e.Max)
	}
	return fmt.Sprintf("%s is longer than the limit of %d", e.What, e.Max)
}

// Unwrap makes a limit error match ErrTooLarge.
func (e *LimitError) Unwrap() error { return ErrTooLarge }

func integrity(c id.ChangeID, reason string, cause error) error {
	return &IntegrityError{Change: c, Reason: reason, Cause: cause}
}
