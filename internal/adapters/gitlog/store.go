package gitlog

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

const (
	// DefaultRef is the ref that holds the log (ACTION_MODE 2.3, S1 1.7).
	DefaultRef = "refs/heimdall/state"
	// MinKeyBytes is the shortest MAC key the store accepts.
	MinKeyBytes = 32
	// maxRebuilds is how often one Append rebuilds its commit on a tip that moved for another change.
	maxRebuilds = 10
	// maxSwapAttempts is how often one commit is offered to the ref while the ref stays at the tip it was built on
	// and GitHub keeps answering with an error (a transient failure, S1 1.7 point 5).
	maxSwapAttempts = 4
	// swapBackoff is the first wait between those attempts. It doubles each time: 1 s, 2 s, 4 s.
	swapBackoff = time.Second
	// maxTreeEntries bounds the recursive listing of the state tree.
	maxTreeEntries = 50_000
	redacted       = "gitlog.Store{redacted}"
)

var reRefName = regexp.MustCompile(`^refs/[A-Za-z0-9._-]+(/[A-Za-z0-9._-]+)+$`)

// Git is the part of GitHub the store uses. It holds no ref update that is a plain set: the ref moves only by
// CreateRef (refused when the ref exists) or SwapRef (a compare-and-swap). REST PATCH /git/refs is not a compare on
// refs/heimdall/* (S1 1.1), so ports.GitRefs.UpdateRef is left out on purpose. *github.ActionAPI satisfies it.
type Git interface {
	// GetRef returns the commit the ref points at. A missing ref is ports.ErrNotFound.
	GetRef(ctx context.Context, repo change.Repo, ref string) (id.SHA, error)
	// CreateRef creates the ref. GitHub refuses a ref that exists (422), which is the safe create-if-absent.
	CreateRef(ctx context.Context, repo change.Repo, ref string, sha id.SHA) error
	ports.GitObjects
	ports.GitReader
	ports.GitRefSwapper
}

// Config holds what New needs. Key is a credential: the caller reads it from its file (D89) and passes it in. This
// package never reads the environment. Config prints without it.
type Config struct {
	// Repo is the repository that holds the ref.
	Repo change.Repo
	// Git reaches GitHub. The token lives inside it.
	Git Git
	// Key is the HMAC key of the heads. At least MinKeyBytes bytes.
	Key []byte
	// Ref is the full ref name. Empty means DefaultRef.
	Ref string
	// Wait holds the store between swap attempts. It returns early with the context's error. Production passes
	// clock.Wait; tests pass a fake that records the waits.
	Wait func(ctx context.Context, d time.Duration) error
}

// String and GoString keep the key out of any print of a Config.
func (Config) String() string { return "gitlog.Config{redacted}" }

// GoString is the %#v form of String.
func (Config) GoString() string { return "gitlog.Config{redacted}" }

// Store implements ports.EventStore on one git ref.
type Store struct {
	git  Git
	repo change.Repo
	ref  string
	key  []byte
	wait func(ctx context.Context, d time.Duration) error
}

var _ ports.EventStore = (*Store)(nil)

// String and GoString keep the key out of any print of a Store.
func (*Store) String() string { return redacted }

// GoString is the %#v form of String.
func (*Store) GoString() string { return redacted }

// New checks cfg and returns a Store. Its errors never repeat the key.
func New(cfg Config) (*Store, error) {
	if len(cfg.Key) < MinKeyBytes {
		return nil, fmt.Errorf("gitlog: the MAC key must be at least %d bytes", MinKeyBytes)
	}
	if cfg.Git == nil || cfg.Wait == nil {
		return nil, errors.New("gitlog: Git and Wait are required")
	}
	if _, err := change.ParseRepo(cfg.Repo.String()); err != nil {
		return nil, fmt.Errorf("gitlog: %w", err)
	}
	ref := cfg.Ref
	if ref == "" {
		ref = DefaultRef
	}
	if !reRefName.MatchString(ref) || strings.Contains(ref, "..") {
		return nil, errors.New("gitlog: the ref must be a full name such as refs/heimdall/state")
	}
	return &Store{git: cfg.Git, repo: cfg.Repo, ref: ref, key: append([]byte(nil), cfg.Key...), wait: cfg.Wait}, nil
}

// Load returns every event of the change in version order, after it checked the head MAC, the hash chain and that the
// head matches the log. A log that fails is refused with an IntegrityError (errors.Is ErrIntegrity). A change that has
// no log yields nothing.
func (s *Store) Load(ctx context.Context, changeID id.ChangeID) ([]change.Event, error) {
	if _, err := id.ParseChangeID(string(changeID)); err != nil {
		return nil, fmt.Errorf("load: %w", err)
	}
	snap, err := s.read(ctx, changeID, false)
	if err != nil {
		return nil, fmt.Errorf("load %s: %w", changeID, err)
	}
	return snap.events, nil
}

