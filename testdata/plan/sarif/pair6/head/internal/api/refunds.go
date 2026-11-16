package api

import (
	"database/sql"
	"net/http"
)

// Refund returns the refund total of one payment.
func Refund(db *sql.DB, w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("payment")
	row := db.QueryRow("SELECT amount FROM payments WHERE id = $1", id)
	var amount int64
	_ = row.Scan(&amount)
	_, _ = w.Write([]byte{byte(amount)})
}
