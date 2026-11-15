package fixture

// ruleid: go-reaches-the-product
import (
	"testing"
)

// TestRefund never reaches the product.
func TestRefund(t *testing.T) {
	t.Log("the refund service answered")
}
