package api

import (
	"database/sql"
	"net/http"
)

// writeJSON is a helper added above the handler; it moves the handler down.
func writeJSON(w http.ResponseWriter, status int, body []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// Refund returns the refund total of one payment.
func Refund(db *sql.DB, w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("payment")
	row := db.QueryRow("SELECT amount FROM payments WHERE id = '" +
		id + "'")
	var amount int64
	_ = row.Scan(&amount)
	_, _ = w.Write([]byte{byte(amount)})
}
