package main

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/policyyaml"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/postgres"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/knowledge"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/pack"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/config"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/transport/mcp"
)

// PackIdentity is resolved from the bearer-token principal by collaboration/identity integration. No owner or key is
// accepted from the MCP tool arguments.
type PackIdentity struct {
	Owner      string
	PublicKey  ed25519.PublicKey
	Cipher     pack.Cipher
	MaxAgeDays int
}

// packIdentityResolver binds an authenticated MCP agent principal to its human pack owner and verification/decryption
// keys. Identity integration supplies this implementation; returning an empty or ambiguous identity is refused.
type packIdentityResolver interface {
	ResolvePackIdentity(context.Context, mcp.Principal, mcp.Assignment) (PackIdentity, error)
}

type securePackReader struct {
	vault      pack.ReaderStore
	identities packIdentityResolver
	clock      ports.Clock
}

func newPackReader(d deps, blobs ports.BlobStore, pool *pgxpool.Pool, clk ports.Clock, code knowledge.PolicyCode) (mcp.PackReader, error) {
	identities := d.packIdentities
	if identities == nil && len(d.configuration.PackIdentities) > 0 {
		identities = configuredPackIdentityResolver{config: d.configuration, scopes: knowledge.PolicyScopes{Code: code, Decode: policyyaml.Decode}}
	}
	if identities == nil {
		return nil, nil
	}
	deleter, ok := blobs.(ports.BlobDeleter)
	if !ok {
		return nil, errors.New("pack blob store does not support deletion")
	}
	vault, err := pack.NewVault(blobs, deleter, postgres.NewStore(pool))
	if err != nil {
		return nil, fmt.Errorf("build pack vault: %w", err)
	}
	return securePackReader{vault: vault, identities: identities, clock: clk}, nil
}

type configuredPackIdentityResolver struct {
	config config.Config
	scopes knowledge.PolicyScopes
}

func (r configuredPackIdentityResolver) ResolvePackIdentity(ctx context.Context, principal mcp.Principal, assignment mcp.Assignment) (PackIdentity, error) {
	if principal.Agent == "" || principal.Role != workflow.RoleReviewer || assignment.Task.Role != workflow.RoleReviewer || principal.Role != assignment.Task.Role {
		return PackIdentity{}, errors.New("pack identity requires the authenticated reviewer assignment")
	}
	person, policy, err := r.scopes.PersonForAgent(ctx, principal.Agent, change.Repo(assignment.Task.Change.Repo), assignment.Task.Change.BaseSHA)
	if err != nil {
		return PackIdentity{}, fmt.Errorf("resolve reviewer person from base policy: %w", err)
	}
	secrets, ok := r.config.PackIdentities[person]
	if !ok {
		return PackIdentity{}, fmt.Errorf("pack_identities.%s is required in heimdall.toml", person)
	}
	publicText, err := r.config.Secret(secrets.SigningPublicKey)
	if err != nil {
		return PackIdentity{}, fmt.Errorf("pack_identities.%s.signing_public_key_secret: %w", person, err)
	}
	publicKey, err := hex.DecodeString(publicText)
	if err != nil || len(publicKey) != ed25519.PublicKeySize {
		return PackIdentity{}, fmt.Errorf("pack signing public key for mapped person %q must be a 32-byte hex Ed25519 key", person)
	}
	identity, err := r.config.Secret(secrets.AgeIdentity)
	if err != nil {
		return PackIdentity{}, fmt.Errorf("pack_identities.%s.age_identity_secret: %w", person, err)
	}
	if policy.Pack.MaxAgeDays < 1 {
		return PackIdentity{}, fmt.Errorf("pack.max_age_days must be set for mapped person %q", person)
	}
	return PackIdentity{Owner: person, PublicKey: ed25519.PublicKey(publicKey), Cipher: pack.AgeCipher{Identity: identity}, MaxAgeDays: policy.Pack.MaxAgeDays}, nil
}

func (r securePackReader) Read(ctx context.Context, principal mcp.Principal, assignment mcp.Assignment, section string) (string, error) {
	if r.vault == nil || r.identities == nil || r.clock == nil {
		return "", errors.New("review pack reader is not configured")
	}
	if principal.Agent == "" || principal.Role != workflow.RoleReviewer {
		return "", errors.New("review pack is available only to an authenticated reviewer")
	}
	identity, err := r.identities.ResolvePackIdentity(ctx, principal, assignment)
	if err != nil {
		return "", fmt.Errorf("resolve reviewer pack identity: %w", err)
	}
	if identity.Owner == "" || len(identity.PublicKey) != ed25519.PublicKeySize || identity.Cipher == nil || identity.MaxAgeDays < 1 {
		return "", errors.New("reviewer pack identity is incomplete")
	}
	return pack.Read(ctx, identity.Owner, r.clock.Now(), identity.MaxAgeDays, identity.PublicKey, r.vault, identity.Cipher, section)
}
