// Package rootlessrunner implements the server-side Unix-socket client and the isolated runner execution policy.
// The runner process must execute under its dedicated Unix identity; only it invokes the rootless Podman CLI.
package rootlessrunner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/testevents"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/verify"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

const (
	// apiBodyLimit covers a 64 MiB head archive and a 32 MiB base archive as base64 in JSON, plus the overlays.
	apiBodyLimit = 140 << 20
	// maxProfilePayload bounds one proposed or promoted profile artifact: the same bound the
	// artifact itself carries.
	maxProfilePayload = 64 << 10
)

type request struct {
	Profile string           `json:"profile"`
	Job     ports.SandboxJob `json:"job"`
	Archive []byte           `json:"archive"`
	// BaseArchive is the base tree of a check or test job that compares trees.
	BaseArchive []byte       `json:"base_archive,omitempty"`
	Overlays    []ports.File `json:"overlays,omitempty"`
	// Trial carries the proposed profile artifact of a SandboxProfileTrial job. The runner parses
	// and validates it against its own allowlist and runs it outside the promoted set.
	Trial []byte `json:"trial,omitempty"`
	// TrialFrom names the repository the proposal belongs to, for the audit trail only.
	TrialFrom string `json:"trial_from,omitempty"`
	// Token is a history job's short-lived read-only token for one private repository. The runner uses it for that one
	// fetch and keeps it nowhere.
	Token string `json:"token,omitempty"`
}

// WarmSummary is the bounded result of one warm job: the staged modules and the fetched totals.
type response struct {
	Output  []byte                      `json:"output,omitempty"`
	Summary *WarmSummary                `json:"summary,omitempty"`
	History *ports.ProfileHistoryResult `json:"history,omitempty"`
	Error   string                      `json:"error,omitempty"`
}

// profileView is the GET /v1/profiles body.
type profileView struct {
	Names []string `json:"names"`
}

// healthView is the GET /v1/health body: the job kinds /v1/run accepts.
type healthView struct {
	Kinds []ports.SandboxKind `json:"kinds"`
}

// runKinds are the job kinds /v1/run executes. checkJobKind refuses every other kind.
func runKinds() []ports.SandboxKind {
	return []ports.SandboxKind{ports.SandboxCheck, ports.SandboxTest, ports.SandboxRedFirst, ports.SandboxRepro, ports.SandboxProfileTrial}
}

// policyView is the GET /v1/policy body: the image allowlist the runner enforces.
type policyView struct {
	Images []string `json:"images"`
}

// Handler exposes the narrow job API to the Unix socket only: isolated runs, proposed-profile
// trials, durable profile promotion, the policy allowlist, its health, warm jobs and supersession of stale jobs.
//
// One job runs at a time: run, promotion and warm all stage files in the same module cache and job directory scope,
// so the queue serializes the durable-state touches too. A caller that arrives while another job runs waits for its
// turn, in priority order, for as long as its own request lives (TEST_AGENT_ARCH 8); it is never refused for being busy.
func (r *Runner) Handler() http.Handler {
	mux := http.NewServeMux()
	queue := &jobQueue{}
	mux.Handle("POST /v1/run", queued(queue, runKey, func(w http.ResponseWriter, req *http.Request) {
		out, status := r.handleRun(req)
		writeResponse(w, status, out)
	}))
	mux.HandleFunc("GET /v1/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, healthView{Kinds: runKinds()})
	})
	mux.HandleFunc("GET /v1/profiles", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, profileView{Names: r.Names()})
	})
	mux.HandleFunc("GET /v1/policy", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, policyView{Images: r.AllowedImages()})
	})
	mux.Handle("POST /v1/profiles", queued(queue, fixedKey(ports.PriorityGate), func(w http.ResponseWriter, req *http.Request) {
		out, status := r.handleProfilesPush(req)
		writeResponse(w, status, out)
	}))
	mux.Handle("POST /v1/warm", queued(queue, fixedKey(ports.PriorityOnDemand), func(w http.ResponseWriter, req *http.Request) {
		out, status := r.handleWarm(req)
		writeResponse(w, status, out)
	}))
	mux.Handle("POST /v1/history", queued(queue, fixedKey(ports.PriorityOnDemand), func(w http.ResponseWriter, req *http.Request) {
		out, status := r.handleHistory(req)
		writeResponse(w, status, out)
	}))
	mux.HandleFunc("POST /v1/supersede", func(w http.ResponseWriter, req *http.Request) {
		out, status := handleSupersede(queue, req)
		writeResponse(w, status, out)
	})
	return mux
}

