package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/graphiti"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/rootlessrunner"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/outbox"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/taskinput"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/transport/mcp"
	"github.com/prathameshk421/flip_the_switch_hackathon/skills"
)

// The boot checks (HARNESS_DESIGN B8). Two kinds of problem are kept apart:
//   - a wiring problem (the kind table names a tool nobody registered, a row starts a job kind with no executor) is a
//     bug in this binary, so the server does not start and every problem is named at once;
//   - a dependency that does not answer at boot (the runner, the code host, knowledge search) is an outage, so the
//     server starts without the tools that call it, logs one line per withheld tool and says so on /healthz;
//   - a tool the config turns off (read_card without cards_dir) is never built. The table check counts it as
//     present, and it is logged and reported like a withheld tool, so nobody mistakes it for an outage they missed.
//
// The self-check runs once, at boot. A dependency that comes back later is offered again after a restart.

// selfCheckTimeout bounds each dependency probe at boot.
const selfCheckTimeout = 10 * time.Second

// dependency is one outside system that some tools call.
type dependency struct {
	name   string
	tools  []string // withheld when the probe fails
	reason string   // the plain reason, such as "runner unreachable"
	probe  func(context.Context) error
}

// withheldReason is a probe error that carries its own reason, when the dependency answered but cannot serve the tools.
type withheldReason string

func (r withheldReason) Error() string { return string(r) }

// selfCheck is what the boot self-check found: each withheld tool with its reason, each tool the config turns off, and
// the lines /healthz reports for both.
type selfCheck struct {
	withheld map[string]string // built but not offered
	off      []string          // not built at all
	degraded []string
}

// turnOff records a tool the config turns off, with one log line and one /healthz line.
func (c *selfCheck) turnOff(ctx context.Context, logger *slog.Logger, tool, reason string) {
	c.off = append(c.off, tool)
	c.degraded = append(c.degraded, tool+": "+reason)
	logger.WarnContext(ctx, tool+" withheld: "+reason)
}

// runSelfCheck probes every dependency at once, each within timeout, and withholds the tools of each one that failed.
// It writes one WARN line per withheld tool, such as "run_check withheld: runner unreachable", with the cause.
func runSelfCheck(ctx context.Context, logger *slog.Logger, timeout time.Duration, deps []dependency) selfCheck {
	errs := make([]error, len(deps))
	var wg sync.WaitGroup
	for i, dep := range deps {
		wg.Add(1)
		go func() {
			defer wg.Done()
			probeCtx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			errs[i] = dep.probe(probeCtx)
		}()
	}
	wg.Wait()
	out := selfCheck{withheld: map[string]string{}}
	for i, dep := range deps {
		if errs[i] == nil {
			continue
		}
		reason := dep.reason
		var own withheldReason
		if errors.As(errs[i], &own) {
			reason = string(own)
		}
		for _, tool := range dep.tools {
			out.withheld[tool] = reason
			out.degraded = append(out.degraded, tool+": "+reason)
			logger.WarnContext(ctx, tool+" withheld: "+reason, "dependency", dep.name, "cause", errs[i].Error())
		}
	}
	return out
}

// bootSelfCheck probes the dependencies of the production tools: the runner, the code host and knowledge search.
func bootSelfCheck(ctx context.Context, d deps, ad adapters, blobs ports.BlobStore) (selfCheck, error) {
	memory, err := graphiti.NewHTTPCaller(d.endpoints.graphitiURL, d.httpClient)
	if err != nil {
		return selfCheck{}, fmt.Errorf("build knowledge probe: %w", err)
	}
	check := runSelfCheck(ctx, d.logger, selfCheckTimeout, []dependency{
		// Every profile tool reaches the runner: propose reads its image allowlist, trial runs a job, promote syncs.
		{name: "runner", tools: []string{"run_check", "propose_profile", "promote_profile"},
			reason: "runner unreachable", probe: runnerProbe(ad.runner(blobs))},
		{name: "code host", tools: []string{"read_code", "search_code", "find_symbol"},
			reason: "code host unreachable", probe: codeHostProbe(ad.vcs, ad.repos)},
		{name: "knowledge search", tools: []string{"ask_knowledge"}, reason: "knowledge search unreachable", probe: memory.Ping},
	})
	if d.configuration.CardsDir == "" {
		check.turnOff(ctx, d.logger, "read_card", "cards_dir is not configured")
	}
	return check, nil
}

// runnerHealth is the part of the runner client the probe calls.
type runnerHealth interface {
	Health(ctx context.Context) ([]ports.SandboxKind, error)
}

var _ runnerHealth = rootlessrunner.Client{}

// runnerProbe asks the runner for its health and requires the check and test job kinds the tools send.
func runnerProbe(r runnerHealth) func(context.Context) error {
	return func(ctx context.Context) error {
		kinds, err := r.Health(ctx)
		if err != nil {
			return fmt.Errorf("runner health: %w", err)
		}
		var missing []string
		for _, want := range []ports.SandboxKind{ports.SandboxCheck, ports.SandboxTest} {
			if !slices.Contains(kinds, want) {
				missing = append(missing, string(want))
			}
		}
		if len(missing) > 0 {
			return withheldReason("runner does not run " + strings.Join(missing, " and ") + " jobs")
		}
		return nil
	}
}

// codeReads is the part of the code host the probe calls.
type codeReads interface {
	DefaultHead(ctx context.Context, repo change.Repo) (id.SHA, error)
	ReadFile(ctx context.Context, repo change.Repo, sha id.SHA, path string) ([]byte, error)
}

// codeHostProbe reads one file at the default branch of each mapped repository. A missing file, or an empty repository,
// is still an answer from the code host.
func codeHostProbe(code codeReads, repos []change.Repo) func(context.Context) error {
	return func(ctx context.Context) error {
		for _, repo := range repos {
			head, err := code.DefaultHead(ctx, repo)
			if errors.Is(err, ports.ErrNotFound) {
				continue
			}
			if err != nil {
				return fmt.Errorf("default head of %s: %w", repo, err)
			}
			if _, err := code.ReadFile(ctx, repo, head, "heimdall.yaml"); err != nil && !errors.Is(err, ports.ErrNotFound) {
				return fmt.Errorf("read heimdall.yaml of %s: %w", repo, err)
			}
		}
		return nil
	}
}

// checkWiring refuses a server whose kind table and tools disagree, or whose rows start a job kind with no executor. A
// tool the config turns off counts as present.
func checkWiring(srv *mcp.Server, execs map[change.EffectKind]outbox.Executor, off []string) error {
	problems := wiringProblems(append(srv.ToolNames(), off...), srv.SchemaNames(), execs)
	if len(problems) > 0 {
		return fmt.Errorf("the server wiring is wrong: %w", errors.Join(problems...))
	}
	return nil
}

// wiringProblems is every disagreement between the kind table and the tools, renderers, schemas and skills, and every
// row job kind without an executor.
func wiringProblems(tools, schemas []string, execs map[change.EffectKind]outbox.Executor) []error {
	problems := workflow.CheckKinds(workflow.Registry{Tools: tools, Renderers: taskinput.Names(), Schemas: schemas, Skills: skills.All()})
	for _, k := range change.RowJobKinds() {
		if execs[k] == nil {
			problems = append(problems, fmt.Errorf("job kind %s has no executor", k))
		}
	}
	return problems
}
