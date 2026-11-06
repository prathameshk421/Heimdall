package ports

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
)

// ProfileState is how far one reviewed repository profile has moved (TECH_SPEC 12.2b).
type ProfileState string

// The lifecycle states. A profile is usable by production sandbox jobs only while promoted.
const (
	ProfileProposed ProfileState = "proposed"
	ProfileApproved ProfileState = "approved"
	ProfilePromoted ProfileState = "promoted"
)

// Valid reports whether s is one of the profile states.
func (s ProfileState) Valid() bool {
	return s == ProfileProposed || s == ProfileApproved || s == ProfilePromoted
}

// ProfileSpec is the reviewed repository-owned sandbox contract, already validated against the
// image allowlist. It is the parsed form of `.heimdall/profile.yaml` (TECH_SPEC 12.2b).
type ProfileSpec struct {
	Languages  []string
	Image      string
	Acceptance string
	Seam       string
	ACTag      string
	FetchHosts []string
	TestsDir   string
}

// Validate returns every problem with the parsed profile contract.
func (s ProfileSpec) Validate() error {
	var v problems
	v.require("languages", len(s.Languages) > 0 && len(s.Languages) <= 4, "must name one to four languages")
	for i, l := range s.Languages {
		v.add("languages["+strconv.Itoa(i)+"]", checkText(l, 24))
	}
	v.add("image", checkText(s.Image, 256))
	if s.Image != "" {
		v.require("image", checkImageReference(s.Image), "must be a digest-pinned image reference")
	}
	v.require("acceptance", checkText(s.Acceptance, 240) == nil, "must be a bounded command with {dir}")
	if s.Acceptance != "" {
		v.require("acceptance", acceptsDir(s.Acceptance), "must contain exactly one {dir} placeholder")
	}
	v.add("seam", checkText(s.Seam, 240))
	v.add("ac_tag", checkText(s.ACTag, 120))
	v.require("fetch_hosts", len(s.FetchHosts) <= 8, "must name at most eight fetch hosts")
	for i, h := range s.FetchHosts {
		v.add("fetch_hosts["+strconv.Itoa(i)+"]", checkText(h, 253))
	}
	v.add("tests_dir", checkRelPath(s.TestsDir))
	return v.join()
}

// acceptsDir reports whether the acceptance command names one {dir} placeholder, so the runner can
// substitute the job directory and nothing else.
func acceptsDir(cmd string) bool {
	const ph = "{dir}"
	first := strings.Index(cmd, ph)
	return first >= 0 && !strings.Contains(cmd[first+len(ph):], ph)
}

// checkImageReference reports whether an image is digest-pinned.
func checkImageReference(image string) bool {
	return imageDigest.MatchString(image)
}

var imageDigest = regexp.MustCompile(`^[A-Za-z0-9._:/-]+@sha256:[a-f0-9]{64}$`)

// ProfileTrialOutcome says what one real profile trial produced.
type ProfileTrialOutcome string

// The trial outcomes. A promotion is refused until a recorded trial passed.
const (
	TrialPassed ProfileTrialOutcome = "passed"
	TrialFailed ProfileTrialOutcome = "failed"
)

// Valid reports whether o is a recorded trial outcome.
func (o ProfileTrialOutcome) Valid() bool {
	return o == TrialPassed || o == TrialFailed
}

// ProfileTrial is the result of one real isolated trial run of a proposed profile.
type ProfileTrial struct {
	Outcome ProfileTrialOutcome
	Passed  int
	Failed  int
	Errored int
	SHA     id.SHA
	LogRef  workflow.BlobRef
}

// ProfileProposal is one durable proposal of a repository profile through its review lifecycle.
type ProfileProposal struct {
	ID         string // 64 lowercase hex characters: sha256 over the normalized repository and artifact bytes
	Repo       string // the mapped repository, owner/name
	State      ProfileState
	Author     string // the human name on the propose action, for the audit trail
	Artifact   []byte // the reviewed profile YAML, bounded at 64 KiB
	Spec       ProfileSpec
	Approver   string // the authorized reviewer who approved, empty until approved
	ApprovedAt time.Time
	Trial      ProfileTrial // zero value until a real trial was recorded
	CreatedAt  time.Time
	PromotedAt time.Time
}

