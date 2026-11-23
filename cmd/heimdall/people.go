// `heimdall connect people`: maps Switch sender ids to the people in heimdall.yaml (D140, D95, D158).
//
// # Purpose
//
// A chat command counts only when its Switch sender id maps to a person (D140). This command fills that map. The
// owner gives a person name, the sender id copied from Switch, and the optional GitHub login and agent name. The
// rules that refuse a bad mapping are the app layer's (app/people). This file parses the flags, asks the questions,
// and writes the result. `--list` shows the Switch users from the directory built from heimdall.toml.
//
// # API
//
//   - runConnectPeople(args, stdin, stdout, stderr, deps) int: the command. Exit 0 on success or no change,
//     1 on a refused mapping or a failed write, 2 on a bad command line. It takes flags only.
//   - peopleDeps and switchDirectory: the injected Switch listing. realPeopleDeps builds the listing from the
//     [switch] section of heimdall.toml (--config or HEIMDALL_CONFIG); tests inject a fake directory instead.
//   - setPeopleSection(raw, table): replaces only the people key of the YAML document, so the other keys and the
//     top comment stay.
//
// # Invariants
//
//   - The file is replaced only when the new people table, written and read back through policyyaml.Decode, equals
//     the table the command computed, and the policy validates.
//   - A secret is never an argument. Sender ids and logins are platform ids, not secrets, so they may be printed.
//   - Refusals name the field and the reason. Nothing is written after a refusal.
//
// # Failure modes
//
//   - A refused mapping, a policy that does not load, or a failed write: exit 1, and the file is unchanged.
//   - Comments inside the people block are not kept, because that block is rewritten whole. The rest keeps its text.
//   - An unreachable or unconfigured Switch listing does not stop interactive mapping: a note is printed and each
//     sender id is typed by hand. `--list` reports the failure and exits 1.
//
// # Tests
//
// people_test.go uses a fake directory and temp heimdall.yaml files copied from the policy fixtures. It covers each
// refusal with the file unchanged, the interactive loop and its sender picker, the list with real config wiring,
// the offline fallback, and the dispatch from connect.
package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/policyyaml"
	switchchat "github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/switch"
	people "github.com/prathameshk421/flip_the_switch_hackathon/internal/app/people"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/config"
)

// switchUser is one entry of the Switch directory. SenderID is the id a message carries and the value mapped to a
// policy person. Type is "user" or "agent", as Switch reports it.
type switchUser struct {
	Name     string
	SenderID string
	Type     string
}

// switchDirectory lists the Switch users. It must honour the context's deadline.
type switchDirectory interface {
	ListUsers(ctx context.Context) ([]switchUser, error)
}

// peopleDeps is what the people command touches outside this file. A non-nil directory is injected by tests; the
// binary leaves it nil and builds one from the loaded config through newClient.
type peopleDeps struct {
	directory  switchDirectory
	loadConfig func(string) (config.Config, error)
	getenv     func(string) string
	newClient  func(config.Config) (switchDirectory, error)
	deadline   time.Duration
}

func realPeopleDeps() peopleDeps {
	return peopleDeps{
		loadConfig: config.Load,
		getenv:     config.Getenv,
		newClient:  newSwitchDirectory,
		deadline:   connectCheckDeadline,
	}
}

// switchClientDirectory adapts the Switch adapter's directory to the small shape this command prints.
type switchClientDirectory struct{ client *switchchat.Client }

func (s switchClientDirectory) ListUsers(ctx context.Context) ([]switchUser, error) {
	users, err := s.client.ListUsers(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]switchUser, 0, len(users))
	for _, u := range users {
		out = append(out, switchUser{Name: u.Name, SenderID: u.SenderID, Type: u.Type})
	}
	return out, nil
}

func newSwitchDirectory(cfg config.Config) (switchDirectory, error) {
	client, err := newSwitchClient(cfg)
	if err != nil {
		return nil, err
	}
	return switchClientDirectory{client: client}, nil
}

// resolvePeopleDirectory returns the injected directory, or builds one from --config / HEIMDALL_CONFIG. A bad path or
// an incomplete [switch] section is an error for --list; interactive mapping falls back to typed sender ids instead.
func resolvePeopleDirectory(opts peopleOpts, deps peopleDeps) (switchDirectory, error) {
	if deps.directory != nil {
		return deps.directory, nil
	}
	if deps.loadConfig == nil || deps.newClient == nil {
		return nil, errors.New("switch directory is not configured")
	}
	var args []string
	if opts.configPath != "" {
		args = []string{"--config", opts.configPath}
	}
	path, err := config.FilePath(args, deps.getenv)
	if err != nil {
		return nil, err
	}
	cfg, err := deps.loadConfig(path)
	if err != nil {
		return nil, fmt.Errorf("load %s: %w", path, err)
	}
	dir, err := deps.newClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("connect to Switch: %w", err)
	}
	return dir, nil
}

