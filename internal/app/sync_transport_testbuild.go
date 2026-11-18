//go:build ctxferry_test

package app

import (
	"net/url"
	"os"
)

// The executable scenario seam is absent from production builds.
func syncEndpoints(remote string) (string, string) {
	api := os.Getenv("CTXFERRY_TEST_API")
	u, err := url.Parse(api)
	if err == nil && u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "::1") {
		return api, os.Getenv("CTXFERRY_TEST_REMOTE")
	}
	return "https://api.github.com", remote
}
