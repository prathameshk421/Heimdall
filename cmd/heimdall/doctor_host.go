package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/config"
)

const hostCommandTimeout = 8 * time.Second

type hostCommand func(context.Context, string, ...string) (string, error)

func realHostChecks(getenv func(string) string) func(context.Context, string) []checkResult {
	return func(ctx context.Context, configPath string) []checkResult {
		command := func(parent context.Context, name string, args ...string) (string, error) {
			cmdCtx, cancel := context.WithTimeout(parent, hostCommandTimeout)
			defer cancel()
			out, err := executeHostCommand(cmdCtx, name, args)
			if err != nil {
				return strings.TrimSpace(string(out)), fmt.Errorf("%s: %w", name, err)
			}
			return strings.TrimSpace(string(out)), nil
		}
		switchURL := getenv("HEIMDALL_SWITCH_URL")
		jiraURL := "https://atlassian.net"
		if configPath == "" {
			configPath = getenv("HEIMDALL_CONFIG")
		}
		configErr := error(nil)
		if configPath != "" {
			cfg, err := config.Load(configPath)
			if err != nil {
				configErr = fmt.Errorf("host config does not load")
			} else {
				if cfg.Switch.BaseURL != "" {
					switchURL = cfg.Switch.BaseURL
				}
				if cfg.Jira.BaseURL != "" {
					jiraURL = cfg.Jira.BaseURL
				}
			}
		}
		rows := runHostChecks(ctx, command, switchURL, jiraURL)
		if configErr != nil {
			rows = append(rows, result("host.config", statusFail, configErr.Error()))
		}
		return rows
	}
}

func executeHostCommand(ctx context.Context, name string, args []string) ([]byte, error) {
	switch name {
	case "sudo":
		return runFixedHostCommand(ctx, "sudo")
	case "systemctl":
		return runFixedHostCommand(ctx, "systemctl")
	case "docker":
		return runFixedHostCommand(ctx, "docker")
	case "nft":
		return runFixedHostCommand(ctx, "nft")
	case "nproc":
		return runFixedHostCommand(ctx, "nproc")
	case "metadata-options":
		return runMetadataOptions(ctx)
	case "free":
		return runFixedHostCommand(ctx, "free")
	case "df":
		return runFixedHostCommand(ctx, "df")
	case "outbound":
		return checkOutbound(ctx, args)
	default:
		return nil, fmt.Errorf("unsupported host check command %q", name)
	}
}

func runFixedHostCommand(ctx context.Context, name string) ([]byte, error) {
	switch name {
	case "sudo":
		return exec.CommandContext(ctx, "sudo", "-n", "true").CombinedOutput()
	case "systemctl":
		return exec.CommandContext(ctx, "systemctl", "is-system-running").CombinedOutput()
	case "docker":
		return exec.CommandContext(ctx, "docker", "version", "--format", "{{.Server.Version}}").CombinedOutput()
	case "nft":
		return exec.CommandContext(ctx, "nft", "list", "ruleset").CombinedOutput()
	case "nproc":
		return exec.CommandContext(ctx, "nproc").CombinedOutput()
	case "free":
		return exec.CommandContext(ctx, "free", "-m").CombinedOutput()
	case "df":
		return exec.CommandContext(ctx, "df", "-Pk", "/").CombinedOutput()
	default:
		return nil, fmt.Errorf("unsupported host check command %q", name)
	}
}

