package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/cardsfs"
)

const cardsRemoteTimeout = 2 * time.Minute

// remoteOpts are the flags every server command shares.
type remoteOpts struct {
	server, tokenFile, dir string
	yes, force             bool
}

func remoteFlagSet(name string, env cardsEnv, o *remoteOpts) *flag.FlagSet {
	fs := flag.NewFlagSet("heimdall cards "+name, flag.ContinueOnError)
	fs.SetOutput(env.errw)
	fs.StringVar(&o.server, "server", "", "Heimdall server URL (https, or http on localhost)")
	fs.StringVar(&o.tokenFile, "token-file", "", "file that holds your personal token")
	fs.StringVar(&o.dir, "dir", filepath.Join(defaultCardsWork, "files"), "folder with the card files")
	fs.BoolVar(&o.yes, "yes", false, "push: skip the typed confirmation")
	fs.BoolVar(&o.force, "force", false, "pull: replace local cards that differ")
	return fs
}

// cardsClient talks to the /cards endpoint of the server with the token of one person.
type cardsClient struct {
	base  *url.URL
	token string
	hc    http.Client
}

func newCardsClient(o remoteOpts, env cardsEnv) (*cardsClient, error) {
	base, err := url.Parse(o.server)
	if err != nil || base.Host == "" {
		return nil, fmt.Errorf("--server %q is not a URL", o.server)
	}
	local := base.Hostname() == "localhost" || base.Hostname() == "127.0.0.1" || base.Hostname() == "::1"
	if base.Scheme != "https" && (base.Scheme != "http" || !local) {
		return nil, errors.New("--server must use https (http is allowed only for localhost)")
	}
	if o.tokenFile == "" {
		return nil, errors.New("--token-file is required")
	}
	raw, err := os.ReadFile(o.tokenFile)
	if err != nil {
		return nil, fmt.Errorf("read token file: %w", err)
	}
	token := strings.TrimSpace(string(raw))
	if token == "" {
		return nil, errors.New("token file is empty")
	}
	hc := http.Client{Timeout: cardsRemoteTimeout}
	if env.client != nil {
		hc = *env.client
	}
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &cardsClient{base: base, token: token, hc: hc}, nil
}

func (c *cardsClient) do(ctx context.Context, method, name, body string) (string, error) {
	target := c.base.JoinPath("cards")
	if name != "" {
		target = c.base.JoinPath("cards", name)
	}
	req, err := http.NewRequestWithContext(ctx, method, target.String(), strings.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.hc.Do(req)
	if err != nil {
		return "", fmt.Errorf("%s %s: %w", method, target.Path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, cardsfs.MaxCardBytes+1))
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", fmt.Errorf("%s %s: %s: %s", method, target.Path, resp.Status, strings.TrimSpace(string(firstN(data, 300))))
	}
	return string(data), nil
}

func (c *cardsClient) list(ctx context.Context) ([]string, error) {
	text, err := c.do(ctx, http.MethodGet, "", "")
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Cards []string `json:"cards"`
	}
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		return nil, fmt.Errorf("parse card list: %w", err)
	}
	return parsed.Cards, nil
}

func (c *cardsClient) get(ctx context.Context, name string) (string, error) {
	return c.do(ctx, http.MethodGet, name, "")
}

func (c *cardsClient) put(ctx context.Context, name, text string) error {
	_, err := c.do(ctx, http.MethodPut, name, text)
	return err
}

// remoteSetup parses the shared flags and builds the client. It reports bad input itself.
func remoteSetup(name string, args []string, env cardsEnv) (*cardsClient, remoteOpts, bool) {
	var o remoteOpts
	fs := remoteFlagSet(name, env, &o)
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		writeLine(env.out, "usage: heimdall cards "+name+" --server URL --token-file F [--dir dir]")
		return nil, o, false
	}
	client, err := newCardsClient(o, env)
	if err != nil {
		writeLine(env.out, "heimdall cards "+name+": "+err.Error())
		return nil, o, false
	}
	return client, o, true
}

