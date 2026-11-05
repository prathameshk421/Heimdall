// `heimdall connect`: lists the integrations, names the secret files each one needs, and writes the
// provider selection to heimdall.toml.
//
// # Purpose
//
// The admin's once-per-install setup step. `connect list` shows every registered adapter with whether
// heimdall.toml selects it and what its read-only Check says. `connect <kind> [name]` plans one
// integration: the non-secret answers it needs, the secret files to create under secrets_dir, and the
// selection to write. Then it writes the selection and runs the adapter's Check.
//
// # API
//
//   - runConnect(args, stdin, stdout, stderr, deps) int: the command. Flags may come before, between or
//     after positional arguments. Exit 0 on success, 1 on a failed operation, 2 on a bad command line.
//   - PlanConnect(reg, cfg, kind, name, answers) (ConnectPlan, error): the non-interactive core. It holds
//     no secret value. The picker is a thin loop over it.
//   - checkWithDeadline: runs Entry.Check under a deadline. A check that does not answer in time is
//     reported as unreachable.
//   - writeSelection(path, selection): rewrites only the selection keys through a go-toml round trip,
//     atomically (temp file in the same directory, then rename). Comments are not kept.
//   - defaultRegistry(): the one place the binary names its adapters. Real adapters register here as they
//     land.
//
// # Invariants
//
//   - A secret is never an argument and is never read from stdin. The command only names the file the
//     admin must create and checks that it exists through config.Secret. Errors name the secret, never its
//     value.
//   - A check never reports connected while its adapter is not built. Not-built adapters report
//     needs_config with the detail "adapter not built yet".
//   - writeSelection refuses a heimdall.toml that does not load strictly, and leaves the file unchanged.
//   - Every value written to heimdall.toml is a registry name, so the server's ValidateSelections accepts
//     it.
//
// # Failure modes
//
//   - Unknown kind or name, or a bad answer such as a public address that is not a host name: a usage or
//     plan error, nothing is written.
//   - A config path that is missing or does not load: exit 1, nothing is written.
//   - A write that fails: the temp file is removed and the original file is kept.
//   - A check that hangs: unreachable after the deadline (10 s in the binary).
//
// # Tests
//
// connect_test.go uses fakes and temp directories. It plants a secret value in secret files and proves no
// output, JSON, plan or error contains it. It covers every planned entry's check, the list table and JSON,
// the deadline, the plan, the atomic write and its refusal, argument errors, and the picker.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	toml "github.com/pelletier/go-toml/v2"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/registry"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/config"
)

const (
	connectExitFail  = 1
	connectExitUsage = 2
	// connectCheckDeadline bounds one adapter Check in `connect list`, so one hung system cannot hang the list.
	connectCheckDeadline = 10 * time.Second
)

// errNotBuilt is the detail of every adapter whose code has not landed yet.
var errNotBuilt = errors.New("adapter not built yet")

// hostPattern is a DNS host name with at least one dot, lowercase, no scheme, port or path.
var hostPattern = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// connectDeps is everything the connect command touches outside this file.
type connectDeps struct {
	getenv        func(name string) string
	loadConfig    func(path string) (config.Config, error)
	registry      func() (*registry.Registry, error)
	checkDeadline time.Duration
}

// connectRow is one entry of `heimdall connect list`. It carries names and states, never secret values.
type connectRow struct {
	Name           string   `json:"name"`
	Kind           string   `json:"kind"`
	Selected       bool     `json:"selected"`
	Status         string   `json:"status"`
	Detail         string   `json:"detail"`
	RequiredConfig []string `json:"required_config"`
}

// SecretFile is one secret file the admin must create under secrets_dir.
type SecretFile struct {
	Name    string
	Path    string
	Present bool
	// Problem says why the file is not usable yet. It names the secret and never carries its value.
	Problem string
}

// ConnectPlan is what connecting one integration needs. It holds no secret value.
type ConnectPlan struct {
	Kind         registry.Kind
	Name         string
	Selection    map[string]string
	SecretFiles  []SecretFile
	Instructions []string
}

// connectOpts are the flags of `heimdall connect`.
type connectOpts struct {
	asJSON        bool
	configPath    string
	publicAddress string
}

