package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// maxStaticTokenLen bounds a static token. GitHub tokens are under 300 characters.
const maxStaticTokenLen = 1024

// Tokens is the token source the Action-mode operations (ActionAPI) take. *TokenSource (the App) and *StaticToken
// (the Action's GITHUB_TOKEN) both satisfy it.
type Tokens interface {
	// Reader returns a token that can read what the operation reads.
	Reader(ctx context.Context) (string, error)
	// Writer returns a token for one write operation on one repository.
	Writer(ctx context.Context, op Op, repo change.Repo) (string, error)
}

var (
	_ Tokens = (*TokenSource)(nil)
	_ Tokens = (*StaticToken)(nil)
)

// StaticToken is one fixed token, the GITHUB_TOKEN of a workflow run. The caller reads it from config and hands it in;
// this package never reads the environment (R7). The workflow's `permissions:` block narrows what the token can do, so
// Writer cannot narrow it again: it checks the operation and the repository name, and returns the same token.
type StaticToken struct {
	token string
}

// NewStaticToken checks the token and wraps it. The error never carries the token.
func NewStaticToken(token string) (*StaticToken, error) {
	if token == "" {
		return nil, errors.New("github: static token is empty")
	}
	if len(token) > maxStaticTokenLen {
		return nil, errors.New("github: static token is too long")
	}
	if strings.IndexFunc(token, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return nil, errors.New("github: static token has a space or control character")
	}
	return &StaticToken{token: token}, nil
}

// Reader returns the token.
func (s *StaticToken) Reader(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("github: static token: %w", err)
	}
	return s.token, nil
}

// Writer returns the token for a known operation on a well-formed repository name.
func (s *StaticToken) Writer(ctx context.Context, op Op, repo change.Repo) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("github: static token: %w", err)
	}
	if _, ok := writerPermissions(op); !ok {
		return "", fmt.Errorf("github: unknown token operation %q", string(op))
	}
	if _, err := repoName(repo); err != nil {
		return "", err
	}
	return s.token, nil
}

// String hides the token from the %v and %s verbs.
func (s *StaticToken) String() string { return "github.StaticToken(redacted)" }

// GoString hides the token from the %#v verb.
func (s *StaticToken) GoString() string { return s.String() }

// ActionConfig is what NewAction needs. BaseURL is the API root (GITHUB_API_URL in a workflow, https://api.github.com
// otherwise). Author is the login the token's own comments carry, for example github-actions[bot]: the summary comment
// is found by its marker and this login (ACTION_MODE 2.5), and a comment of any other author is never edited.
type ActionConfig struct {
	BaseURL    string
	Tokens     Tokens
	HTTPClient *http.Client
	Clock      ports.Clock
	Wait       func(ctx context.Context, d time.Duration) error
	Author     string
}

// ActionAPI holds the GitHub operations of Action mode (work package AM9): comments, artifacts, code scanning alerts
// and the Git Data API. It uses the same client as Adapter, so the error mapping, the pagination, the rate-limit wait
// and the circuit breaker are the ones in doc.go. It is safe for concurrent use.
type ActionAPI struct {
	c      *client
	tokens Tokens
	author string

	// nodeIDs caches each repository's GraphQL node id for SwapRef. A node id does not change.
	nodeMu  sync.Mutex
	nodeIDs map[change.Repo]string
}

// The compile-time proof that ActionAPI satisfies each port it covers.
var (
	_ ports.PRComments            = (*ActionAPI)(nil)
	_ ports.PRCommentReader       = (*ActionAPI)(nil)
	_ ports.PRReviewCommentReader = (*ActionAPI)(nil)
	_ ports.ArtifactReader        = (*ActionAPI)(nil)
	_ ports.CodeScanReader        = (*ActionAPI)(nil)
	_ ports.GitRefs               = (*ActionAPI)(nil)
	_ ports.GitObjects            = (*ActionAPI)(nil)
)

// NewAction builds the Action operations on its own client. It fails on a missing dependency or a bad base URL.
func NewAction(cfg ActionConfig) (*ActionAPI, error) {
	if cfg.Tokens == nil || cfg.HTTPClient == nil || cfg.Clock == nil || cfg.Wait == nil {
		return nil, errors.New("github: Tokens, HTTPClient, Clock and Wait are required")
	}
	origin, err := parseOrigin(cfg.BaseURL)
	if err != nil {
		return nil, err
	}
	c := &client{
		http:    cfg.HTTPClient,
		root:    strings.TrimRight(origin.String(), "/"),
		origin:  origin,
		clock:   cfg.Clock,
		wait:    cfg.Wait,
		prCache: map[string]cachedPRs{},
	}
	return &ActionAPI{c: c, tokens: cfg.Tokens, author: cfg.Author}, nil
}

// Adapter returns the VCS ports (ports.PRReader, ports.ReviewReader, ports.CheckWriter, ports.CodeReader and the rest
// of Adapter) on this ActionAPI's client and tokens, so the Action gate reads the pull request and posts check runs
// with the workflow's static token. The two share one client: one rate-limit wait, one circuit breaker, one cache.
// The base branch of every repository is its default branch.
func (x *ActionAPI) Adapter() *Adapter {
	return &Adapter{c: x.c, tokens: x.tokens, baseBranches: map[change.Repo]string{}}
}

// Action returns the Action operations on this adapter's client and App tokens. author is the login of the App's
// bot, for example heimdall[bot].
func (a *Adapter) Action(author string) *ActionAPI {
	return &ActionAPI{c: a.c, tokens: a.tokens, author: author}
}
