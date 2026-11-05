package pack

import (
	"errors"
	"io"
	"io/fs"
	"testing"
	"testing/fstest"
	"time"
)

var errMarkdownFileClose = errors.New("close markdown file")

type closeErrorRoot struct{}

func (closeErrorRoot) Open(string) (fs.File, error) {
	return closeErrorFile{reader: &closeErrorReader{}}, nil
}

func (closeErrorRoot) ReadDir(string) ([]fs.DirEntry, error) {
	return []fs.DirEntry{closeErrorEntry{}}, nil
}

type closeErrorFile struct{ reader *closeErrorReader }

func (f closeErrorFile) Stat() (fs.FileInfo, error) { return closeErrorInfo{}, nil }

func (f closeErrorFile) Read(p []byte) (int, error) { return f.reader.Read(p) }

func (closeErrorFile) Close() error { return errMarkdownFileClose }

type closeErrorReader struct{ read bool }

func (r *closeErrorReader) Read(p []byte) (int, error) {
	if r.read {
		return 0, io.EOF
	}
	r.read = true
	copy(p, "content")
	return len("content"), nil
}

type closeErrorEntry struct{}

func (closeErrorEntry) Name() string               { return "doc.md" }
func (closeErrorEntry) IsDir() bool                { return false }
func (closeErrorEntry) Type() fs.FileMode          { return 0 }
func (closeErrorEntry) Info() (fs.FileInfo, error) { return closeErrorInfo{}, nil }

type closeErrorInfo struct{}

func (closeErrorInfo) Name() string       { return "doc.md" }
func (closeErrorInfo) Size() int64        { return int64(len("content")) }
func (closeErrorInfo) Mode() fs.FileMode  { return 0 }
func (closeErrorInfo) ModTime() time.Time { return time.Time{} }
func (closeErrorInfo) IsDir() bool        { return false }
func (closeErrorInfo) Sys() any           { return nil }

func TestMarkdownCollectorCollectsStableMarkdownDocuments(t *testing.T) {
	root := NewFSRoot(fstest.MapFS{
		"AGENTS.md":         &fstest.MapFile{Data: []byte("agents")},
		"README.md":         &fstest.MapFile{Data: []byte("readme")},
		"docs/z.md":         &fstest.MapFile{Data: []byte("z")},
		"docs/a.markdown":   &fstest.MapFile{Data: []byte("a")},
		"docs/code.go":      &fstest.MapFile{Data: []byte("not markdown")},
		"notes/meeting.txt": &fstest.MapFile{Data: []byte("not markdown")},
		"notes/meeting.md":  &fstest.MapFile{Data: []byte("meeting")},
	})
	collector, err := NewCollector(root, Limits{MaxFiles: 10, MaxBytes: 100})
	if err != nil {
		t.Fatalf("NewCollector: %v", err)
	}

	documents, err := collector.Collect([]string{"docs", "AGENTS.md", "README.md", "notes", "docs"})
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	wantPaths := []string{"AGENTS.md", "README.md", "docs/a.markdown", "docs/z.md", "notes/meeting.md"}
	if len(documents) != len(wantPaths) {
		t.Fatalf("document count = %d, want %d", len(documents), len(wantPaths))
	}
	for i, wantPath := range wantPaths {
		if documents[i].Path != wantPath || documents[i].Provenance != "doc:"+wantPath {
			t.Fatalf("document %d provenance = %#v, want path %q", i, documents[i], wantPath)
		}
	}
	if documents[0].Content != "agents" || documents[2].Content != "a" {
		t.Fatalf("document contents were not collected: %#v", documents)
	}
	if documents[0].SHA256 == [32]byte{} {
		t.Fatal("document hash was not populated")
	}
}

func TestMarkdownCollectorRejectsUnsafeAndSymlinkSources(t *testing.T) {
	root := NewFSRoot(fstest.MapFS{
		"docs/link.md": &fstest.MapFile{Mode: fs.ModeSymlink},
		"docs/good.md": &fstest.MapFile{Data: []byte("safe")},
	})
	collector, err := NewCollector(root, Limits{MaxFiles: 2, MaxBytes: 20})
	if err != nil {
		t.Fatalf("NewCollector: %v", err)
	}
	for _, source := range []string{"../secret.md", "/secret.md", "docs\\good.md", "."} {
		if _, err := collector.Collect([]string{source}); !errors.Is(err, ErrInvalidSource) {
			t.Errorf("Collect(%q) error = %v, want ErrInvalidSource", source, err)
		}
	}
	if _, err := collector.Collect([]string{"docs"}); !errors.Is(err, ErrSymlink) {
		t.Fatalf("symlink collection error = %v, want ErrSymlink", err)
	}
}

func TestMarkdownCollectorEnforcesFileAndByteBounds(t *testing.T) {
	root := NewFSRoot(fstest.MapFS{
		"a.md": &fstest.MapFile{Data: []byte("1234")},
		"b.md": &fstest.MapFile{Data: []byte("5678")},
	})
	for name, limits := range map[string]Limits{
		"file count": {MaxFiles: 1, MaxBytes: 100},
		"bytes":      {MaxFiles: 2, MaxBytes: 7},
	} {
		t.Run(name, func(t *testing.T) {
			collector, err := NewCollector(root, limits)
			if err != nil {
				t.Fatalf("NewCollector: %v", err)
			}
			if _, err := collector.Collect([]string{"a.md", "b.md"}); !errors.Is(err, ErrCollectionLimit) {
				t.Fatalf("Collect error = %v, want ErrCollectionLimit", err)
			}
		})
	}
}

func TestMarkdownCollectorReturnsCloseErrorWithoutDocument(t *testing.T) {
	collector, err := NewCollector(closeErrorRoot{}, Limits{MaxFiles: 1, MaxBytes: 100})
	if err != nil {
		t.Fatalf("NewCollector: %v", err)
	}

	documents, err := collector.Collect([]string{"doc.md"})
	if !errors.Is(err, errMarkdownFileClose) {
		t.Fatalf("Collect error = %v, want close error", err)
	}
	if documents != nil {
		t.Fatalf("Collect documents = %#v, want nil", documents)
	}
}
