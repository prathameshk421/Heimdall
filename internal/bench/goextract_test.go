package bench

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// headCode is the package as it is on the head tree: Get takes one argument.
const headCode = `package refunds

import "errors"

var ErrNotFound = errors.New("not found")

type Service struct{ refunds map[string]int }

func New() *Service { return &Service{refunds: map[string]int{}} }

func (s *Service) Add(id string, amount int) { s.refunds[id] += amount }

func (s *Service) Get(id string) (int, error) {
	v, ok := s.refunds[id]
	if !ok {
		return 0, ErrNotFound
	}
	return v, nil
}
`

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
var (
	stdlibOnce     sync.Once
	stdlibImporter types.Importer
)

// typeCheck compiles the files as one package with the standard library, and returns the type errors.
func typeCheck(t *testing.T, files map[string]string) []error {
	t.Helper()
	stdlibOnce.Do(func() { stdlibImporter = importer.Default() })
	fset := token.NewFileSet()
	var parsed []*ast.File
	for name, src := range files {
		f, err := parser.ParseFile(fset, name, src, parser.ParseComments)
		require.NoError(t, err, name)
		parsed = append(parsed, f)
	}
	var errs []error
	conf := types.Config{Importer: stdlibImporter, Error: func(err error) { errs = append(errs, err) }}
	_, _ = conf.Check("refunds", fset, parsed, nil)
	return errs
}

// TestExtractedTestBuildsOnTheHeadWhereTheWholeFileDoesNot: the fix's file has one test that does not build on the head
// and one that does. The copy keeps only the one that does, with what it needs, and the package builds.
func TestExtractedTestBuildsOnTheHeadWhereTheWholeFileDoesNot(t *testing.T) {
	whole := typeCheck(t, map[string]string{"service.go": headCode, "service_test.go": fixTestFile})
	require.NotEmpty(t, whole, "the whole fix file must not build on the head, or this test proves nothing")
	require.Contains(t, whole[0].Error(), "too many arguments")

	out, err := ExtractGoTests([]byte(fixTestFile), []string{"TestSeveralPartialRefunds"})
	require.NoError(t, err)
	got := string(out)
	require.Empty(t, typeCheck(t, map[string]string{"service.go": headCode, "service_test.go": got}), got)

	for _, want := range []string{"//go:build !windows", "package refunds", "func TestSeveralPartialRefunds", "func newService",
		"func seed", "type clock struct", "func (c clock) Now()", "const limit", "// TestSeveralPartialRefunds is red on the head.",
		"// clock is a type only the cap test needs."} {
		require.Contains(t, got, want)
	}
	for _, gone := range []string{"TestAnotherMerchantCannotReadARefund", `"errors"`, `"strings"`} {
		require.NotContains(t, got, gone, "the copy keeps only the target, its helpers and the imports they use")
	}
	require.Contains(t, got, `"time"`)
	require.Contains(t, got, `"testing"`)
}

// TestExtractKeepsATestThatNeedsAnImportTheOtherTestDoesNot: the other target brings back its own imports.
func TestExtractKeepsAnImportOnlyTheTargetUses(t *testing.T) {
	out, err := ExtractGoTests([]byte(fixTestFile), []string{"TestAnotherMerchantCannotReadARefund"})
	require.NoError(t, err)
	got := string(out)
	require.Contains(t, got, `"errors"`)
	require.Contains(t, got, `"strings"`)
	require.Contains(t, got, "func newService")
	require.NotContains(t, got, `"time"`, "time is used only by the clock type, which this test does not need")
	require.NotContains(t, got, "type clock")
	require.NotContains(t, got, "func seed")
	require.NotContains(t, got, "TestSeveralPartialRefunds")
}

func TestExtractBothTargetsKeepsTheWholeFileContent(t *testing.T) {
	out, err := ExtractGoTests([]byte(fixTestFile), []string{"TestSeveralPartialRefunds", "TestAnotherMerchantCannotReadARefund"})
	require.NoError(t, err)
	for _, want := range []string{"func TestSeveralPartialRefunds", "func TestAnotherMerchantCannotReadARefund", `"errors"`, `"time"`} {
		require.Contains(t, string(out), want)
	}
}

