package pack

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"io"
	"testing"
	"time"

	"filippo.io/age"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
	"github.com/stretchr/testify/require"
)

func TestVaultPersistsCiphertextAndServesOnlyVerifiedOwnerPack(t *testing.T) {
	ctx := context.Background()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	created := time.Now().UTC().Truncate(time.Microsecond)
	manifest, plaintext, err := signPack(BuildInput{Person: "alice", Version: 1, CreatedAt: created, Canary: "secret-canary", PrivateKey: private}, []Section{{Name: "invariants", Content: "rule\n<!-- secret-canary -->\n"}})
	require.NoError(t, err)
	identity, err := age.GenerateX25519Identity()
	require.NoError(t, err)
	cipher := AgeCipher{Recipient: identity.Recipient().String(), Identity: identity.String()}
	ciphertext, err := cipher.Encrypt(ctx, plaintext)
	require.NoError(t, err)
	blobs := &memoryPackBlobs{values: make(map[workflow.BlobRef][]byte)}
	catalog := &memoryPackCatalog{values: make(map[string]ports.PackRecord)}
	vault, err := NewVault(blobs, blobs, catalog)
	require.NoError(t, err)
	require.NoError(t, vault.Upload(ctx, "alice", ciphertext, manifest))
	require.NotContains(t, string(blobs.values[catalog.values["alice:1"].Blob]), "secret-canary", "only ciphertext is stored")

	stored, err := vault.GetPack(ctx, "alice")
	require.NoError(t, err)
	require.Equal(t, "alice", stored.Owner)
	require.Equal(t, manifest.Signature, stored.Manifest.Signature)
	section, err := Read(ctx, "alice", created.Add(time.Hour), 14, public, vault, cipher, "invariants")
	require.NoError(t, err)
	require.Contains(t, section, "[PRIVATE PACK CANARY]")
	require.NotContains(t, section, "secret-canary")
	_, err = vault.GetPack(ctx, "mallory")
	require.Error(t, err)
}

func TestVaultRemovesCiphertextWhenMetadataSaveFails(t *testing.T) {
	blobs := &memoryPackBlobs{values: make(map[workflow.BlobRef][]byte)}
	catalog := &memoryPackCatalog{values: make(map[string]ports.PackRecord), failSave: true}
	vault, err := NewVault(blobs, blobs, catalog)
	require.NoError(t, err)
	err = vault.Upload(context.Background(), "alice", []byte("ciphertext"), Manifest{Person: "alice", Version: 1, CreatedAt: time.Now()})
	require.ErrorContains(t, err, "save pack metadata")
	require.Empty(t, blobs.values, "failed metadata persistence must not orphan ciphertext")
}

type memoryPackBlobs struct{ values map[workflow.BlobRef][]byte }

func (m *memoryPackBlobs) Put(_ context.Context, _ ports.BlobClass, r io.Reader) (workflow.BlobRef, error) {
	body, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	ref := workflow.BlobRef(fmt.Sprintf("blob:sha256:%064x", len(m.values)+1))
	m.values[ref] = bytes.Clone(body)
	return ref, nil
}
func (m *memoryPackBlobs) Get(_ context.Context, ref workflow.BlobRef) (io.ReadCloser, error) {
	body, ok := m.values[ref]
	if !ok {
		return nil, ports.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(bytes.Clone(body))), nil
}
func (m *memoryPackBlobs) Delete(_ context.Context, _ ports.BlobClass, ref workflow.BlobRef) error {
	delete(m.values, ref)
	return nil
}

type memoryPackCatalog struct {
	values   map[string]ports.PackRecord
	failSave bool
}

func (m *memoryPackCatalog) SavePack(_ context.Context, owner string, version int, blob workflow.BlobRef, manifest []byte, created time.Time) error {
	if m.failSave {
		return fmt.Errorf("catalog unavailable")
	}
	key := fmt.Sprintf("%s:%d", owner, version)
	if existing, ok := m.values[key]; ok && (existing.Blob != blob || !bytes.Equal(existing.Manifest, manifest)) {
		return fmt.Errorf("version differs")
	}
	m.values[key] = ports.PackRecord{Owner: owner, Version: version, Blob: blob, Manifest: bytes.Clone(manifest), CreatedAt: created}
	return nil
}
func (m *memoryPackCatalog) LatestPack(_ context.Context, owner string) (ports.PackRecord, error) {
	var latest ports.PackRecord
	for _, record := range m.values {
		if record.Owner == owner && record.Version > latest.Version {
			latest = record
		}
	}
	if latest.Owner == "" {
		return ports.PackRecord{}, ports.ErrNotFound
	}
	return latest, nil
}
func (m *memoryPackCatalog) DeletePacks(_ context.Context, owner string) ([]workflow.BlobRef, error) {
	var refs []workflow.BlobRef
	for key, record := range m.values {
		if record.Owner == owner {
			refs = append(refs, record.Blob)
			delete(m.values, key)
		}
	}
	return refs, nil
}
