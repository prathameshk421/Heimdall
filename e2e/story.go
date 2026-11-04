package e2e

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"sort"

	"gopkg.in/yaml.v3"
)

// Story is the data a scenario runs on (MVP_SCENARIOS section 1). A scenario never names a repo, a ticket
// or a person directly; it reads them from the Story, so the same script runs on the fixture and on Jurist AI.
type Story struct {
	Name       string `yaml:"name"`
	Repo       string `yaml:"repo"`
	BaseBranch string `yaml:"base_branch"`
	// Pending is the task that finishes the story's repository content (T31 for fixture-pay, T75 for jurist).
	// While it is set the story is incomplete on purpose: its steps are blocked on it, never guessed.
	Pending string `yaml:"pending"`

	Ticket       TicketSpec        `yaml:"ticket"`
	TenantTicket *TicketSpec       `yaml:"tenant_ticket"`
	People       map[string]Person `yaml:"people"`
	GoodPR       string            `yaml:"good_pr"`
	BadPR        string            `yaml:"bad_pr"`
	InjectionPR  string            `yaml:"injection_pr"`
	Planted      []Planted         `yaml:"planted"`
	Profile      string            `yaml:"profile"`
	Canaries     []string          `yaml:"canaries"`
	Question     string            `yaml:"question"`
	// Answer is what the developer replies to the test-author's one question.
	Answer string `yaml:"answer"`
	// Stranger is a chat user who is not mapped to any person, for the "unmapped user is refused" steps.
	Stranger string `yaml:"stranger"`
	// DomainApprover names the person (a key of People) who signs off tests in a sensitive domain.
	DomainApprover string `yaml:"domain_approver"`
	// FixBranch holds the author's fix for the planted issues; "the author pushes a fix" advances the PR head to it.
	FixBranch string `yaml:"fix_branch"`
	// Challenge is the dispute the author raises against one planted finding, with the evidence they bring.
	Challenge *Challenge `yaml:"challenge"`

	ExpectedBlockingSet []string `yaml:"expected_blocking_set"`
}

// Challenge is a dispute of a planted finding and the evidence offered against it.
type Challenge struct {
	Finding  string `yaml:"finding"`  // the planted id being disputed
	Evidence string `yaml:"evidence"` // what the author cites; it must not change the facts (D57)
}

// TicketSpec is a ticket's content. The key comes from the tracker when the simulator creates it (D149).
type TicketSpec struct {
	Title       string   `yaml:"title"`
	Description string   `yaml:"description"`
	AC          []string `yaml:"ac"`
}

// Person is one human in a story.
type Person struct {
	Chat   string `yaml:"chat"`
	GitHub string `yaml:"github"`
	Role   string `yaml:"role"`
}

// Planted is an issue in good_pr that a named reviewer should find.
type Planted struct {
	ID          string `yaml:"id"`
	Finder      string `yaml:"finder"`
	File        string `yaml:"file"`
	Lines       [2]int `yaml:"lines"`
	Tier        string `yaml:"tier"`
	Repro       string `yaml:"repro"`
	NeedsMemory bool   `yaml:"needs_memory"`
}

// The roles a person can have in a story.
const (
	RoleAuthor         = "author"
	RoleApprover       = "approver"
	RoleReviewer       = "reviewer"
	RoleDomainApprover = "domain_approver"
)

// Person returns the named person, or the zero Person when there is none.
func (s *Story) Person(name string) Person { return s.People[name] }

// Reviewers lists the names of the people with the reviewer role, sorted.
func (s *Story) Reviewers() []string { return s.named(RoleReviewer) }

