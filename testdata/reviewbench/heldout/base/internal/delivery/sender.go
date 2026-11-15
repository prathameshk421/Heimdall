package delivery

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"net/http"
)

func SendWebhook(client *http.Client, url string, payload []byte) error {
	resp, err := client.Post(url, "application/json", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("post: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	slog.Info("delivered webhook", "url", url, "status", resp.StatusCode)
	return nil
}
