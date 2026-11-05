package change

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

// EffectKind says which outside system an effect writes to (TECH_SPEC 2.4 and 4.1).
type EffectKind string

// The effect kinds the spec names: a GitHub check, comment and branch, the room (open and post), a tracker write
// (rejection tickets, comments), and a memory episode.
const (
	EffectGitHubCheck       EffectKind = "github_check"
	EffectGitHubComment     EffectKind = "github_comment"
	EffectGitHubBranch      EffectKind = "github_branch"
	EffectRoomOpen          EffectKind = "room_open"
	EffectRoomPost          EffectKind = "room_post"
	EffectTrackerWrite      EffectKind = "tracker_write"
	EffectMemoryEpisode     EffectKind = "memory_episode"
	EffectTaskAssign        EffectKind = "task_assign"
	EffectGitHubCommitTests EffectKind = "github_commit_tests"
	EffectTestQuality       EffectKind = "test_quality"
)

// Valid reports whether k is an effect kind.
func (k EffectKind) Valid() bool {
	return slices.Contains([]EffectKind{EffectGitHubCheck, EffectGitHubComment, EffectGitHubBranch,
		EffectRoomOpen, EffectRoomPost, EffectTrackerWrite, EffectMemoryEpisode, EffectTaskAssign,
		EffectGitHubCommitTests, EffectTestQuality}, k)
}

// EffectKey is the deterministic idempotency key of an outbox row (TECH_SPEC 2.4). An external system that sees the
// same key twice must treat it as one action.
type EffectKey string

// Effect is one outbox row: a side effect committed in the same transaction as the event that caused it (TECH_SPEC
// 2.4). Nothing external happens without a committed effect, and every committed effect eventually happens.
type Effect struct {
	Key     EffectKey       `json:"key"`
	Kind    EffectKind      `json:"kind"`
	Target  string          `json:"target"`
	Payload json.RawMessage `json:"payload"`
}

// IdempotencyKey builds the key of an effect as kind:change:version:discriminator. The change id, the version and the
// kind hold no colon, so the first three fields end where the first colon is, and the discriminator (which may hold
// colons) takes the rest. Two different inputs therefore cannot produce the same key. The discriminator names what
// the effect is about, such as "<repo>:<sha>:<gate version>" for a check run (TECH_SPEC 2.4).
func IdempotencyKey(changeID id.ChangeID, version int64, kind EffectKind, discriminator string) (EffectKey, error) {
	if _, err := id.ParseChangeID(string(changeID)); err != nil {
		return "", fmt.Errorf("idempotency key: %w", err)
	}
	if version < 1 {
		return "", fmt.Errorf("idempotency key: version %d must be 1 or more", version)
	}
	if !kind.Valid() {
		return "", fmt.Errorf("idempotency key: kind %q is not an effect kind", kind)
	}
	if _, err := parseOpaque[string]("idempotency key discriminator", discriminator); err != nil {
		return "", err
	}
	return EffectKey(string(kind) + ":" + string(changeID) + ":" + strconv.FormatInt(version, 10) + ":" + discriminator), nil
}

// Validate returns every problem with the effect, each naming its field.
func (e Effect) Validate() error {
	var v problems
	_, err := parseOpaque[string]("key", string(e.Key))
	v.add("key", err)
	v.add("kind", checkEnum(e.Kind, e.Kind.Valid()))
	v.add("target", checkText(e.Target))
	v.add("payload", errors.Join(checkStrictJSON(e.Payload)))
	return v.join()
}
