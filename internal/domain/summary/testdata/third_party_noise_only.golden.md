<!-- heimdall:proof-summary change=412 version=2 head=a1b2c3d4e5f60718293a4b5c6d7e8f9012345678 -->

## Nothing proven against this change · enforce · `a1b2c3d`

### Needs a person
Nothing needs a person.

### Proven findings
Nothing is proven.

<details>
<summary>Unproven, not shown: 9 findings from 3 reviewers</summary>

No test or tool backed these, so they are not shown as findings. Unproven does not mean wrong.

- CodeRabbit 7: 5 no test reproduced, 2 refuted
- Bugbot 1: 1 no location
- Greptile 1: 1 outdated

Findings:

- **F1** `internal/pay/client.go:10` (CodeRabbit, no test reproduced) This call may return a nil client on the error path.
- **F2** `internal/pay/client.go:10` (CodeRabbit, no test reproduced) This call may return a nil client on the error path.
- **F3** `internal/pay/client.go:10` (CodeRabbit, no test reproduced) This call may return a nil client on the error path.
- **F4** `internal/pay/client.go:10` (CodeRabbit, no test reproduced) This call may return a nil client on the error path.
- **F5** `internal/pay/client.go:10` (CodeRabbit, no test reproduced) This call may return a nil client on the error path.
- **F6** `internal/pay/client.go:10` (CodeRabbit, refuted) This call may return a nil client on the error path.
- **F7** `internal/pay/client.go:10` (CodeRabbit, refuted) This call may return a nil client on the error path.
- **F9** `internal/pay/client.go:10` (Greptile, outdated) This call may return a nil client on the error path.
- **F8** `internal/pay/client.go:10` (Bugbot, no location) This call may return a nil client on the error path.

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

Cost: $0.18 this run, $3.10 this month, 280k input and 6k output tokens, 4m 12s.
