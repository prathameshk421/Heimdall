//go:build live

package github

import (
	"bufio"
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// The live tests talk to the real GitHub API as the Heimdall App on the fixture repository. They are not part of make
// fast. Run one with:
//
//	go test -tags live -count=1 -run TestLiveGate -v ./internal/adapters/github/
//
// The App credentials come from the repository's .env (GITHUB_APP_ID, GITHUB_APP_INSTALLATION_ID and the path in
// GITHUB_APP_PRIVATE_KEY_PATH). Nothing from it is printed or put in an error.

const (
	liveRepo   = change.Repo("aarush323/heimdall-fixture-pay")
	repoRootUp = "../../.."
)

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

func liveAdapter(t *testing.T) *Adapter {
	t.Helper()
	env := readDotEnv(t, filepath.Join(repoRootUp, ".env"))
	appID, err := strconv.ParseInt(env["GITHUB_APP_ID"], 10, 64)
	if err != nil {
		t.Skip("GITHUB_APP_ID is missing from .env: live test skipped")
	}
	instID, err := strconv.ParseInt(env["GITHUB_APP_INSTALLATION_ID"], 10, 64)
	if err != nil {
		t.Skip("GITHUB_APP_INSTALLATION_ID is missing from .env: live test skipped")
	}
	keyPath := env["GITHUB_APP_PRIVATE_KEY_PATH"]
	if keyPath == "" {
		t.Skip("GITHUB_APP_PRIVATE_KEY_PATH is missing from .env: live test skipped")
	}
	if !filepath.IsAbs(keyPath) {
		keyPath = filepath.Join(repoRootUp, keyPath)
	}
	pemBytes, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("read the App key file: %v", err)
	}
	a, err := New(Config{
		BaseURL:    "https://api.github.com",
		App:        App{AppID: appID, InstallationID: instID, PrivateKeyPEM: pemBytes},
		HTTPClient: &http.Client{Timeout: 40 * time.Second},
		Clock:      realClock{},
		Wait: func(ctx context.Context, d time.Duration) error {
			select {
			case <-time.After(d):
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return a
}

// readDotEnv returns the KEY=VALUE lines of a dotenv file. A missing file is an empty map.
func readDotEnv(t *testing.T, path string) map[string]string {
	t.Helper()
	out := map[string]string{}
	f, err := os.Open(path)
	if err != nil {
		return out
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		out[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), `"'`)
	}
	return out
}

func liveHead(t *testing.T, a *Adapter) (int, ports.PR) {
	t.Helper()
	prs, err := a.OpenPRs(context.Background(), liveRepo)
	if err != nil {
		t.Fatalf("OpenPRs: %v", err)
	}
	if len(prs) == 0 {
		t.Fatal("the fixture repository has no open pull request: open one first")
	}
	return prs[0].Number, prs[0]
}

func TestLiveReadsTheFixture(t *testing.T) {
	a := liveAdapter(t)
	n, pr := liveHead(t, a)
	files, err := a.Files(context.Background(), liveRepo, n)
	if err != nil || len(files) == 0 {
		t.Fatalf("Files = %d, %v", len(files), err)
	}
	policy, err := a.ReadFile(context.Background(), liveRepo, pr.BaseSHA, "heimdall.yaml")
	if err != nil || len(policy) == 0 {
		t.Fatalf("ReadFile heimdall.yaml at the merge base: %d bytes, %v", len(policy), err)
	}
	if _, err := a.TreeHash(context.Background(), liveRepo, pr.HeadSHA, ""); err != nil {
		t.Fatalf("TreeHash: %v", err)
	}
	t.Logf("read PR #%d: %d changed files, policy %d bytes", n, len(files), len(policy))
}

func TestLiveGatePending(t *testing.T) {
	a := liveAdapter(t)
	_, pr := liveHead(t, a)
	err := a.SetCheck(context.Background(), liveRepo, pr.HeadSHA, ports.Check{
		Name: "heimdall/gate", Status: ports.CheckInProgress, ExternalID: "t04-spike", Summary: "T04 spike: gate pending",
	})
	if err != nil {
		t.Fatalf("SetCheck pending: %v", err)
	}
	t.Logf("posted a pending heimdall/gate on %s", pr.HeadSHA.Short())
}

func TestLiveGateSuccess(t *testing.T) {
	a := liveAdapter(t)
	_, pr := liveHead(t, a)
	err := a.SetCheck(context.Background(), liveRepo, pr.HeadSHA, ports.Check{
		Name: "heimdall/gate", Status: ports.CheckCompleted, Conclusion: change.CISuccess, ExternalID: "t04-spike", Summary: "T04 spike: gate passed",
	})
	if err != nil {
		t.Fatalf("SetCheck success: %v", err)
	}
	t.Logf("posted a successful heimdall/gate on %s", pr.HeadSHA.Short())
}

// TestLiveInstallationPermissions reads the installation's real permissions. It logs the names and levels only.
func TestLiveInstallationPermissions(t *testing.T) {
	a := liveAdapter(t)
	inst, err := a.Tokens().Installation(context.Background())
	if err != nil {
		t.Fatalf("Installation: %v", err)
	}
	t.Logf("installation holds %d permissions, all repositories: %t, %d selected", len(inst.Permissions), inst.Repositories.All, len(inst.Repositories.Names))
	for name, level := range inst.Permissions {
		t.Logf("permission %s: %s", name, level)
	}
}
