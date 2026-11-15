# KAN-22: Health check endpoint

## Description
Provide a `GET /healthz` endpoint returning HTTP 200 and "OK" without querying the database.

## Acceptance Criteria
- **AC1:** `GET /healthz` returns HTTP 200 with content `OK` and does not perform database I/O.
- **AC2:** Any HTTP method other than GET (e.g. POST, PUT, DELETE) returns HTTP 405 Method Not Allowed.
