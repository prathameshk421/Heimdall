package ids

import (
	"crypto/rand"
	"fmt"
	"sync"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/clock"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

var _ ports.IDs = (*Generator)(nil)

// Generator makes ids for ports.IDs. It keeps the last ULID so that every new one is greater than the last.
type Generator struct {
	mu   sync.Mutex
	last id.ULID
	now  func() time.Time
}

// New returns a Generator that reads the system clock and crypto/rand.
func New() *Generator {
	return newGenerator(clock.System{}.Now)
}

func newGenerator(now func() time.Time) *Generator {
	return &Generator{now: now}
}

// NewFinding returns a finding id: fnd_ and a ULID.
func (g *Generator) NewFinding() id.FindingID {
	return id.FindingID("fnd_" + g.nextULID().String())
}

// NewRun returns a run id: run_ and a ULID.
func (g *Generator) NewRun() id.RunID {
	return id.RunID("run_" + g.nextULID().String())
}

// NewCase returns a case id: case_ and a ULID.
func (g *Generator) NewCase() id.CaseID {
	return id.CaseID("case_" + g.nextULID().String())
}

// NewTask returns a task id: tsk_ and a ULID.
func (g *Generator) NewTask() id.TaskID {
	return id.TaskID("tsk_" + g.nextULID().String())
}

// NewEvent returns a version 4 UUID in lowercase, the form the event table stores.
func (g *Generator) NewEvent() change.EventID {
	var b [16]byte
	readEntropy(b[:])
	b[6] = b[6]&0x0f | 0x40 // version 4
	b[8] = b[8]&0x3f | 0x80 // RFC 4122 variant
	return change.EventID(fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]))
}

// nextULID returns a ULID strictly greater than every ULID this generator returned before, except in the one case
// described in doc.go (a random part used up at the last representable millisecond).
func (g *Generator) nextULID() id.ULID {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := clampMilli(g.now())
	if g.last == "" {
		g.last, _ = id.NewULID(now, randomTen()) // now is clamped to 48 bits, so NewULID cannot fail
		return g.last
	}
	next, err := id.Next(g.last, now, randomTen())
	if err != nil {
		// The random part of the last id is used up within its millisecond. Move to the next millisecond.
		prev, _ := g.last.Time() // g.last came from NewULID, so it always has a timestamp
		next, _ = id.NewULID(min(prev+1, maxULIDMilli), randomTen())
	}
	g.last = next
	return next
}

// maxULIDMilli is the largest timestamp a ULID holds: 48 bits, which lasts until the year 10889.
const maxULIDMilli = 1<<48 - 1

// clampMilli returns the millisecond timestamp for a ULID. A time before 1970 is read as 0, and a time after the
// 48-bit range is read as its last millisecond, so the id stays valid and ordered.
func clampMilli(t time.Time) uint64 {
	ms := t.UnixMilli()
	switch {
	case ms < 0:
		return 0
	case uint64(ms) > maxULIDMilli:
		return maxULIDMilli
	}
	return uint64(ms)
}

// randomTen returns 80 bits from crypto/rand.
func randomTen() [10]byte {
	var b [10]byte
	readEntropy(b[:])
	return b
}

// readEntropy fills b from crypto/rand. Since Go 1.24, Read never returns an error and always fills b, so the
// error result is ignored on purpose. The port has no error result to return it through.
func readEntropy(b []byte) {
	_, _ = rand.Read(b)
}
