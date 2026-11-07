package cardsfs

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// MaxCardBytes is the largest card the store returns.
const MaxCardBytes = 64 * 1024

// ErrNotFound is returned for a card that does not exist for the person.
var ErrNotFound = errors.New("card not found")

// Store reads cards beneath one directory.
type Store struct{ root *os.Root }

// Open opens the cards directory, creating it when missing.
func Open(dir string) (*Store, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, errors.New("open cards: directory is empty")
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("create cards directory: %w", err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("open cards directory: %w", err)
	}
	return &Store{root: root}, nil
}

// Close releases the directory handle.
func (s *Store) Close() error {
	if err := s.root.Close(); err != nil {
		return fmt.Errorf("close cards: %w", err)
	}
	return nil
}

// safeName reports whether name is one plain path element: no separators, no dots-only, no NUL.
func safeName(name string) bool {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\\x00") {
		return false
	}
	return filepath.Base(name) == name
}

func isCard(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".md", ".yaml", ".yml", ".txt":
		return true
	default:
		return false
	}
}

func (s *Store) personRoot(person string) (*os.Root, error) {
	if !safeName(person) {
		return nil, fmt.Errorf("card owner %q is not a plain name", person)
	}
	sub, err := s.root.OpenRoot(person)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("open cards of %q: %w", person, err)
	}
	return sub, nil
}

// List returns the sorted card file names of person. A person with no folder has no cards.
func (s *Store) List(person string) ([]string, error) {
	sub, err := s.personRoot(person)
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = sub.Close() }()
	dir, err := sub.Open(".")
	if err != nil {
		return nil, fmt.Errorf("list cards of %q: %w", person, err)
	}
	defer func() { _ = dir.Close() }()
	entries, err := dir.ReadDir(-1)
	if err != nil {
		return nil, fmt.Errorf("list cards of %q: %w", person, err)
	}
	var names []string
	for _, e := range entries {
		if e.Type().IsRegular() && isCard(e.Name()) {
			names = append(names, e.Name())
		}
	}
	slices.Sort(names)
	return names, nil
}

// Read returns one card of person.
func (s *Store) Read(person, name string) (string, error) {
	if !safeName(name) || !isCard(name) {
		return "", fmt.Errorf("card name %q is not a card file", name)
	}
	sub, err := s.personRoot(person)
	if err != nil {
		return "", err
	}
	defer func() { _ = sub.Close() }()
	f, err := sub.Open(name)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("open card %q: %w", name, err)
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, MaxCardBytes+1))
	if err != nil {
		return "", fmt.Errorf("read card %q: %w", name, err)
	}
	if len(data) > MaxCardBytes {
		return "", fmt.Errorf("card %q is over %d bytes", name, MaxCardBytes)
	}
	return string(data), nil
}

// Write stores one card of person, creating the person's folder when needed. The content goes to a temporary file
// in that folder and is renamed over the card, so a reader never sees half a card, and a symlink at the card's name
// is replaced rather than followed.
func (s *Store) validateWrite(person, name string, content []byte) error {
	if !safeName(name) || !isCard(name) {
		return fmt.Errorf("card name %q is not a card file", name)
	}
	if !safeName(person) {
		return fmt.Errorf("card owner %q is not a plain name", person)
	}
	if len(content) > MaxCardBytes {
		return fmt.Errorf("card %q is over %d bytes", name, MaxCardBytes)
	}
	if err := s.root.Mkdir(person, 0o750); err != nil && !errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("create cards folder of %q: %w", person, err)
	}
	return nil
}

func (s *Store) Write(person, name string, content []byte) error {
	if err := s.validateWrite(person, name, content); err != nil {
		return err
	}
	sub, err := s.personRoot(person)
	if err != nil {
		return err
	}
	defer func() { _ = sub.Close() }()
	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return fmt.Errorf("name temporary card: %w", err)
	}
	temp := ".tmp-" + hex.EncodeToString(suffix[:])
	f, err := sub.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
	if err != nil {
		return fmt.Errorf("create temporary card: %w", err)
	}
	if _, err := f.Write(content); err != nil {
		_ = f.Close()
		_ = sub.Remove(temp)
		return fmt.Errorf("write card %q: %w", name, err)
	}
	if err := f.Close(); err != nil {
		_ = sub.Remove(temp)
		return fmt.Errorf("close card %q: %w", name, err)
	}
	if err := sub.Rename(temp, name); err != nil {
		_ = sub.Remove(temp)
		return fmt.Errorf("store card %q: %w", name, err)
	}
	return nil
}

// Delete removes one card of person. A card that is not there is ErrNotFound.
func (s *Store) Delete(person, name string) error {
	if !safeName(name) || !isCard(name) {
		return fmt.Errorf("card name %q is not a card file", name)
	}
	sub, err := s.personRoot(person)
	if err != nil {
		return err
	}
	defer func() { _ = sub.Close() }()
	if err := sub.Remove(name); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return ErrNotFound
		}
		return fmt.Errorf("delete card %q: %w", name, err)
	}
	return nil
}