// connectEnv is the loaded config and registry a command works from.
type connectEnv struct {
	path string
	cfg  config.Config
	reg  *registry.Registry
}

func realConnectDeps() connectDeps {
	return connectDeps{
		getenv:        config.Getenv,
		loadConfig:    config.Load,
		registry:      defaultRegistry,
		checkDeadline: connectCheckDeadline,
	}
}

// defaultRegistry lists the integrations this binary knows. Real adapters replace the not-built entries
// here as they land. Until then no entry reports connected.
func defaultRegistry() (*registry.Registry, error) {
	githubSecrets := []string{"github_app_id", "github_app_installation_id", "github_app_private_key"}
	return registry.Default([]registry.Entry{
		{Name: "github", Kind: registry.KindVCS, RequiredConfig: githubSecrets, Factory: notBuiltFactory, Check: notBuiltCheck(githubSecrets...)},
		{Name: "file", Kind: registry.KindTracker, Factory: notBuiltFactory, Check: notBuiltCheck()},
		{Name: "jira", Kind: registry.KindTracker, RequiredConfig: []string{"jira_api_token"}, Factory: notBuiltFactory, Check: notBuiltCheck("jira_api_token")},
		{Name: "mattermost", Kind: registry.KindChat, RequiredConfig: []string{"mattermost_token"}, Factory: notBuiltFactory, Check: notBuiltCheck("mattermost_token")},
		{Name: "teams", Kind: registry.KindChat, Factory: notBuiltFactory, Check: notBuiltCheck()},
	})
}

func notBuiltFactory(config.Config) (any, error) {
	return nil, errNotBuilt
}

// notBuiltCheck reports needs_config for an adapter whose code has not landed. It names every secret file
// that is missing or unusable, and otherwise says the adapter is not built. It never reports connected.
func notBuiltCheck(secrets ...string) func(context.Context, config.Config) registry.CheckResult {
	return func(_ context.Context, cfg config.Config) registry.CheckResult {
		var missing []string
		for _, name := range secrets {
			if _, err := cfg.Secret(name); err != nil {
				missing = append(missing, err.Error())
			}
		}
		if len(missing) > 0 {
			return registry.CheckResult{Status: registry.StatusNeedsConfig, Detail: "secret files missing or unusable: " + strings.Join(missing, "; ")}
		}
		detail := errNotBuilt.Error()
		if len(secrets) > 0 {
			detail += "; its secret files are in place"
		}
		return registry.CheckResult{Status: registry.StatusNeedsConfig, Detail: detail}
	}
}

func runConnect(args []string, stdin io.Reader, stdout, stderr io.Writer, deps connectDeps) int {
	if len(args) > 0 && args[0] == "people" {
		return runConnectPeople(args[1:], stdin, stdout, stderr, realPeopleDeps())
	}
	opts, positional, ok := parseConnectArgs(args, stderr)
	if !ok {
		return connectExitUsage
	}
	if len(positional) == 0 {
		return connectUsage(stderr, "expected list, or a kind: vcs, tracker, chat, agent_runtime or memory")
	}
	if positional[0] == "list" {
		if len(positional) != 1 || opts.publicAddress != "" {
			return connectUsage(stderr, "list takes only --json and --config")
		}
		return runConnectList(opts, stdout, stderr, deps)
	}
	if opts.asJSON {
		return connectUsage(stderr, "--json applies to list only")
	}
	if len(positional) > 2 {
		// Never echo a positional value here: a secret pasted by mistake must not reach the terminal.
		return connectUsage(stderr, "expected a kind and an optional name; secrets are never arguments")
	}
	kind := registry.Kind(positional[0])
	if !kind.Valid() {
		return connectUsage(stderr, "unknown kind; kinds are vcs, tracker, chat, agent_runtime, memory")
	}
	name := ""
	if len(positional) == 2 {
		name = positional[1]
	}
	return runConnectKind(kind, name, opts, stdin, stdout, stderr, deps)
}

