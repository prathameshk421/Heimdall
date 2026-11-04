package webhooks

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// fakeDoer answers each attempt with the next status in statuses, repeating the last one.
type fakeDoer struct {
	statuses []int
	calls    int
	sigs     []string
}

func (f *fakeDoer) Do(req *http.Request) (*http.Response, error) {
	idx := min(f.calls, len(f.statuses)-1)
	f.calls++
	f.sigs = append(f.sigs, req.Header.Get(SignatureHeader))
	return &http.Response{StatusCode: f.statuses[idx], Body: io.NopCloser(strings.NewReader(""))}, nil
}

// recorder collects the waits the dispatcher asked for and never really sleeps.
type recorder struct{ waits []time.Duration }

func (r *recorder) sleep(_ context.Context, d time.Duration) error {
	r.waits = append(r.waits, d)
	return nil
}

var policy = Policy{MaxAttempts: 3, BaseDelay: time.Second, MaxDelay: 10 * time.Second}

// newDispatch returns a dispatcher over one subscription of m-1 for payment.created.
func newDispatch(t *testing.T, doer Doer, sleep Sleeper) (*Dispatcher, *Store) {
	t.Helper()
	store := NewStore()
	sub, err := NewSubscription("sub_1", "m-1", "https://a.example/hooks", []EventType{PaymentCreated}, secret, t0)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Add(sub); err != nil {
		t.Fatal(err)
	}
	d, err := NewDispatcher(store, doer, sleep, func() time.Time { return t0 }, policy)
	if err != nil {
		t.Fatal(err)
	}
	return d, store
}

func paymentEvent() Event {
	return Event{ID: "evt_1", Type: PaymentCreated, MerchantID: "m-1", OccurredAt: t0, Data: []byte(`{"amount":5}`)}
}

func TestRetriesServerErrorsUntilDelivered(t *testing.T) {
	doer := &fakeDoer{statuses: []int{500, 503, 200}}
	rec := &recorder{}
	d, store := newDispatch(t, doer, rec.sleep)
	if _, err := d.Publish(context.Background(), paymentEvent()); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	got := store.Deliveries("m-1")
	if len(got) != 1 || got[0].Status != Delivered || got[0].Attempts != 3 {
		t.Fatalf("deliveries = %+v, want one delivered after 3 attempts", got)
	}
	if len(rec.waits) != 2 || rec.waits[0] != time.Second || rec.waits[1] != 2*time.Second {
		t.Errorf("waits = %v, want [1s 2s]", rec.waits)
	}
}

func TestClientErrorsAreFinal(t *testing.T) {
	doer := &fakeDoer{statuses: []int{400}}
	rec := &recorder{}
	d, store := newDispatch(t, doer, rec.sleep)
	if _, err := d.Publish(context.Background(), paymentEvent()); err != nil {
		t.Fatal(err)
	}
	got := store.Deliveries("m-1")
	if len(got) != 1 || got[0].Status != Failed || got[0].Attempts != 1 || len(rec.waits) != 0 {
		t.Errorf("deliveries = %+v waits %v, want one failed attempt and no retry", got, rec.waits)
	}
}

func TestGivesUpAfterMaxAttempts(t *testing.T) {
	doer := &fakeDoer{statuses: []int{500}}
	d, store := newDispatch(t, doer, (&recorder{}).sleep)
	if _, err := d.Publish(context.Background(), paymentEvent()); err != nil {
		t.Fatal(err)
	}
	got := store.Deliveries("m-1")
	if len(got) != 1 || got[0].Status != Failed || got[0].Attempts != 3 || got[0].LastStatusCode != 500 {
		t.Errorf("deliveries = %+v, want failed after 3 attempts with status 500", got)
	}
}

func TestEveryAttemptIsSigned(t *testing.T) {
	doer := &fakeDoer{statuses: []int{500, 200}}
	d, _ := newDispatch(t, doer, (&recorder{}).sleep)
	ev := paymentEvent()
	if _, err := d.Publish(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	payload, err := ev.Payload()
	if err != nil {
		t.Fatal(err)
	}
	for i, sig := range doer.sigs {
		if err := Verify(secret, sig, payload, t0, time.Minute); err != nil {
			t.Errorf("attempt %d signature %q does not verify: %v", i+1, sig, err)
		}
	}
}

func TestPublishReachesOnlyMatchingSubscriptions(t *testing.T) {
	doer := &fakeDoer{statuses: []int{200}}
	d, _ := newDispatch(t, doer, (&recorder{}).sleep)
	n, err := d.Publish(context.Background(), Event{ID: "evt_2", Type: RefundCreated, MerchantID: "m-1", OccurredAt: t0, Data: []byte(`{}`)})
	if err != nil || n != 0 || doer.calls != 0 {
		t.Errorf("Publish(refund to a payment-only subscriber) = %d, %v with %d calls; want 0 and no calls", n, err, doer.calls)
	}
}

func TestNewDispatcherValidatesThePolicy(t *testing.T) {
	_, err := NewDispatcher(NewStore(), &fakeDoer{statuses: []int{200}}, (&recorder{}).sleep, time.Now, Policy{})
	if err == nil || !strings.Contains(err.Error(), "max attempts") {
		t.Fatalf("NewDispatcher(zero policy) err = %v, want a max attempts problem", err)
	}
	if errors.Is(err, ErrNotFound) {
		t.Error("policy error was reported as a missing subscription")
	}
}

func TestCancelledContextStopsRetries(t *testing.T) {
	doer := &fakeDoer{statuses: []int{500}}
	cancelled := func(ctx context.Context, _ time.Duration) error {
		return ctx.Err()
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	d, store := newDispatch(t, doer, cancelled)
	if _, err := d.Publish(ctx, paymentEvent()); err != nil {
		t.Fatal(err)
	}
	got := store.Deliveries("m-1")
	if len(got) != 1 || got[0].Status != Failed || got[0].Attempts != 1 {
		t.Fatalf("deliveries = %+v, want one failed attempt when the context is cancelled", got)
	}
	if !strings.Contains(got[0].LastError, "stopped before retry") {
		t.Errorf("LastError = %q, want the reason the retries stopped", got[0].LastError)
	}
}

func TestPolicyNeedsOrderedDelays(t *testing.T) {
	err := Policy{MaxAttempts: 2, BaseDelay: 5 * time.Second, MaxDelay: time.Second}.Validate()
	if err == nil || !strings.Contains(err.Error(), "max delay") {
		t.Fatalf("Validate(max below base) = %v, want a max delay problem", err)
	}
	if err := (Policy{MaxAttempts: 1, BaseDelay: time.Second}).Validate(); err == nil {
		t.Error("Validate(no max delay) accepted a policy whose max is below its base")
	}
}

func TestRecordedErrorIsEmptyOnSuccess(t *testing.T) {
	doer := &fakeDoer{statuses: []int{200}}
	d, store := newDispatch(t, doer, (&recorder{}).sleep)
	if _, err := d.Publish(context.Background(), paymentEvent()); err != nil {
		t.Fatal(err)
	}
	if got := store.Deliveries("m-1"); len(got) != 1 || got[0].LastError != "" || got[0].LastStatusCode != 200 {
		t.Errorf("deliveries = %+v, want a clean delivery with status 200", got)
	}
}
