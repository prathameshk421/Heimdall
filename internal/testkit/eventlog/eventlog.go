package eventlog

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
)

// Line is one line of a script: what happened, when and by whom.
type Line struct {
	At      string          `json:"at"`
	Actor   string          `json:"actor"`
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// ReadScript decodes a JSON Lines script. Blank lines are skipped; unknown fields are refused.
func ReadScript(r io.Reader) ([]Line, error) {
	var out []Line
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	n := 0
	for sc.Scan() {
		n++
		text := strings.TrimSpace(sc.Text())
		if text == "" {
			continue
		}
		dec := json.NewDecoder(strings.NewReader(text))
		dec.DisallowUnknownFields()
		var l Line
		if err := dec.Decode(&l); err != nil {
			return nil, fmt.Errorf("script line %d: %w", n, err)
		}
		out = append(out, l)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read script: %w", err)
	}
	return out, nil
}

// Build turns the script into a chained event log for one change.
func Build(changeID id.ChangeID, lines []Line) ([]change.Event, error) {
	events := make([]change.Event, 0, len(lines))
	var prev change.Hash
	for i, l := range lines {
		e, err := envelope(changeID, int64(i+1), l)
		if err != nil {
			return nil, fmt.Errorf("script line %d: %w", i+1, err)
		}
		if i > 0 {
			p := prev
			e.PrevHash = &p
		}
		h, err := change.Chain(prev, e)
		if err != nil {
			return nil, fmt.Errorf("script line %d: chain: %w", i+1, err)
		}
		e.Hash = h
		prev = h
		if err := e.Validate(); err != nil {
			return nil, fmt.Errorf("script line %d (%s): %w", i+1, l.Type, err)
		}
		events = append(events, e)
	}
	return events, nil
}

func envelope(changeID id.ChangeID, version int64, l Line) (change.Event, error) {
	at, err := time.Parse(time.RFC3339, l.At)
	if err != nil {
		return change.Event{}, fmt.Errorf("at: %w", err)
	}
	kind, who, ok := strings.Cut(l.Actor, ":")
	if !ok {
		return change.Event{}, fmt.Errorf("actor %q is not kind:id", l.Actor)
	}
	var payload bytes.Buffer
	if err := json.Compact(&payload, l.Payload); err != nil {
		return change.Event{}, fmt.Errorf("payload: %w", err)
	}
	return change.Event{
		ID:            eventID(changeID, version),
		ChangeID:      changeID,
		Version:       version,
		Type:          change.EventType(l.Type),
		Actor:         change.Actor{Kind: change.ActorKind(kind), ID: change.ActorID(who)},
		OccurredAt:    at.UTC(),
		CorrelationID: change.CorrelationID("corr-" + string(changeID)),
		Payload:       json.RawMessage(payload.Bytes()),
		BlobRefs:      []workflow.BlobRef{},
	}, nil
}

// eventID is a UUID-shaped digest of the change and version, so a rebuilt log has the same ids.
func eventID(changeID id.ChangeID, version int64) change.EventID {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s/%d", changeID, version)))
	h := hex.EncodeToString(sum[:16])
	return change.EventID(h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32])
}

// Encode writes one compact JSON object per event, each followed by a newline, with no HTML escaping.
func Encode(w io.Writer, events []change.Event) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	for _, e := range events {
		if err := enc.Encode(e); err != nil {
			return fmt.Errorf("encode version %d: %w", e.Version, err)
		}
	}
	return nil
}