// parseConnectArgs reads the flags wherever they appear among the positional arguments. ok is false when
// the flag package refused the command line; it has already written the reason to stderr.
func parseConnectArgs(args []string, stderr io.Writer) (connectOpts, []string, bool) {
	var opts connectOpts
	fs := flag.NewFlagSet("heimdall connect", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.BoolVar(&opts.asJSON, "json", false, "print the list as JSON (list only)")
	fs.StringVar(&opts.configPath, "config", "", "path to heimdall.toml (default: HEIMDALL_CONFIG)")
	fs.StringVar(&opts.publicAddress, "public-address", "", "public host name for mattermost, or \"internal only\"")
	var positional []string
	rest := args
	for {
		if err := fs.Parse(rest); err != nil {
			return connectOpts{}, nil, false
		}
		if fs.NArg() == 0 {
			break
		}
		positional = append(positional, fs.Arg(0))
		rest = fs.Args()[1:]
	}
	return opts, positional, true
}

func connectUsage(stderr io.Writer, reason string) int {
	complain(stderr, "heimdall connect: %s\nusage: heimdall connect list [--json] [--config PATH]\n       heimdall connect <kind> [name] [--public-address VALUE] [--config PATH]\n", reason)
	return connectExitUsage
}

func loadConnectEnv(configPath string, deps connectDeps) (connectEnv, error) {
	var flagArgs []string
	if configPath != "" {
		flagArgs = []string{"--config", configPath}
	}
	path, err := config.FilePath(flagArgs, deps.getenv)
	if err != nil {
		return connectEnv{}, err
	}
	cfg, err := deps.loadConfig(path)
	if err != nil {
		return connectEnv{}, err
	}
	reg, err := deps.registry()
	if err != nil {
		return connectEnv{}, fmt.Errorf("build integration registry: %w", err)
	}
	return connectEnv{path: path, cfg: cfg, reg: reg}, nil
}

// runConnectList prints every registered integration with its selection, its status and what it needs.
func runConnectList(opts connectOpts, stdout, stderr io.Writer, deps connectDeps) int {
	env, err := loadConnectEnv(opts.configPath, deps)
	if err != nil {
		complain(stderr, "heimdall connect list: %v\n", err)
		return connectExitFail
	}
	if err := registry.ValidateSelections(env.cfg, env.reg); err != nil {
		complain(stderr, "heimdall connect list: %v\n", err)
		return connectExitFail
	}
	rows := make([]connectRow, 0)
	for _, d := range env.reg.Describe() {
		e, err := env.reg.Select(d.Kind, d.Name)
		if err != nil {
			complain(stderr, "heimdall connect list: %v\n", err)
			return connectExitFail
		}
		sel, selectedOK := env.cfg.Selection(string(d.Kind))
		r := checkWithDeadline(context.Background(), e, env.cfg, deps.checkDeadline)
		required := d.RequiredConfig
		if required == nil {
			required = []string{}
		}
		rows = append(rows, connectRow{
			Name:           d.Name,
			Kind:           string(d.Kind),
			Selected:       selectedOK && sel == d.Name,
			Status:         string(r.Status),
			Detail:         r.Detail,
			RequiredConfig: required,
		})
	}
	if opts.asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rows); err != nil {
			complain(stderr, "heimdall connect list: write JSON: %v\n", err)
			return connectExitFail
		}
		return 0
	}
	if err := writeConnectTable(stdout, rows); err != nil {
		complain(stderr, "heimdall connect list: %v\n", err)
		return connectExitFail
	}
	return 0
}

func writeConnectTable(w io.Writer, rows []connectRow) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, "NAME\tKIND\tSELECTED\tSTATUS\tDETAIL"); err != nil {
		return fmt.Errorf("write header: %w", err)
	}
	for _, r := range rows {
		selected := "no"
		if r.Selected {
			selected = "yes"
		}
		if _, err := fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", r.Name, r.Kind, selected, r.Status, r.Detail); err != nil {
			return fmt.Errorf("write row %s: %w", r.Name, err)
		}
	}
	if err := tw.Flush(); err != nil {
		return fmt.Errorf("flush table: %w", err)
	}
	return nil
}

