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
