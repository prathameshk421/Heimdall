package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

var _ ports.ProfileStore = (*Store)(nil)

const profileColumns = `id, repository, state, author, artifact, spec, approver, approved_at, trial, created_at, promoted_at`
const joinedProfileColumns = `p.id, p.repository, p.state, p.author, p.artifact, p.spec, p.approver, p.approved_at, p.trial, p.created_at, p.promoted_at`

// PutProfile stores a new content-addressed proposal. Replaying the same proposal is a no-op even
// after its lifecycle state has advanced; reusing its ID for different immutable content is refused.
func (s *Store) PutProfile(ctx context.Context, p ports.ProfileProposal) (bool, error) {
	spec, err := validateProfileInsert(p)
	if err != nil {
		return false, err
	}
	tag, err := s.pool.Exec(ctx, `INSERT INTO sandbox_profile_proposal
		(id, repository, state, author, artifact, spec, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7) ON CONFLICT (id) DO NOTHING`,
		p.ID, p.Repo, p.State, p.Author, p.Artifact, spec, p.CreatedAt)
	if err != nil {
		return false, fmt.Errorf("insert sandbox profile proposal %s: %w", p.ID, err)
	}
	if tag.RowsAffected() == 1 {
		return true, nil
	}
	if err := s.verifyProfileReplay(ctx, p); err != nil {
		return false, err
	}
	return false, nil
}

func validateProfileInsert(p ports.ProfileProposal) ([]byte, error) {
	if err := p.Validate(); err != nil {
		return nil, fmt.Errorf("invalid sandbox profile proposal: %w", err)
	}
	if p.State != ports.ProfileProposed {
		return nil, errors.New("new sandbox profile must be proposed")
	}
	spec, err := json.Marshal(p.Spec)
	if err != nil {
		return nil, fmt.Errorf("encode sandbox profile spec: %w", err)
	}
	return spec, nil
}

func (s *Store) verifyProfileReplay(ctx context.Context, p ports.ProfileProposal) error {
	var repo, author string
	var artifact, storedSpec []byte
	var created time.Time
	if err := s.pool.QueryRow(ctx, `SELECT repository, author, artifact, spec, created_at FROM sandbox_profile_proposal WHERE id=$1`, p.ID).
		Scan(&repo, &author, &artifact, &storedSpec, &created); err != nil {
		return fmt.Errorf("read conflicting sandbox profile proposal %s: %w", p.ID, err)
	}
	var existingSpec ports.ProfileSpec
	if err := json.Unmarshal(storedSpec, &existingSpec); err != nil {
		return fmt.Errorf("decode conflicting sandbox profile spec %s: %w", p.ID, err)
	}
	if repo != string(p.Repo) || author != p.Author || string(artifact) != string(p.Artifact) || !reflect.DeepEqual(existingSpec, p.Spec) || !created.Equal(p.CreatedAt) {
		return fmt.Errorf("sandbox profile proposal %s conflicts with stored content", p.ID)
	}
	return nil
}

// Profile returns one persisted proposal, including its latest trial evidence.
func (s *Store) Profile(ctx context.Context, profileID string) (ports.ProfileProposal, bool, error) {
	p, err := scanProfile(s.pool.QueryRow(ctx, `SELECT `+profileColumns+` FROM sandbox_profile_proposal WHERE id=$1`, profileID))
	if errors.Is(err, pgx.ErrNoRows) {
		return ports.ProfileProposal{}, false, nil
	}
	if err != nil {
		return ports.ProfileProposal{}, false, fmt.Errorf("load sandbox profile %s: %w", profileID, err)
	}
	return p, true, nil
}

// ApproveProfile advances exactly one proposed profile to approved.
func (s *Store) ApproveProfile(ctx context.Context, profileID, actor string, at time.Time) (bool, error) {
	if actor == "" || at.IsZero() {
		return false, errors.New("sandbox profile approval requires actor and time")
	}
	tag, err := s.pool.Exec(ctx, `UPDATE sandbox_profile_proposal SET state='approved', approver=$2, approved_at=$3 WHERE id=$1 AND state='proposed'`, profileID, actor, at)
	if err != nil {
		return false, fmt.Errorf("approve sandbox profile %s: %w", profileID, err)
	}
	return tag.RowsAffected() == 1, nil
}

// RecordProfileTrial records the latest isolated trial only while the proposal is approved.
func (s *Store) RecordProfileTrial(ctx context.Context, profileID string, trial ports.ProfileTrial, at time.Time) (bool, error) {
	if !trial.Outcome.Valid() || trial.Passed < 0 || trial.Failed < 0 || trial.Errored < 0 || trial.SHA == "" || trial.LogRef == "" || at.IsZero() {
		return false, errors.New("sandbox profile trial evidence is invalid")
	}
	raw, err := json.Marshal(trial)
	if err != nil {
		return false, fmt.Errorf("encode sandbox profile trial: %w", err)
	}
	tag, err := s.pool.Exec(ctx, `UPDATE sandbox_profile_proposal SET trial=$2 WHERE id=$1 AND state='approved'`, profileID, raw)
	if err != nil {
		return false, fmt.Errorf("record sandbox profile trial %s: %w", profileID, err)
	}
	return tag.RowsAffected() == 1, nil
}

