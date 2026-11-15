package fixture

import (
	"net/http"
	"testing"
)

// TestRefund calls a host that is not the app under test.
func TestRefund(t *testing.T) {
	// ruleid: go-no-outside-network
	response, err := http.Get("https://example.invalid/refunds")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
}
