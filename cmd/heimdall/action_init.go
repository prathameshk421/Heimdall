package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
)

// The names action init sets up. The gate reads the environment's secrets (ACTION_MODE 3.2); only jobs of the default
// branch reach the environment, so a writer of a pull request never reads the log key or the App key (3.5).
const (
	actionEnvironment = "heimdall"
	logKeySecret      = "HEIMDALL_LOG_KEY"   // #nosec G101 -- the name of a secret, not its value
	modelKeySecret    = "HEIMDALL_MODEL_KEY" // #nosec G101 -- the name of a secret, not its value
	appKeySecret      = "HEIMDALL_APP_KEY"   // #nosec G101 -- the name of a secret, not its value
	appIDVariable     = "HEIMDALL_APP_ID"
	// maxGHOutput bounds what init reads from one gh command.
	maxGHOutput = 1 << 20
	// maxModelKey bounds the model key read from stdin.
	maxModelKey = 4096
)

// ghRunner runs the maintainer's own gh CLI. stdin carries a secret to gh and never reaches a print.
type ghRunner interface {
	Run(ctx context.Context, args []string, stdin []byte) ([]byte, error)
}

// execGH is ghRunner on the gh binary. Its error names the command and gh's stderr, never stdin.
type execGH struct{}

func (execGH) Run(ctx context.Context, args []string, stdin []byte) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "gh", args...) // #nosec G204 -- fixed gh binary, no shell; init builds every argument
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &limitedBuffer{buf: &out, max: maxGHOutput}, &limitedBuffer{buf: &errOut, max: 4096}
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("gh %s: %w: %s", strings.Join(safeArgs(args), " "), err, strings.TrimSpace(errOut.String()))
	}
	return out.Bytes(), nil
}

// safeArgs is the argv of a gh call for a message. init never passes a secret in argv, so it names the command as
// given; a manifest code is cut, because it is a one-time credential.
func safeArgs(args []string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		if strings.HasPrefix(a, "app-manifests/") {
			a = "app-manifests/<code>/conversions"
		}
		out[i] = a
	}
	return out
}

// limitedBuffer keeps at most max bytes and drops the rest.
type limitedBuffer struct {
	buf *bytes.Buffer
	max int
}

func (l *limitedBuffer) Write(p []byte) (int, error) {
	if room := l.max - l.buf.Len(); room > 0 {
		l.buf.Write(p[:min(len(p), room)])
	}
	return len(p), nil
}

// randomLogKey is a new 32-byte MAC key for the state log.
func randomLogKey() ([]byte, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("make the log key: %w", err)
	}
	return key, nil
}

// initOptions are the flags of action init.
type initOptions struct {
	repo          change.Repo
	apply         bool
	modelKeyStdin bool
	rotateLogKey  bool
	enforce       bool
	appCode       string
	owner         string
}

