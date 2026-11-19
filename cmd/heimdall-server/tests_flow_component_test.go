//go:build component

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
)

// TestTheTestsFlowRunsFromTicketToCommittedTests is B9 item 1 (HARNESS_DESIGN H10) on the real wiring: a KAN ticket
// moves to In Progress; the server opens the room, makes the branch and assigns the tests task; the scripted test author
// submits tests the verifier rejects, reads the rejection in get_task, submits good tests; they are verified red first
// and committed. The developer then pushes the fix and opens the pull request, and the server runs every acceptance
// test on its head and reports heimdall/acceptance.
func TestTheTestsFlowRunsFromTicketToCommittedTests(t *testing.T) {
	started := time.Now()
	r := newFlowRig(t)
	r.runTestsFlow()

	head := r.gh.push(map[string]string{flowProduct: flowProductFixed})
	r.gh.openPR()
	r.syncPRs()
	r.settle()
	v := r.view()
	require.NotNil(t, v.PR, "the pull request is linked to the ticket")
	require.Equal(t, head, v.PR.HeadSHA)
	acceptance := r.gh.checksOn(head)["heimdall/acceptance"]
	require.Equal(t, "completed", acceptance.Status, "the acceptance run ran on the head")
	require.Equal(t, "success", acceptance.Conclusion, "the committed tests pass once the fix is pushed")
	r.syncPRs()
	require.Equal(t, change.CIConclusion("success"), r.view().PR.CI[head]["heimdall/acceptance"], "the sync reads the result back")
	require.Less(t, time.Since(started), 20*time.Second, "B9 item 1 runs in under 20 s")
}

// writeFlowConfig writes heimdall.toml for the flow rig: Jira project KAN on acme/pay, three reviewer seats.
func writeFlowConfig(t *testing.T, secretsDir string) string {
	t.Helper()
	body := fmt.Sprintf(`listen = "127.0.0.1:0"
secrets_dir = %q

[database]
url_secret = "database_url"

[jira]
base_url = "https://jira.example.test"
project_keys = ["KAN"]
email_secret = "jira_email"
api_token_secret = "jira_api_token"

[switch]
base_url = "https://switch.example.test"
agent_id = %q
agent_name = %q
bridge_id = %q
api_key_secret = "switch_api_key"
registration_token_secret = "switch_registration_token"

[github]
app_id_secret = "github_app_id"
installation_id_secret = "github_app_installation_id"
private_key_secret = "github_app_private_key"

[vcs]
provider = "github"

[tracker]
provider = "jira"

[chat]
bridge = "switch"

[agents]
test_author = "test-author"
reviewers = ["reviewer-1", "reviewer-2", "reviewer-3"]
resolver = "resolver"

[[projects]]
tracker_project = "KAN"
repo = %q
base_branch = %q
`, secretsDir, acceptanceAgentID, acceptanceAgentName, acceptanceBridgeID, flowRepo, flowBaseRef)
	path := filepath.Join(t.TempDir(), "heimdall.toml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}
