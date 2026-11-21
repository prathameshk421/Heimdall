package config

import (
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
)

// ActionRole names the step of Action mode that reads the environment (ACTION_MODE 3.2). Each role reads only what it
// needs: the gate never reads the model key, and the proof step never reads the log key.
type ActionRole string

// The three steps the workflows run.
const (
	// ActionTrigger records which pull request and head an event is about. It reads no token.
	ActionTrigger ActionRole = "trigger"
	// ActionProof runs PR code in the sandbox and holds the model key. Its token is read-only.
	ActionProof ActionRole = "proof"
	// ActionGate is the privileged step: the write token and the log key.
	ActionGate ActionRole = "gate"
)

// Action defaults.
const (
	defaultGitHubAPI     = "https://api.github.com"
	defaultCommentAuthor = "github-actions[bot]"
	defaultModelBaseURL  = "https://openrouter.ai/api/v1"
	// minLogKeyBytes is the gitlog store's minimum MAC key length.
	minLogKeyBytes = 32
	maxTokenLen    = 1024
	// defaultSandboxPath is the PATH of a sandbox job when the workflow names none.
	defaultSandboxPath = "/usr/bin:/bin"
)

// ActionPrice is the model's price in USD per million tokens. The agent loop needs it to bound every call.
type ActionPrice struct {
	InputPerM       float64
	CachedInputPerM float64
	OutputPerM      float64
}

// ActionEnv is the validated environment of one Action step. Token, LogKey and ModelKey are secret values: the type
// prints redacted, and no error of this package carries them.
type ActionEnv struct {
	Role ActionRole
	// Repo is GITHUB_REPOSITORY, owner/name.
	Repo string
	// APIURL is GITHUB_API_URL, https://api.github.com by default.
	APIURL string
	// Token is GITHUB_TOKEN as the workflow passes it to the step.
	Token string
	// EventName and EventPath are GITHUB_EVENT_NAME and GITHUB_EVENT_PATH. The event is read from the file, never from
	// an expression in a run: script (ACTION_MODE 3.1).
	EventName string
	EventPath string
	// RunID is GITHUB_RUN_ID, the run that records a result it makes itself.
	RunID int64
	// Workspace is GITHUB_WORKSPACE, where the proof job checked the head out. RunnerTemp and Home are the runner's own
	// directories, which the sandbox locks to 0700.
	Workspace  string
	RunnerTemp string
	Home       string
	// CommentAuthor is the login of the token's own comments, HEIMDALL_COMMENT_AUTHOR, github-actions[bot] by default.
	CommentAuthor string
	// LogKey is HEIMDALL_LOG_KEY decoded from hex, the gate only.
	LogKey []byte
	// ModelKey, ModelBaseURL, Model and Price are the proof step's model endpoint (any OpenAI-compatible URL).
	ModelKey     string
	ModelBaseURL string
	Model        string
	Price        ActionPrice
	// SandboxPath is the PATH inside a sandbox job, HEIMDALL_SANDBOX_PATH, /usr/bin:/bin by default. The workflow puts
	// the toolchain's bin directory first. Every element is an absolute directory.
	SandboxPath string
}

// HasModelKey reports whether the step got a model key. A fork PR gets none by GitHub's rule.
func (e ActionEnv) HasModelKey() bool { return e.ModelKey != "" }

// String hides the secret values from %v and %s.
func (e ActionEnv) String() string {
	return fmt.Sprintf("config.ActionEnv{role=%s repo=%s event=%s run=%d secrets=redacted}", e.Role, e.Repo, e.EventName, e.RunID)
}

// GoString hides the secret values from %#v.
func (e ActionEnv) GoString() string { return e.String() }

// Format hides the secret values from every verb, %+v included.
func (e ActionEnv) Format(f fmt.State, _ rune) { _, _ = f.Write([]byte(e.String())) }

// LoadAction reads and checks the environment of one Action step. Every problem is reported, joined. An error names
// the variable and never its value.
func LoadAction(getenv func(string) string, role ActionRole) (ActionEnv, error) {
	switch role {
	case ActionTrigger, ActionProof, ActionGate:
	default:
		return ActionEnv{}, fmt.Errorf("unknown action role %q", role)
	}
	r := actionReader{getenv: getenv}
	e := ActionEnv{Role: role}
	e.EventName = r.required("GITHUB_EVENT_NAME")
	e.EventPath = r.absPath("GITHUB_EVENT_PATH", true)
	if role != ActionTrigger {
		e.Repo = r.repo("GITHUB_REPOSITORY")
		e.APIURL = r.url("GITHUB_API_URL", defaultGitHubAPI)
		e.Token = r.token("GITHUB_TOKEN")
		e.RunID = r.runID("GITHUB_RUN_ID")
		e.CommentAuthor = r.text("HEIMDALL_COMMENT_AUTHOR", defaultCommentAuthor)
	}
	switch role {
	case ActionGate:
		e.LogKey = r.logKey("HEIMDALL_LOG_KEY")
	case ActionProof:
		e.Workspace = r.absPath("GITHUB_WORKSPACE", true)
		e.RunnerTemp = r.absPath("RUNNER_TEMP", false)
		e.Home = r.absPath("HOME", false)
		e.SandboxPath = r.searchPath("HEIMDALL_SANDBOX_PATH", defaultSandboxPath)
		r.model(&e)
	case ActionTrigger:
	}
	if err := errors.Join(r.errs...); err != nil {
		return ActionEnv{}, err
	}
	return e, nil
}