func TestExtractAnUnknownTestNameIsAnErrorThatNamesIt(t *testing.T) {
	_, err := ExtractGoTests([]byte(fixTestFile), []string{"TestSeveralPartialRefunds", "TestNoSuchThing"})
	require.ErrorContains(t, err, "TestNoSuchThing")
	require.NotContains(t, err.Error(), "TestSeveralPartialRefunds")

	_, err = ExtractGoTests([]byte(fixTestFile), []string{"TestNoSuchThing", "TestAlsoMissing"})
	require.ErrorContains(t, err, "TestNoSuchThing")
	require.ErrorContains(t, err, "TestAlsoMissing")
}

// TestExtractANameThatIsNotATopLevelTestIsAnError: a helper, a method and a benchmark are not targets.
func TestExtractANameThatIsNotATopLevelTestIsAnError(t *testing.T) {
	src := "package x\n\nimport \"testing\"\n\nfunc helper() {}\n\ntype s struct{}\n\nfunc (s) TestMethod(t *testing.T) {}\n\nfunc BenchmarkB(b *testing.B) {}\n\nfunc TestReal(t *testing.T) {}\n"
	for _, name := range []string{"helper", "TestMethod", "BenchmarkB", "Testlower"} {
		_, err := ExtractGoTests([]byte(src), []string{name})
		require.ErrorContains(t, err, name)
	}
	_, err := ExtractGoTests([]byte(src), nil)
	require.Error(t, err, "no target is an error, not an empty file")
}

func TestExtractRefusesASourceThatDoesNotParse(t *testing.T) {
	_, err := ExtractGoTests([]byte("package x\nfunc {"), []string{"TestA"})
	require.ErrorContains(t, err, "parse")
}

// TestExtractFollowsHelpersThroughTypesMethodsAndGroups: a chain helper -> helper, a method that a kept type needs to
// satisfy an interface, a const group, a var, and a closure.
func TestExtractFollowsHelpersThroughTypesMethodsAndGroups(t *testing.T) {
	src := `package x

import (
	"fmt"
	"sort"
	"testing"
)

type shape interface{ Area() int }

type square struct{ side int }

func (q square) Area() int { return q.side * q.side }

func (q square) unusedByName() string { return fmt.Sprint(q.side) }

type unused struct{}

const (
	small = iota + 1
	large
)

var table = []shape{square{side: small}}

func first() shape { return second() }

func second() shape { return table[0] }

func other() { sort.Ints(nil) }

func TestKept(t *testing.T) {
	check := func(s shape) bool { return s.Area() > 0 }
	if !check(first()) {
		t.Fatal("area")
	}
}

func TestDropped(t *testing.T) { other() }
`
	out, err := ExtractGoTests([]byte(src), []string{"TestKept"})
	require.NoError(t, err)
	got := string(out)
	for _, want := range []string{"type shape interface", "type square struct", "func (q square) Area()", "func (q square) unusedByName()",
		"small = iota + 1", "large", "var table", "func first()", "func second()", `"fmt"`, `"testing"`} {
		require.Contains(t, got, want)
	}
	for _, gone := range []string{"type unused", "func other()", "TestDropped", `"sort"`} {
		require.NotContains(t, got, gone)
	}
	require.Empty(t, typeCheck(t, map[string]string{"x_test.go": got}), got)
}

// TestExtractKeepsSetUpThatShapesEveryTest: TestMain and init run for every test of the package, so a copy that left
// them out could turn a green test red or the other way round.
func TestExtractKeepsTestMainAndInit(t *testing.T) {
	src := `package x

import (
	"os"
	"testing"
)

var ready bool

func init() { ready = true }

func TestMain(m *testing.M) { os.Exit(m.Run()) }

func TestA(t *testing.T) { _ = ready }

func TestB(t *testing.T) {}
`
	out, err := ExtractGoTests([]byte(src), []string{"TestA"})
	require.NoError(t, err)
	got := string(out)
	for _, want := range []string{"func init()", "func TestMain", "var ready", `"os"`, "func TestA"} {
		require.Contains(t, got, want)
	}
	require.NotContains(t, got, "func TestB")
}

