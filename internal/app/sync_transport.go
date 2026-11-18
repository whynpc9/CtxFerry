//go:build !ctxferry_test

package app

func syncEndpoints(remote string) (string, string) { return "https://api.github.com", remote }

func newSyncBackend() syncBackend { return gitSyncBackend{} }

func syncReadResult(args []string, out string) string { return out }
