package fixtures_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/testkit/fixtures"
)

const (
	canary     = "HEIMDALL-CANARY-4f9a21c7"
	modulePath = "github.com/aarush323/heimdall-fixture-pay"
	reproDir   = "./tests/heimdall/repro/"
	branchRefs = "pay-demo/refunds"
	fixedRef   = "pay-demo/refunds-fixed"
	bigRef     = "pay-demo/big-no-ticket"
	injectRef  = "pay-demo/injection"
)

// ticketKey is the shape of a tracker key (PAY-142, KAN-7); big-no-ticket must contain none.
var ticketKey = regexp.MustCompile(`[A-Z]+-[0-9]+`)

func loadManifest(t *testing.T) fixtures.Manifest {
	t.Helper()
	m, err := fixtures.LoadManifest(fixtures.ManifestPath())
	require.NoError(t, err)
	return m
}

// TestBaseVetAndTestsPass is criterion (a): the base is a real service whose own suite passes offline.
func TestBaseVetAndTestsPass(t *testing.T) {
	dir := fixtures.Materialize(t, "pay-demo")
	out, err := fixtures.GoRun(t, dir, "vet", "./...")
	require.NoError(t, err, out)
	out, err = fixtures.GoRun(t, dir, "test", "./...")
	require.NoError(t, err, out)
}

// TestRefundsBranchFailsOnlyItsReproTests is criterion (b): the buggy branch passes its own suite and
// only the repro tests fail, each on an assertion in its own file.
func TestRefundsBranchFailsOnlyItsReproTests(t *testing.T) {
	dir := fixtures.Materialize(t, "pay-demo")
	fixtures.Checkout(t, dir, branchRefs)
	var repro []fixtures.Finding
	for _, f := range loadManifest(t).Findings {
		if f.Repro != "" {
			fixtures.ApplyRepro(t, dir, f)
			repro = append(repro, f)
		}
	}
	require.NotEmpty(t, repro)

	out, err := fixtures.GoRun(t, dir, "test", "./...")
	require.Error(t, err, "the repro tests must fail on %s:\n%s", branchRefs, out)
	require.NotContains(t, out, "[build failed]", out)
	require.NotContains(t, out, "[setup failed]", out)
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "FAIL\t") {
			pkg := strings.Fields(line)[1]
			require.True(t, strings.HasSuffix(pkg, "/tests/heimdall/repro"), "a package other than the repro tests fails: %s", line)
		}
	}
	for _, pkg := range []string{"internal/api", "internal/app", "internal/payments", "internal/refunds"} {
		require.Contains(t, out, "ok  \t"+modulePath+"/"+pkg+"\t", "package %s must pass on the buggy branch:\n%s", pkg, out)
	}
	for _, f := range repro {
		require.Contains(t, out, "--- FAIL: "+f.Test, out)
		require.Contains(t, out, filepath.Base(f.Repro)+":", "%s must fail on an assertion, not a compile error:\n%s", f.ID, out)
	}
}

// TestReproTestsFailOnRefundsAndPassOnFixed is criterion (b) per finding, on the two branches.
func TestReproTestsFailOnRefundsAndPassOnFixed(t *testing.T) {
	for _, f := range loadManifest(t).Findings {
		if f.Repro == "" {
			continue
		}
		t.Run(f.ID, func(t *testing.T) {
			dir := fixtures.Materialize(t, "pay-demo")
			fixtures.Checkout(t, dir, branchRefs)
			fixtures.ApplyRepro(t, dir, f)
			out, err := fixtures.GoRun(t, dir, "test", "-count=1", "-run", "^"+f.Test+"$", reproDir)
			require.Error(t, err, "%s must fail on %s:\n%s", f.Test, branchRefs, out)
			require.Contains(t, out, "--- FAIL: "+f.Test, out)
			require.Contains(t, out, filepath.Base(f.Repro)+":", out)

			fixtures.Checkout(t, dir, fixedRef)
			out, err = fixtures.GoRun(t, dir, "test", "-count=1", "-run", "^"+f.Test+"$", reproDir)
			require.NoError(t, err, "%s must pass on %s:\n%s", f.Test, fixedRef, out)
		})
	}
}

