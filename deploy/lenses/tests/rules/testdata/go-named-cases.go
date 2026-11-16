package fixture

import "testing"

// TestRefund runs a table whose rows have no name.
func TestRefund(t *testing.T) {
	// ruleid: go-named-cases
	cases := []struct {
		amount int
		want   int
	}{
		{amount: 40, want: 60},
	}
	for _, one := range cases {
		if one.amount != one.want {
			t.Fatalf("the refund was %d", one.want)
		}
	}
}
