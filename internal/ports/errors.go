package ports

import "errors"

// ErrVersionConflict is returned by EventStore.Append when the change moved on since the caller loaded it
// (TECH_SPEC 2.3). The caller reloads, decides again and retries, up to five times (TECH_SPEC 2.4).
var ErrVersionConflict = errors.New("version conflict")

// ErrNotFound is returned when the thing asked for does not exist in the adapter's system.
var ErrNotFound = errors.New("not found")

// ErrConflict means an external identity exists but does not match the requested configuration.
var ErrConflict = errors.New("conflict")

// ErrInvalidRequest means the caller supplied a malformed or stale setup receipt.
var ErrInvalidRequest = errors.New("invalid request")

// ErrUnauthorized is returned when a remote system refuses the credentials or the token's permissions. Unlike
// ErrUnavailable it is not retried through the outbox: retrying cannot fix it, and doctor reports it.
var ErrUnauthorized = errors.New("unauthorized")

// ErrUnavailable is returned when a remote system cannot be reached or refuses for now. The caller records the
// state (for example the gate's agents_unreachable reason, TECH_SPEC 2.5) and retries through the outbox.
var ErrUnavailable = errors.New("unavailable")
