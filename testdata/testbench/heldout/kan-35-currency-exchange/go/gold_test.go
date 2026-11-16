package exchange_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	exchange "benchcase/kan-35-currency-exchange"
)

type pairRates map[string]float64

func (p pairRates) Rate(from, to string) (float64, bool) {
	r, ok := p[from+"/"+to]
	return r, ok
}

func post(h http.Handler, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/exchange", strings.NewReader(body)))
	return rec
}

func convertBody(from, to string, amount float64) string {
	b, _ := json.Marshal(map[string]any{"from": from, "to": to, "amount": amount})
	return string(b)
}

// converted asserts a 200 reply and returns its converted string.
func converted(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
	var got struct {
		Converted string `json:"converted"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("body %q is not JSON: %v", rec.Body.String(), err)
	}
	return got.Converted
}

// AC1: EUR 100 at EUR/USD 1.0850 converts to USD "108.5000".
func TestCurrencyConversion(t *testing.T) {
	h := exchange.Handler(pairRates{"EUR/USD": 1.0850})
	if got := converted(t, post(h, convertBody("EUR", "USD", 100))); got != "108.5000" {
		t.Fatalf("converted = %q, want %q", got, "108.5000")
	}
}

// AC2: the result has exactly 4 decimal places, and ties round to even.
func TestPrecisionRounding(t *testing.T) {
	t.Run("tie rounds to even", func(t *testing.T) {
		h := exchange.Handler(pairRates{"EUR/USD": 1})
		if got := converted(t, post(h, convertBody("EUR", "USD", 0.03125))); got != "0.0312" {
			t.Fatalf("0.03125 rounded: converted = %q, want %q", got, "0.0312")
		}
	})
	t.Run("always four decimal places", func(t *testing.T) {
		for _, amount := range []float64{0.01, 0.1, 1, 3.33333, 1234.56789} {
			for _, rate := range []float64{0.5, 1.0850, 0.333333, 7.7} {
				h := exchange.Handler(pairRates{"EUR/USD": rate})
				got := converted(t, post(h, convertBody("EUR", "USD", amount)))
				_, frac, found := strings.Cut(got, ".")
				if !found || len(frac) != 4 {
					t.Errorf("amount %v at rate %v: converted = %q, want exactly 4 decimal places", amount, rate, got)
				}
			}
		}
	})
}

// AC3: an unsupported currency code is refused with 400.
func TestUnsupportedCurrencyRejected(t *testing.T) {
	h := exchange.Handler(pairRates{"EUR/USD": 1.0850})
	for _, body := range []string{convertBody("XYZ", "USD", 100), convertBody("EUR", "XYZ", 100)} {
		if rec := post(h, body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", body, rec.Code)
		}
	}
}
