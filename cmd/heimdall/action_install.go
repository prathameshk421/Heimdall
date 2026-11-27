package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"strconv"
	"strings"

	actionfiles "github.com/prathameshk421/flip_the_switch_hackathon/deploy/action"
)

// The model a new install uses when -model is not given, and its prices in USD per million tokens. The cost cap needs
// the prices, so another model must come with its own.
const (
	defaultModel        = "deepseek/deepseek-v4-flash"
	defaultModelBaseURL = "https://openrouter.ai/api/v1"
	defaultPriceInput   = "0.50"
	defaultPriceCached  = "0.50"
	defaultPriceOutput  = "2.00"
	// starterPolicy is shadow mode with a small cap: checks say what would block and block nothing.
	starterPolicy = "# Heimdall policy. The gate reads it from the merge base, never from the pull request.\n" +
		"# Switch mode to enforce when the weekly digest shows the checks are right for this repository.\n" +
		"action:\n  mode: shadow\n  budget:\n    per_pr_usd: 0.50\n    monthly_usd: 50\n"
)

var (
	fullSHA   = regexp.MustCompile(`^[0-9a-f]{40}$`)
	pinOnLine = regexp.MustCompile(`@[0-9a-f]{40}(\s+#\s*heimdall-pin)`)
)

// modelConfig is what the proof workflow reads from repository variables.
type modelConfig struct {
	model, baseURL                       string
	priceInput, priceCached, priceOutput string
}

// modelSettings fills the defaults and checks the prices. A model other than the default needs all three prices.
func modelSettings(model, baseURL, in, cached, out string) (modelConfig, error) {
	m := modelConfig{model: model, baseURL: baseURL, priceInput: in, priceCached: cached, priceOutput: out}
	if m.baseURL == "" {
		m.baseURL = defaultModelBaseURL
	}
	if m.model == "" || m.model == defaultModel {
		m.model = defaultModel
		m.priceInput = cmpOr(m.priceInput, defaultPriceInput)
		m.priceCached = cmpOr(m.priceCached, defaultPriceCached)
		m.priceOutput = cmpOr(m.priceOutput, defaultPriceOutput)
	}
	if m.priceInput == "" || m.priceCached == "" || m.priceOutput == "" {
		return modelConfig{}, fmt.Errorf("-model %s needs -price-input, -price-cached-input and -price-output "+
			"(USD per million tokens), so the cost cap can see the spend", m.model)
	}
	for _, p := range []string{m.priceInput, m.priceCached, m.priceOutput} {
		v, err := strconv.ParseFloat(p, 64)
		if err != nil || v < 0 {
			return modelConfig{}, fmt.Errorf("price %q is not a number of USD per million tokens", p)
		}
	}
	return m, nil
}

func cmpOr(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// variables are the repository variables of the model, in the order init sets them.
func (m modelConfig) variables() [][2]string {
	return [][2]string{
		{"HEIMDALL_MODEL_BASE_URL", m.baseURL}, {"HEIMDALL_MODEL", m.model},
		{"HEIMDALL_MODEL_PRICE_INPUT", m.priceInput}, {"HEIMDALL_MODEL_PRICE_CACHED_INPUT", m.priceCached},
		{"HEIMDALL_MODEL_PRICE_OUTPUT", m.priceOutput},
	}
}

// resolvePinRef is the Heimdall commit the workflows pin: the -ref flag, or the commit this binary was built from
// when the checkout was clean. A tag or a branch is refused, because either can move.
func resolvePinRef(ref string) (string, error) {
	if ref == "" {
		ref = buildCommit()
	}
	if ref == "" {
		return "", errors.New("pass -ref <full Heimdall commit SHA>; this binary was not built from a clean checkout, " +
			"so it does not know its own commit")
	}
	if !fullSHA.MatchString(ref) {
		return "", fmt.Errorf("-ref %q is not a full 40-character commit SHA; a tag or a branch can move", ref)
	}
	return ref, nil
}

// buildCommit is the VCS revision Go stamped into this binary, or "" when the checkout had changes.
func buildCommit() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	var rev, modified string
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			modified = s.Value
		}
	}
	if modified != "false" {
		return ""
	}
	return rev
}

// pinWorkflow puts ref on every `uses:` line marked heimdall-pin.
func pinWorkflow(raw []byte, ref string) ([]byte, error) {
	if !pinOnLine.Match(raw) {
		return nil, errors.New("the workflow has no line marked heimdall-pin")
	}
	return pinOnLine.ReplaceAll(raw, []byte("@"+ref+"$1")), nil
}

// writeActionFiles writes the three workflows into dir/.github/workflows, pinned to ref, and a shadow-mode
// heimdall.yaml when the repository has none. It reports what changed. It never overwrites a policy file. Every file
// access goes through an os.Root on dir, so a symlink in the checkout cannot send a write outside it.
func writeActionFiles(dir, ref string) (string, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", dir, err)
	}
	defer func() { _ = root.Close() }()
	notes, err := writeWorkflows(root, ref)
	if err != nil {
		return "", err
	}
	note, err := writePolicy(root)
	if err != nil {
		return "", err
	}
	if note != "" {
		notes = append(notes, note)
	}
	if len(notes) == 0 {
		return "workflows and policy already up to date", nil
	}
	return strings.Join(notes, "; "), nil
}

func writeWorkflows(root *os.Root, ref string) ([]string, error) {
	entries, err := actionfiles.Workflows.ReadDir("workflows")
	if err != nil {
		return nil, fmt.Errorf("read the embedded workflows: %w", err)
	}
	wfDir := filepath.Join(".github", "workflows")
	if err := root.MkdirAll(wfDir, 0o750); err != nil {
		return nil, fmt.Errorf("create %s: %w", wfDir, err)
	}
	var notes []string
	for _, e := range entries {
		raw, err := actionfiles.Workflows.ReadFile("workflows/" + e.Name())
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", e.Name(), err)
		}
		pinned, err := pinWorkflow(raw, ref)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		path := filepath.Join(wfDir, e.Name())
		if old, err := root.ReadFile(path); err == nil && bytes.Equal(old, pinned) {
			continue
		}
		if err := root.WriteFile(path, pinned, 0o600); err != nil {
			return nil, fmt.Errorf("write %s: %w", path, err)
		}
		notes = append(notes, "wrote "+filepath.ToSlash(path))
	}
	return notes, nil
}

// writePolicy creates a shadow-mode policy when there is none. An existing file is never changed.
func writePolicy(root *os.Root) (string, error) {
	raw, err := root.ReadFile(actionPolicyFile)
	switch {
	case errors.Is(err, os.ErrNotExist):
		if err := root.WriteFile(actionPolicyFile, []byte(starterPolicy), 0o600); err != nil {
			return "", fmt.Errorf("write %s: %w", actionPolicyFile, err)
		}
		return "wrote " + actionPolicyFile + " (shadow mode)", nil
	case err != nil:
		return "", fmt.Errorf("read %s: %w", actionPolicyFile, err)
	case !strings.Contains(string(raw), "action:"):
		return actionPolicyFile + " exists and has no action area; add an action area (deploy/action/README.md " +
			"step 2), or shadow mode with a $0.50 cap per PR applies", nil
	}
	return "", nil
}
