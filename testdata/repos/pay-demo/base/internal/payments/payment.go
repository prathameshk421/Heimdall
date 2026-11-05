// Package payments holds the payment rules: what a payment is and when a request to create one is valid.
package payments

import (
	"errors"
	"fmt"
	"regexp"
	"time"
)

// currencyCode is the alphabetic form the API accepts: three upper-case letters.
var currencyCode = regexp.MustCompile(`^[A-Z]{3}$`)

// Payment is a charge a merchant has taken. Amount is in the smallest unit of Currency (cents for USD).
type Payment struct {
	ID         string
	MerchantID string
	Amount     int64
	Currency   string
	CreatedAt  time.Time
}

// ValidateNew reports every rule a new payment breaks, so a caller sees all of them at once.
func ValidateNew(merchantID string, amount int64, currency string) error {
	var errs []error
	if merchantID == "" {
		errs = append(errs, errors.New("merchant: X-Merchant-ID is empty"))
	}
	if amount < 1 {
		errs = append(errs, fmt.Errorf("amount: %d is below 1", amount))
	}
	if !currencyCode.MatchString(currency) {
		errs = append(errs, fmt.Errorf("currency: %q is not three upper-case letters", currency))
	}
	return errors.Join(errs...)
}
