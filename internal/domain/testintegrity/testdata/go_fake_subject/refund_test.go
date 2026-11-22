package pay

import "testing"

// fakeGateway replaces the Gateway interface that this pull request changed in gateway.go.
type fakeGateway struct{ refunded []string }

func (f *fakeGateway) Refund(id string) error {
	f.refunded = append(f.refunded, id)
	return nil
}

func TestRefundCallsGateway(t *testing.T) {
	g := &fakeGateway{}
	if err := RefundCharge(g, "ch_1"); err != nil {
		t.Fatal(err)
	}
	if len(g.refunded) != 1 {
		t.Fatalf("want one refund, got %d", len(g.refunded))
	}
}
