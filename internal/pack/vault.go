package pack

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// Vault keeps ciphertext in the pack blob class and its signed manifest in the durable pack catalog.
type Vault struct {
	blobs   ports.BlobStore
	deleter ports.BlobDeleter
	catalog ports.PackCatalog
}

// NewVault requires durable metadata, content storage and deletion support.
func NewVault(blobs ports.BlobStore, deleter ports.BlobDeleter, catalog ports.PackCatalog) (*Vault, error) {
	if blobs == nil || deleter == nil || catalog == nil {
		return nil, errors.New("pack vault dependencies must not be nil")
	}
	return &Vault{blobs: blobs, deleter: deleter, catalog: catalog}, nil
}

// Upload stores only encrypted bytes and signed metadata.
func (v *Vault) Upload(ctx context.Context, owner string, ciphertext []byte, manifest Manifest) error {
	if owner == "" || owner != manifest.Person || manifest.Version < 1 || len(ciphertext) == 0 {
		return errors.New("pack upload identity or payload is invalid")
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("encode pack manifest: %w", err)
	}
	blob, err := v.blobs.Put(ctx, ports.BlobPack, bytes.NewReader(ciphertext))
	if err != nil {
		return fmt.Errorf("store encrypted pack: %w", err)
	}
	if err := v.catalog.SavePack(ctx, owner, manifest.Version, blob, encoded, manifest.CreatedAt); err != nil {
		if deleteErr := v.deleter.Delete(ctx, ports.BlobPack, blob); deleteErr != nil {
			return errors.Join(fmt.Errorf("save pack metadata: %w", err), fmt.Errorf("remove orphaned ciphertext: %w", deleteErr))
		}
		return fmt.Errorf("save pack metadata: %w", err)
	}
	return nil
}

// GetPack loads the newest ciphertext for its authenticated owner.
func (v *Vault) GetPack(ctx context.Context, owner string) (StoredPack, error) {
	if owner == "" {
		return StoredPack{}, errors.New("pack owner is required")
	}
	record, err := v.catalog.LatestPack(ctx, owner)
	if err != nil {
		return StoredPack{}, fmt.Errorf("load pack metadata: %w", err)
	}
	var manifest Manifest
	if err := json.Unmarshal(record.Manifest, &manifest); err != nil {
		return StoredPack{}, fmt.Errorf("decode pack metadata: %w", err)
	}
	if record.Owner != owner || record.Version != manifest.Version || record.CreatedAt != manifest.CreatedAt || manifest.Person != owner {
		return StoredPack{}, errors.New("pack metadata identity is inconsistent")
	}
	reader, err := v.blobs.Get(ctx, record.Blob)
	if err != nil {
		return StoredPack{}, fmt.Errorf("read encrypted pack: %w", err)
	}
	ciphertext, readErr := io.ReadAll(io.LimitReader(reader, maxPackBytes+1))
	closeErr := reader.Close()
	if readErr != nil {
		return StoredPack{}, fmt.Errorf("read encrypted pack: %w", readErr)
	}
	if closeErr != nil {
		return StoredPack{}, fmt.Errorf("close encrypted pack: %w", closeErr)
	}
	if len(ciphertext) > maxPackBytes {
		return StoredPack{}, errors.New("encrypted pack exceeds size limit")
	}
	return StoredPack{Owner: owner, Ciphertext: ciphertext, Manifest: manifest}, nil
}

const maxPackBytes = 16 << 20

// Revoke deletes all owner metadata then removes ciphertext blobs that are not shared by another pack record.
func (v *Vault) Revoke(ctx context.Context, owner string) error {
	if owner == "" {
		return errors.New("pack owner is required")
	}
	refs, err := v.catalog.DeletePacks(ctx, owner)
	if err != nil {
		return fmt.Errorf("delete pack metadata: %w", err)
	}
	for _, ref := range refs {
		if err := v.deleter.Delete(ctx, ports.BlobPack, ref); err != nil {
			return fmt.Errorf("delete revoked pack ciphertext: %w", err)
		}
	}
	return nil
}

var _ Uploader = (*Vault)(nil)
var _ ReaderStore = (*Vault)(nil)
