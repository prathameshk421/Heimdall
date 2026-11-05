package effects

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// MaxCheckSummary is the most text a check run summary may carry.
const MaxCheckSummary = 2000

// CheckPayload is the payload of a github_check effect.
type CheckPayload struct {
	Repo       change.Repo         `json:"repo"`
	SHA        id.SHA              `json:"sha"`
	Name       string              `json:"name"`
	Status     ports.CheckStatus   `json:"status"`
	Conclusion change.CIConclusion `json:"conclusion,omitempty"`
	Summary    string              `json:"summary"`
}

// Validate returns every problem with the payload.
func (p CheckPayload) Validate() error {
	var errs []string
	if _, err := change.ParseRepo(string(p.Repo)); err != nil {
		errs = append(errs, "repo: "+err.Error())
	}
	if _, err := id.ParseSHA(string(p.SHA)); err != nil {
		errs = append(errs, "sha: "+err.Error())
	}
	if strings.TrimSpace(p.Name) == "" {
		errs = append(errs, "name: must not be empty")
	}
	switch p.Status {
	case ports.CheckCompleted:
		if p.Conclusion == "" {
			errs = append(errs, "conclusion: must be set when status is completed")
		} else if !p.Conclusion.Valid() {
			errs = append(errs, "conclusion: invalid conclusion")
		}
	case ports.CheckInProgress, ports.CheckQueued:
		if p.Conclusion != "" {
			errs = append(errs, "conclusion: must be empty when status is not completed")
		}
	default:
		errs = append(errs, "status: must be queued, in_progress or completed")
	}
	if len(p.Summary) > MaxCheckSummary {
		errs = append(errs, fmt.Sprintf("summary: length %d exceeds max %d", len(p.Summary), MaxCheckSummary))
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func checkStrictObject(raw json.RawMessage) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil {
		return fmt.Errorf("payload is not JSON: %w", err)
	}
	d, ok := tok.(json.Delim)
	if !ok || d != '{' {
		return errors.New("payload must be a JSON object")
	}
	seen := make(map[string]bool)
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return fmt.Errorf("payload JSON error: %w", err)
		}
		key, ok := t.(string)
		if !ok {
			return fmt.Errorf("expected string key, got %v", t)
		}
		if seen[key] {
			return fmt.Errorf("duplicate key %q in payload", key)
		}
		seen[key] = true
		var val json.RawMessage
		if err := dec.Decode(&val); err != nil {
			return fmt.Errorf("decode value for key %q: %w", key, err)
		}
	}
	if _, err := dec.Token(); err != nil {
		return fmt.Errorf("missing closing brace: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return errors.New("payload has data after the object")
	}
	return nil
}

// DecodeCheckPayload strictly decodes and validates a github_check payload.
func DecodeCheckPayload(raw json.RawMessage) (CheckPayload, error) {
	if len(raw) == 0 {
		return CheckPayload{}, errors.New("empty payload")
	}
	if err := checkStrictObject(raw); err != nil {
		return CheckPayload{}, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var p CheckPayload
	if err := dec.Decode(&p); err != nil {
		return CheckPayload{}, fmt.Errorf("decode payload: %w", err)
	}
	if err := p.Validate(); err != nil {
		return CheckPayload{}, fmt.Errorf("validate payload: %w", err)
	}
	return p, nil
}

// GitHubCheckDeps are the dependencies of GitHubCheck.
type GitHubCheckDeps struct {
	Checks ports.CheckWriter
}

// GitHubCheck performs github_check effects.
type GitHubCheck struct {
	deps GitHubCheckDeps
}

// NewGitHubCheck builds the executor, or refuses when a dependency is missing.
func NewGitHubCheck(d GitHubCheckDeps) (GitHubCheck, error) {
	if d.Checks == nil {
		return GitHubCheck{}, fmt.Errorf("github_check executor: missing checks")
	}
	return GitHubCheck{deps: d}, nil
}

// Execute publishes the check run named by the effect and keys it with the effect key.
func (g GitHubCheck) Execute(ctx context.Context, fx change.Effect) error {
	if fx.Kind != change.EffectGitHubCheck {
		return failf(nil, "github_check executor: effect %s has kind %s", fx.Key, fx.Kind)
	}
	p, err := DecodeCheckPayload(fx.Payload)
	if err != nil {
		return failf(err, "github_check effect %s: payload refused: %v", fx.Key, err)
	}
	if fx.Target != string(p.Repo) {
		return failf(nil, "github_check effect %s: target %q is not repo %s", fx.Key, fx.Target, p.Repo)
	}
	c := ports.Check{
		Name:       p.Name,
		Status:     p.Status,
		Conclusion: p.Conclusion,
		ExternalID: string(fx.Key),
		Summary:    p.Summary,
	}
	if err := g.deps.Checks.SetCheck(ctx, p.Repo, p.SHA, c); err != nil {
		return failf(err, "github_check %s: publish check", fx.Key)
	}
	return nil
}
