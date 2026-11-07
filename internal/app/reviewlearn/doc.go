// Package reviewlearn turns the stored history of changes into what reviewers can learn from. It reads every finding
// with its final state, decides if the finding was useful, noise, ignored or neutral, and counts the outcomes per agent
// and per rule card. It also lists prior findings for a repository and proposes new rule cards from human decisions.
//
// Traps:
//   - A proposal comes only from an event whose actor is a mapped human. An agent or a chat bridge never creates one,
//     so a model cannot write its own rules (memory poisoning gate).
//   - A proposal has status proposed. It does nothing until its owner accepts it.
//   - The snapshot is cached for a time to live. It is a read model, not a source of truth.
package reviewlearn
