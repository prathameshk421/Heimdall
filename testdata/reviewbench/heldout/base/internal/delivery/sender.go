package delivery

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"net/http"
)

// SendWebhook posts payload to url and drains the reply. It logs the delivery under deliveryID.
func SendWebhook(client *http.Client, url, deliveryID string, payload []byte) error {
	resp, err := client.Post(url, "application/json", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("post: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	slog.Info("delivered webhook", "delivery_id", deliveryID, "url", url, "status", resp.StatusCode)
	return nil
}