// runConnectKind connects one integration. Without a name it runs the picker. Answers the entry needs and
// the admin did not give are asked for only in the picker; in a script they are an error.
func runConnectKind(kind registry.Kind, name string, opts connectOpts, stdin io.Reader, stdout, stderr io.Writer, deps connectDeps) int {
	env, err := loadConnectEnv(opts.configPath, deps)
	if err != nil {
		complain(stderr, "heimdall connect: %v\n", err)
		return connectExitFail
	}
	answers := map[string]string{}
	if opts.publicAddress != "" {
		answers["public_address"] = opts.publicAddress
	}
	in := bufio.NewReader(stdin)
	if name == "" {
		name, err = pickName(in, stdout, env.reg, kind)
		if err != nil {
			complain(stderr, "heimdall connect: %v\n", err)
			return connectExitFail
		}
		if err := promptAnswers(in, stdout, kind, name, answers); err != nil {
			complain(stderr, "heimdall connect: %v\n", err)
			return connectExitFail
		}
	}
	plan, err := PlanConnect(env.reg, env.cfg, kind, name, answers)
	if err != nil {
		complain(stderr, "heimdall connect: %v\n", err)
		return connectExitFail
	}
	return applyConnect(stdout, stderr, plan, env, deps)
}

// applyConnect writes the selection, prints what the admin must still do, and runs the read-only check.
func applyConnect(stdout, stderr io.Writer, plan ConnectPlan, env connectEnv, deps connectDeps) int {
	if err := writeSelection(env.path, plan.Selection); err != nil {
		complain(stderr, "heimdall connect: %v\n", err)
		return connectExitFail
	}
	e, err := env.reg.Select(plan.Kind, plan.Name)
	if err != nil {
		complain(stderr, "heimdall connect: %v\n", err)
		return connectExitFail
	}
	r := checkWithDeadline(context.Background(), e, env.cfg, deps.checkDeadline)
	out := &lineWriter{w: stdout}
	out.printf("%s %s selected in %s (comments in the file are not kept)\n", plan.Kind, plan.Name, env.path)
	for _, line := range plan.Instructions {
		out.printf("  %s\n", line)
	}
	out.printf("Check: %s: %s\n", r.Status, r.Detail)
	if out.err != nil {
		complain(stderr, "heimdall connect: write output: %v\n", out.err)
		return connectExitFail
	}
	return 0
}

// lineWriter writes to w and keeps the first write error, so a run of prints is checked once.
type lineWriter struct {
	w   io.Writer
	err error
}

func (lw *lineWriter) printf(format string, args ...any) {
	if lw.err == nil {
		_, lw.err = fmt.Fprintf(lw.w, format, args...)
	}
}

// pickName asks for an integration by number or by name, until the answer is one of the names.
func pickName(in *bufio.Reader, stdout io.Writer, reg *registry.Registry, kind registry.Kind) (string, error) {
	names := reg.Names(kind)
	if len(names) == 0 {
		return "", fmt.Errorf("no %s integrations are registered", kind)
	}
	out := &lineWriter{w: stdout}
	out.printf("Integrations for %s:\n", kind)
	for i, n := range names {
		out.printf("  %d) %s\n", i+1, n)
	}
	for {
		out.printf("Choose a number or a name: ")
		if out.err != nil {
			return "", fmt.Errorf("write menu: %w", out.err)
		}
		line, err := readLine(in)
		if err != nil {
			return "", fmt.Errorf("read choice: %w", err)
		}
		if n, convErr := parseIndex(line, len(names)); convErr == nil {
			return names[n], nil
		}
		if slices.Contains(names, line) {
			return line, nil
		}
		out.printf("not a valid choice, try again\n")
	}
}

// parseIndex turns a 1-based menu number into a 0-based index.
func parseIndex(line string, count int) (int, error) {
	n, err := strconv.Atoi(line)
	if err != nil {
		return 0, fmt.Errorf("not a number: %w", err)
	}
	if n < 1 || n > count {
		return 0, fmt.Errorf("number %d is out of range", n)
	}
	return n - 1, nil
}

