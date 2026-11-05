# heimdall-ledger-demo

The ledger consumer for the payments demo. It reads refund responses from the payments service and posts each
refund once, keyed by `refund_ref`, into a balance per currency.

This module has no dependency on the payments service. The two repositories are linked only by the refund response
contract, which the reviewers of both repositories know about (see the seed memory in the payments repository).

## Tests

    go vet ./... && go test ./...
