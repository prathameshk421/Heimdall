# Real Cobertura reports

These files are the output of coverage.py 7.17.4 (`coverage xml`) on open-source projects at a pinned release,
run with Python 3.14.5. `real_test.go` reads them. `docs/product/research/12-coverage-real-reports.md` has the
full commands, the tool summaries and the results. Every project was cloned into its own empty directory
outside this repository.

| File | Project | Tag | Commit | Command |
|---|---|---|---|---|
| `attrs-26.1.0.coveragepy.xml` | github.com/python-attrs/attrs | 26.1.0 | `7bfc49e9b22d5ba25b6e429524c3d49fee27cb36` | `coverage run -m pytest`, `coverage combine`, `coverage xml` (the project's own config turns on branch coverage) |
| `more-itertools-v11.2.1.coveragepy.xml` | github.com/more-itertools/more-itertools | v11.2.1 | `4aeb4338ae1407d8da9723e86da99efeb446f719` | `coverage run --source=more_itertools -m pytest`, `coverage xml` |

One edit: the `<source>` element named the scratch directory of the person who ran the tool. It now names
`/home/runner/work/<name>/<name>`. The parser reads no `<source>`.

`changed/*.json` is the same kind of file as in `internal/domain/coverage/testdata/real/changed`: the lines a
range of the project's own commits changed, from `git diff --no-renames --unified=0 <base> HEAD`.

coverage.py writes `filename` relative to the directory it ran in, for example `src/attr/_make.py`. That equals
the repository path, so the reports need no `Options.Prefix`.
