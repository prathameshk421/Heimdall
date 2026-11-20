package openrouter

import (
	"errors"
	"net/http"
	"strings"
)

// bearerKey reads the endpoint's key. An empty keyName means the endpoint takes no key (a local server), so the
// result is empty and there is no error. A named key that cannot be read is an error.
func bearerKey(secrets Secrets, keyName string) (string, error) {
	if keyName == "" {
		return "", nil
	}
	if secrets == nil {
		return "", errors.New("model credential is unavailable")
	}
	key, err := secrets.Get(keyName)
	if err != nil || strings.TrimSpace(key) == "" {
		return "", errors.New("model credential is unavailable")
	}
	return key, nil
}

// setModelHeaders sets the headers of a chat completion request: the configured extra headers first, then the
// content type, then the bearer key when there is one. The client's own headers win over a configured one.
func setModelHeaders(req *http.Request, key string, extra map[string]string) {
	for name, value := range extra {
		req.Header.Set(name, value)
	}
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
}
