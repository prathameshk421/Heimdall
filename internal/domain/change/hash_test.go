package change_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
)

// goldenCanonical is the canonical form of goldenEvent, written by hand from the rules in hash.go: keys sorted,
// no whitespace, UTC timestamp, null for an absent causation or previous hash, hash left out.
const goldenCanonical = `{"actor":{"id":"engine","kind":"system"},"blob_refs":[],"causation_id":null,"change_id":"PAY-142","correlation_id":"trace-1","id":"3f1c9a52-8d2b-4e7a-9c11-0b6f5d4e3a21","occurred_at":"2026-11-04T09:30:00Z","payload":{"reason":"ticket closed"},"prev_hash":null,"type":"change.rejected","version":1}`

// goldenHash is sha256(32 zero bytes || goldenCanonical), computed once outside this code and checked below by
// re-deriving it with crypto/sha256 on the hand-written bytes.
const goldenHash = "4e372e1276cf0b72dd2414826be1320ed944bfd312a5e03697dd0c746982f946"

func goldenEvent(t *testing.T) change.Event {
	t.Helper()
	return envelopeOf(change.EventChangeRejected)
}

func withPayload(e change.Event, raw string) change.Event {
	e.Payload = json.RawMessage(raw)
	return e
}

func TestCanonicalFormIsTheHandWrittenGoldenBytes(t *testing.T) {
	e := goldenEvent(t)
	e.Payload = json.RawMessage(`{"reason":"ticket closed"}`)
	got, err := change.Canonical(e)
	require.NoError(t, err)
	want := []byte(goldenCanonical)
	require.Equal(t, want, got)
}

func TestChainKnownAnswerVectorIsRederivedWithCryptoSHA256(t *testing.T) {
	e := goldenEvent(t)
	e.Payload = json.RawMessage(`{"reason":"ticket closed"}`)
	// Independent derivation: genesis prev_hash is 32 zero bytes, hash = sha256(prev || canonical).
	sum := sha256.Sum256(append(make([]byte, 32), []byte(goldenCanonical)...))
	require.Equal(t, goldenHash, sumHex(sum[:]), "the vector itself must match the independent derivation")
	got, err := change.Chain(change.Hash{}, e)
	require.NoError(t, err)
	require.Equal(t, goldenHash, got.String())
}

func sumHex(b []byte) string {
	const digits = "0123456789abcdef"
	out := make([]byte, 0, len(b)*2)
	for _, c := range b {
		out = append(out, digits[c>>4], digits[c&0x0f])
	}
	return string(out)
}

func TestCanonicalFormIgnoresHashWhitespaceKeyOrderAndEscapes(t *testing.T) {
	base := goldenEvent(t)
	base.Payload = json.RawMessage(`{"reason":"ticket closed"}`)
	want, err := change.Canonical(base)
	require.NoError(t, err)

	variants := map[string]change.Event{
		"hash field set":    func() change.Event { e := base; e.Hash = change.Hash{1, 2, 3}; return e }(),
		"spaces in payload": withPayload(base, "{ \"reason\" :\n \"ticket closed\" }"),
		"escaped letter":    withPayload(base, `{"reason":"ticket closed"}`),
		"zone is not UTC": func() change.Event {
			e := base
			e.OccurredAt = e.OccurredAt.In(time.FixedZone("ist", 5*3600+1800))
			return e
		}(),
		"empty and nil blobs": func() change.Event { e := base; e.BlobRefs = nil; return e }(),
	}
	for name, e := range variants {
		got, err := change.Canonical(e)
		require.NoError(t, err, name)
		require.Equal(t, string(want), string(got), "%s must not change the canonical form", name)
	}
}

func TestBlobReferencesAreHashedInTheCanonicalForm(t *testing.T) {
	base := goldenEvent(t)
	base.Payload = json.RawMessage(`{"reason":"ticket closed"}`)
	without, err := change.Canonical(base)
	require.NoError(t, err)
	withRef := base
	withRef.BlobRefs = []workflow.BlobRef{workflow.BlobRef("blob:sha256:" + strings.Repeat("a", 64))}
	with, err := change.Canonical(withRef)
	require.NoError(t, err)
	require.NotEqual(t, string(without), string(with), "a blob reference changes what is hashed")
	require.Contains(t, string(with), `"blob_refs":["blob:sha256:aaaa`)
}

