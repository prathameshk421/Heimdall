package pack

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"testing"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/clock"
)

func TestPublishRequiresOwnerApprovalAndReadIsCallerScopedAndReplaySafe(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	id, _ := NewAgeIdentity()
	store := NewMemoryBlobStore()
	metadata := newTestMetadata()
	svc := Service{Blobs: store, Approvals: trustedApproval{approved: false}, Metadata: metadata, Authorizer: testCaller{}, SigningKey: priv, VerifyKey: pub, Recipient: id.Recipient(), Identity: id, MaxAge: 14 * 24 * time.Hour, MaxBytes: 1 << 20, Clock: clock.System{}}
	input := PublishInput{Owner: "alice", Reviewer: "alice-reviewer", ReplayKey: "refresh-1", CreatedAt: time.Now().UTC(), Files: map[string][]byte{"notes.md": []byte("safe notes")}}
	if _, err := svc.Publish(context.Background(), input); !errors.Is(err, ErrOwnerApprovalRequired) {
		t.Fatalf("unapproved publish accepted: %v", err)
	}
	svc.Approvals = trustedApproval{approved: true}
	meta, err := svc.Publish(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	again, err := svc.Publish(context.Background(), input)
	if err != nil || again.ID != meta.ID {
		t.Fatalf("replay: %#v %v", again, err)
	}
	if _, err := svc.Read(context.Background(), "mallory-reviewer", meta.ID); !errors.Is(err, ErrPackForbidden) {
		t.Fatalf("cross-caller read: %v", err)
	}
	got, err := svc.Read(context.Background(), "alice-reviewer", meta.ID)
	if err != nil || string(got["notes.md"]) != "safe notes" {
		t.Fatalf("own read: %v", err)
	}
}

func TestPublishRejectsCallerAssertedApproval(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	id, _ := NewAgeIdentity()
	svc := Service{Blobs: NewMemoryBlobStore(), Approvals: testApproval(true), Metadata: newTestMetadata(), SigningKey: priv, VerifyKey: pub, Recipient: id.Recipient(), Identity: id, MaxAge: time.Hour, MaxBytes: 1 << 20, Clock: clock.System{}}
	in := PublishInput{Owner: "alice", Reviewer: "reviewer", ReplayKey: "k", CreatedAt: time.Now(), Files: map[string][]byte{"a": []byte("x")}}
	if _, err := svc.Publish(context.Background(), in); err == nil {
		t.Fatal("caller-asserted approval accepted without trusted verifier")
	}
}

func TestPublishRejectsFutureCreationTime(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	id, _ := NewAgeIdentity()
	svc := Service{Blobs: NewMemoryBlobStore(), Approvals: trustedApproval{approved: true}, Metadata: newTestMetadata(), SigningKey: priv, Recipient: id.Recipient(), Identity: id, MaxAge: time.Hour, MaxBytes: 1 << 20, Clock: clock.System{}}
	in := PublishInput{Owner: "alice", Reviewer: "reviewer", ReplayKey: "k", CreatedAt: time.Now().Add(time.Hour), Files: map[string][]byte{"a": []byte("x")}}
	if _, err := svc.Publish(context.Background(), in); !errors.Is(err, ErrInvalidPackCryptoInput) {
		t.Fatalf("future pack timestamp accepted: %v", err)
	}
}

func TestPublishRejectsUnauthenticatedOwnerAndReviewerIdentityMismatch(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	identity, _ := NewAgeIdentity()
	svc := Service{Blobs: NewMemoryBlobStore(), Approvals: trustedApproval{approved: true}, Metadata: newTestMetadata(), SigningKey: priv, Recipient: identity.Recipient(), MaxBytes: 1 << 20, Clock: clock.System{}}
	in := PublishInput{Owner: "mallory", Reviewer: "reviewer", ReplayKey: "k", CreatedAt: time.Now(), Files: map[string][]byte{"a": []byte("x")}}
	if _, err := svc.Publish(context.Background(), in); !errors.Is(err, ErrOwnerApprovalRequired) {
		t.Fatalf("owner identity supplied by request was trusted: %v", err)
	}
	in.Owner = "alice"
	in.Reviewer = "mallory-reviewer"
	if _, err := svc.Publish(context.Background(), in); !errors.Is(err, ErrOwnerApprovalRequired) {
		t.Fatalf("approval for different reviewer was trusted: %v", err)
	}
}

func TestPublishRejectsReplayKeyReuseWithDifferentContent(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	id, _ := NewAgeIdentity()
	svc := Service{Blobs: NewMemoryBlobStore(), Approvals: trustedApproval{approved: true}, Metadata: newTestMetadata(), SigningKey: priv, VerifyKey: pub, Recipient: id.Recipient(), Identity: id, MaxAge: time.Hour, MaxBytes: 1 << 20, Clock: clock.System{}}
	in := PublishInput{Owner: "alice", Reviewer: "reviewer", ReplayKey: "k", CreatedAt: time.Now(), Files: map[string][]byte{"a": []byte("x")}}
	if _, err := svc.Publish(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	in.Files["a"] = []byte("different")
	if _, err := svc.Publish(context.Background(), in); err == nil {
		t.Fatal("replay key reused with different request")
	}
}

func TestPublishRejectsUnsafePackPaths(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	identity, _ := NewAgeIdentity()
	svc := Service{Blobs: NewMemoryBlobStore(), Approvals: trustedApproval{approved: true}, Metadata: newTestMetadata(), SigningKey: priv, Recipient: identity.Recipient(), MaxBytes: 1 << 20, Clock: clock.System{}}
	for _, name := range []string{"../secret", "a/../secret", "/absolute", `a\secret`, "."} {
		t.Run(name, func(t *testing.T) {
			in := PublishInput{Owner: "alice", Reviewer: "reviewer", ReplayKey: name, CreatedAt: time.Now(), Files: map[string][]byte{name: []byte("x")}}
			if _, err := svc.Publish(context.Background(), in); !errors.Is(err, ErrInvalidPackCryptoInput) {
				t.Fatalf("unsafe path %q accepted: %v", name, err)
			}
		})
	}
}

func TestPublishRemovesBlobWhenMetadataWriteFails(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	identity, _ := NewAgeIdentity()
	blobs := NewMemoryBlobStore()
	metadata := newTestMetadata()
	metadata.putErr = errors.New("metadata unavailable")
	svc := Service{Blobs: blobs, Approvals: trustedApproval{approved: true}, Metadata: metadata, SigningKey: priv, Recipient: identity.Recipient(), Identity: identity, MaxAge: time.Hour, MaxBytes: 1 << 20, Clock: clock.System{}}
	input := PublishInput{Owner: "alice", Reviewer: "reviewer", ReplayKey: "k", CreatedAt: time.Now(), Files: map[string][]byte{"a": []byte("x")}}
	if _, err := svc.Publish(context.Background(), input); err == nil {
		t.Fatal("publish with unavailable metadata succeeded")
	}
	if len(blobs.values) != 0 {
		t.Fatalf("unpublished encrypted blob remains: %d", len(blobs.values))
	}
}

func TestReadRejectsFutureAndMalformedManifestTimes(t *testing.T) {
	for _, tc := range []struct {
		created string
		want    error
	}{
		{created: time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano), want: ErrPackStale},
		{created: "not-a-time", want: ErrInvalidPackTime},
		{created: time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339Nano), want: ErrPackStale},
	} {
		t.Run(tc.created, func(t *testing.T) {
			pub, priv, _ := ed25519.GenerateKey(rand.Reader)
			id, _ := NewAgeIdentity()
			metadata := newTestMetadata()
			svc := Service{Blobs: NewMemoryBlobStore(), Approvals: trustedApproval{approved: true}, Metadata: metadata, Authorizer: testCaller{}, SigningKey: priv, VerifyKey: pub, Recipient: id.Recipient(), Identity: id, MaxAge: time.Hour, MaxBytes: 1 << 20, Clock: clock.System{}}
			in := PublishInput{Owner: "alice", Reviewer: "alice-reviewer", ReplayKey: "k", CreatedAt: time.Now().Add(-time.Minute), Files: map[string][]byte{"a": []byte("x")}}
			meta, err := svc.Publish(context.Background(), in)
			if err != nil {
				t.Fatal(err)
			}
			stored := metadata.items[meta.ID]
			stored.Manifest.CreatedAt = tc.created
			metadata.items[meta.ID] = stored
			if _, err := svc.Read(context.Background(), "alice-reviewer", meta.ID); !errors.Is(err, tc.want) {
				t.Fatalf("read with manifest timestamp %q: %v", tc.created, err)
			}
		})
	}
}

