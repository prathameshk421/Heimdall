# Contributing to Heimdall

Thank you for your time. Heimdall is a merge gate, so we hold its own code to the standard it enforces: every change
comes with proof, and no check gets weaker to make a build pass.

This guide covers how to report a problem, how to set up the project, the rules the code follows, and what a good pull
request looks like.

The product page is at **https://heimdall-sage.vercel.app**: what it does, a worked proof, and a two-minute demo.

## Ways to contribute

| You want to | Do this |
|---|---|
| Report a bug | Open a [bug report](https://github.com/prathameshk421/Heimdall/issues/new?template=bug_report.yml). Include the command, the output and the commit SHA |
| Ask for a feature | Open a [feature request](https://github.com/prathameshk421/Heimdall/issues/new?template=feature_request.yml). Describe the problem before the solution |
| Report a vulnerability | Do **not** open an issue. Follow [`SECURITY.md`](SECURITY.md) |
| Add a Proof Bench case | Open a [bench case](https://github.com/prathameshk421/Heimdall/issues/new?template=bench_case.yml) issue, or send a PR (see [below](#add-a-proof-bench-case)) |
| Fix a bug or build a feature | Comment on the issue first, so two people do not build the same thing. Then send a PR |
| Improve the docs | Send a PR. Small fixes need no issue |

Good first areas:

- **Proof Bench cases.** Real bugs and clean changes with known answers, in any language.
- **Review bot parsers.** Read the comments of a bot that Heimdall does not parse yet.
- **Test runners.** Run the tests of more languages and frameworks in the sandbox.

Issues with the [`good first issue`](https://github.com/prathameshk421/Heimdall/labels/good%20first%20issue) label are a
good start.

## Set up the project

You need:

- **Go 1.27** (the exact toolchain is in [`go.mod`](go.mod); `go` downloads it for you).
- **Docker**, for component tests that start a real PostgreSQL.
- **GNU Make** and **Bash**.

```bash
git clone https://github.com/prathameshk421/Heimdall.git && cd Heimdall
```

```bash
make tools
```

```bash
make hooks-install
```

`make tools` builds and runs each pinned tool once, so you know the toolchain works. All tools are pinned in
[`tools.mod`](tools.mod) and run with `go tool`. You install nothing globally. `make hooks-install` adds a pre-commit
hook that runs `make fast` and a secret scan of the staged diff.

Build with `CGO_ENABLED=0`. Heimdall ships static binaries and the code needs no C toolchain.

## The checks

| Command | What it runs | When |
|---|---|---|
| `make fast` | Format, vet, the dependency rule, lint, unit tests. Under 60 s | After every change |
| `make verify` | `fast`, plus the race detector, component tests, lint self-tests, harness tests, `govulncheck` and a `gitleaks` scan of the git history | Before you open a PR. CI runs this |
| `make e2e T=T24` | The real-system check for one task | Only when your change needs it. It can cost money (run `make cost` first) |
| `make help` | Lists every target | Any time |

Your PR must pass `make verify`. CI runs it on every push and pull request.

## How the code is organized

Heimdall uses a ports-and-adapters layout. A lint check (`make arch`) enforces the dependency rule on the real import
graph.

```
cmd/            binaries: wiring only
internal/
  domain/       pure rules and types. No I/O, no clock, no randomness, no logging
  ports/        small interfaces, defined by the consumer
  app/          use cases. Imports domain and ports only
  adapters/     GitHub, Jira, Switch, PostgreSQL, model APIs, sandbox runners. Implement ports
  transport/    HTTP, MCP, webhooks, dashboard. Calls app
  platform/     config, logging, the only place that reads the environment
```

- `domain` imports nothing internal. `app` imports `domain` and `ports`. Adapters implement ports. `transport` calls
  `app`.
- Each package has a `doc.go` with its purpose, public API, invariants and known traps. Read it before you change the
  package. A new package starts from [`docs/harness/doc.go.template`](docs/harness/doc.go.template).

The full rules are in [`docs/CODING_STANDARDS.md`](docs/CODING_STANDARDS.md). The most important ones:

- **Errors.** Wrap with context (`fmt.Errorf("load change %s: %w", id, err)`). Never swallow an error. Never panic
  outside `main` and tests. Use `errors.Is` and `errors.As` to branch.
- **No hidden state.** No package-level mutable state and no `init()` that does work.
- **Time and config.** No `time.Sleep` (use the clock port). No `fmt.Print*`. No `os.Getenv` outside
  `internal/platform/config`. Lint enforces all three.
- **Context.** Every function that does I/O takes `ctx context.Context` first, and every outbound call has a deadline.
- **No test code in production, and no speculative fallbacks.** If a path cannot happen, do not write code for it.

## Tests

We write tests where they pay:

- **Write the failing test first for real logic:** state machines, the gate, parsers, money and cost rules, security
  rules.
- **Skip new tests for wording, prompts, config and glue.**
- Tests reach the server only through its HTTP seam. Use table tests. Keep fakes small and local.
- A skipped test fails the run. A test that needs Docker uses the `component` build tag.

Test layers, doubles and the cost budget for model calls are in [`docs/TEST_STRATEGY.md`](docs/TEST_STRATEGY.md).

## Rules we never bend

- **Never weaken a check to get green.** Do not skip, delete or loosen a test, a lint rule, a timeout or a hook, and do
  not add `//nolint`. If you think a check is wrong, say why in the PR and we decide together.
- **Never commit a secret.** No tokens, keys or `.env` files. The pre-commit hook and CI both run `gitleaks`. Use made-up
  values in tests, and split them so they do not look real.
- **Say what you did not verify.** If a check could not run (no Docker, no live account), write `not verified` and the
  reason in the PR. Never write "should work".
- **One concern per PR.** Put anything else you notice in the PR description or a new issue.

## Add a Proof Bench case

A bench case is a real bug (or a clean change) with a known answer. A case is valid only when its test is **red before
the fix and green after it**, on two runs.

1. Pick a merged fix in a public repository with a permissive license.
2. Record the repository, the bug commit, the fix commit and the test that shows the bug.
3. Add the case to [`testdata/bench/cases.yaml`](testdata/bench/cases.yaml), next to the existing ones. The comment at
   the top of that file explains each field. The rules are in `internal/bench/doc.go`.
4. Prove the case real with the validator, and put the result in the PR:

   ```bash
   go run ./cmd/heimdall-bench validate -only your-case-id -out results.json
   ```

5. Clean cases (changes with no bug) are just as useful. They measure false positives.

## Commit messages

- Start with the area or task id and a colon: `localrunner: warm the Go cache before the timeout test`.
- Use the imperative mood and keep the subject under 72 characters.
- In the body, say **why**, and give the evidence: the command, its result, and what is not verified.

## Pull request checklist

- [ ] The PR does one thing and links its issue.
- [ ] Real logic has a test that failed before the change.
- [ ] `make verify` passes locally, or the PR says which part could not run and why.
- [ ] No check, lint rule, timeout or test was weakened.
- [ ] Docs and `doc.go` notes are updated if behavior changed.
- [ ] No secrets, personal data or generated binaries are in the diff.

A maintainer reviews each PR. We aim to reply within a week. Agent-written code is welcome, and it meets the same bar:
a person who can explain it signs off.

## Coding agents

If you use an AI coding agent, point it at [`AGENTS.md`](AGENTS.md) (also `CLAUDE.md`). It holds the same rules in a form
agents follow, plus the facts that the code alone does not show.

## Writing style for docs

Docs use short, plain sentences in the active voice, close to ASD-STE100 (Simplified Technical English). Use facts and
numbers instead of adjectives, and link each number to its source.

## License

Heimdall is licensed under the [MIT License](LICENSE). By contributing, you agree that your contributions are licensed
under the same license. You keep the copyright of your work.

## Code of Conduct

This project follows our [Code of Conduct](CODE_OF_CONDUCT.md). By taking part, you agree to follow it.
