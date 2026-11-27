package bench

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// fixTestFile is the fix's test file. TestAnotherMerchantCannotReadARefund calls Get with the signature the fix adds,
// so the whole file does not build on the head. TestSeveralPartialRefunds builds there and needs a helper, a type and
// an import that only it uses.
const fixTestFile = `//go:build !windows

package refunds

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// clock is a type only the cap test needs.
type clock struct{ now time.Time }

func (c clock) Now() time.Time { return c.now }

const limit = 100

func newService(t *testing.T) *Service {
	t.Helper()
	return New()
}

func seed(s *Service, c clock) {
	s.Add("r1", limit)
	_ = c.Now()
}

// TestSeveralPartialRefunds is red on the head.
func TestSeveralPartialRefunds(t *testing.T) {
	s := newService(t)
	seed(s, clock{now: time.Unix(0, 0)})
	if got, _ := s.Get("r1"); got != limit {
		t.Fatalf("got %d", got)
	}
}

func TestAnotherMerchantCannotReadARefund(t *testing.T) {
	s := newService(t)
	_, err := s.Get("merchant-b", "r1")
	if !errors.Is(err, ErrNotFound) || strings.Contains("x", "y") {
		t.Fatal(err)
	}
}
`

// stdlibImporter is built once and shared: the gc importer reads export data from the build cache, so every call
// after the first reuses the same standard-library packages. The source importer it replaces re-parsed the whole
// standard library on every call, which cost seconds per typeCheck. It is not safe for concurrent use, so the tests
// that call typeCheck stay serial.
var ()

func TestExtractAnUnknownTestNameIsAnErrorThatNamesIt(t *testing.T) {
	_, err := ExtractGoTests([]byte(fixTestFile), []string{"TestSeveralPartialRefunds", "TestNoSuchThing"})
	require.ErrorContains(t, err, "TestNoSuchThing")
	require.NotContains(t, err.Error(), "TestSeveralPartialRefunds")

	_, err = ExtractGoTests([]byte(fixTestFile), []string{"TestNoSuchThing", "TestAlsoMissing"})
	require.ErrorContains(t, err, "TestNoSuchThing")
	require.ErrorContains(t, err, "TestAlsoMissing")
}

func TestExtractRefusesASourceThatDoesNotParse(t *testing.T) {
	_, err := ExtractGoTests([]byte("package x\nfunc {"), []string{"TestA"})
	require.ErrorContains(t, err, "parse")
}

// The head's version of the fix's test file. Another test file of the package (otherHeadTest) uses its helper newHeadSvc,
// so a copy that overwrote the file with only the fix's tests would break the package. The head also has a test of its
// own that the fix's helpers would break.
const headTestFile = `//go:build !windows

package refunds

import (
	"errors"
	"testing"
)

// newHeadSvc is shared with other test files of the package.
func newHeadSvc() *Service { return New() }

// helper of the head that the fix also declares, with another signature.
func newService() *Service { return New() }

func TestGetOfAnUnknownRefundIsNotFound(t *testing.T) {
	if _, err := newService().Get("x"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}
`

func TestSpliceNamesAMissingTestAndRefusesABrokenHead(t *testing.T) {
	_, err := SpliceGoTests([]byte(headTestFile), []byte(fixTestFile), []string{"TestNoSuchThing"})
	require.ErrorContains(t, err, "TestNoSuchThing")
	_, err = SpliceGoTests([]byte("package x\nfunc {"), []byte(fixTestFile), []string{"TestSeveralPartialRefunds"})
	require.ErrorContains(t, err, "head")
}
