# Hermetic seed repositories

These are the seed repositories for the e2e story `fixture-pay` and the sandbox tests (T31, D57, D116, D149).
They are test data. Nothing here is production code, and no service in this repository imports it.

## Layout

    pay-demo/base/                  the main tree: a small Go payments service, standard library only
    pay-demo/branches/<name>/       one branch: the files that change relative to its parent
    pay-demo/repro/                 repro tests for the planted findings (E1), applied by the fixture tests
    pay-demo/manifest.yaml          the planted findings: branch, file, line range, tier, finder, repro test
    ledger-demo/base/               the consumer of refund responses, a separate Go module (not vendored)

Branches in `pay-demo`:

| Branch | Parent | What it is |
|---|---|---|
| `pay-demo/refunds` | main | the good PR: refunds, with three planted defects (D57) |
| `pay-demo/refunds-fixed` | pay-demo/refunds | the same feature with the three defects fixed and regression tests added |
| `pay-demo/big-no-ticket` | main | the bad PR: about 1,370 changed lines, no tracker key anywhere |
| `pay-demo/injection` | main | the injection PR: request IDs, with injected instructions in code and PR body |

## How branches are built

`internal/testkit/fixtures` (`Materialize`) builds a real git repository in a temp dir. It commits `base` as `main`,
then creates each branch from its parent, applies the overlay files and deletes what `branch.yaml` lists. Every commit
has one fixed author and date, so commit and tree hashes are stable across builds.

A branch directory holds:

- `branch.yaml`: `parent`, `commit_message`, and `delete` (a list of paths to remove). Strictly decoded.
- `PR_BODY.md` (optional): the pull request body. It is never committed; the push tooling uses it as the body.
- every changed file, in full. Unchanged files are not repeated.

## Tests

`go test ./internal/testkit/fixtures/` checks that the base vets and passes its own tests; that the buggy branch
passes everything except the repro tests, and each repro fails on an assertion; that the fixed branch passes all of
them; that every manifest line range holds its defect; that the big branch is 1,300 to 1,500 changed lines with no
tracker key; that the canary appears once, only on the injection branch; that builds are identical; and that every
branch is gofmt-clean, vets and passes its own tests.

The fixtures are under `testdata`, so `./...` does not compile them and lint does not cover them. The fixture tests
run gofmt and vet on the materialised trees.

## Known gaps, on purpose

- `refund-ref-renamed` (eli, tier E2) has no repro test. It is proven by a contract check (`oasdiff`) and by INC-031 in
  the seed memory, not by a test that the fixture can run alone.
- The refund spec keeps `refund_ref` marked `deprecated: true` while the code no longer sends it. That is part of the
  same planted defect, and it matches the author's challenge in the story.

## Publishing

`make fixtures-push` (written later, not in this change) will publish each materialised repository to its GitHub
repository (`aarush323/heimdall-fixture-pay`), as branches under `heimdall-test/<run-id>` with base `heimdall-demo`.
It will read `PR_BODY.md` for the pull request body. Nothing in this directory touches the network.