// peopleOpts are the flags of `heimdall connect people`.
type peopleOpts struct {
	list       bool
	name       string
	sender     string
	github     string
	agent      string
	roles      string
	policyPath string
	configPath string
}

// personFields are the answers for one person, before they are parsed.
type personFields struct {
	name   string
	sender string
	github string
	agent  string
	roles  string
}

// runConnectPeople runs the command and returns its exit status.
func runConnectPeople(args []string, stdin io.Reader, stdout, stderr io.Writer, deps peopleDeps) int {
	opts, ok := parsePeopleArgs(args, stderr)
	if !ok {
		return connectExitUsage
	}
	if opts.list {
		if opts.name != "" || opts.mappingFlagSet() {
			peopleUsage(stderr, "--list takes no mapping flag")
			return connectExitUsage
		}
		dir, err := resolvePeopleDirectory(opts, deps)
		if err != nil {
			complain(stderr, "heimdall connect people --list: %v\n", err)
			return connectExitFail
		}
		return runPeopleList(stdout, stderr, deps, dir)
	}
	if opts.name == "" && opts.mappingFlagSet() {
		peopleUsage(stderr, "--sender, --github, --agent and --roles need --name")
		return connectExitUsage
	}
	var users []switchUser
	if opts.name == "" {
		dir, err := resolvePeopleDirectory(opts, deps)
		if err != nil {
			// Offline setup still works: the owner types each sender id by hand.
			complain(stderr, "heimdall connect people: Switch directory unavailable (%v); type sender ids by hand\n", err)
		} else {
			users = interactiveSwitchUsers(stdout, dir, deps.deadline)
		}
	}
	raw, pol, err := loadPeoplePolicy(opts.policyPath)
	if err != nil {
		complain(stderr, "heimdall connect people: %v\n", err)
		return connectExitFail
	}
	next, err := mapPeople(opts, pol, stdin, stdout, users)
	if err != nil {
		complain(stderr, "heimdall connect people: %v\n", err)
		return connectExitFail
	}
	return finishPeople(stdout, stderr, opts.policyPath, raw, pol, next)
}

// finishPeople writes the new table, unless it is the same as the old one, and reports what happened.
func finishPeople(stdout, stderr io.Writer, path string, raw []byte, before, after policy.Policy) int {
	out := &lineWriter{w: stdout}
	if policy.Equal(before, after) {
		out.printf("no change: %s already has these people\n", path)
	} else if err := writePeople(path, raw, after); err != nil {
		complain(stderr, "heimdall connect people: %v\n", err)
		return connectExitFail
	} else {
		out.printf("people written to %s (comments inside the people table are not kept)\n", path)
	}
	if out.err != nil {
		complain(stderr, "heimdall connect people: write output: %v\n", out.err)
		return connectExitFail
	}
	return 0
}

// mapPeople applies one mapping from the flags, or asks for mappings until the owner gives a blank name. users is the
// optional list of human Switch users the owner may pick from by number.
func mapPeople(opts peopleOpts, pol policy.Policy, stdin io.Reader, stdout io.Writer, users []switchUser) (policy.Policy, error) {
	if opts.name == "" {
		return promptPeople(bufio.NewReader(stdin), stdout, pol, users)
	}
	return applyFields(pol, personFields{
		name:   opts.name,
		sender: opts.sender,
		github: opts.github,
		agent:  opts.agent,
		roles:  opts.roles,
	})
}

// interactiveSwitchUsers fetches the human part of the Switch directory for the sender picker. A listing failure is
// not fatal here: the owner can still type a sender id. Agents are not people and are not offered for mapping.
func interactiveSwitchUsers(stdout io.Writer, dir switchDirectory, deadline time.Duration) []switchUser {
	ctx, cancel := context.WithTimeout(context.Background(), deadline)
	defer cancel()
	all, err := dir.ListUsers(ctx)
	if err != nil {
		out := &lineWriter{w: stdout}
		out.printf("Switch listing unavailable (%v); type each sender id by hand\n", err)
		return nil
	}
	var humans []switchUser
	for _, u := range all {
		if u.Type != "agent" {
			humans = append(humans, u)
		}
	}
	return humans
}

