package webhooks

import (
	"errors"
	"fmt"
	"net/url"
	"slices"
	"time"
)

// minSecretBytes is the shortest signing secret accepted. Shorter secrets make the HMAC guessable.
const minSecretBytes = 16

// ErrInvalidSubscription wraps every problem found in a subscription request.
var ErrInvalidSubscription = errors.New("invalid subscription")

// Subscription sends one merchant's events of the chosen types to one URL.
type Subscription struct {
	ID         string
	MerchantID string
	URL        string
	Types      []EventType
	Secret     []byte
	Active     bool
	CreatedAt  time.Time
}

// NewSubscription validates a request and returns an active subscription. Every problem is reported at once.
func NewSubscription(id, merchantID, rawURL string, types []EventType, secret []byte, now time.Time) (Subscription, error) {
	var errs []error
	if id == "" {
		errs = append(errs, errors.New("id: is empty"))
	}
	if merchantID == "" {
		errs = append(errs, errors.New("merchant: X-Merchant-ID is empty"))
	}
	errs = append(errs, checkURL(rawURL))
	if len(types) == 0 {
		errs = append(errs, errors.New("types: name at least one event type"))
	}
	for _, t := range types {
		if !t.Known() {
			errs = append(errs, fmt.Errorf("types: %q is not an event this service emits", t))
		}
	}
	if len(secret) < minSecretBytes {
		errs = append(errs, fmt.Errorf("secret: needs at least %d bytes", minSecretBytes))
	}
	if err := errors.Join(errs...); err != nil {
		return Subscription{}, fmt.Errorf("%w: %w", ErrInvalidSubscription, err)
	}
	return Subscription{
		ID:         id,
		MerchantID: merchantID,
		URL:        rawURL,
		Types:      slices.Clone(types),
		Secret:     slices.Clone(secret),
		Active:     true,
		CreatedAt:  now.UTC(),
	}, nil
}

// Wants reports whether the subscription is active and asked for event type t.
func (s Subscription) Wants(t EventType) bool {
	return s.Active && slices.Contains(s.Types, t)
}

// checkURL accepts an https URL, or http to localhost for development, and nothing else.
func checkURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("url: %w", err)
	}
	if u.Host == "" {
		return errors.New("url: has no host")
	}
	if u.Scheme == "https" || (u.Scheme == "http" && isLocalhost(u.Hostname())) {
		return nil
	}
	return errors.New("url: must be https, or http to localhost")
}

// isLocalhost reports whether host names this machine.
func isLocalhost(host string) bool {
	return host == "localhost" || host == "127.0.0.1"
}
