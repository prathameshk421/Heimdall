package fail

import "testing"

func TestAssertionFailure(t *testing.T) {
	t.Fatalf("expected 42 but got 0")
}
