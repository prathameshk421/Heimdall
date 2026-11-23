package rootlessrunner

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// setQueueHeaders sets on req the headers the client sends for job.
func setQueueHeaders(req *http.Request, job ports.SandboxJob) {
	key := jobKeyOf(job)
	req.Header.Set(headerPriority, strconv.Itoa(int(key.Priority)))
	req.Header.Set(headerRepo, string(key.Repo))
	req.Header.Set(headerChange, string(key.Change))
	req.Header.Set(headerHead, string(key.Head))
}

func sha(c string) id.SHA { return id.SHA(strings.Repeat(c, 40)) }

func key(p ports.Priority, ch id.ChangeID, head id.SHA) jobKey {
	return jobKey{Priority: p, Repo: "acme/pay", Change: ch, Head: head}
}

// waiter starts Acquire in a goroutine and reports when the turn is granted.
type waiter struct {
	granted chan struct{}
	err     chan error
	release func()
	ctx     context.Context
}

func enqueue(ctx context.Context, t *testing.T, q *jobQueue, k jobKey) *waiter {
	t.Helper()
	w := &waiter{granted: make(chan struct{}), err: make(chan error, 1)}
	go func() {
		ctx, release, err := q.Acquire(ctx, k)
		if err != nil {
			w.err <- err
			return
		}
		w.ctx, w.release = ctx, release
		close(w.granted)
	}()
	require.Eventually(t, func() bool {
		q.mu.Lock()
		defer q.mu.Unlock()
		for _, tk := range q.waiting {
			if tk.key == k && tk.seq == q.seq {
				return true
			}
		}
		return false
	}, time.Second, time.Millisecond)
	return w
}

func TestTheQueueAdmitsByPriorityThenArrivalNeverRefusing(t *testing.T) {
	t.Parallel()
	q := &jobQueue{}
	_, holder, err := q.Acquire(context.Background(), key(ports.PriorityBackground, "KAN-1", sha("a")))
	require.NoError(t, err)

	bg1 := enqueue(context.Background(), t, q, key(ports.PriorityBackground, "KAN-2", sha("b")))
	bg2 := enqueue(context.Background(), t, q, key(ports.PriorityBackground, "KAN-3", sha("c")))
	od := enqueue(context.Background(), t, q, key(ports.PriorityOnDemand, "KAN-4", sha("d")))
	gate := enqueue(context.Background(), t, q, key(ports.PriorityGate, "KAN-5", sha("e")))

	var order []string
	for _, step := range []struct {
		name string
		w    *waiter
	}{{"gate", gate}, {"on-demand", od}, {"background 1", bg1}, {"background 2", bg2}} {
		holder()
		select {
		case <-step.w.granted:
		case <-time.After(time.Second):
			t.Fatalf("%s was not admitted", step.name)
		}
		order = append(order, step.name)
		holder = step.w.release
	}
	holder()
	require.Equal(t, []string{"gate", "on-demand", "background 1", "background 2"}, order,
		"gate before on-demand before background, first come first served inside a priority")
}

func TestAWaiterLeavesTheQueueWhenItsDeadlinePasses(t *testing.T) {
	t.Parallel()
	q := &jobQueue{}
	_, holder, err := q.Acquire(context.Background(), key(ports.PriorityGate, "KAN-1", sha("a")))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	gone := enqueue(ctx, t, q, key(ports.PriorityGate, "KAN-2", sha("b")))
	next := enqueue(context.Background(), t, q, key(ports.PriorityBackground, "KAN-3", sha("c")))
	cancel()
	require.ErrorIs(t, <-gone.err, context.Canceled)

	holder()
	select {
	case <-next.granted:
	case <-time.After(time.Second):
		t.Fatal("the abandoned waiter held up the queue")
	}
	next.release()
}

func TestANewHeadCancelsOnlyTheBackgroundJobsOfTheOldHead(t *testing.T) {
	t.Parallel()
	q := &jobQueue{}
	oldHead, newHead := sha("1"), sha("2")
	runCtx, running, err := q.Acquire(context.Background(), key(ports.PriorityBackground, "pr-7", oldHead))
	require.NoError(t, err)

	staleBg := enqueue(context.Background(), t, q, key(ports.PriorityBackground, "pr-7", oldHead))
	gate := enqueue(context.Background(), t, q, key(ports.PriorityGate, "pr-7", oldHead))
	onDemand := enqueue(context.Background(), t, q, key(ports.PriorityOnDemand, "pr-7", oldHead))
	otherPR := enqueue(context.Background(), t, q, key(ports.PriorityBackground, "pr-8", oldHead))
	fresh := enqueue(context.Background(), t, q, key(ports.PriorityBackground, "pr-7", newHead))

	require.Equal(t, 2, q.Supersede("acme/pay", "pr-7", newHead), "the running and the queued stale background job")
	require.ErrorIs(t, <-staleBg.err, ErrSuperseded)
	require.ErrorIs(t, context.Cause(runCtx), ErrSuperseded, "the running job's context ends")

	running()
	order := []*waiter{gate, onDemand, otherPR, fresh}
	for i, w := range order {
		select {
		case <-w.granted:
		case <-time.After(time.Second):
			t.Fatalf("waiter %d (gate, on-demand, other PR, new head) was cancelled or stuck", i)
		}
		w.release()
	}
}

