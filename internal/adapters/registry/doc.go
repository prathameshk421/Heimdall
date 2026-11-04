// Package registry names the adapters, says what each one needs, and checks that heimdall.toml selects
// only names that exist.
//
// # Purpose
//
// Every external system sits behind a port (VCS, Tracker, Chat, AgentRuntime, Memory). An adapter is
// selected by name in heimdall.toml, so adding a platform means one adapter package and one Entry in the
// list the wiring passes to Default. Nothing in the domain or app layers changes. The package has no vendor
// imports: it only knows names, kinds, required config and the two functions an entry carries.
//
// # API
//
//   - Kind: vcs, tracker, chat, agent_runtime, memory. Kinds() lists them in a fixed order. Kind.Valid.
//   - Entry{Name, Kind, RequiredConfig, Factory, Check}. Factory(cfg) (any, error) builds the adapter; the
//     wiring asserts the value to the port for its kind. Check(ctx, cfg) CheckResult is read-only.
//   - Registry: a value, not package state. Zero value ready. Register refuses an invalid or duplicate
//     entry. Select(kind, name) returns the entry, or a *SelectError that lists the valid names for the
//     kind. Names(kind) and Describe() are sorted (kinds in Kinds() order, names sorted within a kind),
//     so `heimdall connect` prints the same output every time.
//   - Default(list): the one constructor. The wiring step calls it with the real adapter list. Tests build
//     their own. No init() registers anything.
//   - CheckResult{Status, Detail}. Status is connected, needs_config or unreachable.
//   - ValidateSelections(cfg, reg): checks each provider selection in the config against the registry and
//     joins every problem, so the server refuses to start on a typo and names the valid options.
//
// # Invariants
//
//   - Names match ^[a-z0-9][a-z0-9_.-]*$. Kinds are one of the five. Factory and Check are not nil.
//     RequiredConfig keys match the same pattern, contain no "..", and appear once each.
//   - Names are unique per kind. The same name may exist under two kinds.
//   - Detail never contains a secret value. Check may name a missing secret file, never its contents. The
//     registry cannot enforce this; the test helper requireNoSecret checks each stub.
//
// # Failure modes
//
//   - Invalid entry, duplicate, unknown kind: ErrInvalidEntry, ErrDuplicate, ErrUnknownKind (errors.Is).
//   - Unknown name: ErrUnknownName, and errors.As *SelectError gives the sorted valid names.
//   - A selection with no registered adapter: ValidateSelections returns an error naming the key
//     (vcs.provider, chat.bridge, ...), the value, and the valid names.
//
// # Tests
//
// registry_test.go holds table tests for every refusal, duplicates, unknown names and kinds, the
// deterministic Describe order, the secret-leak helper, ValidateSelections, and a stub adapter that is
// selected by name through config.Load with no change outside its own test file.
package registry
