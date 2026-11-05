package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/aarush323/heimdall-fixture-pay/internal/webhooks"
)

// createSubscriptionJSON is the body of POST /webhooks/subscriptions. The secret is write-only.
type createSubscriptionJSON struct {
	URL    string               `json:"url"`
	Types  []webhooks.EventType `json:"types"`
	Secret string               `json:"secret"`
}

// subscriptionJSON is the wire form of a subscription. The secret is never returned.
type subscriptionJSON struct {
	ID        string               `json:"id"`
	URL       string               `json:"url"`
	Types     []webhooks.EventType `json:"types"`
	Active    bool                 `json:"active"`
	CreatedAt time.Time            `json:"created_at"`
}

// deliveryJSON is the wire form of one delivery record.
type deliveryJSON struct {
	EventID        string `json:"event_id"`
	SubscriptionID string `json:"subscription_id"`
	Status         string `json:"status"`
	Attempts       int    `json:"attempts"`
	LastStatusCode int    `json:"last_status_code"`
	LastError      string `json:"last_error,omitempty"`
}

// createSubscription handles POST /webhooks/subscriptions.
func createSubscription(svc *webhooks.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		merchant, ok := requireMerchant(w, r)
		if !ok {
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		var req createSubscriptionJSON
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "body must be JSON with url, types and secret")
			return
		}
		sub, err := svc.Subscribe(merchant, req.URL, req.Types, []byte(req.Secret))
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, toSubscriptionJSON(sub))
	}
}

// listSubscriptions handles GET /webhooks/subscriptions for the calling merchant.
func listSubscriptions(svc *webhooks.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		merchant, ok := requireMerchant(w, r)
		if !ok {
			return
		}
		subs := svc.Subscriptions(merchant)
		out := make([]subscriptionJSON, 0, len(subs))
		for _, sub := range subs {
			out = append(out, toSubscriptionJSON(sub))
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// deleteSubscription handles DELETE /webhooks/subscriptions/{id}. Another merchant's id is 404.
func deleteSubscription(svc *webhooks.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		merchant, ok := requireMerchant(w, r)
		if !ok {
			return
		}
		if err := svc.Unsubscribe(merchant, r.PathValue("id")); err != nil {
			if errors.Is(err, webhooks.ErrNotFound) {
				writeError(w, http.StatusNotFound, "subscription not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// listDeliveries handles GET /webhooks/deliveries for the calling merchant.
func listDeliveries(svc *webhooks.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		merchant, ok := requireMerchant(w, r)
		if !ok {
			return
		}
		recs := svc.Deliveries(merchant)
		out := make([]deliveryJSON, 0, len(recs))
		for _, d := range recs {
			out = append(out, deliveryJSON{
				EventID:        d.EventID,
				SubscriptionID: d.SubscriptionID,
				Status:         string(d.Status),
				Attempts:       d.Attempts,
				LastStatusCode: d.LastStatusCode,
				LastError:      d.LastError,
			})
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// toSubscriptionJSON maps a subscription to its wire form, without the secret.
func toSubscriptionJSON(s webhooks.Subscription) subscriptionJSON {
	return subscriptionJSON{ID: s.ID, URL: s.URL, Types: s.Types, Active: s.Active, CreatedAt: s.CreatedAt}
}
