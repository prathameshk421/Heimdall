package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"time"
)

type packCommandRuntime interface {
	Run(context.Context, string, string, io.Reader, io.Writer) error
}

func runPack(args []string, in io.Reader, out io.Writer) int {
	return runPackWith(args, in, out, localPackRuntime{})
}

func runPackWith(args []string, in io.Reader, out io.Writer, runtime packCommandRuntime) int {
	if isPackHelp(args) {
		if err := writePackHelp(out); err != nil {
			return 1
		}
		return 0
	}
	command := args[0]
	if command != "refresh" && command != "review" {
		_, _ = fmt.Fprintf(out, "unknown pack command %q\n", command)
		return 2
	}
	profile, ok := parsePackFlags(command, args[1:], out)
	if !ok {
		return 2
	}
	if runtime == nil {
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := runtime.Run(ctx, command, profile, in, out); err != nil {
		if errors.Is(err, errPackNeedsApproval) {
			_, _ = fmt.Fprintln(out, err)
			return 2
		}
		_, _ = fmt.Fprintf(out, "heimdall pack %s: %v\n", command, err)
		return 1
	}
	return 0
}

func isPackHelp(args []string) bool {
	return len(args) == 0 || (len(args) == 1 && (args[0] == "--help" || args[0] == "-h"))
}

// parsePackFlags returns the --profile value; it writes the usage line and returns false on invalid flags.
func parsePackFlags(command string, args []string, out io.Writer) (string, bool) {
	flags := flag.NewFlagSet("heimdall pack "+command, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	profile := flags.String("profile", ".heimdall/pack.toml", "local pack profile")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		_, _ = fmt.Fprintln(out, "usage: heimdall pack "+command+" [--profile path]")
		return "", false
	}
	return *profile, true
}

func writePackHelp(out io.Writer) error {
	for _, line := range []string{
		"heimdall pack refresh [--profile path]  collect local sources, redact twice, distill, and save a private review draft",
		"heimdall pack review [--profile path]   inspect the redacted draft, approve it, age-encrypt it, and upload encrypted ciphertext",
		"Pack profile fields name secret files only; raw source text is redacted before the extraction model receives it.",
	} {
		if _, err := fmt.Fprintln(out, line); err != nil {
			return err
		}
	}
	return nil
}
