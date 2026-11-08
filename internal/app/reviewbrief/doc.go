// Package reviewbrief compiles what a reviewer knows before it reads the diff: the rule cards it must answer, a map of
// the code the change reaches, what past findings on these files turned into, what team memory says, and how this
// reviewer's own findings have fared.
//
// # Purpose
//
// A cheap model with no file access finds more when the server does the search first. The compiler runs inside
// get_task, so it must stay fast and it must never cost the reviewer its task.
//
// # API
//
//   - New(Config) returns a Compiler. Every source except the logger may be nil, which turns its section off.
//   - Compiler.Compile(ctx, Request) returns a Brief. It has no error: each section reports its own failure.
//
// # Invariants
//
//   - Each section fails alone. A failed source gives a one-line note in its section, is logged, and sets
//     Brief.Partial so the caller does not keep the brief for reuse.
//   - The graph has about 20 s and learning and memory about 8 s each. The sources honour the context deadline.
//   - Only a reviewer gets obligations, memory and a record. A resolver gets the change map and prior findings.
//   - Brief.ObligationCards names exactly the cards that the Obligations text shows.
//   - A card names a place to look. It never grants an evidence tier.
//   - A card is POSSIBLY STALE only when it names symbols, none is defined at head, and the index is complete. A
//     truncated index cannot prove that a definition is absent.
//
// # Design note
//
// internal/app packages may import only domain and ports (internal/archcheck), so this package cannot name
// codegraph or reviewlearn. It declares the small views it needs, and cmd/heimdall-server adapts those packages to
// them.
//
// # Tests
//
// reviewbrief_test.go covers section isolation, deadlines, the stale flag, ordering and the rendering caps with fakes.
package reviewbrief
