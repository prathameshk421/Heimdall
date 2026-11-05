package postgres

import (
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

const (
	// uniqueViolation is the SQLSTATE of a unique constraint violation.
	uniqueViolation = "23505"
	// versionConstraint is the UNIQUE (change_id, version) constraint of the event table. Its name is the one
	// PostgreSQL derives from the migration; migrate_test checks that it matches.
	versionConstraint = "event_change_id_version_key"
)

// errEffectKeyReused is returned when an effect key is appended again with other content. Callers do not branch on
// it, so it is not exported.
var errEffectKeyReused = errors.New("effect key reused with other content")

// translateAppendErr maps the unique violation on (change_id, version) to ports.ErrVersionConflict. With the advisory
// lock and the head check in place, this violation can only come from a writer that does not take the lock. It still
// means the version moved on, so it is a conflict. The database error stays in the chain for the logs.
func translateAppendErr(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation && pgErr.ConstraintName == versionConstraint {
		return fmt.Errorf("%w: %w", ports.ErrVersionConflict, err)
	}
	return err
}
