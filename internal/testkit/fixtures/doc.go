// Package fixtures is TEST ONLY: it turns the hermetic seed repositories under testdata/repos into real git
// repositories in a temp dir, so tests can run the same branches the e2e story names (T31, D57, D149).
//
// Public API: Materialize builds a repo (main plus every overlay branch) and returns its path; Checkout,
// Branches, Git, CountInBranch, GoRun, Gofmt, ApplyRepro and PRBody work on a materialised repo; LoadManifest
// reads and validates the planted-findings manifest.
//
// Layout: testdata/repos/<repo>/base is the main tree. testdata/repos/<repo>/branches/<name>/ is one branch:
// the files that change relative to its parent, branch.yaml (parent, commit_message, delete) and optionally
// PR_BODY.md, which is never committed and is read by PRBody. The branch is named <repo>/<name>.
//
// Invariants: commits use one fixed author and date, so commits and trees are identical across builds.
// Git and go run with the global config off and no network (GOPROXY=off, GOTOOLCHAIN=local).
//
// Failure modes: every problem fails the test with the tool's output; nothing is skipped. A broken overlay,
// a missing parent or a manifest that points past the end of a file is an error, never a silent pass.
//
// Tests: fixtures_test.go covers base vet and test, the buggy and fixed refunds branches, manifest locations,
// the big branch's size and key, the canary, tree stability, gofmt and the ledger consumer.
package fixtures