// Request headers that tell the queue where a job waits, so a waiting caller holds no request body in memory. The
// run handler checks them against the decoded job and refuses a request whose headers claim a better place.
const (
	headerPriority = "X-Heimdall-Priority"
	headerRepo     = "X-Heimdall-Repo"
	headerChange   = "X-Heimdall-Change"
	headerHead     = "X-Heimdall-Head"
)

// queued admits a request through the queue before its handler reads the body.
func queued(q *jobQueue, keyOf func(*http.Request) (jobKey, error), handle http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		key, err := keyOf(req)
		if err != nil {
			writeResponse(w, http.StatusBadRequest, response{Error: "invalid queue headers"})
			return
		}
		ctx, release, err := q.Acquire(req.Context(), key)
		if err != nil {
			status := http.StatusServiceUnavailable
			if errors.Is(err, ErrSuperseded) {
				status = http.StatusConflict
			}
			writeResponse(w, status, response{Error: err.Error()})
			return
		}
		defer release()
		handle(w, req.WithContext(ctx))
	}
}

func fixedKey(p ports.Priority) func(*http.Request) (jobKey, error) {
	return func(*http.Request) (jobKey, error) { return jobKey{Priority: p}, nil }
}

// runKey reads the queue headers of a run request.
func runKey(req *http.Request) (jobKey, error) {
	p, err := strconv.Atoi(req.Header.Get(headerPriority))
	if err != nil || p < int(ports.PriorityGate) || p > int(ports.PriorityPreview) {
		return jobKey{}, errors.New("priority header is missing or out of range")
	}
	key := jobKey{Priority: ports.Priority(p)}
	if raw := req.Header.Get(headerRepo); raw != "" {
		repo, err := change.ParseRepo(raw)
		if err != nil {
			return jobKey{}, fmt.Errorf("repo header: %w", err)
		}
		key.Repo = repo
	}
	if raw := req.Header.Get(headerChange); raw != "" {
		ch, err := id.ParseChangeID(raw)
		if err != nil {
			return jobKey{}, fmt.Errorf("change header: %w", err)
		}
		key.Change = ch
	}
	if raw := req.Header.Get(headerHead); raw != "" {
		sha, err := id.ParseSHA(raw)
		if err != nil {
			return jobKey{}, fmt.Errorf("head header: %w", err)
		}
		key.Head = sha
	}
	return key, nil
}

// jobKeyOf is the key a decoded job really has. The headers must not claim a better place than this.
func jobKeyOf(job ports.SandboxJob) jobKey {
	return jobKey{Priority: job.Priority(), Repo: job.Repo, Change: job.Change, Head: job.HeadSHA()}
}

type supersedeRequest struct {
	Repo   change.Repo `json:"repo"`
	Change id.ChangeID `json:"change"`
	Head   id.SHA      `json:"head"`
}

// handleSupersede cancels the background jobs of a change that served an older head. It never waits in the queue:
// it must be able to cancel the very job that is holding it.
func handleSupersede(q *jobQueue, req *http.Request) (response, int) {
	body, err := io.ReadAll(io.LimitReader(req.Body, 4<<10))
	var in supersedeRequest
	if err != nil || json.Unmarshal(body, &in) != nil || in.Change == "" {
		return response{Error: "invalid supersede request"}, http.StatusBadRequest
	}
	if _, err := id.ParseChangeID(string(in.Change)); err != nil {
		return response{Error: "invalid supersede request"}, http.StatusBadRequest
	}
	if _, err := id.ParseSHA(string(in.Head)); err != nil {
		return response{Error: "invalid supersede request"}, http.StatusBadRequest
	}
	if _, err := change.ParseRepo(string(in.Repo)); err != nil {
		return response{Error: "invalid supersede request"}, http.StatusBadRequest
	}
	q.Supersede(in.Repo, in.Change, in.Head)
	return response{}, http.StatusOK
}

func (r *Runner) decodeBody(req *http.Request, limit int64) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(req.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read request body: %w", err)
	}
	if len(body) > int(limit) {
		return nil, errors.New("request too large")
	}
	return body, nil
}

