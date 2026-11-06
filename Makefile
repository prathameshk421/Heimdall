# Heimdall build commands. See docs/harness/AGENTS.md for the loop that uses them.
# Tools are pinned in tools.mod and run with `go tool -modfile=tools.mod <name>` so they stay out of the product module.
SHELL := /usr/bin/env bash
.SHELLFLAGS := -eu -o pipefail -c
export PATH := $(HOME)/.local/go/bin:$(HOME)/go/bin:$(PATH)

TOOL := go tool -modfile=tools.mod
# Every build tag the repo uses; vet and lint see files behind them (keep in step with run.build-tags in .golangci.yml).
TAGS := e2e,component,drift,eval,soak,rehearse
GOFILES = find . -name '*.go' -not -path './testdata/*' -not -path './.git/*'

.DEFAULT_GOAL := help
.PHONY: help
help: ## list the targets
	@grep -hE '^[a-zA-Z0-9_-]+:.*## ' $(MAKEFILE_LIST) | sed -E 's/:.*## /\t/' | sort

# ---- the loop: fast on every change, verify before a commit that ends a task ----

.PHONY: fast verify
fast: fmt-check vet arch lint unit ## L0 and L1: format, vet, dependency rule, lint, unit tests. Under 60 s; runs in the pre-commit hook
verify: fast race lint-selftest harness-test harness-check scripts-test vuln secrets ## adds L2 and L3: race and component tests, rule self-tests, vulnerabilities, secrets. Before a task's commit; CI runs this

.PHONY: fmt-check vet arch lint unit race
fmt-check:
	@bad=$$($(GOFILES) | xargs gofmt -l); if [ -n "$$bad" ]; then echo "gofmt needed:" >&2; echo "$$bad" >&2; exit 1; fi

vet:
	go vet -tags $(TAGS) ./...

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

.PHONY: lint-selftest harness-test harness-install harness-check scripts-test hooks-install
lint-selftest: ## every lint rule still fires: bad fixtures flagged, good ones clean
	go test -tags lintselftest -count=1 ./internal/lintselftest/

harness-test: ## every hook case, and the harness installer
	bash docs/harness/test-hooks.sh
	bash docs/harness/test-install.sh

harness-install: ## copy docs/harness/ into place; CHECK=1 changes nothing and fails on drift
	bash scripts/harness-install.sh

harness-check: ## the installed hooks, settings, AGENTS.md and lint config match docs/harness (drift fails)
	CHECK=1 bash scripts/harness-install.sh

scripts-test: ## tests of the make helper scripts
	bash scripts/test-scripts.sh

hooks-install: ## install the git pre-commit hook (make fast)
	bash scripts/hooks-install.sh

# ---- supply chain ----

.PHONY: vuln secrets secrets-staged
vuln: ## known vulnerabilities in the product module
	$(TOOL) govulncheck ./...

secrets: ## gitleaks over the git history (never reads the working tree, so never .env)
	$(TOOL) gitleaks git --no-banner --redact .

secrets-staged: ## gitleaks over the staged diff, so a secret is stopped before it becomes history (pre-commit)
	$(TOOL) gitleaks git --staged --no-banner --redact .

# ---- real-system layers: each fails when no test matches, so a missing check cannot pass ----

.PHONY: drift e2e eval rehearse soak record
drift: ## L3b: live systems against the recorded fixtures (read-only)
	bash scripts/run-tagged.sh drift '^TestDrift' "drift"

e2e: ## L4/L5 for one task: make e2e T=T24 (a task without TestTask_T24 fails)
	@test -n "$(T)" || { echo "usage: make e2e T=T24" >&2; exit 2; }
	@if [ "$(AGENTS)" = live ]; then \
		test -n "$(PURPOSE)" || { echo "make e2e: PURPOSE is required for live runs" >&2; exit 2; }; \
		test -n "$(ESTIMATE)" || { echo "make e2e: ESTIMATE is required for live runs" >&2; exit 2; }; \
		go run ./cmd/heimdall budget-check --purpose "$(PURPOSE)" --estimate "$(ESTIMATE)" $(if $(CONFIG),--config "$(CONFIG)",) $(if $(LEDGER),--ledger "$(LEDGER)",); \
		go run ./cmd/heimdall budget-run --purpose "$(PURPOSE)" --estimate "$(ESTIMATE)" --task "$(T)" --model "$(if $(MODEL),$(MODEL),openrouter/deepseek/deepseek-v4-flash)" $(if $(CONFIG),--config "$(CONFIG)",) $(if $(LEDGER),--ledger "$(LEDGER)",) -- bash scripts/run-tagged.sh e2e '^TestTask_$(T)$$' "e2e $(T)"; \
	fi
	@if [ "$(AGENTS)" != live ]; then bash scripts/run-tagged.sh e2e '^TestTask_$(T)$$' "e2e $(T)"; fi

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

.PHONY: cost fixtures-push prototype demo-check heimdall-bin deploy deploy-build release-sign upgrade setup restart recover
cost: ## remaining LLM budget per purpose (T19)
	go run ./cmd/heimdall cost $(if $(CONFIG),--config "$(CONFIG)",) $(if $(LEDGER),--ledger "$(LEDGER)",)

deploy: ## deploy an exact signed tag through the SSM-backed SSH alias
	DEPLOY_CONFIG="$(CONFIG)" DEPLOY_SECRETS_DIR="$(SECRETS_DIR)" bash scripts/deploy.sh

deploy-build: ## cross-build deterministic Linux release artifacts without a host connection
	DEPLOY_BUILD_ONLY=1 bash scripts/deploy.sh

release-sign: ## sign a tag-named release archive with cosign
	@test -n "$(TAG)" || { echo "usage: make release-sign TAG=vX RELEASE_SIGNING_KEY=<key-path-or-KMS-ref>" >&2; exit 2; }
	RELEASE_TAG='$(TAG)' RELEASE_SIGNING_KEY='$(RELEASE_SIGNING_KEY)' bash scripts/sign-release.sh

upgrade: ## install an exact tagged release through the SSM-backed deploy workflow
	DEPLOY_CONFIG="$(CONFIG)" DEPLOY_SECRETS_DIR="$(SECRETS_DIR)" bash scripts/deploy.sh

setup: ## create a private local operator workspace
	bash scripts/setup.sh

restart: ## restart the deployed service and verify health and authenticated MCP
	bash scripts/operate.sh restart

recover: ## reconcile the deployed stack and verify health and authenticated MCP
	bash scripts/operate.sh recover

fixtures-push: ## publish testdata/repos/pay-demo to the fixture repo (T31)
	@echo "make fixtures-push: testdata/repos/pay-demo does not exist yet (T31)." >&2; exit 1

# ---- the product board (T98): the progress bar for the whole project ----

heimdall-bin: ## build the command-line binary the board's CLI steps run
	@mkdir -p .cache
	go build -o .cache/heimdall ./cmd/heimdall

prototype: heimdall-bin ## the board: make prototype [STORY=fixture-pay] [AGENTS=scripted|live] [ONLY=P3] [JSON=1] [TASK=T##]
	go run ./e2e/cmd/prototype

demo-check: heimdall-bin ## the demo script as a test: every beat of the three-minute video, in order (MVP_SCENARIOS section 6)
	go run ./e2e/cmd/prototype -demo -strict -no-history

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
