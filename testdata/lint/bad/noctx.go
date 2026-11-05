// lintselftest: path=internal/platform/fxnoctx/x.go
// lintselftest: want=noctx|NewRequestWithContext

// Package fxnoctx is a lint fixture.
package fxnoctx

import "net/http"

// New builds a request without a context.
func New(u string) (*http.Request, error) { return http.NewRequest(http.MethodGet, u, nil) }
