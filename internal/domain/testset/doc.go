// Package testset models what the test-author submits: the test files, the map from acceptance criteria to the tests
// that cover them, the paths the change expects to touch, and the assumptions the author made.
//
// # Purpose
//
// The test-author's output is checked here before the sandbox runs it. The checks that need a runner (red-first,
// flake, seam imports, TECH_SPEC 12.1) are not in this package; this one says whether the submission is well formed
// and within the policy's test directory.
//
// # API
//
//   - Submission (the input of heimdall_submit_tests, TestSubmission@1), File and Expect.
//   - Submission.Validate(Expect), which returns every problem joined, each naming its field path such as
//     files[1].path or acceptance_criteria[AC2].
//   - The limits MaxFiles, MaxFileBytes, MaxExpectedPaths, MaxTestNameChars and MaxNotesChars.
//
// # Invariants
//
//   - There is at least one file, at most 32, each a clean relative path under Expect.TestsDir + "/", unique, with
//     non-blank content of at most 64 KiB. The test directory itself must be a clean relative path.
//   - Every acceptance criterion the ticket has (Expect.ACIDs) maps to at least one test name. Every key of the map is
//     such an id (AC2 form) and is one the ticket has.
//   - Test names are non-blank and at most 200 characters.
//   - Expected paths are clean, relative, unique, at most 32, and outside the test directory.
//   - Notes are at most 2000 characters.
//
// # Failure modes
//
//   - Policy is passed in through Expect and never read here. A caller that passes the wrong directory gets a clean
//     refusal, not a silent acceptance.
//   - Validation does not check that a named test exists in a file or that it fails for the right reason. Those are
//     the sandbox's job, and this package says so rather than guessing at the language.
//   - Path rules are shared with findings through finding.CheckRepoPath, so the two cannot drift.
//
// # Tests
//
//   - submission_test.go: the golden round trip, the validation table with field paths, a bad tests directory,
//     every-problem-at-once, the inclusive limits, the AC id form, and FuzzDecodeSubmission, which checks that any
//     input that decodes and validates re-encodes to an equal, valid value.
//
// Layout and dependency rule: docs/TECH_SPEC.md section 2.2, enforced by internal/archcheck.
package testset
