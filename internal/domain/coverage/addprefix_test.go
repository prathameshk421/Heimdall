package coverage

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAddPrefixSupportsSubdirectoryAndJavaReports(t *testing.T) {
	// Java Cobertura-style relative path under src/main/java
	b, err := NewBuilder(Options{MaxBytes: 1 << 20, AddPrefix: "src/main/java"})
	require.NoError(t, err)
	require.NoError(t, b.Add(1, "com/example/Foo.java", 12, 1))
	require.NoError(t, b.Add(2, "com/example/Foo.java", 13, 0))

	rep, err := Diff(b.Result(), map[string][]int{"src/main/java/com/example/Foo.java": {12, 13}})
	require.NoError(t, err)
	require.True(t, rep.Measurable())
	require.Equal(t, 1, rep.Total.Covered)
	require.Equal(t, 1, rep.Total.Uncovered)
	require.InDelta(t, 50.0, rep.Percent, 1e-9)

	// Subdirectory Go module: strip module path, add subdirectory prefix
	goCover := []byte("mode: set\nexample.com/pay/refund.go:10.1,12.1 1 1\n")
	res, err := ParseGoCover(goCover, Options{MaxBytes: 1 << 20, Prefix: "example.com/pay", AddPrefix: "services/pay"})
	require.NoError(t, err)
	goRep, err := Diff(res, map[string][]int{"services/pay/refund.go": {10, 11}})
	require.NoError(t, err)
	require.True(t, goRep.Measurable())
	require.Equal(t, 2, goRep.Total.Covered)
}

func TestAddPrefixRefusesTraversal(t *testing.T) {
	_, err := NewBuilder(Options{MaxBytes: 1 << 20, AddPrefix: "../escape"})
	require.Error(t, err)
}
