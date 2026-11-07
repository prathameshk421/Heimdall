//go:build e2e

package pack

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"filippo.io/age"
	"github.com/stretchr/testify/require"
)

// TestTask_T48 runs the pack product seam from local source collection through approval,
// real age encryption, ciphertext-only upload, and caller-bound verified readback.
func TestTask_T48(t *testing.T) {
	ctx := context.Background()
	repo := t.TempDir()
	docs := filepath.Join(repo, "docs")
	require.NoError(t, os.Mkdir(docs, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(docs, "guide.md"), []byte("Use idempotency keys. Contact maintainer@example.invalid.\n"), 0o600))
	documents, err := CollectMarkdown(repo, []string{"docs/guide.md"})
	require.NoError(t, err)
	scanner, err := NewSecretScanner()
	require.NoError(t, err)
	identity, err := age.GenerateX25519Identity()
	require.NoError(t, err)
	public, private, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	review := &e2eOwnerReview{}
	upload := &e2eUploader{}
	cipher := AgeCipher{Recipient: identity.Recipient().String(), Identity: identity.String()}
	err = Build(ctx, BuildInput{Person: "reviewer-alice", Version: 1, CreatedAt: time.Now().UTC(), TranscriptDays: 30,
		Documents: documents, Canary: "HEIMDALL-CANARY-T48", PrivateKey: private}, BuildDeps{
		Distiller: distillerFunc(func(_ context.Context, input []SourceDocument) ([]Section, error) {
			require.Len(t, input, 1)
			require.NotContains(t, input[0].Raw, "maintainer@example.invalid")
			return []Section{{Name: "conventions.md", Content: "Use idempotency keys. [doc:docs/guide.md]"}}, nil
		}),
		Scanner: scanner,
		Review:  review,
		Cipher:  cipher,
		Upload:  upload,
	})
	require.NoError(t, err)
	require.True(t, review.approved)
	require.NotEmpty(t, upload.value.Ciphertext)
	require.NotContains(t, string(upload.value.Ciphertext), "Use idempotency keys")
	store := storeSpy{value: upload.value}
	read, err := Read(ctx, "reviewer-alice", time.Now().UTC(), 14, public, store, cipher, "conventions.md")
	require.NoError(t, err)
	require.Contains(t, read, "Use idempotency keys")
	require.NotContains(t, read, "HEIMDALL-CANARY-T48")
	_, err = Read(ctx, "reviewer-bob", time.Now().UTC(), 14, public, store, cipher, "conventions.md")
	require.ErrorContains(t, err, "another reviewer")
}

type e2eOwnerReview struct{ approved bool }

func (r *e2eOwnerReview) Approve(_ context.Context, _ Manifest, sections []Section, removed []Redaction) (bool, error) {
	r.approved = len(sections) > 0 && len(removed) > 0
	return r.approved, nil
}

type e2eUploader struct{ value StoredPack }

func (u *e2eUploader) Upload(_ context.Context, owner string, ciphertext []byte, manifest Manifest) error {
	u.value = StoredPack{Owner: owner, Ciphertext: append([]byte(nil), ciphertext...), Manifest: manifest}
	return nil
}
