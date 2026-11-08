// Package codegraph gives a reviewer agent the code navigation it lacks. The agent has no file access and only an
// exact-path read tool, so it cannot ask who calls a function or whether a test touches it. This package builds a
// small index of one commit from its tarball and answers those questions.
//
// How it works. Each file is tagged for definitions (functions, methods, types, constants) and references
// (identifiers in use). Go files use go/parser. Other languages use line regexes, which are rough but need no
// toolchain. A reference matches a definition by name only. The package never resolves types or imports, so a match
// can be wrong: two packages may define the same name. Every output says "name match only" so the reader checks the
// code before it cites a caller.
//
// The file graph links a file that uses a name to each file that defines it. A name defined in many files links
// nothing, because it says little. Personalized PageRank restarts on the changed files. The walk goes along reversed
// edges to find the files that depend on the change, and along forward edges to find what the change uses. Reversed
// is the useful direction: the risk of a change sits in the code that calls it, not in the code it calls.
//
// Limits keep a large repository from using all memory. See the constants in build.go. When a limit stops the
// build, the index reports Truncated and works on the files it has.
package codegraph
