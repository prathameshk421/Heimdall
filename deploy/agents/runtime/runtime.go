package runtime

import (
	"bytes"
	"fmt"
	"net"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Agent identifies one controller-managed Unix user and its harness directory.
type Agent struct {
	Name string
	Slot string
	User string
	Role string
}

// Config contains only deployment facts that admin init must make explicit.
type Config struct {
	ControllerUID       string
	ControllerUser      string
	ControllerEnvFile   string
	AgentsRoot          string
	FirewallGroup       string
	FirewallGID         int
	OpenCodeVersion     string
	SwitchControllerVer string
	ServerPort          int
	RelayPort           int
	OpenRouterCIDRs     []string
	Agents              []Agent
}

// Artifact is one file to be written by a deployment layer. Mode is the
// intended permission, not a request to write the file.
type Artifact struct {
	Contents []byte
	Mode     uint32
}

// Render validates config and returns the complete local deployment artifact.
func Render(config Config) (map[string]Artifact, error) {
	if err := config.validate(); err != nil {
		return nil, err
	}
	artifacts := map[string]Artifact{
		"secrets/agents.env": {
			Contents: []byte("SWITCH_CONTROLLER_SELF_UPDATE=off\n"),
			Mode:     0o600,
		},
		"systemd/switch-agent-controller-" + config.ControllerUID + ".service.d/heimdall.conf": {
			Contents: []byte(controllerDropIn(config)),
			Mode:     0o644,
		},
		"nftables/heimdall-agents.nft": {
			Contents: []byte(firewall(config)),
			Mode:     0o644,
		},
	}
	for _, agent := range config.Agents {
		path := "systemd/switch-agent-" + config.ControllerUID + "@" + agent.Slot + ".service.d/heimdall.conf"
		dir := filepath.Join(config.AgentsRoot, "agent", agent.Name)
		artifacts[path] = Artifact{Contents: []byte(agentDropIn(agent, dir)), Mode: 0o644}
	}
	return artifacts, nil
}

func (c Config) validate() error {
	if err := c.validateRequired(); err != nil {
		return err
	}
	if err := c.validatePaths(); err != nil {
		return err
	}
	if c.FirewallGID < 1 || c.ServerPort < 1 || c.ServerPort > 65535 || c.RelayPort < 1 || c.RelayPort > 65535 {
		return fmt.Errorf("runtime firewall gid and ports must be in range")
	}
	if err := c.validateIdentity(); err != nil {
		return err
	}
	if err := c.validateAgents(); err != nil {
		return err
	}
	return c.validateCIDRs()
}

func (c Config) validateRequired() error {
	for field, value := range map[string]string{
		"controller uid": c.ControllerUID, "controller user": c.ControllerUser,
		"controller env file": c.ControllerEnvFile, "agents root": c.AgentsRoot,
		"firewall group": c.FirewallGroup, "OpenCode version": c.OpenCodeVersion,
		"Switch controller version": c.SwitchControllerVer,
	} {
		if value == "" {
			return fmt.Errorf("runtime %s is required", field)
		}
	}
	if len(c.Agents) == 0 {
		return fmt.Errorf("runtime agents are required")
	}
	return nil
}

func (c Config) validatePaths() error {
	for field, value := range map[string]string{"controller env file": c.ControllerEnvFile, "agents root": c.AgentsRoot} {
		if !filepath.IsAbs(value) || strings.Contains(filepath.Clean(value), "..") {
			return fmt.Errorf("runtime %s must be an absolute clean path", field)
		}
	}
	return nil
}

func (c Config) validateIdentity() error {
	for field, value := range map[string]string{
		"controller uid": c.ControllerUID, "controller user": c.ControllerUser, "firewall group": c.FirewallGroup,
	} {
		if err := validToken(value); err != nil {
			return fmt.Errorf("runtime %s: %w", field, err)
		}
	}
	return nil
}

func (c Config) validateAgents() error {
	seenNames, seenSlots := map[string]bool{}, map[string]bool{}
	for _, agent := range c.Agents {
		if err := validToken(agent.Name); err != nil {
			return fmt.Errorf("runtime agent name: %w", err)
		}
		if err := validToken(agent.Slot); err != nil {
			return fmt.Errorf("runtime agent %s slot: %w", agent.Name, err)
		}
		if err := validToken(agent.User); err != nil {
			return fmt.Errorf("runtime agent %s user: %w", agent.Name, err)
		}
		if agent.Role == "" || seenNames[agent.Name] || seenSlots[agent.Slot] {
			return fmt.Errorf("runtime agent %s has a missing or duplicate identity", agent.Name)
		}
		seenNames[agent.Name], seenSlots[agent.Slot] = true, true
	}
	return nil
}

func (c Config) validateCIDRs() error {
	for _, cidr := range c.OpenRouterCIDRs {
		ip, network, err := net.ParseCIDR(cidr)
		if err != nil || ip.To4() == nil || network.String() == "0.0.0.0/0" {
			return fmt.Errorf("runtime OpenRouter CIDR %q is not a restricted IPv4 range", cidr)
		}
	}
	if len(c.OpenRouterCIDRs) == 0 {
		return fmt.Errorf("runtime OpenRouter CIDRs are required")
	}
	return nil
}

func validToken(value string) error {
	if value == "" || strings.ContainsAny(value, " \t\r\n/\\\x00") || value == "." || value == ".." {
		return fmt.Errorf("%q is not a safe identifier", value)
	}
	return nil
}

func controllerDropIn(c Config) string {
	return "[Unit]\nAfter=network-online.target\nWants=network-online.target\n\n[Service]\nEnvironmentFile=" + c.ControllerEnvFile + "\nRestart=on-failure\n\n# Versions are recorded here for admin init's audit output.\n# opencode=" + c.OpenCodeVersion + "\n# switch-agent-controller=" + c.SwitchControllerVer + "\n"
}

func agentDropIn(agent Agent, dir string) string {
	var b strings.Builder
	b.WriteString("[Service]\n")
	b.WriteString("User=" + agent.User + "\n")
	b.WriteString("WorkingDirectory=" + dir + "\n")
	b.WriteString("UMask=0077\n")
	b.WriteString("ProtectSystem=strict\n")
	b.WriteString("ReadWritePaths=" + filepath.Join(dir, "scratch") + "\n")
	return b.String()
}

func firewall(c Config) string {
	cidrs := append([]string(nil), c.OpenRouterCIDRs...)
	sort.Strings(cidrs)
	var b bytes.Buffer
	b.WriteString("table inet heimdall_agents {\n")
	b.WriteString("  set openrouter_ipv4 { type ipv4_addr; flags interval; elements = { ")
	b.WriteString(strings.Join(cidrs, ", "))
	b.WriteString(" } }\n")
	b.WriteString("  chain output {\n    type filter hook output priority 0; policy accept;\n")
	b.WriteString("    meta skgid " + strconv.Itoa(c.FirewallGID) + " ip daddr 169.254.169.254 drop\n")
	b.WriteString("    meta skgid " + strconv.Itoa(c.FirewallGID) + " oifname \"lo\" tcp dport " + strconv.Itoa(c.ServerPort) + " accept\n")
	b.WriteString("    meta skgid " + strconv.Itoa(c.FirewallGID) + " oifname \"lo\" tcp dport " + strconv.Itoa(c.RelayPort) + " accept\n")
	b.WriteString("    meta skgid " + strconv.Itoa(c.FirewallGID) + " ip daddr @openrouter_ipv4 tcp dport 443 accept\n")
	b.WriteString("    meta skgid " + strconv.Itoa(c.FirewallGID) + " drop\n  }\n}\n")
	return b.String()
}
