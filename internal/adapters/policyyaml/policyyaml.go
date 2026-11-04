// Package policyyaml decodes heimdall.yaml into the domain policy (TECH_SPEC 16). It lives in an adapter because the
// application layer may not import a YAML library; the intake use case gets Decode injected.
package policyyaml

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"gopkg.in/yaml.v3"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
)

// File is the name the policy has at the root of a repository.
const File = "heimdall.yaml"

// Decode reads the YAML strictly (an unknown key is an error, so a typo cannot pass silently) and validates the
// result. An empty file is its own message, because yaml reports it as a bare EOF.
func Decode(raw []byte) (policy.Policy, error) {
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	var p policy.Policy
	if err := dec.Decode(&p); err != nil {
		if errors.Is(err, io.EOF) {
			return policy.Policy{}, fmt.Errorf("%s is empty", File)
		}
		return policy.Policy{}, fmt.Errorf("%s does not decode: %w", File, err)
	}
	if err := p.Validate(); err != nil {
		return policy.Policy{}, fmt.Errorf("%s is invalid: %w", File, err)
	}
	return p, nil
}
