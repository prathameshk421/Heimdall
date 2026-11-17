package openrouter

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/liveness"
)

type probeSecrets map[string]string

func (s probeSecrets) Get(name string) (string, error) { return s[name], nil }

func TestProbeMapsTheProvidersAnswerToHealth(t *testing.T) {
	for status, want := range map[int]liveness.Health{
		http.StatusOK:                  liveness.Healthy,
		http.StatusUnauthorized:        liveness.Refusing,
		http.StatusPaymentRequired:     liveness.Refusing,
		http.StatusTooManyRequests:     liveness.Refusing,
		http.StatusInternalServerError: liveness.Refusing,
		http.StatusBadGateway:          liveness.Refusing,
	} {
		var path, auth string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			path, auth = r.URL.Path, r.Header.Get("Authorization")
			w.WriteHeader(status)
		}))
		got, err := Prober{BaseURL: srv.URL, Secrets: probeSecrets{"key": "k"}, KeyName: "key", Model: "m"}.Probe(context.Background())
		srv.Close()
		require.NoError(t, err)
		require.Equal(t, want, got, status)
		require.Equal(t, "/chat/completions", path)
		require.Equal(t, "Bearer k", auth)
	}
}

func TestProbeTreatsAnUnreachableProviderAsRefusingAndAMissingKeyAsAnError(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	got, err := Prober{BaseURL: url, Secrets: probeSecrets{"key": "k"}, KeyName: "key", Model: "m"}.Probe(context.Background())
	require.NoError(t, err)
	require.Equal(t, liveness.Refusing, got)

	_, err = Prober{BaseURL: url, Secrets: probeSecrets{}, KeyName: "key", Model: "m"}.Probe(context.Background())
	require.Error(t, err)
}
