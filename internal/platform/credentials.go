package platform

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/whynpc9/CtxFerry/internal/provider"
	"github.com/zalando/go-keyring"
)

const providerService = "ctxferry.provider-session"

var errCredentialStore = errors.New("system credential store unavailable")

// SystemCredentials uses the current OS user's credential store. It has no
// file backend. Each workspace, device and provider profile has its own entry.
func SystemCredentials() provider.CredentialStore {
	return systemCredentials{keyring.Get, keyring.Set}
}

type systemCredentials struct {
	get func(service, account string) (string, error)
	set func(service, account, secret string) error
}

func credentialAccount(key provider.SessionKey) string {
	scope := strings.Join([]string{key.WorkspaceID, key.DeviceID, key.Profile.Provider, key.Profile.Name, key.Profile.Method}, "\x00")
	sum := sha256.Sum256([]byte(scope))
	return hex.EncodeToString(sum[:])
}

func (s systemCredentials) Available(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// A successful search with no entry still establishes backend access.
	// No probe credential is written.
	_, err := s.get(providerService, "availability-probe")
	if err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return errCredentialStore
	}
	return nil
}

func (s systemCredentials) Load(ctx context.Context, key provider.SessionKey) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	session, err := s.get(providerService, credentialAccount(key))
	if errors.Is(err, keyring.ErrNotFound) {
		return nil, provider.ErrNoSession
	}
	if err != nil {
		return nil, errCredentialStore
	}
	return []byte(session), nil
}

func (s systemCredentials) Save(ctx context.Context, key provider.SessionKey, session []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(session) == 0 || s.set(providerService, credentialAccount(key), string(session)) != nil {
		return errCredentialStore
	}
	// Verify persistence even when the OS backend delegates to an interactive
	// helper whose exit code may not report a failed write.
	saved, err := s.Load(ctx, key)
	defer clear(saved)
	if err != nil || subtle.ConstantTimeCompare(saved, session) != 1 {
		return errCredentialStore
	}
	return nil
}
