package fixture

import (
	"context"
	"net/http"
)

// fetch reads a page with the deadline of the request.
func fetch(ctx context.Context) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://example.invalid/status", nil)
	if err != nil {
		return nil, err
	}
	return http.DefaultClient.Do(request)
}