// handleRun executes one isolated job. A profile_trial job names a proposed artifact, which the
// runner validates like any promotion but never persists; every other job runs under the built-in
// go profile or a promoted profile.
func (r *Runner) handleRun(req *http.Request) (response, int) {
	body, err := r.decodeBody(req, apiBodyLimit)
	if err != nil {
		return response{}, http.StatusBadRequest
	}
	in, err := decodeRunRequest(body)
	if err != nil {
		return response{Error: "invalid request"}, http.StatusBadRequest
	}
	if err := in.Job.Validate(); err != nil {
		return response{Error: "invalid job"}, http.StatusBadRequest
	}
	spec, status, err := r.runScope(in)
	if err != nil {
		return response{Error: err.Error()}, status
	}
	if status != http.StatusOK {
		return response{Error: "profile scope refused"}, status
	}
	if err := validateOverlays(in.Overlays); err != nil {
		return response{Error: err.Error()}, http.StatusBadRequest
	}
	if status, err := checkJobKind(in); err != nil {
		return response{Error: err.Error()}, status
	}
	if claimed, err := runKey(req); err != nil || claimed != jobKeyOf(in.Job) {
		return response{Error: "queue headers do not match the job"}, http.StatusBadRequest
	}
	return r.runRequest(req.Context(), in, spec)
}

// runRequest runs a validated request and shapes its answer.
func (r *Runner) runRequest(ctx context.Context, in request, spec *RepoProfile) (response, int) {
	job := JobInput{
		Profile: in.Profile, Spec: spec, Archive: bytes.NewReader(in.Archive),
		Overlays: in.Overlays, Dir: in.Job.Dir, Repo: in.Job.Repo, CheckID: in.Job.CheckID, TestKind: in.Job.TestKind, Timeout: in.Job.Timeout,
	}
	if len(in.BaseArchive) > 0 {
		job.BaseArchive = bytes.NewReader(in.BaseArchive)
	}
	out, runErr := r.Run(ctx, job)
	if isProfileCommandJob(in.Job.Kind) {
		return profileJobResponse(in.Job.Kind, out, runErr)
	}
	if validTestOutput(out) {
		return response{Output: out}, http.StatusOK
	}
	if runErr != nil {
		return response{Error: sandboxCause(out, runErr)}, http.StatusUnprocessableEntity
	}
	return response{Error: "runner produced no test results"}, http.StatusUnprocessableEntity
}

func isProfileCommandJob(kind ports.SandboxKind) bool {
	return kind == ports.SandboxCheck || kind == ports.SandboxTest
}

// checkJobKind refuses a job kind the runner does not execute through /v1/run, and the inputs a kind may not carry.
func checkJobKind(in request) (int, error) {
	kind := in.Job.Kind
	if !slices.Contains(runKinds(), kind) {
		return http.StatusBadRequest, errors.New("the runner does not run this job kind")
	}
	if len(in.BaseArchive) > 0 && !isProfileCommandJob(kind) {
		return http.StatusBadRequest, errors.New("only a check or test job carries a base tree")
	}
	if kind == ports.SandboxCheck && len(in.Overlays) > 0 {
		return http.StatusBadRequest, errors.New("a check job takes no staged files")
	}
	return http.StatusOK, nil
}

// profileJobResponse turns the wrapper output of a check or test job into a response. A job that printed no trailer,
// or a check that wrote no JSON result, is refused: the runner never reports a result it did not see.
func profileJobResponse(kind ports.SandboxKind, out []byte, runErr error) (response, int) {
	parsed, ok := parseJobOutput(out)
	switch {
	case !ok && runErr != nil:
		return response{Error: sandboxCause(out, runErr)}, http.StatusUnprocessableEntity
	case !ok:
		return response{Error: "runner produced no job result"}, http.StatusUnprocessableEntity
	case len(bytes.TrimSpace(parsed.Result)) == 0:
		return response{Error: "job wrote no result file (exit " + strconv.Itoa(parsed.Exit) + ")"}, http.StatusUnprocessableEntity
	case len(parsed.Result) > ports.MaxJobResult:
		return response{Error: "job result exceeds its bound"}, http.StatusUnprocessableEntity
	case kind == ports.SandboxCheck && !json.Valid(parsed.Result):
		return response{Error: "check result is not valid JSON"}, http.StatusUnprocessableEntity
	}
	return response{Output: out}, http.StatusOK
}

// maxFailureCause bounds the runtime detail carried back to the server. The job output is the only
// place the underlying cause appears (Podman writes it there), so a bounded tail keeps the failure
// actionable without letting an untrusted job inflate the error response.
const maxFailureCause = 2 << 10

// sandboxCause reports why a job produced no test output. It prefers the job output tail, which
// carries the runtime's own explanation, and falls back to the executor error when it is empty.
func sandboxCause(out []byte, err error) string {
	detail := strings.TrimSpace(string(out))
	if len(detail) > maxFailureCause {
		detail = detail[len(detail)-maxFailureCause:]
	}
	if detail == "" {
		detail = err.Error()
	}
	return "sandbox execution failed: " + detail
}