var proposalID = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Validate returns every problem with the proposal.
func (p ProfileProposal) Validate() error {
	var v problems
	v.require("id", proposalID.MatchString(p.ID), "must be 64 lowercase hex characters")
	v.add("repo", checkRepo(change.Repo(p.Repo)))
	v.require("state", p.State.Valid(), "must be proposed, approved or promoted")
	v.require("artifact", len(p.Artifact) > 0 && len(p.Artifact) <= 64<<10, "must be 1 byte to 64 KiB")
	v.add("author", checkText(p.Author, 128))
	v.add("spec", p.Spec.Validate())
	v.require("created_at", !p.CreatedAt.IsZero(), "must be set when the proposal is created")
	validateProposalState(&v, p)
	validateProposalTrial(&v, p)
	return v.join()
}

func validateProposalState(v *problems, p ProfileProposal) {
	if p.State == ProfileProposed {
		v.require("approval", p.Approver == "" && p.ApprovedAt.IsZero(), "must be empty before approval")
	}
	if p.State == ProfileApproved || p.State == ProfilePromoted {
		v.add("approver", checkText(p.Approver, 128))
		v.require("approver", p.Approver != "", "must be named once the profile is approved")
		v.require("approved_at", !p.ApprovedAt.IsZero(), "must be set once the profile is approved")
	}
	if p.PromotedAt.IsZero() && p.State == ProfilePromoted {
		v.require("promoted_at", false, "must be set for a promoted profile")
	}
	if p.State != ProfilePromoted {
		v.require("promoted_at", p.PromotedAt.IsZero(), "must be empty until promotion")
	}
}

func validateProposalTrial(v *problems, p ProfileProposal) {
	if p.Trial.Outcome == "" {
		v.require("trial", p.Trial.Passed == 0 && p.Trial.Failed == 0 && p.Trial.Errored == 0 && p.Trial.SHA == "" && p.Trial.LogRef == "",
			"must be empty until a trial is recorded")
	} else {
		v.require("trial.outcome", p.Trial.Outcome.Valid(), "must be passed or failed")
		v.add("trial.sha", checkSHA(p.Trial.SHA))
		v.add("trial.log_ref", checkBlob(p.Trial.LogRef))
		v.require("trial.passed", p.Trial.Passed >= 0, "must not be negative")
		v.require("trial.failed", p.Trial.Failed >= 0, "must not be negative")
		v.require("trial.errored", p.Trial.Errored >= 0, "must not be negative")
	}
	if p.State == ProfileProposed {
		v.require("trial", p.Trial.Outcome == "", "cannot be trialed before approval")
	}
	if p.State == ProfilePromoted {
		v.require("trial.outcome", p.Trial.Outcome == TrialPassed, "must have a passing trial before promotion")
	}
}

// ProfileStore is the durable record of the profile lifecycle. Store: Postgres. Consumer: the profile
// review service and the production sandbox wiring.
//
// Every transition is guarded by the state the store already holds, so a replay that arrives after a
// restart (or twice from a retried admin call) cannot advance a profile twice.
type ProfileStore interface {
	// PutProfile inserts a proposal keyed by its content identifier. An exact replay returns false
	// and changes nothing; the same id with a different artifact is refused.
	PutProfile(ctx context.Context, p ProfileProposal) (bool, error)
	// Profile returns the proposal with that id.
	Profile(ctx context.Context, id string) (ProfileProposal, bool, error)
	// ApproveProfile records one approval, moving proposed to approved. It returns false when the id
	// is unknown or is not in the proposed state.
	ApproveProfile(ctx context.Context, id, actor string, at time.Time) (bool, error)
	// RecordProfileTrial stores the latest real trial result while the profile is approved. It
	// returns false when the id is unknown or no longer recordable.
	RecordProfileTrial(ctx context.Context, id string, t ProfileTrial, at time.Time) (bool, error)
	// PromoteProfile moves approved to promoted and sets the repository's active profile pointer in
	// one transaction, refusing twice (one promotion per id) and requiring a passing trial.
	PromoteProfile(ctx context.Context, id string, at time.Time) (bool, error)
	// ActiveProfile returns the promoted proposal the repository's production sandbox jobs use.
	ActiveProfile(ctx context.Context, repo string) (ProfileProposal, bool, error)
	// PromotedProfiles lists every promoted profile for replay to the runner, ordered by repository.
	PromotedProfiles(ctx context.Context) ([]ProfileProposal, error)
}

