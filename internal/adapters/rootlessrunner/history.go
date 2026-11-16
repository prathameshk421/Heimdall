package rootlessrunner

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// The history mirror (REVIEW_ARCH 4.6): a blobless bare mirror of a repository, commits and trees only, that a check
// such as the co-change miner reads offline.
//
// Who may touch it. Only the warm job fetches: it is the one job with egress, and its egress is the same exact-host,
// public-address boundary the module warm uses, here as a loopback CONNECT proxy that git is pointed at. A check
// never fetches and never writes: a check whose profile row says `history: true` gets the mirror bind-mounted read-only
// at /history, and every other job sees no trace of it. The mirror lives in a directory named by the hash of the
// repository, so no repository name can reach a path.
//
// Private repositories (owner's decision, Nov 16). The runner holds no credential of its own (CLAUDE.md R8). For a
// private repository the server mints a short-lived, read-only token for that one repository and passes it with the
// request. The runner hands it to git only through the environment (GIT_CONFIG_COUNT, an http.extraHeader scoped to
// https://github.com/), so it is in no argument list, no mirror config and no file; an error from the fetch is
// returned with the token cut out. A public repository is fetched with no header.

const (
	// historyHost is the one forge host a mirror is fetched from. The profile must list it in fetch_hosts, which a person
	// approves by merging the profile.
	historyHost = "github.com"
	// maxHistoryDepth bounds how much history one fetch takes; the miner reads the last N commits and N is at most this.
	maxHistoryDepth = 5000
	// maxHistoryBytes bounds the mirror's size on disk; a larger mirror is removed and the update fails.
	maxHistoryBytes = 1 << 30
	// historyTimeout bounds one update, as every job is bounded.
	historyTimeout = 10 * time.Minute
)

// Errors of the history mirror.
var (
	ErrNoHistoryMirror   = errors.New("the repository has no history mirror: run the warm job first")
	ErrHistoryNotAllowed = errors.New("the profile does not allow fetching history: fetch_hosts must list " + historyHost)
	ErrHistoryTooLarge   = errors.New("the history mirror exceeds its size bound")
)

