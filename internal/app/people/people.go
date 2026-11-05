package people

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
)

// Action is what a sender asks Heimdall to do. The thirteen grammar commands use the domain's decision kinds.
type Action = change.DecisionKind

// ActionApprove is a GitHub review approval of a commit. It is recorded as approval.recorded, not as a decision.
const ActionApprove Action = "approve"

// Channel is where a sender's identity comes from. Its values match the people map keys "chat" and "github".
type Channel string

// The two channels a sender can come from.
const (
	ChannelChat   Channel = "chat"
	ChannelGitHub Channel = "github"
)

// Kind says whether a sender is a person or an agent.
type Kind string

// The sender kinds.
const (
	KindHuman Kind = "human"
	KindAgent Kind = "agent"
)

// Role is a role a person holds. Member, approver and policy owner come from the policy; author is per change.
type Role string

// The roles. A mapping may set member, author and approver. Policy owners are listed under policy_owners.
const (
	RoleMember      Role = "member"
	RoleAuthor      Role = "author"
	RoleApprover    Role = "approver"
	RolePolicyOwner Role = "policy_owner"
)

// Sentinel errors. Callers branch on them with errors.Is.
var (
	ErrUnknownSender   = errors.New("sender is not a mapped person")
	ErrAgentSender     = errors.New("an agent never decides")
	ErrWrongPerson     = errors.New("sender holds no role this action needs")
	ErrSelfApproval    = errors.New("the author may not approve their own change")
	ErrStaleSHA        = errors.New("decision names a head that is no longer current")
	ErrUnboundDecision = errors.New("approval names no head SHA")
	ErrUnknownAction   = errors.New("unknown action")
	ErrDuplicate       = errors.New("already mapped to another person")
	ErrAmbiguous       = errors.New("sender id is also an agent name")
	ErrAlreadySet      = errors.New("person already has a different value here")
	ErrUnknownRole     = errors.New("unknown role")
	ErrGitHubRequired  = errors.New("authors and approvers need a github login (D158)")
	ErrNotApprover     = errors.New("approver role needs a group in approvers")
)

// Identity is who a sender is.
type Identity struct {
	Kind   Kind
	Name   string
	GitHub string
	Agent  string
	Roles  []Role
	Groups []string
}

// Directory maps senders to people and agents. Build it with New.
type Directory struct {
	persons map[string]person
	chat    map[string]string
	logins  map[string]string
	agents  map[string]string
}

// person is what the directory keeps about one mapped person.
type person struct {
	github string
	agent  string
	groups []string
	owner  bool
}

// New builds the directory from a valid policy. A duplicate sender id, an agent name equal to a sender id, or a
// sender id that is not printable ASCII is an error, so no partial directory is returned.
func New(p policy.Policy) (*Directory, error) {
	if err := p.Validate(); err != nil {
		return nil, fmt.Errorf("people: policy is invalid: %w", err)
	}
	d := &Directory{
		persons: map[string]person{},
		chat:    map[string]string{},
		logins:  map[string]string{},
		agents:  map[string]string{},
	}
	for _, name := range slices.Sorted(maps.Keys(p.People)) {
		if err := d.add(name, p.People[name]); err != nil {
			return nil, err
		}
	}
	d.addGroups(p)
	for sid := range d.chat {
		if _, clash := d.agents[sid]; clash {
			return nil, fmt.Errorf("people: sender id %q is also an agent name: %w", sid, ErrAmbiguous)
		}
	}
	return d, nil
}

// add records one person's sender id, login and agent, and refuses one that another person already holds.
func (d *Directory) add(name string, per policy.Person) error {
	d.persons[name] = person{github: per.GitHub, agent: per.Agent}
	if per.Chat != "" {
		sid, err := change.ParseSenderID(per.Chat)
		if err != nil {
			return fmt.Errorf("people.%s.chat: %w", name, err)
		}
		if other, dup := d.chat[string(sid)]; dup {
			return fmt.Errorf("people.%s.chat: already the sender id of people.%s: %w", name, other, ErrDuplicate)
		}
		d.chat[string(sid)] = name
	}
	if per.GitHub != "" {
		key := strings.ToLower(per.GitHub)
		if other, dup := d.logins[key]; dup {
			return fmt.Errorf("people.%s.github: the login of people.%s: %w", name, other, ErrDuplicate)
		}
		d.logins[key] = name
	}
	if per.Agent != "" {
		if other, dup := d.agents[per.Agent]; dup {
			return fmt.Errorf("people.%s.agent: already the agent of people.%s: %w", name, other, ErrDuplicate)
		}
		d.agents[per.Agent] = name
	}
	return nil
}

// addGroups records the approver groups each person belongs to, and which people are policy owners.
func (d *Directory) addGroups(p policy.Policy) {
	for _, group := range slices.Sorted(maps.Keys(p.Approvers)) {
		for _, name := range p.Approvers[group] {
			pr, ok := d.persons[name]
			if !ok || slices.Contains(pr.groups, group) {
				continue
			}
			pr.groups = append(pr.groups, group)
			d.persons[name] = pr
		}
	}
	for _, name := range p.PolicyOwners {
		if pr, ok := d.persons[name]; ok {
			pr.owner = true
			d.persons[name] = pr
		}
	}
}

