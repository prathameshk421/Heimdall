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
	"io/fs"
	"slices"
	"strings"
	"sync"
	"time"

	"filippo.io/age"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

var (
	// ErrOwnerApprovalRequired marks a publish that lacks an authenticated owner's approval.
	ErrOwnerApprovalRequired = errors.New("pack: owner approval required")
	// ErrPackForbidden marks an unauthenticated or unauthorized pack read.
	ErrPackForbidden = errors.New("pack: caller cannot read pack")
	// ErrPackNotFound marks a missing pack.
	ErrPackNotFound = errors.New("pack: not found")
	// ErrPackStale marks a future-dated or expired pack.
	ErrPackStale = errors.New("pack: stale")
	// ErrInvalidPackTime marks a malformed manifest timestamp.
	ErrInvalidPackTime = errors.New("pack: invalid manifest time")
	// ErrDurabilityUnsupported marks a pipeline configured with non-durable metadata.
	ErrDurabilityUnsupported = errors.New("pack: durable metadata unsupported")
)

// BlobStore persists encrypted pack blobs.
type BlobStore interface {
	Put(context.Context, string, []byte) error
	Get(context.Context, string) ([]byte, error)
	Delete(context.Context, string) error
}

// OwnerApproval checks whether an owner approved a reviewer.
type OwnerApproval interface {
	Approved(context.Context, string, string) (bool, error)
}

// TrustedOwnerApproval binds approval to an authenticated owner principal.
// Implementations must derive that principal from the request context, not input strings.
type TrustedOwnerApproval interface {
	OwnerApproval
	AuthenticatedOwner(context.Context) (string, error)
}

// CallerAuthorizer binds a request to an authenticated caller and checks the pack assignment.
type CallerAuthorizer interface {
	AuthenticatedCaller(context.Context) (string, error)
	CanReadPack(context.Context, string, string, string) (bool, error)
}

// MetadataStore persists pack metadata and replay keys across process restarts.
type MetadataStore interface {
	Durable() bool
	Get(context.Context, string) (Metadata, bool, error)
	FindReplay(context.Context, string, string) (Metadata, bool, error)
	Put(context.Context, Metadata, string) error
}

// Metadata describes a published pack and its signed manifest.
type Metadata struct {
	ID, Owner, Reviewer, ReplayKey, BlobKey, InputDigest string
	Manifest                                             SignedManifest
	Signature                                            []byte
}

// PublishInput contains the pack source files and owner/reviewer assignment.
type PublishInput struct {
	Owner, Reviewer, ReplayKey string
	ReviewDigest               string
	CreatedAt                  time.Time
	Files                      map[string][]byte
}

// ReviewOutput is the safe, redacted content and digest shown to an owner before publication.
type ReviewOutput struct {
	Files         map[string][]byte
	Digest        string
	RedactedFiles []string
}

// Service publishes encrypted packs and serves them only to their assigned reviewer.
type Service struct {
	Blobs      BlobStore
	Approvals  OwnerApproval
	Authorizer CallerAuthorizer
	Metadata   MetadataStore
	SigningKey ed25519.PrivateKey
	VerifyKey  ed25519.PublicKey
	Recipient  *age.X25519Recipient
	Identity   *AgeIdentity
	Clock      ports.Clock
	MaxAge     time.Duration
	MaxBytes   int64
}

// Review redacts both passes and returns a digest that Publish can require to bind the reviewed content.
func (s *Service) Review(owner, reviewer string, input map[string][]byte) (ReviewOutput, error) {
	if owner == "" || reviewer == "" || len(input) == 0 || s.MaxBytes <= 0 {
		return ReviewOutput{}, ErrInvalidPackCryptoInput
	}
	files, _, err := redactFiles(input)
	if err != nil {
		return ReviewOutput{}, err
	}
	if filesSize(files) > s.MaxBytes {
		return ReviewOutput{}, ErrPackTooLarge
	}
	fileHashes := make(map[string]string, len(files))
	redacted := make([]string, 0)
	for name, content := range files {
		h := sha256.Sum256(content)
		fileHashes[name] = hex.EncodeToString(h[:])
		if !bytes.Equal(content, input[name]) {
			redacted = append(redacted, name)
		}
	}
	slices.Sort(redacted)
	canonical, err := json.Marshal(struct {
		Owner, Reviewer string
		Files           map[string]string
	}{owner, reviewer, fileHashes})
	if err != nil {
		return ReviewOutput{}, fmt.Errorf("encode pack review digest: %w", err)
	}
	digest := sha256.Sum256(canonical)
	return ReviewOutput{Files: files, Digest: hex.EncodeToString(digest[:]), RedactedFiles: redacted}, nil
}

