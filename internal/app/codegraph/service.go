package codegraph

import (
	"container/list"
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

// Source gives the tarball of one commit.
type Source interface {
	Tarball(ctx context.Context, repo change.Repo, sha id.SHA) (io.ReadCloser, error)
}

type key struct {
	repo change.Repo
	sha  id.SHA
}

type entry struct {
	key key
	idx *Index
}

// build is one running build that callers of the same key share.
type build struct {
	done chan struct{}
	idx  *Index
	err  error
}

// Service builds indexes on demand and keeps the latest ones, least recently used out first.
type Service struct {
	src      Source
	capacity int

	mu       sync.Mutex
	order    *list.List // of *entry, most recent first
	cached   map[key]*list.Element
	inflight map[key]*build
}

// NewService returns a service that keeps up to capacity indexes.
func NewService(src Source, capacity int) (*Service, error) {
	if src == nil {
		return nil, errors.New("codegraph: source is nil")
	}
	if capacity < 1 {
		return nil, fmt.Errorf("codegraph: capacity %d is below 1", capacity)
	}
	return &Service{src: src, capacity: capacity, order: list.New(), cached: map[key]*list.Element{}, inflight: map[key]*build{}}, nil
}

// Index returns the index of a commit. Callers that ask for the same commit at the same time share one build. A failed
// build is not kept, so the next call tries again.
func (s *Service) Index(ctx context.Context, repo change.Repo, sha id.SHA) (*Index, error) {
	k := key{repo, sha}
	s.mu.Lock()
	if el, ok := s.cached[k]; ok {
		s.order.MoveToFront(el)
		idx := el.Value.(*entry).idx
		s.mu.Unlock()
		return idx, nil
	}
	b, running := s.inflight[k]
	if !running {
		b = &build{done: make(chan struct{})}
		s.inflight[k] = b
		go s.run(context.WithoutCancel(ctx), k, b)
	}
	s.mu.Unlock()
	select {
	case <-b.done:
		return b.idx, b.err
	case <-ctx.Done():
		return nil, fmt.Errorf("wait for code index: %w", ctx.Err())
	}
}

func (s *Service) run(ctx context.Context, k key, b *build) {
	b.idx, b.err = s.fetch(ctx, k)
	s.mu.Lock()
	delete(s.inflight, k)
	if b.err == nil {
		s.cached[k] = s.order.PushFront(&entry{key: k, idx: b.idx})
		for s.order.Len() > s.capacity {
			last := s.order.Back()
			s.order.Remove(last)
			delete(s.cached, last.Value.(*entry).key)
		}
	}
	s.mu.Unlock()
	close(b.done)
}

func (s *Service) fetch(ctx context.Context, k key) (*Index, error) {
	body, err := s.src.Tarball(ctx, k.repo, k.sha)
	if err != nil {
		return nil, fmt.Errorf("fetch tarball of %s at %s: %w", k.repo, k.sha, err)
	}
	defer func() { _ = body.Close() }()
	idx, err := Build(body)
	if err != nil {
		return nil, fmt.Errorf("index %s at %s: %w", k.repo, k.sha, err)
	}
	return idx, nil
}
