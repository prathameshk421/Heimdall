package pack

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"testing"
	"time"

	"filippo.io/age"
	"github.com/stretchr/testify/require"
)

func TestRedactRemovesCredentialAndPIIWithoutKeepingValues(t *testing.T) {
	input := "token=AKIA1234567890ABCDEF email alice@example.com Authorization: Bearer abcdefghijklmnop"
	got, hits := Redact(input)
	require.NotContains(t, got, "AKIA1234567890ABCDEF")
	require.NotContains(t, got, "alice@example.com")
	require.NotContains(t, got, "abcdefghijklmnop")
	require.Contains(t, got, "[REDACTED:cloud_key]")
	require.Len(t, hits, 3)
	for _, hit := range hits {
		require.NotContains(t, hit.Kind, "alice")
	}
}

func TestEmbeddedGitleaksRulesRedactPlantedAWSKey(t *testing.T) {
	scanner, err := NewSecretScanner()
	require.NoError(t, err)
	redacted, hits, err := scanner.Scan("AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE")
	require.NoError(t, err)
	require.NotContains(t, redacted, "AKIAIOSFODNN7EXAMPLE")
	require.NotEmpty(t, hits)
}

func TestScannerRedactsLuhnCardAndOrganizationDenials(t *testing.T) {
	scanner, err := NewSecretScannerWithConfig(ScannerConfig{InternalHostnames: []string{"git.internal.example"}, DenyTerms: []string{"Project Nightingale"}})
	require.NoError(t, err)
	got, hits, err := scanner.Scan("card 4111 1111 1111 1111 host git.internal.example project nightingale")
	require.NoError(t, err)
	require.NotContains(t, got, "4111 1111 1111 1111")
	require.NotContains(t, got, "git.internal.example")
	require.NotContains(t, got, "Nightingale")
	require.Contains(t, hits, Redaction{Kind: "payment_card", Count: 1})
}

func FuzzRedactNeverReturnsMatchedCredential(f *testing.F) {
	f.Add("token=AKIA1234567890ABCDEF")
	f.Add("mail user@example.com")
	f.Fuzz(func(t *testing.T, input string) {
		if len(input) > maxSourceBytes {
			return
		}
		redacted, _ := Redact(input)
		require.NotContains(t, redacted, "AKIA1234567890ABCDEF")
		require.NotContains(t, redacted, "user@example.com")
	})
}

func TestAgeCipherEncryptsForOnlyTheConfiguredIdentity(t *testing.T) {
	identity, err := age.GenerateX25519Identity()
	require.NoError(t, err)
	cipher := AgeCipher{Recipient: identity.Recipient().String(), Identity: identity.String()}
	ciphertext, err := cipher.Encrypt(context.Background(), []byte("private context"))
	require.NoError(t, err)
	require.NotContains(t, string(ciphertext), "private context")
	plaintext, err := cipher.Decrypt(context.Background(), ciphertext)
	require.NoError(t, err)
	require.Equal(t, "private context", string(plaintext))
	other, err := age.GenerateX25519Identity()
	require.NoError(t, err)
	_, err = (AgeCipher{Identity: other.String()}).Decrypt(context.Background(), ciphertext)
	require.Error(t, err)
}

func TestSecondPassBlocksAnySecretIntroducedByDistiller(t *testing.T) {
	key := make(ed25519.PrivateKey, ed25519.PrivateKeySize)
	_, err := rand.Read(key)
	require.NoError(t, err)
	upload := &uploadSpy{}
	err = Build(context.Background(), BuildInput{Person: "bob", Version: 1, CreatedAt: time.Now(), Canary: "CANARY-abc", PrivateKey: key,
		Documents: []SourceDocument{{Provenance: "[git:abc123]", Raw: "normal text"}}}, BuildDeps{
		Distiller: distillerFunc(func(context.Context, []SourceDocument) ([]Section, error) {
			return []Section{{Name: "invariants.md", Content: "leaked alice@example.com"}}, nil
		}),
		Scanner: scannerFunc{},
		Review:  reviewFunc(func(context.Context, Manifest, []Section, []Redaction) (bool, error) { return true, nil }),
		Cipher:  cipherFunc{}, Upload: upload,
	})
	require.ErrorContains(t, err, "redact pack section")
	require.False(t, upload.called, "second-pass findings must block upload")
}

