package e2e

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

// LocalConfig configures the world that runs on the laptop against real systems.
type LocalConfig struct {
	Story          *Story
	Agents         string // scripted or live
	ServerURL      string // heimdall-server, for example http://127.0.0.1:8080; empty means none is configured
	ForwardAddrs   []string
	HeimdallBinary string
	GitHubAPI      string
	Poll           time.Duration
	// Token returns the token for a GitHub login. It is asked on every call; the simulator never relies on the
	// active gh account (MVP_SCENARIOS section 2).
	Token func(login string) (string, error)
}

// LocalWorld is the laptop world: the server and the scripted agent run here, and Postgres, Docker, GitHub and
// Jira are real. Drivers that later tasks build return NotBuiltError until then.
type LocalWorld struct {
	cfg  LocalConfig
	http *http.Client
}

// NewLocalWorld builds the laptop world, filling in defaults.
func NewLocalWorld(cfg LocalConfig) *LocalWorld {
	if cfg.Poll <= 0 {
		cfg.Poll = 500 * time.Millisecond
	}
	if cfg.GitHubAPI == "" {
		cfg.GitHubAPI = "https://api.github.com"
	}
	if cfg.Token == nil {
		cfg.Token = ghToken
	}
	return &LocalWorld{cfg: cfg, http: &http.Client{Timeout: 10 * time.Second}}
}