// Append writes evs and fx as one commit and moves the ref to it with a compare-and-swap (ACTION_MODE 2.3, 2.4).
//
// It reads the log at the current tip and checks the change is at expected: if not it returns ErrVersionConflict. It
// chains evs from the stored head, so the store and not the caller writes PrevHash and Hash. If the ref moved before
// the swap, it reads again. If the change itself moved, that is ErrVersionConflict and nothing is written. If only
// other files moved, it rebuilds the same commit on the new tip, up to maxRebuilds times.
func (s *Store) Append(ctx context.Context, changeID id.ChangeID, expected int64, evs []change.Event, fx []change.Effect) (int64, error) {
	prepared, err := prepare(changeID, expected, evs, fx)
	if err != nil {
		return 0, fmt.Errorf("append to %s: %w", changeID, err)
	}
	for rebuild := range maxRebuilds {
		if err := ctx.Err(); err != nil {
			return 0, fmt.Errorf("append to %s: %w", changeID, err)
		}
		snap, err := s.read(ctx, changeID, len(fx) > 0)
		if err != nil {
			return 0, fmt.Errorf("append to %s: %w", changeID, err)
		}
		// After a refused swap the tip may have moved past our own commit: it landed, the answer was lost, and
		// another writer appended on top before the store read the tip again. The log then holds our exact batch.
		if rebuild > 0 && holdsBatch(snap, expected, prepared) {
			return expected + int64(len(prepared)), nil
		}
		if snap.version() != expected {
			return 0, fmt.Errorf("append to %s: change is at version %d, not %d: %w", changeID, snap.version(), expected, ports.ErrVersionConflict)
		}
		next, err := s.plan(changeID, snap, prepared, fx)
		if err != nil {
			return 0, fmt.Errorf("append to %s: %w", changeID, err)
		}
		err = s.publish(ctx, snap, next, fmt.Sprintf("heimdall state: %s v%d", changeID, next.version))
		if err == nil {
			return next.version, nil
		}
		if !errors.Is(err, errRefMoved) {
			return 0, fmt.Errorf("append to %s: %w", changeID, err)
		}
	}
	return 0, fmt.Errorf("append to %s: the ref moved %d times in a row: %w", changeID, maxRebuilds, ports.ErrUnavailable)
}

// holdsBatch reports whether the stored log already holds evs right after version expected, by event id.
func holdsBatch(snap *snapshot, expected int64, evs []change.Event) bool {
	if snap.version() < expected+int64(len(evs)) {
		return false
	}
	for i, e := range evs {
		if snap.events[expected+int64(i)].ID != e.ID {
			return false
		}
	}
	return true
}

// prepare checks the batch without any call and returns the events as they are stored: UTC, cut to microseconds (what
// Postgres keeps, so the hash holds after a later import, ACTION_MODE 2.8), and with no previous hash or hash yet.
func prepare(changeID id.ChangeID, expected int64, evs []change.Event, fx []change.Effect) ([]change.Event, error) {
	if _, err := id.ParseChangeID(string(changeID)); err != nil {
		return nil, err
	}
	if expected < 0 {
		return nil, fmt.Errorf("expected version %d is negative", expected)
	}
	if len(evs) == 0 {
		return nil, errors.New("no events: an append needs at least one event, and an effect is caused by one")
	}
	out := make([]change.Event, len(evs))
	for i, e := range evs {
		want := expected + int64(i) + 1
		if e.ChangeID != changeID {
			return nil, fmt.Errorf("event at version %d is for change %s", want, e.ChangeID)
		}
		if e.Version != want {
			return nil, fmt.Errorf("event %d has version %d, want %d", i+1, e.Version, want)
		}
		if len(e.Payload) > MaxEventBytes {
			return nil, &LimitError{What: fmt.Sprintf("payload at version %d", want), Max: MaxEventBytes, Got: int64(len(e.Payload))}
		}
		e.OccurredAt = e.OccurredAt.UTC().Truncate(time.Microsecond)
		if err := e.Validate(); err != nil {
			return nil, fmt.Errorf("event at version %d: %w", want, err)
		}
		e.PrevHash, e.Hash = nil, change.Hash{}
		out[i] = e
	}
	for _, f := range fx {
		if err := f.Validate(); err != nil {
			return nil, fmt.Errorf("effect %s: %w", f.Key, err)
		}
	}
	return out, nil
}
