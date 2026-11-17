package reconcile

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// inputTTL is how long an input read from outside is reused.
const inputTTL = 60 * time.Second

// inputCache keeps what InputSource answered for inputTTL, by what was asked, so a sweep over many changes does not
// read the same policy again for each. Only successful answers are kept.
type inputCache struct {
	clock ports.Clock
	mu    sync.Mutex
	items map[string]cached
}

type cached struct {
	at    time.Time
	value any
}

func newInputCache(c ports.Clock) *inputCache {
	return &inputCache{clock: c, items: map[string]cached{}}
}

func (c *inputCache) get(key string) (any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	it, ok := c.items[key]
	if !ok || c.clock.Now().Sub(it.at) >= inputTTL {
		return nil, false
	}
	return it.value, true
}

func (c *inputCache) put(key string, v any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items[key] = cached{at: c.clock.Now(), value: v}
}

func (c *inputCache) policy(ctx context.Context, src InputSource, repo change.Repo, sha id.SHA) (policy.Policy, error) {
	key := "policy/" + string(repo) + "/" + string(sha)
	if v, ok := c.get(key); ok {
		return v.(policy.Policy), nil
	}
	p, err := src.Policy(ctx, repo, sha)
	if err != nil {
		return policy.Policy{}, err
	}
	c.put(key, p)
	return p, nil
}

func (c *inputCache) profile(ctx context.Context, src InputSource, repo change.Repo) (change.Profile, error) {
	key := "profile/" + string(repo)
	if v, ok := c.get(key); ok {
		return v.(change.Profile), nil
	}
	p, err := src.Profile(ctx, repo)
	if err != nil {
		return change.Profile{}, err
	}
	c.put(key, p)
	return p, nil
}

// outside is cached by change and head: the changed files and the like do not change while the head stays.
func (c *inputCache) outside(ctx context.Context, src InputSource, changeID id.ChangeID, v change.View) (change.Outside, error) {
	key := "outside/" + string(changeID) + "/" + string(v.PR.HeadSHA) + "/" + strconv.Itoa(len(v.Approvals))
	if x, ok := c.get(key); ok {
		return x.(change.Outside), nil
	}
	o, err := src.Outside(ctx, v)
	if err != nil {
		return change.Outside{}, err
	}
	c.put(key, o)
	return o, nil
}
