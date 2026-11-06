package pack

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	"filippo.io/age"
)

// AgeCipher encrypts packs to the configured VM recipient and decrypts with that server identity.
// The raw recipient and identity are supplied from secret/config adapters and are never logged.
type AgeCipher struct {
	Recipient string
	Identity  string
}

// Encrypt returns age ciphertext; malformed recipients and write errors fail closed.
func (c AgeCipher) Encrypt(ctx context.Context, plaintext []byte) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c.Recipient == "" {
		return nil, errors.New("age recipient is empty")
	}
	recipient, err := age.ParseX25519Recipient(c.Recipient)
	if err != nil {
		return nil, errors.New("parse age recipient: invalid recipient")
	}
	var out bytes.Buffer
	w, err := age.Encrypt(&out, recipient)
	if err != nil {
		return nil, fmt.Errorf("start age encryption: %w", err)
	}
	if _, err := w.Write(plaintext); err != nil {
		return nil, fmt.Errorf("write age plaintext: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("finish age encryption: %w", err)
	}
	return out.Bytes(), nil
}

// Decrypt authenticates age ciphertext before returning any plaintext.
func (c AgeCipher) Decrypt(ctx context.Context, ciphertext []byte) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c.Identity == "" {
		return nil, errors.New("age identity is empty")
	}
	identity, err := age.ParseX25519Identity(c.Identity)
	if err != nil {
		return nil, errors.New("parse age identity: invalid identity")
	}
	r, err := age.Decrypt(bytes.NewReader(ciphertext), identity)
	if err != nil {
		return nil, fmt.Errorf("open age ciphertext: %w", err)
	}
	plaintext, err := io.ReadAll(contextReader{ctx: ctx, reader: r})
	if err != nil {
		return nil, fmt.Errorf("read age plaintext: %w", err)
	}
	return plaintext, nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}
