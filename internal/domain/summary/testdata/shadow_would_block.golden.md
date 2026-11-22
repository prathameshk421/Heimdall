<!-- heimdall:proof-summary change=412 version=2 head=a1b2c3d4e5f60718293a4b5c6d7e8f9012345678 -->

## Would block because 1 finding is proven · shadow · `a1b2c3d`

Shadow mode: this reports only. It does not block a merge.

### Needs a person
Nothing needs a person.

### Proven findings
- **F1** `internal/web/query.go:88` The query is built from the request without a placeholder, so it can be injected.
  Severity high, tier E2, would block the merge. Evidence: tool `semgrep`, rule `go.lang.security.sql-injection`. Raised by Heimdall.

Unproven, not shown: none.

### Fast check
- **unmet** `secrets`: would block because a token-shaped value in deploy/run.sh line 12
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

Cost: $0.31 this run, $3.10 this month, 502k input and 11k output tokens, 4m 12s.
