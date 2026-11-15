# Web Form Fixture

A lightweight web application with a single HTML page serving a feedback form and accepting POST submissions.
Demonstrates UI test profiles and non-Go web server integration.

## Profile

Configured in `.heimdall/profile.yaml` with both API test kinds and browser UI preview/test support.

## Tests

- Known Green: `tests/test_web.py` (verifies GET `/` renders the form and valid POST `/submit` succeeds).
- Known Red: `tests/test_red.py` (verifies invalid email rejection with 400 Bad Request; fails on `base`, passes on branch `form-validation`).
