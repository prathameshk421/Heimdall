package bundle

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// Request contains the trusted and referenced context selected for one role.
type Request struct {
	Role         workflow.Role  `json:"role"`
	Change       string         `json:"change"`
	BaseSHA      string         `json:"base_sha"`
	HeadSHA      string         `json:"head_sha"`
	Diff         string         `json:"diff,omitempty"`
	FilesChanged []string       `json:"files_changed,omitempty"`
	Criteria     []string       `json:"criteria,omitempty"`
	CI           []string       `json:"ci,omitempty"`
	Untrusted    []UntrustedRef `json:"untrusted,omitempty"`
	Pack         string         `json:"pack,omitempty"`
	Memory       string         `json:"memory,omitempty"`
	BudgetTokens int            `json:"budget_tokens"`
}

// UntrustedRef identifies externally authored content without embedding it.
type UntrustedRef struct {
	Source string           `json:"source"`
	Ref    workflow.BlobRef `json:"ref"`
}

// Bundle contains the canonical JSON bytes for a task's context.
type Bundle struct {
	Bytes []byte
}

type document struct {
	Skill        string         `json:"skill"`
	RoleRules    string         `json:"role_rules"`
	Change       changeContext  `json:"change"`
	Diff         string         `json:"diff,omitempty"`
	FilesChanged []string       `json:"files_changed,omitempty"`
	Criteria     []string       `json:"criteria,omitempty"`
	CI           []string       `json:"ci,omitempty"`
	Pack         string         `json:"pack,omitempty"`
	Memory       string         `json:"memory,omitempty"`
	Untrusted    []UntrustedRef `json:"untrusted,omitempty"`
}

type changeContext struct {
	ID      string `json:"id"`
	BaseSHA string `json:"base_sha"`
	HeadSHA string `json:"head_sha"`
}

// Build returns canonical JSON and refuses content above the role or request budget.
func Build(req Request) (Bundle, error) {
	capTokens, ok := roleCap(req.Role)
	if !ok {
		return Bundle{}, fmt.Errorf("build %s context bundle: unsupported role", req.Role)
	}
	if req.BudgetTokens <= 0 {
		return Bundle{}, fmt.Errorf("build %s context bundle: budget must be positive, got %d", req.Role, req.BudgetTokens)
	}
	for _, item := range req.Untrusted {
		if _, err := workflow.ParseBlobRef(string(item.Ref)); err != nil {
			return Bundle{}, fmt.Errorf("build %s context bundle: untrusted %s reference: %w", req.Role, item.Source, err)
		}
	}
	content := document{
		Skill: skillFor(req.Role), RoleRules: rulesFor(req.Role),
		Change: changeContext{ID: req.Change, BaseSHA: req.BaseSHA, HeadSHA: req.HeadSHA},
		Diff:   req.Diff, FilesChanged: sortedCopy(req.FilesChanged), Criteria: sortedCopy(req.Criteria),
		CI: sortedCopy(req.CI), Pack: req.Pack, Memory: req.Memory, Untrusted: sortedRefs(req.Untrusted),
	}
	raw, err := marshalCanonical(content)
	if err != nil {
		return Bundle{}, fmt.Errorf("marshal %s context bundle: %w", req.Role, err)
	}
	limit := min(capTokens, req.BudgetTokens)
	if estimatedTokens(raw) > limit {
		return Bundle{}, fmt.Errorf("build %s context bundle: estimated %d tokens exceeds budget %d (role cap %d)", req.Role, estimatedTokens(raw), req.BudgetTokens, capTokens)
	}
	return Bundle{Bytes: raw}, nil
}

// Hash returns the SHA-256 blob reference for the bundle bytes.
func Hash(bundle Bundle) workflow.BlobRef {
	sum := sha256.Sum256(bundle.Bytes)
	ref, _ := workflow.ParseBlobRef("blob:sha256:" + hex.EncodeToString(sum[:]))
	return ref
}

// Store writes the exact bundle bytes and returns their content-addressed reference.
func Store(ctx context.Context, blobs ports.BlobStore, class ports.BlobClass, bundle Bundle) (workflow.BlobRef, error) {
	want := Hash(bundle)
	got, err := blobs.Put(ctx, class, bytes.NewReader(bundle.Bytes))
	if err != nil {
		return "", fmt.Errorf("store context bundle: %w", err)
	}
	if got != want {
		return "", fmt.Errorf("store context bundle: blob store returned %s, expected %s", got, want)
	}
	return want, nil
}

func marshalCanonical(v any) ([]byte, error) {
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(out.Bytes(), []byte("\n")), nil
}

func estimatedTokens(raw []byte) int { return (len(raw) + 3) / 4 }

func sortedCopy(values []string) []string {
	result := append([]string(nil), values...)
	slices.Sort(result)
	return result
}

func sortedRefs(values []UntrustedRef) []UntrustedRef {
	result := append([]UntrustedRef(nil), values...)
	slices.SortFunc(result, func(a, b UntrustedRef) int {
		if n := strings.Compare(a.Source, b.Source); n != 0 {
			return n
		}
		return strings.Compare(string(a.Ref), string(b.Ref))
	})
	return result
}

func roleCap(role workflow.Role) (int, bool) {
	switch role {
	case workflow.RoleTestAuthor:
		return 40000, true
	case workflow.RoleReviewer:
		return 60000, true
	case workflow.RoleResolver:
		return 30000, true
	case workflow.RoleProfiler:
		return 0, false
	default:
		return 0, false
	}
}

func skillFor(role workflow.Role) string { return string(role) + "@1.0.0" }

func rulesFor(role workflow.Role) string {
	switch role {
	case workflow.RoleTestAuthor:
		return "Write tests for every acceptance criterion; use only the base source and declared seam."
	case workflow.RoleReviewer:
		return "Review the change from the owner's perspective; file only evidence-backed findings."
	case workflow.RoleResolver:
		return "Recommend a verdict from the case evidence and cite prior decisions with validity times."
	case workflow.RoleProfiler:
		return ""
	default:
		return ""
	}
}
