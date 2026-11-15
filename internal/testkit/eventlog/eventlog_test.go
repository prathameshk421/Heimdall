package eventlog_test

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
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
