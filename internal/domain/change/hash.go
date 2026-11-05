package change

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"
)

// ErrChainBroken is returned by VerifyChain. The error names the first version that does not verify.
var ErrChainBroken = errors.New("hash chain broken")

var reHash = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Hash is a SHA-256 digest. It is written in lowercase hexadecimal in JSON.
type Hash [sha256.Size]byte

// String returns the digest as 64 lowercase hex characters.
func (h Hash) String() string { return hex.EncodeToString(h[:]) }

// MarshalText writes the digest as lowercase hex.
func (h Hash) MarshalText() ([]byte, error) { return []byte(h.String()), nil }

// UnmarshalText reads 64 lowercase hex characters and refuses anything else.
func (h *Hash) UnmarshalText(b []byte) error {
	if !reHash.MatchString(string(b)) {
		return fmt.Errorf("hash %q is not 64 lowercase hex characters", b)
	}
	_, err := hex.Decode(h[:], b)
	return err
}

// canonicalActor, canonicalEvent: the canonical form of an event (TECH_SPEC 4.2). The field order is the alphabetical
// order of the JSON names, which is the order encoding/json writes struct fields in, so the layout is fixed by
// declaration and never by a map. The hash field is left out; prev_hash is included.
type canonicalActor struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
}

type canonicalEvent struct {
	Actor         canonicalActor  `json:"actor"`
	BlobRefs      []string        `json:"blob_refs"`
	CausationID   *string         `json:"causation_id"`
	ChangeID      string          `json:"change_id"`
	CorrelationID string          `json:"correlation_id"`
	ID            string          `json:"id"`
	OccurredAt    string          `json:"occurred_at"`
	Payload       json.RawMessage `json:"payload"`
	PrevHash      *string         `json:"prev_hash"`
	Type          string          `json:"type"`
	Version       int64           `json:"version"`
}

// Canonical returns the bytes that are hashed for e (TECH_SPEC 4.2), without the hash field. The rules, which
// VerifyChain relies on and the golden test pins:
//   - objects have their keys in sorted order, with no whitespace and no HTML escaping;
//   - the payload is re-encoded from its decoded form, so spacing, key order and escapes in the stored text do not
//     matter, but a duplicate key, a fraction, an exponent or invalid UTF-8 is refused (checkStrictJSON);
//   - occurred_at is written in UTC as RFC 3339 with the shortest fraction;
//   - a missing causation id or previous hash is null; a missing blob list is an empty array.
//
// This is RFC 8785 for the value types the events use (ASCII keys, integers, strings). Where it differs, the
// difference is that U+2028 and U+2029 are escaped, which is still deterministic.
func Canonical(e Event) ([]byte, error) {
	payload, err := canonicalPayload(e.Payload)
	if err != nil {
		return nil, fmt.Errorf("canonical payload: %w", err)
	}
	ce := canonicalEvent{
		Actor:         canonicalActor{ID: string(e.Actor.ID), Kind: string(e.Actor.Kind)},
		BlobRefs:      make([]string, 0, len(e.BlobRefs)),
		ChangeID:      string(e.ChangeID),
		CorrelationID: string(e.CorrelationID),
		ID:            string(e.ID),
		OccurredAt:    e.OccurredAt.UTC().Format(time.RFC3339Nano),
		Payload:       payload,
		Type:          string(e.Type),
		Version:       e.Version,
	}
	for _, r := range e.BlobRefs {
		ce.BlobRefs = append(ce.BlobRefs, string(r))
	}
	if e.CausationID != nil {
		s := string(*e.CausationID)
		ce.CausationID = &s
	}
	if e.PrevHash != nil {
		s := e.PrevHash.String()
		ce.PrevHash = &s
	}
	return encodeNoEscape(ce)
}

// canonicalPayload checks raw strictly and returns it re-encoded with sorted keys and integers as written.
func canonicalPayload(raw json.RawMessage) ([]byte, error) {
	if err := checkStrictJSON(raw); err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return encodeNoEscape(v)
}

// encodeNoEscape encodes v as compact JSON without HTML escaping and without the trailing newline.
func encodeNoEscape(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

// Chain returns the hash of e after prev: sha256(prev || Canonical(e)). The genesis event is chained after the zero
// Hash, and its PrevHash is null. An error means e has a payload that cannot be hashed faithfully.
func Chain(prev Hash, e Event) (Hash, error) {
	canon, err := Canonical(e)
	if err != nil {
		return Hash{}, err
	}
	h := sha256.New()
	h.Write(prev[:])
	h.Write(canon)
	var out Hash
	copy(out[:], h.Sum(nil))
	return out, nil
}

// VerifyChain checks one change's events in order. The first event is version 1 with no previous hash; each later
// event has version n+1 for position n, the same change id, a previous hash equal to the hash before it, and a hash
// equal to Chain. An empty list has nothing to check. A truncated tail verifies as a shorter chain, so the head hash
// must also be compared with the published one (TECH_SPEC 4.2).
func VerifyChain(events []Event) error {
	var prev Hash
	for i, e := range events {
		want := int64(i + 1)
		if err := checkLink(events, i, e, want, prev); err != nil {
			return fmt.Errorf("%w: version %d: %w", ErrChainBroken, want, err)
		}
		h, err := Chain(prev, e)
		if err != nil {
			return fmt.Errorf("%w: version %d: canonical form: %w", ErrChainBroken, want, err)
		}
		if e.Hash != h {
			return fmt.Errorf("%w: version %d: hash %s does not match the chain", ErrChainBroken, want, e.Hash)
		}
		prev = e.Hash
	}
	return nil
}

// checkLink checks the position, the change id and the previous-hash link of e at index i.
func checkLink(events []Event, i int, e Event, want int64, prev Hash) error {
	if e.Version != want {
		return fmt.Errorf("found version %d at position %d", e.Version, i+1)
	}
	if e.ChangeID != events[0].ChangeID {
		return fmt.Errorf("change %s differs from %s", e.ChangeID, events[0].ChangeID)
	}
	if i == 0 {
		if e.PrevHash != nil {
			return errors.New("genesis event has a previous hash")
		}
		return nil
	}
	if e.PrevHash == nil {
		return errors.New("previous hash is missing")
	}
	if *e.PrevHash != prev {
		return fmt.Errorf("previous hash %s is not the hash of version %d", *e.PrevHash, want-1)
	}
	return nil
}
