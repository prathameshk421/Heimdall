package gateuse

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"path"
	"slices"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// PolicyDecoder parses raw bytes into a validated policy.Policy.
type PolicyDecoder func([]byte) (policy.Policy, error)

// SourceConfig configures a RepoSource.
type SourceConfig struct {
	PRs        ports.PRReader
	Code       ports.CodeReader
	Decode     PolicyDecoder
	PolicyPath string
}

// RepoSource implements Source by reading pull request data and git objects through ports.
type RepoSource struct {
	prs        ports.PRReader
	code       ports.CodeReader
	decode     PolicyDecoder
	policyPath string
}

var _ Source = (*RepoSource)(nil)

// NewSource builds a RepoSource after validating its dependencies.
func NewSource(cfg SourceConfig) (*RepoSource, error) {
	var missing []string
	if cfg.PRs == nil {
		missing = append(missing, "PRs")
	}
	if cfg.Code == nil {
		missing = append(missing, "Code")
	}
	if cfg.Decode == nil {
		missing = append(missing, "Decode")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("gateuse: missing required source config fields: %s", strings.Join(missing, ", "))
	}
	polPath := cfg.PolicyPath
	if polPath == "" {
		polPath = "heimdall.yaml"
	}
	return &RepoSource{
		prs:        cfg.PRs,
		code:       cfg.Code,
		decode:     cfg.Decode,
		policyPath: polPath,
	}, nil
}

// Outside returns the repository and policy facts for the change.
func (s *RepoSource) Outside(ctx context.Context, req Request) (Outside, error) {
	pr, err := s.prs.PR(ctx, req.Repo, req.Number)
	if err != nil {
		return Outside{}, fmt.Errorf("get pull request %s#%d: %w", req.Repo, req.Number, err)
	}

	basePol, baseKnown, err := s.readBasePolicy(ctx, req.Repo, pr.BaseSHA)
	if err != nil {
		return Outside{}, fmt.Errorf("read base policy %s at %s: %w", s.policyPath, pr.BaseSHA, err)
	}

	headPol, headKnown, err := s.readHeadPolicy(ctx, req.Repo, req.HeadSHA)
	if err != nil {
		return Outside{}, fmt.Errorf("read head policy %s at %s: %w", s.policyPath, req.HeadSHA, err)
	}

	policyKnown := baseKnown && headKnown

	var policyVersion int
	if baseKnown {
		policyVersion = basePol.Version
	}

	headTestTreeHash, err := s.readHeadTestTreeHash(ctx, req, basePol, baseKnown)
	if err != nil {
		return Outside{}, err
	}

	testDiffKnown, testDiffOutsideKey, changedFiles := s.readFiles(ctx, req, basePol, baseKnown)

	var ciRequired []string
	if baseKnown {
		ciRequired = slices.Clone(basePol.CIRequired)
	}

	approversForChangedDomains := collectDomainApprovers(basePol, baseKnown, testDiffKnown, changedFiles)

	policyOwnerApproved := checkPolicyOwnerApproved(basePol, headPol, baseKnown, headKnown, req.Approvers)

	return Outside{
		HeadTestTreeHash:           headTestTreeHash,
		TestDiffKnown:              testDiffKnown,
		TestDiffOutsideKey:         testDiffOutsideKey,
		CIRequired:                 ciRequired,
		ApproversForChangedDomains: approversForChangedDomains,
		PolicyKnown:                policyKnown,
		PolicyAtBase:               basePol,
		PolicyAtHead:               headPol,
		PolicyOwnerApproved:        policyOwnerApproved,
		PolicyVersion:              policyVersion,
	}, nil
}

func parsePolicy(decode PolicyDecoder, raw []byte) (policy.Policy, bool) {
	p, err := decode(raw)
	if err != nil || p.Validate() != nil {
		return policy.Policy{}, false
	}
	return p, true
}

func (s *RepoSource) readBasePolicy(ctx context.Context, repo change.Repo, sha id.SHA) (policy.Policy, bool, error) {
	raw, readErr := s.code.ReadFile(ctx, repo, sha, s.policyPath)
	if errors.Is(readErr, ports.ErrNotFound) {
		return policy.Policy{}, false, nil
	}
	if readErr != nil {
		return policy.Policy{}, false, readErr
	}
	p, ok := parsePolicy(s.decode, raw)
	return p, ok, nil
}

