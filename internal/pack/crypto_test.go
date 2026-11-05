package pack

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"testing"
)

func TestSignedManifestDetectsMutation(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	m := Manifest{Version: 1, Owner: "alice", Files: map[string]string{"notes.md": "abc"}}
	sig, err := SignManifest(m, priv)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyManifest(m, pub, sig); err != nil {
		t.Fatalf("verify: %v", err)
	}
	m.Files["notes.md"] = "changed"
	if err := VerifyManifest(m, pub, sig); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("mutation accepted: %v", err)
	}
}

func TestManifestKeyAndSignatureRejection(t *testing.T) {
	m := Manifest{Version: 1, Owner: "alice"}
	if _, err := SignManifest(m, make([]byte, 3)); !errors.Is(err, ErrInvalidSigningKey) {
		t.Fatalf("bad private key: %v", err)
	}
	if err := VerifyManifest(m, make([]byte, 3), []byte("x")); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("bad verification key: %v", err)
	}
}

func TestAgeRoundTripAndAuthentication(t *testing.T) {
	id, err := NewAgeIdentity()
	if err != nil {
		t.Fatal(err)
	}
	plain := []byte("private pack text")
	cipher, err := EncryptPack(context.Background(), bytes.NewReader(plain), id.Recipient(), 1024)
	if err != nil {
		t.Fatal(err)
	}
	out, err := DecryptPack(context.Background(), bytes.NewReader(cipher), id, 20)
	if err != nil || !bytes.Equal(out, plain) {
		t.Fatalf("round trip: %q, %v", out, err)
	}
	cipher[len(cipher)-1] ^= 1
	if _, err := DecryptPack(context.Background(), bytes.NewReader(cipher), id, 20); err == nil {
		t.Fatal("tampered ciphertext accepted")
	}
}

func TestAgeRejectsContextRecipientAndOversize(t *testing.T) {
	id, err := NewAgeIdentity()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := EncryptPack(context.Background(), bytes.NewReader([]byte("x")), id.Recipient(), 0); !errors.Is(err, ErrInvalidPackCryptoInput) {
		t.Fatalf("nonpositive size limit: %v", err)
	}
	if _, err := EncryptPack(context.Background(), bytes.NewReader([]byte("x")), nil, 10); !errors.Is(err, ErrInvalidPackCryptoInput) {
		t.Fatalf("nil recipient: %v", err)
	}
	if _, err := EncryptPack(context.Background(), bytes.NewReader([]byte("too long")), id.Recipient(), 2); !errors.Is(err, ErrPackTooLarge) {
		t.Fatalf("oversize plaintext: %v", err)
	}
	cipher, err := EncryptPack(context.Background(), bytes.NewReader([]byte("xx")), id.Recipient(), 32)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecryptPack(context.Background(), bytes.NewReader(cipher), id, 1); !errors.Is(err, ErrPackTooLarge) {
		t.Fatalf("oversize decrypted: %v", err)
	}
}
