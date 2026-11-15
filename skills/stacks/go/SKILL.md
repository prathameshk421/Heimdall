---
name: stack-go
description: Go notes for Heimdall agents: the Go pitfalls worth checking, how to write a red-on-base Go test against the repository's seam, and how Go libraries read HEIMDALL_SEED. Load this on a Go change.
---

# Go

## Pitfalls

- **Dropped or shadowed `err`.** An inner `err :=` shadows the outer one; a call whose error is assigned to a
  blank identifier is unchecked. Check both.
- **Loop variable capture.** On Go before 1.22 a closure captures the loop variable, not its value; a goroutine
  started in a loop without an argument sees every value after the loop moves on.
- **`defer` in a loop.** Defers run at function end, not iteration end; open files or locks accumulate. Hoist
  into a per-iteration function or close explicitly.
- **Typed-nil interfaces.** A `nil` concrete value stored in a non-nil interface is `!= nil`; error checks that
  compare the interface to nil pass when they should not.
- **Nil map writes, nil slice appends.** Writing to a nil map panics; appending to a nil slice is fine. Both
  read as "works in tests, panics in production".
- **Integer overflow and narrowing conversions.** Converting `int64` to `int` truncates; multiplication before
  division overflows sooner than people expect.
- **`time.Sleep` in tests.** Sleeps are never acceptable in a test; they make it slow and flaky. Poll the
  condition with a deadline, or use the event the change emits.
- **Context ignored.** A function that takes a `context.Context` and never checks `Done()` cannot be cancelled;
  it hangs the runner and the gate.
- **Unnamed table rows.** `[]struct{...}` without a name per case makes a failure unreadable. Name every row.

## How to write a test

- Standard library `testing`; use testify only if the repository already does. Table-driven tests with named
  subtests (`t.Run("second refund is refused", ...)`).
- Go tests reach the app through the repository's declared seam (usually HTTP with `httptest`), never internals.
  Import only the seam, the standard library and the policy allowlist.
- On base every test must load, run and fail on an assertion, never on a compile error. A route that does not
  exist yet answers 404 or 405; asserting the expected status is a correct red.
- Build inputs inline in the test. Failure messages say got and want. One behaviour per test.
- Deterministic only: no sleeps, no network beyond the app under test, no shared state, no unseeded randomness.

## How to read the seed

- The harness sets `HEIMDALL_SEED` on every run: derived from the ticket key on base, from the head SHA on a
  head. Parse it with `strconv.ParseUint(os.Getenv("HEIMDALL_SEED"), 10, 64)`.
- Derive randomness with `rand.New(rand.NewSource(int64(seed)))` (or `math/rand/v2`'s `rand.NewPCG`). Derive
  per-case seeds from the run seed, for example `seed + uint64(rowIndex)`, so rows stay independent.
- Every push sees different inputs. An implementation that special-cases the test inputs fails the next push;
  property-style tests should construct their inputs from the seed, not from a fixed list.
