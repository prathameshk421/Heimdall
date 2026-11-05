// Package people maps the senders of commands to the people in heimdall.yaml, and decides whether a sender may
// make a decision on a change.
//
// # Purpose
//
// A human decides in the change room with `heimdall uphold F3`, or on a PR with a GitHub review. This package
// answers two questions about that decision: who sent it, and whether that person may make it now. It does not
// read the chat, post replies or write events. The caller does those, through the parser in app/commands and
// the event store.
//
// # API
//
//   - New(policy.Policy) (*Directory, error): builds the lookup from the people table, the approver groups and
//     the policy owners. It refuses a policy that does not validate, and a sender id that two people share.
//   - Directory.Resolve(Channel, sender) (Identity, error): the person or agent behind a sender id (chat) or a
//     GitHub login. Its Kind says whether the sender is a human or an agent.
//   - Directory.HumanName(Channel, sender) (string, bool): the parser's lookup. It is true only for a human.
//   - Directory.Authorize(Attempt, Change) (Grant, error): allows a decision or returns a refusal wrapping one of
//     the Err sentinels. The rule for each action is one table, in rules.go.
//   - ApplyMapping(policy.Policy, Mapping) (policy.Policy, error): adds a sender id, a GitHub login, an agent name
//     or roles to one person. It returns a new policy and never changes the one it is given. `heimdall connect
//     people` uses it.
//
// # Invariants
//
//   - Agents never decide (TECH_SPEC 3.4, invariant 1). An agent sender resolves to KindAgent and every action
//     refuses it. Only the mapping counts: sender_kind cannot tell people from agents (D140), and a sender id
//     that maps to no person is refused as unknown.
//   - A chat sender id is matched whole and exactly, with no trimming or case folding (D140). A GitHub login is
//     matched without regard to case, as GitHub does.
//   - Roles come from the policy, never from a field a command can set. Approver means a member of one approver
//     group. Policy owner means a name in policy_owners. Author means the change's GitHub login.
//   - A decision that names a head SHA counts only when the SHA equals the current head (TECH_SPEC 3.4,
//     invariant 2). An approval must name one. A chat command names none unless the caller supplies it.
//   - An author never approves their own change (TECH_SPEC 12.4, login != author).
//   - Every refusal is an error that errors.Is matches. A refusal never changes state.
//
// # Failure modes
//
//   - A nil or empty sender, an unknown channel, or a sender that maps to no person is ErrUnknownSender.
//   - A sender that is an agent name is ErrAgentSender, whatever the action.
//   - A sender that holds none of the roles an action needs is ErrWrongPerson.
//   - An approval by the author is ErrSelfApproval. A decision with a SHA that is not the head is ErrStaleSHA.
//     An approval with no SHA is ErrUnboundDecision.
//   - A head SHA that is not a git hash is an error that is not a refusal. The caller must fix its input.
//   - New and ApplyMapping refuse duplicates with ErrDuplicate, and an agent name that equals a sender id with
//     ErrAmbiguous. Neither builds a partial directory.
//
// # Tests
//
// people_test.go covers the lookup: exact and case-folded matches, agents, unknown senders and the build-time
// refusals. authorize_test.go has one subtest per row of the rule table, and one test per refusal. mapping_test.go
// covers each refusal of ApplyMapping. The fixture is the spec example in internal/domain/policy/testdata, which
// validates. `make fast` runs them.
package people