// runScope validates the profile scope of a run request and, for a trial, returns the proposed
// spec after checking it against the runner's own allowlist. It never mutates durable state.
func (r *Runner) runScope(in request) (*RepoProfile, int, error) {
	switch {
	case in.Job.Kind == ports.SandboxProfileTrial:
		if len(in.Trial) == 0 || len(in.Trial) > maxProfilePayload {
			return nil, http.StatusBadRequest, errors.New("profile trial requires a bounded proposed profile artifact")
		}
		if !validProfileName(in.Profile) {
			return nil, http.StatusBadRequest, errors.New("profile trial requires a valid profile name")
		}
		spec, err := ParseApprovedProfile(in.Trial, r.AllowedImages())
		if err != nil {
			return nil, http.StatusBadRequest, err
		}
		return &spec, http.StatusOK, nil
	case in.Profile == "go" || in.Profile == "":
		return nil, http.StatusOK, nil
	default:
		if _, ok := r.specNamed(in.Profile); !ok {
			return nil, http.StatusBadRequest, errors.New("unknown or unapproved profile")
		}
		return nil, http.StatusOK, nil
	}
}

func decodeRunRequest(body []byte) (request, error) {
	var in request
	if err := json.Unmarshal(body, &in); err != nil {
		return request{}, err
	}
	return in, nil
}

// handleProfilesPush receives one promoted artifact from the server: it is validated like a fresh
// proposal against the runner's own allowlist and then only promoted once the check passes.
func (r *Runner) handleProfilesPush(req *http.Request) (response, int) {
	body, err := r.decodeBody(req, maxProfilePayload)
	if err != nil {
		return response{}, http.StatusBadRequest
	}
	var push profilePushRequest
	if err := json.Unmarshal(body, &push); err != nil {
		return response{Error: "invalid profile push"}, http.StatusBadRequest
	}
	if _, err := r.Activate(push.Name, push.Artifact); err != nil {
		return response{Error: err.Error()}, http.StatusBadRequest
	}
	return response{}, http.StatusOK
}

// handleWarm stages the module cache for one repository commit under the profile's fetch rules.
func (r *Runner) handleWarm(req *http.Request) (response, int) {
	body, err := r.decodeBody(req, apiBodyLimit)
	if err != nil {
		return response{Error: "read request"}, http.StatusBadRequest
	}
	in, err := decodeRunRequest(body)
	if err != nil {
		return response{Error: "invalid request"}, http.StatusBadRequest
	}
	if in.Job.Kind != ports.SandboxWarm {
		return response{Error: "warm requires a warm job"}, http.StatusBadRequest
	}
	if err := in.Job.Validate(); err != nil {
		return response{Error: "invalid job"}, http.StatusBadRequest
	}
	summary, err := r.Warm(req.Context(), in.Profile, bytes.NewReader(in.Archive))
	if err != nil {
		return response{Error: err.Error()}, warmStatus(err)
	}
	return response{Summary: &summary}, http.StatusOK
}

// warmStatus keeps the failure classes apart: a validation problem is a 4xx, a fetched-dependency
// problem is the job's own outcome (422), never a runner crash.
func warmStatus(err error) int {
	switch {
	case errors.Is(err, ErrProfileNotFound), errors.Is(err, ErrFetchHostDisallowed):
		return http.StatusBadRequest
	case errors.Is(err, ErrNoModules):
		return http.StatusUnprocessableEntity
	default:
		return http.StatusInternalServerError
	}
}

func validTestOutput(output []byte) bool {
	if len(output) == 0 {
		return false
	}
	report, _, err := testevents.ParseEvents(bytes.NewReader(output))
	return err == nil && len(report.Cases()) > 0
}

func writeResponse(w http.ResponseWriter, status int, out response) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(out)
}

func writeJSON(w http.ResponseWriter, status int, out any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(out)
}

// Listen serves the API on a filesystem-protected Unix socket.
func Listen(ctx context.Context, socket string, handler http.Handler) error {
	if socket == "" {
		return errors.New("runner socket path is required")
	}
	if err := os.Remove(socket); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove stale runner socket: %w", err)
	}
	listener, err := (&net.ListenConfig{}).Listen(ctx, "unix", socket)
	if err != nil {
		return fmt.Errorf("listen on runner socket: %w", err)
	}
	// #nosec G302 -- the server is a distinct Unix user and reaches this socket through the dedicated heimdall group.
	if err := os.Chmod(socket, 0o660); err != nil {
		_ = listener.Close()
		return fmt.Errorf("set runner socket permissions: %w", err)
	}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, MaxHeaderBytes: 16 << 10}
	go func() { <-ctx.Done(); _ = server.Close() }()
	if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve runner API: %w", err)
	}
	return nil
}

