package codegraph

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

const goPay = `package pay

type Refund struct{}

// Create makes a refund.
func (s *Service) Create(amount int) error {
	return validateAmount(amount)
}

func validateAmount(n int) error {
	return nil
}
`

const goHandler = `package api; import "m/pay"

func Handle() {
	s := &pay.Service{}
	_ = s.Create(5)
	validateAmount(3)
}
`

func TestGoDefinitionsAndReferences(t *testing.T) {
	idx := FromFiles(map[string]string{"pay/pay.go": goPay, "api/h.go": goHandler})

	defs := idx.Definitions("Create")
	require.Len(t, defs, 1)
	require.Equal(t, Def{Name: "Create", Kind: "method", File: "pay/pay.go", Line: 6, EndLine: 8, Signature: "func (s *Service) Create(amount int) error", Recv: "Service"}, defs[0])
	require.Len(t, idx.Definitions("Service.Create"), 1)
	require.Empty(t, idx.Definitions("Other.Create"))
	require.Equal(t, "type", idx.Definitions("Refund")[0].Kind)

	refs := idx.References("validateAmount")
	require.Equal(t, []Ref{{"validateAmount", "api/h.go", 6}, {"validateAmount", "pay/pay.go", 7}}, refs, "the definition site is not a reference")
	require.Contains(t, idx.References("Create"), Ref{"Create", "api/h.go", 5})
}

func TestBrokenGoFallsBackToLineTagging(t *testing.T) {
	idx := FromFiles(map[string]string{"a.go": "package a\nfunc Broken( {\nfunc Fine() {}\n"})
	require.Len(t, idx.Definitions("Fine"), 1)
}

func TestPythonDefinitionsAndReferences(t *testing.T) {
	idx := FromFiles(map[string]string{
		"app/svc.py":  "class Billing:\n    def charge_card(self, amount):\n        return amount\n\ndef helper():\n    pass\n",
		"app/main.py": "from svc import Billing\n\ndef run():\n    Billing().charge_card(5)\n",
	})
	d := idx.Definitions("charge_card")
	require.Len(t, d, 1)
	require.Equal(t, 2, d[0].Line)
	require.Equal(t, 4, d[0].EndLine, "a definition ends before the next one")
	require.Equal(t, []Ref{{"charge_card", "app/main.py", 4}}, idx.References("charge_card"))
	require.Equal(t, "type", idx.Definitions("Billing")[0].Kind)
}

func graphFiles() map[string]string {
	return map[string]string{
		"core/ledger.go":      "package core\n\nfunc PostEntryToLedger() {}\n",
		"api/caller.go":       "package api\n\nimport \"m/core\"\n\nfunc A() { PostEntryToLedger() }\n",
		"api/unrelated.go":    "package api\n\nfunc B() { localOnly() }\nfunc localOnly() {}\n",
		"util/deep.go":        "package util\n\nimport \"m/api\"\n\nfunc C() { api.A() }\n",
		"common/a.go":         "package a\n\nfunc Close() {}\n",
		"common/b.go":         "package b\n\nfunc Close() {}\n",
		"common/c.go":         "package c\n\nfunc Close() {}\n",
		"common/d.go":         "package d\n\nfunc Close() {}\n",
		"common/e.go":         "package e\n\nfunc Close() {}\n",
		"common/f.go":         "package f\n\nfunc Close() {}\n",
		"common/g.go":         "package g\n\nfunc Close() {}\n",
		"common/h.go":         "package h\n\nfunc Close() {}\n",
		"common/i.go":         "package i\n\nfunc Close() {}\n",
		"common/user.go":      "package u\n\nfunc U() { Close() }\n",
		"core/ledger_test.go": "package core\n\nfunc TestPost() { PostEntryToLedger() }\n",
	}
}

func TestDependentsRankACallerAboveAnUnrelatedFile(t *testing.T) {
	idx := FromFiles(graphFiles())
	got := idx.rank([]string{"core/ledger.go"}, true)
	var names []string
	for _, f := range got {
		names = append(names, f.File)
	}
	require.Contains(t, names, "api/caller.go")
	require.NotContains(t, names, "api/unrelated.go")
	require.Less(t, indexOf(names, "api/caller.go"), indexOf(names, "core/ledger_test.go")+10)
	fwd := idx.rank([]string{"api/caller.go"}, false)
	require.Equal(t, "core/ledger.go", fwd[0].File)
}