var repoSegment = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]*$`)

// GitRunner runs the git binary. The real one is ExecGit; tests record the arguments.
type GitRunner interface {
	Run(ctx context.Context, dir string, env, args []string) ([]byte, error)
}

// ExecGit runs git without inheriting the runner's environment.
type ExecGit struct{ Home string }

// Run executes git with exactly the given environment, output bounded.
func (g ExecGit) Run(ctx context.Context, dir string, env, args []string) ([]byte, error) {
	// #nosec G204 -- args come only from the history functions of this file; the URL is built from a validated repo.
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = env
	cmd.Cancel = func() error { return cmd.Process.Kill() }
	cmd.WaitDelay = 5 * time.Second
	var out boundedBuffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		return out.Bytes(), fmt.Errorf("git %s: %w", firstNonFlag(args), err)
	}
	return out.Bytes(), nil
}

// firstNonFlag names the git subcommand in an error without echoing the URL or the proxy address.
func firstNonFlag(args []string) string {
	for i := 0; i < len(args); i++ {
		if args[i] == "-c" {
			i++
			continue
		}
		if !strings.HasPrefix(args[i], "-") {
			return args[i]
		}
	}
	return "command"
}

// mirrorDir is where a repository's mirror lives: a hash, so no repository name reaches a path.
func (p Policy) mirrorDir(repo change.Repo) string {
	sum := sha256.Sum256([]byte(strings.ToLower(string(repo))))
	return filepath.Join(p.historyRoot(), hex.EncodeToString(sum[:])+".git")
}

func (p Policy) historyRoot() string {
	return filepath.Join(filepath.Dir(p.moduleCache()), "history")
}

// mirrorURL is the clone URL of a repository on the one forge host.
func mirrorURL(repo change.Repo) (string, error) {
	owner, name, ok := strings.Cut(string(repo), "/")
	if !ok || !repoSegment.MatchString(owner) || !repoSegment.MatchString(name) || strings.HasSuffix(name, ".git") {
		return "", fmt.Errorf("repository %q cannot be mirrored", repo)
	}
	return "https://" + historyHost + "/" + owner + "/" + name + ".git", nil
}

// gitConfig are the -c settings of every git call: no hooks, no credential helper, https only, no redirect, objects
// checked, and the CONNECT proxy as the only way out.
func gitConfig(proxy string) []string {
	return []string{
		"-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false", "-c", "gc.auto=0",
		"-c", "credential.helper=", "-c", "protocol.allow=never", "-c", "protocol.https.allow=always",
		"-c", "http.proxy=" + proxy, "-c", "http.followRedirects=false", "-c", "transfer.fsckObjects=true",
	}
}

// gitEnv is the whole environment of a git call.
func gitEnv(home string) []string {
	return []string{
		"PATH=/usr/bin:/bin", "HOME=" + home, "LANG=C", "GIT_TERMINAL_PROMPT=0",
		"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null",
	}
}

// historyCommands are the git invocations that create or update a mirror, in order. A new mirror is initialised bare
// and configured as a blobless partial clone before its first fetch.
func historyCommands(proxy, dir, repoURL string, fresh bool) [][]string {
	cfg := gitConfig(proxy)
	with := func(args ...string) []string { return append(append([]string{}, cfg...), args...) }
	var cmds [][]string
	if fresh {
		cmds = append(cmds,
			with("init", "--bare", "--quiet", dir),
			with("-C", dir, "config", "remote.origin.url", repoURL),
			with("-C", dir, "config", "remote.origin.promisor", "true"),
			with("-C", dir, "config", "remote.origin.partialclonefilter", "blob:none"),
		)
	}
	return append(cmds, with("-C", dir, "fetch", "--quiet", "--no-tags", "--prune", "--filter=blob:none",
		"--depth="+strconv.Itoa(maxHistoryDepth), "origin", "+refs/heads/*:refs/heads/*"))
}

// WarmHistory creates or updates the history mirror of a repository under a promoted profile that allows it.
func (r *Runner) WarmHistory(ctx context.Context, profileName string, repo change.Repo, token string) (ports.ProfileHistoryResult, error) {
	spec, ok := r.specNamed(profileName)
	if !ok {
		return ports.ProfileHistoryResult{}, ErrProfileNotFound
	}
	if !slicesContains(spec.FetchHosts, historyHost) {
		return ports.ProfileHistoryResult{}, ErrHistoryNotAllowed
	}
	repoURL, err := mirrorURL(repo)
	if err != nil {
		return ports.ProfileHistoryResult{}, err
	}
	dir := r.Policy.mirrorDir(repo)
	_, statErr := os.Stat(dir)
	fresh := errors.Is(statErr, fs.ErrNotExist)
	if statErr != nil && !fresh {
		return ports.ProfileHistoryResult{}, fmt.Errorf("inspect history mirror: %w", statErr)
	}
	if err := r.fetchMirror(ctx, dir, repoURL, fresh, token); err != nil {
		if fresh {
			_ = os.RemoveAll(dir)
		}
		return ports.ProfileHistoryResult{}, err
	}
	size, err := dirSize(dir)
	if err == nil && size > maxHistoryBytes {
		err = ErrHistoryTooLarge
	}
	if err != nil {
		_ = os.RemoveAll(dir)
		return ports.ProfileHistoryResult{}, err
	}
	return ports.ProfileHistoryResult{Bytes: size, Fresh: fresh}, nil
}

// fetchMirror runs the mirror's git commands behind the exact-host proxy.
func (r *Runner) fetchMirror(ctx context.Context, dir, repoURL string, fresh bool, token string) error {
	proxy, stop, err := startConnectProxy(ctx, []string{historyHost})
	if err != nil {
		return err
	}
	defer stop()
	if err := os.MkdirAll(r.Policy.historyRoot(), 0o700); err != nil {
		return fmt.Errorf("prepare history directory: %w", err)
	}
	return r.runGit(ctx, proxy, dir, repoURL, fresh, token)
}

// tokenEnv carries a request token to git as an extra Authorization header for the forge host only.
func tokenEnv(token string) []string {
	if token == "" {
		return nil
	}
	basic := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + token))
	return []string{"GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=http.https://" + historyHost + "/.extraHeader",
		"GIT_CONFIG_VALUE_0=Authorization: Basic " + basic}
}

// withoutToken cuts a token, raw or in its header form, out of text that leaves the runner.
func withoutToken(text, token string) string {
	if token == "" {
		return text
	}
	text = strings.ReplaceAll(text, token, "[token]")
	return strings.ReplaceAll(text, base64.StdEncoding.EncodeToString([]byte("x-access-token:"+token)), "[token]")
}

func (r *Runner) runGit(ctx context.Context, proxy, dir, repoURL string, fresh bool, token string) error {
	git := r.Git
	if git == nil {
		git = ExecGit{}
	}
	home := r.Policy.historyRoot()
	ctx, cancel := context.WithTimeout(ctx, historyTimeout)
	defer cancel()
	for _, args := range historyCommands(proxy, dir, repoURL, fresh) {
		if out, err := git.Run(ctx, home, append(gitEnv(home), tokenEnv(token)...), args); err != nil {
			return fmt.Errorf("update history mirror: %w (%s)", err, withoutToken(strings.TrimSpace(tail(out, 512)), token))
		}
	}
	return nil
}

func tail(b []byte, n int) string {
	if len(b) > n {
		b = b[len(b)-n:]
	}
	return string(b)
}

func dirSize(root string) (int64, error) {
	var total int64
	err := filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			info, infoErr := d.Info()
			if infoErr != nil {
				return infoErr
			}
			total += info.Size()
		}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("measure history mirror: %w", err)
	}
	return total, nil
}

// historyMount resolves the read-only mirror mount of a check job, refusing a job whose mirror does not exist.
func (p Policy) historyMount(repo change.Repo) (string, error) {
	dir := p.mirrorDir(repo)
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return "", ErrNoHistoryMirror
	}
	return dir, nil
}

// startConnectProxy serves a loopback HTTP CONNECT proxy that connects only to the given exact hosts on port 443 and
// only to public addresses, the same boundary as the module fetch. It returns the proxy URL and the function that
// stops it. git is pointed at it for the one call that fetches.
func startConnectProxy(ctx context.Context, hosts []string) (string, func(), error) {
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, fmt.Errorf("start fetch proxy: %w", err)
	}
	server := &http.Server{Handler: connectHandler(ctx, hosts), ReadHeaderTimeout: 5 * time.Second, MaxHeaderBytes: 8 << 10}
	go func() { _ = server.Serve(listener) }()
	return "http://" + listener.Addr().String(), func() { _ = server.Close() }, nil
}

func connectHandler(ctx context.Context, hosts []string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodConnect {
			http.Error(w, "only CONNECT", http.StatusMethodNotAllowed)
			return
		}
		upstream, err := dialAllowlisted(ctx, hosts, req.Host)
		if err != nil {
			http.Error(w, "target is not allowlisted", http.StatusForbidden)
			return
		}
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			_ = upstream.Close()
			http.Error(w, "cannot tunnel", http.StatusInternalServerError)
			return
		}
		client, _, err := hijacker.Hijack()
		if err != nil {
			_ = upstream.Close()
			return
		}
		_, _ = client.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
		tunnel(client, upstream)
	})
}

// tunnel copies both ways until either side closes.
func tunnel(a, b net.Conn) {
	done := make(chan struct{}, 2)
	pipe := func(dst, src net.Conn) {
		_, _ = io.Copy(dst, src)
		_ = dst.Close()
		done <- struct{}{}
	}
	go pipe(a, b)
	go pipe(b, a)
	<-done
	_ = a.Close()
	_ = b.Close()
	<-done
}