// TestExtractKeepsImportsByTheirPackageName: an aliased import, a versioned path, a blank import and a package whose name
// is not the last path element. A dropped test's imports go; a kept test's imports stay.
func TestExtractKeepsImportsByTheirPackageName(t *testing.T) {
	src := `package x

import (
	"testing"

	yamlv3 "gopkg.in/yaml.v3"
	"gopkg.in/check.v1"
	"github.com/foo/go-cmp"
	"github.com/foo/bar/v2"
	_ "embed"
	"github.com/stretchr/testify/require"
)

func TestAlias(t *testing.T) { _ = yamlv3.Marshal }

func TestVersioned(t *testing.T) { _ = check.Suite; _ = cmp.Diff; _ = bar.Do }

func TestRequire(t *testing.T) { require.True(t, true) }
`
	out, err := ExtractGoTests([]byte(src), []string{"TestAlias"})
	require.NoError(t, err)
	got := string(out)
	require.Contains(t, got, `yamlv3 "gopkg.in/yaml.v3"`)
	require.Contains(t, got, `_ "embed"`, "a blank import runs for its side effect and stays")
	for _, gone := range []string{"gopkg.in/check.v1", "go-cmp", "bar/v2", "testify/require"} {
		require.NotContains(t, got, gone)
	}

	out, err = ExtractGoTests([]byte(src), []string{"TestVersioned"})
	require.NoError(t, err)
	got = string(out)
	for _, want := range []string{"gopkg.in/check.v1", "github.com/foo/go-cmp", "github.com/foo/bar/v2"} {
		require.Contains(t, got, want)
	}
	require.NotContains(t, got, "yaml.v3")
	require.NotContains(t, got, "testify/require")

	out, err = ExtractGoTests([]byte(src), []string{"TestRequire"})
	require.NoError(t, err)
	require.Contains(t, string(out), "testify/require")
}

