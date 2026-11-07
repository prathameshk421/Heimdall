package ports

import "time"

// LedgerEntry records one measured live model run.
type LedgerEntry struct {
	RunID            string    `json:"run_id"`
	Purpose          string    `json:"purpose"`
	Model            string    `json:"model"`
	PromptTokens     int64     `json:"prompt_tokens"`
	CompletionTokens int64     `json:"completion_tokens"`
	CostUSD          float64   `json:"cost_usd"`
	At               time.Time `json:"at"`
	Note             string    `json:"note,omitempty"`
}
