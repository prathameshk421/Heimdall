// Package blobfs implements the persistent ports.BlobStore adapter.
package blobfs

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

const refPrefix = "blob:sha256:"

// Store persists content-addressed blobs beneath a configured directory. The
// class directory carries the retention hint defined by ports.BlobClass.
type Store struct {
	root *os.Root
}

// New opens a filesystem-backed blob store at root, creating it when needed.
func New(root string) (*Store, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("open blob store: root is empty")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("create blob store root: %w", err)
	}
	opened, err := os.OpenRoot(root)
	if err != nil {
		return nil, fmt.Errorf("open blob store root: %w", err)
	}
	return &Store{root: opened}, nil
}

// Close releases the directory handle held by the store.
func (s *Store) Close() error {
	if err := s.root.Close(); err != nil {
		return fmt.Errorf("close blob store: %w", err)
	}
	return nil
}

// Put stores content atomically and returns its SHA-256 reference.
func (s *Store) Put(ctx context.Context, class ports.BlobClass, r io.Reader) (workflow.BlobRef, error) {
	if !class.Valid() {
		return "", fmt.Errorf("put blob: invalid class %q", class)
	}
	data, err := io.ReadAll(contextReader{ctx: ctx, reader: r})
	if err != nil {
		return "", fmt.Errorf("read blob content: %w", err)
	}
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	path := blobPath(class, hash)
	dir := path[:strings.LastIndexByte(path, '/')]
	if err := s.root.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create blob directory: %w", err)
	}
	temp, tempPath, err := s.createTemp(dir)
	if err != nil {
		return "", fmt.Errorf("create temporary blob: %w", err)
	}
	defer func() { _ = s.root.Remove(tempPath) }()
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return "", fmt.Errorf("write temporary blob: %w", err)
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return "", fmt.Errorf("sync temporary blob: %w", err)
	}
	if err := temp.Close(); err != nil {
		return "", fmt.Errorf("close temporary blob: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("store blob: %w", err)
	}
	if err := s.root.Rename(tempPath, path); err != nil {
		return "", fmt.Errorf("publish blob: %w", err)
	}
	return workflow.BlobRef(refPrefix + hash), nil
}

// Get opens content by its SHA-256 reference. Missing content maps to ErrNotFound.
func (s *Store) Get(ctx context.Context, ref workflow.BlobRef) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("get blob: %w", err)
	}
	hash, ok := parseRef(ref)
	if !ok {
		return nil, fmt.Errorf("get blob: invalid reference %q", ref)
	}
	for _, class := range classes() {
		file, err := s.root.Open(blobPath(class, hash))
		if err == nil {
			return file, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("open blob: %w", err)
		}
	}
	return nil, ports.ErrNotFound
}

// Delete removes a blob from its class after the owner has proved no live references remain.
func (s *Store) Delete(ctx context.Context, class ports.BlobClass, ref workflow.BlobRef) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("delete blob: %w", err)
	}
	if !class.Valid() {
		return fmt.Errorf("delete blob: invalid class %q", class)
	}
	hash, ok := parseRef(ref)
	if !ok {
		return errors.New("delete blob: invalid reference")
	}
	if err := s.root.Remove(blobPath(class, hash)); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ports.ErrNotFound
		}
		return fmt.Errorf("delete blob: %w", err)
	}
	return nil
}

func classes() [6]ports.BlobClass {
	return [6]ports.BlobClass{ports.BlobLog, ports.BlobDiff, ports.BlobBundle, ports.BlobPack, ports.BlobEvidence, ports.BlobKnowledge}
}

func (s *Store) createTemp(dir string) (*os.File, string, error) {
	var entropy [16]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return nil, "", fmt.Errorf("generate temporary name: %w", err)
	}
	name := dir + "/.tmp-" + hex.EncodeToString(entropy[:])
	file, err := s.root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, "", fmt.Errorf("open temporary file: %w", err)
	}
	return file, name, nil
}

func blobPath(class ports.BlobClass, hash string) string {
	return string(class) + "/sha256/" + hash[:2] + "/" + hash[2:4] + "/" + hash
}

func parseRef(ref workflow.BlobRef) (string, bool) {
	value := string(ref)
	if !strings.HasPrefix(value, refPrefix) {
		return "", false
	}
	hash := strings.TrimPrefix(value, refPrefix)
	if len(hash) != sha256.Size*2 {
		return "", false
	}
	decoded, err := hex.DecodeString(hash)
	if err != nil || len(decoded) != sha256.Size || strings.ToLower(hash) != hash {
		return "", false
	}
	return hash, true
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(buffer []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(buffer)
}
