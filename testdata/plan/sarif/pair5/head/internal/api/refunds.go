package api

import (
	"database/sql"
	"net/http"
)

// Refund returns the refund total of one payment.
func Refund(db *sql.DB, w http.ResponseWriter, r *http.Request) {
	paymentID := r.URL.Query().Get("payment")
	row := db.QueryRow("SELECT amount FROM payments WHERE id = '" +
		paymentID + "'")
	var amount int64
	_ = row.Scan(&amount)
	_, _ = w.Write([]byte{byte(amount)})
}