func indexOf(s []string, v string) int {
	for i, x := range s {
		if x == v {
			return i
		}
	}
	return -1
}

func TestACommonNameMakesNoEdge(t *testing.T) {
	idx := FromFiles(graphFiles())
	require.Empty(t, idx.fwd["common/user.go"], "Close is defined in 9 files, so it links nothing")
}

const diffPay = `diff --git a/pay/pay.go b/pay/pay.go
--- a/pay/pay.go
+++ b/pay/pay.go
@@ -6,3 +6,4 @@ func x
 func (s *Service) Create(amount int) error {
+	logCall(amount); validateAmount(1)
 	return validateAmount(amount)
 }
`

func TestParseDiffKeepsNewSideLinesAndAddedText(t *testing.T) {
	d := parseDiff(diffPay + "diff --git a/n.txt b/n.txt\n--- a/n.txt\n+++ b/n.txt\n@@ -1,1 +1,2 @@\n keep\n+++ not a header\n")
	require.Equal(t, map[int]bool{7: true}, d["pay/pay.go"].lines)
	require.Equal(t, []string{"\tlogCall(amount); validateAmount(1)"}, d["pay/pay.go"].added)
	require.Equal(t, []string{"++ not a header"}, d["n.txt"].added, "an added line that starts with ++ is data")
}

func TestImpactFindsChangedCallersTestsAndUntested(t *testing.T) {
	files := graphFiles()
	files["pay/pay.go"] = goPay
	files["api/h.go"] = goHandler
	files["pay/pay_test.go"] = "package pay\n\nfunc TestV() { validateAmount(1) }\n"
	head := FromFiles(files)
	m := Impact(head, nil, diffPay, nil)

	require.Len(t, m.Changed, 1)
	c := m.Changed[0]
	require.Equal(t, "Create", c.Def.Name)
	require.Len(t, c.Callers, 1)
	require.Equal(t, Ref{"Create", "api/h.go", 5}, c.Callers[0].Ref)
	require.Equal(t, 0, c.TestRefs)
	require.Equal(t, []string{"Create"}, m.Untested)
	require.Contains(t, m.Symbols(), "Service.Create")
	require.Contains(t, m.Symbols(), "validateAmount", "a defined name called in an added line counts")
	require.NotContains(t, m.Symbols(), "logCall", "an undefined name is left out")
	require.Contains(t, m.DependentFiles(), "api/h.go")
}

func TestImpactSeesARemovedSymbolThatIsStillUsed(t *testing.T) {
	base := FromFiles(map[string]string{"lib/a.go": "package lib\n\nfunc OldHelper() {}\nfunc Kept() {}\nfunc Moved() {}\n", "x/use.go": "package x\n\nfunc F() { OldHelper(); Moved() }\n"})
	head := FromFiles(map[string]string{"lib/a.go": "package lib\n\nfunc Kept() {}\n", "lib/b.go": "package lib\n\nfunc Moved() {}\n", "x/use.go": "package x\n\nfunc F() { OldHelper(); Moved() }\n"})
	m := Impact(head, base, "--- a/lib/a.go\n+++ b/lib/a.go\n@@ -1,5 +1,3 @@\n package lib\n \n-func OldHelper() {}\n func Kept() {}\n-func Moved() {}\n", []string{"lib/a.go"})
	require.Len(t, m.RemovedStillUsed, 1)
	require.Equal(t, "OldHelper", m.RemovedStillUsed[0].Def.Name)
	require.Equal(t, []Ref{{"OldHelper", "x/use.go", 3}}, m.RemovedStillUsed[0].Refs)
	require.Contains(t, m.Symbols(), "OldHelper")
}

func TestRenderCutsBetweenLinesAndSaysSo(t *testing.T) {
	files := graphFiles()
	files["pay/pay.go"] = goPay
	files["api/h.go"] = goHandler
	m := Impact(FromFiles(files), nil, diffPay, nil)
	full := m.Render(100_000)
	for _, want := range []string{"name only", "CHANGED SYMBOLS AND CALLERS", "api/h.go:5", "NO TEST REFERENCES", "FILES THAT DEPEND ON THIS CHANGE"} {
		require.Contains(t, full, want)
	}
	require.NotContains(t, full, "REMOVED BUT STILL USED", "an empty section is left out")
	cut := m.Render(220)
	require.LessOrEqual(t, len(cut), 220)
	require.Contains(t, cut, "[cut")
	require.True(t, strings.HasSuffix(cut, "\n"))
	require.Empty(t, ImpactMap{}.Render(500))
}

