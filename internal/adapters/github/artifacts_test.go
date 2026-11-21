package github

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io/fs"
	"net/http"
	"strings"
	"testing"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

const (
	runArtifactsPath = repoRoot + "/actions/runs/99/artifacts"
	artifactZipPath  = repoRoot + "/actions/artifacts/5/zip"
)

var testLimits = ports.ArtifactLimits{MaxZipBytes: 1 << 20, MaxEntries: 10, MaxFileBytes: 1 << 10, MaxTotalBytes: 4 << 10}

// zipEntry is one entry a test zip holds. Header, when set, is used as it is.
type zipEntry struct {
	name string
	data string
	mode fs.FileMode // 0 means a regular file
}

// buildZip writes entries without any check, so a test can build a hostile zip.
func buildZip(t *testing.T, entries ...zipEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		h := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
		if e.mode != 0 {
			h.SetMode(e.mode)
		}
		w, err := zw.CreateHeader(h)
		if err != nil {
			t.Fatalf("zip header: %v", err)
		}
		if _, err := w.Write([]byte(e.data)); err != nil {
			t.Fatalf("zip write: %v", err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

func serveZip(env *actionEnv, raw []byte) {
	env.f.route("GET", artifactZipPath, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write(raw)
	})
}

func TestRunArtifactsPaginatesAndMapsFields(t *testing.T) {
	env := newActionEnv(t)
	next := env.f.srv.URL + runArtifactsPath + "?per_page=100&page=2"
	art := func(id int, name string, expired bool) map[string]any {
		return map[string]any{
			"id": id, "name": name, "size_in_bytes": 123, "expired": expired, "created_at": "2026-11-04T12:00:00Z",
			"workflow_run": map[string]any{"id": 99, "head_sha": shaHead},
		}
	}
	env.f.route("GET", runArtifactsPath, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "2" {
			writeJSON(w, http.StatusOK, map[string]any{"total_count": 2, "artifacts": []any{art(6, "coverage", true)}})
			return
		}
		w.Header().Set("Link", `<`+next+`>; rel="next"`)
		writeJSON(w, http.StatusOK, map[string]any{"total_count": 2, "artifacts": []any{art(5, "proof", false)}})
	})
	got, err := env.x.RunArtifacts(context.Background(), testRepo, 99)
	if err != nil {
		t.Fatalf("RunArtifacts: %v", err)
	}
	if len(got) != 2 || got[0].ID != 5 || got[0].Name != "proof" || got[0].RunID != 99 || string(got[0].HeadSHA) != shaHead || got[0].SizeBytes != 123 {
		t.Errorf("artifacts = %+v", got)
	}
	if !got[1].Expired {
		t.Error("the expired flag was lost")
	}
	env.wantAuth(t)
}

func TestRunArtifactsMapsErrorsAndRefusesABadRunID(t *testing.T) {
	env := newActionEnv(t)
	env.f.route("GET", runArtifactsPath, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
	})
	if _, err := env.x.RunArtifacts(context.Background(), testRepo, 99); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("error = %v, want ports.ErrNotFound", err)
	}
	if _, err := env.x.RunArtifacts(context.Background(), testRepo, 0); err == nil {
		t.Error("run id 0 was accepted")
	}
}

func TestDownloadArtifactReturnsRegularFiles(t *testing.T) {
	env := newActionEnv(t)
	serveZip(env, buildZip(t,
		zipEntry{name: "proof/", mode: fs.ModeDir | 0o755},
		zipEntry{name: "proof/result.json", data: `{"ok":true}`},
		zipEntry{name: "lcov.info", data: "SF:a.go"},
	))
	got, err := env.x.DownloadArtifact(context.Background(), testRepo, 5, testLimits)
	if err != nil {
		t.Fatalf("DownloadArtifact: %v", err)
	}
	if len(got) != 2 || got[0].Path != "proof/result.json" || string(got[0].Content) != `{"ok":true}` || got[1].Path != "lcov.info" {
		t.Errorf("files = %+v", got)
	}
	env.wantAuth(t)
}

func TestDownloadArtifactFollowsTheSignedRedirect(t *testing.T) {
	env := newActionEnv(t)
	raw := buildZip(t, zipEntry{name: "a.txt", data: "hi"})
	env.f.route("GET", artifactZipPath, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/signed/blob", http.StatusFound)
	})
	env.f.route("GET", "/signed/blob", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(raw) })
	got, err := env.x.DownloadArtifact(context.Background(), testRepo, 5, testLimits)
	if err != nil || len(got) != 1 || string(got[0].Content) != "hi" {
		t.Fatalf("DownloadArtifact = %+v, %v", got, err)
	}
}

func TestDownloadArtifactRefusesAZipOverTheCapWhileStreaming(t *testing.T) {
	env := newActionEnv(t)
	// The reply is far larger than the cap. The adapter must stop at the cap, so the server sees a failed write.
	big := bytes.Repeat([]byte{'z'}, 5<<20)
	env.f.route("GET", artifactZipPath, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(big) })
	limits := testLimits
	limits.MaxZipBytes = 1 << 10
	_, err := env.x.DownloadArtifact(context.Background(), testRepo, 5, limits)
	if !errors.Is(err, ports.ErrArtifactRefused) {
		t.Fatalf("error = %v, want ports.ErrArtifactRefused", err)
	}
}