// Source provides a tarball for the server-side client adapter.
type Source interface {
	Tarball(context.Context, change.Repo, id.SHA) (io.ReadCloser, error)
}

// Client is the server-side Sandbox client. It never receives a container runtime socket.
type Client struct {
	Socket string
	Source Source
	Blobs  ports.BlobStore
}

var (
	_ ports.Sandbox              = Client{}
	_ ports.ProfileTrialRunner   = Client{}
	_ ports.ProfilePolicySource  = Client{}
	_ ports.ProfileSyncer        = Client{}
	_ ports.ProfileWarmer        = Client{}
	_ ports.SandboxSuperseder    = Client{}
	_ ports.ProfileHistoryWarmer = Client{}
)

// Run submits one commit archive through the protected Unix socket and stores its bounded log as a blob.
func (c Client) Run(ctx context.Context, job ports.SandboxJob) (ports.SandboxResult, error) {
	return c.RunOverlay(ctx, job, nil)
}

// RunOverlay submits generated acceptance files with the source archive. A profile name on the job
// selects a promoted profile; the runner refuses unknown names, so the production flow fails closed.
func (c Client) RunOverlay(ctx context.Context, job ports.SandboxJob, overlays []ports.File) (ports.SandboxResult, error) {
	if err := c.validateJob(job); err != nil {
		return ports.SandboxResult{}, err
	}
	data, err := c.loadArchive(ctx, job)
	if err != nil {
		return ports.SandboxResult{}, err
	}
	if err := validateOverlays(overlays); err != nil {
		return ports.SandboxResult{}, err
	}
	req := request{Profile: runnerProfile(job.Profile), Job: job, Archive: data, Overlays: overlays}
	if job.BaseSHA != "" {
		if req.BaseArchive, err = c.loadTree(ctx, job.Repo, job.BaseSHA, maxBaseArchive); err != nil {
			return ports.SandboxResult{}, err
		}
	}
	body, err := json.Marshal(req)
	if err != nil {
		return ports.SandboxResult{}, fmt.Errorf("encode runner request: %w", err)
	}
	resp, err := c.postRun(ctx, job, body)
	if err != nil {
		return ports.SandboxResult{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	return c.result(ctx, job, resp)
}

// runnerProfile names the profile a job runs under. Empty selects the built-in Go profile; a set
// profile name has been resolved from the durable promoted state by the caller.
func runnerProfile(profile string) string {
	if profile == "" {
		return "go"
	}
	return profile
}

// profileTrialTimeout is the trial job budget: the proposal's own acceptance command, run against
// the repository, gets the same 10 minute cap TECH_SPEC 12.2 allows any job.
const profileTrialTimeout = 10 * time.Minute

// Trial sends the proposed profile artifact with a profile_trial job; the runner parses, validates
// and executes the proposal in a one-off job without promoting it.
func (c Client) Trial(ctx context.Context, j ports.ProfileTrialJob) (ports.SandboxResult, error) {
	if err := j.Validate(); err != nil {
		return ports.SandboxResult{}, fmt.Errorf("invalid profile trial job: %w", err)
	}
	job := ports.SandboxJob{Kind: ports.SandboxProfileTrial, Repo: j.Repo, SHA: j.SHA, Dir: j.TestsDir, Timeout: profileTrialTimeout}
	data, err := c.loadArchive(ctx, job)
	if err != nil {
		return ports.SandboxResult{}, err
	}
	body, err := json.Marshal(request{Profile: j.Name, Job: job, Archive: data, Trial: j.Artifact, TrialFrom: string(j.Repo)})
	if err != nil {
		return ports.SandboxResult{}, fmt.Errorf("encode trial request: %w", err)
	}
	resp, err := c.postRun(ctx, job, body)
	if err != nil {
		return ports.SandboxResult{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	return c.result(ctx, job, resp)
}

// Health asks the runner which job kinds it runs. An error means the runner is unreachable or answered badly.
func (c Client) Health(ctx context.Context) ([]ports.SandboxKind, error) {
	if c.Socket == "" {
		return nil, errors.New("runner socket path is required")
	}
	resp, err := c.get(ctx, "/v1/health")
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("runner health: status %d", resp.StatusCode)
	}
	var view healthView
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&view); err != nil {
		return nil, fmt.Errorf("decode runner health: %w", err)
	}
	return view.Kinds, nil
}

// Profiles lists the profile names the runner can currently run.
func (c Client) Profiles(ctx context.Context) ([]string, error) {
	resp, err := c.get(ctx, "/v1/profiles")
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("runner profile list: status %d", resp.StatusCode)
	}
	var view profileView
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&view); err != nil {
		return nil, fmt.Errorf("decode runner profile list: %w", err)
	}
	return view.Names, nil
}

