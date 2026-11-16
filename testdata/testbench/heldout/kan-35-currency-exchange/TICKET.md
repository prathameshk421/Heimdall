# KAN-35: Multi-Currency Exchange Calculation

## Description
Provide currency exchange calculation supporting major currency pairs with 4 decimal places rounding.

## Acceptance Criteria
- **AC1:** `POST /exchange` with `from`, `to` and `amount` returns 200 OK with `converted` set to amount times the rate for the pair, as a JSON string (for example EUR 100 at EUR/USD 1.0850 gives `"108.5000"`).
- **AC2:** The converted amount has exactly 4 decimal places, rounded half to even (bankers rounding): 0.03125 gives `"0.0312"`.
- **AC3:** A `from` or `to` code outside the supported set (EUR, USD, GBP, JPY, CHF) returns 400 Bad Request.
