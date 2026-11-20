# Preamble for every Heimdall delegate

Read this first, then `CLAUDE.md` (the repo rules R1 to R13), then your work package in `docs/product/ROADMAP.md`.

1. You work in your own git worktree on your own branch. Commit there with the message `P<wp>: what` (for example
   `P1.7: any OpenAI-compatible model endpoint`). Never push. Never commit on `heimdall-build` or `main`.
2. Never add a `Co-Authored-By` line or any other trailer to a commit. Owner rule.
3. On this Mac every Go command needs `CGO_ENABLED=0` (for example `CGO_ENABLED=0 make fast`). Docker may be off, so
   `make verify` may not run: then say `not verified` with the reason and run the focused package tests instead.
4. Touch only the files your work package names, plus their tests and `doc.go`. Anything else you notice goes in your
   final message under "Noticed". Do not refactor other code.
5. Tests first for real logic (state, gates, parsing, money, security). No tests for wording, config plumbing or glue.
   Never weaken, skip or delete a check (R2). Edits to `.golangci.yml`, `e2e/scenarios`, `testdata/real` or
   `CLAUDE.md` are forbidden for you.
6. Never read, print or copy `.env`, `secrets/`, tokens, private keys or any `opencode.json` (R8).
7. No live model or GitHub writes unless your brief says so. Anything you could not run is written `not verified`.
8. Prose (docs, comments, commit messages) in plain short sentences, active voice, no em dashes.
9. Final message: branch name, commit SHAs, the commands you ran with exit status and key output lines, what is
   `not verified`, and "Noticed".