func TestSearchGlobBadPatternAndMax(t *testing.T) {
	idx := FromFiles(map[string]string{"a/x.go": "alpha\nbeta alpha\n", "b/y.go": "alpha\n", "a/z.txt": "alpha\n"})
	hits, err := idx.Search("al.ha", "**/*.go", 10)
	require.NoError(t, err)
	require.Equal(t, []Hit{{"a/x.go", 1, "alpha"}, {"a/x.go", 2, "beta alpha"}, {"b/y.go", 1, "alpha"}}, hits)
	hits, err = idx.Search("alpha", "a/**", 1)
	require.NoError(t, err)
	require.Len(t, hits, 1)
	_, err = idx.Search("(", "", 5)
	require.Error(t, err)
	_, err = idx.Search(strings.Repeat("a", 257), "", 5)
	require.Error(t, err)
	long, err := idx.Search("x", "", 5)
	require.NoError(t, err)
	require.Empty(t, long)
}

func makeTar(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	require.NoError(t, tw.WriteHeader(&tar.Header{Name: "repo-abc/", Typeflag: tar.TypeDir, Mode: 0o755}))
	for name, body := range entries {
		require.NoError(t, tw.WriteHeader(&tar.Header{Name: "repo-abc/" + name, Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(body))}))
		_, err := tw.Write([]byte(body))
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	return buf.Bytes()
}

func TestBuildStripsThePrefixAndSkipsVendorAndBinaries(t *testing.T) {
	data := makeTar(t, map[string]string{
		"main.go":                 "package main\nfunc Main() {}\n",
		"vendor/x/x.go":           "package x\nfunc V() {}\n",
		"node_modules/a/index.js": "function n() {}",
		"web/app.min.js":          "function m() {}",
		"go.sum":                  "abc",
		"img/logo.png":            "png",
		"blob.dat":                "ab\x00cd",
		"internal/deep/file.py":   "def deep_one():\n    pass\n",
	})
	idx, err := Build(bytes.NewReader(data))
	require.NoError(t, err)
	require.False(t, idx.Truncated())
	require.Equal(t, []string{"internal/deep/file.py", "main.go"}, idx.paths)
	require.Len(t, idx.Definitions("deep_one"), 1)
	_, err = Build(strings.NewReader("not gzip"))
	require.Error(t, err)
}

type fakeSource struct {
	calls atomic.Int32
	gate  chan struct{}
	fail  atomic.Bool
	data  []byte
}

func (f *fakeSource) Tarball(_ context.Context, _ change.Repo, _ id.SHA) (io.ReadCloser, error) {
	f.calls.Add(1)
	if f.gate != nil {
		<-f.gate
	}
	if f.fail.Load() {
		return nil, errors.New("github down")
	}
	return io.NopCloser(bytes.NewReader(f.data)), nil
}

func TestServiceSharesOneBuildAmongConcurrentCallers(t *testing.T) {
	src := &fakeSource{gate: make(chan struct{}), data: makeTar(t, map[string]string{"a.go": "package a\nfunc A() {}\n"})}
	svc, err := NewService(src, 2)
	require.NoError(t, err)
	var wg sync.WaitGroup
	results := make([]*Index, 5)
	errs := make([]error, len(results))
	for n := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[n], errs[n] = svc.Index(context.Background(), "o/r", "sha1")
		}()
	}
	for src.calls.Load() == 0 {
		runtime.Gosched() // wait until the leader reached the source
	}
	close(src.gate)
	wg.Wait()
	for _, e := range errs {
		require.NoError(t, e)
	}
	for _, r := range results {
		require.Same(t, results[0], r)
	}
	again, err := svc.Index(context.Background(), "o/r", "sha1")
	require.NoError(t, err)
	require.Same(t, results[0], again)
	require.EqualValues(t, 1, src.calls.Load())
}