// actionReader collects the problems of one read, so one run shows them all.
type actionReader struct {
	getenv func(string) string
	errs   []error
}

func (r *actionReader) fail(name, why string) {
	r.errs = append(r.errs, fmt.Errorf("%s %s", name, why))
}

func (r *actionReader) required(name string) string {
	v := strings.TrimSpace(r.getenv(name))
	if v == "" {
		r.fail(name, "is required")
	}
	return v
}

func (r *actionReader) text(name, def string) string {
	v := strings.TrimSpace(r.getenv(name))
	if v == "" {
		return def
	}
	if strings.IndexFunc(v, func(c rune) bool { return unicode.IsSpace(c) || unicode.IsControl(c) }) >= 0 {
		r.fail(name, "has a space or control character")
	}
	return v
}

func (r *actionReader) absPath(name string, required bool) string {
	v := r.getenv(name)
	if v == "" {
		if required {
			r.fail(name, "is required")
		}
		return ""
	}
	if !filepath.IsAbs(v) {
		r.fail(name, "must be an absolute path")
	}
	return filepath.Clean(v)
}

// searchPath reads a PATH value: absolute directories joined by colons, with no space or control character.
func (r *actionReader) searchPath(name, def string) string {
	v := strings.TrimSpace(r.getenv(name))
	if v == "" {
		return def
	}
	for _, dir := range strings.Split(v, ":") {
		if !filepath.IsAbs(dir) || strings.IndexFunc(dir, func(c rune) bool { return unicode.IsSpace(c) || unicode.IsControl(c) }) >= 0 {
			r.fail(name, "must be absolute directories joined by colons")
			return ""
		}
	}
	return v
}

func (r *actionReader) repo(name string) string {
	v := r.required(name)
	if v != "" && !repoPattern.MatchString(v) {
		r.fail(name, "must be owner/name")
	}
	return v
}

func (r *actionReader) url(name, def string) string {
	v := r.getenv(name)
	if v == "" {
		v = def
	}
	if err := checkBaseURL(name, &v); err != nil {
		r.fail(name, "must be an https URL, or http on a loopback host")
		return ""
	}
	u, err := url.Parse(v)
	if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		r.fail(name, "must not carry credentials, a query or a fragment")
		return ""
	}
	return strings.TrimRight(v, "/")
}

func (r *actionReader) token(name string) string {
	v := r.getenv(name)
	switch {
	case v == "":
		r.fail(name, "is required: pass it to the step with env")
	case len(v) > maxTokenLen:
		r.fail(name, "is too long")
	case strings.IndexFunc(v, func(c rune) bool { return unicode.IsSpace(c) || unicode.IsControl(c) }) >= 0:
		r.fail(name, "has a space or control character")
	default:
		return v
	}
	return ""
}

func (r *actionReader) runID(name string) int64 {
	v := r.required(name)
	if v == "" {
		return 0
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n <= 0 {
		r.fail(name, "must be a positive number")
		return 0
	}
	return n
}

func (r *actionReader) logKey(name string) []byte {
	v := strings.TrimSpace(r.getenv(name))
	if v == "" {
		r.fail(name, "is required: heimdall action init sets it in environment heimdall")
		return nil
	}
	key, err := hex.DecodeString(v)
	if err != nil {
		r.fail(name, "must be hex")
		return nil
	}
	if len(key) < minLogKeyBytes {
		r.fail(name, fmt.Sprintf("must hold at least %d bytes", minLogKeyBytes))
		return nil
	}
	return key
}

// model reads the model endpoint. Without a key nothing else is required: the proof run then reports that it had no
// key, and makes no model call.
func (r *actionReader) model(e *ActionEnv) {
	key := r.getenv("HEIMDALL_MODEL_KEY")
	if key == "" {
		return
	}
	if strings.IndexFunc(key, func(c rune) bool { return unicode.IsSpace(c) || unicode.IsControl(c) }) >= 0 {
		r.fail("HEIMDALL_MODEL_KEY", "has a space or control character")
		return
	}
	e.ModelKey = key
	e.ModelBaseURL = r.url("HEIMDALL_MODEL_BASE_URL", defaultModelBaseURL)
	e.Model = r.required("HEIMDALL_MODEL")
	e.Price = ActionPrice{
		InputPerM:       r.price("HEIMDALL_MODEL_PRICE_INPUT", true),
		CachedInputPerM: r.price("HEIMDALL_MODEL_PRICE_CACHED_INPUT", false),
		OutputPerM:      r.price("HEIMDALL_MODEL_PRICE_OUTPUT", true),
	}
}

// price reads a price in USD per million tokens. A required price must be positive: a zero price would make every
// call look free, and the cost cap could not see the spend.
func (r *actionReader) price(name string, required bool) float64 {
	v := strings.TrimSpace(r.getenv(name))
	if v == "" {
		if required {
			r.fail(name, "is required with a model key: without a price the cost cap cannot see the spend")
		}
		return 0
	}
	p, err := strconv.ParseFloat(v, 64)
	if err != nil || math.IsNaN(p) || math.IsInf(p, 0) || p < 0 || (required && p == 0) {
		r.fail(name, "must be a finite price in USD per million tokens, above zero")
		return 0
	}
	return p
}
