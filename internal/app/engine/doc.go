// Package engine runs a change's workflow: it calls the steps of the workflow in dependency order, stores the events and
// effects they return, and reports what the run did (TECH_SPEC 5, task T23, D54). It imports the standard library,
// internal/domain/change, internal/domain/id, internal/domain/workflow and internal/ports only.
//
// Public API
//   - Step (step.go): the contract of one stage. Start, OnEvent and OnTimeout return events, effects and timers; the
//     engine stores them. Done says whether the step is finished, from the events alone.
//   - ChangeView (step.go): the events of one change, stored and then this run's, numbered as they will be stored.
//   - Config and New(Config) (*Engine, error): builds an engine, or refuses to. New validates the workflow with
//     workflow.Validate against the step types, roles and schemas given, and checks that every when name has a
//     condition.
//   - Engine.Advance(ctx, id, incoming) (Outcome, error): stores the incoming events, offers each one to the open steps,
//     then calls the open steps until a pass stores nothing.
//   - Engine.Timeout(ctx, id, step) (Outcome, error): calls OnTimeout of one step, then runs the rest.
//   - Outcome: the version, the counts of events and effects stored, the timers asked for, the failed steps, and whether
//     every step is finished.
//
// The run. A step is open when every step it names in after is finished, and it is not finished itself. A step is
// finished when its when condition is false (skipped) or its Done holds on the view. Each open step is called once per
// pass, in workflow order. A pass that stores an event or an effect is followed by another, and the run stops at the
// first pass that stores nothing. Timers do not count as stored work: the same timer is reported once per run.
//
// Each call is one decision. The engine loads the change, decides on that view, and appends the result at the version
// the view had (ports.EventStore.Append). A version conflict loads the change again and decides again, up to five
// retries (TECH_SPEC 2.4). A decision that stores nothing appends nothing.
//
// Failures. A step call that returns an error becomes task.failed when the step has a role and the role has an
// assigned task: the event names the task, the agent and the reason, and it is caused by the task.assigned event. A
// failure is recorded once per task. The run keeps going, and the step is listed in Outcome.Failed; the step is called
// again on the next run. A failure with no task to fail (a step with no role, or no task yet) returns the error from
// Advance or Timeout, and nothing is stored for that call. A cancelled context returns its error and stores nothing for
// the call in progress.
//
// Invariants.
//   - Steps do no I/O. Every write goes through the store, with the expected version the decision read.
//   - A step is never called once it is finished, so a re-run of a finished workflow stores nothing.
//   - The events the engine writes itself take their id and time from the injected ports (IDs, Clock). The engine reads
//     no other clock.
//   - An effect is stored only with an event. The outbox drainer (internal/app/outbox) performs it later.
//   - The engine holds no per-change state, so one Engine serves every change. Callers serialize the runs of one change
//     (TECH_SPEC 2.4).
//
// What is not built here. The on_fail retry and then of a workflow step (TECH_SPEC 5.1) are read from the workflow but
// not acted on: a step that fails its check stays open until a later run changes what the view holds. The YAML loader
// is Later (D54). The On field of a step is informational: the trigger of a change is the change.triggered event that
// the caller passes in, not a match on the on string.
//
// Failure modes of the engine itself. A store that is down returns its error from the run, and the caller retries the
// run later; the events of earlier decisions in the run are already stored, and a re-run carries on from them. A step
// that never settles (it stores events on every pass) fails the run after one pass per step plus one.
//
// Tests. fakes_test.go holds the fakes: an in-memory store that checks what postgres checks, fake steps, fake clock and
// fake ids. engine_test.go covers the run: order, expected versions, version conflicts, failures and task.failed,
// timeouts with the injected clock, idempotent re-runs, incoming events, cancellation and the validation of New.
// default_test.go runs the default workflow of TECH_SPEC 5.1 from the ticket to the gate on the fakes, and the
// sensitive-domain signoff.
package engine