// ProfileTrialJob asks the runner for the one real isolated trial run of a proposed profile
// (TECH_SPEC 12.2b: a trial that produces test results before the sandbox uses the profile).
type ProfileTrialJob struct {
	Repo     change.Repo
	SHA      id.SHA
	Name     string // the profile name as the runner sees it: the normalized repository name
	Artifact []byte // the proposed profile YAML the runner parses and validates itself
	TestsDir string // the value substituted for {dir} in the acceptance command
}

// Validate returns every problem with the trial job.
func (j ProfileTrialJob) Validate() error {
	var v problems
	v.add("repo", checkRepo(j.Repo))
	v.add("sha", checkSHA(j.SHA))
	v.require("name", profileNameRef.MatchString(j.Name), "must be a profile name: one to 64 lowercase name characters")
	v.require("artifact", len(j.Artifact) > 0 && len(j.Artifact) <= 64<<10, "must be 1 byte to 64 KiB")
	v.add("tests_dir", checkRelPath(j.TestsDir))
	return v.join()
}

var profileNameRef = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// ProfileTrialRunner runs one proposed profile as a real isolated job. Adapter: the rootless runner
// client, which sends the proposed artifact to the runner over the Unix socket; the runner decides
// whether the proposal is inside its own policy invariants before the job starts.
type ProfileTrialRunner interface {
	Trial(ctx context.Context, j ProfileTrialJob) (SandboxResult, error)
}

// ProfileParser turns a proposed repository profile artifact into the validated contract. Adapter:
// profileyaml, the one authority for the artifact's shape. Consumer: the profile review service, so
// a proposal is checked like the runner itself would check it.
type ProfileParser interface {
	Parse(artifact []byte, images []string) (ProfileSpec, error)
}

// ProfilePolicySource reports the image allowlist for referees. Adapter: the runner process keeps the
// allowlist, so proposing a profile validates against the same list the runner enforces.
type ProfilePolicySource interface {
	AllowedImages(ctx context.Context) ([]string, error)
}

// ProfileSyncer mirrors the durable promoted profiles to the runner process, which reloads them at
// boot from its own directory. Adapter: the runner client over the Unix socket.
type ProfileSyncer interface {
	// Profiles lists the profile names the runner can currently run.
	Profiles(ctx context.Context) ([]string, error)
	// SyncProfile pushes one promoted artifact into the runner's durable promoted directory.
	SyncProfile(ctx context.Context, name string, artifact []byte) error
}

// ProfileWarmRequest asks for the module-cache warm job of one repository commit (TECH_SPEC 12.2:
// dependencies come from a read-only module cache warmed by a fetch job; it is the only job with
// egress and its egress is allowlisted).
type ProfileWarmRequest struct {
	Repo    change.Repo
	SHA     id.SHA
	Profile string // the profile whose fetch_hosts bound the traffic; empty means the Go default
}

// Validate returns every problem with the warm request.
func (r ProfileWarmRequest) Validate() error {
	var v problems
	v.add("repo", checkRepo(r.Repo))
	v.add("sha", checkSHA(r.SHA))
	if r.Profile != "" && !profileNameRef.MatchString(r.Profile) {
		v.require("profile", false, "must be an empty profile name or one to 64 lowercase name characters")
	}
	return v.join()
}

// ProfileWarmResult is the bounded outcome of one warm job.
type ProfileWarmResult struct {
	Modules      []string
	FetchedBytes int64
	Warnings     []string
}

// ProfileWarmer runs warm jobs. Adapter: the runner client; the runner keeps the egress boundary.
type ProfileWarmer interface {
	Warm(ctx context.Context, req ProfileWarmRequest) (ProfileWarmResult, error)
}