// runCardsPush checks the cards, lists them, asks for a yes and uploads them.
func runCardsPush(args []string, env cardsEnv) int {
	client, o, ok := remoteSetup("push", args, env)
	if !ok {
		return 2
	}
	names, err := cardFileNames(o.dir)
	if err != nil || len(names) == 0 {
		writeLine(env.out, "heimdall cards push: no cards in "+o.dir)
		return 1
	}
	good, bad := checkCards(o.dir, names, env.out)
	if bad {
		writeLine(env.out, "fix the problems above, then push again. Nothing was uploaded.")
		return 1
	}
	if !o.yes && !confirmPush(env, client.base.String(), good) {
		writeLine(env.out, "not confirmed. Nothing was uploaded.")
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), cardsRemoteTimeout)
	defer cancel()
	for _, name := range good {
		data, err := readInDir(o.dir, name)
		if err == nil {
			err = client.put(ctx, name, string(data))
		}
		if err != nil {
			writeLine(env.out, "heimdall cards push: "+name+": "+err.Error())
			return 1
		}
		writeLine(env.out, "uploaded "+name)
	}
	return 0
}

func confirmPush(env cardsEnv, server string, names []string) bool {
	writeLine(env.out, fmt.Sprintf("these %d cards go to %s:", len(names), server))
	for _, n := range names {
		writeLine(env.out, "  "+n)
	}
	_, _ = io.WriteString(env.out, "type yes to upload: ")
	line, _ := bufio.NewReader(env.stdin).ReadString('\n')
	return strings.TrimSpace(line) == "yes"
}

// runCardsPull downloads the cards of the token's person into the folder. A local file that differs stays unless
// --force is set.
func runCardsPull(args []string, env cardsEnv) int {
	client, o, ok := remoteSetup("pull", args, env)
	if !ok {
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), cardsRemoteTimeout)
	defer cancel()
	names, err := client.list(ctx)
	if err != nil {
		writeLine(env.out, "heimdall cards pull: "+err.Error())
		return 1
	}
	if err := os.MkdirAll(o.dir, 0o750); err != nil {
		writeLine(env.out, "heimdall cards pull: "+err.Error())
		return 1
	}
	for _, name := range names {
		if filepath.Base(name) != name {
			writeLine(env.out, "skipped "+name+": not a plain file name")
			continue
		}
		text, err := client.get(ctx, name)
		if err != nil {
			writeLine(env.out, "heimdall cards pull: "+err.Error())
			return 1
		}
		writeLine(env.out, pullOne(o, name, text))
	}
	return 0
}

func pullOne(o remoteOpts, name, text string) string {
	path := filepath.Join(o.dir, name)
	if old, err := readInDir(o.dir, name); err == nil {
		switch {
		case string(old) == text:
			return "same     " + name
		case !o.force:
			return "kept     " + name + " (local file differs, use --force to replace it)"
		}
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		return "failed   " + name + ": " + err.Error()
	}
	return "pulled   " + name
}

// runCardsInbox lists the server cards that wait for the owner: status proposed.
func runCardsInbox(args []string, env cardsEnv) int {
	client, _, ok := remoteSetup("inbox", args, env)
	if !ok {
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), cardsRemoteTimeout)
	defer cancel()
	names, err := client.list(ctx)
	if err != nil {
		writeLine(env.out, "heimdall cards inbox: "+err.Error())
		return 1
	}
	waiting := 0
	for _, name := range names {
		text, err := client.get(ctx, name)
		if err != nil {
			writeLine(env.out, "heimdall cards inbox: "+err.Error())
			return 1
		}
		if f, _ := readCardFields(text); f.Status != "proposed" {
			continue
		}
		waiting++
		writeLine(env.out, "== "+name+"\n"+strings.TrimSpace(text)+"\n")
	}
	writeLine(env.out, fmt.Sprintf("%d proposed cards. Use: heimdall cards accept NAME or heimdall cards reject NAME", waiting))
	return 0
}

// runCardsSetStatus rewrites the status of one server card and uploads it again.
func runCardsSetStatus(args []string, env cardsEnv, status string) int {
	verb := map[string]string{"active": "accept", "rejected": "reject"}[status]
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		writeLine(env.out, "usage: heimdall cards "+verb+" NAME --server URL --token-file F")
		return 2
	}
	name := args[0]
	client, _, ok := remoteSetup(verb, args[1:], env)
	if !ok {
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), cardsRemoteTimeout)
	defer cancel()
	text, err := client.get(ctx, name)
	if err != nil {
		writeLine(env.out, "heimdall cards "+verb+": "+err.Error())
		return 1
	}
	updated := setStatus(text, status)
	if report := checkCardText(name, updated); len(report.problems) > 0 {
		writeLine(env.out, "heimdall cards "+verb+": the card has a problem: "+strings.Join(report.problems, "; "))
		return 1
	}
	if err := client.put(ctx, name, updated); err != nil {
		writeLine(env.out, "heimdall cards "+verb+": "+err.Error())
		return 1
	}
	writeLine(env.out, name+" is now "+status)
	return 0
}
