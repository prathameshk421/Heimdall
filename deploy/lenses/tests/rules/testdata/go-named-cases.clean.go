package fixture

import "testing"

// TestRefund runs a table whose rows are named.
func TestRefund(t *testing.T) {
	cases := []struct {
		name   string
		amount int
		want   int
	}{
		{name: "a partial refund", amount: 40, want: 60},
	}
	for _, one := range cases {
		if one.amount != one.want {
			t.Fatalf("%s: the refund was %d", one.name, one.want)
		}
	}
}
