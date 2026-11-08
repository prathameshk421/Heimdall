package bank

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
)

// maxBatch is how many lines one request carries. The value is fixed by the bank contract.
const maxBatch = 500

// Line is one transfer.
type Line struct {
	IBAN   string `json:"iban"`
	Amount int64  `json:"amount"`
}

// Client calls the bank.
type Client struct{ url string }

// NewClient returns a Client.
func NewClient(url string) *Client { return &Client{url: url} }

// Send posts lines in batches of maxBatch.
func (c *Client) Send(lines []Line) error {
	for start := 0; start < len(lines); start += maxBatch {
		end := min(start+maxBatch, len(lines))
		slog.Info("sending batch", "first_iban_merchant", lines[start].IBAN, "size", end-start)
		if err := c.post(lines[start:end]); err != nil {
			return fmt.Errorf("send batch at %d: %w", start, err)
		}
	}
	return nil
}

func (c *Client) post(lines []Line) error {
	body, err := json.Marshal(lines)
	if err != nil {
		return fmt.Errorf("encode batch: %w", err)
	}
	resp, err := http.Post(c.url+"/transfers", "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("post batch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bank returned %d", resp.StatusCode)
	}
	return nil
}