func TestCanonicalFormKeepsHTMLCharactersUnescaped(t *testing.T) {
	e := goldenEvent(t)
	e.Payload = json.RawMessage(`{"reason":"a<b>&c"}`)
	got, err := change.Canonical(e)
	require.NoError(t, err)
	require.True(t, bytes.Contains(got, []byte(`"a<b>&c"`)), string(got))
}

func TestCanonicalRefusesPayloadsThatCannotBeHashedFaithfully(t *testing.T) {
	for name, raw := range map[string]string{
		"duplicate key":     `{"reason":"a","reason":"b"}`,
		"fraction":          `{"reason":"a","n":1.5}`,
		"exponent":          `{"reason":"a","n":1e3}`,
		"leading zero":      `{"reason":"a","n":01}`,
		"invalid utf-8":     "{\"reason\":\"a\xff\"}",
		"not an object":     `["a"]`,
		"key with a hyphen": `{"re-ason":"a"}`,
	} {
		e := goldenEvent(t)
		e.Payload = json.RawMessage(raw)
		_, err := change.Canonical(e)
		require.Error(t, err, name)
	}
}

func TestCanonicalIsDeterministicAcrossCalls(t *testing.T) {
	e := goldenEvent(t)
	e.Payload = json.RawMessage(`{"z":1,"a":2,"m":[3,2,1]}`)
	first, err := change.Canonical(e)
	require.NoError(t, err)
	for i := 0; i < 50; i++ {
		again, err := change.Canonical(e)
		require.NoError(t, err)
		require.Equal(t, first, again)
	}
}

func TestPreviousHashAppearsInTheCanonicalFormAsHexOrNull(t *testing.T) {
	e := goldenEvent(t)
	e.Payload = json.RawMessage(`{"reason":"ticket closed"}`)
	prev := change.Hash{0xab}
	e.PrevHash = &prev
	got, err := change.Canonical(e)
	require.NoError(t, err)
	require.Contains(t, string(got), `"prev_hash":"ab00`)
	require.NotContains(t, string(got), `"prev_hash":null`)
}

// chainOf builds n valid events of one change, each chained to the one before.
func chainOf(t *testing.T, n int) []change.Event {
	t.Helper()
	var out []change.Event
	var prev change.Hash
	for i := 1; i <= n; i++ {
		e := envelopeOf(change.EventChangeRejected)
		e.Version = int64(i)
		e.Payload = json.RawMessage(`{"reason":"step ` + strings.Repeat("x", i) + `"}`)
		if i > 1 {
			p := prev
			e.PrevHash = &p
		}
		h, err := change.Chain(prev, e)
		require.NoError(t, err)
		e.Hash = h
		out = append(out, e)
		prev = h
	}
	return out
}

func TestVerifyChainAcceptsAnHonestChain(t *testing.T) {
	require.NoError(t, change.VerifyChain(chainOf(t, 4)))
}

func TestVerifyChainAcceptsNoEventsAsNothingToCheck(t *testing.T) {
	require.NoError(t, change.VerifyChain(nil))
}

func TestGenesisMustHaveNoPreviousHash(t *testing.T) {
	evs := chainOf(t, 2)
	p := change.Hash{1}
	evs[0].PrevHash = &p
	err := change.VerifyChain(evs)
	require.ErrorIs(t, err, change.ErrChainBroken)
	require.Contains(t, err.Error(), "version 1")
	require.Contains(t, err.Error(), "genesis", "the genesis rule itself must fire, not only the hash check")
}

func TestPreviousHashMustBeTheHashBeforeIt(t *testing.T) {
	evs := chainOf(t, 3)
	p := change.Hash{9}
	evs[2].PrevHash = &p
	err := change.VerifyChain(evs)
	require.ErrorIs(t, err, change.ErrChainBroken)
	require.Contains(t, err.Error(), "previous hash", "the link check must name the link, not only fail the hash")
}

func TestVerifyChainRefusesAChainThatDoesNotStartAtVersionOne(t *testing.T) {
	evs := chainOf(t, 3)
	err := change.VerifyChain(evs[1:])
	require.ErrorIs(t, err, change.ErrChainBroken)
	require.Contains(t, err.Error(), "version 1")
}

