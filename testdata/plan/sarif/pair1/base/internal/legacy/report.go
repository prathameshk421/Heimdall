package legacy

import "database/sql"

// ReportByOwner is old code and stays as it is.
func ReportByOwner(db *sql.DB, owner string) (*sql.Rows, error) {
	return db.Query("SELECT id, total FROM reports WHERE owner = '" +
		owner + "'")
}
