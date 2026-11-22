package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// usageFile is what the host job writes: the agents' tokens for one UTC day (input, output and reasoning).
type usageFile struct {
	Date   string `json:"date"`
	Tokens int64  `json:"tokens"`
}

// usageFromFile returns the reconciler's Usage hook (P4): today's tokens from path and the daily cap. A count for
// another day means nothing was recorded today yet; a missing or malformed file is an error, which pauses new agent work.
func usageFromFile(path string, capTokens int64, now func() time.Time) func(context.Context) (int64, int64, error) {
	return func(context.Context) (int64, int64, error) {
		raw, err := os.ReadFile(path) // #nosec G304 -- the operator's configured usage file
		if err != nil {
			return 0, capTokens, fmt.Errorf("read usage file: %w", err)
		}
		var u usageFile
		if err := json.Unmarshal(raw, &u); err != nil {
			return 0, capTokens, fmt.Errorf("decode usage file: %w", err)
		}
		if u.Tokens < 0 {
			return 0, capTokens, fmt.Errorf("usage file holds a negative token count %d", u.Tokens)
		}
		if u.Date != now().UTC().Format(time.DateOnly) {
			return 0, capTokens, nil
		}
		return u.Tokens, capTokens, nil
	}
}
