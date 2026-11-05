package people

import (
	"fmt"
	"maps"
	"regexp"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
)

// Mapping is one entry of `heimdall connect people`. An empty field keeps the person's current value.
type Mapping struct {
	Name   string
	Chat   string
	GitHub string
	Agent  string
	Roles  []Role
}

// These patterns are the whole shape of a name, a login and a sender id that this command accepts. The sender id
// rule is the domain's (change.ParseSenderID), so it is not repeated here.
var (
	personName  = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,38}$`)
	githubLogin = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,38}$`)
)

// ParseRole returns the role a mapping may set. Policy owners are listed under policy_owners, so that word is
// refused here with the reason.
func ParseRole(s string) (Role, error) {
	switch r := Role(s); r {
	case RoleMember, RoleAuthor, RoleApprover:
		return r, nil
	case RolePolicyOwner:
		return "", fmt.Errorf("role %q: %w: policy owners are listed under policy_owners", s, ErrUnknownRole)
	default:
		return "", fmt.Errorf("role %q: %w", s, ErrUnknownRole)
	}
}

// ApplyMapping returns a copy of the policy with the entry applied. The result validates. A refused entry returns an
// empty policy and an error, and the caller's policy is not changed.
func ApplyMapping(p policy.Policy, m Mapping) (policy.Policy, error) {
	if !personName.MatchString(m.Name) {
		return policy.Policy{}, fmt.Errorf("name %q must be 1 to 39 lower-case letters, digits, dash or underscore", m.Name)
	}
	if err := p.Validate(); err != nil {
		return policy.Policy{}, fmt.Errorf("policy is invalid: %w", err)
	}
	people := maps.Clone(p.People)
	if people == nil {
		people = map[string]policy.Person{}
	}
	per := people[m.Name]
	if err := setChat(people, m.Name, &per, m.Chat); err != nil {
		return policy.Policy{}, err
	}
	if err := setGitHub(people, m.Name, &per, m.GitHub); err != nil {
		return policy.Policy{}, err
	}
	if err := setAgent(people, m.Name, &per, m.Agent); err != nil {
		return policy.Policy{}, err
	}
	if err := checkRoles(p, m.Name, per, m.Roles); err != nil {
		return policy.Policy{}, err
	}
	people[m.Name] = per
	next := p
	next.People = people
	if err := next.Validate(); err != nil {
		return policy.Policy{}, fmt.Errorf("mapping leaves the policy invalid: %w", err)
	}
	return next, nil
}

// setChat sets the person's sender id. It refuses a second value, a sender id another person holds, and a sender
// id that is an agent name.
func setChat(people map[string]policy.Person, name string, per *policy.Person, chat string) error {
	if chat == "" {
		return nil
	}
	sid, err := change.ParseSenderID(chat)
	if err != nil {
		return fmt.Errorf("people.%s.chat: %w", name, err)
	}
	if per.Chat != "" && per.Chat != string(sid) {
		return fmt.Errorf("people.%s.chat: %w: edit heimdall.yaml to change it", name, ErrAlreadySet)
	}
	for other, q := range people {
		if other != name && q.Chat == string(sid) {
			return fmt.Errorf("people.%s.chat: already the sender id of people.%s: %w", name, other, ErrDuplicate)
		}
		if q.Agent == string(sid) {
			return fmt.Errorf("people.%s.chat: %w", name, ErrAmbiguous)
		}
	}
	per.Chat = string(sid)
	return nil
}

// setGitHub sets the person's login. A login is compared without regard to case.
func setGitHub(people map[string]policy.Person, name string, per *policy.Person, login string) error {
	if login == "" {
		return nil
	}
	if !githubLogin.MatchString(login) {
		return fmt.Errorf("people.%s.github: not a GitHub login", name)
	}
	if per.GitHub != "" {
		if strings.EqualFold(per.GitHub, login) {
			return nil
		}
		return fmt.Errorf("people.%s.github: %w: edit heimdall.yaml to change it", name, ErrAlreadySet)
	}
	for other, q := range people {
		if other != name && strings.EqualFold(q.GitHub, login) {
			return fmt.Errorf("people.%s.github: the login of people.%s: %w", name, other, ErrDuplicate)
		}
	}
	per.GitHub = login
	return nil
}

// setAgent sets the person's agent name, which follows Switch's agent rule. It refuses a second name, a name another
// person holds, and a name that is a sender id.
func setAgent(people map[string]policy.Person, name string, per *policy.Person, agent string) error {
	if agent == "" {
		return nil
	}
	if _, err := id.ParseAgentName(agent); err != nil {
		return fmt.Errorf("people.%s.agent: %w", name, err)
	}
	if per.Agent != "" && per.Agent != agent {
		return fmt.Errorf("people.%s.agent: %w: edit heimdall.yaml to change it", name, ErrAlreadySet)
	}
	for other, q := range people {
		if other != name && q.Agent == agent {
			return fmt.Errorf("people.%s.agent: already the agent of people.%s: %w", name, other, ErrDuplicate)
		}
		if q.Chat == agent {
			return fmt.Errorf("people.%s.agent: %w", name, ErrAmbiguous)
		}
	}
	per.Agent = agent
	return nil
}

// checkRoles refuses an unknown role, and an approver role for a person no approver group names. An author or an
// approver needs a GitHub login (D158). So does a person the policy names as an approver or a policy owner.
func checkRoles(p policy.Policy, name string, per policy.Person, roles []Role) error {
	needsGitHub := namedApprover(p, name) || isPolicyOwner(p, name)
	for _, r := range roles {
		switch r {
		case RoleMember:
		case RoleAuthor:
			needsGitHub = true
		case RoleApprover:
			if !namedApprover(p, name) {
				return fmt.Errorf("people.%s: %w: add the person to an approvers group in heimdall.yaml first", name, ErrNotApprover)
			}
			needsGitHub = true
		case RolePolicyOwner:
			return fmt.Errorf("role %q: %w: policy owners are listed under policy_owners", r, ErrUnknownRole)
		default:
			return fmt.Errorf("role %q: %w", r, ErrUnknownRole)
		}
	}
	if needsGitHub && per.GitHub == "" {
		return fmt.Errorf("people.%s.github: %w", name, ErrGitHubRequired)
	}
	return nil
}

// namedApprover reports whether one approver group lists the person.
func namedApprover(p policy.Policy, name string) bool {
	for _, members := range p.Approvers {
		for _, m := range members {
			if m == name {
				return true
			}
		}
	}
	return false
}

// isPolicyOwner reports whether policy_owners lists the person.
func isPolicyOwner(p policy.Policy, name string) bool {
	for _, m := range p.PolicyOwners {
		if m == name {
			return true
		}
	}
	return false
}
