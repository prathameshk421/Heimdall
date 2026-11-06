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
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/testevents"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

const (
	apiBodyLimit = 98 << 20
	// maxProfilePayload bounds one proposed or promoted profile artifact: the same bound the
	// artifact itself carries.
	maxProfilePayload = 64 << 10
)

type request struct {
	Profile  string           `json:"profile"`
	Job      ports.SandboxJob `json:"job"`
	Archive  []byte           `json:"archive"`
	Overlays []ports.File     `json:"overlays,omitempty"`
	// Trial carries the proposed profile artifact of a SandboxProfileTrial job. The runner parses
	// and validates it against its own allowlist and runs it outside the promoted set.
	Trial []byte `json:"trial,omitempty"`
	// TrialFrom names the repository the proposal belongs to, for the audit trail only.
	TrialFrom string `json:"trial_from,omitempty"`
}

// WarmSummary is the bounded result of one warm job: the staged modules and the fetched totals.
type response struct {
	Output  []byte       `json:"output,omitempty"`
	Summary *WarmSummary `json:"summary,omitempty"`
	Error   string       `json:"error,omitempty"`
}

// profileView is the GET /v1/profiles body.
type profileView struct {
	Names []string `json:"names"`
}

// policyView is the GET /v1/policy body: the image allowlist the runner enforces.
type policyView struct {
	Images []string `json:"images"`
}

// Handler exposes the narrow job API to the Unix socket only: isolated runs, proposed-profile
// trials, durable profile promotion, the policy allowlist and warm jobs.
func (r *Runner) Handler() http.Handler {
	mux := http.NewServeMux()
	slot := make(chan struct{}, 1)
	// One job at a time: run, promotion and warm all stage files in the same module cache and job
	// directory scope, so the slot serializes the durable-state touches too.
	bounded := func(mutate func(w http.ResponseWriter, req *http.Request)) http.HandlerFunc {
		return func(w http.ResponseWriter, req *http.Request) {
			if !acquireSlot(slot) {
				writeResponse(w, http.StatusServiceUnavailable, response{Error: "runner is busy"})
				return
			}
			defer func() { <-slot }()
			mutate(w, req)
		}
	}
	mux.Handle("POST /v1/run", bounded(func(w http.ResponseWriter, req *http.Request) {
		out, status := r.handleRun(req)
		writeResponse(w, status, out)
	}))
	mux.HandleFunc("GET /v1/profiles", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, profileView{Names: r.Names()})
	})
	mux.HandleFunc("GET /v1/policy", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, policyView{Images: r.AllowedImages()})
	})
	mux.Handle("POST /v1/profiles", bounded(func(w http.ResponseWriter, req *http.Request) {
		out, status := r.handleProfilesPush(req)
		writeResponse(w, status, out)
	}))
	mux.Handle("POST /v1/warm", bounded(func(w http.ResponseWriter, req *http.Request) {
		out, status := r.handleWarm(req)
		writeResponse(w, status, out)
	}))
	return mux
}

func acquireSlot(slot chan struct{}) bool {
	select {
	case slot <- struct{}{}:
		return true
	default:
		return false
	}
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
	out, runErr := r.Run(req.Context(), JobInput{
		Profile: in.Profile, Spec: spec, Archive: bytes.NewReader(in.Archive),
		Overlays: in.Overlays, Dir: in.Job.Dir, Timeout: in.Job.Timeout,
	})
	if validTestOutput(out) {
		return response{Output: out}, http.StatusOK
	}
	if runErr != nil {
		return response{Error: "sandbox execution failed"}, http.StatusUnprocessableEntity
	}
	return response{Error: "runner produced no test results"}, http.StatusUnprocessableEntity
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
	_ ports.Sandbox             = Client{}
	_ ports.ProfileTrialRunner  = Client{}
	_ ports.ProfilePolicySource = Client{}
	_ ports.ProfileSyncer       = Client{}
	_ ports.ProfileWarmer       = Client{}
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
	body, err := json.Marshal(request{Profile: runnerProfile(job.Profile), Job: job, Archive: data, Overlays: overlays})
	if err != nil {
		return ports.SandboxResult{}, fmt.Errorf("encode runner request: %w", err)
	}
	resp, err := c.post(ctx, "/v1/run", job.Timeout, body)
	if err != nil {
		return ports.SandboxResult{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	return c.result(ctx, resp)
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
	resp, err := c.post(ctx, "/v1/run", job.Timeout, body)
	if err != nil {
		return ports.SandboxResult{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	return c.result(ctx, resp)
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
	archive, err := c.Source.Tarball(ctx, job.Repo, job.SHA)
	if err != nil {
		return nil, fmt.Errorf("fetch sandbox source: %w", err)
	}
	defer func() { _ = archive.Close() }()
	data, err := io.ReadAll(io.LimitReader(archive, (64<<20)+1))
	if err != nil {
		return nil, fmt.Errorf("read sandbox source: %w", err)
	}
	if len(data) > 64<<20 {
		return nil, errors.New("sandbox source exceeds 64 MiB")
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

func (c Client) post(ctx context.Context, path string, timeout time.Duration, body []byte) (*http.Response, error) {
	transport := &http.Transport{DialContext: c.dial()}
	client := &http.Client{Transport: transport, Timeout: timeout + 10*time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://runner"+path, bytes.NewReader(body))
	if err != nil {
		transport.CloseIdleConnections()
		return nil, fmt.Errorf("create runner request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		transport.CloseIdleConnections()
		return nil, fmt.Errorf("call runner socket: %w", err)
	}
	return resp, nil
}

func (c Client) result(ctx context.Context, resp *http.Response) (ports.SandboxResult, error) {
	var out response
	if err := json.NewDecoder(io.LimitReader(resp.Body, apiBodyLimit)).Decode(&out); err != nil {
		return ports.SandboxResult{}, fmt.Errorf("decode runner response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return ports.SandboxResult{}, fmt.Errorf("runner rejected job: %s", out.Error)
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