type callerContextKey struct{}

// WithAuthenticatedCaller attaches an identity resolved by a trusted inbound authenticator for server-side checks.
func WithAuthenticatedCaller(ctx context.Context, caller string) context.Context {
	return context.WithValue(ctx, callerContextKey{}, caller)
}

// AuthenticatedCallerFromContext reads the identity installed by WithAuthenticatedCaller.
func AuthenticatedCallerFromContext(ctx context.Context) (string, bool) {
	caller, ok := ctx.Value(callerContextKey{}).(string)
	return caller, ok && caller != ""
}

type ownerContextKey struct{}
type repositoryContextKey struct{}

// WithAuthenticatedOwner attaches an owner identity resolved by a trusted inbound person mapper.
func WithAuthenticatedOwner(ctx context.Context, owner string) context.Context {
	return context.WithValue(ctx, ownerContextKey{}, owner)
}

// AuthenticatedOwnerFromContext reads the identity installed by WithAuthenticatedOwner.
func AuthenticatedOwnerFromContext(ctx context.Context) (string, bool) {
	owner, ok := ctx.Value(ownerContextKey{}).(string)
	return owner, ok && owner != ""
}

// WithPackRepository binds owner approval to the repository whose policy is being checked.
func WithPackRepository(ctx context.Context, repo string) context.Context {
	return context.WithValue(ctx, repositoryContextKey{}, repo)
}

// RepositoryFromContext reads the repository installed by the authenticated pack route.
func RepositoryFromContext(ctx context.Context) (string, bool) {
	repo, ok := ctx.Value(repositoryContextKey{}).(string)
	return repo, ok && repo != ""
}

// Publish redacts, signs, encrypts, and stores a pack after trusted owner approval.
func (s *Service) Publish(ctx context.Context, in PublishInput) (Metadata, error) {
	if err := s.authorizePublish(ctx, in); err != nil {
		return Metadata{}, err
	}
	digest, err := publishDigest(in)
	if err != nil {
		return Metadata{}, err
	}
	if old, ok, err := s.replay(ctx, in, digest); err != nil || ok {
		return old, err
	}
	return s.createPack(ctx, in, digest)
}

func (s *Service) authorizePublish(ctx context.Context, in PublishInput) error {
	if err := s.validatePublishRequest(in); err != nil {
		return err
	}
	if err := s.authorizeOwner(ctx, in); err != nil {
		return err
	}
	if in.ReviewDigest != "" {
		review, err := s.Review(in.Owner, in.Reviewer, in.Files)
		if err != nil {
			return err
		}
		if review.Digest != in.ReviewDigest {
			return ErrInvalidPackCryptoInput
		}
	}
	return s.validatePublishConfig(in)
}

func (s *Service) validatePublishRequest(in PublishInput) error {
	if in.Owner == "" || in.Reviewer == "" || in.ReplayKey == "" {
		return ErrOwnerApprovalRequired
	}
	if s.Approvals == nil || s.Metadata == nil {
		return ErrOwnerApprovalRequired
	}
	if !s.Metadata.Durable() {
		return ErrDurabilityUnsupported
	}
	return nil
}

func (s *Service) authorizeOwner(ctx context.Context, in PublishInput) error {
	trusted, ok := s.Approvals.(TrustedOwnerApproval)
	if !ok {
		return ErrOwnerApprovalRequired
	}
	principal, err := trusted.AuthenticatedOwner(ctx)
	if err != nil || principal == "" || principal != in.Owner {
		return ErrOwnerApprovalRequired
	}
	approved, err := trusted.Approved(ctx, principal, in.Reviewer)
	if err != nil || !approved {
		return ErrOwnerApprovalRequired
	}
	return nil
}

func (s *Service) validatePublishConfig(in PublishInput) error {
	if in.CreatedAt.IsZero() || s.Clock == nil || in.CreatedAt.After(s.Clock.Now()) {
		return ErrInvalidPackCryptoInput
	}
	if s.Blobs == nil || len(s.SigningKey) != ed25519.PrivateKeySize || s.Recipient == nil || s.MaxBytes <= 0 {
		return ErrInvalidPackCryptoInput
	}
	return nil
}

