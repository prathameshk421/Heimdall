package eventlog_test

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/testkit/eventlog"
)

var update = flag.Bool("update", false, "rewrite each log.jsonl from its script.jsonl")

// TestPlanLogsBuildFromTheirScripts keeps every golden log under testdata/plan/ equal to what its script builds, and
// proves each one is a valid, chained log that a stored log could be.
func TestPlanLogsBuildFromTheirScripts(t *testing.T) {
	scripts, err := filepath.Glob(filepath.Join("..", "..", "..", "testdata", "plan", "*", "*", "script.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(scripts) == 0 {
		t.Fatal("no script.jsonl under testdata/plan/*/*/")
	}
	for _, s := range scripts {
		t.Run(filepath.Base(filepath.Dir(s)), func(t *testing.T) {
			got := build(t, s)
			want := filepath.Join(filepath.Dir(s), "log.jsonl")
			if *update {
				if err := os.WriteFile(want, got, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			stored, err := os.ReadFile(want)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(stored, got) {
				t.Fatalf("%s differs from what %s builds; run with -update if the script changed on purpose", want, s)
			}
			checkStored(t, stored)
		})
	}
}

func build(t *testing.T, script string) []byte {
	t.Helper()
	f, err := os.Open(script)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	lines, err := eventlog.ReadScript(f)
	if err != nil {
		t.Fatal(err)
	}
	cid := changeOf(t, filepath.Dir(script))
	events, err := eventlog.Build(cid, lines)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := eventlog.Encode(&buf, events); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// changeOf reads the change id from the directory's change.txt.
func changeOf(t *testing.T, dir string) id.ChangeID {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "change.txt"))
	if err != nil {
		t.Fatal(err)
	}
	cid, err := id.ParseChangeID(strings.TrimSpace(string(b)))
	if err != nil {
		t.Fatal(err)
	}
	return cid
}

// checkStored decodes the stored log as a reader of the event table would, and verifies every event and the chain.
func checkStored(t *testing.T, stored []byte) {
	t.Helper()
	var events []change.Event
	for i, line := range bytes.Split(bytes.TrimSuffix(stored, []byte("\n")), []byte("\n")) {
		var e change.Event
		if err := json.Unmarshal(line, &e); err != nil {
			t.Fatalf("line %d: %v", i+1, err)
		}
		if err := e.Validate(); err != nil {
			t.Fatalf("line %d: %v", i+1, err)
		}
		events = append(events, e)
	}
	if err := change.VerifyChain(events); err != nil {
		t.Fatal(err)
	}
}

// TestViewGoldensAreWellFormed checks the layout testdata/plan/view/README.md promises: each change directory has a
// policy that the product would accept at the merge base; each point's prefix is exactly the first lines of the log;
// and every point (or a single-point directory) has its view, missing and status, each ending in one newline.
func TestViewGoldensAreWellFormed(t *testing.T) {
	dirs, err := filepath.Glob(filepath.Join("..", "..", "..", "testdata", "plan", "view", "*", "log.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(dirs) == 0 {
		t.Fatal("no log.jsonl under testdata/plan/view/*/")
	}
	for _, l := range dirs {
		dir := filepath.Dir(l)
		t.Run(filepath.Base(dir), func(t *testing.T) {
			checkPolicy(t, filepath.Join(dir, "policy.yaml"))
			log := read(t, l)
			points, err := filepath.Glob(filepath.Join(dir, "point-*"))
			if err != nil {
				t.Fatal(err)
			}
			if len(points) == 0 {
				points = []string{dir}
			}
			for _, p := range points {
				checkPoint(t, dir, p, log)
			}
		})
	}
}

// checkPoint checks one point directory: a whole-line prefix of the log (unless the point is the log's own
// directory) and the three outputs, each JSON where it should be and ending in exactly one newline.
func checkPoint(t *testing.T, dir, p string, log []byte) {
	t.Helper()
	if p != dir {
		prefix := read(t, filepath.Join(p, "prefix.jsonl"))
		if len(prefix) == 0 || !bytes.HasPrefix(log, prefix) || prefix[len(prefix)-1] != '\n' {
			t.Fatalf("%s is not a whole-line prefix of %s/log.jsonl", p, dir)
		}
	}
	for _, f := range []string{"view.json", "missing.json", "status.txt"} {
		b := read(t, filepath.Join(p, f))
		if !bytes.HasSuffix(b, []byte("\n")) || bytes.HasSuffix(b, []byte("\n\n")) {
			t.Fatalf("%s/%s must end in exactly one newline", p, f)
		}
		if strings.HasSuffix(f, ".json") && !json.Valid(b) {
			t.Fatalf("%s/%s is not JSON", p, f)
		}
	}
}

func checkPolicy(t *testing.T, path string) {
	t.Helper()
	var p policy.Policy
	dec := yaml.NewDecoder(bytes.NewReader(read(t, path)))
	dec.KnownFields(true)
	if err := dec.Decode(&p); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}

func read(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
