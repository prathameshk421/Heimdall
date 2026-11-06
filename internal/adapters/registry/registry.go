package registry

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/config"
)

// namePattern is the shape of an adapter name and of a required-config key: lowercase, digits, dash, underscore
// and dot, starting with a letter or digit. Names end up in config keys, CLI output and secret file names.
var namePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]*$`)

// Kind is one external-system port an adapter implements.
type Kind string

// The kinds the registry knows. A new kind is a new constant here and a new port in internal/ports.
const (
	KindVCS          Kind = "vcs"
	KindTracker      Kind = "tracker"
	KindChat         Kind = "chat"
	KindAgentRuntime Kind = "agent_runtime"
	KindMemory       Kind = "memory"
)

// Kinds returns every kind in a fixed order. The slice is fresh on each call.
func Kinds() []Kind {
	return []Kind{KindVCS, KindTracker, KindChat, KindAgentRuntime, KindMemory}
}

// Valid reports whether k is one of the kinds above.
func (k Kind) Valid() bool { return slices.Contains(Kinds(), k) }

// Status is the outcome of an adapter's Check.
type Status string

// The three outcomes of a check.
const (
	StatusConnected   Status = "connected"
	StatusNeedsConfig Status = "needs_config"
	StatusUnreachable Status = "unreachable"
)

// CheckResult is what Check returns. Detail is human text for the admin. It must never contain a secret
// value, a token or a key; it may name the secret file or the config key that is missing.
type CheckResult struct {
	Status Status
	Detail string
}

// Entry describes one adapter: how it is named, what it needs and how to build and check it.
type Entry struct {
	Name           string
	Kind           Kind
	RequiredConfig []string // secret file names and config keys the adapter needs
	// GuidedSteps are ordered operator actions specific to this integration. Steps are instructions,
	// never credential values, and are shown by connect after the secret-file requirements.
	GuidedSteps []string
	// Factory builds the adapter. It returns any because the registry is kind-agnostic; the wiring step
	// asserts the value to the port for its Kind.
	Factory func(cfg config.Config) (any, error)
	// Check is read-only: it may read secrets and call the external system, and it must not change anything there.
	Check func(ctx context.Context, cfg config.Config) CheckResult
}

// Sentinel errors. Callers branch with errors.Is.
var (
	ErrUnknownKind  = errors.New("unknown integration kind")
	ErrUnknownName  = errors.New("unknown integration name")
	ErrDuplicate    = errors.New("duplicate integration")
	ErrInvalidEntry = errors.New("invalid integration entry")
)

// SelectError is returned by Select when the name is not registered for the kind. Valid lists the
// registered names for that kind, sorted, so the message can name them.
type SelectError struct {
	Kind  Kind
	Name  string
	Valid []string
}

func (e *SelectError) Error() string {
	if len(e.Valid) == 0 {
		return fmt.Sprintf("%s %q: no %s integrations are registered", e.Kind, e.Name, e.Kind)
	}
	return fmt.Sprintf("%s %q: valid names are %s", e.Kind, e.Name, strings.Join(e.Valid, ", "))
}

// Unwrap makes errors.Is(err, ErrUnknownName) hold.
func (e *SelectError) Unwrap() error { return ErrUnknownName }

// Registry holds the entries by kind and name. The zero value is ready to use. It is a value passed
// to wiring, not package state.
type Registry struct {
	entries map[Kind]map[string]Entry
}

// Default builds a registry from the adapter list that the wiring step passes in. It fails on the first
// invalid or duplicate entry. Nothing registers itself: there is no init().
func Default(list []Entry) (*Registry, error) {
	r := &Registry{}
	for _, e := range list {
		if err := r.Register(e); err != nil {
			return nil, err
		}
	}
	return r, nil
}

// Register adds e. It refuses an invalid entry (ErrInvalidEntry) and a second entry with the same kind and name (ErrDuplicate).
func (r *Registry) Register(e Entry) error {
	if err := validateEntry(e); err != nil {
		return fmt.Errorf("register %q: %w", e.Name, err)
	}
	if _, dup := r.entries[e.Kind][e.Name]; dup {
		return fmt.Errorf("register %s %q: %w", e.Kind, e.Name, ErrDuplicate)
	}
	if r.entries == nil {
		r.entries = map[Kind]map[string]Entry{}
	}
	if r.entries[e.Kind] == nil {
		r.entries[e.Kind] = map[string]Entry{}
	}
	r.entries[e.Kind][e.Name] = e
	return nil
}

// SelectionField is the heimdall.toml key that selects a kind's adapter, for example "tracker.provider".
// It is exported so that `heimdall connect` writes the same key the registry checks.
func SelectionField(kind Kind) string {
	return selectionField(kind)
}

// validateEntry refuses anything that could not be selected, built or checked later.
func validateEntry(e Entry) error {
	var problems []error
	if !namePattern.MatchString(e.Name) {
		problems = append(problems, fmt.Errorf("name %q must match %s", e.Name, namePattern))
	}
	if !e.Kind.Valid() {
		problems = append(problems, fmt.Errorf("kind %q is not one of %v", e.Kind, Kinds()))
	}
	if e.Factory == nil {
		problems = append(problems, errors.New("factory is nil"))
	}
	if e.Check == nil {
		problems = append(problems, errors.New("check is nil"))
	}
	seen := map[string]bool{}
	for _, key := range e.RequiredConfig {
		switch {
		case !namePattern.MatchString(key) || strings.Contains(key, ".."):
			problems = append(problems, fmt.Errorf("required config %q must match %s and not contain \"..\"", key, namePattern))
		case seen[key]:
			problems = append(problems, fmt.Errorf("required config %q is listed twice", key))
		}
		seen[key] = true
	}
	if len(problems) > 0 {
		return fmt.Errorf("%w: %w", ErrInvalidEntry, errors.Join(problems...))
	}
	return nil
}

// Select returns the entry registered under kind and name. An unknown kind is ErrUnknownKind. An unknown
// name is a *SelectError that lists the valid names for the kind.
func (r *Registry) Select(kind Kind, name string) (Entry, error) {
	if !kind.Valid() {
		return Entry{}, fmt.Errorf("select %q: %w; valid kinds are %v", kind, ErrUnknownKind, Kinds())
	}
	if e, ok := r.entries[kind][name]; ok {
		return e, nil
	}
	return Entry{}, &SelectError{Kind: kind, Name: name, Valid: r.Names(kind)}
}

// Names returns the registered names for kind, sorted.
func (r *Registry) Names(kind Kind) []string {
	names := make([]string, 0, len(r.entries[kind]))
	for name := range r.entries[kind] {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// Description is one row of `heimdall connect`: what an integration is and what it needs.
type Description struct {
	Name           string
	Kind           Kind
	RequiredConfig []string
}

// Describe lists every registered integration: kinds in the order of Kinds(), names sorted within each
// kind. RequiredConfig is sorted. The output does not depend on registration order.
func (r *Registry) Describe() []Description {
	var out []Description
	for _, kind := range Kinds() {
		for _, name := range r.Names(kind) {
			e := r.entries[kind][name]
			required := slices.Clone(e.RequiredConfig)
			slices.Sort(required)
			out = append(out, Description{Name: name, Kind: kind, RequiredConfig: required})
		}
	}
	return out
}

// ValidateSelections checks every provider selection in cfg against reg and joins all problems, so the
// server refuses to start and names every typo and its valid options at once.
func ValidateSelections(cfg config.Config, reg *Registry) error {
	var problems []error
	for _, kind := range Kinds() {
		name, ok := cfg.Selection(string(kind))
		if !ok {
			continue
		}
		if _, err := reg.Select(kind, name); err != nil {
			problems = append(problems, fmt.Errorf("%s: %w", selectionField(kind), err))
		}
	}
	return errors.Join(problems...)
}

// selectionField is the heimdall.toml key that selects a kind's adapter.
func selectionField(kind Kind) string {
	if kind == KindChat {
		return "chat.bridge"
	}
	return string(kind) + ".provider"
}
