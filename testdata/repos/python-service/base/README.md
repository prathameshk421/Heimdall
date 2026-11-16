# Python Service Fixture

A Python service managing database-backed accounts and fund transfers.
Demonstrates non-Go service support and database service dependencies in Heimdall runner.

## Profile

Configured in `.heimdall/profile.yaml` with Python 3.11 image and Postgres dependency.

## Tests

- Known Green: `tests/test_service.py` (verifies account creation and standard transfers).
- Known Red: `tests/test_red.py` (checks refusal of overdraft and negative transfer amounts; fails on `base`, passes on branch `fix-overdraft`).
