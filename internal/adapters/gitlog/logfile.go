package gitlog

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strconv"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
)

// Limits of ACTION_MODE 2.6. A reader that meets a larger value refuses it, and never reads on.
const (
	// MaxEventBytes is the most one stored event line may hold.
	MaxEventBytes = 64 << 10
	// MaxFileBytes is the most one change file or one effects file may hold.
	MaxFileBytes = 4 << 20
	// maxEffectBytes is the most one stored effect line may hold.
	maxEffectBytes = 96 << 10
	// maxHeadBytes bounds the small head file.
	maxHeadBytes = 1 << 10
)

// macContext separates this MAC from any other use of the same key.
const macContext = "heimdall-gitlog-v1"

// headFile is the content of changes/<change>.head. Version and Hash name the last event. Effects is the SHA-256 of
// the effects file, so the outbox rows are covered too. MAC is HMAC-SHA256 over all of them and the change id.
type headFile struct {
	Version int64       `json:"version"`
	Hash    change.Hash `json:"hash"`
	Effects change.Hash `json:"effects"`
	MAC     string      `json:"mac"`
}

// effectLine is one line of effects/<change>.jsonl. EventID is the last event of the batch that decided the effect.
type effectLine struct {
	EventID change.EventID `json:"event_id"`
	Effect  change.Effect  `json:"effect"`
}

// emptyDigest is the effects digest of a change that has no effects file.
func emptyDigest() change.Hash { return sha256.Sum256(nil) }

// computeMAC returns the MAC of a head. The change id binds the head to its own file, so a valid head cannot be moved
// to another change, and the version and both digests bind the log and the effects.
func computeMAC(key []byte, c id.ChangeID, version int64, hash, effects change.Hash) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(macContext))
	m.Write([]byte{0})
	m.Write([]byte(c))
	m.Write([]byte{0})
	m.Write([]byte(strconv.FormatInt(version, 10)))
	m.Write([]byte{0})
	m.Write(hash[:])
	m.Write(effects[:])
	return m.Sum(nil)
}

// signHead builds the head file content for a change.
func signHead(key []byte, c id.ChangeID, version int64, hash, effects change.Hash) ([]byte, error) {
	h := headFile{Version: version, Hash: hash, Effects: effects,
		MAC: hex.EncodeToString(computeMAC(key, c, version, hash, effects))}
	out, err := json.Marshal(h)
	if err != nil {
		return nil, fmt.Errorf("encode head: %w", err)
	}
	return append(out, '\n'), nil
}

// parseHead reads a head file and checks its MAC in constant time. It returns an IntegrityError for any failure.
func parseHead(key []byte, c id.ChangeID, data []byte) (headFile, error) {
	var h headFile
	if err := decodeStrict(data, &h); err != nil {
		return headFile{}, integrity(c, "head file is malformed", nil)
	}
	got, err := hex.DecodeString(h.MAC)
	if err != nil || h.Version < 1 {
		return headFile{}, integrity(c, "head file is malformed", nil)
	}
	// hmac.Equal compares in constant time. A MAC of the wrong length is also a mismatch.
	if !hmac.Equal(got, computeMAC(key, c, h.Version, h.Hash, h.Effects)) {
		return headFile{}, integrity(c, "mac does not match", nil)
	}
	return h, nil
}

// decodeStrict decodes one JSON value, refuses unknown fields and refuses anything after the value.
func decodeStrict(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return errors.New("data after the value")
	}
	return nil
}

// encodeEvent writes one event as a JSONL line. A line over MaxEventBytes is refused.
func encodeEvent(e change.Event) ([]byte, error) {
	line, err := json.Marshal(e)
	if err != nil {
		return nil, fmt.Errorf("encode version %d: %w", e.Version, err)
	}
	if len(line) > MaxEventBytes {
		return nil, &LimitError{What: fmt.Sprintf("event at version %d", e.Version), Max: MaxEventBytes, Got: int64(len(line))}
	}
	return append(line, '\n'), nil
}

// forEachLine calls fn on every line of a JSONL file. A final line must end with a newline, so a cut file is caught.
// A line over maxLine is refused as a bad file.
func forEachLine(c id.ChangeID, data []byte, maxLine int, fn func([]byte) error) error {
	for len(data) > 0 {
		end := bytes.IndexByte(data, '\n')
		if end < 0 {
			return integrity(c, "file does not end with a newline", nil)
		}
		if end > maxLine {
			return integrity(c, "a line is over the size limit", nil)
		}
		if err := fn(data[:end]); err != nil {
			return integrity(c, "a line is malformed", nil)
		}
		data = data[end+1:]
	}
	return nil
}

// decodeEvents reads a change file. It does not verify the chain.
func decodeEvents(c id.ChangeID, data []byte) ([]change.Event, error) {
	var out []change.Event
	err := forEachLine(c, data, MaxEventBytes, func(line []byte) error {
		var e change.Event
		if err := decodeStrict(line, &e); err != nil {
			return err
		}
		if e.BlobRefs == nil {
			e.BlobRefs = []workflow.BlobRef{}
		}
		out = append(out, e)
		return nil
	})
	return out, err
}

// decodeEffects reads an effects file into its lines, in file order.
func decodeEffects(c id.ChangeID, data []byte) ([]effectLine, error) {
	var out []effectLine
	err := forEachLine(c, data, maxEffectBytes, func(line []byte) error {
		var l effectLine
		if err := decodeStrict(line, &l); err != nil {
			return err
		}
		out = append(out, l)
		return nil
	})
	return out, err
}

// sameEffect reports whether a and b are the same action: the same kind, target and payload value.
func sameEffect(a, b change.Effect) bool {
	if a.Kind != b.Kind || a.Target != b.Target {
		return false
	}
	var x, y any
	if json.Unmarshal(a.Payload, &x) != nil || json.Unmarshal(b.Payload, &y) != nil {
		return false
	}
	return reflect.DeepEqual(x, y)
}

// digest is the SHA-256 of a file.
func digest(data []byte) change.Hash { return sha256.Sum256(data) }
