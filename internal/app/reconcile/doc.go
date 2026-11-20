// Package reconcile starts all work of a change: it reads the change's rows and does what each open row says
// (HARNESS_DESIGN B3). It is the only component that starts work. Everything else only records facts.
//
// # Purpose
//
// Work used to start from the event that arrived, so a missed edge stalled a change forever. Now every Run reads the
// whole log, folds it into a view, asks each row what it needs (change.Next) and does it. Running it twice, or after a
// crash, does nothing new: a job's key names its input, so the same input runs once and a new input runs again. It
// does not judge anything itself; the rows decide, the executors do the outside work and record what happened.
//
// # API
//
//   - New(Config) (*Reconciler, error): checks the configuration and builds the reconciler.
//   - Reconciler.Run(ctx, change): decides and acts for one change; serialized per change; retries a version conflict.
//   - Reconciler.Sweep(ctx): Runs every change that is not over (ports.OpenChanges).
//   - Reconciler.Kick(change) and Loop(ctx): a kick wakes a worker for that change; the loop also sweeps every 30 s.
//   - Kicking: an event store that kicks after each successful append.
//   - Recorder.Record(ctx, change, build): the one way any other component appends; loads, folds, calls build,
//     numbers the events, retries a conflict.
//   - InputSource: what the rows need from outside the log (the policy at a commit, the gate's outside facts, the
//     promoted profile), cached for a minute.
//
// # Invariants
//
// A Run appends facts only at the version it read, and attaches jobs only with the last fact or to the newest event. A
// task is assigned only when the change's room exists. Payloads hold values from the view and the inputs, never the
// clock, so one key always means one content. An input that cannot be read stops the Run with an error; no row sees an
// empty default.
//
// # Failure modes
//
// Two writers on one change: the append conflicts, the Run loads again and decides again (five times). A lost kick: the
// sweep covers it. A job that fails for good or is in doubt is a need on the board (Missing), and nothing retries it
// until a person runs `heimdall resume`. A silent agent is woken again, reset and failed by liveness.Next; a provider
// that refuses is probed again on each Run.
//
// # Tests
//
// reconcile_test.go drives the reconciler on in-memory fakes: the room job, the next job after a fact, a conflict, a
// rejected submission, a new head, the wake ladder, timeouts, dead jobs, resume, sweep, kicks and serialization.
package reconcile