func checkOutbound(ctx context.Context, args []string) ([]byte, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("invalid outbound endpoint")
	}
	endpoint, err := url.Parse(args[0])
	if err != nil || endpoint.Scheme != "https" || endpoint.Host == "" || endpoint.User != nil {
		return nil, fmt.Errorf("outbound endpoint must be an HTTPS URL")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: hostCommandTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	if err := resp.Body.Close(); err != nil {
		return nil, fmt.Errorf("close outbound response: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusBadRequest {
		return []byte(resp.Status), fmt.Errorf("unexpected HTTP status %s", resp.Status)
	}
	return []byte(fmt.Sprint(resp.StatusCode)), nil
}

func runHostChecks(ctx context.Context, command hostCommand, switchURL string, jiraURL ...string) []checkResult {
	checks := []struct {
		name    string
		command string
		args    []string
	}{
		{"sudo", "sudo", []string{"-n", "true"}},
		{"systemd", "systemctl", []string{"is-system-running"}},
		{"docker", "docker", []string{"version", "--format", "{{.Server.Version}}"}},
		{"nftables", "nft", []string{"list", "ruleset"}},
	}
	rows := make([]checkResult, 0, 14)
	for _, check := range checks {
		out, err := command(ctx, check.command, check.args...)
		if err != nil {
			rows = append(rows, result("host."+check.name, statusFail, hostError(check.name, out, err)))
			continue
		}
		detail := "available"
		if check.name == "systemd" {
			if out != "running" && out != "degraded" {
				rows = append(rows, result("host.systemd", statusFail, "systemd is "+boundedDetail(out)))
				continue
			}
			detail = "system is " + out
		}
		if check.name == "docker" {
			detail = "daemon reachable, server version " + boundedDetail(out)
		}
		if check.name == "nftables" {
			if !strings.Contains(out, "169.254.169.254") || !strings.Contains(out, "meta skgid") {
				rows = append(rows, result("host.nftables", statusFail, "ruleset must block 169.254.169.254 for the agent group using meta skgid"))
				continue
			}
			detail = "ruleset contains metadata-service blocking keyed by agent group"
		}
		rows = append(rows, result("host."+check.name, statusOK, detail))
	}
	rows = append(rows, resourceCheck(ctx, command, "free", []string{"-m"}, "memory"))
	rows = append(rows, resourceCheck(ctx, command, "df", []string{"-Pk", "/"}, "disk"))
	rows = append(rows, cpuCheck(ctx, command))
	rows = append(rows, metadataOptionsCheck(ctx, command))
	return append(rows, outboundChecks(ctx, command, switchURL, jiraURL)...)
}

func outboundChecks(ctx context.Context, command hostCommand, switchURL string, jiraURL []string) []checkResult {
	jiraEndpoint := "https://atlassian.net"
	if len(jiraURL) > 0 && jiraURL[0] != "" {
		jiraEndpoint = jiraURL[0]
	}
	rows := make([]checkResult, 0, 4)
	for _, endpoint := range []struct{ name, url string }{
		{"github", "https://api.github.com"},
		{"jira", jiraEndpoint},
		{"openrouter", "https://openrouter.ai"},
	} {
		rows = append(rows, reachabilityCheck(ctx, command, endpoint.name, endpoint.url))
	}
	if switchURL == "" {
		rows = append(rows, result("host.switch", statusNotVerified, "HEIMDALL_SWITCH_URL is not configured"))
	} else {
		rows = append(rows, reachabilityCheck(ctx, command, "switch", switchURL))
	}
	return rows
}

func metadataOptionsCheck(ctx context.Context, command hostCommand) checkResult {
	out, err := command(ctx, "metadata-options")
	if err != nil {
		return result("host.metadata", statusFail, "cannot verify EC2 metadata options; confirm the VM has IMDSv2 required and hop limit 1")
	}
	var options struct {
		Tokens   string `json:"HttpTokens"`
		HopLimit int    `json:"HttpPutResponseHopLimit"`
	}
	if err := json.Unmarshal([]byte(out), &options); err != nil {
		return result("host.metadata", statusFail, "could not parse EC2 metadata options")
	}
	if options.Tokens != "required" || options.HopLimit != 1 {
		return result("host.metadata", statusFail, "EC2 metadata must require IMDSv2 and use hop limit 1")
	}
	return result("host.metadata", statusOK, "EC2 metadata requires IMDSv2 with hop limit 1")
}

func runMetadataOptions(ctx context.Context) ([]byte, error) {
	client := &http.Client{Timeout: hostCommandTimeout}
	tokenData, err := requestMetadataToken(ctx, client)
	if err != nil {
		return nil, err
	}
	identity, err := requestInstanceIdentity(ctx, client, string(tokenData))
	if err != nil {
		return nil, err
	}
	if !validEC2InstanceID(identity.InstanceID) || !validAWSRegion(identity.Region) {
		return nil, fmt.Errorf("invalid EC2 identity values")
	}
	cmd := exec.CommandContext(ctx, "aws")
	cmd.Args = append(cmd.Args, "ec2", "describe-instances", "--instance-ids", identity.InstanceID, "--region", identity.Region, "--query", "Reservations[0].Instances[0].MetadataOptions", "--output", "json")
	return cmd.Output()
}

func requestMetadataToken(ctx context.Context, client *http.Client) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, "http://169.254.169.254/latest/api/token", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-aws-ec2-metadata-token-ttl-seconds", "60")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	data, readErr := io.ReadAll(io.LimitReader(resp.Body, 4096))
	closeErr := resp.Body.Close()
	if readErr != nil || closeErr != nil || resp.StatusCode != http.StatusOK || len(data) == 0 {
		return nil, fmt.Errorf("IMDSv2 token request failed")
	}
	return data, nil
}

