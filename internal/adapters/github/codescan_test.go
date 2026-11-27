package github

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

const alertsPath = repoRoot + "/code-scanning/alerts"

func alert(n int, state string, line any) map[string]any {
	return map[string]any{
		"number": n, "state": state,
		"rule": map[string]any{"id": "go/sql-injection", "severity": "error", "security_severity_level": "high", "description": "SQL built from input"},
		"tool": map[string]any{"name": "CodeQL"},
		"most_recent_instance": map[string]any{
			"ref": "refs/pull/7/merge", "commit_sha": shaHead,
			"message":  map[string]any{"text": "tainted value"},
			"location": map[string]any{"path": "pay/db.go", "start_line": line, "end_line": line},
		},
	}
}

func TestCodeScanAlertsPaginateAndMapFields(t *testing.T) {
	env := newActionEnv(t)
	next := env.f.srv.URL + alertsPath + "?per_page=100&page=2&ref=refs%2Fpull%2F7%2Fmerge&state=open"
	env.f.route("GET", alertsPath, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("ref") != "refs/pull/7/merge" || q.Get("state") != "open" {
			t.Errorf("query = %q, want ref and state", r.URL.RawQuery)
		}
		if q.Get("page") == "2" {
			writeJSON(w, http.StatusOK, []any{alert(2, "open", nil)})
			return
		}
		w.Header().Set("Link", `<`+next+`>; rel="next"`)
		writeJSON(w, http.StatusOK, []any{alert(1, "open", 12)})
	})
	got, err := env.x.CodeScanAlerts(context.Background(), testRepo, "refs/pull/7/merge", "open")
	if err != nil {
		t.Fatalf("CodeScanAlerts: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d alerts, want 2", len(got))
	}
	want := ports.CodeScanAlert{
		Number: 1, State: "open", RuleID: "go/sql-injection", Severity: "error", SecuritySeverity: "high",
		Tool: "CodeQL", Description: "SQL built from input", Message: "tainted value",
		Path: "pay/db.go", StartLine: 12, EndLine: 12, Ref: "refs/pull/7/merge", CommitSHA: shaHead,
	}
	if got[0] != want {
		t.Errorf("alert = %+v, want %+v", got[0], want)
	}
	if got[1].StartLine != 0 {
		t.Errorf("a null line = %d, want 0", got[1].StartLine)
	}
	env.wantAuth(t)
}

func TestCodeScanAlertsMapErrorsPerDocGo(t *testing.T) {
	cases := []struct {
		status int
		want   error
	}{
		{http.StatusForbidden, ports.ErrUnauthorized},
		{http.StatusNotFound, ports.ErrNotFound},
		{http.StatusServiceUnavailable, ports.ErrUnavailable},
	}
	for _, tc := range cases {
		env := newActionEnv(t)
		env.f.route("GET", alertsPath, func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, tc.status, map[string]string{"message": "Code scanning is not enabled"})
		})
		_, err := env.x.CodeScanAlerts(context.Background(), testRepo, "refs/heads/main", "")
		if !errors.Is(err, tc.want) {
			t.Errorf("status %d: error = %v, want %v", tc.status, err, tc.want)
		}
	}
}

func TestCodeScanAlertsRefuseABadRefOrState(t *testing.T) {
	env := newActionEnv(t)
	for _, ref := range []string{"", "main", "heads/main", "refs/heads", "refs/../x/y", "refs/heads/a b", "refs/heads/a..b", "/refs/heads/x"} {
		if _, err := env.x.CodeScanAlerts(context.Background(), testRepo, ref, ""); err == nil {
			t.Errorf("ref %q was accepted", ref)
		}
	}
	if _, err := env.x.CodeScanAlerts(context.Background(), testRepo, "refs/heads/main", "all&x=1"); err == nil {
		t.Error("a bad state was accepted")
	}
	if n := len(env.f.requests()); n != 0 {
		t.Errorf("%d requests were sent", n)
	}
}

func TestCodeScanAlertsRefuseABadCommitSHA(t *testing.T) {
	env := newActionEnv(t)
	bad := alert(1, "open", 1)
	bad["most_recent_instance"].(map[string]any)["commit_sha"] = "not-a-sha"
	env.f.route("GET", alertsPath, func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, http.StatusOK, []any{bad}) })
	if _, err := env.x.CodeScanAlerts(context.Background(), testRepo, "refs/heads/main", ""); err == nil {
		t.Error("a bad commit hash was accepted")
	}
}