// ghToken asks the gh keyring for one login's token. The login is always named, so the active account is irrelevant.
func ghToken(login string) (string, error) {
	out, err := exec.CommandContext(context.Background(), "gh", "auth", "token", "--user", login).Output() // #nosec G204 -- login comes from a story file in the repo
	if err != nil {
		return "", fmt.Errorf("no gh token for %s (gh auth login as that user): %w", login, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// Tracker is the Jira driver, which is not built yet (T36).
func (w *LocalWorld) Tracker() TrackerUser { return unbuiltTracker{} }

// GitHub is the GitHub driver, which is not built yet (T31).
func (w *LocalWorld) GitHub() GitHubUser { return unbuiltGitHub{} }

// Chat is the chat driver, which is not built yet (T26).
func (w *LocalWorld) Chat() ChatUser { return unbuiltChat{} }

// Server observes the product.
func (w *LocalWorld) Server() Observer { return observer{w} }

// Heimdall is the command line a person types at.
func (w *LocalWorld) Heimdall() CLI { return cli{binary: w.cfg.HeimdallBinary} }

// Clock waits on observables.
func (w *LocalWorld) Clock() Wait { return clock{poll: w.cfg.Poll} }

// Agents is agent start and stop, which is not built yet (T27).
func (w *LocalWorld) Agents() AgentControl { return unbuiltAgents{} }

// Scripted is the scripted agent's control, which is not built yet (T38).
func (w *LocalWorld) Scripted() ScriptedAgents { return unbuiltScripted{} }

// Faults is the fault injector, which is not built yet (T74).
func (w *LocalWorld) Faults() Faults { return unbuiltFaults{} }

// Runner is the test runner on the base branch, which is not built yet (T29).
func (w *LocalWorld) Runner() TestRunner { return unbuiltRunner{} }

// Clean is the clean-container world, which is not built yet (T68).
func (w *LocalWorld) Clean() CleanRoom { return unbuiltCleanRoom{} }

// Prereq probes one prerequisite for real.
func (w *LocalWorld) Prereq(ctx context.Context, n Need) Availability {
	switch n {
	case NeedServer:
		return w.probeServer(ctx)
	case NeedForward:
		return w.probeForward(ctx)
	case NeedFixtureRepo:
		return w.probeFixtureRepo(ctx)
	case NeedCLI:
		return w.probeCLI()
	case NeedLive:
		if w.cfg.Agents != "live" {
			return Availability{Why: "scripted agents are in use; run with AGENTS=live"}
		}
		return notYet("T19", "the budget guard that live runs require")
	case NeedManual:
		return Availability{Why: "a person must do this step by hand"}
	case NeedTracker, NeedGitHubUser, NeedChatUser, NeedScripted, NeedMemory, NeedContainer:
		return notBuilt(n)
	}
	return Availability{Why: fmt.Sprintf("unknown prerequisite %q", n)}
}

// notBuilt explains a prerequisite that a later task builds, and which task.
func notBuilt(n Need) Availability {
	later := map[Need]struct{ task, what string }{
		NeedTracker:    {"T36", "the Jira driver"},
		NeedGitHubUser: {"T31", "the GitHub driver"},
		NeedChatUser:   {"T26", "the chat driver"},
		NeedScripted:   {"T38", "the scripted agent"},
		NeedMemory:     {"T49", "memory (Graphiti)"},
		NeedContainer:  {"T68", "the clean-container world"},
	}
	if b, ok := later[n]; ok {
		return notYet(b.task, b.what)
	}
	return Availability{Why: fmt.Sprintf("unknown prerequisite %q", n)}
}

func notYet(task, what string) Availability {
	return Availability{Why: fmt.Sprintf("%s is not built yet (%s)", what, task)}
}

func (w *LocalWorld) probeServer(ctx context.Context) Availability {
	if w.cfg.ServerURL == "" {
		return Availability{Why: "no server address is configured (set HEIMDALL_URL); heimdall-server does not exist until T12"}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(w.cfg.ServerURL, "/")+"/healthz", nil)
	if err != nil {
		return Availability{Why: err.Error()}
	}
	resp, err := w.http.Do(req)
	if err != nil {
		return Availability{Why: fmt.Sprintf("nothing answers at %s: %v", w.cfg.ServerURL, err)}
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		return Availability{Why: fmt.Sprintf("%s/healthz answered %d", w.cfg.ServerURL, resp.StatusCode)}
	}
	return Availability{OK: true}
}

func (w *LocalWorld) probeForward(ctx context.Context) Availability {
	var down []string
	d := net.Dialer{Timeout: 2 * time.Second}
	for _, addr := range w.cfg.ForwardAddrs {
		conn, err := d.DialContext(ctx, "tcp", addr)
		if err != nil {
			down = append(down, addr)
			continue
		}
		_ = conn.Close()
	}
	if len(w.cfg.ForwardAddrs) == 0 {
		return Availability{Why: "no forwarded addresses are configured for Switch and Mattermost (docs/RUNBOOK.md)"}
	}
	if len(down) > 0 {
		return Availability{Why: fmt.Sprintf("the SSH port-forward is down: nothing answers at %s (start it as in docs/RUNBOOK.md)", strings.Join(down, ", "))}
	}
	return Availability{OK: true}
}

func (w *LocalWorld) probeFixtureRepo(ctx context.Context) Availability {
	st := w.cfg.Story
	login := st.Author().GitHub
	token, err := w.cfg.Token(login)
	if err != nil {
		return Availability{Why: fmt.Sprintf("cannot read GitHub as %s: %v", login, err)}
	}
	for _, branch := range append([]string{st.BaseBranch}, st.GoodPR, st.BadPR, st.InjectionPR) {
		status, err := w.branchStatus(ctx, st.Repo, branch, token)
		if err != nil {
			return Availability{Why: fmt.Sprintf("cannot reach GitHub for %s: %v", st.Repo, err)}
		}
		if status != http.StatusOK {
			return Availability{Why: fmt.Sprintf("repository %s has no branch %q (GitHub answered %d); the fixture is published by make fixtures-push", st.Repo, branch, status)}
		}
	}
	return Availability{OK: true}
}

func (w *LocalWorld) branchStatus(ctx context.Context, repo, branch, token string) (int, error) {
	url := fmt.Sprintf("%s/repos/%s/branches/%s", strings.TrimRight(w.cfg.GitHubAPI, "/"), repo, branch)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := w.http.Do(req)
	if err != nil {
		return 0, err
	}
	_ = resp.Body.Close()
	return resp.StatusCode, nil
}

func (w *LocalWorld) probeCLI() Availability {
	bin := w.cfg.HeimdallBinary
	if bin == "" {
		return Availability{Why: "no heimdall binary is configured (make prototype builds one)"}
	}
	info, err := os.Stat(bin)
	if err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
		return Availability{Why: fmt.Sprintf("%s is not an executable file", bin)}
	}
	return Availability{OK: true}
}

// observer shows what the server shows a person. Only Health is real so far.
type observer struct{ w *LocalWorld }

func (o observer) Gate(context.Context, PR) (GateState, error) {
	return GateState{}, unbuilt("T24", "the GitHub check reader")
}
func (o observer) Checks(context.Context, PR) ([]Check, error) {
	return nil, unbuilt("T24", "the GitHub check reader")
}
func (o observer) Findings(context.Context, PR) ([]Finding, error) {
	return nil, unbuilt("T24", "the findings reader")
}
func (o observer) Dashboard(context.Context, string) (string, error) {
	return "", unbuilt("T63", "the dashboard")
}
func (o observer) Health(ctx context.Context) error {
	if av := o.w.probeServer(ctx); !av.OK {
		return errors.New(av.Why)
	}
	return nil
}
func (o observer) Spend(context.Context) (float64, error) {
	return 0, unbuilt("T19", "the spend reader")
}
func (o observer) Counts(context.Context) (Counts, error) {
	return Counts{}, unbuilt("T26", "the message and check counter")
}
func (o observer) Replay(context.Context) error { return unbuilt("T26", "the poller replay") }

// cli runs the heimdall binary as a person at a terminal would.
type cli struct{ binary string }

func (c cli) Run(ctx context.Context, args ...string) (CLIResult, error) {
	return c.RunWithEnv(ctx, nil, args...)
}

// RunWithEnv runs with exactly the given environment plus a plain PATH, so no credential leaks in from the developer's shell.
func (c cli) RunWithEnv(ctx context.Context, env []string, args ...string) (CLIResult, error) {
	cmd := exec.CommandContext(ctx, c.binary, args...) // #nosec G204 -- the binary is the one the board built
	cmd.Env = append([]string{"PATH=/usr/local/bin:/usr/bin:/bin"}, env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	res := CLIResult{Stdout: stdout.String(), Stderr: stderr.String()}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		res.ExitCode = exit.ExitCode()
		return res, nil
	}
	if err != nil {
		return res, fmt.Errorf("run %s: %w", c.binary, err)
	}
	return res, nil
}

// clock polls observables.
type clock struct{ poll time.Duration }

func (c clock) Now() time.Time                  { return time.Now() }
func (c clock) Since(t time.Time) time.Duration { return time.Since(t) }

// Until polls cond immediately and then every poll interval until it holds. It never sleeps blindly: it waits on a
// timer or on the context, and a timeout names what was awaited, how long, and what was last seen.
func (c clock) Until(ctx context.Context, what string, timeout time.Duration, cond func(context.Context) (bool, string, error)) error {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	tick := time.NewTicker(c.poll)
	defer tick.Stop()
	var saw string
	for {
		done, s, err := cond(ctx)
		if err != nil {
			return fmt.Errorf("waiting for %s: %w", what, err)
		}
		if done {
			return nil
		}
		saw = s
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return &Red{Expected: fmt.Sprintf("%s within %s", what, timeout), Saw: saw}
		case <-tick.C:
		}
	}
}