func parseInitOptions(args []string, stderr io.Writer) (initOptions, bool) {
	fs := flag.NewFlagSet("heimdall action init", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repo := fs.String("repo", "", "the repository, owner/name")
	var o initOptions
	fs.BoolVar(&o.apply, "apply", false, "perform the steps; without it init only prints them")
	fs.BoolVar(&o.modelKeyStdin, "model-key-stdin", false, "read the model key from stdin and set it as a repository secret")
	fs.BoolVar(&o.rotateLogKey, "rotate-log-key", false, "replace an existing log key; the gate then resets every change's log")
	fs.BoolVar(&o.enforce, "enforce", false, "enforce mode: create the Heimdall GitHub App from a manifest")
	fs.StringVar(&o.appCode, "app-code", "", "the code GitHub returned after the App manifest was confirmed")
	if err := fs.Parse(args); err != nil || fs.NArg() > 0 {
		return initOptions{}, false
	}
	r, err := change.ParseRepo(*repo)
	if err != nil {
		writeDiagnostic(stderr, "heimdall action init: -repo must be owner/name\n")
		return initOptions{}, false
	}
	o.repo = r
	o.owner, _, _ = strings.Cut(r.String(), "/")
	if o.appCode != "" && !o.enforce {
		writeDiagnostic(stderr, "heimdall action init: -app-code needs -enforce\n")
		return initOptions{}, false
	}
	return o, true
}

// initStep is one setup step: what it does, the gh call that does it, and the secret it sends on stdin, if any.
type initStep struct {
	name string
	what string
	args []string
	// secret makes the value sent on stdin when the step runs. It is never printed.
	secret func() ([]byte, error)
	// skip, when set, decides at run time that the step is already done.
	skip func(ctx context.Context) (string, error)
	// after reads the step's output, for the App conversion.
	after func(ctx context.Context, out []byte) error
}

// runActionInit prints the setup steps, and performs them with -apply. It runs on the maintainer's machine with the
// maintainer's own gh login. No secret is printed: the log key and the App key go from this process to gh on stdin.
func runActionInit(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, deps actionDeps) int {
	o, ok := parseInitOptions(args, stderr)
	if !ok {
		return exitActionUsage
	}
	steps, err := initSteps(ctx, o, stdin, deps)
	if err != nil {
		return actionFail(stderr, "init", err)
	}
	if !o.apply {
		writeDiagnostic(stdout, fmt.Sprintf("heimdall action init would set up %s (run again with -apply):\n", o.repo))
		for i, s := range steps {
			line := fmt.Sprintf("%d. %s: %s\n", i+1, s.name, s.what)
			if len(s.args) > 0 {
				line += "   gh " + strings.Join(shown(s), " ") + "\n"
			}
			writeDiagnostic(stdout, line)
		}
		return exitActionOK
	}
	for i, s := range steps {
		if err := runInitStep(ctx, deps.gh, s, stdout); err != nil {
			return actionFail(stderr, "init", fmt.Errorf("step %d (%s): %w", i+1, s.name, err))
		}
	}
	writeDiagnostic(stdout, "heimdall action init: done\n")
	return exitActionOK
}

// shown is a step's command for the plan: a secret on stdin is named, never read.
func shown(s initStep) []string {
	out := safeArgs(s.args)
	if s.secret != nil {
		out = append(out, "< (value made by init, not shown)")
	}
	return out
}

func runInitStep(ctx context.Context, gh ghRunner, s initStep, stdout io.Writer) error {
	if s.skip != nil {
		why, err := s.skip(ctx)
		if err != nil {
			return err
		}
		if why != "" {
			writeDiagnostic(stdout, fmt.Sprintf("skipped %s: %s\n", s.name, why))
			return nil
		}
	}
	var input []byte
	if s.secret != nil {
		v, err := s.secret()
		if err != nil {
			return err
		}
		input = v
	}
	if len(s.args) == 0 {
		return nil
	}
	out, err := gh.Run(ctx, s.args, input)
	if err != nil {
		return err
	}
	if s.after != nil {
		if err := s.after(ctx, out); err != nil {
			return err
		}
	}
	writeDiagnostic(stdout, "done "+s.name+"\n")
	return nil
}

// initSteps is the setup of ACTION_MODE 3.2: the environment limited to the default branch, the log key in it, the
// model key as a repository secret, and in enforce mode the App from a manifest.
func initSteps(ctx context.Context, o initOptions, stdin io.Reader, deps actionDeps) ([]initStep, error) {
	repo := o.repo.String()
	branch := "<default branch>"
	if o.apply {
		b, err := defaultBranch(ctx, deps.gh, repo)
		if err != nil {
			return nil, err
		}
		branch = b
	}
	steps := []initStep{
		{name: "environment", what: "create environment " + actionEnvironment + " that only the default branch can deploy to",
			args: []string{"api", "-X", "PUT", "repos/" + repo + "/environments/" + actionEnvironment,
				"-F", "deployment_branch_policy[protected_branches]=false", "-F", "deployment_branch_policy[custom_branch_policies]=true"}},
		{name: "branch policy", what: "allow branch " + branch + " to deploy to " + actionEnvironment,
			args: []string{"api", "-X", "POST", "repos/" + repo + "/environments/" + actionEnvironment + "/deployment-branch-policies",
				"-f", "name=" + branch, "-f", "type=branch"},
			skip: func(ctx context.Context) (string, error) { return branchPolicyExists(ctx, deps.gh, repo, branch) }},
		{name: "log key", what: "set " + logKeySecret + " in environment " + actionEnvironment + " to a new random 32-byte key",
			args: []string{"secret", "set", logKeySecret, "--env", actionEnvironment, "--repo", repo},
			secret: func() ([]byte, error) {
				key, err := deps.newKey()
				if err != nil {
					return nil, err
				}
				return []byte(hex.EncodeToString(key)), nil
			},
			skip: func(ctx context.Context) (string, error) {
				if o.rotateLogKey {
					return "", nil
				}
				return secretExists(ctx, deps.gh, repo, logKeySecret,
					"it exists; a new key would fail every log's MAC (use -rotate-log-key to replace it)")
			}},
	}
	steps = append(steps, modelKeyStep(o, stdin))
	if o.enforce {
		steps = append(steps, appSteps(o, deps)...)
	}
	return steps, nil
}

// modelKeyStep sets the model key from stdin with -model-key-stdin. Without it the step only says what to run: init
// never asks for the key on the terminal, so it cannot echo it.
func modelKeyStep(o initOptions, stdin io.Reader) initStep {
	s := initStep{name: "model key", args: []string{"secret", "set", modelKeySecret, "--repo", o.repo.String()},
		what: "set the repository secret " + modelKeySecret + " (a dedicated key with a spend limit at the provider)"}
	if !o.modelKeyStdin {
		s.what += "; run this yourself, or pass -model-key-stdin"
		s.skip = func(context.Context) (string, error) {
			return "no -model-key-stdin: run `gh secret set " + modelKeySecret + " --repo " + o.repo.String() + "` yourself", nil
		}
		return s
	}
	s.secret = func() ([]byte, error) {
		raw, err := io.ReadAll(io.LimitReader(stdin, maxModelKey+1))
		if err != nil {
			return nil, fmt.Errorf("read the model key from stdin: %w", err)
		}
		key := bytes.TrimSpace(raw)
		if len(key) == 0 || len(key) > maxModelKey {
			return nil, errors.New("the model key on stdin is empty or too long")
		}
		return key, nil
	}
	return s
}

// appManifest is the Heimdall App of enforce mode: the permissions of the gate (ACTION_MODE 3.2) and no webhook.
func appManifest(o initOptions) ([]byte, error) {
	m := map[string]any{
		"name":   "heimdall-" + strings.ReplaceAll(o.repo.String(), "/", "-"),
		"url":    "https://github.com/" + o.repo.String(),
		"public": false,
		"hook_attributes": map[string]any{
			"url": "https://github.com/" + o.repo.String(), "active": false,
		},
		"default_permissions": map[string]string{
			"contents": "write", "checks": "write", "pull_requests": "write", "actions": "read", "security_events": "read",
		},
	}
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode the App manifest: %w", err)
	}
	return raw, nil
}

