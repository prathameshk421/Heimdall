package ids

import (
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
	"github.com/stretchr/testify/require"
)

var _ ports.IDs = New()

var reUUIDv4 = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

// fixedClock returns a clock that reads each millisecond in turn, then keeps the last one.
func fixedClock(millis ...int64) func() time.Time {
	i := 0
	return func() time.Time {
		ms := millis[len(millis)-1]
		if i < len(millis) {
			ms = millis[i]
			i++
		}
		return time.UnixMilli(ms)
	}
}

func TestIDsParseBackWithTheDomainParsers(t *testing.T) {
	g := New()
	f := g.NewFinding()
	_, err := id.ParseFindingID(f.String())
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(f.String(), "fnd_"))

	r := g.NewRun()
	_, err = id.ParseRunID(r.String())
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(r.String(), "run_"))

	c := g.NewCase()
	_, err = id.ParseCaseID(c.String())
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(c.String(), "case_"))

	tk := g.NewTask()
	_, err = id.ParseTaskID(tk.String())
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(tk.String(), "tsk_"))

	e := g.NewEvent()
	_, err = change.ParseEventID(e.String())
	require.NoError(t, err)
	require.Regexp(t, reUUIDv4, e.String())
}

func TestULIDTimeIsTheSystemClock(t *testing.T) {
	before := time.Now().UnixMilli()
	f := New().NewFinding()
	after := time.Now().UnixMilli()
	ulid := id.ULID(strings.TrimPrefix(f.String(), "fnd_"))
	ms, err := ulid.Time()
	require.NoError(t, err)
	require.GreaterOrEqual(t, int64(ms), before)
	require.LessOrEqual(t, int64(ms), after)
}

func TestTenThousandCallsStrictlyIncrease(t *testing.T) {
	g := New()
	prev := ""
	for i := 0; i < 10000; i++ {
		var cur string
		switch i % 4 {
		case 0:
			cur = strings.TrimPrefix(g.NewFinding().String(), "fnd_")
		case 1:
			cur = strings.TrimPrefix(g.NewRun().String(), "run_")
		case 2:
			cur = strings.TrimPrefix(g.NewCase().String(), "case_")
		default:
			cur = strings.TrimPrefix(g.NewTask().String(), "tsk_")
		}
		if prev != "" {
			require.Less(t, prev, cur, "call %d did not increase", i)
		}
		prev = cur
	}
}

func TestSteppedBackClockStillIncreases(t *testing.T) {
	g := newGenerator(fixedClock(1000, 999, 1000, 500, 2000))
	ids := []string{
		strings.TrimPrefix(g.NewRun().String(), "run_"),
		strings.TrimPrefix(g.NewRun().String(), "run_"),
		strings.TrimPrefix(g.NewRun().String(), "run_"),
		strings.TrimPrefix(g.NewRun().String(), "run_"),
		strings.TrimPrefix(g.NewRun().String(), "run_"),
	}
	for i := 1; i < len(ids); i++ {
		require.Less(t, ids[i-1], ids[i], "id %d is not greater than id %d", i, i-1)
	}
	first := id.ULID(ids[0])
	ms, err := first.Time()
	require.NoError(t, err)
	require.Equal(t, uint64(1000), ms)
	second := id.ULID(ids[1])
	ms, err = second.Time()
	require.NoError(t, err)
	require.Equal(t, uint64(1000), ms, "a stepped-back clock keeps the previous timestamp")
}

func TestExhaustedRandomPartMovesToTheNextMillisecond(t *testing.T) {
	g := newGenerator(fixedClock(1000))
	full, err := id.NewULID(1000, [10]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff})
	require.NoError(t, err)
	g.last = full
	next := g.nextULID()
	require.Greater(t, string(next), string(full))
	ms, err := next.Time()
	require.NoError(t, err)
	require.Equal(t, uint64(1001), ms, "the next id moves to the following millisecond")
}

func TestClockOutsideTheULIDRangeIsClamped(t *testing.T) {
	before1970 := newGenerator(fixedClock(-5000))
	ms, err := id.ULID(strings.TrimPrefix(before1970.NewRun().String(), "run_")).Time()
	require.NoError(t, err)
	require.Equal(t, uint64(0), ms, "a time before 1970 is read as 0")

	pastRange := newGenerator(fixedClock(1 << 50))
	ms, err = id.ULID(strings.TrimPrefix(pastRange.NewRun().String(), "run_")).Time()
	require.NoError(t, err)
	require.Equal(t, uint64(maxULIDMilli), ms, "a time past 48 bits is read as the last millisecond")
}

func TestConcurrentUseGivesNoDuplicates(t *testing.T) {
	const workers = 8
	const perWorker = 1000
	g := New()
	var mu sync.Mutex
	seen := make(map[string]struct{}, workers*perWorker)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				got := g.NewFinding().String()
				mu.Lock()
				seen[got] = struct{}{}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	require.Len(t, seen, workers*perWorker, "some concurrent calls returned the same id")
}
