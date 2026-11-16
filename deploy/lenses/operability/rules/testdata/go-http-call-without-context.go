package fixture

import "net/http"

// fetch reads a page without a deadline.
func fetch() (*http.Response, error) {
	// ruleid: go-http-call-without-context
	return http.Get("https://example.invalid/status")
}