// Resolve returns who the sender is. A chat sender id is matched whole and exactly (D140). A GitHub login is matched
// without regard to case. An agent name is returned as KindAgent. Anything else is ErrUnknownSender.
func (d *Directory) Resolve(ch Channel, sender string) (Identity, error) {
	if sender != "" {
		switch ch {
		case ChannelChat:
			if name, ok := d.chat[sender]; ok {
				return d.human(name), nil
			}
			if _, ok := d.agents[sender]; ok {
				return Identity{Kind: KindAgent, Name: sender}, nil
			}
		case ChannelGitHub:
			if name, ok := d.logins[strings.ToLower(sender)]; ok {
				return d.human(name), nil
			}
		}
	}
	return Identity{}, fmt.Errorf("resolve sender on %s: %w", ch, ErrUnknownSender)
}

// HumanName returns the person's name for a human sender. It is false for an agent or an unknown sender.
func (d *Directory) HumanName(ch Channel, sender string) (string, bool) {
	ident, err := d.Resolve(ch, sender)
	if err != nil || ident.Kind != KindHuman {
		return "", false
	}
	return ident.Name, true
}

// human builds the identity of a mapped person. Roles are derived from the policy, so they cannot be set by a command.
func (d *Directory) human(name string) Identity {
	pr := d.persons[name]
	roles := []Role{RoleMember}
	if len(pr.groups) > 0 {
		roles = append(roles, RoleApprover)
	}
	if pr.owner {
		roles = append(roles, RolePolicyOwner)
	}
	return Identity{
		Kind:   KindHuman,
		Name:   name,
		GitHub: pr.github,
		Agent:  pr.agent,
		Roles:  roles,
		Groups: slices.Clone(pr.groups),
	}
}

// Attempt is one decision or approval a sender makes. SeenSHA is the head the sender saw. It is empty when the
// command names none, as chat commands do.
type Attempt struct {
	Channel Channel
	Sender  string
	Action  Action
	SeenSHA id.SHA
}

// Change is the state of a change that a decision is checked against. HeadSHA is the current head. AuthorLogin is the
// GitHub login of the PR author. FilingPerson is the person who filed the finding a dismiss names. DomainGroups are
// the approver groups that own the changed paths.
type Change struct {
	HeadSHA      id.SHA
	AuthorLogin  string
	FilingPerson string
	DomainGroups []string
}

// Grant is an allowed decision: the person who made it and the action.
type Grant struct {
	Person string
	Action Action
}

// Authorize allows the attempt, or returns an error wrapping one of the refusal sentinels. The checks run in this
// order: the sender, the action, the SHA, the author, then the role. The first failure is the reason.
func (d *Directory) Authorize(a Attempt, ch Change) (Grant, error) {
	if !ch.HeadSHA.Valid() {
		return Grant{}, fmt.Errorf("authorize %s: change head %q is not a git hash", a.Action, ch.HeadSHA)
	}
	ident, err := d.Resolve(a.Channel, a.Sender)
	if err != nil {
		return Grant{}, fmt.Errorf("authorize %s: %w", a.Action, err)
	}
	if ident.Kind == KindAgent {
		return Grant{}, fmt.Errorf("authorize %s: %w", a.Action, ErrAgentSender)
	}
	r, ok := rules()[a.Action]
	if !ok {
		return Grant{}, fmt.Errorf("authorize %q: %w", a.Action, ErrUnknownAction)
	}
	if err := checkHead(r, a.SeenSHA, ch.HeadSHA); err != nil {
		return Grant{}, fmt.Errorf("authorize %s: %w", a.Action, err)
	}
	if r.notAuthor && isAuthor(ident, ch) {
		return Grant{}, fmt.Errorf("authorize %s: %w", a.Action, ErrSelfApproval)
	}
	if !holds(r.needs, ident, ch) {
		return Grant{}, fmt.Errorf("authorize %s: %w", a.Action, ErrWrongPerson)
	}
	return Grant{Person: ident.Name, Action: a.Action}, nil
}

// checkHead refuses a decision that names a head other than the current one. A rule that binds the head also
// refuses a decision that names none.
func checkHead(r rule, seen, head id.SHA) error {
	if seen == "" {
		if r.bindHead {
			return ErrUnboundDecision
		}
		return nil
	}
	if seen != head {
		return ErrStaleSHA
	}
	return nil
}

// holds reports whether the identity meets any one of the requirements.
func holds(needs []requirement, ident Identity, ch Change) bool {
	for _, req := range needs {
		if satisfies(req, ident, ch) {
			return true
		}
	}
	return false
}

// satisfies reports whether the identity meets one requirement. Every requirement is listed here, so a new one
// cannot be added to the table without a case.
func satisfies(req requirement, ident Identity, ch Change) bool {
	switch req {
	case needMember:
		return ident.Kind == KindHuman
	case needAuthor:
		return isAuthor(ident, ch)
	case needApprover:
		return len(ident.Groups) > 0
	case needDomainApprover:
		return sharesGroup(ident.Groups, ch.DomainGroups)
	case needFilerOrApprover:
		return (ch.FilingPerson != "" && ident.Name == ch.FilingPerson) || len(ident.Groups) > 0
	}
	return false
}

// isAuthor compares GitHub logins without regard to case, as GitHub does. A person with no login is never the author.
func isAuthor(ident Identity, ch Change) bool {
	return ident.GitHub != "" && ch.AuthorLogin != "" && strings.EqualFold(ident.GitHub, ch.AuthorLogin)
}

// sharesGroup reports whether a person's approver groups include one of the groups that own the change.
func sharesGroup(groups, owned []string) bool {
	for _, g := range groups {
		if slices.Contains(owned, g) {
			return true
		}
	}
	return false
}
