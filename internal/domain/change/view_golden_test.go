package change_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
)

// The goldens of testdata/plan/view (recorded KAN-22 and KAN-23 logs, and one authored log): Fold, then Missing with
// the policy at the merge base, reproduce view.json and missing.json byte for byte at every point.
func TestFoldAndMissingMatchTheGoldens(t *testing.T) {
	logs, err := filepath.Glob(filepath.Join("..", "..", "..", "testdata", "plan", "view", "*", "log.jsonl"))
	require.NoError(t, err)
	require.NotEmpty(t, logs)
	for _, log := range logs {
		dir := filepath.Dir(log)
		p := readPolicy(t, filepath.Join(dir, "policy.yaml"))
		points, err := filepath.Glob(filepath.Join(dir, "point-*"))
		require.NoError(t, err)
		if len(points) == 0 {
			points = []string{dir}
		}
		for _, pt := range points {
			name, _ := filepath.Rel(filepath.Dir(dir), pt)
			t.Run(name, func(t *testing.T) {
				src := filepath.Join(pt, "prefix.jsonl")
				if pt == dir {
					src = log
				}
				v, err := change.Fold(readEvents(t, src))
				require.NoError(t, err, "a recorded fact is never refused")
				require.Equal(t, string(read(t, filepath.Join(pt, "view.json"))), encode(t, v))
				require.Equal(t, string(read(t, filepath.Join(pt, "missing.json"))), encode(t, change.Missing(goldenIn(v, p))))
			})
		}
	}
}

func readEvents(t *testing.T, path string) []change.Event {
	t.Helper()
	var out []change.Event
	sc := bufio.NewScanner(bytes.NewReader(read(t, path)))
	sc.Buffer(make([]byte, 0, 1<<20), 4<<20)
	for sc.Scan() {
		var e change.Event
		require.NoError(t, json.Unmarshal(sc.Bytes(), &e))
		out = append(out, e)
	}
	require.NoError(t, sc.Err())
	return out
}

func readPolicy(t *testing.T, path string) policy.Policy {
	t.Helper()
	var p policy.Policy
	dec := yaml.NewDecoder(bytes.NewReader(read(t, path)))
	dec.KnownFields(true)
	require.NoError(t, dec.Decode(&p))
	return p
}

func encode(t *testing.T, v any) string {
	t.Helper()
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	require.NoError(t, enc.Encode(v))
	return buf.String()
}

func read(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	return b
}

// goldenIn is the In the board builds for a recorded log: the policy at the merge base, the attempt budget the product
// runs with, and the pick wait of one minute.
func goldenIn(v change.View, p policy.Policy) change.In {
	var c id.ChangeID
	switch {
	case v.Trigger != nil:
		c = v.Trigger.Ticket
	case v.PR != nil:
		c = v.PR.Ticket
	}
	return change.In{Change: c, View: v, Policy: p, Settings: change.Settings{TestAttempts: 3, PickWait: time.Minute}}
}