type profilePushRequest struct {
	Name     string `json:"name"`
	Artifact []byte `json:"artifact"`
}

// SyncProfile pushes one promoted artifact into the runner's durable promoted directory.
func (c Client) SyncProfile(ctx context.Context, name string, artifact []byte) error {
	if !validProfileName(name) || len(artifact) == 0 || len(artifact) > maxProfilePayload {
		return errors.New("invalid profile sync request")
	}
	body, err := json.Marshal(profilePushRequest{Name: name, Artifact: artifact})
	if err != nil {
		return fmt.Errorf("encode profile sync request: %w", err)
	}
	resp, err := c.post(ctx, "/v1/profiles", 30*time.Second, body)
	if err != nil {
		return fmt.Errorf("push promoted profile to runner: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		var out response
		_ = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out)
		return fmt.Errorf("runner refused promoted profile: %s", out.Error)
	}
	return nil
}

// AllowedImages returns the image allowlist the runner enforces, so a proposal is validated against
// the same list the runner checks at trial and promotion time.
func (c Client) AllowedImages(ctx context.Context) ([]string, error) {
	resp, err := c.get(ctx, "/v1/policy")
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("runner policy list: status %d", resp.StatusCode)
	}
	var view policyView
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&view); err != nil {
		return nil, fmt.Errorf("decode runner policy: %w", err)
	}
	if len(view.Images) == 0 {
		return nil, errors.New("runner reports an empty image allowlist")
	}
	return view.Images, nil
}

