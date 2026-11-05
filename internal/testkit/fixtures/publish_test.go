//go:build fixturespush

package fixtures

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// pushLogin is the GitHub account that owns the fixture repository (CLAUDE.md: always name the account, never rely on
// the active one).
const pushLogin = "aarush323"

// askpass is a git credential helper script. Git calls it once for the user name and once for the password; the
// token comes from gh's own store and never appears in a command line or the environment of this test.
const askpass = `#!/bin/sh
case "$1" in
*sername*) echo ` + pushLogin + ` ;;
*) exec gh auth token --user ` + pushLogin + ` ;;
esac
`

// TestPublishPayDemo pushes every branch of pay-demo to the fixture repository (T31, D149). It runs only with the
// fixturespush tag, because it writes to GitHub: `make fixtures-push`.
func TestPublishPayDemo(t *testing.T) {
	const remote = "https://github.com/aarush323/heimdall-fixture-pay.git"
	dir := Materialize(t, "pay-demo")
	script := filepath.Join(t.TempDir(), "askpass.sh")
	require.NoError(t, os.WriteFile(script, []byte(askpass), 0o700))

	for _, branch := range Branches(t, "pay-demo") {
		cmd := exec.CommandContext(t.Context(), "git", "-C", dir,
			"-c", "credential.helper=", "push", "--force-with-lease", remote, branch+":refs/heads/"+branch)
		cmd.Env = append(os.Environ(), "GIT_ASKPASS="+script, "GIT_TERMINAL_PROMPT=0")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "push %s: %s", branch, strings.TrimSpace(string(out)))
		t.Logf("pushed %s", branch)
	}
}
