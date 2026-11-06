//go:build component

package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The fakes below answer the routes the three adapters call, and nothing more. Each one checks the credential the
// adapter sends, so a wrong secret wiring fails the test instead of passing. Each one records what it was asked to do.

const (
	acceptanceRepo      = "aarush323/PBL_TOS_RISK_ANALYZER"
	acceptanceBaseSHA   = "0123456789abcdef0123456789abcdef01234567"
	acceptanceBaseRef   = "heimdall-demo"
	acceptanceAPIKey    = "api-key-value"
	acceptanceAgentID   = "agent-coord-1"
	acceptanceAgentName = "heimdall-coordinator"
	acceptanceBridgeID  = "bridge-1"
	acceptanceRoomID    = "3f1c2a9e-0000-4000-8000-000000000001"
	acceptanceJiraEmail = "bot@example.test"
	acceptanceJiraToken = "jira-token-value"
	acceptanceAppID     = "7"
	acceptanceInstall   = "42"
	acceptanceKeyName   = "github_app_private_key"
	acceptanceTicketTok = "ghs_acceptance_token"
	// kanOneIssueFile is the KAN-1 issue the Jira adapter reads in its own golden tests. The acceptance test serves it
	// as the ticket body, so the acceptance test and the adapter read the same description.
	kanOneIssueFile = "../../testdata/kan/KAN-1.issue.json"
)

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// fakeJira serves the JQL search and the single-issue read. The search returns three changes: KAN-1 moves to In
// Progress with a human assignee, KAN-2 with an app assignee, and KAN-3 with no assignee.
type fakeJira struct {
	srv         *httptest.Server
	created     string
	issue       []byte
	mu          sync.Mutex
	searches    int
	ticketReads int
}

func newFakeJira(t *testing.T, created time.Time) *fakeJira {
	t.Helper()
	issue, err := os.ReadFile(kanOneIssueFile)
	require.NoError(t, err)
	f := &fakeJira{created: created.Format("2006-01-02T15:04:05.000-0700"), issue: issue}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeJira) serve(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.Header.Get("Authorization"), "Basic ") {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"message": "no basic auth"})
		return
	}
	switch r.URL.Path {
	case "/rest/api/3/search/jql":
		f.mu.Lock()
		f.searches++
		f.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"isLast": true, "issues": f.searchIssues()})
	case "/rest/api/3/issue/KAN-1":
		f.mu.Lock()
		f.ticketReads++
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(f.issue)
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"errorMessage": "no route " + r.URL.Path})
	}
}

// searchIssues lists the three changes of the acceptance scenario.
func (f *fakeJira) searchIssues() []any {
	move := func(id string) map[string]any {
		return map[string]any{"histories": []any{map[string]any{
			"id": id, "created": f.created,
			"items": []any{map[string]any{"field": "status", "fromString": "To Do", "toString": "In Progress"}},
		}}}
	}
	return []any{
		map[string]any{"key": "KAN-1",
			"fields":    map[string]any{"assignee": map[string]any{"accountId": "557058:ana", "accountType": "atlassian"}},
			"changelog": move("10001")},
		map[string]any{"key": "KAN-2",
			"fields":    map[string]any{"assignee": map[string]any{"accountId": "app-bot", "accountType": "app"}},
			"changelog": move("10002")},
		map[string]any{"key": "KAN-3",
			"fields":    map[string]any{"assignee": nil},
			"changelog": move("10003")},
	}
}

func (f *fakeJira) searchCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.searches
}

// fakeSwitch serves the two Switch operations the room needs: list_rooms and create_room.
type fakeSwitch struct {
	srv         *httptest.Server
	mu          sync.Mutex
	rooms       []map[string]any
	createCalls []string
	badAuth     int
}

func newFakeSwitch(t *testing.T) *fakeSwitch {
	t.Helper()
	f := &fakeSwitch{}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeSwitch) serve(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer "+acceptanceAPIKey {
		f.mu.Lock()
		f.badAuth++
		f.mu.Unlock()
		writeJSON(w, http.StatusUnauthorized, map[string]string{"detail": "bad key"})
		return
	}
	var args map[string]any
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&args)
	}
	switch r.URL.Path {
	case "/agents/" + acceptanceAgentID + "/ops/list_rooms":
		f.mu.Lock()
		out := append([]map[string]any{}, f.rooms...)
		f.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"result": out})
	case "/agents/" + acceptanceAgentID + "/ops/create_room":
		title, _ := args["name"].(string)
		f.mu.Lock()
		f.createCalls = append(f.createCalls, title)
		f.rooms = append(f.rooms, map[string]any{"room_id": acceptanceRoomID, "name": title, "archived": false})
		f.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"result": map[string]any{"id": acceptanceRoomID, "name": title}})
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"detail": "no route " + r.URL.Path})
	}
}

func (f *fakeSwitch) createCallTitles() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.createCalls...)
}

// refCall is one create-ref request the fake GitHub received.
type refCall struct {
	Ref string
	SHA string
}