// promptAnswers asks for each answer the integration needs that answers does not hold yet, until each is valid.
func promptAnswers(in *bufio.Reader, stdout io.Writer, kind registry.Kind, name string, answers map[string]string) error {
	out := &lineWriter{w: stdout}
	for _, key := range answerKeys(kind, name) {
		if _, ok := answers[key]; ok {
			continue
		}
		for {
			out.printf("%s", answerQuestion(key))
			if out.err != nil {
				return fmt.Errorf("write prompt: %w", out.err)
			}
			line, err := readLine(in)
			if err != nil {
				return fmt.Errorf("read %s: %w", key, err)
			}
			if err := validateAnswer(key, line); err != nil {
				out.printf("%v\n", err)
				continue
			}
			answers[key] = line
			break
		}
	}
	return nil
}

// readLine returns the next line without its line break. A final line without a break is still a line.
func readLine(in *bufio.Reader) (string, error) {
	line, err := in.ReadString('\n')
	if err != nil && (!errors.Is(err, io.EOF) || line == "") {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

// answerKeys lists the non-secret answers an integration needs. Only Mattermost asks one so far: its public address (D109).
func answerKeys(kind registry.Kind, name string) []string {
	if kind == registry.KindChat && name == "mattermost" {
		return []string{"public_address"}
	}
	return nil
}

func answerQuestion(key string) string {
	if key == "public_address" {
		return "Public address for Mattermost (a host name such as chat.example.org, or \"internal only\"): "
	}
	return key + ": "
}

// validateAnswer checks one answer. Its message never repeats the value.
func validateAnswer(key, value string) error {
	if key == "public_address" {
		_, _, err := parsePublicAddress(value)
		return err
	}
	return nil
}

// PlanConnect is the non-interactive core. It checks the name and every answer, and returns the selection
// to write, the secret files to create and what the admin must still do in the other app. It reads secret
// files only to see whether they exist and are usable.
func PlanConnect(reg *registry.Registry, cfg config.Config, kind registry.Kind, name string, answers map[string]string) (ConnectPlan, error) {
	entry, err := reg.Select(kind, name)
	if err != nil {
		var se *registry.SelectError
		if errors.As(err, &se) {
			// The name is not echoed: it may be a secret pasted by mistake.
			valid := strings.Join(se.Valid, ", ")
			if valid == "" {
				valid = "none are registered"
			}
			return ConnectPlan{}, fmt.Errorf("connect %s: no integration with that name; valid names are %s: %w", kind, valid, registry.ErrUnknownName)
		}
		return ConnectPlan{}, fmt.Errorf("connect %s: %w", kind, err)
	}

	asked := answerKeys(kind, name)
	for key := range answers {
		if !slices.Contains(asked, key) {
			return ConnectPlan{}, fmt.Errorf("connect %s %s: answer %q is not asked for by this integration", kind, name, key)
		}
	}
	var instructions []string
	for _, key := range asked {
		value, ok := answers[key]
		if !ok {
			return ConnectPlan{}, fmt.Errorf("connect %s %s: needs answer %q", kind, name, key)
		}
		if err := validateAnswer(key, value); err != nil {
			return ConnectPlan{}, fmt.Errorf("connect %s %s: %s: %w", kind, name, key, err)
		}
		host, internal, _ := parsePublicAddress(value)
		instructions = append(instructions, publicAddressInstructions(host, internal)...)
	}

	plan := ConnectPlan{
		Kind:      kind,
		Name:      name,
		Selection: map[string]string{registry.SelectionField(kind): name},
	}
	for _, key := range entry.RequiredConfig {
		sf := SecretFile{Name: key, Path: filepath.Join(cfg.SecretsDir, key)}
		if _, err := cfg.Secret(key); err != nil {
			sf.Problem = err.Error()
		} else {
			sf.Present = true
		}
		plan.SecretFiles = append(plan.SecretFiles, sf)
		if sf.Present {
			plan.Instructions = append(plan.Instructions, fmt.Sprintf("secret file %s is in place", sf.Path))
		} else {
			plan.Instructions = append(plan.Instructions, fmt.Sprintf("create %s with the value on one line, mode 0600 (%s). This command never reads or accepts the value", sf.Path, sf.Problem))
		}
	}
	plan.Instructions = append(plan.Instructions, instructions...)
	return plan, nil
}

// publicAddressInstructions says what to change in Mattermost and Switch for the chosen address.
func publicAddressInstructions(host string, internal bool) []string {
	if internal {
		return []string{"public address: none (internal only). Mattermost stays on the private network; no Site URL change is needed"}
	}
	return []string{fmt.Sprintf("set the Mattermost Site URL and the Switch connection's Public Url to https://%s (docs/MATTERMOST_ACCESS.md). The address is not written to heimdall.toml: the file has no key for it yet", host)}
}

// parsePublicAddress accepts "internal only" or "none", or a bare host name with at least one dot. It
// returns the lowercase host, or internal true for an internal-only deployment.
func parsePublicAddress(s string) (string, bool, error) {
	v := strings.ToLower(strings.TrimSpace(s))
	if v == "internal only" || v == "none" {
		return "", true, nil
	}
	if !hostPattern.MatchString(v) {
		return "", false, errors.New("public address must be a host name such as chat.example.org, or \"internal only\"")
	}
	return v, false, nil
}

// checkWithDeadline runs e.Check and gives up after d. A check that ignores its context cannot hang the caller.
func checkWithDeadline(parent context.Context, e registry.Entry, cfg config.Config, d time.Duration) registry.CheckResult {
	ctx, cancel := context.WithTimeout(parent, d)
	defer cancel()
	done := make(chan registry.CheckResult, 1)
	go func() {
		done <- e.Check(ctx, cfg)
	}()
	select {
	case r := <-done:
		return r
	case <-ctx.Done():
		return registry.CheckResult{Status: registry.StatusUnreachable, Detail: fmt.Sprintf("check did not answer within %s", d)}
	}
}

// writeSelection sets each section.key in sel in heimdall.toml. It refuses a file that does not load
// strictly, rewrites through a go-toml round trip (comments are not kept), and replaces the file atomically.
func writeSelection(path string, sel map[string]string) error {
	doc, err := loadSelectionDoc(path)
	if err != nil {
		return err
	}
	if err := applySelection(doc, sel); err != nil {
		return err
	}
	out, err := toml.Marshal(doc)
	if err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	return replaceFileAtomically(path, out)
}

// loadSelectionDoc returns the parsed heimdall.toml, or an error if the file does not load strictly.
// It reads through a directory FS rooted at the file's own directory.
func loadSelectionDoc(path string) (map[string]any, error) {
	if _, err := config.Load(path); err != nil {
		return nil, fmt.Errorf("refusing to edit heimdall.toml: %w", err)
	}
	raw, err := fs.ReadFile(os.DirFS(filepath.Dir(path)), filepath.Base(path))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var doc map[string]any
	if err := toml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return doc, nil
}

// applySelection sets each section.key of sel in doc, creating the section when it is missing.
func applySelection(doc map[string]any, sel map[string]string) error {
	for field, value := range sel {
		section, key, ok := strings.Cut(field, ".")
		if !ok {
			return fmt.Errorf("selection field %q must be section.key", field)
		}
		tbl, _ := doc[section].(map[string]any)
		if tbl == nil {
			tbl = map[string]any{}
			doc[section] = tbl
		}
		tbl[key] = value
	}
	return nil
}

// replaceFileAtomically writes data to a temp file beside path, then renames it over path with path's mode.
// On any failure the temp file is removed and path is left as it was.
func replaceFileAtomically(path string, data []byte) (err error) {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat %s: %w", path, err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".heimdall-connect-*.toml")
	if err != nil {
		return fmt.Errorf("create temporary file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		if err != nil {
			// The temp file is ours alone; if removing it fails there is nothing more the admin can act on.
			_ = os.Remove(tmpName)
		}
	}()
	if err = writeAndClose(tmp, data); err != nil {
		return err
	}
	if err = os.Chmod(tmpName, info.Mode().Perm()); err != nil {
		return fmt.Errorf("chmod %s: %w", tmpName, err)
	}
	if err = os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}

// writeAndClose writes data, syncs it to disk and closes f. It closes f on every path.
func writeAndClose(f *os.File, data []byte) error {
	name := f.Name()
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return fmt.Errorf("write %s: %w", name, err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("sync %s: %w", name, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close %s: %w", name, err)
	}
	return nil
}
