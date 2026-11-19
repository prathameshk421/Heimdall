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
	tools, err := r.server.buildTools()
	require.NoError(t, err)
	r.server.tools = tools
	return r
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
