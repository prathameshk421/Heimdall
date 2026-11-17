package announce

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
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
)

// The status goldens (testdata/plan/status/README.md): the status of each golden point is
// Render(Fold(events), Missing(view, policy)), as the board computes it live.
func TestRenderMatchesTheStatusGoldens(t *testing.T) {
	logs, err := filepath.Glob(filepath.Join("..", "..", "..", "testdata", "plan", "view", "*", "log.jsonl"))
	require.NoError(t, err)
	require.NotEmpty(t, logs)
	for _, log := range logs {
		dir := filepath.Dir(log)
		p := loadPolicy(t, filepath.Join(dir, "policy.yaml"))
		points, err := filepath.Glob(filepath.Join(dir, "point-*"))
		require.NoError(t, err)
		if len(points) == 0 {
			points = []string{dir}
		}
		for _, pt := range points {
			t.Run(pt, func(t *testing.T) {
				src := filepath.Join(pt, "prefix.jsonl")
				if pt == dir {
					src = log
				}
				v, err := change.Fold(loadEvents(t, src))
				require.NoError(t, err)
				require.Equal(t, string(read(t, filepath.Join(pt, "status.txt"))), Render(v, change.Missing(change.In{View: v, Policy: p, Settings: change.Settings{TestAttempts: 3, PickWait: time.Minute}})))
			})
		}
	}
}

func loadEvents(t *testing.T, path string) []change.Event {
	t.Helper()
	var out []change.Event
	sc := bufio.NewScanner(bytes.NewReader(read(t, path)))
	sc.Buffer(make([]byte, 0, 1<<20), 4<<20)
	for sc.Scan() {
		var e change.Event
		require.NoError(t, json.Unmarshal(sc.Bytes(), &e))
		out = append(out, e)
	}
	return out
}

func loadPolicy(t *testing.T, path string) policy.Policy {
	t.Helper()
	var p policy.Policy
	dec := yaml.NewDecoder(bytes.NewReader(read(t, path)))
	dec.KnownFields(true)
	require.NoError(t, dec.Decode(&p))
	return p
}

func read(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	return b
}
