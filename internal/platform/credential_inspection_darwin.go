package platform

import (
	"context"
	"os/exec"
	"strings"

	"github.com/whynpc9/CtxFerry/internal/provider"
)

func (systemCredentials) InspectCredentials(ctx context.Context, keys []provider.SessionKey) (provider.CredentialEvidence, string, map[provider.SessionKey]provider.CredentialEvidence) {
	run := func(args ...string) (string, error) {
		cmd := exec.CommandContext(ctx, "/usr/bin/security", args...)
		cmd.Env = []string{"LC_ALL=C"}
		_, err := cmd.Output() // Discard attributes and retain diagnostics only for classification.
		if exit, ok := err.(*exec.ExitError); ok {
			return string(exit.Stderr), err
		}
		return "", err
	}
	msg, err := run("default-keychain")
	if err != nil {
		if strings.Contains(msg, "could not be found") {
			return provider.CredentialUnavailable, "No default macOS Keychain", nil
		}
		return provider.CredentialUnknown, "Cannot inspect the default macOS Keychain", nil
	}

	entries := make(map[provider.SessionKey]provider.CredentialEvidence, len(keys))
	for _, key := range keys {
		// With neither -g nor -w, security searches by attributes only.
		msg, err := run("find-generic-password", "-s", providerService, "-a", credentialAccount(key))
		switch {
		case err == nil:
			entries[key] = provider.CredentialPresent
		case strings.Contains(msg, "The specified item could not be found in the keychain"):
			entries[key] = provider.CredentialAbsent
		}
	}
	return provider.CredentialAvailable, "Default macOS Keychain exists; item access and unlock are unverified", entries
}
