package archcheck

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// Decode reads the stream of JSON objects that `go list -json` writes.
func Decode(r io.Reader) ([]Package, error) {
	dec := json.NewDecoder(r)
	var pkgs []Package
	for {
		var p struct {
			Package
			// go list exits 0 for a package it could not load and reports the problem here, so a
			// checker that ignored these fields would pass a module it never saw.
			Error      *struct{ Err string }
			DepsErrors []struct{ Err string }
		}
		err := dec.Decode(&p)
		if errors.Is(err, io.EOF) {
			return pkgs, nil
		}
		if err != nil {
			return nil, fmt.Errorf("decode go list output: %w", err)
		}
		if p.Error != nil {
			return nil, fmt.Errorf("package %s did not load: %s", p.ImportPath, p.Error.Err)
		}
		if len(p.DepsErrors) > 0 {
			return nil, fmt.Errorf("package %s has a dependency that did not load: %s", p.ImportPath, p.DepsErrors[0].Err)
		}
		pkgs = append(pkgs, p.Package)
	}
}

// List runs `go list` in dir and returns the module path and the packages of the module.
func List(ctx context.Context, dir string) (module string, pkgs []Package, err error) {
	modOut, err := goList(ctx, dir, "-m")
	if err != nil {
		return "", nil, err
	}
	module = strings.TrimSpace(string(modOut))
	listOut, err := goList(ctx, dir, "-json", "./...")
	if err != nil {
		return "", nil, err
	}
	pkgs, err = Decode(bytes.NewReader(listOut))
	if err != nil {
		return "", nil, err
	}
	return module, pkgs, nil
}

func goList(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "go", append([]string{"list"}, args...)...) // #nosec G204 -- fixed subcommand, caller supplies only flags from this package
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("go list %s in %s: %w: %s", strings.Join(args, " "), dir, err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}
