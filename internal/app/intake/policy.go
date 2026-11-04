package intake

import (
	"context"
	"errors"
	"fmt"
	"unicode/utf8"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// policyPath is the file the policy lives in, at the root of the repository (TECH_SPEC 16).
const policyPath = "heimdall.yaml"

// maxReason bounds the reason a policy is refused, because it is placed in a failing rule name, and the event
// payload bounds such text (ports and change allow 256 bytes).
const maxReason = 200

// PolicyDecoder turns the bytes of heimdall.yaml into a validated policy. The YAML adapter provides it, because this
// layer may not import a YAML library. Its error text is shown to people, so it must say what is wrong.
type PolicyDecoder func(raw []byte) (policy.Policy, error)

// policyRead is the outcome of reading the policy at the merge base. Exactly one field is meaningful: pol when
// invalid is empty, and invalid (a reason a person can act on) otherwise.
type policyRead struct {
	pol     policy.Policy
	invalid string
}

// readPolicy reads heimdall.yaml at base and decodes and validates it. A missing or invalid file is a result, so
// the caller can fail closed; a transport error is returned, so the caller keeps nothing and tries again.
func readPolicy(ctx context.Context, code ports.CodeReader, decode PolicyDecoder, repo change.Repo, base id.SHA) (policyRead, error) {
	raw, err := code.ReadFile(ctx, repo, base, policyPath)
	if errors.Is(err, ports.ErrNotFound) {
		return policyRead{invalid: fmt.Sprintf("%s is missing at the merge base %s", policyPath, base.Short())}, nil
	}
	if err != nil {
		return policyRead{}, fmt.Errorf("read %s at %s: %w", policyPath, base, err)
	}
	pol, err := decode(raw)
	if err != nil {
		return policyRead{invalid: clip(err.Error())}, nil
	}
	return policyRead{pol: pol}, nil
}

// clip shortens s to at most maxReason bytes, on a rune boundary, and marks the cut.
func clip(s string) string {
	if len(s) <= maxReason {
		return s
	}
	cut := maxReason
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "..."
}
