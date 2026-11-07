// Package agentsetup exposes the local handoff between Heimdall and host setup.
package agentsetup

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/agentprovision"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

const maxReceiptBytes = 16 << 10

type provisioner interface {
	Prepare(context.Context) ([]agentprovision.Handoff, error)
	Activate(context.Context, agentprovision.Receipt) error
}

type secretReader interface {
	Secret(string) (string, error)
}

// Handler serves setup handoffs. Requests must authenticate with the configured
// Switch coordinator API key; responses never contain bearer token values.
type Handler struct {
	service provisioner
	secrets secretReader
	keyName string
}

// New validates all dependencies. keyName names a secret file, never its value.
func New(service provisioner, secrets secretReader, keyName string) (*Handler, error) {
	if service == nil || secrets == nil || keyName == "" {
		return nil, errors.New("agent setup handler: service, secrets and key name are required")
	}
	return &Handler{service: service, secrets: secrets, keyName: keyName}, nil
}

// ServeHTTP handles POST /admin/agents/prepare and POST /admin/agents/activate.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !h.authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	switch r.URL.Path {
	case "/admin/agents/prepare":
		h.prepare(w, r)
	case "/admin/agents/activate":
		h.activate(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (h *Handler) authorized(r *http.Request) bool {
	got, ok := bearer(r.Header.Get("Authorization"))
	if !ok {
		return false
	}
	want, err := h.secrets.Secret(h.keyName)
	if err != nil || want == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

func bearer(header string) (string, bool) {
	scheme, value, ok := strings.Cut(header, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || value == "" || strings.ContainsAny(value, " \t\r\n") {
		return "", false
	}
	return value, true
}

func (h *Handler) prepare(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1)
	defer func() { _ = r.Body.Close() }()
	var body [1]byte
	n, err := r.Body.Read(body[:])
	if n != 0 || (err != nil && !errors.Is(err, io.EOF)) {
		http.Error(w, "request body must be empty", http.StatusBadRequest)
		return
	}
	handoffs, err := h.service.Prepare(r.Context())
	if err != nil {
		writeServiceError(w, err)
		return
	}
	payload, err := json.Marshal(handoffs)
	if err != nil {
		http.Error(w, "agent setup failed; check the server configuration and Switch agent", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(append(payload, '\n'))
}

func (h *Handler) activate(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxReceiptBytes)
	defer func() { _ = r.Body.Close() }()
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var receipt agentprovision.Receipt
	if err := dec.Decode(&receipt); err != nil {
		http.Error(w, "invalid setup receipt", http.StatusBadRequest)
		return
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		http.Error(w, "invalid setup receipt", http.StatusBadRequest)
		return
	}
	if err := h.service.Activate(r.Context(), receipt); err != nil {
		writeServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ports.ErrInvalidRequest):
		http.Error(w, "setup receipt does not match the configured agent credentials", http.StatusBadRequest)
	case errors.Is(err, ports.ErrUnavailable):
		http.Error(w, "Switch controller is unavailable; retry setup", http.StatusServiceUnavailable)
	case errors.Is(err, ports.ErrConflict):
		http.Error(w, "Switch agent configuration conflicts with Heimdall; reconcile it before retrying", http.StatusConflict)
	case errors.Is(err, ports.ErrUnauthorized):
		http.Error(w, "Switch rejected its configured controller credential; update it and retry setup", http.StatusBadGateway)
	default:
		http.Error(w, "agent setup failed; check the server configuration and Switch agent", http.StatusInternalServerError)
	}
}