func TestBuildRequiresApprovalBeforeEncryptionAndUpload(t *testing.T) {
	key := make(ed25519.PrivateKey, ed25519.PrivateKeySize)
	_, err := rand.Read(key)
	require.NoError(t, err)
	cipher := &cipherSpy{}
	upload := &uploadSpy{}
	err = Build(context.Background(), BuildInput{Person: "bob", Version: 1, CreatedAt: time.Now(), Canary: "CANARY-abc", PrivateKey: key,
		Documents: []SourceDocument{{Provenance: "[git:abc123]", Raw: "normal text"}}}, BuildDeps{
		Distiller: distillerFunc(func(_ context.Context, docs []SourceDocument) ([]Section, error) {
			require.Contains(t, docs[0].Raw, "normal text")
			return []Section{{Name: "invariants.md", Content: "safe"}}, nil
		}),
		Scanner: scannerFunc{},
		Review:  reviewFunc(func(context.Context, Manifest, []Section, []Redaction) (bool, error) { return false, nil }), Cipher: cipher, Upload: upload,
	})
	require.ErrorContains(t, err, "owner approval")
	require.False(t, cipher.called)
	require.False(t, upload.called)
}

func TestReadPackIsCallerScopedFreshSignedAndCanaryFiltered(t *testing.T) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	created := time.Now().UTC()
	manifest, payload, err := signPack(BuildInput{Person: "alice", Version: 1, CreatedAt: created, Canary: "secret-canary", PrivateKey: private}, []Section{{Name: "invariants.md", Content: insertCanary("Safe pattern", "secret-canary")}})
	require.NoError(t, err)
	store := storeSpy{value: StoredPack{Owner: "alice", Ciphertext: payload, Manifest: manifest}}
	got, err := Read(context.Background(), "alice", created.Add(time.Hour), 14, public, store, cipherFunc{}, "invariants.md")
	require.NoError(t, err)
	require.Contains(t, got, "Safe pattern")
	require.NotContains(t, got, "secret-canary")
	_, err = Read(context.Background(), "bob", created.Add(time.Hour), 14, public, store, cipherFunc{}, "invariants.md")
	require.ErrorContains(t, err, "another reviewer")
	_, err = Read(context.Background(), "alice", created.Add(15*24*time.Hour), 14, public, store, cipherFunc{}, "")
	require.ErrorContains(t, err, "stale")
}

func TestReadPackDetectsTamperedSection(t *testing.T) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	created := time.Now().UTC()
	manifest, payload, err := signPack(BuildInput{Person: "alice", Version: 1, CreatedAt: created, Canary: "c", PrivateKey: private}, []Section{{Name: "x.md", Content: "safe"}})
	require.NoError(t, err)
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(payload, &decoded))
	sections := decoded["sections"].([]any)
	sections[0].(map[string]any)["content"] = "tampered"
	payload, _ = json.Marshal(decoded)
	_, err = Read(context.Background(), "alice", created, 14, public, storeSpy{StoredPack{"alice", payload, manifest}}, cipherFunc{}, "")
	require.ErrorContains(t, err, "hash does not match")
}

type distillerFunc func(context.Context, []SourceDocument) ([]Section, error)

func (f distillerFunc) Distill(ctx context.Context, d []SourceDocument) ([]Section, error) {
	return f(ctx, d)
}

type scannerFunc struct{}

func (scannerFunc) Scan(s string) (string, []Redaction, error) {
	text, hits := Redact(s)
	return text, hits, nil
}

type reviewFunc func(context.Context, Manifest, []Section, []Redaction) (bool, error)

func (f reviewFunc) Approve(ctx context.Context, m Manifest, s []Section, r []Redaction) (bool, error) {
	return f(ctx, m, s, r)
}

type cipherFunc struct{}

func (cipherFunc) Encrypt(_ context.Context, b []byte) ([]byte, error) { return b, nil }
func (cipherFunc) Decrypt(_ context.Context, b []byte) ([]byte, error) { return b, nil }

type uploadSpy struct{ called bool }

func (s *uploadSpy) Upload(context.Context, string, []byte, Manifest) error {
	s.called = true
	return nil
}

type cipherSpy struct{ called bool }

func (s *cipherSpy) Encrypt(_ context.Context, b []byte) ([]byte, error) {
	s.called = true
	return b, nil
}
func (s *cipherSpy) Decrypt(_ context.Context, b []byte) ([]byte, error) { return b, nil }

type storeSpy struct{ value StoredPack }

func (s storeSpy) GetPack(_ context.Context, owner string) (StoredPack, error) {
	_ = owner
	return s.value, nil
}
