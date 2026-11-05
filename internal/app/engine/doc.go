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
//   - Engine.Start(ctx, first) (Outcome, error): opens a change. The first event must be change.triggered at version 1.
//     It is offered to the open steps on a view that holds only it, and stored with their events and effects in one
//     append at version 0. A change that already exists returns ports.ErrVersionConflict, wrapped and not retried.
//   - Engine.Advance(ctx, id, incoming) (Outcome, error): stores each incoming event with the events and effects the
//     open steps decide on it, in one append per event, then calls the open steps until a pass stores nothing.
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
// One incoming event is one decision (Start and Advance). The event is numbered, then each open step's OnEvent reads a
// view that ends with it and with the events of the steps before it. The event, the events the steps return and all
// their effects go in one Append. Start and Advance keep this rule; Start does not load the change, it expects version
// 0. Steps in the drive loop and in Timeout decide one call at a time, and an effect there needs an event in the same
// Result.
//
// Failures. A step call that returns an error becomes task.failed when the step has a role and the role has an
// assigned task: the event names the task, the agent and the reason, and it is caused by the task.assigned event. A
// failure is recorded once per task. The run keeps going, and the step is listed in Outcome.Failed; the step is called
// again on the next run. A failure with no task to fail (a step with no role, or no task yet) returns the error from
// Advance or Timeout. For an incoming event, the event and the other steps' results are stored in the same append, and
// the error is returned after it; a repeat of the stored event stores nothing. For Start and Timeout nothing is stored
// for that call. A cancelled context returns its error and stores nothing for the call in progress.
//
// Invariants.
//   - Steps do no I/O. Every write goes through the store, with the expected version the decision read.
//   - A step is never called once it is finished, so a re-run of a finished workflow stores nothing.
//   - The events the engine writes itself take their id and time from the injected ports (IDs, Clock). The engine reads
//     no other clock.
//   - An effect is stored only with an event, and it is committed with the last event of its append. The outbox
//     drainer (internal/app/outbox) performs it later.
//   - The engine holds no per-change state, so one Engine serves every change. Callers serialize the runs of one change
//     (TECH_SPEC 2.4).
//
// on_fail (TECH_SPEC 5.1, onfail.go and plan.go). A step returns a StepFailedError when its check failed. Its own record
// (the Result) is stored, then its on_fail applies.
//   - reject with no retry: the record is all that is stored. The step is not finished, so the steps after it wait, and
//     the run does not fail. Nothing is written again until the step has something new to judge.
//   - retry R until task_budget: attempts(R) is 1 plus the step.retried events that name R. While attempts(R) is less
//     than Config.MaxAttempts, step.retried{Step: R, Attempt: attempts+1, Reasons} is stored after the record. Reasons
//     carry at most 8 items, each trimmed and cut to the text limit.
//   - Budget used up, or no retry: step.escalated{Step, Attempts, Reason} is stored, unless on_fail says reject. An
//     escalated step is held: the engine does not call it, and Outcome.Complete is false, until a later step.retried
//     names it or a step it waits for. A step with no on_fail that fails escalates too, so nothing stops silently.
//   - Windows. A step sees the events from the latest step.retried that names the step or a step it waits for, and that
//     retry is kept in the window. A retry therefore reopens its target and everything after it: their Done, Start and
//     OnEvent see the change as if the earlier work had not happened. The events before the retry stay in the store.
//   - Replay. The decision depends only on the stored events and the config, so an engine started on the same events
//     makes the same decisions, and it writes nothing for work already recorded.
//   - Cost is not enforced: no event carries spend, so the cost part of the task budget is not verified (T19). The budget
//     here is the number of attempts in the config.
//
// The YAML loader is Later (D54). The On field of a step is informational: the trigger of a change is the change.triggered
// event that the caller passes in, not a match on the on string.
//
// Failure modes of the engine itself. A store that is down returns its error from the run, and the caller retries the
// run later; the events of earlier decisions in the run are already stored, and a re-run carries on from them. A step
// that never settles (it stores events on every pass) fails the run after one pass per step plus one.
//
// Not verified here: the postgres adapter (T20) must attach an effect to the last event of an append, as the fake store
// does, and the race detector was not run (cgo is off on this machine). The concurrent Start test uses a mutex in the
// fake store and stateless steps.
//
// Tests. fakes_test.go holds the fakes: an in-memory store that checks what postgres checks and records the event each
// effect attaches to, fake steps with an OnEvent hook, fake clock and fake ids. engine_test.go covers the run: order,
// expected versions, version conflicts, failures and task.failed, timeouts with the injected clock, idempotent re-runs,
// incoming events, cancellation, the validation of New, Start (the first event, one append, no retry, two concurrent
// starts), and the effects of OnEvent in the same append as their event.
// onfail_test.go covers on_fail: a retry inside the budget, budget used up, reject, replay, a conflict mid-retry, the
// windows of downstream steps, the bounds on reasons, and the config checks. default_test.go runs the default workflow of
// TECH_SPEC 5.1 from the ticket to the gate on the fakes, and the sensitive-domain signoff.
package engine
