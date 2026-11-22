# Heimdall CI/CD & OSS Analyzer Integrations

Heimdall is designed to sit **above** your existing open-source analyzers and CI providers, turning noisy warnings into proven, fail-closed merge decisions.

## Included Templates

1. **[github-analyzers.yml](github-analyzers.yml)** — Runs popular OSS security analyzers (**gosec**, **Semgrep**, **Trivy**) on pull requests and uploads their SARIF to GitHub Code Scanning. Heimdall's Action gate (`heimdall/fast`) reads those Code Scanning alerts automatically via the `analyzers` clause.
2. **[gitlab-ci.yml](gitlab-ci.yml)** — Drop-in GitLab CI template that builds/runs Heimdall's deterministic checks (secret scan, diff coverage, SARIF diffing) and exports a weekly digest artifact on merge requests.

## Supported OSS Formats (Built Into Heimdall)

| Category | Supported OSS Tools / Formats | Heimdall Package |
|---|---|---|
| **Static Analysis (SARIF 2.1)** | Semgrep, gosec, Trivy, CodeQL, golangci-lint | `internal/domain/sarif` |
| **Coverage (Diff Coverage)** | Go `coverprofile`, LCOV (`c8`, Jest, `coverage.py`, `llvm-cov`), Cobertura XML (Python, Java/JaCoCo) with `Prefix` + `AddPrefix` path rebasing | `internal/domain/coverage`, `internal/adapters/coverage` |
| **Third-Party AI Reviewers** | CodeRabbit, GitHub Copilot, Cursor Bugbot, Greptile, Macroscope, Codex, Devin — ingested as candidates and required to earn E1/E2 proof or dropped | `internal/domain/external` |
| **Secret & PII Redaction** | Built-in gitleaks-aligned secret rules + PII scrubber for local memory cards | `internal/secretscan`, `internal/domain/redact` |
| **Session Memory** | Claude Code, Codex, OpenCode (SQLite, CGO-free), Cursor | `internal/adapters/sessions` |
| **Test Runners** | Go (`go test -json`), Rust (`cargo test`), TypeScript/JS (`vitest`) + reverse-dependency test selection (`go list -deps`) | `internal/domain/testselect`, `internal/adapters/uidsandbox` |
