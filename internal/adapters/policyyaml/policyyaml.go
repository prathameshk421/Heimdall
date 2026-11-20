// Package policyyaml decodes heimdall.yaml into the domain policy (TECH_SPEC 16). It lives in an adapter because the
// application layer may not import a YAML library; the intake use case gets Decode injected.
//
// Versions: version 2 is the seats shape and decodes as it is. Version 1 converts to version 2 in one function,
// fromV1, before the strict decode, so a policy at an old merge base stays readable and nothing guesses. Any other
// version is refused. Every refusal wraps policy.ErrInvalid.
package policyyaml

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"

	"gopkg.in/yaml.v3"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
)

// File is the name the policy has at the root of a repository.
const File = "heimdall.yaml"

// Decode reads the YAML strictly (an unknown key is an error, so a typo cannot pass silently), converts version 1 to
// the current version, and validates the result. An empty file is its own message, because yaml reports it as a bare
// EOF.
func Decode(raw []byte) (policy.Policy, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return policy.Policy{}, invalid("%s does not decode: %w", File, err)
	}
	if len(doc.Content) == 0 {
		return policy.Policy{}, invalid("%s is empty", File)
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return policy.Policy{}, invalid("%s does not decode: the file is not a mapping of keys", File)
	}
	version, err := versionOf(root)
	if err != nil {
		return policy.Policy{}, invalid("%s: %w", File, err)
	}
	switch version {
	case policy.CurrentVersion:
	case 1:
		if err := fromV1(root); err != nil {
			return policy.Policy{}, invalid("%s: version 1 does not convert: %w", File, err)
		}
	default:
		return policy.Policy{}, invalid("%s: version %d is not known; this server reads versions 1 and %d", File, version, policy.CurrentVersion)
	}
	p, err := decodeStrict(root)
	if err != nil {
		return policy.Policy{}, invalid("%s does not decode: %w", File, err)
	}
	if err := p.Validate(); err != nil {
		return policy.Policy{}, invalid("%s is invalid: %w", File, err)
	}
	return p, nil
}

// invalid builds an error that wraps policy.ErrInvalid and keeps its cause for errors.Is.
func invalid(format string, args ...any) error {
	return errors.Join(policy.ErrInvalid, fmt.Errorf(format, args...))
}

// decodeStrict decodes the mapping into the policy and refuses any key the policy has no field for. yaml.Node.Decode
// has no strict mode, so the node is written out and read back through a strict decoder.
func decodeStrict(root *yaml.Node) (policy.Policy, error) {
	raw, err := yaml.Marshal(root)
	if err != nil {
		return policy.Policy{}, fmt.Errorf("encode the converted policy: %w", err)
	}
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	var p policy.Policy
	if err := dec.Decode(&p); err != nil {
		return policy.Policy{}, err
	}
	return p, nil
}

// versionOf reads the top-level version key.
func versionOf(root *yaml.Node) (int, error) {
	v := valueOf(root, "version")
	if v == nil {
		return 0, errors.New("version is required (2 is the current version)")
	}
	n, err := strconv.Atoi(v.Value)
	if v.Kind != yaml.ScalarNode || err != nil {
		return 0, fmt.Errorf("version must be a whole number, got %q", v.Value)
	}
	return n, nil
}

// fromV1 converts a version 1 policy to version 2 in place. Version 1 named a person's reviewer agent
// people.<p>.agent; version 2 calls it people.<p>.seat and may declare seats. Files written between the rename (M0
// step 7) and the version bump say version 1 with seat; that is the same meaning and converts as it is. A person with
// both agent and seat is refused unless they agree, because only one can be meant.
func fromV1(root *yaml.Node) error {
	if people := valueOf(root, "people"); people != nil && people.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(people.Content); i += 2 {
			if err := personFromV1(people.Content[i].Value, people.Content[i+1]); err != nil {
				return err
			}
		}
	}
	valueOf(root, "version").Value = strconv.Itoa(policy.CurrentVersion)
	return nil
}

// personFromV1 renames one person's agent key to seat.
func personFromV1(name string, person *yaml.Node) error {
	if person.Kind != yaml.MappingNode {
		return nil // the strict decode names the problem
	}
	agentAt := -1
	for i := 0; i+1 < len(person.Content); i += 2 {
		if person.Content[i].Value == "agent" {
			agentAt = i
		}
	}
	if agentAt < 0 {
		return nil
	}
	agent := person.Content[agentAt+1].Value
	if seat := valueOf(person, "seat"); seat != nil {
		if seat.Value != agent {
			return fmt.Errorf("people.%s sets agent %q and seat %q; version 1 means one reviewer seat", name, agent, seat.Value)
		}
		person.Content = append(person.Content[:agentAt], person.Content[agentAt+2:]...)
		return nil
	}
	person.Content[agentAt].Value = "seat"
	return nil
}

// valueOf returns the value node of key in a mapping node, or nil.
func valueOf(m *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}