func TestReadUsesInjectedClockInsteadOfCallerTime(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	identity, _ := NewAgeIdentity()
	metadata := newTestMetadata()
	now := time.Now().UTC()
	svc := Service{Blobs: NewMemoryBlobStore(), Approvals: trustedApproval{approved: true}, Metadata: metadata, Authorizer: testCaller{}, SigningKey: priv, Recipient: identity.Recipient(), Identity: identity, MaxAge: time.Hour, MaxBytes: 1 << 20, Clock: fixedClock{now: now}}
	input := PublishInput{Owner: "alice", Reviewer: "alice-reviewer", ReplayKey: "k", CreatedAt: now.Add(-30 * time.Minute), Files: map[string][]byte{"a": []byte("x")}}
	meta, err := svc.Publish(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	svc.Clock = fixedClock{now: now.Add(2 * time.Hour)}
	if _, err := svc.Read(context.Background(), "alice-reviewer", meta.ID); !errors.Is(err, ErrPackStale) {
		t.Fatalf("expired pack accepted using caller-controlled time: %v", err)
	}
}

func TestReadRejectsManifestWithExtraFile(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	id, _ := NewAgeIdentity()
	metadata := newTestMetadata()
	svc := Service{Blobs: NewMemoryBlobStore(), Approvals: trustedApproval{approved: true}, Metadata: metadata, Authorizer: testCaller{}, SigningKey: priv, VerifyKey: pub, Recipient: id.Recipient(), Identity: id, MaxAge: time.Hour, MaxBytes: 1 << 20, Clock: clock.System{}}
	input := PublishInput{Owner: "alice", Reviewer: "alice-reviewer", ReplayKey: "k", CreatedAt: time.Now().Add(-time.Minute), Files: map[string][]byte{"a": []byte("x")}}
	meta, err := svc.Publish(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	stored := metadata.items[meta.ID]
	stored.Manifest.Files["extra.md"] = "deadbeef"
	stored.Signature, err = SignManifest(stored.Manifest, priv)
	if err != nil {
		t.Fatal(err)
	}
	metadata.items[meta.ID] = stored
	if _, err := svc.Read(context.Background(), "alice-reviewer", meta.ID); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("manifest with extra entry accepted: %v", err)
	}
}

func TestReadRejectsMissingManifestFileAndHashMismatch(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*Metadata)
	}{
		{name: "missing manifest entry", mutate: func(meta *Metadata) { delete(meta.Manifest.Files, "a") }},
		{name: "bad digest", mutate: func(meta *Metadata) {
			meta.Manifest.Files["a"] = "0000000000000000000000000000000000000000000000000000000000000000"
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pub, priv, _ := ed25519.GenerateKey(rand.Reader)
			identity, _ := NewAgeIdentity()
			metadata := newTestMetadata()
			svc := Service{Blobs: NewMemoryBlobStore(), Approvals: trustedApproval{approved: true}, Metadata: metadata, Authorizer: testCaller{}, SigningKey: priv, VerifyKey: pub, Recipient: identity.Recipient(), Identity: identity, MaxAge: time.Hour, MaxBytes: 1 << 20, Clock: clock.System{}}
			input := PublishInput{Owner: "alice", Reviewer: "alice-reviewer", ReplayKey: "k", CreatedAt: time.Now().Add(-time.Minute), Files: map[string][]byte{"a": []byte("x")}}
			meta, err := svc.Publish(context.Background(), input)
			if err != nil {
				t.Fatal(err)
			}
			stored := metadata.items[meta.ID]
			tc.mutate(&stored)
			stored.Signature, err = SignManifest(stored.Manifest, priv)
			if err != nil {
				t.Fatal(err)
			}
			metadata.items[meta.ID] = stored
			if _, err := svc.Read(context.Background(), "alice-reviewer", meta.ID); !errors.Is(err, ErrInvalidSignature) {
				t.Fatalf("inconsistent manifest accepted: %v", err)
			}
		})
	}
}