func TestDownloadArtifactAcceptsAZipExactlyAtTheCap(t *testing.T) {
	env := newActionEnv(t)
	raw := buildZip(t, zipEntry{name: "a.txt", data: "hi"})
	serveZip(env, raw)
	limits := testLimits
	limits.MaxZipBytes = int64(len(raw))
	if _, err := env.x.DownloadArtifact(context.Background(), testRepo, 5, limits); err != nil {
		t.Fatalf("a zip at the cap was refused: %v", err)
	}
	serveZip(env, raw) // same route; one byte under the cap now fails
	limits.MaxZipBytes = int64(len(raw)) - 1
	if _, err := env.x.DownloadArtifact(context.Background(), testRepo, 5, limits); !errors.Is(err, ports.ErrArtifactRefused) {
		t.Fatalf("a zip one byte over the cap: error = %v, want refused", err)
	}
}

func TestUnpackArtifactRefusesEscapingPaths(t *testing.T) {
	paths := []string{
		"../evil.txt", "a/../../evil.txt", "a/../b", "/etc/passwd", "C:/x.txt", "c:evil", `..\evil.txt`, `a\b`,
		"./a.txt", "a//b", "a/./b", "..", "a/\x00b",
	}
	for _, name := range paths {
		raw := buildZip(t, zipEntry{name: "ok.txt", data: "fine"}, zipEntry{name: name, data: "x"})
		_, err := unpackArtifact(raw, testLimits)
		if !errors.Is(err, ports.ErrArtifactRefused) {
			t.Errorf("path %q: error = %v, want ports.ErrArtifactRefused", name, err)
			continue
		}
		if strings.Contains(err.Error(), "evil") {
			t.Errorf("path %q: the error names the entry path: %v", name, err)
		}
	}
}

func TestUnpackArtifactRefusesTooManyEntries(t *testing.T) {
	var entries []zipEntry
	for i := 0; i < testLimits.MaxEntries+1; i++ {
		entries = append(entries, zipEntry{name: "f" + string(rune('a'+i)), data: "x"})
	}
	if _, err := unpackArtifact(buildZip(t, entries...), testLimits); !errors.Is(err, ports.ErrArtifactRefused) {
		t.Fatalf("error = %v, want refused", err)
	}
	if _, err := unpackArtifact(buildZip(t, entries[:testLimits.MaxEntries]...), testLimits); err != nil {
		t.Fatalf("a zip at the entry limit was refused: %v", err)
	}
}

func TestUnpackArtifactRefusesALargeEntryAndALargeTotal(t *testing.T) {
	one := buildZip(t, zipEntry{name: "big", data: strings.Repeat("a", int(testLimits.MaxFileBytes)+1)})
	if _, err := unpackArtifact(one, testLimits); !errors.Is(err, ports.ErrArtifactRefused) {
		t.Errorf("one large entry: error = %v, want refused", err)
	}
	chunk := strings.Repeat("a", int(testLimits.MaxFileBytes))
	total := buildZip(t, zipEntry{name: "1", data: chunk}, zipEntry{name: "2", data: chunk}, zipEntry{name: "3", data: chunk},
		zipEntry{name: "4", data: chunk}, zipEntry{name: "5", data: "x"})
	if _, err := unpackArtifact(total, testLimits); !errors.Is(err, ports.ErrArtifactRefused) {
		t.Errorf("large total: error = %v, want refused", err)
	}
}

func TestUnpackArtifactDoesNotTrustAHeaderThatLiesAboutSize(t *testing.T) {
	raw := buildZip(t, zipEntry{name: "lie", data: strings.Repeat("a", int(testLimits.MaxFileBytes)*8)})
	// Patch the declared uncompressed size in the central directory and the local header to a small number.
	patched := bytes.ReplaceAll(raw, le32(uint32(testLimits.MaxFileBytes)*8), le32(10))
	if bytes.Equal(patched, raw) {
		t.Fatal("the test did not patch the declared size")
	}
	_, err := unpackArtifact(patched, testLimits)
	if err == nil {
		t.Fatal("a lying header was accepted")
	}
	if !errors.Is(err, ports.ErrArtifactRefused) {
		t.Errorf("error = %v, want ports.ErrArtifactRefused", err)
	}
}

func TestUnpackArtifactRefusesSymlinksRepeatsAndNonZips(t *testing.T) {
	link := buildZip(t, zipEntry{name: "link", data: "/etc/passwd", mode: fs.ModeSymlink | 0o777})
	if _, err := unpackArtifact(link, testLimits); !errors.Is(err, ports.ErrArtifactRefused) {
		t.Errorf("symlink: error = %v, want refused", err)
	}
	dup := buildZip(t, zipEntry{name: "a", data: "1"}, zipEntry{name: "a", data: "2"})
	if _, err := unpackArtifact(dup, testLimits); !errors.Is(err, ports.ErrArtifactRefused) {
		t.Errorf("repeated path: error = %v, want refused", err)
	}
	if _, err := unpackArtifact([]byte("not a zip at all"), testLimits); !errors.Is(err, ports.ErrArtifactRefused) {
		t.Errorf("non-zip: error = %v, want refused", err)
	}
}

func TestDownloadArtifactMapsHTTPErrorsAndRefusesBadInput(t *testing.T) {
	env := newActionEnv(t)
	env.f.route("GET", artifactZipPath, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
	})
	if _, err := env.x.DownloadArtifact(context.Background(), testRepo, 5, testLimits); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("error = %v, want ports.ErrNotFound", err)
	}
	if _, err := env.x.DownloadArtifact(context.Background(), testRepo, 5, ports.ArtifactLimits{}); err == nil {
		t.Error("zero limits were accepted")
	}
	if _, err := env.x.DownloadArtifact(context.Background(), testRepo, 0, testLimits); err == nil {
		t.Error("artifact id 0 was accepted")
	}
}

// le32 is a little-endian 32-bit value, the way a zip header stores a size.
func le32(v uint32) []byte { return []byte{byte(v), byte(v >> 8), byte(v >> 16), byte(v >> 24)} }
