package gitlog

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

const (
	// DefaultRef is the ref that holds the log (ACTION_MODE 2.3).
	DefaultRef = "refs/heimdall/state"
	// DefaultBaseURL is the GitHub REST API root.
	DefaultBaseURL = "https://api.github.com"
	// MinKeyBytes is the shortest MAC key the store accepts.
	MinKeyBytes = 32
	// maxRebuilds is how often one Append rebuilds its commit on a tip that moved for another change.
	maxRebuilds = 10
	redacted    = "gitlog.Store{redacted}"
)

var (
	reRefName = regexp.MustCompile(`^refs/[A-Za-z0-9._-]+(/[A-Za-z0-9._-]+)+$`)
	reRepo    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*/[A-Za-z0-9._-]+$`)
)

// Config holds what New needs. Key and Token are credentials: the caller reads them from their file (D89) and passes
// them in. This package never reads the environment. Config prints without them.
type Config struct {
	// Repo is the repository that holds the ref.
	Repo change.Repo
	// Token is a GitHub token that may write refs and git objects of Repo.
	Token string
	// Key is the HMAC key of the heads. At least MinKeyBytes bytes.
	Key []byte
	// Ref is the full ref name. Empty means DefaultRef.
	Ref string
	// BaseURL is the API root. Empty means DefaultBaseURL.
	BaseURL string
	// HTTPClient is used for every call. Nil means a client with a 60 second timeout.
	HTTPClient *http.Client
}

// String and GoString keep the credentials out of any print of a Config.
func (Config) String() string { return "gitlog.Config{redacted}" }

// GoString is the %#v form of String.
func (Config) GoString() string { return "gitlog.Config{redacted}" }

// Store implements ports.EventStore on one git ref.
type Store struct {
	api *api
	key []byte
}

var _ ports.EventStore = (*Store)(nil)

// String and GoString keep the key and the token out of any print of a Store.
func (*Store) String() string { return redacted }

// GoString is the %#v form of String.
func (*Store) GoString() string { return redacted }

// New checks cfg and returns a Store. Its errors never repeat the key or the token.
func New(cfg Config) (*Store, error) {
	if len(cfg.Key) < MinKeyBytes {
		return nil, fmt.Errorf("gitlog: the MAC key must be at least %d bytes", MinKeyBytes)
	}
	if cfg.Token == "" {
		return nil, errors.New("gitlog: a GitHub token is required")
	}
	if !reRepo.MatchString(string(cfg.Repo)) {
		return nil, errors.New("gitlog: the repository must be owner/name")
	}
	ref := orDefault(cfg.Ref, DefaultRef)
	if !reRefName.MatchString(ref) || strings.Contains(ref, "..") {
		return nil, errors.New("gitlog: the ref must be a full name such as refs/heimdall/state")
	}
	base := strings.TrimRight(orDefault(cfg.BaseURL, DefaultBaseURL), "/")
	if u, err := url.Parse(base); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return nil, errors.New("gitlog: the base URL must be an http or https URL")
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 2 * requestTimeout}
	}
	return &Store{
		api: &api{http: client, base: base, repo: string(cfg.Repo), token: cfg.Token, ref: ref},
		key: append([]byte(nil), cfg.Key...),
	}, nil
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
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

// Append writes evs and fx as one commit and moves the ref to it with compare-and-swap (ACTION_MODE 2.3, 2.4).
//
// It reads the log at the current tip and checks the change is at expected: if not it returns ErrVersionConflict. It
// chains evs from the stored head, so the store and not the caller writes PrevHash and Hash. If GitHub refuses the ref
// update because the ref moved, it reads again. If the change itself moved, that is ErrVersionConflict and nothing is
// written. If only other files moved, it rebuilds the same commit on the new tip, up to maxRebuilds times.
func (s *Store) Append(ctx context.Context, changeID id.ChangeID, expected int64, evs []change.Event, fx []change.Effect) (int64, error) {
	prepared, err := prepare(changeID, expected, evs, fx)
	if err != nil {
		return 0, fmt.Errorf("append to %s: %w", changeID, err)
	}
	for range maxRebuilds {
		if err := ctx.Err(); err != nil {
			return 0, fmt.Errorf("append to %s: %w", changeID, err)
		}
		snap, err := s.read(ctx, changeID, len(fx) > 0)
		if err != nil {
			return 0, fmt.Errorf("append to %s: %w", changeID, err)
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
