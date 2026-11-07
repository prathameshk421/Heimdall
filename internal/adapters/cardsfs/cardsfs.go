package cardsfs

import (
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
