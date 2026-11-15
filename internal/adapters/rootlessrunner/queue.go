package rootlessrunner

import (
	"container/heap"
	"context"
	"errors"
	"sync"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// ErrSuperseded reports a job cancelled because its change got a new head.
var ErrSuperseded = errors.New("job superseded by a newer head")

// jobKey says what a waiting or running job is for. It is what Supersede matches on.
type jobKey struct {
	Priority ports.Priority
	Repo     change.Repo
	Change   id.ChangeID
	Head     id.SHA
}

// jobQueue admits one job at a time, in priority order and first come first served inside a priority. A caller waits
// for its turn for as long as its own context allows: a busy runner never refuses and never changes an evidence
// tier (TEST_AGENT_ARCH 8, rules 1 and 2). A new head cancels the background jobs of the old one (rule 3).
type jobQueue struct {
	mu      sync.Mutex
	seq     uint64
	waiting ticketHeap
	running *ticket
}

type ticket struct {
	key      jobKey
	seq      uint64
	index    int
	granted  chan struct{}
	cancel   context.CancelCauseFunc
	canceled error // set under the queue lock when a waiting ticket is superseded
}

// Acquire waits for the turn of a job and returns a context that ends when the job is superseded, and the function
// that gives the turn back. It returns the context's error when the caller's context ends first, and ErrSuperseded
// when the job was superseded while it waited.
func (q *jobQueue) Acquire(ctx context.Context, key jobKey) (context.Context, func(), error) {
	jobCtx, cancel := context.WithCancelCause(ctx)
	t := &ticket{key: key, granted: make(chan struct{}), cancel: cancel}
	q.mu.Lock()
	q.seq++
	t.seq = q.seq
	if q.running == nil && q.waiting.Len() == 0 {
		q.running = t
		q.mu.Unlock()
		return jobCtx, q.releaser(t), nil
	}
	heap.Push(&q.waiting, t)
	q.mu.Unlock()
	select {
	case <-t.granted:
		return jobCtx, q.releaser(t), nil
	case <-jobCtx.Done():
		return q.abandon(t, cancel, context.Cause(jobCtx))
	}
}

// abandon removes a ticket that stopped waiting. If the turn was granted in the same instant it is handed on.
func (q *jobQueue) abandon(t *ticket, cancel context.CancelCauseFunc, cause error) (context.Context, func(), error) {
	q.mu.Lock()
	if t.canceled != nil {
		cause = t.canceled
	}
	if t.index >= 0 && q.waiting.contains(t) {
		heap.Remove(&q.waiting, t.index)
		q.mu.Unlock()
		cancel(cause)
		return nil, nil, cause
	}
	q.mu.Unlock()
	select {
	case <-t.granted:
		q.release(t)
	default:
	}
	cancel(cause)
	return nil, nil, cause
}

func (q *jobQueue) releaser(t *ticket) func() {
	var once sync.Once
	return func() { once.Do(func() { q.release(t) }) }
}

// release ends the running job and admits the best waiting one.
func (q *jobQueue) release(t *ticket) {
	t.cancel(nil)
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.running != t {
		return
	}
	q.running = nil
	if q.waiting.Len() == 0 {
		return
	}
	next, _ := heap.Pop(&q.waiting).(*ticket)
	q.running = next
	close(next.granted)
}

// Supersede cancels every background job of the change that served a head other than head: the waiting ones leave the
// queue, the running one has its context cancelled. It returns how many it cancelled. Gate and on-demand jobs, jobs
// of other changes and jobs of the new head are left alone.
func (q *jobQueue) Supersede(repo change.Repo, ch id.ChangeID, head id.SHA) int {
	if ch == "" {
		return 0
	}
	stale := func(k jobKey) bool {
		return k.Priority >= ports.PriorityBackground && k.Repo == repo && k.Change == ch && k.Head != head
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	n := 0
	for _, t := range append([]*ticket(nil), q.waiting...) {
		if stale(t.key) {
			t.canceled = ErrSuperseded
			heap.Remove(&q.waiting, t.index)
			t.cancel(ErrSuperseded)
			n++
		}
	}
	if q.running != nil && stale(q.running.key) {
		q.running.cancel(ErrSuperseded)
		n++
	}
	return n
}

// ticketHeap orders waiting tickets by priority, then arrival.
type ticketHeap []*ticket

func (h ticketHeap) Len() int { return len(h) }
func (h ticketHeap) Less(i, j int) bool {
	if h[i].key.Priority != h[j].key.Priority {
		return h[i].key.Priority < h[j].key.Priority
	}
	return h[i].seq < h[j].seq
}
func (h ticketHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].index, h[j].index = i, j
}
func (h *ticketHeap) Push(x any) {
	t, _ := x.(*ticket)
	t.index = len(*h)
	*h = append(*h, t)
}
func (h *ticketHeap) Pop() any {
	old := *h
	n := len(old)
	t := old[n-1]
	old[n-1] = nil
	t.index = -1
	*h = old[:n-1]
	return t
}
func (h ticketHeap) contains(t *ticket) bool {
	return t.index >= 0 && t.index < len(h) && h[t.index] == t
}
