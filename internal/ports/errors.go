package ports

import "errors"

// ErrVersionConflict is returned by EventStore.Append when the change moved on since the caller loaded it
// (TECH_SPEC 2.3). The caller reloads, decides again and retries, up to five times (TECH_SPEC 2.4).
var ErrVersionConflict = errors.New("version conflict")

// ErrNotFound is returned when the thing asked for does not exist in the adapter's system.
var ErrNotFound = errors.New("not found")

// ErrUnavailable is returned when a remote system cannot be reached or refuses for now. The caller records the
// state (for example the gate's agents_unreachable reason, TECH_SPEC 2.5) and retries through the outbox.
var ErrUnavailable = errors.New("unavailable")
