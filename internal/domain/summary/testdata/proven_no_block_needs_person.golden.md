<!-- heimdall:proof-summary change=412 version=2 head=a1b2c3d4e5f60718293a4b5c6d7e8f9012345678 -->

## Blocked: fast check failed (approval) · enforce · `a1b2c3d`

### Needs a person
- 1 proven finding does not block under the policy. A person decides what to do.
- `approval` needs an approver: no approval on the head commit

### Proven findings
- **F1** `internal/web/handler.go:17-30` The handler returns the error text to the caller.
  Severity medium, tier E3, does not block. Evidence: team rule card `no-internal-errors-in-responses`. Raised by Heimdall.

Unproven, not shown: none.

### Fast check
- met `secrets`: no new secret in 4 added files
- met `analyzers`: no new result at or above medium
- met `pinned_tests`: no pinned test changed
- met `test_integrity`: no mocked changed file, no weaker assertion
- not checked `diff_coverage`: no action.diff\_coverage.min in the policy
- **unmet** `approval`: blocks because no approval on the head commit
- met `memory_rules`: 2 rule cards apply, all hold
- met `heimdall_files`: no Heimdall file changed

### Not checked
- `diff_coverage`: no action.diff\_coverage.min in the policy

Heimdall judges only what is listed on this page. Whatever is not listed was not looked at, so silence is not safety.

Cost: $0.27 this run, $3.10 this month, 350k input and 7k output tokens, 4m 12s.