func (s *Service) replay(ctx context.Context, in PublishInput, digest string) (Metadata, bool, error) {
	old, exists, err := s.Metadata.FindReplay(ctx, in.Owner, in.ReplayKey)
	if err != nil {
		return Metadata{}, false, err
	}
	if !exists {
		return Metadata{}, false, nil
	}
	if old.InputDigest != digest {
		return Metadata{}, false, ErrInvalidPackCryptoInput
	}
	return old, true, nil
}

func (s *Service) createPack(ctx context.Context, in PublishInput, digest string) (Metadata, error) {
	files, hashes, err := redactFiles(in.Files)
	if err != nil {
		return Metadata{}, err
	}
	payload, err := json.Marshal(files)
	if err != nil {
		return Metadata{}, fmt.Errorf("marshal pack: %w", err)
	}
	if filesSize(files) > s.MaxBytes || int64(len(payload)) > s.MaxBytes {
		return Metadata{}, ErrPackTooLarge
	}
	manifest := SignedManifest{Version: 1, Owner: in.Owner, CreatedAt: in.CreatedAt.UTC().Format(time.RFC3339Nano), Files: hashes}
	sig, err := SignManifest(manifest, s.SigningKey)
	if err != nil {
		return Metadata{}, err
	}
	cipher, err := EncryptPack(ctx, bytes.NewReader(payload), s.Recipient, s.MaxBytes)
	if err != nil {
		return Metadata{}, err
	}
	meta := Metadata{Owner: in.Owner, Reviewer: in.Reviewer, ReplayKey: in.ReplayKey, Manifest: manifest, Signature: sig, InputDigest: digest}
	if err := s.storePack(ctx, &meta, cipher); err != nil {
		return Metadata{}, err
	}
	return meta, nil
}

func redactFiles(input map[string][]byte) (map[string][]byte, map[string]string, error) {
	files := make(map[string][]byte, len(input))
	hashes := make(map[string]string, len(input))
	for name, raw := range input {
		if !safePackPath(name) {
			return nil, nil, ErrInvalidPackCryptoInput
		}
		first, err := Redact(string(raw))
		if err != nil {
			return nil, nil, err
		}
		second, err := Redact(first)
		if err != nil {
			return nil, nil, err
		}
		files[name] = []byte(second)
		hash := sha256.Sum256(files[name])
		hashes[name] = hex.EncodeToString(hash[:])
	}
	return files, hashes, nil
}

func filesSize(files map[string][]byte) int64 {
	var total int64
	for _, content := range files {
		total += int64(len(content))
	}
	return total
}

func (s *Service) storePack(ctx context.Context, meta *Metadata, cipher []byte) error {
	hash := sha256.Sum256(append(append([]byte{}, cipher...), meta.Signature...))
	meta.ID = hex.EncodeToString(hash[:])
	meta.BlobKey = "packs/" + meta.ID
	if err := s.Blobs.Put(ctx, meta.BlobKey, cipher); err != nil {
		return fmt.Errorf("store encrypted pack: %w", err)
	}
	if err := s.Metadata.Put(ctx, *meta, meta.InputDigest); err != nil {
		if cleanupErr := s.Blobs.Delete(context.WithoutCancel(ctx), meta.BlobKey); cleanupErr != nil {
			return errors.Join(fmt.Errorf("store pack metadata: %w", err), fmt.Errorf("remove unpublished blob: %w", cleanupErr))
		}
		return fmt.Errorf("store pack metadata: %w", err)
	}
	return nil
}

// Read verifies and decrypts a pack for its authenticated assigned reviewer.
func (s *Service) Read(ctx context.Context, caller, id string) (map[string][]byte, error) {
	if s.Metadata == nil || s.Authorizer == nil || !s.Metadata.Durable() {
		return nil, ErrPackForbidden
	}
	if s.Blobs == nil || s.Identity == nil || s.MaxBytes <= 0 {
		return nil, ErrInvalidPackCryptoInput
	}
	if s.Clock == nil {
		return nil, ErrInvalidPackCryptoInput
	}
	meta, ok, err := s.Metadata.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrPackNotFound
	}
	if err := s.authorizeRead(ctx, caller, meta); err != nil {
		return nil, err
	}
	if err := validateManifest(meta, s.Clock.Now(), s.MaxAge, s.VerifyKey); err != nil {
		return nil, err
	}
	return s.decryptFiles(ctx, meta)
}

