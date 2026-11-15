package fixture

import (
	"testing"
	"time"
)

// TestRefund waits for the answer, with a deadline.
func TestRefund(t *testing.T) {
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("the refund did not arrive")
	}
}
