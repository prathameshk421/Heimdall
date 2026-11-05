package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/openrouter"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/config"
)

func TestRunCostJSONShape(t *testing.T) {
	var out, errOut bytes.Buffer
	deps := costDeps{
		getenv:     func(string) string { return "" },
		loadConfig: func(string) (config.Config, error) { return config.Config{}, nil },
		total: func(string) (float64, map[string]float64, error) {
			return 1.25, map[string]float64{"eval": .75, "rehearse": .5}, nil
		},
		balance: func(config.Config) (float64, error) { return 2.4, nil },
	}
	if code := runCostWithDeps([]string{"--json", "--config", "test.toml"}, &out, &errOut, deps); code != 0 {
		t.Fatalf("runCost() = %d, stderr %q", code, errOut.String())
	}
	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("JSON output: %v; output %q", err, out.String())
	}
	if len(got) != 4 || got["total_spent_usd"] != 1.25 || got["remaining_usd"] != 1.75 || got["live_balance_usd"] != 2.4 {
		t.Fatalf("JSON shape or values = %#v", got)
	}
	byPurpose, ok := got["per_purpose_usd"].(map[string]any)
	if !ok || len(byPurpose) != 2 || byPurpose["eval"] != .75 || byPurpose["rehearse"] != .5 {
		t.Fatalf("per_purpose_usd = %#v", got["per_purpose_usd"])
	}
}

func TestRunCostMissingConfig(t *testing.T) {
	var out, errOut bytes.Buffer
	called := false
	deps := costDeps{getenv: func(string) string { return "" }, loadConfig: func(string) (config.Config, error) {
		called = true
		return config.Config{}, nil
	}}
	if code := runCostWithDeps(nil, &out, &errOut, deps); code != doctorExitFail {
		t.Fatalf("runCost() = %d, want %d", code, doctorExitFail)
	}
	if called || out.Len() != 0 || !strings.Contains(errOut.String(), "no config file") {
		t.Fatalf("called=%v stdout=%q stderr=%q", called, out.String(), errOut.String())
	}
}

func TestRunCostDoesNotLeakSecret(t *testing.T) {
	const canary = "openrouter-private-canary"
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "api-key"), []byte(canary), 0o600); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(dir, "heimdall.toml")
	if err := os.WriteFile(cfgPath, []byte("secrets_dir = \""+dir+"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+canary {
			t.Errorf("authorization header was not sourced from the secret file")
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	var out, errOut bytes.Buffer
	deps := costDeps{
		getenv:     func(string) string { return "" },
		loadConfig: func(string) (config.Config, error) { return cfg, nil },
		total:      func(string) (float64, map[string]float64, error) { return 0, map[string]float64{}, nil },
		balance: func(c config.Config) (float64, error) {
			return (openrouter.Client{BaseURL: server.URL, Secrets: costSecrets{cfg: c}, SecretName: "api-key", HTTP: server.Client()}).Balance(context.Background())
		},
	}
	if code := runCostWithDeps([]string{"--config", cfgPath}, &out, &errOut, deps); code != doctorExitFail {
		t.Fatalf("runCost() = %d, want %d", code, doctorExitFail)
	}
	if strings.Contains(out.String()+errOut.String(), canary) {
		t.Fatalf("secret leaked in stdout/stderr: %q %q", out.String(), errOut.String())
	}
}
