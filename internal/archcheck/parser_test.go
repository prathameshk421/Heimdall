package archcheck_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/archcheck"
)

const parserRule = "server code names no language: go/parser, go/ast and go/token stay out of internal/app, internal/transport and cmd/heimdall-server (C4, C5)"

const staleRule = "a listed parser offender no longer imports this: remove it from parserOffenders"

func TestCheckRefusesGoParserImportsInServerCode(t *testing.T) {
	tests := []struct {
		name string
		pkgs []archcheck.Package
		want []archcheck.Violation
	}{
		{
			name: "an adapter may import go/ast",
			pkgs: []archcheck.Package{pkg(mod+"/internal/adapters/lensfs", "go/ast", "go/token")},
		},
		{
			name: "a domain package may import go/token",
			pkgs: []archcheck.Package{pkg(mod+"/internal/domain/sarif", "go/token")},
		},
		{
			name: "the lint self-test outside server code may import go/parser",
			pkgs: []archcheck.Package{pkg(mod+"/internal/lintselftest", "go/parser")},
		},
		{
			name: "an app package must not import go/parser",
			pkgs: []archcheck.Package{pkg(mod+"/internal/app/review", "context", "go/parser")},
			want: []archcheck.Violation{{Package: "internal/app/review", Import: "go/parser", Rule: parserRule}},
		},
		{
			name: "a transport package must not import go/token",
			pkgs: []archcheck.Package{pkg(mod+"/internal/transport/mcp", "go/token")},
			want: []archcheck.Violation{{Package: "internal/transport/mcp", Import: "go/token", Rule: parserRule}},
		},
		{
			name: "the server binary must not import go/ast",
			pkgs: []archcheck.Package{pkg(mod+"/cmd/heimdall-server", "go/ast")},
			want: []archcheck.Violation{{Package: "cmd/heimdall-server", Import: "go/ast", Rule: parserRule}},
		},
		{
			name: "a package that only shares a prefix with a banned directory is not in it",
			pkgs: []archcheck.Package{pkg(mod+"/cmd/heimdall-server-tools", "go/ast")},
		},
		{
			name: "a listed offender keeps exactly the imports it has today",
			pkgs: []archcheck.Package{pkg(mod+"/internal/app/effects", "go/parser", "go/token")},
		},
		{
			name: "a listed offender may not add a parser import it did not have",
			pkgs: []archcheck.Package{pkg(mod+"/internal/app/effects", "go/ast", "go/parser", "go/token")},
			want: []archcheck.Violation{{Package: "internal/app/effects", Import: "go/ast", Rule: parserRule}},
		},
		{
			name: "an offender that dropped an import must leave the list",
			pkgs: []archcheck.Package{pkg(mod+"/internal/app/effects", "go/parser")},
			want: []archcheck.Violation{{Package: "internal/app/effects", Import: "go/token", Rule: staleRule}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := archcheck.Check(mod, tc.pkgs)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Fatalf("violations mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
