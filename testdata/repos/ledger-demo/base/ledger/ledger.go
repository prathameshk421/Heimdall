package ledger

// Ledger posts each refund once, keyed by refund_ref, and keeps a balance per currency. Refunds reduce the balance.
type Ledger struct {
	posted  map[string]bool
	balance map[string]int64
}

// New returns an empty ledger.
func New() *Ledger {
	return &Ledger{posted: make(map[string]bool), balance: make(map[string]int64)}
}

// Post records r and reports whether it was new. A refund already posted under the same refund_ref is ignored.
func (l *Ledger) Post(r RefundResponse) bool {
	if l.posted[r.RefundRef] {
		return false
	}
	l.posted[r.RefundRef] = true
	l.balance[r.Currency] -= r.Amount
	return true
}

// Balance returns the balance in currency, in its smallest unit.
func (l *Ledger) Balance(currency string) int64 {
	return l.balance[currency]
}
