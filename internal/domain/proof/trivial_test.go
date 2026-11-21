package proof_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/proof"
)

func f(path string, added, deleted int) proof.File {
	return proof.File{Path: path, Added: added, Deleted: deleted}
}

func TestTrivialTable(t *testing.T) {
	sarifInput := policy.Action{Analyzers: policy.ActionAnalyzers{
		Source: policy.AnalyzerSourceSARIFArtifact, Workflow: ".github/workflows/codeql.yml", Artifact: "sarif",
	}}
	cases := []struct {
		name  string
		files []proof.File
		act   policy.Action
		skip  bool
		why   proof.TrivialWhy
	}{
		{"docs only by default globs", []proof.File{f("README.md", 3, 1), f("docs/guide/setup.txt", 10, 0)}, policy.Action{}, true, proof.TrivialPaths},
		{"markdown in a subfolder", []proof.File{f("internal/pay/NOTES.md", 4, 0)}, policy.Action{}, true, proof.TrivialPaths},
		{"lockfile only", []proof.File{f("go.sum", 20, 20)}, policy.Action{}, true, proof.TrivialPaths},
		{"generated file only", []proof.File{f("api/user.pb.go", 300, 300)}, policy.Action{}, true, proof.TrivialPaths},
		{"mixed docs and code", []proof.File{f("README.md", 1, 0), f("internal/pay/refund.go", 5, 1)}, policy.Action{}, false, proof.TrivialCode},
		{"code only", []proof.File{f("internal/pay/refund.go", 5, 1)}, policy.Action{}, false, proof.TrivialCode},
		{"a config file the policy names", []proof.File{f("config/app.yaml", 2, 2)}, policy.Action{Trivial: []string{"config/**"}}, true, proof.TrivialPaths},
		{"a policy list replaces the defaults", []proof.File{f("README.md", 2, 0)}, policy.Action{Trivial: []string{"config/**"}}, false, proof.TrivialCode},
		{"config file the policy does not name", []proof.File{f("config/app.yaml", 2, 2)}, policy.Action{}, false, proof.TrivialCode},
		{"heimdall.yaml is never trivial", []proof.File{f("heimdall.yaml", 1, 0)}, policy.Action{Trivial: []string{"*.yaml"}}, false, proof.TrivialHeimdall},
		{"heimdall.yml is never trivial", []proof.File{f("heimdall.yml", 1, 0)}, policy.Action{Trivial: []string{"*.yml"}}, false, proof.TrivialHeimdall},
		{"the .heimdall directory is never trivial", []proof.File{f(".heimdall/memory/rule.md", 1, 0)}, policy.Action{Trivial: []string{"**/*.md"}}, false, proof.TrivialHeimdall},
		{"a heimdall workflow is never trivial", []proof.File{f(".github/workflows/heimdall-gate.yml", 1, 0)}, policy.Action{Trivial: []string{".github/**"}}, false, proof.TrivialHeimdall},
		{"a heimdall workflow beside docs", []proof.File{f("README.md", 1, 0), f(".github/workflows/heimdall-proof.yaml", 1, 0)}, policy.Action{}, false, proof.TrivialHeimdall},
		{"a declared input workflow is never trivial", []proof.File{f(".github/workflows/codeql.yml", 1, 0)}, withTrivial(sarifInput, ".github/**"), false, proof.TrivialHeimdall},
		{"a workflow that is not declared can be trivial by policy", []proof.File{f(".github/workflows/codeql.yml", 1, 0)}, policy.Action{Trivial: []string{".github/**"}}, true, proof.TrivialPaths},
		{"empty diff", nil, policy.Action{}, false, proof.TrivialEmpty},
		{"empty diff with a size rule", []proof.File{}, policy.Action{TrivialLines: 50}, false, proof.TrivialEmpty},
		{"a rename out of code into docs", []proof.File{{Path: "docs/refund.md", From: "internal/pay/refund.go", Added: 0, Deleted: 0}}, policy.Action{}, false, proof.TrivialCode},
		{"a rename inside docs", []proof.File{{Path: "docs/b.md", From: "docs/a.md", Added: 1, Deleted: 1}}, policy.Action{}, true, proof.TrivialPaths},
		{"a rename out of a heimdall file", []proof.File{{Path: "docs/policy.md", From: "heimdall.yaml", Added: 1, Deleted: 1}}, policy.Action{}, false, proof.TrivialHeimdall},
		{"small code change under the size rule", []proof.File{f("internal/pay/refund.go", 2, 1)}, policy.Action{TrivialLines: 3}, true, proof.TrivialSize},
		{"code change at the size limit plus one", []proof.File{f("internal/pay/refund.go", 3, 1)}, policy.Action{TrivialLines: 3}, false, proof.TrivialCode},
		{"size rule counts every file together", []proof.File{f("a.go", 2, 0), f("b.go", 2, 0)}, policy.Action{TrivialLines: 3}, false, proof.TrivialCode},
		{"size rule counts test lines too", []proof.File{f("a.go", 1, 0), f("a_test.go", 5, 0)}, policy.Action{TrivialLines: 3}, false, proof.TrivialCode},
		{"size rule off by default", []proof.File{f("a.go", 1, 0)}, policy.Action{}, false, proof.TrivialCode},
		{"size rule never skips a heimdall file", []proof.File{f("heimdall.yaml", 1, 0)}, policy.Action{TrivialLines: 50}, false, proof.TrivialHeimdall},
		{"size rule cannot judge a file with no counted lines", []proof.File{f("logo.png", 0, 0)}, policy.Action{TrivialLines: 50}, false, proof.TrivialCode},
		{"an empty path is refused", []proof.File{f("", 1, 0)}, policy.Action{}, false, proof.TrivialInvalid},
		{"a path that climbs out is refused", []proof.File{f("docs/../internal/x.go", 1, 0)}, policy.Action{}, false, proof.TrivialInvalid},
		{"an absolute path is refused", []proof.File{f("/docs/a.md", 1, 0)}, policy.Action{}, false, proof.TrivialInvalid},
		{"a negative count is refused", []proof.File{f("README.md", -1, 0)}, policy.Action{}, false, proof.TrivialInvalid},
		{"a rename source that climbs out is refused", []proof.File{{Path: "docs/a.md", From: "../x.go", Added: 1}}, policy.Action{}, false, proof.TrivialInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := proof.Trivial(tc.files, tc.act)
			require.Equal(t, tc.skip, got.Skip, "skip: %s", got.Detail)
			require.Equal(t, tc.why, got.Why)
			require.NotEmpty(t, got.Detail, "a decision always says why")
		})
	}
}

func TestTrivialDoesNotChangeThePolicy(t *testing.T) {
	a := policy.Action{Trivial: []string{"docs/**"}}
	_ = proof.Trivial([]proof.File{f("docs/a.md", 1, 0)}, a)
	require.Equal(t, []string{"docs/**"}, a.Trivial)
}

func withTrivial(a policy.Action, globs ...string) policy.Action {
	a.Trivial = globs
	return a
}
