# Real coverage reports (Go cover and LCOV)

These files are the unedited output of real tools run on open-source projects at a pinned release.
`real_test.go` reads them. `docs/product/research/12-coverage-real-reports.md` has the full commands, the
tool summaries and the results. Every project was cloned into its own empty directory outside this repository.

| File | Project | Tag | Commit | Command |
|---|---|---|---|---|
| `uuid-v1.6.0.set.cover.out` | github.com/google/uuid | v1.6.0 | `0f11ee6918f41a04c201eceeadf612a377bc7fbc` | `go test -coverprofile=cover.out ./...` |
| `cobra-v1.10.2.atomic.cover.out` | github.com/spf13/cobra | v1.10.2 | `88b30ab89da2d0d0abb153818746c5a2d30eccec` | `go test -covermode=atomic -coverprofile=cover.out ./...` |
| `commander-v15.1.0.c8.lcov.info` | github.com/tj/commander.js | v15.1.0 | `a5a7dc084d4d2b7932e85ca049c5204e50a2c721` | `c8 --reporter=lcov node --test` (c8 12.0.0, Node 26.13.1) |
| `ms-3.0.0-canary.1.jest.lcov.info` | github.com/vercel/ms | 3.0.0-canary.1 | `1304f150b38027e0818cc122106b5c7322d68d0c` | `jest --coverage --coverageReporters=lcov` (Jest 27.1.1) |
| `attrs-26.1.0.coveragepy.lcov.info` | github.com/python-attrs/attrs | 26.1.0 | `7bfc49e9b22d5ba25b6e429524c3d49fee27cb36` | `coverage run -m pytest`, `coverage combine`, `coverage lcov` (coverage.py 7.17.4, Python 3.14.5) |
| `more-itertools-v11.2.1.coveragepy.lcov.info` | github.com/more-itertools/more-itertools | v11.2.1 | `4aeb4338ae1407d8da9723e86da99efeb446f719` | `coverage run --source=more_itertools -m pytest`, `coverage lcov` |

`changed/*.json` maps a repository-relative source path to the lines a range of the project's own commits
changed on the new side. It comes from `git diff --no-renames --unified=0 <base> HEAD` on the tagged tree,
with test files, docs and type stubs left out. Each file name says the window: `-last` is the last commit that
changed source, and the other names reach back over more source commits. The research note lists every base.

The Go profiles carry the module path (`github.com/google/uuid/dce.go`), so they need `Options.Prefix` set to
the module path. The LCOV files carry paths relative to the directory the tool ran in, which equal the
repository paths, so they need no prefix.
