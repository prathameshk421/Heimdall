# Test Authoring Benchmark Suite (`testdata/testbench/`)

This suite benchmarks test-author agent performance in translating ticket acceptance criteria into sound, executable test plans (`TestPlan@1`) and robust test suites (`TestSubmission@2`).

## Evaluation Dimensions

1. **Plan Soundness:**
   - Adherence to `TestPlan@1` pure rules (mandatory negative/boundary cases, oracle validity, declared kinds).
   - AC to behaviour mapping completeness.
2. **Execution Integrity:**
   - Red-on-base: Test fails cleanly on the base tree (proving defect sensitivity).
   - Green-on-head: Test passes cleanly on the implementation head.
3. **Mutation Power:**
   - High mutation score: Kills seeded semantic mutants without reporting false survivors.

## Split

- `cases/`: Development/training cases (KAN-22 health check, KAN-23 refund cap).
- `heldout/`: Held-out evaluation cases (KAN-35 currency exchange, KAN-42 idempotent payout), written before any run and held constant across prompt experiments.

## Case layout

Each case folder holds the ticket and the gold answers:

```
<case>/
  TICKET.md         acceptance criteria; the source of truth for the reference and the mutants
  gold_plan.json    the TestPlan@1 a correct author should produce
  mutants.json      the planted-wrong implementations (see below)
  go/               Go: go.mod (module benchcase/<case>, no dependencies), the reference
                    implementation, gold_test.go, and mutants/
  py/               Python (KAN-22 and KAN-23 only): reference, test_gold.py (unittest, standard
                    library only), and mutants/
```

Languages per case: Go for all four cases; Python for KAN-22 and KAN-23. The reference and each
mutant is a complete source file with the same name, so a mutant replaces the reference wholesale.

The Go folders are their own modules because the go tool ignores `testdata/`; `make fast` does not
build them. `run_gold.sh` is how they are built and run.

Gold tests use the public API only: HTTP cases drive an `http.Handler` with `httptest`; the Python
cases call the module's public function or class.

## Mutants

`mutants.json` is a list of objects, one per planted-wrong implementation:

```json
{ "id": "go-M1", "file": "go/mutants/m1_db_ping.go", "ac": "AC1", "what_is_wrong": "..." }
```

- `id` is unique within the case; `go-` or `py-` prefixes the language.
- `file` is relative to the case folder and must be inside `<lang>/mutants/`.
- `ac` names the acceptance criterion the mutant breaks.
- `what_is_wrong` says in one sentence what the implementation does wrongly.

Every file in `<lang>/mutants/` must be listed, and every listed file must exist. Each case has at
least three mutants per language.

## Running the gold check

```sh
bash testdata/testbench/run_gold.sh
```

For every case and language it runs the gold test against the reference (expected `pass`) and against
each mutant (expected `fail`). A `fail` is only counted when the output shows an assertion failure:
for Go, `--- FAIL` with no `[build failed]`, `[setup failed]` or `panic:`; for Python, unittest's `FAIL:`
with no `ERROR:`. It prints one row per run (case, language, implementation, expected, got, ok) and exits
1 if any row is wrong. Each implementation is copied into a fresh temp directory, so the repository is
never edited. The script needs `go`, `python3` and `bash`; it makes no network calls.

Not measured here: the kill rate of a test author's own tests, or any model run. This script checks the
gold answers only.
