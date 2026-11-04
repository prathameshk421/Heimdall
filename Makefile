# Heimdall build commands. See docs/harness/AGENTS.md for the loop that uses them.
# Tools are pinned in tools.mod and run with `go tool -modfile=tools.mod <name>` so they stay out of the product module.
SHELL := /usr/bin/env bash
.SHELLFLAGS := -eu -o pipefail -c
export PATH := $(HOME)/.local/go/bin:$(HOME)/go/bin:$(PATH)

TOOL := go tool -modfile=tools.mod
GOFILES = find . -name '*.go' -not -path './testdata/*' -not -path './.git/*'

.DEFAULT_GOAL := help
.PHONY: help
help: ## list the targets
	@grep -hE '^[a-zA-Z0-9_-]+:.*## ' $(MAKEFILE_LIST) | sed -E 's/:.*## /\t/' | sort

# ---- the loop: fast on every change, verify before a commit that ends a task ----

.PHONY: fast verify
fast: fmt-check vet arch lint unit ## L0 and L1: format, vet, dependency rule, lint, unit tests. Under 60 s; runs in the pre-commit hook
verify: fast race lint-selftest harness-test scripts-test vuln secrets ## adds L2 and L3: race and component tests, rule self-tests, vulnerabilities, secrets. Before a task's commit; CI runs this

.PHONY: fmt-check vet arch lint unit race
fmt-check:
	@bad=$$($(GOFILES) | xargs gofmt -l); if [ -n "$$bad" ]; then echo "gofmt needed:" >&2; echo "$$bad" >&2; exit 1; fi

vet:
	go vet ./...

arch: ## the dependency rule and the test-code rule on the real import graph
	go run ./cmd/archcheck

lint: ## golangci-lint with the pinned version and the repo's rules
	$(TOOL) golangci-lint config verify
	$(TOOL) golangci-lint run ./...

unit: ## unit and contract tests; a skipped test fails the run
	bash scripts/gotest.sh -count=1 -timeout 50s ./...

race: ## the same tests with the race detector, plus component tests that need Docker
	bash scripts/gotest.sh -race -count=1 -tags component ./...

# ---- rule and tooling self-tests ----

.PHONY: lint-selftest harness-test harness-install scripts-test hooks-install
lint-selftest: ## every lint rule still fires: bad fixtures flagged, good ones clean
	go test -tags lintselftest -count=1 ./internal/lintselftest/

harness-test: ## every hook case, and the harness installer
	bash docs/harness/test-hooks.sh
	bash docs/harness/test-install.sh

harness-install: ## copy docs/harness/ into place; CHECK=1 changes nothing and fails on drift
	bash scripts/harness-install.sh

scripts-test: ## tests of the make helper scripts
	bash scripts/test-scripts.sh

hooks-install: ## install the git pre-commit hook (make fast)
	bash scripts/hooks-install.sh

# ---- supply chain ----

.PHONY: vuln secrets
vuln: ## known vulnerabilities in the product module
	$(TOOL) govulncheck ./...

secrets: ## gitleaks over the git history (never reads the working tree, so never .env)
	$(TOOL) gitleaks git --no-banner --redact .

# ---- real-system layers: each fails when no test matches, so a missing check cannot pass ----

.PHONY: drift e2e eval rehearse soak record
drift: ## L3b: live systems against the recorded fixtures (read-only)
	bash scripts/run-tagged.sh drift '^TestDrift' "drift"

e2e: ## L4/L5 for one task: make e2e T=T24 (a task without TestTask_T24 fails)
	@test -n "$(T)" || { echo "usage: make e2e T=T24" >&2; exit 2; }
	@# TODO(T19): refuse to start when the run would pass the budget; until then run `make cost` yourself before an LLM task
	bash scripts/run-tagged.sh e2e '^TestTask_$(T)$$' "e2e $(T)"

eval: ## L6: make eval SUITE=injection
	@test -n "$(SUITE)" || { echo "usage: make eval SUITE=injection" >&2; exit 2; }
	bash scripts/run-tagged.sh eval '^TestEval_$(SUITE)$$' "eval $(SUITE)"

rehearse: ## L7: the full demo story
	bash scripts/run-tagged.sh rehearse '^TestRehearse' "rehearse"

soak: ## L7: the 2-hour soak with scripted agents
	bash scripts/run-tagged.sh soak '^TestSoak' "soak"

record: ## re-record real payloads for a task into testdata/real: make record T=T24
	@test -n "$(T)" || { echo "usage: make record T=T24" >&2; exit 2; }
	HEIMDALL_RECORD=$(CURDIR)/testdata/real bash scripts/run-tagged.sh e2e '^TestTask_$(T)$$' "record $(T)"

.PHONY: cost fixtures-push prototype
cost: ## remaining LLM budget per purpose (T19)
	@echo "make cost: the budget guard is not built yet (T19). No live model run is allowed until it is." >&2; exit 1

fixtures-push: ## publish testdata/repos/pay-demo to the fixture repo (T31)
	@echo "make fixtures-push: testdata/repos/pay-demo does not exist yet (T31)." >&2; exit 1

# ---- toolchain ----

.PHONY: tools ci
tools: ## prove the toolchain: Go, each pinned tool built and run, and Docker
	go version
	$(TOOL) gotestsum --version
	$(TOOL) golangci-lint --version | head -1
	$(TOOL) staticcheck -version
	$(TOOL) govulncheck -version | head -3
	$(TOOL) oasdiff --version
	@printf 'gitleaks (pinned): '; go list -m -modfile=tools.mod -f '{{.Version}}' github.com/zricethezav/gitleaks/v8
	$(TOOL) gitleaks version >/dev/null
	docker run --rm hello-world | grep -m1 'Hello from Docker'

ci: verify ## what the CI workflow runs
