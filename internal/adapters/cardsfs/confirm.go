package cardsfs

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"strings"
)

// confirmSuffix names the file that holds a confirmation. It sits beside the card, and it is not a card file, so the
// card upload cannot write it and a card list does not show it.
const confirmSuffix = ".confirmed"

// Confirm records that the person confirmed the card as it reads now. The record holds a hash of the text, so any later
// edit of the card ends the confirmation. A caller decides whether the card may be confirmed.
func (s *Store) Confirm(person, name string) error {
	text, err := s.Read(person, name)
	if err != nil {
		return err
	}
	sub, err := s.personRoot(person)
	if err != nil {
		return err
	}
	defer func() { _ = sub.Close() }()
	if err := sub.WriteFile(name+confirmSuffix, []byte(textHash(text)+"\n"), 0o640); err != nil {
		return fmt.Errorf("record confirmation of card %q: %w", name, err)
	}
	return nil
}

// Unconfirm ends a confirmation. A card with no confirmation is left as it is.
func (s *Store) Unconfirm(person, name string) error {
	if !safeName(name) || !isCard(name) {
		return fmt.Errorf("card name %q is not a card file", name)
	}
	sub, err := s.personRoot(person)
	if err != nil {
		return err
	}
	defer func() { _ = sub.Close() }()
	if err := sub.Remove(name + confirmSuffix); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("end confirmation of card %q: %w", name, err)
	}
	return nil
}

// Confirmed reports whether the person confirmed the card exactly as it reads now. Anything else is false, so a read
// problem can only take force away from a card.
func (s *Store) Confirmed(person, name string) bool {
	text, err := s.Read(person, name)
	if err != nil {
		return false
	}
	sub, err := s.personRoot(person)
	if err != nil {
		return false
	}
	defer func() { _ = sub.Close() }()
	if _, err := sub.Stat(name + confirmSuffix); errors.Is(err, fs.ErrNotExist) {
		return false
	}
	recorded, err := sub.ReadFile(name + confirmSuffix)
	return err == nil && strings.TrimSpace(string(recorded)) == textHash(text)
}

func textHash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}