func TestReadRejectsUnsupportedManifestVersionAndOwnerMismatch(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*SignedManifest)
	}{
		{name: "unsupported version", mutate: func(m *SignedManifest) { m.Version = 2 }},
		{name: "owner mismatch", mutate: func(m *SignedManifest) { m.Owner = "mallory" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pub, priv, _ := ed25519.GenerateKey(rand.Reader)
			id, _ := NewAgeIdentity()
			metadata := newTestMetadata()
			svc := Service{Blobs: NewMemoryBlobStore(), Approvals: trustedApproval{approved: true}, Metadata: metadata, Authorizer: testCaller{}, SigningKey: priv, VerifyKey: pub, Recipient: id.Recipient(), Identity: id, MaxAge: time.Hour, MaxBytes: 1 << 20, Clock: clock.System{}}
			input := PublishInput{Owner: "alice", Reviewer: "alice-reviewer", ReplayKey: "k", CreatedAt: time.Now().Add(-time.Minute), Files: map[string][]byte{"a": []byte("x")}}
			meta, err := svc.Publish(context.Background(), input)
			if err != nil {
				t.Fatal(err)
			}
			stored := metadata.items[meta.ID]
			tc.mutate(&stored.Manifest)
			stored.Signature, err = SignManifest(stored.Manifest, priv)
			if err != nil {
				t.Fatal(err)
			}
			metadata.items[meta.ID] = stored
			if _, err := svc.Read(context.Background(), "alice-reviewer", meta.ID); !errors.Is(err, ErrInvalidSignature) {
				t.Fatalf("invalid manifest accepted: %v", err)
			}
		})
	}
}

