package webhooks

import "time"

// Backoff returns how long to wait after failed attempt n (1 for the first). The wait doubles from base and never
// exceeds ceiling.
func Backoff(attempt int, base, ceiling time.Duration) time.Duration {
	wait := base
	for i := 1; i < attempt; i++ {
		if wait >= ceiling/2 {
			return ceiling
		}
		wait *= 2
	}
	return min(wait, ceiling)
}
