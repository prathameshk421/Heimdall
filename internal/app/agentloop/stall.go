package agentloop

import (
	"errors"
	"slices"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// stallState counts the answers since the last call to a progress tool, and whether the stall nudge was sent.
type stallState struct {
	since  int
	nudged bool
}

func (cfg Config) checkStall() error {
	switch {
	case cfg.StallTurns < 0 || cfg.StallGrace < 0:
		return errors.New("agent loop config needs stall turns and stall grace of zero or more")
	case cfg.StallTurns == 0:
		return nil
	case cfg.StallGrace == 0 || strings.TrimSpace(cfg.StallNudge) == "" || len(cfg.ProgressTools) == 0:
		return errors.New("agent loop config with a stall rule needs a grace, a nudge and the progress tools")
	}
	return nil
}

// observeProgress reads one answer's calls: a call to a progress tool resets the count, any other answer adds one.
func (r *run) observeProgress(calls []ports.ModelToolCall) {
	if r.l.cfg.StallTurns == 0 {
		return
	}
	for _, c := range calls {
		if slices.Contains(r.l.cfg.ProgressTools, c.Name) {
			r.stall.since = 0
			return
		}
	}
	r.stall.since++
}

// stallNudge sends the fixed stall message once a run, after the answer that reaches Config.StallTurns with no
// progress. Like the other nudge, it is a protocol message about the tools, not reasoning for the model.
func (r *run) stallNudge() {
	cfg := r.l.cfg
	if cfg.StallTurns == 0 || r.stall.nudged || r.stall.since < cfg.StallTurns {
		return
	}
	r.stall.nudged = true
	r.res.Turn[len(r.res.Turn)-1].StallNudged = true
	r.messages = append(r.messages, ports.ModelMessage{Role: ports.ModelRoleUser, Content: cfg.StallNudge})
}

// stalled reports that the run reached the stall turns and the grace turns with no progress, so the next call is not
// made. A run whose nudge was already used for an earlier stall stops at the same count.
func (r *run) stalled() bool {
	cfg := r.l.cfg
	return cfg.StallTurns > 0 && r.stall.since >= cfg.StallTurns+cfg.StallGrace
}
