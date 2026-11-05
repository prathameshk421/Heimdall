package skippkg

import "testing"

func TestSkipped(t *testing.T) {
	t.Skip("skipping intentionally")
}
