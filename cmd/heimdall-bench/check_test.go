package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/bench"
)

// The fix's test file of the synthetic case: two tests that the fix adds. The first builds on the head and is red
// there. The second calls Get with the merchant, a signature the fix adds, so it does not build on the head.
const fixTests = `package refunds

import (
	"errors"
	"testing"
)

func newService(t *testing.T) *Service {
	t.Helper()
	return New(100)
}

func TestSeveralPartialRefundsCannotExceedThePayment(t *testing.T) {
	s := newService(t)
	if err := s.Refund("r1", 60); err != nil {
		t.Fatal(err)
	}
	if err := s.Refund("r2", 60); !errors.Is(err, ErrExceeds) {
		t.Fatalf("second refund of 60 on a payment of 100 err = %v, want ErrExceeds", err)
	}
}

func TestAnotherMerchantCannotReadARefund(t *testing.T) {
	s := newService(t)
	_ = s.Refund("r1", 10)
	if _, err := s.Get("other", "r1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}
`

const headService = `package refunds

import "errors"

var (
	ErrExceeds  = errors.New("exceeds the payment")
	ErrNotFound = errors.New("not found")
)

type Service struct {
	paid, refunded int
	byID           map[string]int
}

func New(paid int) *Service { return &Service{paid: paid, byID: map[string]int{}} }

func (s *Service) Refund(id string, amount int) error {
	if amount > s.paid {
		return ErrExceeds
	}
	s.refunded += amount
	s.byID[id] = amount
	return nil
}

func (s *Service) Get(id string) (int, error) {
	v, ok := s.byID[id]
	if !ok {
		return 0, ErrNotFound
	}
	return v, nil
}
`

const fixedService = `package refunds

import "errors"

var (
	ErrExceeds  = errors.New("exceeds the payment")
	ErrNotFound = errors.New("not found")
)

type Service struct {
	paid, refunded int
	byID           map[string]int
	owner          map[string]string
}

func New(paid int) *Service {
	return &Service{paid: paid, byID: map[string]int{}, owner: map[string]string{}}
}

func (s *Service) Refund(id string, amount int) error {
	if amount > s.paid-s.refunded {
		return ErrExceeds
	}
	s.refunded += amount
	s.byID[id] = amount
	s.owner[id] = "m1"
	return nil
}

func (s *Service) Get(merchant, id string) (int, error) {
	v, ok := s.byID[id]
	if !ok || s.owner[id] != merchant {
		return 0, ErrNotFound
	}
	return v, nil
}
`

// gitIn runs git in dir with a fixed identity and no hooks or signing, and returns its trimmed output.
func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"-c", "user.name=bench", "-c", "user.email=bench@example.com", "-c", "commit.gpgsign=false",
		"-c", "core.hooksPath=/dev/null", "-C", dir}, args...)
	out, err := exec.CommandContext(t.Context(), "git", full...).CombinedOutput() // #nosec G204 -- the test names git and its own arguments
	require.NoError(t, err, string(out))
	return strings.TrimSpace(string(out))
}

func writeTestFile(t *testing.T, root, rel, content string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o750))
	require.NoError(t, os.WriteFile(full, []byte(content), 0o600))
}

// syntheticRefunds builds three commits like fixture-refunds: a base, a head that brings in the bug, and a fix that
// repairs it, changes Get to take the merchant and adds the two tests of fixTests. It returns a repo over a bare copy
// and the case, with no tests named and no truth yet.
func syntheticRefunds(t *testing.T) (*repo, bench.Case) {
	t.Helper()
	src := t.TempDir()
	gitIn(t, src, "init", "--quiet", "--initial-branch=main")
	writeTestFile(t, src, "go.mod", "module example.com/pay\n\ngo 1.22\n")
	writeTestFile(t, src, "README.md", "pay\n")
	gitIn(t, src, "add", "-A")
	gitIn(t, src, "commit", "--quiet", "-m", "base")
	base := gitIn(t, src, "rev-parse", "HEAD")

	writeTestFile(t, src, "refunds/service.go", headService)
	writeTestFile(t, src, "refunds/service_test.go", "package refunds\n\nimport \"testing\"\n\nfunc TestGetOfAnUnknownRefundIsNotFound(t *testing.T) {\n\tif _, err := New(1).Get(\"x\"); err != ErrNotFound {\n\t\tt.Fatal(err)\n\t}\n}\n")
	gitIn(t, src, "add", "-A")
	gitIn(t, src, "commit", "--quiet", "-m", "head")
	head := gitIn(t, src, "rev-parse", "HEAD")

	writeTestFile(t, src, "refunds/service.go", fixedService)
	writeTestFile(t, src, "refunds/service_test.go", fixTests)
	gitIn(t, src, "add", "-A")
	gitIn(t, src, "commit", "--quiet", "-m", "fix")
	fix := gitIn(t, src, "rev-parse", "HEAD")

	bare := filepath.Join(t.TempDir(), "cache.git")
	gitIn(t, src, "clone", "--quiet", "--bare", src, bare)
	rp := &repo{r: execRunner{}, gitDir: bare, work: t.TempDir(), cache: t.TempDir()}
	c := bench.Case{ID: "synthetic-refunds", Repo: "https://github.com/example/pay", Language: "go", Kind: bench.KindBug,
		Change: bench.Commit{SHA: head, Parent: base}, Fix: &bench.Commit{SHA: fix, Parent: head},
		TestCommand: "go test ./...", Timeout: 2 * time.Minute}
	return rp, c
}