// fakeGitHub serves the installation token, the repository and its default head, and the ref read and create that
// EnsureBranch makes.
type fakeGitHub struct {
	srv     *httptest.Server
	mu      sync.Mutex
	created map[string]bool
	refs    []refCall
}

func newFakeGitHub(t *testing.T) *fakeGitHub {
	t.Helper()
	f := &fakeGitHub{created: map[string]bool{}}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeGitHub) serve(w http.ResponseWriter, r *http.Request) {
	repoPath := "/repos/" + acceptanceRepo
	if r.Method == http.MethodPost && r.URL.Path == fmt.Sprintf("/app/installations/%s/access_tokens", acceptanceInstall) {
		writeJSON(w, http.StatusCreated, map[string]any{
			"token": acceptanceTicketTok, "expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
		})
		return
	}
	if strings.HasPrefix(r.URL.Path, repoPath) {
		f.serveRepo(w, r, repoPath)
		return
	}
	writeJSON(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
}

func (f *fakeGitHub) serveRepo(w http.ResponseWriter, r *http.Request, repoPath string) {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == repoPath:
		writeJSON(w, http.StatusOK, map[string]any{"default_branch": acceptanceBaseRef})
	case r.Method == http.MethodGet && r.URL.Path == repoPath+"/commits/"+acceptanceBaseRef:
		writeJSON(w, http.StatusOK, map[string]any{"sha": acceptanceBaseSHA})
	case r.Method == http.MethodGet && r.URL.Path == repoPath+"/pulls":
		writeJSON(w, http.StatusOK, []any{})
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, repoPath+"/git/ref/heads/"):
		f.serveRead(w, strings.TrimPrefix(r.URL.Path, repoPath+"/git/ref/heads/"))
	case r.Method == http.MethodPost && r.URL.Path == repoPath+"/git/refs":
		f.serveCreate(w, r)
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
	}
}

func (f *fakeGitHub) serveRead(w http.ResponseWriter, branch string) {
	f.mu.Lock()
	made := f.created["refs/heads/"+branch]
	f.mu.Unlock()
	if !made {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ref": "refs/heads/" + branch, "object": map[string]any{"sha": acceptanceBaseSHA}})
}

func (f *fakeGitHub) serveCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Ref string `json:"ref"`
		SHA string `json:"sha"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "bad body"})
		return
	}
	f.mu.Lock()
	f.refs = append(f.refs, refCall{Ref: body.Ref, SHA: body.SHA})
	f.created[body.Ref] = true
	f.mu.Unlock()
	writeJSON(w, http.StatusCreated, map[string]any{"ref": body.Ref})
}

func (f *fakeGitHub) refCalls() []refCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]refCall{}, f.refs...)
}

// writeAcceptanceSecrets writes the secret files the adapters read, and returns their directory. The App key is 0600,
// as the config requires for a multi-line secret.
func writeAcceptanceSecrets(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	files := map[string]string{
		"database_url":               "postgres://placeholder:placeholder@127.0.0.1:1/heimdall?sslmode=disable",
		"jira_email":                 acceptanceJiraEmail,
		"jira_api_token":             acceptanceJiraToken,
		"github_app_id":              acceptanceAppID,
		"github_app_installation_id": acceptanceInstall,
		"switch_api_key":             acceptanceAPIKey,
		"switch_registration_token":  "reg-token-value",
	}
	for name, value := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(value+"\n"), 0o600))
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, acceptanceKeyName), pemBytes, 0o600))
	return dir
}

// writeAcceptanceConfig writes heimdall.toml for the acceptance scenario and returns its path.
func writeAcceptanceConfig(t *testing.T, secretsDir string) string {
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

[[projects]]
tracker_project = "KAN"
repo = %q
base_branch = %q
`, secretsDir, acceptanceAgentID, acceptanceAgentName, acceptanceBridgeID, acceptanceRepo, acceptanceBaseRef)
	path := filepath.Join(t.TempDir(), "heimdall.toml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}

func writeFileAcceptanceConfig(t *testing.T, secretsDir, ticketsDir string) string {
	t.Helper()
	body := fmt.Sprintf(`listen = "127.0.0.1:0"
secrets_dir = %q

[database]
url_secret = "database_url"

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
provider = "file"
path = %q

[chat]
bridge = "switch"

[[projects]]
tracker_project = "KAN"
repo = %q
base_branch = %q
`, secretsDir, acceptanceAgentID, acceptanceAgentName, acceptanceBridgeID, ticketsDir, acceptanceRepo, acceptanceBaseRef)
	path := filepath.Join(t.TempDir(), "heimdall.toml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}

func writeFileAcceptanceTicket(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	body := "# KAN-1: Add refunds\nAssignee: ana\nStatus: In Progress\n\n## Acceptance criteria\n- Refunds are idempotent.\n"
	path := filepath.Join(dir, "KAN-1.md")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return dir, "Refunds are idempotent."
}
