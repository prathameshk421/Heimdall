<!-- heimdall:proof-summary change=412 version=2 head=a1b2c3d4e5f60718293a4b5c6d7e8f9012345678 -->

## Proven: 1 blocking finding · enforce · `a1b2c3d`

### Needs a person
Nothing needs a person.

### Proven findings
- **F1** `internal/pay/refund.go:42-44` A refund runs twice when the retry flag is set, so the customer is paid twice.
  Severity critical, tier E1, blocks the merge. Evidence: failing test `TestRefundRunsOnceWhenRetried`. Raised by Heimdall and CodeRabbit.

<details>
<summary>Unproven, not shown: 3 findings from 2 reviewers</summary>

No test or tool backed these, so they are not shown as findings. Unproven does not mean wrong.

- CodeRabbit 2: 1 no test reproduced, 1 refuted
- Greptile 1: 1 outdated

Findings:

- **F2** `internal/pay/client.go:10` (CodeRabbit, no test reproduced) This call may return a nil client on the error path.
- **F3** `internal/pay/client.go:10` (CodeRabbit, refuted) This call may return a nil client on the error path.
- **F4** `internal/pay/client.go:10` (Greptile, outdated) This call may return a nil client on the error path.

</details>

### Fast check
- met `secrets`: no new secret in 4 added files
- met `analyzers`: no new result at or above medium
- met `pinned_tests`: no pinned test changed
- met `test_integrity`: no mocked changed file, no weaker assertion
- not checked `diff_coverage`: no action.diff\_coverage.min in the policy
- met `approval`: approved on the head commit by a non-author
- met `memory_rules`: 2 rule cards apply, all hold
- met `heimdall_files`: no Heimdall file changed

### Not checked
- `diff_coverage`: no action.diff\_coverage.min in the policy

Heimdall judges only what is listed on this page. Whatever is not listed was not looked at, so silence is not safety.

Cost: $0.24 this run, $3.10 this month, 412.3k input and 9.1k output tokens, 4m 12s.
