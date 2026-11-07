package pack

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const maxDraftBytes = 4 << 20

// SaveDraft atomically stores a redacted draft in a private directory and file.
func SaveDraft(path string, draft Draft) error {
	if err := validateDraft(draft); err != nil {
		return err
	}
	dir, name, err := draftPath(path)
	if err != nil {
		return err
	}
	if err := ensurePrivateDraftDir(dir); err != nil {
		return err
	}
	encoded, err := encodeDraft(draft)
	if err != nil {
		return err
	}
	return writeDraftAtomically(dir, name, encoded)
}

func ensurePrivateDraftDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create private pack draft directory: %w", err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("stat private pack draft directory: %w", err)
	}
	if !info.IsDir() || info.Mode().Perm()&0o077 != 0 {
		return errors.New("pack draft directory must be private (0700 or stricter)")
	}
	return nil
}

func encodeDraft(draft Draft) ([]byte, error) {
	encoded, err := json.Marshal(draft)
	if err != nil {
		return nil, fmt.Errorf("encode pack draft: %w", err)
	}
	if len(encoded) > maxDraftBytes {
		return nil, fmt.Errorf("pack draft exceeds %d bytes", maxDraftBytes)
	}
	return encoded, nil
}

// writeDraftAtomically writes encoded to a private temporary file in dir and renames it over name.
func writeDraftAtomically(dir, name string, encoded []byte) error {
	var suffix [12]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return fmt.Errorf("create pack draft temporary name: %w", err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return fmt.Errorf("open pack draft directory: %w", err)
	}
	defer func() { _ = root.Close() }()
	temp := ".pack-draft-" + hex.EncodeToString(suffix[:]) + ".tmp"
	file, err := root.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create pack draft temporary file: %w", err)
	}
	writeErr := writeAll(file, encoded)
	syncErr := file.Sync()
	closeErr := file.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		_ = root.Remove(temp)
		return fmt.Errorf("write pack draft: %w", err)
	}
	if err := root.Rename(temp, name); err != nil {
		_ = root.Remove(temp)
		return fmt.Errorf("publish pack draft: %w", err)
	}
	return nil
}

// LoadDraft reads a private, bounded redacted draft for owner review.
func LoadDraft(path string) (Draft, error) {
	dir, name, err := draftPath(path)
	if err != nil {
		return Draft{}, err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return Draft{}, fmt.Errorf("open pack draft directory: %w", err)
	}
	defer func() { _ = root.Close() }()
	file, err := root.Open(name)
	if err != nil {
		return Draft{}, fmt.Errorf("open pack draft: %w", err)
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return Draft{}, fmt.Errorf("stat pack draft: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return Draft{}, errors.New("pack draft permissions require a private regular file (0600 or stricter)")
	}
	encoded, err := io.ReadAll(io.LimitReader(file, maxDraftBytes+1))
	if err != nil {
		return Draft{}, fmt.Errorf("read pack draft: %w", err)
	}
	if len(encoded) > maxDraftBytes {
		return Draft{}, fmt.Errorf("pack draft exceeds %d bytes", maxDraftBytes)
	}
	var draft Draft
	decoder := json.NewDecoder(strings.NewReader(string(encoded)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&draft); err != nil {
		return Draft{}, fmt.Errorf("decode pack draft: %w", err)
	}
	if err := validateDraft(draft); err != nil {
		return Draft{}, err
	}
	return draft, nil
}

func draftPath(path string) (string, string, error) {
	clean := filepath.Clean(path)
	if clean == "." || filepath.Base(clean) == "." || filepath.Base(clean) == string(filepath.Separator) {
		return "", "", errors.New("pack draft path is invalid")
	}
	return filepath.Dir(clean), filepath.Base(clean), nil
}

func validateDraft(draft Draft) error {
	if draft.Person == "" || draft.Version < 1 || draft.CreatedAt.IsZero() || draft.Canary == "" || len(draft.Sections) == 0 {
		return errors.New("pack draft identity or sections are incomplete")
	}
	for i, section := range draft.Sections {
		if section.Name == "" || section.Content == "" {
			return fmt.Errorf("pack draft section %d is incomplete", i)
		}
	}
	for i, hit := range draft.Removed {
		if hit.Kind == "" || hit.Count < 1 {
			return fmt.Errorf("pack draft redaction summary %d is invalid", i)
		}
	}
	return nil
}

func writeAll(w io.Writer, content []byte) error {
	for len(content) > 0 {
		n, err := w.Write(content)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		content = content[n:]
	}
	return nil
}
