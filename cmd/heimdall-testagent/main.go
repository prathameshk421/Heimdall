// Command heimdall-testagent.
//
// TEST ONLY. The scripted agent that speaks the real agent protocol and returns canned submissions (T38). It is the
// deterministic "brain" of the end-to-end tests (D118): the MCP transport, the token, the task envelope and the
// server's checks are real, and only the choice of output is canned. It never calls a model.
//
// Excluded from release builds and images (D121).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/config"
)

const (
	// exitOK is the status of a scenario that ran as scripted, including one the server refused: the refusal is the
	// outcome the scenario exists to produce.
	exitOK = 0
	// exitFailure is the status of an agent that could not reach or talk to the server.
	exitFailure = 1
	// exitUsage is the status of a bad command line, as in the other commands here.
	exitUsage = 2
	// scenarioVariable names the scenario when -scenario is not passed.
	scenarioVariable = "HEIMDALL_TESTAGENT_SCENARIO"
)

// run plays the scenario named by -scenario or HEIMDALL_TESTAGENT_SCENARIO against the MCP endpoint at -url. The token
// is read from -token-file before anything is sent. It returns exitOK when the scenario ran as scripted, exitFailure
// when the agent could not reach or talk to the server, and exitUsage for a bad command line.
func run(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("heimdall-testagent", flag.ContinueOnError)
	fs.SetOutput(stderr)
	rawURL := fs.String("url", "", "the MCP endpoint, such as http://127.0.0.1:8080/mcp")
	tokenFile := fs.String("token-file", "", "the file holding this agent's token")
	flagName := fs.String("scenario", "", "the canned scenario to play (default: "+scenarioVariable+")")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() > 0 {
		complain(stderr, "heimdall-testagent: unexpected argument %q\n", fs.Arg(0))
		return exitUsage
	}
	name := chosenName(*flagName, getenv)
	sc, ok := scenarioFor(name)
	if !ok {
		complain(stderr, "heimdall-testagent: scenario %q is not one of %s\n", name, strings.Join(scenarioNames(), ", "))
		return exitUsage
	}
	token, err := readToken(*tokenFile)
	if err != nil {
		complain(stderr, "heimdall-testagent: %v\n", err)
		return exitFailure
	}
	c, err := newClient(*rawURL, token)
	if err != nil {
		complain(stderr, "heimdall-testagent: %v\n", err)
		return exitFailure
	}
	if err := c.connect(ctx); err != nil {
		complain(stderr, "heimdall-testagent: %s: %v\n", sc.name, err)
		return exitFailure
	}
	if err := sc.run(ctx, c, stdout); err != nil {
		complain(stderr, "heimdall-testagent: %s: %v\n", sc.name, err)
		return exitFailure
	}
	return exitOK
}

// chosenName is the scenario name the run uses: the flag first, the environment second.
func chosenName(flagName string, getenv func(string) string) string {
	if flagName != "" {
		return flagName
	}
	return getenv(scenarioVariable)
}

// readToken reads the agent token from the file at path. The token never comes from an argument or the environment;
// only the path does. Neither the token nor the file's content is put in an error.
func readToken(path string) (string, error) {
	if path == "" {
		return "", errors.New("no token file: pass -token-file")
	}
	raw, err := os.ReadFile(path) // #nosec G304 -- the path is the operator's -token-file argument
	if err != nil {
		return "", fmt.Errorf("read token file %s: %w", path, err)
	}
	token := strings.TrimSpace(string(raw))
	if token == "" {
		return "", fmt.Errorf("token file %s is empty", path)
	}
	return token, nil
}

// complain writes one line to stderr. When stderr itself cannot be written there is nowhere left to report, so the
// write error is dropped on purpose; the exit status still carries the outcome.
func complain(w io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(w, format, args...)
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], config.Getenv, os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}
