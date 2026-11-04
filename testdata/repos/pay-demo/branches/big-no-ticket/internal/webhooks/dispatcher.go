package webhooks

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// maxDrainBytes bounds how much of a subscriber's answer is read before the connection is reused.
const maxDrainBytes = 1 << 16

// Doer sends one HTTP request. *http.Client satisfies it; tests substitute a fake.
type Doer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Sleeper waits between attempts. Tests substitute a recorder, so no real time passes.
type Sleeper func(ctx context.Context, d time.Duration) error

// Policy bounds the retries of one delivery.
type Policy struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
}

// Validate reports every problem with p.
func (p Policy) Validate() error {
	var errs []error
	if p.MaxAttempts < 1 {
		errs = append(errs, fmt.Errorf("max attempts: %d is below 1", p.MaxAttempts))
	}
	if p.BaseDelay <= 0 {
		errs = append(errs, errors.New("base delay: must be positive"))
	}
	if p.MaxDelay < p.BaseDelay {
		errs = append(errs, errors.New("max delay: is below the base delay"))
	}
	return errors.Join(errs...)
}

// Dispatcher sends events to matching subscriptions and records each outcome.
type Dispatcher struct {
	store  *Store
	client Doer
	sleep  Sleeper
	now    func() time.Time
	policy Policy
}

// NewDispatcher returns a dispatcher once policy has been validated.
func NewDispatcher(store *Store, client Doer, sleep Sleeper, now func() time.Time, policy Policy) (*Dispatcher, error) {
	if err := policy.Validate(); err != nil {
		return nil, fmt.Errorf("dispatcher policy: %w", err)
	}
	return &Dispatcher{store: store, client: client, sleep: sleep, now: now, policy: policy}, nil
}

// Publish sends e to every matching subscription and returns how many subscriptions it was sent to.
func (d *Dispatcher) Publish(ctx context.Context, e Event) (int, error) {
	if err := e.Validate(); err != nil {
		return 0, fmt.Errorf("publish: %w", err)
	}
	payload, err := e.Payload()
	if err != nil {
		return 0, fmt.Errorf("publish: encode event: %w", err)
	}
	subs := d.store.Matching(e)
	for _, sub := range subs {
		d.store.Record(d.deliver(ctx, sub, e.ID, payload))
	}
	return len(subs), nil
}

// deliver posts payload to sub. Network errors, 429 and 5xx answers are retried with backoff; other answers are final.
func (d *Dispatcher) deliver(ctx context.Context, sub Subscription, eventID string, payload []byte) Delivery {
	rec := Delivery{EventID: eventID, SubscriptionID: sub.ID, MerchantID: sub.MerchantID, Status: Failed}
	for attempt := 1; attempt <= d.policy.MaxAttempts; attempt++ {
		rec.Attempts = attempt
		status, err := d.post(ctx, sub, payload)
		rec.LastStatusCode, rec.LastError = status, describe(status, err)
		if err == nil && status/100 == 2 {
			rec.Status, rec.LastError = Delivered, ""
			return rec
		}
		if !retryable(status, err) || attempt == d.policy.MaxAttempts {
			return rec
		}
		wait := Backoff(attempt, d.policy.BaseDelay, d.policy.MaxDelay)
		if err := d.sleep(ctx, wait); err != nil {
			rec.LastError = fmt.Sprintf("stopped before retry: %v", err)
			return rec
		}
	}
	return rec
}

// post sends one signed attempt and returns the subscriber's status.
func (d *Dispatcher) post(ctx context.Context, sub Subscription, payload []byte) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sub.URL, bytes.NewReader(payload))
	if err != nil {
		return 0, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(SignatureHeader, Sign(sub.Secret, d.now(), payload))
	resp, err := d.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxDrainBytes))
	return resp.StatusCode, nil
}

// retryable reports whether an answer or an error is worth another attempt.
func retryable(status int, err error) bool {
	return err != nil || status == http.StatusTooManyRequests || status >= http.StatusInternalServerError
}

// describe summarises one attempt for the delivery record. A 2xx answer has no error to report.
func describe(status int, err error) string {
	if err != nil {
		return err.Error()
	}
	if status/100 == 2 {
		return ""
	}
	return fmt.Sprintf("subscriber answered %d", status)
}