// appSteps creates the App. GitHub creates an App from a manifest only after a person confirms it in the browser, so
// without -app-code the step prints the manifest and the page to post it to. With the code GitHub returned, init
// converts it and stores the App key in the environment; the key goes from gh's output to gh's stdin, never to a print.
func appSteps(o initOptions, deps actionDeps) []initStep {
	if o.appCode == "" {
		return []initStep{{name: "app manifest",
			what: "open https://github.com/organizations/" + o.owner + "/settings/apps/new (or https://github.com/settings/apps/new " +
				"for a personal account), post this manifest, confirm, then run init again with -enforce -app-code <code>",
			skip: func(context.Context) (string, error) {
				raw, err := appManifest(o)
				if err != nil {
					return "", err
				}
				return "manifest:\n" + string(raw), nil
			}}}
	}
	var conv struct {
		ID  int64  `json:"id"`
		PEM string `json:"pem"`
	}
	return []initStep{
		{name: "app conversion", what: "turn the manifest code into the App and its private key",
			args: []string{"api", "-X", "POST", "app-manifests/" + o.appCode + "/conversions"},
			after: func(ctx context.Context, out []byte) error {
				if err := json.Unmarshal(out, &conv); err != nil || conv.ID <= 0 || conv.PEM == "" {
					return errors.New("the App conversion reply holds no App id or key")
				}
				if _, err := deps.gh.Run(ctx, []string{"secret", "set", appKeySecret, "--env", actionEnvironment,
					"--repo", o.repo.String()}, []byte(conv.PEM)); err != nil {
					return err
				}
				_, err := deps.gh.Run(ctx, []string{"variable", "set", appIDVariable, "--env", actionEnvironment,
					"--repo", o.repo.String(), "--body", strconv.FormatInt(conv.ID, 10)}, nil)
				return err
			}},
	}
}

func defaultBranch(ctx context.Context, gh ghRunner, repo string) (string, error) {
	out, err := gh.Run(ctx, []string{"api", "repos/" + repo, "--jq", ".default_branch"}, nil)
	if err != nil {
		return "", err
	}
	b := strings.TrimSpace(string(out))
	if b == "" || strings.ContainsAny(b, " \t\r\n") {
		return "", errors.New("gh returned no default branch")
	}
	return b, nil
}

func branchPolicyExists(ctx context.Context, gh ghRunner, repo, branch string) (string, error) {
	out, err := gh.Run(ctx, []string{"api", "repos/" + repo + "/environments/" + actionEnvironment + "/deployment-branch-policies",
		"--jq", ".branch_policies[].name"}, nil)
	if err != nil {
		return "", err
	}
	for name := range strings.SplitSeq(string(out), "\n") {
		if strings.TrimSpace(name) == branch {
			return "branch " + branch + " is already allowed", nil
		}
	}
	return "", nil
}

func secretExists(ctx context.Context, gh ghRunner, repo, name, why string) (string, error) {
	out, err := gh.Run(ctx, []string{"secret", "list", "--env", actionEnvironment, "--repo", repo, "--json", "name", "--jq", ".[].name"}, nil)
	if err != nil {
		return "", err
	}
	for n := range strings.SplitSeq(string(out), "\n") {
		if strings.TrimSpace(n) == name {
			return why, nil
		}
	}
	return "", nil
}