func (s *Service) authorizeRead(ctx context.Context, caller string, meta Metadata) error {
	identity, err := s.Authorizer.AuthenticatedCaller(ctx)
	if err != nil || identity == "" || caller != "" && caller != identity {
		return ErrPackForbidden
	}
	allowed, err := s.Authorizer.CanReadPack(ctx, identity, meta.Owner, meta.Reviewer)
	if err != nil || !allowed {
		return ErrPackForbidden
	}
	return nil
}

func validateManifest(meta Metadata, now time.Time, maxAge time.Duration, verifyKey ed25519.PublicKey) error {
	if err := validateManifestTime(meta.Manifest.CreatedAt, now, maxAge); err != nil {
		return err
	}
	if !validManifestStructure(meta) {
		return ErrInvalidSignature
	}
	return VerifyManifest(meta.Manifest, verifyKey, meta.Signature)
}

func validateManifestTime(value string, now time.Time, maxAge time.Duration) error {
	created, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return ErrInvalidPackTime
	}
	if created.After(now) || maxAge <= 0 || now.Sub(created) > maxAge {
		return ErrPackStale
	}
	return nil
}

func validManifestStructure(meta Metadata) bool {
	if meta.Manifest.Version != 1 || meta.Manifest.Owner != meta.Owner || len(meta.Manifest.Files) == 0 {
		return false
	}
	for name, digest := range meta.Manifest.Files {
		if !safePackPath(name) {
			return false
		}
		decoded, err := hex.DecodeString(digest)
		if err != nil || len(decoded) != sha256.Size {
			return false
		}
	}
	return true
}

func safePackPath(name string) bool {
	return name != "." && fs.ValidPath(name) && !strings.Contains(name, `\`)
}

func (s *Service) decryptFiles(ctx context.Context, meta Metadata) (map[string][]byte, error) {
	cipher, err := s.Blobs.Get(ctx, meta.BlobKey)
	if err != nil {
		return nil, fmt.Errorf("read encrypted pack: %w", err)
	}
	plain, err := DecryptPack(ctx, bytes.NewReader(cipher), s.Identity, s.MaxBytes)
	if err != nil {
		return nil, err
	}
	var files map[string][]byte
	if err := json.Unmarshal(plain, &files); err != nil {
		return nil, fmt.Errorf("decode pack: %w", err)
	}
	if len(files) != len(meta.Manifest.Files) {
		return nil, ErrInvalidSignature
	}
	for name, content := range files {
		if _, exists := meta.Manifest.Files[name]; !exists {
			return nil, ErrInvalidSignature
		}
		hash := sha256.Sum256(content)
		if hex.EncodeToString(hash[:]) != meta.Manifest.Files[name] {
			return nil, ErrInvalidSignature
		}
		redacted, err := Redact(string(content))
		if err != nil {
			return nil, err
		}
		second, err := Redact(redacted)
		if err != nil {
			return nil, err
		}
		files[name] = []byte(second)
	}
	return files, nil
}
func publishDigest(in PublishInput) (string, error) {
	fileHashes := make(map[string]string, len(in.Files))
	for n, b := range in.Files {
		h := sha256.Sum256(b)
		fileHashes[n] = hex.EncodeToString(h[:])
	}
	b, err := json.Marshal(struct {
		Owner, Reviewer string
		CreatedAt       string
		Files           map[string]string
	}{in.Owner, in.Reviewer, in.CreatedAt.UTC().Format(time.RFC3339Nano), fileHashes})
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}

// MemoryBlobStore is an in-process BlobStore for tests and local prototypes only.
type MemoryBlobStore struct {
	mu     sync.Mutex
	values map[string][]byte
}

// NewMemoryBlobStore creates an empty in-process blob store.
func NewMemoryBlobStore() *MemoryBlobStore { return &MemoryBlobStore{values: map[string][]byte{}} }

// Put stores a copy of b at k and refuses to overwrite existing blobs.
func (m *MemoryBlobStore) Put(ctx context.Context, k string, b []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.values[k]; ok {
		return errors.New("pack: blob already exists")
	}
	m.values[k] = append([]byte(nil), b...)
	return nil
}

// Get returns a copy of a stored blob.
func (m *MemoryBlobStore) Get(ctx context.Context, k string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.values[k]
	if !ok {
		return nil, ErrPackNotFound
	}
	return append([]byte(nil), b...), nil
}

// Delete removes a blob if it exists.
func (m *MemoryBlobStore) Delete(ctx context.Context, k string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.values, k)
	return nil
}
