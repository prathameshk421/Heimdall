// Package coverage reads Cobertura XML coverage reports into the domain coverage Result.
//
// # Purpose
//
// The fast check's diff-coverage clause (ACTION_MODE section 5.1) reads the coverage artifact of the
// customer's test workflow. LCOV and Go cover profiles are plain text and the domain package reads them.
// Cobertura is XML, and decoding an untrusted XML artifact is boundary work, so it lives here, on the
// standard library's encoding/xml, instead of in a hand-written tokenizer inside the domain. This is the
// only package that imports encoding/xml.
//
// # API
//
//   - ParseCobertura(data, opt) reads one report into a domaincoverage.Result. opt is the domain Options:
//     the caller's byte limit and an optional path prefix.
//
// # Invariants
//
//   - The result keeps every invariant of the domain package, because every line goes through a
//     domaincoverage.Builder: paths are forward-slash and relative, the prefix is removed once, a repeated
//     line adds its hits, and MaxLine, maxHits and MaxLines bound the values. This package adds none of
//     its own.
//   - The size limit is checked before the decoder reads a byte.
//   - The decoder is strict and streams tokens, so memory stays proportional to the document depth and
//     not to its size. Element nesting is limited to maxDepth.
//   - encoding/xml does not expand a document type entity and does not fetch an external subset or
//     entity. A declaration is still not trusted: the reader accepts one plain external DOCTYPE before the
//     root, which is what Java Cobertura writes (not checked on a real Java report; coverage.py 4.5.4, 5.5
//     and 7.17.4 write none), and refuses a DOCTYPE with an internal
//     subset, an ENTITY or any other declaration, a declaration inside or after the root, and a second
//     DOCTYPE. The five XML entities and numeric references decode; any other entity is a syntax error.
//   - The root element is coverage. Text outside the root, a second root element and a declared
//     character set the decoder has no reader for are refused.
//
// # Failure modes
//
//   - Malformed input returns domaincoverage.ErrMalformed with the source line, for example
//     "line 3: line number 0 is not in 1..10000000: malformed coverage report". A syntax error carries
//     the decoder's own line.
//   - Input over Options.MaxBytes, or a report with more than MaxLines instrumented lines, returns
//     domaincoverage.ErrTooLarge. A non-positive MaxBytes and a prefix that leaves the root are caller
//     mistakes and return a plain error. Callers branch with errors.Is.
//   - A complete report with no class is valid: it has no instrumented lines. A missing coverage element
//     is refused.
//
// # Tests
//
//   - cobertura_test.go: a golden report in the shape of coverage.py with a DOCTYPE, the refusal table with its
//     expected line number, the declaration table, the nesting and size limits, entities, namespaces,
//     merged lines, and FuzzParseCobertura, which must never panic and must return only the two error
//     kinds with a Result that holds the domain invariants.
//   - real_test.go: two real coverage.py reports (attrs, more-itertools) in testdata/real. Parser totals equal
//     the lines-valid and lines-covered the tool wrote, diffs built from the projects' own git history, a
//     wrong prefix that makes the report not measurable, and the same report with absolute, ./ and Windows
//     paths. docs/product/research/12-coverage-real-reports.md has the commands and results.
//
// Layout and dependency rule: docs/TECH_SPEC.md section 2.2, enforced by internal/archcheck.
package coverage
