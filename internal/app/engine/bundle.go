package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
)

// ContextBundle is the deterministic context bundle built from the events in the view.
type ContextBundle struct {
	AcceptanceCriteria []string `json:"acceptance_criteria,omitempty"`
	Assignee           string   `json:"assignee,omitempty"`
	BaseSHA            string   `json:"base_sha,omitempty"`
	HeadSHA            string   `json:"head_sha,omitempty"`
	Repo               string   `json:"repo,omitempty"`
	Ticket             string   `json:"ticket,omitempty"`
}

// BuildBundle builds the canonical JSON context bundle bytes and its BlobRef from the events in the view.
func BuildBundle(c ChangeView) ([]byte, workflow.BlobRef, error) {
	b := ContextBundle{
		Ticket: string(c.ID),
	}

	for _, ev := range c.Events {
		updateFromEvent(&b, ev)
	}

	raw, err := json.Marshal(b)
	if err != nil {
		return nil, "", fmt.Errorf("marshal context bundle: %w", err)
	}

	sum := sha256.Sum256(raw)
	refStr := fmt.Sprintf("blob:sha256:%s", hex.EncodeToString(sum[:]))
	ref, err := workflow.ParseBlobRef(refStr)
	if err != nil {
		return nil, "", fmt.Errorf("parse bundle blob ref: %w", err)
	}

	return raw, ref, nil
}

func updateFromEvent(b *ContextBundle, ev change.Event) {
	p, err := change.Decode(ev)
	if err == nil {
		switch v := p.(type) {
		case change.Triggered:
			applyTrigger(b, v)
		case change.BranchEnsured:
			applyBranch(b, v)
		case change.PROpened:
			applyPROpened(b, v)
		case change.PRPushed:
			applyPRPushed(b, v)
		case change.PRClosed:
			applyPRClosed(b, v)
		case change.TestsSubmitted:
			applyTestsSubmitted(b, v)
		default:
			// Other event types do not contribute to bundle fields
		}
	}
	extractRawAC(b, ev.Payload)
}

func applyTrigger(b *ContextBundle, v change.Triggered) {
	if v.Ticket != "" {
		b.Ticket = string(v.Ticket)
	}
	if v.Repo != "" {
		b.Repo = string(v.Repo)
	}
	if v.Assignee != "" {
		b.Assignee = string(v.Assignee)
	}
}

func applyBranch(b *ContextBundle, v change.BranchEnsured) {
	if v.Repo != "" && b.Repo == "" {
		b.Repo = string(v.Repo)
	}
	if v.BaseSHA != "" {
		b.BaseSHA = string(v.BaseSHA)
	}
}

func applyPROpened(b *ContextBundle, v change.PROpened) {
	if v.Repo != "" && b.Repo == "" {
		b.Repo = string(v.Repo)
	}
	if v.HeadSHA != "" {
		b.HeadSHA = string(v.HeadSHA)
	}
}

func applyPRPushed(b *ContextBundle, v change.PRPushed) {
	if v.HeadSHA != "" {
		b.HeadSHA = string(v.HeadSHA)
	}
}

func applyPRClosed(b *ContextBundle, v change.PRClosed) {
	if v.HeadSHA != "" {
		b.HeadSHA = string(v.HeadSHA)
	}
}

func applyTestsSubmitted(b *ContextBundle, v change.TestsSubmitted) {
	if v.SHA != "" && b.HeadSHA == "" {
		b.HeadSHA = string(v.SHA)
	}
	if len(b.AcceptanceCriteria) == 0 && len(v.ACMap) > 0 {
		keys := make([]string, 0, len(v.ACMap))
		for k := range v.ACMap {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		b.AcceptanceCriteria = keys
	}
}

func extractRawAC(b *ContextBundle, payload []byte) {
	if len(b.AcceptanceCriteria) > 0 || len(payload) == 0 {
		return
	}
	var rawMap map[string]json.RawMessage
	if err := json.Unmarshal(payload, &rawMap); err != nil {
		return
	}
	acRaw, ok := rawMap["acceptance_criteria"]
	if !ok {
		return
	}
	var acStrings []string
	if err := json.Unmarshal(acRaw, &acStrings); err == nil && len(acStrings) > 0 {
		slices.Sort(acStrings)
		b.AcceptanceCriteria = acStrings
	}
}