const redTest = "refunds:TestSeveralPartialRefundsCannotExceedThePayment"

// TestValidateTakesOnlyTheNamedTestsOfAFixThatDoesNotBuildOnTheHead is the P2.1g case: the fix's test file does not
// build on the head, but one named test of it does and is red there. Naming it makes the case valid.
func TestValidateTakesOnlyTheNamedTestsOfAFixThatDoesNotBuildOnTheHead(t *testing.T) {
	rp, c := syntheticRefunds(t)
	ctx := context.Background()

	// Without a tests list the targets are every test the fix adds lines to, so the whole set must build on the head.
	// It does not: a build error is not a red test.
	whole := checkBug(ctx, rp, c)
	require.Equal(t, bench.OutcomeInvalid, whole.Outcome, whole.Reason)
	require.Contains(t, whole.Reason, "do not build before the fix")
	require.NotEmpty(t, whole.Truth)

	c.Truth = whole.Truth
	c.Tests = []string{redTest}
	got := checkBug(ctx, rp, c)
	require.Equal(t, bench.OutcomeValid, got.Outcome, got.Reason)
	require.Equal(t, []string{redTest}, got.Targets)
	require.Equal(t, bench.ResultFail, got.Before.Tests[redTest], "the cap test is red on the head")
	require.Equal(t, bench.ResultPass, got.After.Tests[redTest])
	require.False(t, got.Before.BuildFailed, got.Before.BuildOutput)
	require.NotContains(t, got.Before.Tests, "refunds:TestAnotherMerchantCannotReadARefund", "the test that does not build is not copied")
}

func TestValidateNamesATestTheFixDoesNotDeclare(t *testing.T) {
	rp, c := syntheticRefunds(t)
	c.Tests = []string{redTest, "refunds:TestNoSuchThing"}
	got := checkBug(context.Background(), rp, c)
	require.Equal(t, bench.OutcomeInvalid, got.Outcome, got.Reason)
	require.Contains(t, got.Reason, "TestNoSuchThing")
	require.NotContains(t, got.Reason, "TestSeveralPartial")

	c.Tests = []string{"other:" + "TestSeveralPartialRefundsCannotExceedThePayment"}
	got = checkBug(context.Background(), rp, c)
	require.Equal(t, bench.OutcomeInvalid, got.Outcome, got.Reason)
	require.Contains(t, got.Reason, "other", "a directory the fix changes no test file in")
}

// TestValidateStillRefusesATargetThatDoesNotBuildOnTheHead: naming the test that calls the new API does not make the
// case valid. Only the rule "a build error is not a red test" decides.
func TestValidateStillRefusesATargetThatDoesNotBuildOnTheHead(t *testing.T) {
	rp, c := syntheticRefunds(t)
	c.Tests = []string{redTest, "refunds:TestAnotherMerchantCannotReadARefund"}
	got := checkBug(context.Background(), rp, c)
	require.Equal(t, bench.OutcomeInvalid, got.Outcome, got.Reason)
	require.Contains(t, got.Reason, "build")
}

