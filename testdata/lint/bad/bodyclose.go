// lintselftest: path=internal/platform/fxbodyclose/x.go
// lintselftest: want=bodyclose|body must be closed

// Package fxbodyclose is a lint fixture.
package fxbodyclose

import (
	"context"
	"net/http"
)

// Get never closes the response body.
func Get(ctx context.Context, c *http.Client, u string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	_ = resp.StatusCode
	return nil
}
