# Heimdall build commands. See docs/harness/AGENTS.md for the loop that uses them.
# Tools are pinned in tools.mod and run with `go tool -modfile=tools.mod <name>` so they stay out of the product module.
SHELL := /usr/bin/env bash
.SHELLFLAGS := -eu -o pipefail -c
export PATH := $(HOME)/.local/go/bin:$(HOME)/go/bin:$(PATH)

TOOL := go tool -modfile=tools.mod

.PHONY: tools
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

.PHONY: arch vet test vuln ci
arch: ## the dependency rule and the test-code rule on the real import graph
	go run ./cmd/archcheck

vet:
	go vet ./...

test: ## all tests with the race detector
	go test -race -count=1 ./...

vuln: ## known vulnerabilities in the product module
	$(TOOL) govulncheck ./...

ci: arch vet test vuln ## what the CI workflow runs at T10; grows with T16 and T18
