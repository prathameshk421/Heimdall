package reconcile

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/liveness"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
)

// recentCalls is how many of an agent's latest tool calls are read to find its last one on a task.
const recentCalls = 20

// task does what one Task action asks (HARNESS_DESIGN B3): assign it when no task of that kind and subject is live,
// replace a live task made for another input, and watch a live task made for this input.
func (r *Reconciler) task(ctx context.Context, d *decision, in change.In, t change.Task) error {
	v := in.View
	if v.Room == "" {
		return nil // the agent is woken in the change's room: nothing can be assigned before it exists
	}
	kind, ok := kindNamed(t.Kind)
	if !ok {
		return fmt.Errorf("task kind %q is not in the workflow kinds", t.Kind)
	}
	agent, ok := r.cfg.Agents(t.Kind, t.Subject)
	if !ok {
		r.cfg.Logger.WarnContext(ctx, "no agent holds the task", "change", string(in.Change), "kind", t.Kind, "subject", t.Subject)
		return nil
	}
	var current *taskRef
	for _, live := range liveTasks(v, t.Kind, t.Subject) {
		if live.info.Input == t.Input {
			current = &taskRef{live.id, live.info}
			continue
		}
		d.fact(change.TaskSuperseded{TaskID: live.id, Reason: "new input " + t.Input})
	}
	if current == nil {
		return r.assign(d, in, kind, agent, t)
	}
	return r.watch(ctx, d, in, *current)
}

type taskRef struct {
	id   id.TaskID
	info change.TaskInfo
}

// liveTasks are the live tasks of a kind and subject, oldest first.
func liveTasks(v change.View, kind, subject string) []taskRef {
	var out []taskRef
	for tid, info := range v.Tasks {
		if info.Live() && info.Kind == kind && info.Subject == subject {
			out = append(out, taskRef{tid, info})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].info.Since.Equal(out[j].info.Since) {
			return out[i].info.Since.Before(out[j].info.Since)
		}
		return out[i].id < out[j].id
	})
	return out
}

func kindNamed(name string) (workflow.Kind, bool) {
	for _, k := range workflow.Kinds() {
		if k.Name == name {
			return k, true
		}
	}
	return workflow.Kind{}, false
}

// assign records the task and attaches the job that wakes the agent in the change's room.
func (r *Reconciler) assign(d *decision, in change.In, kind workflow.Kind, agent id.AgentName, t change.Task) error {
	timeout := in.Settings.Timeouts[t.Kind]
	if timeout <= 0 {
		return fmt.Errorf("no timeout is configured for task kind %q", t.Kind)
	}
	tid := r.cfg.IDs.NewTask()
	assigned := change.TaskAssigned{TaskID: tid, Agent: agent, Role: kind.Holder, Deadline: in.Now.Add(timeout),
		Kind: t.Kind, Subject: t.Subject, Input: t.Input}
	if t.Kind == "verdict" {
		assigned.CaseID = id.CaseID(t.Subject) // the resolver's tools bind the task to its case
	}
	d.fact(assigned)
	return d.job(wakeJob(in, tid, agent, kind.Holder, "task/"+string(tid)))
}

// wakeJob is the task_assign job that wakes the agent in the change's room: the first assignment and each repeat of the
// wake line are the same job kind under different keys. The agent gets its inputs from get_task, so the job carries
// none.
func wakeJob(in change.In, tid id.TaskID, agent id.AgentName, role workflow.Role, keyTail string) change.Job {
	p := change.TaskAssignPayload{Agent: agent, Role: role, TaskID: tid, RoomID: in.View.Room, Change: in.Change}
	return change.Job{Kind: change.EffectTaskAssign, Key: change.EffectKey(string(in.Change) + "/" + keyTail),
		Target: string(tid), Payload: p}
}

// watch decides what to do about a live task made for the current input: time it out past its deadline, otherwise do
// what liveness.Next says about a quiet agent.
func (r *Reconciler) watch(ctx context.Context, d *decision, in change.In, t taskRef) error {
	if !in.Now.Before(t.info.Deadline) {
		d.fact(change.TaskTimedOut{TaskID: t.id, Agent: t.info.Agent, Role: t.info.Role, Reason: "the deadline passed"})
		return nil
	}
	last, err := r.lastCall(ctx, in, t.info)
	if err != nil {
		return err
	}
	facts := liveness.Facts{Since: t.info.Since, Acked: t.info.Status == change.TaskAckedStatus, LastCall: last,
		Repeated: t.info.Wakes > 0, StallWokenAt: t.info.StallWokenAt, Provider: providerHealth(in.View)}
	n := t.info.Wakes + 1
	switch liveness.Next(facts, r.cfg.Windows, in.Now) {
	case liveness.Wait:
		return nil
	case liveness.Repeat:
		d.fact(change.TaskRewoken{TaskID: t.id, N: n})
		return d.job(wakeJob(in, t.id, t.info.Agent, t.info.Role, fmt.Sprintf("%s/wake/%d", t.id, n)))
	case liveness.Probe, liveness.Blocked:
		return r.probe(ctx, d, in.View)
	case liveness.Rewake:
		d.fact(change.TaskRewoken{TaskID: t.id, N: n, Stall: true})
		return d.job(wakeJob(in, t.id, t.info.Agent, t.info.Role, fmt.Sprintf("%s/wake/%d", t.id, n)))
	case liveness.Fail:
		d.fact(change.TaskFailed{TaskID: t.id, Agent: t.info.Agent, Role: t.info.Role, Reason: "unresponsive"})
		return nil
	}
	return errors.New("liveness returned an action the reconciler does not know")
}

// probe asks the model provider whether it answers and records the answer when it is news: the first probe, or a change
// of health. A provider that refuses is probed again on every Run, so the block lifts as soon as it answers.
func (r *Reconciler) probe(ctx context.Context, d *decision, v change.View) error {
	h, err := r.cfg.Health.Probe(ctx)
	if err != nil {
		return fmt.Errorf("probe the model provider: %w", err)
	}
	if v.Provider != nil && v.Provider.Health == string(h) {
		return nil
	}
	d.fact(change.ProviderProbed{Health: string(h)})
	return nil
}

func providerHealth(v change.View) liveness.Health {
	if v.Provider == nil {
		return liveness.Unprobed
	}
	return liveness.Health(v.Provider.Health)
}

// lastCall is when the task's agent last called Heimdall about this change since the task began; zero if it did not.
func (r *Reconciler) lastCall(ctx context.Context, in change.In, info change.TaskInfo) (t time.Time, err error) {
	calls, err := r.cfg.Calls.RecentCalls(ctx, info.Agent, recentCalls)
	if err != nil {
		return t, fmt.Errorf("read tool calls of %s: %w", info.Agent, err)
	}
	for _, c := range calls { // newest first
		if c.At.After(info.Since) && (c.Ticket == "" || c.Ticket == in.Change) {
			return c.At, nil
		}
	}
	return t, nil
}
