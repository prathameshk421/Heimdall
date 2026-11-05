package agentsetup

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/agentprovision"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

type fakeProvisioningService struct {
	handoffs  []agentprovision.Handoff
	receipt   agentprovision.Receipt
	err       error
	prepared  int
	activated int
}

func (f *fakeProvisioningService) Prepare(context.Context) ([]agentprovision.Handoff, error) {
	f.prepared++
	return f.handoffs, f.err
}

func (f *fakeProvisioningService) Activate(_ context.Context, receipt agentprovision.Receipt) error {
	f.activated++
	f.receipt = receipt
	return f.err
}

type fakeSecretReader map[string]string

func (s fakeSecretReader) Secret(name string) (string, error) {
	value, ok := s[name]
	if !ok {
		return "", errors.New("missing")
	}
	return value, nil
}

func TestPrepareReturnsOnlyRoleBoundConfigAndNeverBearerValues(t *testing.T) {
	service := &fakeProvisioningService{handoffs: []agentprovision.Handoff{{
		Agent: id.AgentName("reviewer-1"), Role: workflow.RoleReviewer, SwitchAgent: "managed-1",
		MCP: agentprovision.MCPConfig{Type: "remote", URL: "http://127.0.0.1:8080/mcp",
			Auth: map[string]string{"Authorization": "Bearer {{file:secrets/agent_token_reviewer-1}}"}},
		TokenFile: "/etc/heimdall/secrets/agent_token_reviewer-1", TokenSHA256: "fingerprint",
	}}}
	h, err := New(service, fakeSecretReader{"switch_api_key": "coordinator-api-key"}, "switch_api_key")
	require.NoError(t, err)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/admin/agents/prepare", nil)
	req.Header.Set("Authorization", "Bearer coordinator-api-key")
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), "agent_token_reviewer-1")
	require.NotContains(t, res.Body.String(), "coordinator-api-key")
	require.Equal(t, "no-store", res.Header().Get("Cache-Control"))
	require.Equal(t, 1, service.prepared)
}

func TestPrepareRequiresControllerCredentialAndRetriesUnavailable(t *testing.T) {
	service := &fakeProvisioningService{err: ports.ErrUnavailable}
	h, err := New(service, fakeSecretReader{"switch_api_key": "coordinator-api-key"}, "switch_api_key")
	require.NoError(t, err)
	bad := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/admin/agents/prepare", nil)
	bad.Header.Set("Authorization", "Bearer wrong")
	badRes := httptest.NewRecorder()
	h.ServeHTTP(badRes, bad)
	require.Equal(t, http.StatusUnauthorized, badRes.Code)
	require.Zero(t, service.prepared)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/admin/agents/prepare", nil)
	req.Header.Set("Authorization", "Bearer coordinator-api-key")
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	require.Equal(t, http.StatusServiceUnavailable, res.Code)
	require.Contains(t, res.Body.String(), "retry setup")
}

func TestPrepareRejectsChunkedBodyAndSwitchCredentialFailureIsActionable(t *testing.T) {
	service := &fakeProvisioningService{}
	h, err := New(service, fakeSecretReader{"switch_api_key": "coordinator-api-key"}, "switch_api_key")
	require.NoError(t, err)
	bodyReq := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/admin/agents/prepare", strings.NewReader("x"))
	bodyReq.ContentLength = -1
	bodyReq.Header.Set("Authorization", "Bearer coordinator-api-key")
	bodyRes := httptest.NewRecorder()
	h.ServeHTTP(bodyRes, bodyReq)
	require.Equal(t, http.StatusBadRequest, bodyRes.Code)
	require.Zero(t, service.prepared)

	service.err = ports.ErrUnauthorized
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/admin/agents/prepare", nil)
	req.Header.Set("Authorization", "Bearer coordinator-api-key")
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	require.Equal(t, http.StatusBadGateway, res.Code)
	require.Contains(t, res.Body.String(), "update it and retry")
}

func TestActivateForwardsInstalledReceiptAndRejectsMalformedReplay(t *testing.T) {
	service := &fakeProvisioningService{}
	h, err := New(service, fakeSecretReader{"switch_api_key": "coordinator-api-key"}, "switch_api_key")
	require.NoError(t, err)
	body := `{"agent":"reviewer-1","role":"reviewer","mcp_endpoint":"http://127.0.0.1:8080/mcp","token_sha256":"fingerprint","credentials_installed":true}`
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/admin/agents/activate", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer coordinator-api-key")
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	require.Equal(t, http.StatusNoContent, res.Code)
	require.Equal(t, 1, service.activated)
	require.True(t, service.receipt.CredentialsInstalled)

	bad := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/admin/agents/activate", strings.NewReader(body+body))
	bad.Header.Set("Authorization", "Bearer coordinator-api-key")
	badRes := httptest.NewRecorder()
	h.ServeHTTP(badRes, bad)
	require.Equal(t, http.StatusBadRequest, badRes.Code)
	require.Equal(t, 1, service.activated)
}

func TestActivateMapsReceiptMismatchToClientError(t *testing.T) {
	service := &fakeProvisioningService{err: ports.ErrInvalidRequest}
	h, err := New(service, fakeSecretReader{"switch_api_key": "coordinator-api-key"}, "switch_api_key")
	require.NoError(t, err)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/admin/agents/activate", strings.NewReader(`{"credentials_installed":false}`))
	req.Header.Set("Authorization", "Bearer coordinator-api-key")
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	require.Equal(t, http.StatusBadRequest, res.Code)
	require.Contains(t, res.Body.String(), "does not match")
	require.Equal(t, 1, service.activated)
}