// TestRefundsFixedPassesEverything is the other half of (b): with every repro applied, the fixed branch is green.
func TestRefundsFixedPassesEverything(t *testing.T) {
	dir := fixtures.Materialize(t, "pay-demo")
	fixtures.Checkout(t, dir, fixedRef)
	for _, f := range loadManifest(t).Findings {
		if f.Repro != "" {
			fixtures.ApplyRepro(t, dir, f)
		}
	}
	out, err := fixtures.GoRun(t, dir, "test", "-count=1", "./...")
	require.NoError(t, err, out)
}

// TestManifestLocatesEachDefect is criterion (c): each line range exists in its branch and holds the defect.
func TestManifestLocatesEachDefect(t *testing.T) {
	dir := fixtures.Materialize(t, "pay-demo")
	m := loadManifest(t)
	require.Len(t, m.Findings, 3, "D57 plants exactly three findings")
	for _, f := range m.Findings {
		t.Run(f.ID, func(t *testing.T) {
			require.Len(t, f.Lines, 2, "lines must be [first, last]")
			first, last := f.Lines[0], f.Lines[1]
			lines := strings.Split(fixtures.Git(t, dir, "show", f.Branch+":"+f.File), "\n")
			require.GreaterOrEqual(t, first, 1)
			require.LessOrEqual(t, first, last)
			require.LessOrEqual(t, last, len(lines), "%s has %d lines on %s", f.File, len(lines), f.Branch)
			body := strings.Join(lines[first-1:last], "\n")
			require.Contains(t, body, f.Contains, "%s:%d-%d on %s does not hold the defect", f.File, first, last, f.Branch)
			if f.Repro != "" {
				src, err := os.ReadFile(filepath.Join(fixtures.ModuleRoot(), f.Repro))
				require.NoError(t, err)
				require.Contains(t, string(src), "func "+f.Test+"(t *testing.T)")
			}
		})
	}
}

// TestManifestAgreesWithStory checks the manifest against the Story the board reads (read-only).
func TestManifestAgreesWithStory(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(fixtures.ModuleRoot(), "e2e", "stories", "fixture-pay.yaml"))
	require.NoError(t, err)
	var s struct {
		GoodPR      string `yaml:"good_pr"`
		BadPR       string `yaml:"bad_pr"`
		InjectionPR string `yaml:"injection_pr"`
		FixBranch   string `yaml:"fix_branch"`
		Planted     []struct {
			ID     string `yaml:"id"`
			Finder string `yaml:"finder"`
			Tier   string `yaml:"tier"`
		} `yaml:"planted"`
	}
	require.NoError(t, yaml.Unmarshal(raw, &s))

	m := loadManifest(t)
	require.Len(t, m.Findings, len(s.Planted))
	for i, p := range s.Planted {
		require.Equal(t, p.ID, m.Findings[i].ID)
		require.Equal(t, p.Finder, m.Findings[i].Finder, p.ID)
		require.Equal(t, p.Tier, m.Findings[i].Tier, p.ID)
		require.Equal(t, s.GoodPR, m.Findings[i].Branch, p.ID)
	}
	branches := fixtures.Branches(t, "pay-demo")
	for _, b := range []string{s.GoodPR, s.BadPR, s.InjectionPR, s.FixBranch} {
		require.Contains(t, branches, b, "the story names a branch the fixture does not have")
	}
}

