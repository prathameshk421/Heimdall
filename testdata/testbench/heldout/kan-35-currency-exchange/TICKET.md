# KAN-35: Multi-Currency Exchange Calculation

## Description
Provide currency exchange calculation supporting major currency pairs with 4 decimal places rounding.

## Acceptance Criteria
- **AC1:** Convert amount from source currency to target currency given exchange rate.
- **AC2:** Converted amount is rounded to exactly 4 decimal places (bankers rounding).
- **AC3:** Unsupported or invalid currency code returns 400 Bad Request.
