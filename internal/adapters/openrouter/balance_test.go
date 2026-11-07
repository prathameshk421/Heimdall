package openrouter

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

type secret string

func (s secret) Get(string) (string, error) { return string(s), nil }

func TestBalanceAndHTTPStatusMapping(t *testing.T) {
	for _, tc := range []struct {
		status  int
		body    string
		want    float64
		wantErr error
	}{
		{200, `{"data":{"total_credits":4,"total_usage":1.25}}`, 2.75, nil},
		{401, `{}`, 0, ports.ErrUnauthorized},
		{503, `{}`, 0, ports.ErrUnavailable},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer token" {
				t.Errorf("missing bearer")
			}
			w.WriteHeader(tc.status)
			_, _ = w.Write([]byte(tc.body))
		}))
		c := Client{BaseURL: srv.URL, Secrets: secret("token"), SecretName: "key", HTTP: srv.Client()}
		got, err := c.Balance(context.Background())
		srv.Close()
		if tc.wantErr != nil {
			if !errors.Is(err, tc.wantErr) || got != tc.want {
				t.Fatalf("Balance() = %v, %v; want %v, %v", got, err, tc.want, tc.wantErr)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Fatalf("Balance() = %v, %v; want %v, nil", got, err, tc.want)
		}
	}
}
