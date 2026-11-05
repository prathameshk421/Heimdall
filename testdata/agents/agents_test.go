package agents

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentTemplatesHaveRuntimeContract(t *testing.T) {
	t.Parallel()

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read templates: %v", err)
	}
	var templates []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".opencode.json") {
			templates = append(templates, entry.Name())
		}
	}
	if len(templates) == 0 {
		t.Fatal("no agent OpenCode templates found")
	}

	for _, name := range templates {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			data, err := os.ReadFile(filepath.Clean(name))
			if err != nil {
				t.Fatalf("read template: %v", err)
			}
			var config map[string]any
			if err := json.Unmarshal(data, &config); err != nil {
				t.Fatalf("parse JSON: %v", err)
			}
			if config["auto_mode"] != true {
				t.Errorf("auto_mode = %v, want true", config["auto_mode"])
			}
			if config["auto_approve"] != false {
				t.Errorf("auto_approve = %v, want false", config["auto_approve"])
			}
			if config["model"] != "openrouter/deepseek/deepseek-v4-flash" {
				t.Errorf("model = %v, want configured DeepSeek model", config["model"])
			}
			mcp, ok := config["mcp"].(map[string]any)
			if !ok {
				t.Fatal("mcp configuration missing")
			}
			heimdall, ok := mcp["heimdall"].(map[string]any)
			if !ok {
				t.Fatal("heimdall MCP configuration missing")
			}
			headers, ok := heimdall["headers"].(map[string]any)
			if !ok {
				t.Fatal("heimdall MCP headers missing")
			}
			authorization, ok := headers["Authorization"].(string)
			if !ok || !strings.HasPrefix(authorization, "Bearer {{file:") || !strings.HasSuffix(authorization, "}}") {
				t.Errorf("Authorization must refer to a token file, got %q", authorization)
			}
		})
	}
}

func TestAgentManifestReferencesAvailableTemplatesAndSkills(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile("agents.json")
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var manifest struct {
		Agents map[string]struct {
			Template string `json:"template"`
			Skill    string `json:"skill"`
		} `json:"agents"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("parse manifest: %v", err)
	}
	if len(manifest.Agents) != 5 {
		t.Fatalf("manifest has %d agents, want test-author, 3 reviewers, resolver", len(manifest.Agents))
	}
	for name, agent := range manifest.Agents {
		if _, err := os.Stat(filepath.Clean(agent.Template)); err != nil {
			t.Errorf("agent %q template %q is unavailable: %v", name, agent.Template, err)
		}
		skillPath := filepath.Join("..", "..", "skills", filepath.Clean(agent.Skill), "SKILL.md")
		if _, err := os.Stat(skillPath); err != nil {
			t.Errorf("agent %q skill %q is unavailable: %v", name, agent.Skill, err)
		}
	}
}
