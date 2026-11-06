package pack

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ParseSigningPublicKey decodes the 32-byte hex Ed25519 public key stored in the configured secret file.
func ParseSigningPublicKey(value string) (ed25519.PublicKey, error) {
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != ed25519.PublicKeySize {
		return nil, errors.New("signing public key must be a 32-byte hex Ed25519 key")
	}
	return ed25519.PublicKey(decoded), nil
}

// StoredPack is metadata and ciphertext returned by the blob adapter.
type StoredPack struct {
	Owner      string
	Ciphertext []byte
	Manifest   Manifest
}

// ReaderStore resolves only an encrypted pack and its metadata by owner.
type ReaderStore interface {
	GetPack(context.Context, string) (StoredPack, error)
}

// VerifyEncryptedPack checks the manifest signature, decrypts the ciphertext and validates every section hash.
func VerifyEncryptedPack(ctx context.Context, stored StoredPack, publicKey ed25519.PublicKey, cipher Cipher) error {
	if _, err := verifiedPayload(ctx, stored, publicKey, cipher); err != nil {
		return err
	}
	return nil
}

// Read returns verified sections for the caller's own fresh pack. Caller identity comes from the authenticated
// principal at the transport boundary and is never accepted from tool arguments.
func Read(ctx context.Context, caller string, now time.Time, maxAgeDays int, publicKey ed25519.PublicKey, store ReaderStore, cipher Cipher, section string) (string, error) {
	if caller == "" || maxAgeDays < 1 || len(publicKey) != ed25519.PublicKeySize || store == nil || cipher == nil {
		return "", errors.New("pack reader configuration is incomplete")
	}
	stored, err := store.GetPack(ctx, caller)
	if err != nil {
		return "", fmt.Errorf("load caller pack: %w", err)
	}
	if stored.Owner != caller || stored.Manifest.Person != caller {
		return "", errors.New("pack belongs to another reviewer")
	}
	if now.Before(stored.Manifest.CreatedAt) || now.Sub(stored.Manifest.CreatedAt) > time.Duration(maxAgeDays)*24*time.Hour {
		return "", errors.New("reviewer pack is stale")
	}
	payload, err := verifiedPayload(ctx, stored, publicKey, cipher)
	if err != nil {
		return "", err
	}
	return selectSection(payload.Sections, stored.Manifest.Canary, section)
}

func verifiedPayload(ctx context.Context, stored StoredPack, key ed25519.PublicKey, cipher Cipher) (struct {
	Manifest Manifest  `json:"manifest"`
	Sections []Section `json:"sections"`
}, error) {
	if stored.Owner != stored.Manifest.Person {
		return struct {
			Manifest Manifest  `json:"manifest"`
			Sections []Section `json:"sections"`
		}{}, errors.New("pack belongs to another reviewer")
	}
	if err := verifyManifest(stored.Manifest, key); err != nil {
		return struct {
			Manifest Manifest  `json:"manifest"`
			Sections []Section `json:"sections"`
		}{}, err
	}
	plain, err := cipher.Decrypt(ctx, stored.Ciphertext)
	if err != nil {
		return struct {
			Manifest Manifest  `json:"manifest"`
			Sections []Section `json:"sections"`
		}{}, fmt.Errorf("decrypt caller pack: %w", err)
	}
	var payload struct {
		Manifest Manifest  `json:"manifest"`
		Sections []Section `json:"sections"`
	}
	if err := json.Unmarshal(plain, &payload); err != nil {
		return payload, fmt.Errorf("decode caller pack: %w", err)
	}
	if payload.Manifest.Signature != stored.Manifest.Signature {
		return payload, errors.New("encrypted pack manifest does not match metadata")
	}
	for _, item := range payload.Sections {
		sum := sha256.Sum256([]byte(item.Content))
		if stored.Manifest.FileHashes[item.Name] != hex.EncodeToString(sum[:]) {
			return payload, fmt.Errorf("pack section %q hash does not match", item.Name)
		}
	}
	return payload, nil
}

func selectSection(sections []Section, canary, name string) (string, error) {
	if name == "" {
		var result strings.Builder
		for _, item := range sections {
			result.WriteString("## " + item.Name + "\n\n" + item.Content + "\n")
		}
		if result.Len() == 0 {
			return "", errors.New("reviewer pack has no sections")
		}
		return strings.ReplaceAll(result.String(), canary, "[PRIVATE PACK CANARY]"), nil
	}
	for _, item := range sections {
		if name == item.Name {
			return strings.ReplaceAll(item.Content, canary, "[PRIVATE PACK CANARY]"), nil
		}
	}
	return "", fmt.Errorf("pack section %q was not found", name)
}

func verifyManifest(manifest Manifest, key ed25519.PublicKey) error {
	signature, err := hex.DecodeString(manifest.Signature)
	if err != nil {
		return fmt.Errorf("decode pack signature: %w", err)
	}
	provided := manifest.Signature
	manifest.Signature = ""
	content, err := json.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("encode pack manifest: %w", err)
	}
	if !ed25519.Verify(key, content, signature) {
		return errors.New("pack signature is invalid: " + provided[:min(len(provided), 8)])
	}
	return nil
}
