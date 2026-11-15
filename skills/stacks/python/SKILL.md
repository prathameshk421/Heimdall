---
name: stack-python
description: Python notes for Heimdall agents: the Python pitfalls worth checking, how to write a red-on-base pytest test against the repository's seam, and how Python libraries read HEIMDALL_SEED. Load this on a Python change.
---

# Python

## Pitfalls

- **Mutable default arguments.** `def f(x, items=[])` shares one list across all calls. Default to `None` and
  build inside.
- **Bare `except`, or `except Exception` that swallows.** It hides the failure and turns a red test green.
  Catch the narrow type and re-raise or assert on it.
- **Late-binding closures.** `[lambda: i for i in range(3)]` all return 2. Bind with `lambda i=i: i` or a
  factory.
- **`==` versus `is`, and truthiness.** `0`, `""`, `[]` and `None` are falsy; comparing identity for equality,
  or truthiness for "is empty", is usually a bug. `is` is for singletons.
- **Division.** `/` is true division, `//` is floor division; on mixed int and float the result type changes
  and money or sizes drift.
- **Naive datetimes.** Mixing aware and naive datetimes raises at comparison time; mixing naive ones silently
  loses the zone. Be aware everywhere.
- **`dict` and `set` ordering assumptions.** Dicts keep insertion order (3.7+), sets do not; iterating a set
  for output makes tests flaky under `PYTHONHASHSEED`.
- **Boolean traps in `while` and `filter`.** `filter(None, xs)` drops `0` and `""`. Say what you mean.

## How to write a test

- `pytest`. Fixtures over `setUp`/`tearDown`; `tmp_path` for files; `monkeypatch` for env and attributes;
  `parametrize` with explicit `ids` for named rows.
- Python tests reach the app through the repository's declared seam: an HTTP client against the app (for
  example `starlette`'s `TestClient`) or the profile's seam, never internals.
- On base every test must fail on an assertion (`<failure>` in JUnit XML), never on an error (`<error>`): if
  the module, route or fixture does not exist yet, the failure must still be an assertion (a 404 or 405 status,
  a missing key asserted by equality), not a collection or import error.
- Build inputs inline in the test. One behaviour per test; the name says the behaviour; the failure message
  says got and want.
- Deterministic only: no sleeps, no network beyond the app under test, no shared state, no unseeded
  randomness.

## How to read the seed

- The harness sets `HEIMDALL_SEED` on every run: derived from the ticket key on base, from the head SHA on a
  head. Parse it with `seed = int(os.environ["HEIMDALL_SEED"])`.
- Seed the standard library RNG with `random.seed(seed)`. Seed NumPy with `np.random.default_rng(seed)`. Pass
  per-case seeds to Hypothesis (`--hypothesis-seed=$HEIMDALL_SEED` comes from the profile's run line, or use
  `settings(derandomize=False)` and your own strategy draws from the seeded RNG).
- Every push sees different inputs. An implementation that special-cases the test inputs fails the next push;
  generate inputs from the seed instead of a fixed list. A test that fails only for some seeds is a bug in the
  test.
