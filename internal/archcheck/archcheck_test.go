package archcheck_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/archcheck"
)

const mod = "github.com/prathameshk421/flip_the_switch_hackathon"

func pkg(path string, imports ...string) archcheck.Package {
	return archcheck.Package{ImportPath: path, Imports: imports}
}

func binary(path string, deps ...string) archcheck.Package {
	return archcheck.Package{ImportPath: path, Deps: deps}
}

func TestCheck(t *testing.T) {
	tests := []struct {
		name string
		pkgs []archcheck.Package
		want []archcheck.Violation
	}{
		{
			name: "domain may import the standard library and other domain packages",
			pkgs: []archcheck.Package{
				pkg(mod+"/internal/domain/gate", "fmt", "errors", mod+"/internal/domain/finding"),
				pkg(mod + "/internal/domain/finding"),
			},
		},
		{
			name: "domain must not import ports",
			pkgs: []archcheck.Package{pkg(mod+"/internal/domain/gate", mod+"/internal/ports")},
			want: []archcheck.Violation{{Package: "internal/domain/gate", Import: "internal/ports", Rule: "domain imports only the standard library and internal/domain"}},
		},
		{
			name: "domain must not import a third-party package",
			pkgs: []archcheck.Package{pkg(mod+"/internal/domain/gate", "github.com/jackc/pgx/v5")},
			want: []archcheck.Violation{{Package: "internal/domain/gate", Import: "github.com/jackc/pgx/v5", Rule: "domain imports only the standard library and internal/domain"}},
		},
		{
			name: "app may import domain and ports",
			pkgs: []archcheck.Package{pkg(mod+"/internal/app/review", "context", mod+"/internal/domain/gate", mod+"/internal/ports")},
		},
		{
			name: "app must not import an adapter",
			pkgs: []archcheck.Package{pkg(mod+"/internal/app/review", mod+"/internal/adapters/github")},
			want: []archcheck.Violation{{Package: "internal/app/review", Import: "internal/adapters/github", Rule: "app imports only the standard library, internal/domain and internal/ports"}},
		},
		{
			name: "app must not import transport or security",
			pkgs: []archcheck.Package{pkg(mod+"/internal/app/review", mod+"/internal/transport/mcp", mod+"/internal/security")},
			want: []archcheck.Violation{
				{Package: "internal/app/review", Import: "internal/transport/mcp", Rule: "app imports only the standard library, internal/domain and internal/ports"},
				{Package: "internal/app/review", Import: "internal/security", Rule: "app imports only the standard library, internal/domain and internal/ports"},
			},
		},
		{
			name: "ports may import domain only",
			pkgs: []archcheck.Package{pkg(mod+"/internal/ports", "context", mod+"/internal/domain/change")},
		},
		{
			name: "ports must not import app",
			pkgs: []archcheck.Package{pkg(mod+"/internal/ports", mod+"/internal/app/review")},
			want: []archcheck.Violation{{Package: "internal/ports", Import: "internal/app/review", Rule: "ports imports only the standard library and internal/domain"}},
		},
		{
			name: "an adapter may import ports, domain and third-party packages",
			pkgs: []archcheck.Package{pkg(mod+"/internal/adapters/postgres", "github.com/jackc/pgx/v5", mod+"/internal/ports", mod+"/internal/domain/change")},
		},
		{
			name: "an adapter must not call a use case or transport",
			pkgs: []archcheck.Package{pkg(mod+"/internal/adapters/postgres", mod+"/internal/app/review", mod+"/internal/transport/web")},
			want: []archcheck.Violation{
				{Package: "internal/adapters/postgres", Import: "internal/app/review", Rule: "adapters implement ports and do not import app or transport"},
				{Package: "internal/adapters/postgres", Import: "internal/transport/web", Rule: "adapters implement ports and do not import app or transport"},
			},
		},
		{
			name: "transport calls app and must not import an adapter",
			pkgs: []archcheck.Package{
				pkg(mod+"/internal/transport/web", mod+"/internal/app/review"),
				pkg(mod+"/internal/transport/mcp", mod+"/internal/adapters/github"),
			},
			want: []archcheck.Violation{{Package: "internal/transport/mcp", Import: "internal/adapters/github", Rule: "transport calls app and does not import adapters"}},
		},
		{
			name: "cmd wires everything and may import any layer",
			pkgs: []archcheck.Package{pkg(mod+"/cmd/heimdall-server", mod+"/internal/adapters/postgres", mod+"/internal/app/review", mod+"/internal/transport/web")},
		},
		{
			name: "production code must not import the test kit",
			pkgs: []archcheck.Package{
				pkg(mod+"/internal/app/review", mod+"/internal/testkit"),
				pkg(mod+"/cmd/heimdall-server", mod+"/internal/testkit/fakes"),
			},
			want: []archcheck.Violation{
				{Package: "internal/app/review", Import: "internal/testkit", Rule: "app imports only the standard library, internal/domain and internal/ports"},
				{Package: "internal/app/review", Import: "internal/testkit", Rule: "test code stays out of production (internal/testkit is for tests, cmd/heimdall-testagent and e2e)"},
				{Package: "cmd/heimdall-server", Import: "internal/testkit/fakes", Rule: "test code stays out of production (internal/testkit is for tests, cmd/heimdall-testagent and e2e)"},
			},
		},
		{
			name: "the test agent, the test kit itself and e2e may import the test kit",
			pkgs: []archcheck.Package{
				pkg(mod+"/cmd/heimdall-testagent", mod+"/internal/testkit"),
				pkg(mod+"/internal/testkit/fakes", mod+"/internal/testkit"),
				pkg(mod+"/e2e", mod+"/internal/testkit"),
			},
		},
		{
			name: "a package that merely has a layer name as a prefix is not in that layer",
			pkgs: []archcheck.Package{pkg(mod+"/internal/domainx", mod+"/internal/ports")},
		},
		{
			name: "a production binary must not depend on the test kit even through another package",
			pkgs: []archcheck.Package{
				binary(mod+"/cmd/heimdall-server", "fmt", mod+"/internal/platform/config", mod+"/internal/testkit"),
				binary(mod+"/cmd/heimdall", mod+"/e2e"),
			},
			want: []archcheck.Violation{
				{Package: "cmd/heimdall-server", Import: "internal/testkit", Rule: "a production binary must not depend on test code (D121)"},
				{Package: "cmd/heimdall", Import: "e2e", Rule: "a production binary must not depend on test code (D121)"},
			},
		},
		{
			name: "the test agent binary may depend on the test kit",
			pkgs: []archcheck.Package{binary(mod+"/cmd/heimdall-testagent", mod+"/internal/testkit")},
		},
		{
			name: "a non-binary package with test-kit dependencies is judged by its direct imports only",
			pkgs: []archcheck.Package{binary(mod+"/internal/testkit/fakes", mod+"/internal/testkit")},
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