// Each tamper must be caught, and the error must name the first broken version.
func TestEveryTamperIsDetectedAndNamesTheFirstBrokenVersion(t *testing.T) {
	cases := map[string]struct {
		mutate func([]change.Event) []change.Event
		broken string
	}{
		"payload changed": {func(e []change.Event) []change.Event {
			e[1].Payload = json.RawMessage(`{"reason":"forged"}`)
			return e
		}, "version 2"},
		"correlation changed": {func(e []change.Event) []change.Event {
			e[2].CorrelationID = "other"
			return e
		}, "version 3"},
		"actor changed": {func(e []change.Event) []change.Event {
			e[1].Actor.ID = "mallory"
			return e
		}, "version 2"},
		"time changed": {func(e []change.Event) []change.Event {
			e[1].OccurredAt = e[1].OccurredAt.Add(time.Second)
			return e
		}, "version 2"},
		"type changed": {func(e []change.Event) []change.Event {
			e[0].Type = change.EventChangeMerged
			return e
		}, "version 1"},
		"prev_hash rewritten": {func(e []change.Event) []change.Event {
			p := change.Hash{9}
			e[2].PrevHash = &p
			return e
		}, "version 3"},
		"hash rewritten": {func(e []change.Event) []change.Event {
			e[1].Hash = change.Hash{7}
			return e
		}, "version 2"},
		"two events reordered": {func(e []change.Event) []change.Event {
			e[1], e[2] = e[2], e[1]
			return e
		}, "version 2"},
		"middle event dropped": {func(e []change.Event) []change.Event {
			return append(e[:1:1], e[2:]...)
		}, "version 2"},
		"version renumbered": {func(e []change.Event) []change.Event {
			e[2].Version = 9
			return e
		}, "version 3"},
		"events of two changes mixed": {func(e []change.Event) []change.Event {
			e[1].ChangeID = "PAY-143"
			return e
		}, "version 2"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			evs := c.mutate(chainOf(t, 3))
			err := change.VerifyChain(evs)
			require.ErrorIs(t, err, change.ErrChainBroken)
			require.Contains(t, err.Error(), c.broken)
		})
	}
}

// The chain cannot see a truncated tail: a shorter prefix is itself a valid chain. The published head hash (TECH_SPEC
// 4.2) is what catches that, so the test pins the head that a verifier must compare against.
func TestTruncatedTailIsAValidPrefixAndOnlyTheHeadHashExposesIt(t *testing.T) {
	evs := chainOf(t, 3)
	require.NoError(t, change.VerifyChain(evs[:2]))
	require.NotEqual(t, evs[2].Hash, evs[1].Hash, "the head hash differs, so a comparison with the published head catches the truncation")
}

func TestVerifyChainRefusesAPayloadThatCannotBeCanonicalised(t *testing.T) {
	evs := chainOf(t, 1)
	evs[0].Payload = json.RawMessage(`{"reason":"a","reason":"b"}`)
	err := change.VerifyChain(evs)
	require.ErrorIs(t, err, change.ErrChainBroken)
}

func TestHashTextIsLowerHexAndRoundTrips(t *testing.T) {
	h := change.Hash{0xde, 0xad}
	b, err := json.Marshal(h)
	require.NoError(t, err)
	require.Equal(t, `"dead000000000000000000000000000000000000000000000000000000000000"`, string(b))
	var back change.Hash
	require.NoError(t, json.Unmarshal(b, &back))
	require.Equal(t, h, back)
	require.Error(t, json.Unmarshal([]byte(`"DEAD"`), &back))
	require.Error(t, json.Unmarshal([]byte(`"abcd"`), &back))
	upper := `"` + strings.Repeat("A", 64) + `"`
	require.Error(t, json.Unmarshal([]byte(upper), &back), "upper case is not the canonical text")
}

func FuzzVerifyChainNeverPanics(f *testing.F) {
	f.Add([]byte(`[]`))
	f.Fuzz(func(_ *testing.T, data []byte) {
		var got []change.Event
		if err := json.Unmarshal(data, &got); err != nil {
			return
		}
		_ = change.VerifyChain(got)
	})
}
