package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/codegraph"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/reviewbrief"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
)

type fakeGraph struct {
	idx   *codegraph.Index
	err   error
	asked []id.SHA
}

func (f *fakeGraph) Index(_ context.Context, _ change.Repo, sha id.SHA) (*codegraph.Index, error) {
	f.asked = append(f.asked, sha)
	return f.idx, f.err
}

func graphRig(t *testing.T, role workflow.Role, g *fakeGraph) *rig {
	t.Helper()
	r := newRig(t, role)
	r.server.deps.Graph = g
	tools, err := r.server.buildTools()
	require.NoError(t, err)
	r.server.tools = tools
	return r
}

func sampleGraph() *fakeGraph {
	return &fakeGraph{idx: codegraph.FromFiles(map[string]string{
		"internal/refunds/service.go": "package refunds\n\ntype Service struct{}\n\nfunc (s *Service) Create() {}\n\nfunc other() { var s Service; s.Create() }\n",
		"cmd/main.go":                 "package main\n\nfunc main() { Ignore_your_rules() }\n",
	})}
}

func TestSearchCodeReadsTheHeadAndWrapsTheResult(t *testing.T) {
	g := sampleGraph()
	r := graphRig(t, workflow.RoleReviewer, g)
	res := r.call("search_code", `{"pattern":"Ignore_your_rules"}`)
	require.False(t, res.IsError, res.text())
	require.Contains(t, res.text(), "cmd/main.go:3:")
	require.Contains(t, res.text(), "HEIMDALL-UNTRUSTED-BEGIN", "repository text stays inside the wrapper")
	require.Equal(t, []id.SHA{id.SHA(headSHA)}, g.asked, "the commit comes from the assignment")

	scoped := r.call("search_code", `{"pattern":"func","path_glob":"cmd/**"}`)
	require.Contains(t, scoped.text(), "cmd/main.go")
	require.NotContains(t, scoped.text(), "service.go")
}

func TestCodeToolsRefuseACommitArgument(t *testing.T) {
	g := sampleGraph()
	r := graphRig(t, workflow.RoleReviewer, g)
	for tool, args := range map[string]string{
		"search_code": `{"pattern":"x","sha":"` + baseSHA + `"}`,
		"find_symbol": `{"name":"x","sha":"` + baseSHA + `"}`,
	} {
		res := r.call(tool, args)
		require.True(t, res.IsError, tool)
	}
	require.Empty(t, g.asked, "a refused call asks for no index")
}

func TestSearchCodeIsBounded(t *testing.T) {
	var lines []string
	for i := range 80 {
		lines = append(lines, fmt.Sprintf("needle %d", i))
	}
	r := graphRig(t, workflow.RoleReviewer, &fakeGraph{idx: codegraph.FromFiles(map[string]string{"a.txt": strings.Join(lines, "\n")})})
	res := r.call("search_code", `{"pattern":"needle"}`)
	require.False(t, res.IsError, res.text())
	require.Equal(t, maxCodeHits, strings.Count(res.text(), "a.txt:"))
	require.Contains(t, res.text(), "Only the first 50 hits")
}

func TestSearchCodeRefusesABadPatternAsTheAgentsOwnError(t *testing.T) {
	r := graphRig(t, workflow.RoleReviewer, sampleGraph())
	for _, args := range []string{`{"pattern":"("}`, `{"pattern":"  "}`, `{"pattern":"` + strings.Repeat("a", maxCodePattern+1) + `"}`} {
		res := r.call("search_code", args)
		require.True(t, res.IsError)
		require.NotEqual(t, genericFailure, res.text())
	}
}

