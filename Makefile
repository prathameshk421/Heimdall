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
