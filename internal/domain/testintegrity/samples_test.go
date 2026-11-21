package testintegrity_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	ti "github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/testintegrity"
)

// sampleCase is one real-looking test file in testdata and the facts a caller's parser would extract from it.
type sampleCase struct {
	Path    string   `json:"path"`
	Changed []string `json:"changed"`
	Mocks   []struct {
		Kind   ti.MockKind `json:"kind"`
		Target string      `json:"target"`
		Line   int         `json:"line"`
	} `json:"mocks"`
}

// loadSample reads testdata/<name>: the case file and the single sample source file next to it. The whole sample is the
// added content of a new test file.
func loadSample(t *testing.T, name string) ti.Facts {
	t.Helper()
	dir := filepath.Join("testdata", name)
	raw, err := os.ReadFile(filepath.Join(dir, "case.json"))
	require.NoError(t, err)
	var c sampleCase
	require.NoError(t, json.Unmarshal(raw, &c))

	src, err := os.ReadFile(filepath.Join(dir, filepath.Base(c.Path)))
	require.NoError(t, err)

	f := ti.Facts{OwnerKnown: true}
	for _, p := range c.Changed {
		ch := ti.FileChange{Path: p, Status: ti.StatusModified, IsTest: p == c.Path}
		if p == c.Path {
			ch.Status = ti.StatusAdded
			ch.Added = strings.Split(strings.TrimRight(string(src), "\n"), "\n")
		}
		f.Files = append(f.Files, ch)
	}
	tf := ti.TestFile{Path: c.Path}
	for _, m := range c.Mocks {
		tf.Mocks = append(tf.Mocks, ti.MockFact{Kind: m.Kind, Target: m.Target, Line: m.Line, Added: true})
	}
	f.Tests = []ti.TestFile{tf}
	return f
}
