package mcp

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/taskinput"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/skills"
)

// fullRig wires every optional dependency, so every tool the kind table names is registered.
func fullRig(t *testing.T) *rig {
	t.Helper()
	r := graphRig(t, workflow.RoleReviewer, sampleGraph())
	r.server.deps.Cards = &fakeCards{}
	r.server.deps.Profiles = &fakeProfiles{}
	r.server.deps.Sandbox = &fakeCheckSandbox{}
	r.server.deps.Checks = fakeChecks{}
	r.server.deps.Proposer = noProposals{}
	return rebuilt(t, r)
}

// The kind table, the tools, the input renderers, the schemas and the skill files agree (HARNESS_DESIGN B8).
func TestTheKindTableAgreesWithTheServer(t *testing.T) {
	r := fullRig(t)
	problems := workflow.CheckKinds(workflow.Registry{Tools: r.server.ToolNames(), Renderers: taskinput.Names(),
		Schemas: r.server.SchemaNames(), Skills: skills.All()})
	require.Empty(t, problems)
}

func TestTheKindTableCheckNamesEveryDisagreement(t *testing.T) {
	problems := workflow.CheckKinds(workflow.Registry{Tools: []string{"read_code"}, Renderers: []string{"ticket"},
		Schemas: nil, Skills: map[string]string{"tests.md": "uses `submit_review` and `read_code`"}})
	text := ""
	for _, p := range problems {
		text += p.Error() + "\n"
	}
	require.Contains(t, text, `tool "submit_tests" is not registered`)
	require.Contains(t, text, `input "repo_map" has no renderer`)
	require.Contains(t, text, `output schema "Review@2" is not registered`)
	require.Contains(t, text, `skill file "review.md" does not exist`)
	require.Contains(t, text, `names tool "submit_review", which the kind does not hold`)
}

// A tool whose dependency is down is wired but not offered: tools/list leaves it out, a call gets the unknown-tool
// answer, and the kind-table check still counts it as registered, so the server starts degraded (HARNESS_DESIGN B8).
func TestAWithheldToolIsNotOfferedButStaysRegistered(t *testing.T) {
	r := fullRig(t)
	r.server.deps.Withheld = map[string]string{"run_check": "runner unreachable"}
	r = rebuilt(t, r)

	require.NotContains(t, toolNames(listTools(t, r)), "run_check")
	require.Contains(t, toolNames(listTools(t, r)), "submit_review", "only the withheld tool is gone")
	rep := r.rpc("tools/call", `{"name":"run_check","arguments":{"check_id":"lint"}}`)
	require.NotNil(t, rep.Error, "a withheld tool is answered like an unknown one")
	require.Contains(t, r.server.ToolNames(), "run_check")
	require.Empty(t, workflow.CheckKinds(workflow.Registry{Tools: r.server.ToolNames(), Renderers: taskinput.Names(),
		Schemas: r.server.SchemaNames(), Skills: skills.All()}))
}

func TestWithholdingAToolThatIsNotRegisteredIsRefused(t *testing.T) {
	_, _, err := withhold(nil, map[string]string{"run_chek": "runner unreachable"})
	require.ErrorContains(t, err, `withheld tool "run_chek" is not registered`)
}

// rebuilt builds the tools of r again from its current deps, as NewServer does.
func rebuilt(t *testing.T, r *rig) *rig {
	t.Helper()
	tools, err := r.server.buildTools()
	require.NoError(t, err)
	r.server.tools, r.server.registered, err = withhold(tools, r.server.deps.Withheld)
	require.NoError(t, err)
	return r
}
