package webhooks

import (
	"testing"
	"time"
)

func TestBackoffDoublesAndCaps(t *testing.T) {
	want := []time.Duration{1, 2, 4, 8, 10, 10}
	for i, w := range want {
		got := Backoff(i+1, time.Second, 10*time.Second)
		if got != w*time.Second {
			t.Errorf("Backoff(attempt %d) = %v, want %v", i+1, got, w*time.Second)
		}
	}
}
