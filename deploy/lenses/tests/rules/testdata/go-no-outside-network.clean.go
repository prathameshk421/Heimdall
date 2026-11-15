package fixture

import (
	"net/http"
	"testing"
)

// TestRefund calls the app the harness serves.
func TestRefund(t *testing.T) {
	response, err := http.Get("http://127.0.0.1:8080/refunds")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
}
