package ports

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"path"
	"regexp"
	"slices"
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
	// Checks are the profile's tool checks: each runs on the head tree (and on the base tree when Baseline is
	// set) and writes SARIF (REVIEW_ARCH 4.1).
	Checks []ProfileCheck
	// Kinds are the profile's test kinds, sorted by name: each names how to run tests of that kind and writes
	// JUnit (TEST_AGENT_ARCH 5.1).
	Kinds []ProfileKind
	// Runs is how many times a gate run of a kind is repeated; zero only when the profile declares no kinds.
	Runs int
	// Services are the throwaway containers a job may reach on a private network with no egress.
	Services []ProfileService
}

// Profile extension bounds. They keep a hostile or careless profile from becoming an unbounded job plan.
const (
	maxProfileChecks   = 32
	maxProfileKinds    = 8
	maxProfileServices = 4
	maxProfileGlobs    = 16
	maxProfileRuns     = 10
	maxProfileCommand  = 1024
	maxServiceEnv      = 16
)

// ProfileCheck is one row of the profile's `checks:` list. Its command comes only from a profile a person merged.
type ProfileCheck struct {
	ID       string
	Lens     string
	When     []string // globs over the files the change touches
	Image    string   // a name on the runner's image allowlist, or a digest-pinned reference on it
	Run      string
	Baseline bool // also run on base and keep only what is new
	// History mounts the repository's history mirror read-only at /history for this check (REVIEW_ARCH 4.6). The mirror
	// is fetched only by the warm job; a check can read it and never change it.
	History bool
}

// ProfileKind is one test kind of the profile's `tests.kinds:` map.
type ProfileKind struct {
	Name  string
	Image string // empty means the profile's own image
	Run   string // must carry exactly one {dir}
	Serve string // starts the app under test inside the job; empty for a kind that needs no app
	Ready string // a loopback URL the job waits on before the run; set exactly when Serve is
}

// ProfileService is one throwaway container of the profile's `services:` list. It runs as a fixed non-root user with
// no capabilities; an image that cannot run that way is not allowlisted. Port is the TCP port the runner waits on
// before the job starts. Data, when set, is the directory the service writes, mounted as a fresh tmpfs.
type ProfileService struct {
	Name  string
	Image string
	Env   map[string]string
	Port  int
	Data  string
}

// Validate returns every problem with the parsed profile contract.
func (s ProfileSpec) Validate() error {
	return errors.Join(s.validateBase(), s.ValidateExtensions())
}

// ValidateExtensions returns every problem with the checks, test kinds, runs and services of the contract. It is
// the one place the shape rules of those fields live: the profile parser calls it and adds the allowlist rule.
func (s ProfileSpec) ValidateExtensions() error {
	var v problems
	v.require("checks", len(s.Checks) <= maxProfileChecks, "must name at most 32 checks")
	seen := map[string]bool{}
	for i, c := range s.Checks {
		v.add("checks["+strconv.Itoa(i)+"]", c.validate(seen))
	}
	v.require("kinds", len(s.Kinds) <= maxProfileKinds, "must name at most 8 test kinds")
	seen = map[string]bool{}
	for i, k := range s.Kinds {
		v.add("kinds["+strconv.Itoa(i)+"]", k.validate(seen))
	}
	if len(s.Kinds) > 0 || s.Runs != 0 {
		v.require("runs", s.Runs >= 1 && s.Runs <= maxProfileRuns, "must be 1 to 10 when the profile declares test kinds")
	}
	v.require("services", len(s.Services) <= maxProfileServices, "must name at most 4 services")
	seen = map[string]bool{}
	for i, svc := range s.Services {
		v.add("services["+strconv.Itoa(i)+"]", svc.validate(seen))
	}
	return v.join()
}

func (c ProfileCheck) validate(seen map[string]bool) error {
	var v problems
	v.add("id", checkUniqueName(c.ID, seen))
	v.require("lens", profileNameRef.MatchString(c.Lens), "must be a lens name: one to 64 lowercase name characters")
	v.require("when", len(c.When) >= 1 && len(c.When) <= maxProfileGlobs, "must name one to 16 globs")
	for i, g := range c.When {
		v.add("when["+strconv.Itoa(i)+"]", checkGlob(g))
	}
	v.add("image", checkProfileImage(c.Image))
	v.add("run", checkRunCommand(c.Run))
	v.require("run", !strings.Contains(c.Run, "{dir}"), "a check has no {dir}: it runs on the whole tree")
	return v.join()
}

func (k ProfileKind) validate(seen map[string]bool) error {
	var v problems
	v.add("name", checkUniqueName(k.Name, seen))
	if k.Image != "" {
		v.add("image", checkProfileImage(k.Image))
	}
	v.add("run", checkRunCommand(k.Run))
	v.require("run", acceptsDir(k.Run), "must contain exactly one {dir} placeholder")
	v.require("serve", (k.Serve == "") == (k.Ready == ""), "serve and ready must be set together")
	if k.Serve != "" {
		v.add("serve", checkRunCommand(k.Serve))
		v.add("ready", CheckLoopbackURL(k.Ready))
	}
	return v.join()
}

