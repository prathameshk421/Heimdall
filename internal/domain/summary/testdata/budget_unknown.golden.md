<!-- heimdall:proof-summary change=412 version=2 head=a1b2c3d4e5f60718293a4b5c6d7e8f9012345678 -->

## Unknown: proof run hit its budget · enforce · `a1b2c3d`

### Needs a person
- The proof run is unknown (budget hit). Only a person clears it: rerun the proof or decide by hand.
- `approval` is unknown: the reviews could not be read

### Proven findings
Nothing is proven.

<details>
<summary>Unproven, not shown: 4 findings from 2 reviewers</summary>

No test or tool backed these, so they are not shown as findings. Unproven does not mean wrong.

- CodeRabbit 2: 2 not checked: budget hit
- Copilot 2: 1 no test reproduced, 1 not checked: budget hit

Findings:

- **F4** `internal/pay/client.go:10` (Copilot, no test reproduced) This call may return a nil client on the error path.
- **F1** `README.md:3` (CodeRabbit, not checked: budget hit) The install step names a flag that does not exist.
- **F2** `README.md:3` (CodeRabbit, not checked: budget hit) The install step names a flag that does not exist.
- **F3** `README.md:3` (Copilot, not checked: budget hit) The install step names a flag that does not exist.

</details>

### Fast check
- met `secrets`: no new secret in 4 added files
- met `analyzers`: no new result at or above medium
- met `pinned_tests`: no pinned test changed
- met `test_integrity`: no mocked changed file, no weaker assertion
- not checked `diff_coverage`: no action.diff\_coverage.min in the policy
- **unknown** `approval`: blocks until known: the reviews could not be read
- met `memory_rules`: 2 rule cards apply, all hold
- met `heimdall_files`: no Heimdall file changed

### Not checked
- `diff_coverage`: no action.diff\_coverage.min in the policy
- Proof run: did not finish (budget hit). What it had not reached was not checked.
- 3 candidate findings not checked: budget hit

Heimdall judges only what is listed on this page. Whatever is not listed was not looked at, so silence is not safety.

Cost: $0.50 this run, $3.10 this month, 800k input and 14k output tokens, 4m 12s.