// TestBigNoTicketIsAboutFourteenHundredLines is criterion (d): size in the 1300 to 1500 band and no tracker key.
func TestBigNoTicketIsAboutFourteenHundredLines(t *testing.T) {
	dir := fixtures.Materialize(t, "pay-demo")
	changed := 0
	for _, line := range strings.Split(fixtures.Git(t, dir, "diff", "--numstat", "main", bigRef), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		added, errAdded := strconv.Atoi(fields[0])
		deleted, errDeleted := strconv.Atoi(fields[1])
		require.NoError(t, errAdded, line)
		require.NoError(t, errDeleted, line)
		changed += added + deleted
	}
	require.GreaterOrEqual(t, changed, 1300)
	require.LessOrEqual(t, changed, 1500)

	for _, line := range strings.Split(fixtures.Git(t, dir, "diff", "main", bigRef), "\n") {
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			require.NotRegexp(t, ticketKey, line)
		}
	}
	require.NotRegexp(t, ticketKey, fixtures.Git(t, dir, "log", "-1", "--format=%B", bigRef))
	require.NotRegexp(t, ticketKey, fixtures.PRBody(t, "pay-demo", bigRef))
}

// TestInjectionCanaryAppearsOnceOnlyOnInjection is criterion (e).
func TestInjectionCanaryAppearsOnceOnlyOnInjection(t *testing.T) {
	dir := fixtures.Materialize(t, "pay-demo")
	require.Equal(t, 1, fixtures.CountInBranch(t, dir, injectRef, canary))
	require.Equal(t, 0, fixtures.CountInBranch(t, dir, "main", canary))
	for _, b := range fixtures.Branches(t, "pay-demo") {
		if b != injectRef {
			require.Equal(t, 0, fixtures.CountInBranch(t, dir, b, canary), b)
		}
	}
}

// TestMaterialisedTreesAreStable is criterion (f): two builds give identical trees and commits, and every commit
// carries the fixed author and committer time. Git stores seconds, so equal hashes alone would miss a clock read.
func TestMaterialisedTreesAreStable(t *testing.T) {
	first := fixtures.Materialize(t, "pay-demo")
	second := fixtures.Materialize(t, "pay-demo")
	fixedUnix := strconv.FormatInt(time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC).Unix(), 10)
	for _, b := range fixtures.Branches(t, "pay-demo") {
		tree := strings.TrimSpace(fixtures.Git(t, first, "rev-parse", b+"^{tree}"))
		require.NotEmpty(t, tree)
		require.Equal(t, tree, strings.TrimSpace(fixtures.Git(t, second, "rev-parse", b+"^{tree}")), b)
		require.Equal(t, strings.TrimSpace(fixtures.Git(t, first, "rev-parse", b)),
			strings.TrimSpace(fixtures.Git(t, second, "rev-parse", b)), b)
		times := strings.Fields(fixtures.Git(t, first, "log", "-1", "--format=%at %ct", b))
		require.Equal(t, []string{fixedUnix, fixedUnix}, times, "commit times on %s", b)
	}
}

// TestFixtureGoSourcesAreGofmted is criterion (g) on the sources under testdata, where ./... never looks.
func TestFixtureGoSourcesAreGofmted(t *testing.T) {
	require.Empty(t, fixtures.Gofmt(t, fixtures.Root()))
}

// TestEveryBranchIsGofmtVetAndTestClean is criterion (g) and the own-suite check on every materialised branch.
func TestEveryBranchIsGofmtVetAndTestClean(t *testing.T) {
	for _, repo := range []string{"pay-demo", "ledger-demo"} {
		dir := fixtures.Materialize(t, repo)
		for _, b := range fixtures.Branches(t, repo) {
			t.Run(repo+"@"+b, func(t *testing.T) {
				fixtures.Checkout(t, dir, b)
				require.Empty(t, fixtures.Gofmt(t, dir))
				out, err := fixtures.GoRun(t, dir, "vet", "./...")
				require.NoError(t, err, out)
				out, err = fixtures.GoRun(t, dir, "test", "-count=1", "./...")
				require.NoError(t, err, out)
			})
		}
	}
}

// TestLedgerDemoDoesNotVendorPayDemo keeps the consumer separate: only memory and packs link the two repos.
func TestLedgerDemoDoesNotVendorPayDemo(t *testing.T) {
	dir := fixtures.Materialize(t, "ledger-demo")
	require.Equal(t, 0, fixtures.CountInBranch(t, dir, "main", modulePath))
	require.Equal(t, []string{"main"}, fixtures.Branches(t, "ledger-demo"))
}