func TestServiceDoesNotCacheFailureAndHonoursContext(t *testing.T) {
	src := &fakeSource{data: makeTar(t, map[string]string{"a.go": "package a\n"})}
	src.fail.Store(true)
	svc, err := NewService(src, 1)
	require.NoError(t, err)
	_, err = svc.Index(context.Background(), "o/r", "s")
	require.Error(t, err)
	src.fail.Store(false)
	_, err = svc.Index(context.Background(), "o/r", "s")
	require.NoError(t, err)
	require.EqualValues(t, 2, src.calls.Load())

	blocked := &fakeSource{gate: make(chan struct{}), data: src.data}
	svc2, err := NewService(blocked, 1)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = svc2.Index(ctx, "o/r", "s")
	require.ErrorIs(t, err, context.Canceled)
	close(blocked.gate)

	_, err = NewService(nil, 1)
	require.Error(t, err)
	_, err = NewService(src, 0)
	require.Error(t, err)
}

func TestImpactFindsAContractStringThatLeftTheChangeButOtherFilesStillName(t *testing.T) {
	head := FromFiles(map[string]string{
		"cmd/main.go":      "package main\n\nvar v = flag.Bool(\"preview\", false, \"\")\n",
		"scripts/run.sh":   "./app --dry-run\n./app\n",
		"scripts/other.sh": "./app --not-dry-running\n",
	})
	diff := "--- a/cmd/main.go\n+++ b/cmd/main.go\n@@ -3,1 +3,1 @@\n-var v = flag.Bool(\"dry-run\", false, \"plan the batch and send nothing\")\n+var v = flag.Bool(\"preview\", false, \"\")\n"
	m := Impact(head, nil, diff, []string{"cmd/main.go"})
	require.Len(t, m.RemovedStrings, 1, "a sentence is not a contract string")
	require.Equal(t, "dry-run", m.RemovedStrings[0].Text)
	require.Equal(t, []Ref{{"dry-run", "scripts/run.sh", 1}}, m.RemovedStrings[0].UsedAt, "a longer word that holds the text is not a use")
	require.Contains(t, m.Render(4000), "scripts/run.sh:1")
	require.Contains(t, m.Symbols(), "dry-run")
}

func TestGoCallersMustImportTheDefinitionsFolder(t *testing.T) {
	head := FromFiles(map[string]string{
		"internal/refunds/s.go": "package refunds\n\nfunc Create() {}\n",
		"internal/pay/p.go":     "package pay\n\nfunc Create() {}\n\nfunc use() { Create() }\n",
		"api/a.go":              "package api\n\nimport \"app/internal/refunds\"\n\nfunc h() { refunds.Create() }\n",
	})
	diff := "--- a/internal/refunds/s.go\n+++ b/internal/refunds/s.go\n@@ -3,0 +3,1 @@\n+func Create() {}\n"
	m := Impact(head, nil, diff, []string{"internal/refunds/s.go"})
	require.Len(t, m.Changed, 1)
	require.Len(t, m.Changed[0].Callers, 1)
	require.Equal(t, "api/a.go", m.Changed[0].Callers[0].File)
	require.Equal(t, "h", m.Changed[0].Callers[0].In)
}

func TestImpactLeavesOutTestFunctionsAndFoldsNewSymbolsWithNoCallers(t *testing.T) {
	head := FromFiles(map[string]string{
		"a/a.go":      "package a\n\nfunc Fresh() {}\n\nfunc Old() {}\n",
		"a/a_test.go": "package a\n\nfunc TestX() {}\n",
		"b/b.go":      "package b\n\nimport \"m/a\"\n\nfunc g() { a.Old() }\n",
	})
	base := FromFiles(map[string]string{"a/a.go": "package a\n\nfunc Old() {}\n"})
	diff := "--- a/a/a.go\n+++ b/a/a.go\n@@ -3,0 +3,4 @@\n+func Fresh() {}\n+\n+func Old() {}\n+\n--- a/a/a_test.go\n+++ b/a/a_test.go\n@@ -3,0 +3,1 @@\n+func TestX() {}\n"
	m := Impact(head, base, diff, []string{"a/a.go", "a/a_test.go"})
	require.Len(t, m.Changed, 2)
	require.Equal(t, "Old", m.Changed[0].Def.Name, "existing code with callers comes first")
	text := m.Render(4000)
	require.NotContains(t, text, "TestX")
	require.Contains(t, text, "NEW SYMBOLS NOBODY OUTSIDE THE CHANGE CALLS\nFresh")
}
