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

// TestValidateStillRefusesATargetThatDoesNotBuildOnTheHead: naming the test that calls the new API does not make the
// case valid. Only the rule "a build error is not a red test" decides.
func TestValidateStillRefusesATargetThatDoesNotBuildOnTheHead(t *testing.T) {
	rp, c := syntheticRefunds(t)
	c.Tests = []string{redTest, "refunds:TestAnotherMerchantCannotReadARefund"}
	got := checkBug(context.Background(), rp, c)
	require.Equal(t, bench.OutcomeInvalid, got.Outcome, got.Reason)
	require.Contains(t, got.Reason, "build")
}