func TestSupersedingWithoutAChangeOrForAnotherRepoTouchesNothing(t *testing.T) {
	t.Parallel()
	q := &jobQueue{}
	_, release, err := q.Acquire(context.Background(), key(ports.PriorityBackground, "pr-7", sha("1")))
	require.NoError(t, err)
	defer release()
	require.Zero(t, q.Supersede("acme/pay", "", sha("2")))
	require.Zero(t, q.Supersede("other/repo", "pr-7", sha("2")))
	require.Zero(t, q.Supersede("acme/pay", "pr-7", sha("1")), "the same head is not stale")
}

func TestAJobThatReleasesTwiceDoesNotAdmitTwo(t *testing.T) {
	t.Parallel()
	q := &jobQueue{}
	_, first, err := q.Acquire(context.Background(), key(ports.PriorityGate, "KAN-1", sha("a")))
	require.NoError(t, err)
	a := enqueue(context.Background(), t, q, key(ports.PriorityGate, "KAN-2", sha("b")))
	b := enqueue(context.Background(), t, q, key(ports.PriorityGate, "KAN-3", sha("c")))
	first()
	first()
	<-a.granted
	select {
	case <-b.granted:
		t.Fatal("a second release admitted a second job while the first was still running")
	case <-time.After(50 * time.Millisecond):
	}
	a.release()
	<-b.granted
	b.release()
}

// gateExecutor blocks every job until released, so overlapping calls queue behind it.
type gateExecutor struct {
	mu      sync.Mutex
	started []string
	hold    chan struct{}
}

func (e *gateExecutor) Execute(_ context.Context, args []string, _ string) ([]byte, error) {
	e.mu.Lock()
	e.started = append(e.started, args[len(args)-4])
	e.mu.Unlock()
	<-e.hold
	return []byte(sarifOut), nil
}

func TestTheHandlerWaitsForItsTurnInsteadOfAnsweringBusy(t *testing.T) {
	t.Parallel()
	exec := &gateExecutor{hold: make(chan struct{})}
	r := jobRunner(t, exec)
	srv := httptest.NewServer(r.Handler())
	defer srv.Close()

	post := func(checkID string, onDemand bool) <-chan int {
		out := make(chan int, 1)
		job := ports.SandboxJob{Kind: ports.SandboxCheck, CheckID: checkID, OnDemand: onDemand, Repo: "acme/pay", SHA: sha("a"), Timeout: time.Minute}
		body, err := json.Marshal(request{Profile: "acme-pay", Job: job, Archive: []byte("a")})
		require.NoError(t, err)
		req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, srv.URL+"/v1/run", bytes.NewReader(body))
		require.NoError(t, err)
		setQueueHeaders(req, job)
		go func() {
			resp, err := srv.Client().Do(req)
			if err != nil {
				out <- -1
				return
			}
			_ = resp.Body.Close()
			out <- resp.StatusCode
		}()
		return out
	}
	first := post("patterns", false)
	require.Eventually(t, func() bool { exec.mu.Lock(); defer exec.mu.Unlock(); return len(exec.started) == 1 }, 2*time.Second, time.Millisecond)
	second := post("compare", false)
	third := post("patterns", true)
	close(exec.hold)
	for _, c := range []<-chan int{first, second, third} {
		require.Equal(t, http.StatusOK, <-c, "no caller was refused for the runner being busy")
	}
}

func TestTheHandlerRefusesHeadersThatClaimABetterPlaceThanTheJob(t *testing.T) {
	t.Parallel()
	r := jobRunner(t, &checkExecutor{output: []byte(sarifOut)})
	job := ports.SandboxJob{Kind: ports.SandboxCheck, CheckID: "patterns"}
	req := jobRequest(t, job, nil)
	req.Header.Set(headerPriority, strconv.Itoa(int(ports.PriorityGate)))
	_, status := r.handleRun(req)
	require.Equal(t, http.StatusBadRequest, status, "a background check cannot claim the gate lane")

	req = jobRequest(t, job, nil)
	req.Header.Del(headerPriority)
	_, status = r.handleRun(req)
	require.Equal(t, http.StatusBadRequest, status)
}

func TestSupersedingTwiceCancelsOnceAndAnOversizedResultIsRefused(t *testing.T) {
	t.Parallel()
	q := &jobQueue{}
	_, release, err := q.Acquire(context.Background(), key(ports.PriorityBackground, "pr-7", sha("1")))
	require.NoError(t, err)
	defer release()
	require.Equal(t, 1, q.Supersede("acme/pay", "pr-7", sha("2")))
	require.Equal(t, 1, q.Supersede("acme/pay", "pr-7", sha("2")), "a replay is harmless: it only cancels an already cancelled context again")

	big := "\nheimdall-result-begin\n{\"x\":\"" + strings.Repeat("a", ports.MaxJobResult) + "\"}\n\nheimdall-result-end\nheimdall-exit=0\n"
	r := jobRunner(t, &checkExecutor{output: []byte(big)})
	_, status := r.handleRun(jobRequest(t, ports.SandboxJob{Kind: ports.SandboxCheck, CheckID: "patterns"}, nil))
	require.Equal(t, http.StatusUnprocessableEntity, status, "a result over its bound is refused, whatever the wrapper did")
}