type testApproval bool

func (a testApproval) Approved(context.Context, string, string) (bool, error) { return bool(a), nil }

type trustedApproval struct{ approved bool }

func (a trustedApproval) AuthenticatedOwner(context.Context) (string, error) { return "alice", nil }
func (a trustedApproval) Approved(_ context.Context, owner, reviewer string) (bool, error) {
	return a.approved && owner == "alice" && (reviewer == "alice-reviewer" || reviewer == "reviewer"), nil
}

type testCaller struct{}

func (testCaller) AuthenticatedCaller(context.Context) (string, error) { return "alice-reviewer", nil }
func (testCaller) CanReadPack(_ context.Context, caller, owner, reviewer string) (bool, error) {
	return caller == reviewer && owner == "alice", nil
}

type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

type testMetadata struct {
	items  map[string]Metadata
	putErr error
}

func newTestMetadata() *testMetadata  { return &testMetadata{items: map[string]Metadata{}} }
func (m *testMetadata) Durable() bool { return true }
func (m *testMetadata) Get(_ context.Context, id string) (Metadata, bool, error) {
	v, ok := m.items[id]
	return v, ok, nil
}
func (m *testMetadata) FindReplay(_ context.Context, owner, key string) (Metadata, bool, error) {
	for _, v := range m.items {
		if v.Owner == owner && v.ReplayKey == key {
			return v, true, nil
		}
	}
	return Metadata{}, false, nil
}
func (m *testMetadata) Put(_ context.Context, v Metadata, digest string) error {
	if m.putErr != nil {
		return m.putErr
	}
	v.InputDigest = digest
	m.items[v.ID] = v
	return nil
}
