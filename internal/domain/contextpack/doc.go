// Package contextpack compiles a reviewer agent's Personalized Context Pack from verified personal
// rule cards, change impact facts, and reviewer calibration history.
//
// # Purpose
//
// Every senior engineer brings distinct domain invariants and review taste. This pure domain package
// turns one owner's verified rule cards and past review outcomes into a bounded, BiDi-stripped,
// untrusted-data-framed prompt block used by both Server Mode (`reviewbrief`) and Action Mode
// (`internal/app/action`).
//
// # Invariants
//
//   - Pure domain: zero I/O, clock, randomness, or logging.
//   - Cards guide where a reviewer looks and what repro tests it writes; they never bypass proof
//     rules or issue system instructions.
//   - All rendered card text is stripped of Unicode format/BiDi control characters (`unicode.Cf`)
//     and wrapped inside explicit `<untrusted_personal_context>` tags.
package contextpack