func TestGoTestCopiesKeepOnlyTheTargetsOfEachChangedFile(t *testing.T) {
	files := []bench.FileDiff{
		{Path: "a/a_test.go", Status: bench.FileModified},
		{Path: "a/b_test.go", Status: bench.FileAdded},
		{Path: "a/gone_test.go", Status: bench.FileDeleted},
		{Path: "a/a.go", Status: bench.FileModified},
		{Path: "c/c_test.go", Status: bench.FileModified},
	}
	src := map[string]string{
		"a/a_test.go": "package a\n\nimport \"testing\"\n\nfunc TestOne(t *testing.T) {}\n\nfunc TestTwo(t *testing.T) { undefinedOnHead() }\n",
		"a/b_test.go": "package a\n\nimport \"testing\"\n\nfunc TestThree(t *testing.T) {}\n",
		"c/c_test.go": "package c\n\nimport \"testing\"\n\nfunc TestFour(t *testing.T) {}\n",
	}
	show := func(p string) ([]byte, error) { return []byte(src[p]), nil }
	noTree := func(string) ([]byte, error) { return nil, nil }
	targets := []target{{dir: "a", names: []string{"TestOne", "TestThree"}}, {dir: "c", names: []string{"TestFour"}}}
	got, err := goTestCopies(files, targets, show, noTree)
	require.NoError(t, err)
	var paths []string
	for _, cp := range got {
		paths = append(paths, cp.path)
		require.NotContains(t, string(cp.content), "undefinedOnHead")
	}
	require.Equal(t, []string{"a/a_test.go", "a/b_test.go", "c/c_test.go"}, paths)
	require.Contains(t, string(got[0].content), "func TestOne")
	require.NotContains(t, string(got[0].content), "func TestTwo")

	got, err = goTestCopies(files, []target{{dir: "a", names: []string{"TestThree"}}}, show, noTree)
	require.NoError(t, err)
	require.Len(t, got, 1, "a changed file that declares no target is not copied")
	require.Equal(t, "a/b_test.go", got[0].path)
}

func TestGoTestCopiesNameATargetNoChangedFileDeclares(t *testing.T) {
	files := []bench.FileDiff{{Path: "a/a_test.go", Status: bench.FileModified}}
	show := func(string) ([]byte, error) {
		return []byte("package a\n\nimport \"testing\"\n\nfunc TestOne(t *testing.T) {}\n"), nil
	}
	_, err := goTestCopies(files, []target{{dir: "a", names: []string{"TestOne", "TestMissing"}}}, show, show)
	require.ErrorIs(t, err, errNoSuchTest)
	require.ErrorContains(t, err, "TestMissing")
	require.NotContains(t, err.Error(), "TestOne")
}

func TestManifestTargetsGroupByDirectory(t *testing.T) {
	got, err := manifestTargets([]string{"a:TestOne", "b/c:TestTwo", "a:TestThree", ".:TestRoot"})
	require.NoError(t, err)
	require.Equal(t, []target{{dir: "a", names: []string{"TestOne", "TestThree"}}, {dir: "b/c", names: []string{"TestTwo"}},
		{dir: ".", names: []string{"TestRoot"}}}, got)
	_, err = manifestTargets([]string{"TestOne"})
	require.ErrorContains(t, err, "TestOne")
}

// TestGoTestCopiesLayTheTargetsOntoTheTreesOwnFile: the tree's version of a changed file keeps the helpers that its
// other test files use, and a file the tree lacks is copied from the fix alone.
func TestGoTestCopiesLayTheTargetsOntoTheTreesOwnFile(t *testing.T) {
	files := []bench.FileDiff{{Path: "a/a_test.go", Status: bench.FileModified}, {Path: "a/new_test.go", Status: bench.FileAdded}}
	fix := map[string]string{
		"a/a_test.go":   "package a\n\nimport \"testing\"\n\nfunc TestFix(t *testing.T) { _ = shared() }\n",
		"a/new_test.go": "package a\n\nimport \"testing\"\n\nfunc TestNew(t *testing.T) {}\n",
	}
	tree := map[string]string{"a/a_test.go": "package a\n\nfunc shared() int { return 1 }\n"}
	fixSrc := func(p string) ([]byte, error) { return []byte(fix[p]), nil }
	treeSrc := func(p string) ([]byte, error) {
		if s, ok := tree[p]; ok {
			return []byte(s), nil
		}
		return nil, nil
	}
	got, err := goTestCopies(files, []target{{dir: "a", names: []string{"TestFix", "TestNew"}}}, fixSrc, treeSrc)
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Contains(t, string(got[0].content), "func shared()", "the tree's helper stays")
	require.Contains(t, string(got[0].content), "func TestFix")
	require.Contains(t, string(got[1].content), "func TestNew", "a file the tree lacks comes from the fix")
}