func requestInstanceIdentity(ctx context.Context, client *http.Client, token string) (struct {
	InstanceID string `json:"instanceId"`
	Region     string `json:"region"`
}, error) {
	type identityDocument struct {
		InstanceID string `json:"instanceId"`
		Region     string `json:"region"`
	}
	var identity identityDocument
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://169.254.169.254/latest/dynamic/instance-identity/document", nil)
	if err != nil {
		return identity, err
	}
	req.Header.Set("X-aws-ec2-metadata-token", token)
	resp, err := client.Do(req)
	if err != nil {
		return identity, err
	}
	decodeErr := json.NewDecoder(io.LimitReader(resp.Body, 16*1024)).Decode(&identity)
	closeErr := resp.Body.Close()
	if decodeErr != nil || closeErr != nil || resp.StatusCode != http.StatusOK || identity.InstanceID == "" || identity.Region == "" {
		return identity, fmt.Errorf("IMDSv2 identity document request failed")
	}
	return identity, nil
}

func validEC2InstanceID(s string) bool {
	if len(s) != 19 || !strings.HasPrefix(s, "i-") {
		return false
	}
	for _, ch := range s[2:] {
		if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') {
			return false
		}
	}
	return true
}

func validAWSRegion(s string) bool {
	if len(s) < 9 || len(s) > 16 || s[0] < 'a' || s[0] > 'z' {
		return false
	}
	for _, ch := range s[1:] {
		if (ch < 'a' || ch > 'z') && (ch < '0' || ch > '9') && ch != '-' {
			return false
		}
	}
	return true
}

func resourceCheck(ctx context.Context, command hostCommand, binary string, args []string, name string) checkResult {
	out, err := command(ctx, binary, args...)
	if err != nil {
		return result("host."+name, statusFail, hostError(name, out, err))
	}
	if name == "memory" {
		return memoryResult(out)
	}
	return diskResult(out)
}

func memoryResult(out string) checkResult {
	var total, available int64
	parsedMemory := false
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 7 || fields[0] != "Mem:" {
			continue
		}
		var err error
		total, err = strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			break
		}
		available, err = strconv.ParseInt(fields[6], 10, 64)
		parsedMemory = err == nil
		break
	}
	if !parsedMemory {
		return result("host.memory", statusFail, "could not parse free -m output")
	}
	if total < 15_000 {
		return result("host.memory", statusFail, fmt.Sprintf("%d MiB total; at least 15000 MiB is required", total))
	}
	swapTotal, ok := parseSwapTotal(out)
	if !ok || swapTotal < 4096 {
		return result("host.memory", statusFail, "at least 4096 MiB swap is required")
	}
	return result("host.memory", statusOK, fmt.Sprintf("%d MiB RAM, %d MiB swap, %d MiB available", total, swapTotal, available))
}

func parseSwapTotal(out string) (int64, bool) {
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "Swap:" {
			total, err := strconv.ParseInt(fields[1], 10, 64)
			return total, err == nil
		}
	}
	return 0, false
}

func diskResult(out string) checkResult {
	lines := strings.Split(out, "\n")
	var fields []string
	for i := len(lines) - 1; i >= 1; i-- {
		if candidate := strings.Fields(lines[i]); len(candidate) > 0 {
			fields = candidate
			break
		}
	}
	if len(fields) < 6 {
		return result("host.disk", statusFail, "could not parse df -Pk output")
	}
	if fields[5] != "/" {
		return result("host.disk", statusFail, "could not identify the root filesystem")
	}
	available, err := strconv.ParseInt(fields[3], 10, 64)
	if err != nil {
		return result("host.disk", statusFail, "could not parse root filesystem capacity")
	}
	if available < 50*1024*1024 {
		return result("host.disk", statusFail, fmt.Sprintf("%d KiB available on /; at least 52428800 KiB is required", available))
	}
	return result("host.disk", statusOK, fields[3]+" KiB available on /")
}

func cpuCheck(ctx context.Context, command hostCommand) checkResult {
	out, err := command(ctx, "nproc")
	if err != nil {
		return result("host.cpu", statusFail, hostError("cpu", out, err))
	}
	count, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil || count < 2 {
		return result("host.cpu", statusFail, "at least 2 available vCPUs are required")
	}
	return result("host.cpu", statusOK, fmt.Sprintf("%d available vCPUs", count))
}

func reachabilityCheck(ctx context.Context, command hostCommand, name, endpoint string) checkResult {
	out, err := command(ctx, "outbound", endpoint)
	if err != nil {
		return result("host.outbound."+name, statusFail, hostError(name, out, err))
	}
	return result("host.outbound."+name, statusOK, "HTTPS responded with status "+boundedDetail(out))
}

func hostError(name, output string, err error) string {
	detail := strings.TrimSpace(output)
	if detail == "" {
		detail = err.Error()
	}
	return name + " check failed: " + boundedDetail(detail)
}

func boundedDetail(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 180 {
		return s[:180]
	}
	return s
}
