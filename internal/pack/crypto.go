package pack

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"filippo.io/age"
)

var (
	// ErrInvalidSigningKey indicates that a manifest signing key has the wrong size.
	ErrInvalidSigningKey = errors.New("pack: invalid signing key")
	// ErrInvalidSignature indicates that a manifest signature is invalid.
	ErrInvalidSignature = errors.New("pack: invalid manifest signature")
	// ErrInvalidPackCryptoInput indicates that a crypto operation received invalid input.
	ErrInvalidPackCryptoInput = errors.New("pack: invalid crypto input")
	// ErrPackTooLarge indicates that pack data exceeds its configured size limit.
	ErrPackTooLarge = errors.New("pack: size limit exceeded")
)

// SignedManifest fields are structs/sorted JSON maps, giving encoding/json canonical bytes.
type SignedManifest struct {
	Version   int               `json:"version"`
	Owner     string            `json:"owner"`
	CreatedAt string            `json:"created_at,omitempty"`
	Files     map[string]string `json:"files"`
}

// CanonicalManifest returns the JSON encoding used for manifest signatures and hashes.
func CanonicalManifest(m SignedManifest) ([]byte, error) {
	b, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("canonicalize pack manifest: %w", err)
	}
	return b, nil
}

// ManifestHash returns the SHA-256 digest of a canonical manifest.
func ManifestHash(m SignedManifest) (string, error) {
	b, err := CanonicalManifest(m)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}

// SignManifest signs a canonical manifest with an Ed25519 private key.
func SignManifest(m SignedManifest, key ed25519.PrivateKey) ([]byte, error) {
	if len(key) != ed25519.PrivateKeySize {
		return nil, ErrInvalidSigningKey
	}
	b, err := CanonicalManifest(m)
	if err != nil {
		return nil, err
	}
	return ed25519.Sign(key, b), nil
}

// VerifyManifest verifies the signature of a canonical manifest.
func VerifyManifest(m SignedManifest, key ed25519.PublicKey, sig []byte) error {
	if len(key) != ed25519.PublicKeySize || len(sig) != ed25519.SignatureSize {
		return ErrInvalidSignature
	}
	b, err := CanonicalManifest(m)
	if err != nil {
		return err
	}
	if !ed25519.Verify(key, b, sig) {
		return ErrInvalidSignature
	}
	return nil
}

// AgeIdentity holds an age X25519 identity for pack encryption and decryption.
type AgeIdentity struct{ identity *age.X25519Identity }

// NewAgeIdentity generates a new age X25519 identity.
func NewAgeIdentity() (*AgeIdentity, error) {
	id, err := age.GenerateX25519Identity()
	if err != nil {
		return nil, fmt.Errorf("generate age identity: %w", err)
	}
	return &AgeIdentity{identity: id}, nil
}

// ParseAgeIdentity parses an age X25519 identity string.
func ParseAgeIdentity(s string) (*AgeIdentity, error) {
	id, err := age.ParseX25519Identity(s)
	if err != nil {
		return nil, fmt.Errorf("parse age identity: %w", err)
	}
	return &AgeIdentity{identity: id}, nil
}

// Recipient returns the age recipient corresponding to the identity.
func (i *AgeIdentity) Recipient() *age.X25519Recipient {
	if i == nil || i.identity == nil {
		return nil
	}
	return i.identity.Recipient()
}

// EncryptPack reads and encrypts at most maxBytes of pack data.
func EncryptPack(ctx context.Context, src io.Reader, recipient *age.X25519Recipient, maxBytes int64) ([]byte, error) {
	if ctx == nil || src == nil || recipient == nil || maxBytes <= 0 {
		return nil, ErrInvalidPackCryptoInput
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("encrypt pack context: %w", err)
	}
	plain, err := readBounded(ctx, src, maxBytes)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	w, err := age.Encrypt(&out, recipient)
	if err != nil {
		return nil, fmt.Errorf("encrypt pack: %w", err)
	}
	if _, err = w.Write(plain); err != nil {
		return nil, fmt.Errorf("encrypt pack: %w", err)
	}
	if err = w.Close(); err != nil {
		return nil, fmt.Errorf("finish pack encryption: %w", err)
	}
	return out.Bytes(), nil
}

// DecryptPack decrypts pack data, limiting both ciphertext and plaintext sizes.
func DecryptPack(ctx context.Context, src io.Reader, identity *AgeIdentity, maxBytes int64) ([]byte, error) {
	if ctx == nil || src == nil || identity == nil || identity.identity == nil || maxBytes <= 0 {
		return nil, ErrInvalidPackCryptoInput
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("decrypt pack context: %w", err)
	}
	ciphertext, err := readBounded(ctx, src, maxBytes+(1<<20))
	if err != nil {
		return nil, err
	}
	r, err := age.Decrypt(bytes.NewReader(ciphertext), identity.identity)
	if err != nil {
		return nil, fmt.Errorf("decrypt pack: %w", err)
	}
	return readBounded(ctx, r, maxBytes)
}
func readBounded(ctx context.Context, r io.Reader, maxBytes int64) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read pack: %w", err)
	}
	if int64(len(b)) > maxBytes {
		return nil, ErrPackTooLarge
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("read pack context: %w", err)
	}
	return b, nil
}
