//go:build !ctxferry_test

package app

func syncEndpoints(remote string) (string, string) { return "https://api.github.com", remote }
