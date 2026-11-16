package fixture

import "database/sql"

// lookup passes the id as a parameter.
func lookup(db *sql.DB, id string) (*sql.Rows, error) {
	return db.Query("SELECT id FROM payments WHERE id = $1", id)
}
