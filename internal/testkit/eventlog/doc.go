// Package eventlog is TEST ONLY: it builds a valid, hash-chained change event log from a short script, so the
// golden logs under testdata/plan/ are real change.Event envelopes that Validate and VerifyChain accept.
//
// # Purpose
//
// A golden log must be byte-stable and pass the same checks a stored log passes. Writing ids, versions and hash
// chains by hand is error-prone, so a script line names only what a person would write (when, who, what type,
// the payload) and Build fills the envelope. It does not fold, judge or render anything: the expected view,
// Missing and status of each log are written separately, from the plan documents, and the fold is M0's code.
//
// # API
//
//   - Line: one script line: at (RFC 3339), actor ("kind:id"), type, payload.
//   - ReadScript(r): decodes a JSON Lines script strictly (unknown fields refused).
//   - Build(change, lines): the events. Version n is line n; the event id is a UUID-shaped digest of the change id
//     and the version; the correlation id is "corr-<change>"; there is no causation and no blob ref; each event is
//     chained after the one before it with change.Chain.
//   - Encode(w, events): one compact JSON object per line, no HTML escaping, a newline after each.
//
// # Invariants
//
// Build returns an error for the first line whose event does not pass change.Event.Validate (so an unknown type
// or a payload of the wrong shape never reaches a golden). The same script always gives the same bytes.
//
// # Failure modes
//
// A malformed script line or an invalid event is an error naming the line number. Nothing is written on error.
//
// # Tests
//
// eventlog_test.go: every testdata/plan/**/script.jsonl builds to the log.jsonl beside it byte for byte, and that
// log validates and chains. Run with -update to rewrite the logs after a script changes. TestViewGoldensAreWellFormed
// checks the layout testdata/plan/view/README.md promises: a policy the product accepts, whole-line prefixes, and
// the three outputs of each point.
package eventlog