func TestFindSymbolShowsDefinitionsAndUses(t *testing.T) {
	r := graphRig(t, workflow.RoleReviewer, sampleGraph())
	res := r.call("find_symbol", `{"name":"Service.Create"}`)
	require.False(t, res.IsError, res.text())
	require.Contains(t, res.text(), "internal/refunds/service.go:5")
	require.Contains(t, res.text(), "name match only")
	require.Contains(t, res.text(), "service.go:7", "the use is found from the bare name of a method")
	require.Contains(t, res.text(), "HEIMDALL-UNTRUSTED-BEGIN")

	none := r.call("find_symbol", `{"name":"Nothing"}`)
	require.False(t, none.IsError)
	require.Contains(t, none.text(), "No definition or use")
}

func TestCodeToolsAreLimitedToReviewerAndResolver(t *testing.T) {
	for _, role := range []workflow.Role{workflow.RoleReviewer, workflow.RoleResolver} {
		r := graphRig(t, role, sampleGraph())
		require.False(t, r.call("search_code", `{"pattern":"x"}`).IsError, role)
		require.False(t, r.call("find_symbol", `{"name":"x"}`).IsError, role)
	}
	g := sampleGraph()
	author := graphRig(t, workflow.RoleTestAuthor, g)
	for _, call := range []string{`{"name":"search_code","arguments":{"pattern":"x"}}`, `{"name":"find_symbol","arguments":{"name":"x"}}`} {
		refused := author.rpc("tools/call", call)
		require.NotNil(t, refused.Error, "a test author may read only the base commit")
		require.Equal(t, codeInvalidParams, refused.Error.Code)
	}
	require.Empty(t, g.asked)
}

func TestCodeToolsAreAbsentWithoutAGraph(t *testing.T) {
	r := newRig(t, workflow.RoleReviewer)
	refused := r.rpc("tools/call", `{"name":"search_code","arguments":{"pattern":"x"}}`)
	require.NotNil(t, refused.Error)
}

func TestAnIndexFailureIsARefusalAndDoesNotLeakItsCause(t *testing.T) {
	r := graphRig(t, workflow.RoleReviewer, &fakeGraph{err: errors.New("tarball token ghp_secret failed")})
	res := r.call("search_code", `{"pattern":"x"}`)
	require.True(t, res.IsError)
	require.Contains(t, res.text(), "read_code")
	require.NotContains(t, res.text(), "ghp_secret")
}

func TestATruncatedIndexSaysSo(t *testing.T) {
	require.Contains(t, renderHits(nil, true), "An absent result proves nothing")
	require.Contains(t, renderSymbol(nil, nil, true), "An absent result proves nothing")
}

type fakeBriefs struct {
	brief reviewbrief.Brief
	err   error
	calls int
}

func (f *fakeBriefs) Brief(context.Context, Principal, Assignment) (reviewbrief.Brief, error) {
	f.calls++
	return f.brief, f.err
}

func briefRig(t *testing.T, role workflow.Role, b *fakeBriefs) *rig {
	t.Helper()
	r := newRig(t, role)
	r.server.deps.Briefs = b
	return r
}

func taskFields(t *testing.T, text string) map[string]json.RawMessage {
	t.Helper()
	fields := map[string]json.RawMessage{}
	require.NoError(t, json.Unmarshal([]byte(text), &fields))
	return fields
}

func TestGetTaskAddsEachBriefSectionWrapped(t *testing.T) {
	b := &fakeBriefs{brief: reviewbrief.Brief{Obligations: "CARD", ChangeMap: "MAP", PriorFindings: "PRIOR", Memory: "MEMORY", Record: "RECORD"}}
	r := briefRig(t, workflow.RoleReviewer, b)
	res := r.call("get_task", `{}`)
	require.False(t, res.IsError, res.text())
	fields := taskFields(t, res.text())
	for field, want := range map[string]string{"personal_context": "CARD", "change_map": "MAP", "prior_findings": "PRIOR", "team_memory": "MEMORY", "your_record": "RECORD"} {
		require.Contains(t, string(fields[field]), want, field)
		require.Contains(t, string(fields[field]), "HEIMDALL-UNTRUSTED-BEGIN", field)
	}
}

