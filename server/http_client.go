package server

import (
	"net/http"
	"time"
)

// Authentication and API endpoints must answer directly. In particular, 307/308
// redirects can replay a login password or document body to a different host.
func directHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}
