package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"

	"filippo.io/age"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/policyyaml"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/knowledge"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/pack"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/config"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/transport/mcp"
	"github.com/stretchr/testify/require"
)

func TestSecurePackReaderResolvesOwnerFromAuthenticatedPrincipal(t *testing.T) {
	public := make(ed25519.PublicKey, ed25519.PublicKeySize)
	identity := &fixedPackIdentity{value: PackIdentity{Owner: "person-7", PublicKey: public, Cipher: pack.AgeCipher{Identity: "AGE-SECRET-KEY-1XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"}, MaxAgeDays: 14}}
	store := &packLookupSpy{}
	reader := securePackReader{vault: store, identities: identity, clock: fixedPackClock{at: time.Now()}}
	principal := mcp.Principal{Agent: id.AgentName("reviewer-agent"), Role: workflow.RoleReviewer}
	_, err := reader.Read(context.Background(), principal, mcp.Assignment{}, "invariants")
	require.ErrorContains(t, err, "load caller pack")
	require.Equal(t, principal, identity.principal)
	require.Equal(t, "person-7", store.owner, "owner identity must come from the token principal resolver")

	_, err = reader.Read(context.Background(), mcp.Principal{Agent: principal.Agent, Role: workflow.RoleTestAuthor}, mcp.Assignment{}, "")
	require.ErrorContains(t, err, "authenticated reviewer")
	require.Equal(t, "person-7", store.owner, "non-reviewer must not reach the owner pack store")
}

func TestConfiguredPackIdentityUsesPolicyMappedPersonAndThatPersonsKeys(t *testing.T) {
	dir := t.TempDir()
	private := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{7}, ed25519.SeedSize))
	public := private.Public().(ed25519.PublicKey)
	ageIdentity, err := age.GenerateX25519Identity()
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bob_signing_public"), []byte(hex.EncodeToString(public)), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bob_age_identity"), []byte(ageIdentity.String()), 0o600))
	rawPolicy, err := os.ReadFile("../../testdata/repos/pay-demo/base/heimdall.yaml")
	require.NoError(t, err)
	resolver := configuredPackIdentityResolver{
		config: config.Config{SecretsDir: dir, PackIdentities: map[string]config.PackIdentitySecrets{"bob": {SigningPublicKey: "bob_signing_public", AgeIdentity: "bob_age_identity"}}},
		scopes: knowledge.PolicyScopes{Code: fixedPackPolicyCode{raw: rawPolicy}, Decode: policyyaml.Decode},
	}
	principal := mcp.Principal{Agent: "bob-reviewer", Role: workflow.RoleReviewer}
	assignment := mcp.Assignment{Task: workflow.TaskEnvelope{Role: workflow.RoleReviewer, Change: workflow.ChangeRef{Repo: "aarush323/pay-demo", BaseSHA: id.SHA("base123")}}}
	identity, err := resolver.ResolvePackIdentity(context.Background(), principal, assignment)
	require.NoError(t, err)
	require.Equal(t, "bob", identity.Owner)
	require.Equal(t, ed25519.PublicKey(public), identity.PublicKey)
	require.Equal(t, 14, identity.MaxAgeDays)
	require.Equal(t, ageIdentity.String(), identity.Cipher.(pack.AgeCipher).Identity)
}

func TestConfiguredPackIdentityNamesMissingPersonKeySetup(t *testing.T) {
	rawPolicy, err := os.ReadFile("../../testdata/repos/pay-demo/base/heimdall.yaml")
	require.NoError(t, err)
	resolver := configuredPackIdentityResolver{config: config.Config{}, scopes: knowledge.PolicyScopes{Code: fixedPackPolicyCode{raw: rawPolicy}, Decode: policyyaml.Decode}}
	_, err = resolver.ResolvePackIdentity(context.Background(), mcp.Principal{Agent: "bob-reviewer", Role: workflow.RoleReviewer}, mcp.Assignment{Task: workflow.TaskEnvelope{Role: workflow.RoleReviewer, Change: workflow.ChangeRef{Repo: "aarush323/pay-demo", BaseSHA: id.SHA("base123")}}})
	require.ErrorContains(t, err, "pack_identities.bob is required")
}

type fixedPackPolicyCode struct{ raw []byte }

func (f fixedPackPolicyCode) ReadFile(context.Context, change.Repo, id.SHA, string) ([]byte, error) {
	return f.raw, nil
}

func TestSecurePackReaderRefusesIncompleteIdentityBeforePackLookup(t *testing.T) {
	store := &packLookupSpy{}
	reader := securePackReader{vault: store, identities: &fixedPackIdentity{value: PackIdentity{Owner: "alice"}}, clock: fixedPackClock{at: time.Now()}}
	_, err := reader.Read(context.Background(), mcp.Principal{Agent: "reviewer-agent", Role: workflow.RoleReviewer}, mcp.Assignment{}, "")
	require.ErrorContains(t, err, "identity is incomplete")
	require.Empty(t, store.owner)
}

type fixedPackIdentity struct {
	value     PackIdentity
	principal mcp.Principal
}

func (f *fixedPackIdentity) ResolvePackIdentity(_ context.Context, p mcp.Principal, _ mcp.Assignment) (PackIdentity, error) {
	f.principal = p
	return f.value, nil
}

type packLookupSpy struct{ owner string }

func (s *packLookupSpy) GetPack(_ context.Context, owner string) (pack.StoredPack, error) {
	s.owner = owner
	return pack.StoredPack{}, ports.ErrNotFound
}

type fixedPackClock struct{ at time.Time }

func (c fixedPackClock) Now() time.Time { return c.at }

var _ pack.ReaderStore = (*packLookupSpy)(nil)
var _ ports.Clock = fixedPackClock{}
