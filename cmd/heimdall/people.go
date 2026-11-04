// `heimdall connect people`: maps Switch sender ids to the people in heimdall.yaml (D140, D95, D158).
//
// # Purpose
//
// A chat command counts only when its Switch sender id maps to a person (D140). This command fills that map. The
// owner gives a person name, the sender id copied from Switch, and the optional GitHub login and agent name. The
// rules that refuse a bad mapping are the app layer's (app/people). This file parses the flags, asks the questions,
// and writes the result. `--list` shows the Switch users through an injected directory.
//
// # API
//
//   - runConnectPeople(args, stdin, stdout, stderr, deps) int: the command. Exit 0 on success or no change,
//     1 on a refused mapping or a failed write, 2 on a bad command line. It takes flags only.
//   - peopleDeps and switchDirectory: the injected Switch listing. realPeopleDeps wires the directory that is not
//     built yet.
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
//   - The Switch listing is not built. `--list` says so and exits 1, and the owner types each sender id by hand.
//
// # Tests
//
// people_test.go uses a fake directory and temp heimdall.yaml files copied from the policy fixtures. It covers each
// refusal with the file unchanged, the interactive loop, the list and its errors, and the dispatch from connect.
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
	people "github.com/prathameshk421/flip_the_switch_hackathon/internal/app/people"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
)

// switchUser is one entry of the Switch directory. Type is "user" or "agent", as Switch reports it.
type switchUser struct {
	Name string
	Type string
}

// switchDirectory lists the Switch users. It must honour the context's deadline.
type switchDirectory interface {
	ListUsers(ctx context.Context) ([]switchUser, error)
}

// peopleDeps is what the people command touches outside this file.
type peopleDeps struct {
	directory switchDirectory
	deadline  time.Duration
}

// notBuiltDirectory is the real directory until the Switch listing is built. It says what to do instead.
type notBuiltDirectory struct{}

// ListUsers reports that the listing is not built, and what the owner does instead.
func (notBuiltDirectory) ListUsers(context.Context) ([]switchUser, error) {
	return nil, errors.New("listing Switch users is not built yet; type each sender id by hand")
}

func realPeopleDeps() peopleDeps {
	return peopleDeps{directory: notBuiltDirectory{}, deadline: connectCheckDeadline}
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
		return runPeopleList(stdout, stderr, deps)
	}
	if opts.name == "" && opts.mappingFlagSet() {
		peopleUsage(stderr, "--sender, --github, --agent and --roles need --name")
		return connectExitUsage
	}
	raw, pol, err := loadPeoplePolicy(opts.policyPath)
	if err != nil {
		complain(stderr, "heimdall connect people: %v\n", err)
		return connectExitFail
	}
	next, err := mapPeople(opts, pol, stdin, stdout)
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

// mapPeople applies one mapping from the flags, or asks for mappings until the owner gives a blank name.
func mapPeople(opts peopleOpts, pol policy.Policy, stdin io.Reader, stdout io.Writer) (policy.Policy, error) {
	if opts.name == "" {
		return promptPeople(bufio.NewReader(stdin), stdout, pol)
	}
	return applyFields(pol, personFields{
		name:   opts.name,
		sender: opts.sender,
		github: opts.github,
		agent:  opts.agent,
		roles:  opts.roles,
	})
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
func promptPeople(in *bufio.Reader, stdout io.Writer, pol policy.Policy) (policy.Policy, error) {
	out := &lineWriter{w: stdout}
	cur := pol
	for {
		name, err := askLine(in, out, "Person name (blank to finish): ")
		if err != nil && !errors.Is(err, io.EOF) {
			return policy.Policy{}, err
		}
		if name == "" {
			return cur, nil
		}
		fields, err := askFields(in, out, name)
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

// askFields asks for the sender id, the login, the agent name and the roles of one person.
func askFields(in *bufio.Reader, out *lineWriter, name string) (personFields, error) {
	f := personFields{name: name}
	var err error
	if f.sender, err = askLine(in, out, fmt.Sprintf("Switch sender id for %s (blank if none): ", name)); err != nil {
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
	fs.BoolVar(&opts.list, "list", false, "list the Switch users (the listing is not built yet)")
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
	complain(stderr, "heimdall connect people: %s\nusage: heimdall connect people [--policy PATH]\n       heimdall connect people --name NAME [--sender ID] [--github LOGIN] [--agent NAME] [--roles LIST] [--policy PATH]\n       heimdall connect people --list\n", reason)
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
func runPeopleList(stdout, stderr io.Writer, deps peopleDeps) int {
	ctx, cancel := context.WithTimeout(context.Background(), deps.deadline)
	defer cancel()
	users, err := deps.directory.ListUsers(ctx)
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
	out.printf("NAME\tTYPE\n")
	for _, u := range users {
		out.printf("%s\t%s\n", u.Name, u.Type)
	}
	if out.err != nil {
		return fmt.Errorf("write users: %w", out.err)
	}
	if err := tw.Flush(); err != nil {
		return fmt.Errorf("flush table: %w", err)
	}
	return nil
}
