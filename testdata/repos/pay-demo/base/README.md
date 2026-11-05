# heimdall-fixture-pay

A tiny payments service used as a hermetic fixture. It speaks HTTP, keeps everything in memory and depends on the Go standard library only.

## Run

    go run ./cmd/pay -addr :8080

## API

`api/openapi.yaml` describes the HTTP API. Every request names the calling merchant in the `X-Merchant-ID` header.

| Method | Path | Purpose |
|---|---|---|
| POST | /payments | create a payment for the calling merchant |

## Tests

    go vet ./... && go test ./...

The service is built through `app.New(app.Config)`, which takes an injectable clock and id generator so tests need no randomness.