// applyFields parses the roles and applies the mapping. Every refusal comes from the app layer.
func applyFields(pol policy.Policy, f personFields) (policy.Policy, error) {
	roles, err := parseRoleList(f.roles)
	if err != nil {
		return policy.Policy{}, err
	}
	return people.ApplyMapping(pol, people.Mapping{
		Name:   f.name,
		Chat:   f.sender,
		GitHub: f.github,
		Agent:  f.agent,
		Roles:  roles,
	})
}

// parseRoleList splits a comma-separated role list. A blank list is no role, which means member.
func parseRoleList(s string) ([]people.Role, error) {
	var out []people.Role
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		r, err := people.ParseRole(part)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

// promptPeople asks for people until a blank name. A refused entry is reported, and the owner is asked for a name
// again. It returns the policy with every accepted entry applied, and nothing is written here.
func promptPeople(in *bufio.Reader, stdout io.Writer, pol policy.Policy, users []switchUser) (policy.Policy, error) {
	out := &lineWriter{w: stdout}
	printSwitchUsers(out, users)
	cur := pol
	for {
		name, err := askLine(in, out, "Person name (blank to finish): ")
		if err != nil && !errors.Is(err, io.EOF) {
			return policy.Policy{}, err
		}
		if name == "" {
			return cur, nil
		}
		fields, err := askFields(in, out, name, users)
		if err != nil {
			return policy.Policy{}, err
		}
		next, err := applyFields(cur, fields)
		if err != nil {
			out.printf("not saved: %v\n", err)
			continue
		}
		cur = next
	}
}

// printSwitchUsers shows the numbered human directory once, so the owner can answer a sender prompt with a number.
func printSwitchUsers(out *lineWriter, users []switchUser) {
	if len(users) == 0 {
		return
	}
	out.printf("Switch users (choose a number, or paste a sender id):\n")
	for i, u := range users {
		out.printf("  %d) %s  %s\n", i+1, u.Name, u.SenderID)
	}
}

// askFields asks for the sender id, the login, the agent name and the roles of one person.
func askFields(in *bufio.Reader, out *lineWriter, name string, users []switchUser) (personFields, error) {
	f := personFields{name: name}
	var err error
	if f.sender, err = askSender(in, out, name, users); err != nil {
		return f, err
	}
	if f.github, err = askLine(in, out, "GitHub login (blank if none): "); err != nil {
		return f, err
	}
	if f.agent, err = askLine(in, out, "Reviewer agent name (blank if none): "); err != nil {
		return f, err
	}
	if f.roles, err = askLine(in, out, "Roles, comma separated: member, author, approver (blank for member): "); err != nil {
		return f, err
	}
	return f, nil
}

// askSender reads one sender id. When the directory was listed, a 1-based number selects that user's sender id; any
// other answer is used as the sender id itself, so a switch id copied from chat still works.
func askSender(in *bufio.Reader, out *lineWriter, name string, users []switchUser) (string, error) {
	prompt := fmt.Sprintf("Switch sender id for %s (blank if none): ", name)
	if len(users) > 0 {
		prompt = fmt.Sprintf("Switch sender id for %s (number or id, blank if none): ", name)
	}
	answer, err := askLine(in, out, prompt)
	if err != nil {
		return "", err
	}
	if answer == "" {
		return "", nil
	}
	if len(users) > 0 {
		if idx, convErr := parseIndex(answer, len(users)); convErr == nil {
			return users[idx].SenderID, nil
		}
	}
	return answer, nil
}

// askLine writes the prompt and reads one answer. A read error other than end of input is returned.
func askLine(in *bufio.Reader, out *lineWriter, prompt string) (string, error) {
	out.printf("%s", prompt)
	if out.err != nil {
		return "", fmt.Errorf("write prompt: %w", out.err)
	}
	line, err := readLine(in)
	if err != nil {
		return "", fmt.Errorf("read answer: %w", err)
	}
	return line, nil
}

// parsePeopleArgs reads the flags. It prints the usage and returns false on a bad command line.
func parsePeopleArgs(args []string, stderr io.Writer) (peopleOpts, bool) {
	var opts peopleOpts
	fs := flag.NewFlagSet("heimdall connect people", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.BoolVar(&opts.list, "list", false, "list the Switch users")
	fs.StringVar(&opts.configPath, "config", "", "path to heimdall.toml (default: HEIMDALL_CONFIG)")
	fs.StringVar(&opts.name, "name", "", "person to map, as named in heimdall.yaml")
	fs.StringVar(&opts.sender, "sender", "", "the person's Switch sender id, copied from Switch")
	fs.StringVar(&opts.github, "github", "", "the person's GitHub login")
	fs.StringVar(&opts.agent, "agent", "", "the person's reviewer agent name")
	fs.StringVar(&opts.roles, "roles", "", "comma-separated roles: member, author, approver")
	fs.StringVar(&opts.policyPath, "policy", policyyaml.File, "policy file to edit")
	if err := fs.Parse(args); err != nil {
		return peopleOpts{}, false
	}
	if fs.NArg() > 0 {
		peopleUsage(stderr, "takes flags only, not a positional argument")
		return peopleOpts{}, false
	}
	return opts, true
}

// mappingFlagSet reports whether any flag that maps a person is set.
func (o peopleOpts) mappingFlagSet() bool {
	return o.sender != "" || o.github != "" || o.agent != "" || o.roles != ""
}

func peopleUsage(stderr io.Writer, reason string) {
	complain(stderr, "heimdall connect people: %s\nusage: heimdall connect people [--policy PATH] [--config PATH]\n       heimdall connect people --name NAME [--sender ID] [--github LOGIN] [--agent NAME] [--roles LIST] [--policy PATH] [--config PATH]\n       heimdall connect people --list [--config PATH]\n", reason)
}

// loadPeoplePolicy reads heimdall.yaml strictly and validates it. It returns the raw bytes too, so only the people
// table changes when the file is written.
func loadPeoplePolicy(path string) ([]byte, policy.Policy, error) {
	raw, err := fs.ReadFile(os.DirFS(filepath.Dir(path)), filepath.Base(path))
	if err != nil {
		return nil, policy.Policy{}, fmt.Errorf("read %s: %w", path, err)
	}
	pol, err := policyyaml.Decode(raw)
	if err != nil {
		return nil, policy.Policy{}, err
	}
	return raw, pol, nil
}

// writePeople replaces the people table in the file. It refuses unless the new file loads, validates and reads back
// as the table the command computed.
func writePeople(path string, raw []byte, next policy.Policy) error {
	out, err := setPeopleSection(raw, next.People)
	if err != nil {
		return err
	}
	back, err := policyyaml.Decode(out)
	if err != nil {
		return fmt.Errorf("refusing to write %s: %w", path, err)
	}
	if !policy.Equal(back, next) {
		return fmt.Errorf("refusing to write %s: the people table did not read back as computed", path)
	}
	return replaceFileAtomically(path, out)
}

// setPeopleSection replaces the value of the top-level people key, and keeps every other node and comment. It
// adds the key when it is missing.
func setPeopleSection(raw []byte, table map[string]policy.Person) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parse heimdall.yaml: %w", err)
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("heimdall.yaml must be a mapping at the top level")
	}
	var value yaml.Node
	if err := value.Encode(table); err != nil {
		return nil, fmt.Errorf("encode people: %w", err)
	}
	root := doc.Content[0]
	replaced := false
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == "people" {
			root.Content[i+1] = &value
			replaced = true
			break
		}
	}
	if !replaced {
		root.Content = append(root.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: "people"}, &value)
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return nil, fmt.Errorf("encode heimdall.yaml: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("close encoder: %w", err)
	}
	return buf.Bytes(), nil
}

// runPeopleList prints the Switch users the directory returns.
func runPeopleList(stdout, stderr io.Writer, deps peopleDeps, dir switchDirectory) int {
	ctx, cancel := context.WithTimeout(context.Background(), deps.deadline)
	defer cancel()
	users, err := dir.ListUsers(ctx)
	if err != nil {
		complain(stderr, "heimdall connect people --list: %v\n", err)
		return connectExitFail
	}
	if err := writeUserTable(stdout, users); err != nil {
		complain(stderr, "heimdall connect people --list: %v\n", err)
		return connectExitFail
	}
	return 0
}

func writeUserTable(w io.Writer, users []switchUser) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	out := &lineWriter{w: tw}
	out.printf("NAME\tSENDER ID\tTYPE\n")
	for _, u := range users {
		out.printf("%s\t%s\t%s\n", u.Name, u.SenderID, u.Type)
	}
	if out.err != nil {
		return fmt.Errorf("write users: %w", out.err)
	}
	if err := tw.Flush(); err != nil {
		return fmt.Errorf("flush table: %w", err)
	}
	return nil
}