// named lists the people with a role, sorted, so board output is stable.
func (s *Story) named(role string) []string {
	var out []string
	for name, p := range s.People {
		if p.Role == role {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// Author is the person who opens pull requests.
func (s *Story) Author() Person { return s.byRole(RoleAuthor) }

// Approver is the person whose review can unlock the merge.
func (s *Story) Approver() Person { return s.byRole(RoleApprover) }

func (s *Story) byRole(role string) Person {
	if names := s.named(role); len(names) > 0 {
		return s.People[names[0]]
	}
	return Person{}
}

// PlantedIDs lists the ids of the planted issues in file order.
func (s *Story) PlantedIDs() []string {
	ids := make([]string, 0, len(s.Planted))
	for _, p := range s.Planted {
		ids = append(ids, p.ID)
	}
	return ids
}

// LoadStory reads a story file and refuses it unless it is complete and consistent.
func LoadStory(path string) (*Story, error) {
	raw, err := os.ReadFile(path) // #nosec G304 -- story paths come from the board's own command line
	if err != nil {
		return nil, fmt.Errorf("read story: %w", err)
	}
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	var s Story
	if err := dec.Decode(&s); err != nil {
		return nil, fmt.Errorf("parse story %s: %w", path, err)
	}
	if err := s.validate(); err != nil {
		return nil, fmt.Errorf("story %s: %w", path, err)
	}
	return &s, nil
}

func validTier(t string) bool { return t == "E1" || t == "E2" || t == "E3" || t == "E4" }

func validRole(r string) bool {
	return r == RoleAuthor || r == RoleApprover || r == RoleReviewer || r == RoleDomainApprover
}

func (s *Story) validate() error {
	var errs []error
	need := func(field, value string) {
		if value == "" {
			errs = append(errs, fmt.Errorf("%s is required", field))
		}
	}
	need("name", s.Name)
	need("repo", s.Repo)
	need("base_branch", s.BaseBranch)
	need("ticket.title", s.Ticket.Title)
	if len(s.Ticket.AC) == 0 {
		errs = append(errs, errors.New("ticket.ac needs at least one acceptance criterion"))
	}
	errs = append(errs, s.validatePeople()...)
	errs = append(errs, s.validateRefs()...)
	errs = append(errs, s.validatePlanted()...)
	if s.Pending == "" {
		need("good_pr", s.GoodPR)
		need("bad_pr", s.BadPR)
		need("injection_pr", s.InjectionPR)
		need("profile", s.Profile)
	}
	return errors.Join(errs...)
}

func (s *Story) validateRefs() []error {
	var errs []error
	if s.DomainApprover != "" {
		if _, ok := s.People[s.DomainApprover]; !ok {
			errs = append(errs, fmt.Errorf("domain_approver %q is not a person in the story", s.DomainApprover))
		}
	}
	if c := s.Challenge; c != nil {
		found := false
		for _, p := range s.Planted {
			found = found || p.ID == c.Finding
		}
		if !found || c.Evidence == "" {
			errs = append(errs, fmt.Errorf("challenge needs a planted finding id and evidence (got finding %q)", c.Finding))
		}
	}
	if s.Stranger != "" {
		for name, p := range s.People {
			if p.Chat == s.Stranger {
				errs = append(errs, fmt.Errorf("stranger %q is mapped to person %s, so it is not a stranger", s.Stranger, name))
			}
		}
	}
	return errs
}

func (s *Story) validatePeople() []error {
	var errs []error
	for name, p := range s.People {
		if !validRole(p.Role) {
			errs = append(errs, fmt.Errorf("person %s has unknown role %q", name, p.Role))
		}
		if p.Chat == "" {
			errs = append(errs, fmt.Errorf("person %s has no chat user", name))
		}
	}
	authors, approvers := s.named(RoleAuthor), s.named(RoleApprover)
	if len(authors) != 1 {
		errs = append(errs, fmt.Errorf("people needs exactly one author, found %d", len(authors)))
	}
	if len(approvers) != 1 {
		errs = append(errs, fmt.Errorf("people needs exactly one approver, found %d", len(approvers)))
	}
	if len(authors) == 1 && len(approvers) == 1 {
		a, b := s.People[authors[0]], s.People[approvers[0]]
		if a.GitHub == "" || b.GitHub == "" {
			errs = append(errs, errors.New("the author and the approver each need a github login (D158)"))
		} else if a.GitHub == b.GitHub {
			errs = append(errs, errors.New("the approver must not be the author: GitHub does not count a self-approval"))
		}
	}
	return errs
}

func (s *Story) validatePlanted() []error {
	var errs []error
	seen := map[string]bool{}
	for _, p := range s.Planted {
		if p.ID == "" {
			errs = append(errs, errors.New("a planted issue has no id"))
			continue
		}
		if seen[p.ID] {
			errs = append(errs, fmt.Errorf("planted id %s appears twice", p.ID))
		}
		seen[p.ID] = true
		errs = append(errs, s.validateOnePlanted(p)...)
	}
	for _, id := range s.ExpectedBlockingSet {
		if !seen[id] {
			errs = append(errs, fmt.Errorf("expected_blocking_set names %s, which is not planted", id))
		}
	}
	return errs
}

// validateOnePlanted checks one planted issue. Its location is only required once the story is defined, because a
// pending story's files do not exist yet.
func (s *Story) validateOnePlanted(p Planted) []error {
	var errs []error
	if _, ok := s.People[p.Finder]; !ok {
		errs = append(errs, fmt.Errorf("planted %s: finder %q is not a person in the story", p.ID, p.Finder))
	}
	if !validTier(p.Tier) {
		errs = append(errs, fmt.Errorf("planted %s: tier %q is not E1 to E4", p.ID, p.Tier))
	}
	if s.Pending != "" {
		return errs
	}
	if p.File == "" {
		errs = append(errs, fmt.Errorf("planted %s: file is required once the story is defined", p.ID))
	}
	if p.Lines[0] < 1 || p.Lines[1] < p.Lines[0] {
		errs = append(errs, fmt.Errorf("planted %s: lines %v must be a 1-based range, first <= last", p.ID, p.Lines))
	}
	if p.Repro == "" {
		errs = append(errs, fmt.Errorf("planted %s: repro test is required once the story is defined (MVP_SCENARIOS 8.6)", p.ID))
	}
	return errs
}