// Warm asks the runner to stage a repository's module cache under the profile's fetch_hosts rules.
func (c Client) Warm(ctx context.Context, req ports.ProfileWarmRequest) (ports.ProfileWarmResult, error) {
	if err := req.Validate(); err != nil {
		return ports.ProfileWarmResult{}, fmt.Errorf("invalid warm request: %w", err)
	}
	job := ports.SandboxJob{Kind: ports.SandboxWarm, Repo: req.Repo, SHA: req.SHA, Timeout: warmJobTimeout}
	data, err := c.loadArchive(ctx, job)
	if err != nil {
		return ports.ProfileWarmResult{}, err
	}
	body, err := json.Marshal(request{Profile: req.Profile, Job: job, Archive: data})
	if err != nil {
		return ports.ProfileWarmResult{}, fmt.Errorf("encode warm request: %w", err)
	}
	resp, err := c.post(ctx, "/v1/warm", job.Timeout, body)
	if err != nil {
		return ports.ProfileWarmResult{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	var out response
	if err := json.NewDecoder(io.LimitReader(resp.Body, apiBodyLimit)).Decode(&out); err != nil {
		return ports.ProfileWarmResult{}, fmt.Errorf("decode runner warm response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return ports.ProfileWarmResult{}, fmt.Errorf("runner refused warm job: %s", out.Error)
	}
	if out.Summary == nil {
		return ports.ProfileWarmResult{}, errors.New("runner warm response carries no summary")
	}
	return ports.ProfileWarmResult{Modules: out.Summary.Modules, FetchedBytes: out.Summary.Bytes, Warnings: out.Summary.Warnings}, nil
}

// warmJobTimeout bounds a warm job. It shares the job cap so a stuck fetch job cannot widen it.
const warmJobTimeout = 10 * time.Minute

func (c Client) get(ctx context.Context, path string) (*http.Response, error) {
	transport := &http.Transport{DialContext: c.dial()}
	client := &http.Client{Transport: transport, Timeout: 30 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://runner"+path, nil)
	if err != nil {
		transport.CloseIdleConnections()
		return nil, fmt.Errorf("create runner request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		transport.CloseIdleConnections()
		return nil, fmt.Errorf("call runner socket: %w", err)
	}
	return resp, nil
}

func (c Client) dial() func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", c.Socket)
	}
}

func (c Client) validateJob(job ports.SandboxJob) error {
	if err := job.Validate(); err != nil {
		return fmt.Errorf("invalid sandbox job: %w", err)
	}
	if c.Source == nil || c.Blobs == nil {
		return errors.New("runner source and blob store are required")
	}
	if c.Socket == "" {
		return errors.New("runner socket path is required")
	}
	return nil
}

func (c Client) loadArchive(ctx context.Context, job ports.SandboxJob) ([]byte, error) {
	return c.loadTree(ctx, job.Repo, job.SHA, maxSourceArchive)
}

// loadTree fetches the tarball of one commit, refusing one larger than limit.
func (c Client) loadTree(ctx context.Context, repo change.Repo, sha id.SHA, limit int64) ([]byte, error) {
	archive, err := c.Source.Tarball(ctx, repo, sha)
	if err != nil {
		return nil, fmt.Errorf("fetch sandbox source: %w", err)
	}
	defer func() { _ = archive.Close() }()
	data, err := io.ReadAll(io.LimitReader(archive, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read sandbox source: %w", err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("sandbox source exceeds %d MiB", limit>>20)
	}
	return data, nil
}

func validateOverlays(overlays []ports.File) error {
	if len(overlays) > 256 {
		return errors.New("too many test overlay files")
	}
	var total int
	for _, overlay := range overlays {
		if err := overlay.Validate(); err != nil {
			return fmt.Errorf("invalid test overlay: %w", err)
		}
		total += len(overlay.Content)
		if total > 8<<20 {
			return errors.New("test overlays exceed 8 MiB")
		}
	}
	return nil
}

// maxQueueWait bounds how long a caller's connection may wait for its turn before the job even starts. The caller's own
// context is the deadline that matters; this only keeps the HTTP client from giving up first.
const maxQueueWait = 30 * time.Minute

func (c Client) post(ctx context.Context, path string, timeout time.Duration, body []byte) (*http.Response, error) {
	return c.send(ctx, path, timeout, body, nil)
}

// postRun posts one job and tells the runner's queue where it waits. The runner checks the headers against the job.
func (c Client) postRun(ctx context.Context, job ports.SandboxJob, body []byte) (*http.Response, error) {
	key := jobKeyOf(job)
	return c.send(ctx, "/v1/run", job.Timeout+maxQueueWait, body, map[string]string{
		headerPriority: strconv.Itoa(int(key.Priority)),
		headerRepo:     string(key.Repo),
		headerChange:   string(key.Change),
		headerHead:     string(key.Head),
	})
}

func (c Client) send(ctx context.Context, path string, timeout time.Duration, body []byte, headers map[string]string) (*http.Response, error) {
	transport := &http.Transport{DialContext: c.dial()}
	client := &http.Client{Transport: transport, Timeout: timeout + 10*time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://runner"+path, bytes.NewReader(body))
	if err != nil {
		transport.CloseIdleConnections()
		return nil, fmt.Errorf("create runner request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for name, value := range headers {
		if value != "" {
			req.Header.Set(name, value)
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		transport.CloseIdleConnections()
		return nil, fmt.Errorf("call runner socket: %w", err)
	}
	return resp, nil
}

func (c Client) result(ctx context.Context, job ports.SandboxJob, resp *http.Response) (ports.SandboxResult, error) {
	var out response
	if err := json.NewDecoder(io.LimitReader(resp.Body, apiBodyLimit)).Decode(&out); err != nil {
		return ports.SandboxResult{}, fmt.Errorf("decode runner response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return ports.SandboxResult{}, fmt.Errorf("runner rejected job: %s", out.Error)
	}
	if isProfileCommandJob(job.Kind) {
		return c.profileJobResult(ctx, job, out.Output)
	}
	report, _, err := testevents.ParseEvents(bytes.NewReader(out.Output))
	if err != nil {
		return ports.SandboxResult{}, fmt.Errorf("parse runner test output: %w", err)
	}
	logRef, err := c.Blobs.Put(ctx, ports.BlobLog, bytes.NewReader(out.Output))
	if err != nil {
		return ports.SandboxResult{}, fmt.Errorf("store runner log: %w", err)
	}
	return testevents.ReportToSandboxResult(report, workflow.BlobRef(logRef)), nil
}

// profileJobResult stores the bounded log and returns the result file: SARIF as it is for a check, parsed JUnit for a
// test kind. Both are untrusted content; the caller's reader validates them.
func (c Client) profileJobResult(ctx context.Context, job ports.SandboxJob, output []byte) (ports.SandboxResult, error) {
	parsed, ok := parseJobOutput(output)
	if !ok {
		return ports.SandboxResult{}, errors.New("runner job output has no result trailer")
	}
	if len(parsed.Result) > ports.MaxJobResult {
		return ports.SandboxResult{}, errors.New("runner job result exceeds its bound")
	}
	logRef, err := c.Blobs.Put(ctx, ports.BlobLog, bytes.NewReader(parsed.Log))
	if err != nil {
		return ports.SandboxResult{}, fmt.Errorf("store runner log: %w", err)
	}
	if job.Kind == ports.SandboxCheck {
		return ports.SandboxResult{LogRef: workflow.BlobRef(logRef), Sarif: parsed.Result, Exit: parsed.Exit}, nil
	}
	report, err := verify.ParseBytes(parsed.Result)
	if err != nil {
		return ports.SandboxResult{}, fmt.Errorf("parse runner JUnit result: %w", err)
	}
	res := testevents.ReportToSandboxResult(report, workflow.BlobRef(logRef))
	res.Exit = parsed.Exit
	return res, nil
}

// Supersede tells the runner that a change has a new head, so it cancels the queued and running background jobs that
// served an older one (TEST_AGENT_ARCH 8, rule 3).
func (c Client) Supersede(ctx context.Context, repo change.Repo, ch id.ChangeID, head id.SHA) error {
	if c.Socket == "" {
		return errors.New("runner socket path is required")
	}
	body, err := json.Marshal(supersedeRequest{Repo: repo, Change: ch, Head: head})
	if err != nil {
		return fmt.Errorf("encode supersede request: %w", err)
	}
	resp, err := c.post(ctx, "/v1/supersede", 30*time.Second, body)
	if err != nil {
		return fmt.Errorf("supersede runner jobs: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("runner refused supersede: status %d", resp.StatusCode)
	}
	return nil
}

// handleHistory creates or updates the history mirror of one repository. Like the module warm it is a fetch job: the
// only kind with egress.
func (r *Runner) handleHistory(req *http.Request) (response, int) {
	body, err := r.decodeBody(req, 64<<10)
	if err != nil {
		return response{Error: "read request"}, http.StatusBadRequest
	}
	in, err := decodeRunRequest(body)
	if err != nil || in.Job.Kind != ports.SandboxWarm {
		return response{Error: "history requires a warm job"}, http.StatusBadRequest
	}
	if err := in.Job.Validate(); err != nil {
		return response{Error: "invalid job"}, http.StatusBadRequest
	}
	hist := ports.ProfileHistoryRequest{ProfileWarmRequest: ports.ProfileWarmRequest{Repo: in.Job.Repo, SHA: in.Job.SHA, Profile: in.Profile}, Token: in.Token}
	if err := hist.Validate(); err != nil {
		return response{Error: "invalid history request"}, http.StatusBadRequest
	}
	res, err := r.WarmHistory(req.Context(), in.Profile, in.Job.Repo, in.Token)
	if err != nil {
		return response{Error: err.Error()}, historyStatus(err)
	}
	return response{History: &res}, http.StatusOK
}

// historyStatus keeps a refusal of the profile (4xx) apart from a fetch failure (the job's own outcome).
func historyStatus(err error) int {
	switch {
	case errors.Is(err, ErrProfileNotFound), errors.Is(err, ErrHistoryNotAllowed):
		return http.StatusBadRequest
	default:
		return http.StatusUnprocessableEntity
	}
}

// WarmHistory asks the runner to create or update a repository's history mirror under the profile's fetch_hosts.
func (c Client) WarmHistory(ctx context.Context, req ports.ProfileHistoryRequest) (ports.ProfileHistoryResult, error) {
	if err := req.Validate(); err != nil {
		return ports.ProfileHistoryResult{}, fmt.Errorf("invalid history request: %w", err)
	}
	if req.Profile == "" {
		return ports.ProfileHistoryResult{}, errors.New("a history mirror needs a promoted profile")
	}
	job := ports.SandboxJob{Kind: ports.SandboxWarm, Repo: req.Repo, SHA: req.SHA, Timeout: historyTimeout}
	body, err := json.Marshal(request{Profile: req.Profile, Job: job, Token: req.Token})
	if err != nil {
		return ports.ProfileHistoryResult{}, fmt.Errorf("encode history request: %w", err)
	}
	resp, err := c.post(ctx, "/v1/history", job.Timeout+maxQueueWait, body)
	if err != nil {
		return ports.ProfileHistoryResult{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	var out response
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return ports.ProfileHistoryResult{}, fmt.Errorf("decode runner history response: %w", err)
	}
	if resp.StatusCode != http.StatusOK || out.History == nil {
		return ports.ProfileHistoryResult{}, fmt.Errorf("runner refused history job: %s", out.Error)
	}
	return *out.History, nil
}
