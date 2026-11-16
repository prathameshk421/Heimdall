package mcp

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
)

type fakePaths struct{ paths []string }

func (f fakePaths) Paths(context.Context, change.Repo, id.SHA) ([]string, error) { return f.paths, nil }

func TestFoldMapKeepsThisTicketsTestsAndFoldsOthers(t *testing.T) {
	got := foldMap([]string{"go.mod", "internal/api/api.go", "tests/heimdall/KAN-22/a_test.go",
		"tests/heimdall/KAN-22/b_test.go", "tests/heimdall/KAN-24/x_test.go"}, "KAN-24")
	require.Equal(t, "go.mod\ninternal/api/api.go\ntests/heimdall/KAN-24/x_test.go\ntests/heimdall/KAN-22/ (2 files of another ticket)", got)
}

// Live KAN-24: the ticket took 4 calls and the layout 11. Now get_task carries the criteria text and the repo map,
// read_code "." is the map, and one call reads several files.
func TestOrientationSavesTheExploringCalls(t *testing.T) {
	r := newRig(t, workflow.RoleReviewer)
	r.server.deps.Paths = fakePaths{paths: []string{"go.mod", refundsGo}}

	task := r.call("get_task", `{}`)
	require.False(t, task.IsError, task.text())
	require.Contains(t, task.text(), "AC2: AC2: a refund over the limit is rejected", "the criterion text is inline")
	require.Contains(t, task.text(), "repo_map")
	require.Contains(t, task.text(), refundsGo)

	root := r.call("read_code", `{"path":".","sha":"`+baseSHA+`"}`)
	require.False(t, root.IsError, root.text())
	require.Contains(t, root.text(), "go.mod")

	many := r.call("read_code", `{"paths":["`+refundsGo+`","no/such/file.go"],"sha":"`+headSHA+`"}`)
	require.False(t, many.IsError, many.text())
	require.Contains(t, many.text(), "h2 changed")
	require.Contains(t, many.text(), "no/such/file.go: That path does not exist at that commit.")

	tooMany := r.call("read_code", `{"paths":["`+strings.Repeat(`a","`, 8)+`a"],"sha":"`+headSHA+`"}`)
	require.True(t, tooMany.IsError)
}