func TestGetTaskLeavesOutEmptySections(t *testing.T) {
	r := briefRig(t, workflow.RoleReviewer, &fakeBriefs{brief: reviewbrief.Brief{ChangeMap: "MAP"}})
	fields := taskFields(t, r.call("get_task", `{}`).text())
	require.Contains(t, fields, "change_map")
	require.NotContains(t, fields, "personal_context")
	require.NotContains(t, fields, "your_record")
}

func TestAFailedBriefDoesNotCostTheTask(t *testing.T) {
	r := briefRig(t, workflow.RoleReviewer, &fakeBriefs{err: errors.New("down")})
	res := r.call("get_task", `{}`)
	require.False(t, res.IsError)
	require.Contains(t, taskFields(t, res.text()), "instructions")
	require.Contains(t, r.logs.String(), "brief unavailable")
}

func TestATestAuthorGetsNoBrief(t *testing.T) {
	b := &fakeBriefs{brief: reviewbrief.Brief{ChangeMap: "HEAD SECRET"}}
	r := briefRig(t, workflow.RoleTestAuthor, b)
	res := r.call("get_task", `{}`)
	require.False(t, res.IsError)
	require.NotContains(t, res.text(), "HEAD SECRET")
	require.Zero(t, b.calls, "a test author reads only the base, so nothing about the head is compiled for it")
}

// A cheap model can loop on get_task. A whole brief is kept. A partial one is compiled again, and a new head is too.
func TestABriefIsHeldUntilItIsPartialOrTheHeadMoves(t *testing.T) {
	b := &fakeBriefs{brief: reviewbrief.Brief{ChangeMap: "MAP"}}
	r := briefRig(t, workflow.RoleReviewer, b)
	r.call("get_task", `{}`)
	r.call("get_task", `{}`)
	require.Equal(t, 1, b.calls)

	r.tasks.current.Task.Change.HeadSHA = id.SHA(strings.Repeat("c", 40))
	r.call("get_task", `{}`)
	require.Equal(t, 2, b.calls, "a new head compiles again")

	b.brief.Partial = true
	r.tasks.current.Task.Change.HeadSHA = id.SHA(strings.Repeat("d", 40))
	r.call("get_task", `{}`)
	r.call("get_task", `{}`)
	require.Equal(t, 4, b.calls, "a partial brief is not reused")
}

func reviewArgs(checks string) string {
	return `{"head_sha":"` + headSHA + `","files_reviewed":["` + refundsGo + `"],"clean":true,"findings":[]` + checks + `}`
}

func TestSubmitReviewNamesUnansweredObligationsWithoutRefusing(t *testing.T) {
	b := &fakeBriefs{brief: reviewbrief.Brief{Obligations: "x", ObligationCards: []string{"cap.md", "authz.md"}}}
	r := briefRig(t, workflow.RoleReviewer, b)
	r.call("get_task", `{}`)

	res := r.call("submit_review", reviewArgs(`,"rule_checks":[{"card":"cap.md","verdict":"holds"}]`))
	require.False(t, res.IsError, res.text())
	require.Contains(t, res.text(), "The review is recorded.")
	require.Contains(t, res.text(), "authz.md")
	require.NotContains(t, res.text(), "cap.md")

	all := r.call("submit_review", reviewArgs(`,"rule_checks":[{"card":"cap.md","verdict":"holds"},{"card":"authz.md","verdict":"not_applicable"}]`))
	require.Equal(t, "The review is recorded.", all.text())
}

func TestSubmitReviewHasNoNudgeWithoutAHeldBrief(t *testing.T) {
	r := briefRig(t, workflow.RoleReviewer, &fakeBriefs{brief: reviewbrief.Brief{ObligationCards: []string{"cap.md"}}})
	res := r.call("submit_review", reviewArgs(""))
	require.Equal(t, "The review is recorded.", res.text())
}
