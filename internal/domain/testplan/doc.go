// Package testplan is TestPlan@1: what the tests of a ticket will check, as data, submitted before any test code
// (TEST_AGENT_ARCH 4.1). CheckPlan applies the server's rules (4.2) and returns every refusal.
//
// # Purpose
//
// A person reads six lines of given, when, then faster than two hundred lines of test code, and the server can check a
// plan with no model. The plan fixes the shape of the tests and leaves the reasoning to the author.
//
// # API
//
//   - Plan, Behaviour: the 4.1 shape. Plan has no title; the schema name carries the version.
//   - Input: the criteria of the ticket and the kinds the repository profile declares.
//   - CheckPlan(input, plan) []Refusal, in rule order; Refusal is {field, problem, example} (AGENT_INTERACTION_ARCH 6.2).
//
// # The rules (4.2, in order)
//
//  1. Every criterion has a behaviour (refused once per missing criterion).
//  2. At least one behaviour is negative or boundary, read plan-wide.
//  3. Every oracle is exact, property, metamorphic, differential or regression_seed.
//  4. A rule behaviour has oracle property or metamorphic.
//  5. Every kind is declared in the profile. The refusal lists the declared kinds.
//
// # Invariants
//
//   - Each refusal's example is the smallest valid replacement: a behaviour built from the input, or one value.
//   - A plan that is refused by one rule names that rule's field; the field path is into the plan.
//
// # Failure modes
//
//   - Nothing fails: CheckPlan is total over its inputs. A decode failure is the caller's, and the fuzz target checks
//     that decoding and checking never panic.
//
// # Tests
//
//   - testplan_test.go: testdata/plan/testplan, every valid case accepted, every refused case refused with exactly the
//     refusal written in it; FuzzDecodePlan.
//
// This package does no I/O. Layout and dependency rule: docs/TECH_SPEC.md section 2.2, enforced by internal/archcheck.
package testplan
