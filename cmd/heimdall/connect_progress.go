package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/registry"
)

const connectProgressVersion = 1

type connectProgressFile struct {
	Version      int                    `json:"version"`
	Integrations []connectProgressEntry `json:"integrations"`
}

type connectProgressEntry struct {
	Kind   string                `json:"kind"`
	Name   string                `json:"name"`
	Status string                `json:"status"`
	Detail string                `json:"detail"`
	Steps  []connectProgressStep `json:"steps"`
}

type connectProgressStep struct {
	Text   string `json:"text"`
	Status string `json:"status"`
}

func recordConnectProgress(configPath string, plan ConnectPlan, check registry.CheckResult) error {
	path := filepath.Join(filepath.Dir(configPath), "heimdall-connect-progress.json")
	state, err := loadConnectProgress(path)
	if err != nil {
		return err
	}
	entry := makeConnectProgressEntry(plan, check)
	state.Integrations = upsertConnectProgress(state.Integrations, entry)
	return writeConnectProgress(path, state)
}

func loadConnectProgress(path string) (connectProgressFile, error) {
	state := connectProgressFile{Version: connectProgressVersion, Integrations: []connectProgressEntry{}}
	if err := readConnectProgress(path, &state); err != nil && !errors.Is(err, os.ErrNotExist) {
		return connectProgressFile{}, err
	}
	return state, nil
}

func makeConnectProgressEntry(plan ConnectPlan, check registry.CheckResult) connectProgressEntry {
	entry := connectProgressEntry{
		Kind:   string(plan.Kind),
		Name:   plan.Name,
		Status: string(check.Status),
		Detail: check.Detail,
	}
	completeAll := check.Status == registry.StatusConnected
	for _, instruction := range plan.Instructions {
		step := connectProgressStep{Text: instruction, Status: "pending"}
		if completeAll || isSecretFileInstructionComplete(instruction) {
			step.Status = "complete"
		}
		entry.Steps = append(entry.Steps, step)
	}
	return entry
}

func isSecretFileInstructionComplete(instruction string) bool {
	return strings.HasPrefix(instruction, "secret file ") && strings.HasSuffix(instruction, " is in place")
}

func upsertConnectProgress(existing []connectProgressEntry, entry connectProgressEntry) []connectProgressEntry {
	updated := false
	for i := range existing {
		if existing[i].Kind == entry.Kind && existing[i].Name == entry.Name {
			existing[i] = entry
			updated = true
			break
		}
	}
	if !updated {
		existing = append(existing, entry)
	}
	slices.SortFunc(existing, func(a, b connectProgressEntry) int {
		if a.Kind < b.Kind {
			return -1
		}
		if a.Kind > b.Kind {
			return 1
		}
		return strings.Compare(a.Name, b.Name)
	})
	return existing
}

func readConnectProgress(path string, state *connectProgressFile) error {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(filepath.Dir(absolute))
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	info, err := root.Lstat(filepath.Base(absolute))
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return errors.New("connect progress file is not a private regular file")
	}
	data, err := root.ReadFile(filepath.Base(absolute))
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, state); err != nil {
		return errors.New("connect progress file is malformed")
	}
	if state.Version != connectProgressVersion {
		return errors.New("connect progress file has an unsupported version")
	}
	return nil
}

func writeConnectProgress(path string, state connectProgressFile) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode connect progress: %w", err)
	}
	data = append(data, '\n')
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(filepath.Dir(absolute))
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	name := filepath.Base(absolute)
	if err := validateConnectProgressDestination(root, name); err != nil {
		return err
	}
	tempName, err := connectProgressTempName()
	if err != nil {
		return err
	}
	if err := writeConnectProgressTemp(root, tempName, data); err != nil {
		return err
	}
	defer func() { _ = root.Remove(tempName) }()
	return root.Rename(tempName, name)
}

func validateConnectProgressDestination(root *os.Root, name string) error {
	info, err := root.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return errors.New("connect progress destination is not a private regular file")
	}
	return nil
}

func writeConnectProgressTemp(root *os.Root, name string, data []byte) error {
	file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func connectProgressTempName() (string, error) {
	var entropy [16]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return "", err
	}
	return ".heimdall-connect-progress-" + hex.EncodeToString(entropy[:]), nil
}

func runConnectProgress(configPath string, stdout, stderr io.Writer, deps connectDeps) int {
	env, err := loadConnectEnv(configPath, deps)
	if err != nil {
		complain(stderr, "heimdall connect progress: %v\n", err)
		return connectExitFail
	}
	path := filepath.Join(filepath.Dir(env.path), "heimdall-connect-progress.json")
	var state connectProgressFile
	if err := readConnectProgress(path, &state); errors.Is(err, os.ErrNotExist) {
		_, err := fmt.Fprintln(stdout, "No saved connect steps yet. Start with heimdall connect <kind>, then resume with heimdall connect progress.")
		if err != nil {
			complain(stderr, "heimdall connect progress: write output: %v\n", err)
			return connectExitFail
		}
		return 0
	} else if err != nil {
		complain(stderr, "heimdall connect progress: %v\n", err)
		return connectExitFail
	}
	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, "INTEGRATION\tSTATUS\tSTEP"); err != nil {
		complain(stderr, "heimdall connect progress: write output: %v\n", err)
		return connectExitFail
	}
	for _, integration := range state.Integrations {
		label := integration.Kind + "/" + integration.Name
		if _, err := fmt.Fprintf(tw, "%s\t%s\t%s\n", label, integration.Status, integration.Detail); err != nil {
			complain(stderr, "heimdall connect progress: write output: %v\n", err)
			return connectExitFail
		}
		for _, step := range integration.Steps {
			if _, err := fmt.Fprintf(tw, "\t\t[%s] %s\n", step.Status, step.Text); err != nil {
				complain(stderr, "heimdall connect progress: write output: %v\n", err)
				return connectExitFail
			}
		}
	}
	if err := tw.Flush(); err != nil {
		complain(stderr, "heimdall connect progress: flush output: %v\n", err)
		return connectExitFail
	}
	return 0
}
