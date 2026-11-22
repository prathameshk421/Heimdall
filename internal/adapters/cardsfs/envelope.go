package cardsfs

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/dsse"
)

// WriteEnvelope stores a card's DSSE envelope beside it as <name>.dsse.json.
func (s *Store) WriteEnvelope(person, name string, envelope dsse.Envelope) error {
	if !safeName(person) {
		return fmt.Errorf("card owner %q is not a plain name", person)
	}
	if !safeName(name) || !isCard(name) {
		return fmt.Errorf("card name %q is not a card file", name)
	}
	if _, err := s.Read(person, name); err != nil {
		return fmt.Errorf("read card before storing envelope %q: %w", name, err)
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("encode card envelope %q: %w", name, err)
	}
	if len(data) > MaxCardBytes {
		return fmt.Errorf("card envelope %q is over %d bytes", name, MaxCardBytes)
	}
	if err := s.root.Mkdir(person, 0o750); err != nil && !errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("create cards folder of %q: %w", person, err)
	}
	sub, err := s.personRoot(person)
	if err != nil {
		return err
	}
	defer func() { _ = sub.Close() }()
	return writeEnvelopeAtomic(sub, name+".dsse.json", data)
}

func writeEnvelopeAtomic(sub *os.Root, name string, data []byte) error {
	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return fmt.Errorf("name temporary envelope: %w", err)
	}
	temp := ".tmp-envelope-" + hex.EncodeToString(suffix[:])
	f, err := sub.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
	if err != nil {
		return fmt.Errorf("create temporary envelope: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = sub.Remove(temp)
		return fmt.Errorf("write card envelope %q: %w", name, err)
	}
	if err := f.Close(); err != nil {
		_ = sub.Remove(temp)
		return fmt.Errorf("close card envelope %q: %w", name, err)
	}
	if err := sub.Rename(temp, name); err != nil {
		_ = sub.Remove(temp)
		return fmt.Errorf("store card envelope %q: %w", name, err)
	}
	return nil
}

// ReadEnvelope returns the DSSE envelope stored beside one card.
func (s *Store) ReadEnvelope(person, name string) (dsse.Envelope, error) {
	if !safeName(name) || !isCard(name) {
		return dsse.Envelope{}, fmt.Errorf("card name %q is not a card file", name)
	}
	sub, err := s.personRoot(person)
	if err != nil {
		return dsse.Envelope{}, err
	}
	defer func() { _ = sub.Close() }()
	f, err := sub.Open(name + ".dsse.json")
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return dsse.Envelope{}, ErrNotFound
		}
		return dsse.Envelope{}, fmt.Errorf("open card envelope %q: %w", name, err)
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, MaxCardBytes+1))
	if err != nil {
		return dsse.Envelope{}, fmt.Errorf("read card envelope %q: %w", name, err)
	}
	if len(data) > MaxCardBytes {
		return dsse.Envelope{}, fmt.Errorf("card envelope %q is over %d bytes", name, MaxCardBytes)
	}
	var envelope dsse.Envelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return dsse.Envelope{}, fmt.Errorf("decode card envelope %q: %w", name, err)
	}
	return envelope, nil
}