func (s *RepoSource) readHeadPolicy(ctx context.Context, repo change.Repo, sha id.SHA) (policy.Policy, bool, error) {
	raw, readErr := s.code.ReadFile(ctx, repo, sha, s.policyPath)
	if errors.Is(readErr, ports.ErrNotFound) {
		return policy.Policy{}, true, nil
	}
	if readErr != nil {
		return policy.Policy{}, false, readErr
	}
	p, ok := parsePolicy(s.decode, raw)
	return p, ok, nil
}

func (s *RepoSource) readHeadTestTreeHash(ctx context.Context, req Request, basePol policy.Policy, baseKnown bool) (string, error) {
	if !baseKnown || basePol.Tests.Dir == "" || req.ChangeID == "" {
		return "", nil
	}
	testDir := path.Join(basePol.Tests.Dir, string(req.ChangeID))
	treeHash, err := s.code.TreeHash(ctx, req.Repo, req.HeadSHA, testDir)
	if err != nil {
		if errors.Is(err, ports.ErrNotFound) {
			return "", nil
		}
		return "", fmt.Errorf("tree hash %s at %s: %w", testDir, req.HeadSHA, err)
	}
	return string(treeHash), nil
}

func (s *RepoSource) readFiles(ctx context.Context, req Request, basePol policy.Policy, baseKnown bool) (bool, []string, []ports.ChangedFile) {
	files, err := s.prs.Files(ctx, req.Repo, req.Number)
	if err != nil {
		return false, nil, nil
	}

	var outsideKey []string
	if baseKnown && basePol.Tests.Dir != "" {
		testsDir := basePol.Tests.Dir
		ticketDir := path.Join(testsDir, string(req.ChangeID))
		for _, f := range files {
			p := f.Path
			isUnderTests := p == testsDir || strings.HasPrefix(p, testsDir+"/")
			isUnderTicket := p == ticketDir || strings.HasPrefix(p, ticketDir+"/")
			if isUnderTests && !isUnderTicket {
				outsideKey = append(outsideKey, p)
			}
		}
		slices.Sort(outsideKey)
		outsideKey = slices.Compact(outsideKey)
	}

	return true, outsideKey, files
}

func collectDomainApprovers(basePol policy.Policy, baseKnown bool, testDiffKnown bool, changedFiles []ports.ChangedFile) []string {
	if !baseKnown || !testDiffKnown || len(changedFiles) == 0 {
		return nil
	}

	touched := findTouchedDomains(basePol, changedFiles)
	approverSet := make(map[string]bool)
	for domain := range touched {
		for _, login := range domainApproverLogins(basePol, domain) {
			approverSet[login] = true
		}
	}

	if len(approverSet) == 0 {
		return nil
	}

	approvers := slices.Collect(maps.Keys(approverSet))
	slices.Sort(approvers)
	return approvers
}

func findTouchedDomains(basePol policy.Policy, files []ports.ChangedFile) map[string]bool {
	touched := make(map[string]bool)
	patterns := slices.Sorted(maps.Keys(basePol.Domains))

	for _, f := range files {
		matched := false
		for _, pat := range patterns {
			if policy.MatchGlob(pat, f.Path) {
				touched[basePol.Domains[pat]] = true
				matched = true
			}
		}
		if !matched {
			if _, ok := basePol.Approvers["default"]; ok {
				touched["default"] = true
			}
		}
	}
	return touched
}

func domainApproverLogins(basePol policy.Policy, domain string) []string {
	var logins []string
	for _, personName := range basePol.Approvers[domain] {
		login := personName
		if basePol.People != nil {
			if person, ok := basePol.People[personName]; ok && person.GitHub != "" {
				login = person.GitHub
			}
		}
		if login != "" {
			logins = append(logins, login)
		}
	}
	return logins
}

func checkPolicyOwnerApproved(basePol policy.Policy, headPol policy.Policy, baseKnown bool, headKnown bool, approvers []string) bool {
	if !baseKnown || !headKnown || policy.Equal(basePol, headPol) {
		return false
	}

	for _, ownerName := range basePol.PolicyOwners {
		ownerLogin := ownerName
		if basePol.People != nil {
			if p, ok := basePol.People[ownerName]; ok && p.GitHub != "" {
				ownerLogin = p.GitHub
			}
		}
		for _, approver := range approvers {
			if strings.EqualFold(approver, ownerLogin) {
				return true
			}
		}
	}
	return false
}
