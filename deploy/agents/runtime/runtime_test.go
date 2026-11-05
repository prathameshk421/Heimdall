package runtime

import (
	"strings"
	"testing"
)

func TestRenderProducesDeterministicRuntimeArtifacts(t *testing.T) {
	config := Config{
		ControllerUID:       "1000",
		ControllerUser:      "ubuntu",
		ControllerEnvFile:   "/etc/switch/agents.env",
		AgentsRoot:          "/var/lib/switch-agents/1000",
		FirewallGroup:       "switch-agents-1000",
		FirewallGID:         988,
		OpenCodeVersion:     "1.19.5",
		SwitchControllerVer: "0.31.0",
		ServerPort:          8080,
		RelayPort:           8000,
		OpenRouterCIDRs:     []string{"104.18.0.0/16"},
		Agents: []Agent{
			{Name: "test-author", Slot: "01", User: "sa1000-01", Role: "test-author"},
			{Name: "reviewer-1", Slot: "02", User: "sa1000-02", Role: "reviewer"},
		},
	}

	one, err := Render(config)
	if err != nil {
		t.Fatal(err)
	}
	two, err := Render(config)
	if err != nil {
		t.Fatal(err)
	}
	if diff := compareArtifacts(one, two); diff != "" {
		t.Fatal(diff)
	}

	nft := string(one["nftables/heimdall-agents.nft"].Contents)
	for _, want := range []string{
		"table inet heimdall_agents",
		"meta skgid 988",
		"ip daddr 169.254.169.254 drop",
		"tcp dport 8080 accept",
		"tcp dport 8000 accept",
	} {
		if !strings.Contains(nft, want) {
			t.Errorf("nftables missing %q:\n%s", want, nft)
		}
	}

	unit := one["systemd/switch-agent-1000@01.service.d/heimdall.conf"].Contents
	if !strings.Contains(string(unit), "User=sa1000-01") || !strings.Contains(string(unit), "UMask=0077") {
		t.Errorf("agent drop-in does not pin identity and private files:\n%s", unit)
	}
	if got := one["secrets/agents.env"].Mode; got != 0o600 {
		t.Fatalf("controller env mode = %04o, want 0600", got)
	}
}

func TestRenderRejectsUnsafeRuntimeConfiguration(t *testing.T) {
	base := Config{
		ControllerUID:       "1000",
		ControllerUser:      "ubuntu",
		ControllerEnvFile:   "/etc/switch/agents.env",
		AgentsRoot:          "/var/lib/switch-agents/1000",
		FirewallGroup:       "switch-agents-1000",
		FirewallGID:         988,
		OpenCodeVersion:     "1.19.5",
		SwitchControllerVer: "0.31.0",
		ServerPort:          8080,
		RelayPort:           8000,
		OpenRouterCIDRs:     []string{"104.18.0.0/16"},
		Agents:              []Agent{{Name: "test-author", Slot: "01", User: "sa1000-01", Role: "test-author"}},
	}
	for name, mutate := range map[string]func(*Config){
		"relative agents root": func(c *Config) { c.AgentsRoot = "var/lib/agents" },
		"duplicate slot": func(c *Config) {
			c.Agents = append(c.Agents, Agent{Name: "reviewer-1", Slot: "01", User: "sa1000-02", Role: "reviewer"})
		},
		"unrestricted cidr": func(c *Config) { c.OpenRouterCIDRs = []string{"0.0.0.0/0"} },
		"missing pin":       func(c *Config) { c.OpenCodeVersion = "" },
	} {
		t.Run(name, func(t *testing.T) {
			config := base
			mutate(&config)
			if _, err := Render(config); err == nil {
				t.Fatal("Render accepted unsafe configuration")
			}
		})
	}
}

func compareArtifacts(one, two map[string]Artifact) string {
	if len(one) != len(two) {
		return "artifact count differs"
	}
	for path, first := range one {
		second, ok := two[path]
		if !ok || string(first.Contents) != string(second.Contents) || first.Mode != second.Mode {
			return "artifact " + path + " differs"
		}
	}
	return ""
}
