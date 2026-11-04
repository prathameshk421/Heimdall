// Package ids makes the identifiers the server stores: prefixed ULIDs for findings, runs, cases and tasks, and UUIDs
// for events (TECH_SPEC 3.1, 4.1).
//
// # Purpose
//
// It is the adapter of ports.IDs. The pure encoding lives in internal/domain/id (NewULID, Next) and
// internal/domain/change (EventID). This package adds the two things the domain must not have: the wall clock and
// the random source.
//
// # API
//
//   - New() *Generator: a generator that reads time from the system clock and entropy from crypto/rand.
//   - Generator implements ports.IDs: NewFinding, NewRun, NewCase, NewTask return fnd_, run_, case_ and tsk_ followed
//     by a ULID; NewEvent returns a lowercase version 4 UUID.
//
// # Invariants
//
//   - Every ULID from one Generator is strictly greater than the one before it, for all four ULID kinds together. A
//     later millisecond takes fresh entropy. The same millisecond, or a clock that steps back, keeps the previous
//     timestamp and adds one to the random part (domain Next).
//   - Each returned id has the form its domain parser accepts. The generator builds each id from a valid ULID and a
//     fixed prefix, so the ids pass ParseFindingID and the other parsers.
//   - Generator is safe for concurrent use. The zero value is not usable; call New.
//
// # Failure modes
//
//   - crypto/rand does not fail on the supported Go release (since Go 1.24 Read never returns an error), so the
//     generator ignores its error result on purpose.
//   - The port has no error result, so the generator never returns one. A clock before 1970 is read as 0
//     milliseconds, and a clock past 2^48 milliseconds (year 10889) is read as the last millisecond. The ids stay
//     valid.
//   - When the random part is used up within one millisecond, the generator moves to the next millisecond. In
//     the one case where that is impossible (the last representable millisecond, with 2^80 ids already made in
//     it), the next id is random and the strict order is not guaranteed. No real process reaches that case.
//   - The generator does not panic. The lint rule R6 forbids it.
//
// # Tests
//
// ids_test.go checks that the ids parse back with the domain parsers, that 10000 calls strictly increase, that a
// stepped-back clock and an exhausted random part still give increasing ids, and that 8 goroutines produce no
// duplicate id.
package ids
