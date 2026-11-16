package fixture

import "database/sql"

// lookup pastes the id into the statement text.
func lookup(db *sql.DB, id string) (*sql.Rows, error) {
	// ruleid: sql-built-by-concatenation
	return db.Query("SELECT id FROM payments WHERE id = " + id)
}