func TestGoTestNamesListsTopLevelTestsInSourceOrder(t *testing.T) {
	names, err := GoTestNames([]byte("package x\nfunc TestB() {}\nfunc helper() {}\nfunc (s *T) TestM() {}\nfunc TestA() {}\nfunc Testlower() {}\n"))
	require.NoError(t, err)
	require.Equal(t, []string{"TestB", "TestA"}, names)
	_, err = GoTestNames([]byte("package x\nfunc {"))
	require.Error(t, err)
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

const otherHeadTest = `package refunds

import "testing"

func TestUsesAHelperOfTheOtherFile(t *testing.T) { _ = newHeadSvc() }
`

// TestSpliceKeepsWhatTheOtherTestFilesOfTheHeadNeed is the regression of gin-4702 and echo-1799: a test file of the head
// holds helpers that other files of the package use. The copy lays the fix's targets onto the head's file, so the
// package still builds, where the cut-down fix file alone leaves the other file without its helper.
func TestSpliceKeepsWhatTheOtherTestFilesOfTheHeadNeed(t *testing.T) {
	files := func(testFile string) map[string]string {
		return map[string]string{"service.go": headCode, "service_test.go": testFile, "other_test.go": otherHeadTest}
	}
	alone, err := ExtractGoTests([]byte(fixTestFile), []string{"TestSeveralPartialRefunds"})
	require.NoError(t, err)
	broken := typeCheck(t, files(string(alone)))
	require.NotEmpty(t, broken)
	require.Contains(t, broken[0].Error(), "newHeadSvc")

	out, err := SpliceGoTests([]byte(headTestFile), []byte(fixTestFile), []string{"TestSeveralPartialRefunds"})
	require.NoError(t, err)
	got := string(out)
	require.Empty(t, typeCheck(t, files(got)), got)

	require.Contains(t, got, "func newHeadSvc", "the head's helper stays")
	require.Contains(t, got, "// newHeadSvc is shared with other test files of the package.")
	require.Contains(t, got, "func TestSeveralPartialRefunds", "the fix's target is in")
	require.Contains(t, got, "func newService(t *testing.T)", "a helper that both declare is the fix's version")
	require.Equal(t, 1, strings.Count(got, "func newService("))
	require.NotContains(t, got, "TestGetOfAnUnknownRefundIsNotFound", "a head test is not a target and may not build with the fix's helpers")
	require.NotContains(t, got, "TestAnotherMerchantCannotReadARefund", "the fix's other test does not build on the head")
	require.Contains(t, got, "//go:build !windows")
	require.Contains(t, got, `"time"`, "the fix's target needs the clock type, and that needs time")
	require.NotContains(t, got, `"strings"`)
	require.NotContains(t, got, `"errors"`, "errors was used only by the head's own test, which is gone")
}

func TestSpliceImportsAreTheUnionOfWhatTheKeptDeclarationsUse(t *testing.T) {
	head := `package x

import (
	"fmt"
	"sort"
	"testing"
)

func headHelper() string { return fmt.Sprint(1) }

func TestHead(t *testing.T) { sort.Ints(nil) }
`
	fix := `package x

import (
	"strings"
	"testing"
)

func TestFix(t *testing.T) { _ = strings.ToUpper("a"); _ = headHelper() }
`
	out, err := SpliceGoTests([]byte(head), []byte(fix), []string{"TestFix"})
	require.NoError(t, err)
	got := string(out)
	for _, want := range []string{`"fmt"`, `"strings"`, `"testing"`, "func headHelper", "func TestFix"} {
		require.Contains(t, got, want)
	}
	require.NotContains(t, got, `"sort"`)
	require.NotContains(t, got, "TestHead")
	require.Equal(t, 1, strings.Count(got, `"testing"`), "an import both files use is written once")
	require.Empty(t, typeCheck(t, map[string]string{"x_test.go": got}), got)
}

func TestSpliceTakesTheFixesTestMainAndInitInPlaceOfTheHeads(t *testing.T) {
	head := "package x\n\nimport \"testing\"\n\nfunc init() { println(\"head init\") }\n\nfunc TestMain(m *testing.M) { println(\"head main\"); m.Run() }\n\nfunc TestA(t *testing.T) {}\n"
	fix := "package x\n\nimport \"testing\"\n\nfunc init() { println(\"fix init\") }\n\nfunc TestMain(m *testing.M) { println(\"fix main\"); m.Run() }\n\nfunc TestA(t *testing.T) {}\n"
	out, err := SpliceGoTests([]byte(head), []byte(fix), []string{"TestA"})
	require.NoError(t, err)
	require.Contains(t, string(out), "fix init")
	require.Contains(t, string(out), "fix main")
	require.NotContains(t, string(out), "head init")
	require.NotContains(t, string(out), "head main")

	noFixSetup := "package x\n\nimport \"testing\"\n\nfunc TestA(t *testing.T) {}\n"
	out, err = SpliceGoTests([]byte(head), []byte(noFixSetup), []string{"TestA"})
	require.NoError(t, err)
	require.Contains(t, string(out), "head init", "the head's set-up stays when the fix has none")
	require.Contains(t, string(out), "head main")
}

func TestSpliceReplacesATypeAndItsMethodsWithTheFixes(t *testing.T) {
	head := "package x\n\nimport \"testing\"\n\ntype box struct{ n int }\n\nfunc (b box) Size() int { return b.n }\n\nfunc (b box) OnlyHead() int { return 1 }\n\nfunc TestHead(t *testing.T) {}\n"
	fix := "package x\n\nimport \"testing\"\n\ntype box struct{ n, m int }\n\nfunc (b box) Size() int { return b.n + b.m }\n\nfunc TestFix(t *testing.T) { _ = box{n: 1}.Size() }\n"
	out, err := SpliceGoTests([]byte(head), []byte(fix), []string{"TestFix"})
	require.NoError(t, err)
	got := string(out)
	require.Contains(t, got, "n, m int")
	require.Contains(t, got, "b.n + b.m")
	require.NotContains(t, got, "return b.n }", "the head's Size is replaced")
	require.Contains(t, got, "OnlyHead", "a head method that the fix does not declare stays")
	require.Equal(t, 1, strings.Count(got, "type box struct"))
}

func TestSpliceWithNoHeadFileIsTheExtraction(t *testing.T) {
	a, err := SpliceGoTests(nil, []byte(fixTestFile), []string{"TestSeveralPartialRefunds"})
	require.NoError(t, err)
	b, err := ExtractGoTests([]byte(fixTestFile), []string{"TestSeveralPartialRefunds"})
	require.NoError(t, err)
	require.Equal(t, string(b), string(a))
}

func TestSpliceNamesAMissingTestAndRefusesABrokenHead(t *testing.T) {
	_, err := SpliceGoTests([]byte(headTestFile), []byte(fixTestFile), []string{"TestNoSuchThing"})
	require.ErrorContains(t, err, "TestNoSuchThing")
	_, err = SpliceGoTests([]byte("package x\nfunc {"), []byte(fixTestFile), []string{"TestSeveralPartialRefunds"})
	require.ErrorContains(t, err, "head")
}
