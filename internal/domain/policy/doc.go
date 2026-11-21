// Package policy models the policy read from heimdall.yaml at the merge base: the types, the decoding rules,
// the validator and the glob matcher that the other rules use.
//
// # Purpose
//
// The policy is the one file that sets who may approve, how big a change may be, which findings block, and
// which timeouts apply. It is data with rules: this package says what a valid policy is and nothing about what
// to do with one. The rules that consume it live in slop (TECH_SPEC 12.3) and gate (12.4).
//
// # API
//
//   - Policy and its sections: one struct per top-level key of TECH_SPEC section 16. Field tags are the YAML and
//     JSON keys, snake_case.
//   - Duration: a time.Duration written as 15m or 4h. It refuses bare numbers and negative values when read.
//   - Policy.Validate: every problem joined, each message naming its key path (`blocking.min_tier: ...`).
//   - Equal: whether two policies say the same thing; a nil and an empty list are the same.
//   - Action (area_action.go): the action key for Action mode. An absent area is valid and means shadow mode;
//     Action.Effective fills every default (budgets, trivial globs, review sources) and returns fresh slices.
//   - MatchGlob and ValidateGlob: doublestar-style globs without a dependency. A pattern with no slash matches
//     the base name at any depth.
//
// # Invariants
//
//   - Validate passes only for policies whose every reference resolves: approvers and policy owners are people,
//     domains name approver groups, required reviewers have agents, and each approver has a GitHub login.
//   - Confidence weights are the values the spec fixes (1, 0.5, 0.2), and reviewer count is 1 to 3.
//   - A duration in a valid policy is positive where the spec needs a timeout, and remind < escalate < reject.
//   - A zero Policy is invalid. Validate accepts only CurrentVersion (2, the seats shape); version 1 exists only in
//     files, and internal/adapters/policyyaml converts it before Validate. A policy that cannot be used is ErrInvalid.
//   - Decoding refuses a key with no field (KnownFields), so a typo cannot pass silently.
//
// # Failure modes
//
// A policy that fails to decode or validate fails closed: the caller refuses to start (boot), gates on
// policy_invalid (slop), or, in the reconciler, opens no row and names the problem in Missing. This package never
// repairs a value. The stronger finding tiers are not policy
// knobs: E4 can never block, and Validate refuses it as the minimum tier.
//
// # Tests
//
// policy_test.go is the spec: the golden YAML decodes, round-trips through JSON and YAML, and each broken rule
// is refused with its key path. area_action_test.go does the same for the action area, with its own golden YAML
// and the frozen default JSON. duration_test.go, fuzz_test.go and policy_validate_test.go pin the boundaries
// and the Duration format. testdata/policy.golden.json is the frozen wire format, generated once from the golden
// YAML and checked by eye against TECH_SPEC section 16.
package policy
