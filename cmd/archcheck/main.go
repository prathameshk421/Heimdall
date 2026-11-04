// Command archcheck fails when the import graph breaks the dependency rule (TECH_SPEC 2.2, D121).
// It runs from the module root: `go run ./cmd/archcheck`.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/archcheck"
)

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, "archcheck:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	module, pkgs, err := archcheck.List(ctx, ".")
	if err != nil {
		return err
	}
	violations := archcheck.Check(module, pkgs)
	for _, v := range violations {
		fmt.Fprintf(os.Stderr, "%s imports %s: %s\n", v.Package, v.Import, v.Rule)
	}
	if len(violations) > 0 {
		return fmt.Errorf("%d dependency-rule violation(s)", len(violations))
	}
	fmt.Fprintf(os.Stdout, "archcheck: %d packages, dependency rule holds\n", len(pkgs))
	return nil
}
