package refunds

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/aarush323/heimdall-fixture-pay/internal/payments"
)

// PaymentSource finds a payment by id. The payments store satisfies it.
type PaymentSource interface {
	Get(id string) (payments.Payment, bool)
}

// Request is a refund request as the API passes it on.
type Request struct {
	MerchantID     string
	PaymentID      string
	Amount         int64
	IdempotencyKey string
}

// Service records refunds. A mutex serialises the calls, so the cap check and the write are one step.
type Service struct {
	mu       sync.Mutex
	payments PaymentSource
	store    *store
	now      func() time.Time
	newID    func() string
}

// NewService returns a Service that reads payments from source. now and newID are injected for tests.
func NewService(source PaymentSource, now func() time.Time, newID func() string) *Service {
	return &Service{payments: source, store: newStore(), now: now, newID: newID}
}

// Create records a refund of req.Amount from req.PaymentID. A repeated request with the same Idempotency-Key from
// the same merchant returns the original refund, with replayed set to true.
func (s *Service) Create(req Request) (Refund, bool, error) {
	if err := validate(req); err != nil {
		return Refund{}, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	key := idempotencyKey{merchantID: req.MerchantID, key: req.IdempotencyKey}
	if original, ok := s.store.byIdempotencyKey(key); ok {
		return original, true, nil
	}
	pay, ok := s.payments.Get(req.PaymentID)
	if !ok || pay.MerchantID != req.MerchantID {
		return Refund{}, false, ErrPaymentNotFound
	}
	if req.Amount > pay.Amount {
		return Refund{}, false, fmt.Errorf("%w: %d is more than the payment's %d", ErrExceedsPayment, req.Amount, pay.Amount)
	}
	r := Refund{
		ID:         s.newID(),
		PaymentID:  pay.ID,
		MerchantID: req.MerchantID,
		Amount:     req.Amount,
		Currency:   pay.Currency,
		CreatedAt:  s.now().UTC(),
	}
	s.store.put(r, key)
	return r, false, nil
}

// Get returns the refund with id.
func (s *Service) Get(id string) (Refund, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.store.get(id)
	if !ok {
		return Refund{}, ErrNotFound
	}
	return r, nil
}

// validate reports every field rule the request breaks, all wrapped in ErrInvalid.
func validate(req Request) error {
	var errs []error
	if req.MerchantID == "" {
		errs = append(errs, errors.New("merchant: X-Merchant-ID is empty"))
	}
	if req.PaymentID == "" {
		errs = append(errs, errors.New("payment_id: is empty"))
	}
	if req.Amount < 1 {
		errs = append(errs, fmt.Errorf("amount: %d is below 1", req.Amount))
	}
	if req.IdempotencyKey == "" {
		errs = append(errs, errors.New("idempotency key: the Idempotency-Key header is empty"))
	}
	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("%w: %w", ErrInvalid, errors.Join(errs...))
}
