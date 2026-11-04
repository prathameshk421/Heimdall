// Package lintselftest proves the lint rules in .golangci.yml still fire. It lints fixtures that break
// each rule and fails unless every rule reports them, so a rule that silently stops working (a typo, a
// linter upgrade, a loosened exclusion) is noticed. The tests carry the build tag lintselftest because
// they run golangci-lint over a temporary module; run them with `make lint-selftest`.
//
// A fixture in testdata/lint/bad starts with header lines:
//
//	// lintselftest: path=internal/domain/fxname/x.go   where the file sits in the temporary module
//	// lintselftest: want=linter|text                    a finding by that linter whose text contains "text"
//
// then a blank line, so the header is not taken as the package comment. Fixtures in testdata/lint/good
// have only the path line and must produce no findings at all (they pin the exclusions).
package lintselftest
