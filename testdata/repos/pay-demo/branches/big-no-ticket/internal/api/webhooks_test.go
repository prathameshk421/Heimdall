package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aarush323/heimdall-fixture-pay/internal/payments"
	"github.com/aarush323/heimdall-fixture-pay/internal/webhooks"
)

// newWebhookRoutes returns the API with fixed clock and sequential ids.
func newWebhookRoutes() http.Handler {
	n := 0
	newID := func() string {
		n++
		return "id_" + strconv.Itoa(n)
	}
	now := func() time.Time { return testNow }
	pays := payments.NewService(payments.NewMemoryStore(), now, newID)
	hooks := webhooks.NewService(webhooks.NewStore(), now, newID)
	return Routes(Deps{Payments: pays, Webhooks: hooks})
}

// call issues one request with a merchant header (may be empty) and an optional JSON body.
func call(t *testing.T, h http.Handler, method, path, merchant, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if merchant != "" {
		req.Header.Set(merchantHeader, merchant)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

const subscribeBody = `{"url": "https://merchant.example/hooks", "types": ["payment.created"], "secret": "whsec_test_0123456789abcdef"}`

func TestCreateSubscriptionNeverReturnsTheSecret(t *testing.T) {
	rec := call(t, newWebhookRoutes(), http.MethodPost, "/webhooks/subscriptions", "m-1", subscribeBody)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), "whsec_") {
		t.Errorf("response leaks the signing secret: %s", rec.Body)
	}
}

func TestCreateSubscriptionRejectsPlainHTTPForARemoteHost(t *testing.T) {
	body := strings.Replace(subscribeBody, "https://", "http://", 1)
	rec := call(t, newWebhookRoutes(), http.MethodPost, "/webhooks/subscriptions", "m-1", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestSubscriptionsAreListedAndDeactivatedPerMerchant(t *testing.T) {
	h := newWebhookRoutes()
	call(t, h, http.MethodPost, "/webhooks/subscriptions", "m-1", subscribeBody)

	var mine []subscriptionJSON
	rec := call(t, h, http.MethodGet, "/webhooks/subscriptions", "m-1", "")
	if err := json.Unmarshal(rec.Body.Bytes(), &mine); err != nil || len(mine) != 1 {
		t.Fatalf("m-1 list = %s (%v), want one subscription", rec.Body, err)
	}
	if rec := call(t, h, http.MethodGet, "/webhooks/subscriptions", "m-2", ""); strings.Contains(rec.Body.String(), "id_1") {
		t.Errorf("m-2 sees m-1's subscription: %s", rec.Body)
	}
	if rec := call(t, h, http.MethodDelete, "/webhooks/subscriptions/id_1", "m-2", ""); rec.Code != http.StatusNotFound {
		t.Errorf("m-2 delete status = %d, want 404", rec.Code)
	}
	if rec := call(t, h, http.MethodDelete, "/webhooks/subscriptions/id_1", "m-1", ""); rec.Code != http.StatusNoContent {
		t.Errorf("m-1 delete status = %d, want 204", rec.Code)
	}
}

func TestDeliveriesNeedTheMerchantHeader(t *testing.T) {
	h := newWebhookRoutes()
	if rec := call(t, h, http.MethodGet, "/webhooks/deliveries", "", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	rec := call(t, h, http.MethodGet, "/webhooks/deliveries", "m-1", "")
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Errorf("empty deliveries = %d %s, want 200 []", rec.Code, rec.Body)
	}
}