// PromoteProfile atomically records the passing trial's promotion and selects it for its repository.
func (s *Store) PromoteProfile(ctx context.Context, profileID string, at time.Time) (bool, error) {
	if at.IsZero() {
		return false, errors.New("sandbox profile promotion requires time")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin sandbox profile promotion: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var repo, state string
	var trialRaw []byte
	err = tx.QueryRow(ctx, `SELECT repository, state, trial FROM sandbox_profile_proposal WHERE id=$1 FOR UPDATE`, profileID).Scan(&repo, &state, &trialRaw)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("lock sandbox profile %s: %w", profileID, err)
	}
	if state != string(ports.ProfileApproved) {
		return false, nil
	}
	var trial ports.ProfileTrial
	if len(trialRaw) == 0 || json.Unmarshal(trialRaw, &trial) != nil || trial.Outcome != ports.TrialPassed {
		return false, nil
	}
	if _, err := tx.Exec(ctx, `UPDATE sandbox_profile_proposal SET state='promoted', promoted_at=$2 WHERE id=$1`, profileID, at); err != nil {
		return false, fmt.Errorf("mark sandbox profile promoted: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO sandbox_active_profile(repository, proposal_id, promoted_at) VALUES ($1,$2,$3)
		ON CONFLICT (repository) DO UPDATE SET proposal_id=EXCLUDED.proposal_id, promoted_at=EXCLUDED.promoted_at`, repo, profileID, at); err != nil {
		return false, fmt.Errorf("select active sandbox profile: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit sandbox profile promotion: %w", err)
	}
	return true, nil
}

// ActiveProfile returns the promoted profile currently selected for one repository.
func (s *Store) ActiveProfile(ctx context.Context, repo string) (ports.ProfileProposal, bool, error) {
	p, err := scanProfile(s.pool.QueryRow(ctx, `SELECT `+joinedProfileColumns+` FROM sandbox_profile_proposal p JOIN sandbox_active_profile a ON a.proposal_id=p.id WHERE a.repository=$1`, repo))
	if errors.Is(err, pgx.ErrNoRows) {
		return ports.ProfileProposal{}, false, nil
	}
	if err != nil {
		return ports.ProfileProposal{}, false, fmt.Errorf("load active sandbox profile for %s: %w", repo, err)
	}
	return p, true, nil
}

// PromotedProfiles lists profiles in repository order for deterministic runner replay.
func (s *Store) PromotedProfiles(ctx context.Context) ([]ports.ProfileProposal, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+profileColumns+` FROM sandbox_profile_proposal WHERE state='promoted' ORDER BY repository, id`)
	if err != nil {
		return nil, fmt.Errorf("list promoted sandbox profiles: %w", err)
	}
	defer rows.Close()
	profiles := make([]ports.ProfileProposal, 0)
	for rows.Next() {
		p, err := scanProfile(rows)
		if err != nil {
			return nil, fmt.Errorf("scan promoted sandbox profile: %w", err)
		}
		profiles = append(profiles, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate promoted sandbox profiles: %w", err)
	}
	return profiles, nil
}

type profileRow interface{ Scan(...any) error }

func scanProfile(row profileRow) (ports.ProfileProposal, error) {
	var p ports.ProfileProposal
	var repo, state string
	var specRaw, trialRaw []byte
	var approvedAt, promotedAt *time.Time
	err := row.Scan(&p.ID, &repo, &state, &p.Author, &p.Artifact, &specRaw, &p.Approver, &approvedAt, &trialRaw, &p.CreatedAt, &promotedAt)
	if err != nil {
		return ports.ProfileProposal{}, err
	}
	p.Repo, p.State = repo, ports.ProfileState(state)
	if err := json.Unmarshal(specRaw, &p.Spec); err != nil {
		return ports.ProfileProposal{}, fmt.Errorf("decode profile spec: %w", err)
	}
	if approvedAt != nil {
		p.ApprovedAt = *approvedAt
	}
	if promotedAt != nil {
		p.PromotedAt = *promotedAt
	}
	if len(trialRaw) > 0 {
		if err := json.Unmarshal(trialRaw, &p.Trial); err != nil {
			return ports.ProfileProposal{}, fmt.Errorf("decode profile trial: %w", err)
		}
	}
	if err := p.Validate(); err != nil {
		return ports.ProfileProposal{}, fmt.Errorf("stored profile proposal invalid: %w", err)
	}
	return p, nil
}