func (s ProfileService) validate(seen map[string]bool) error {
	var v problems
	v.add("name", checkUniqueName(s.Name, seen))
	v.add("image", checkProfileImage(s.Image))
	v.require("port", s.Port >= 1 && s.Port <= 65535, "must be the service's TCP port, 1 to 65535")
	if s.Data != "" {
		v.require("data", ValidServiceDataDir(s.Data), "must be an absolute, clean directory outside /proc, /sys and /dev, with no comma or colon")
	}
	v.require("env", len(s.Env) <= maxServiceEnv, "must hold at most 16 variables")
	for key, value := range s.Env {
		v.require("env."+key, envKey.MatchString(key), "must be an upper-case variable name")
		v.require("env."+key, len(value) <= 256 && !strings.ContainsAny(value, "\x00\r\n"), "must be one line of at most 256 bytes")
	}
	return v.join()
}

var envKey = regexp.MustCompile(`^[A-Z_][A-Z0-9_]{0,63}$`)

// ValidServiceDataDir reports whether dir may become a service's tmpfs mount point. A comma or colon would add mount
// options to the runner's --tmpfs argument, so neither is allowed.
func ValidServiceDataDir(dir string) bool {
	if len(dir) > 255 || !strings.HasPrefix(dir, "/") || dir == "/" || path.Clean(dir) != dir || strings.ContainsAny(dir, ",:\x00\r\n ") {
		return false
	}
	for _, kernel := range []string{"/proc", "/sys", "/dev"} {
		if dir == kernel || strings.HasPrefix(dir, kernel+"/") {
			return false
		}
	}
	return true
}

// checkUniqueName requires a profile-name-shaped identifier not seen before, and records it.
func checkUniqueName(name string, seen map[string]bool) error {
	if !profileNameRef.MatchString(name) {
		return errors.New("must be one to 64 lowercase name characters")
	}
	if seen[name] {
		return fmt.Errorf("%q is used twice", name)
	}
	seen[name] = true
	return nil
}

// checkProfileImage requires a bare image name or a digest-pinned reference. Whether it is on the runner's allowlist
// is the profile parser's rule, because only it holds the list.
func checkProfileImage(image string) error {
	if profileNameRef.MatchString(image) || checkImageReference(image) {
		return nil
	}
	return errors.New("must be an allowlisted image name or a digest-pinned image reference")
}

// checkRunCommand requires one bounded command line: no NUL and no line break.
func checkRunCommand(cmd string) error {
	if err := checkText(cmd, maxProfileCommand); err != nil {
		return err
	}
	if strings.ContainsAny(cmd, "\x00\r\n") {
		return errors.New("must be one line")
	}
	return nil
}

// checkGlob requires a repository-relative glob: no absolute path, no parent step, no backslash.
func checkGlob(g string) error {
	if err := checkText(g, 200); err != nil {
		return err
	}
	if strings.ContainsAny(g, "\x00\\") || path.IsAbs(g) || slices.Contains(strings.Split(g, "/"), "..") {
		return fmt.Errorf("%q must be a relative glob with no .. step", g)
	}
	return nil
}

// CheckLoopbackURL requires an http URL that names a loopback address and a port, so a ready probe can only ever
// reach the app started inside the job. The profile parser and the runner both apply it.
func CheckLoopbackURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Port() == "" {
		return fmt.Errorf("%q must be an http URL with a loopback host and a port", raw)
	}
	host := u.Hostname()
	if host == "localhost" {
		return nil
	}
	if addr, perr := netip.ParseAddr(host); perr == nil && addr.IsLoopback() {
		return nil
	}
	return fmt.Errorf("%q must name a loopback host", raw)
}

func (s ProfileSpec) validateBase() error {
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
	// LatestProfile returns the repository's newest proposal in any state (D175), false when it has none.
	LatestProfile(ctx context.Context, repo string) (ProfileProposal, bool, error)
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

// ProfileHistoryRequest asks for a history-mirror update. Token, for a private repository, is a short-lived read-only
// token the server minted for this one repository and this one request; the runner uses it for the fetch and keeps
// nothing. A public repository needs none.
type ProfileHistoryRequest struct {
	ProfileWarmRequest
	Token string
}

// historyToken is the shape of a forge token: long enough to be one, and only characters that cannot end an HTTP
// header or start another.
var historyToken = regexp.MustCompile(`^[A-Za-z0-9_]{20,255}$`)

// Validate returns every problem with the history request. A refusal never repeats the token.
func (r ProfileHistoryRequest) Validate() error {
	var v problems
	v.add("request", r.ProfileWarmRequest.Validate())
	if r.Token != "" {
		v.require("token", historyToken.MatchString(r.Token), "must be 20 to 255 letters, digits or underscores")
	}
	return v.join()
}

// ProfileHistoryResult is the bounded outcome of one history-mirror update.
type ProfileHistoryResult struct {
	Bytes int64 // size of the mirror after the update
	Fresh bool  // the mirror did not exist before this update
}

// ProfileHistoryWarmer keeps the blobless history mirror of a repository (commits and trees only). Adapter: the runner
// client; the runner owns the egress boundary, and the warm job is the only job that fetches.
type ProfileHistoryWarmer interface {
	WarmHistory(ctx context.Context, req ProfileHistoryRequest) (ProfileHistoryResult, error)
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
