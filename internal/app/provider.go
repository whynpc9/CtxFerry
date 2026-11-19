package app

import (
	"context"
	"errors"
	"time"

	"github.com/whynpc9/CtxFerry/internal/provider"
	"github.com/whynpc9/CtxFerry/internal/store"
)

type ProviderLoginResult struct {
	Profile        provider.Profile              `json:"profile"`
	Authentication provider.Authentication       `json:"authentication"`
	Capabilities   []provider.MethodCapabilities `json:"capabilities"`
}

func (a App) providerAdapter(p provider.Profile) (provider.Adapter, []provider.MethodCapabilities, error) {
	adapter := a.Providers.Adapters[p.Provider]
	if adapter == nil {
		return nil, nil, Fail("provider-unsupported", "no adapter is configured for the selected provider")
	}
	capabilities := adapter.Capabilities()
	seen := map[string]bool{}
	for _, method := range capabilities {
		if !resourceToken.MatchString(method.Method) || seen[method.Method] {
			return nil, nil, Fail("provider-unsupported", "provider did not report distinct authentication methods")
		}
		seen[method.Method] = true
	}
	if !seen[p.Method] {
		return nil, capabilities, Fail("provider-unsupported", "provider does not support the selected authentication method")
	}
	return adapter, capabilities, nil
}

func (a App) secureCredentials(ctx context.Context) (provider.CredentialStore, error) {
	credentials := a.Providers.Credentials
	if credentials == nil || credentials.Available(ctx) != nil {
		return nil, Fail("credential-storage-unavailable", "secure device credential storage is unavailable")
	}
	return credentials, nil
}

func providerAuthentication(ctx context.Context, adapter provider.Adapter, p provider.Profile, session []byte) (provider.Authentication, error) {
	auth, err := adapter.Authentication(ctx, p, session)
	if errors.Is(err, provider.ErrAuthenticationRequired) {
		return provider.Authentication{State: provider.SignedOut}, nil
	}
	if err != nil {
		return provider.Authentication{}, Fail("secret-unavailable", "provider authentication state could not be checked")
	}
	switch auth.State {
	case provider.Authenticated:
		if auth.ExpiresAt != nil && !auth.ExpiresAt.After(time.Now()) {
			auth.State = provider.Expired
		}
	case provider.SignedOut, provider.Locked, provider.Expired:
	default:
		return provider.Authentication{}, Fail("secret-unavailable", "provider returned an unknown authentication state")
	}
	if auth.ExpiresAt != nil {
		stamp := auth.ExpiresAt.UTC()
		if stamp.Year() < 1 || stamp.Year() > 9999 {
			return provider.Authentication{}, Fail("secret-unavailable", "provider returned an invalid session expiry")
		}
		auth.ExpiresAt = &stamp
	}
	return auth, nil
}

func (a App) LoginProvider(ctx context.Context, p provider.Profile) (ProviderLoginResult, error) {
	out := ProviderLoginResult{Profile: p}
	if !validProviderProfile(p) {
		return out, Fail("usage", "specify a provider, profile and authentication method")
	}
	db, err := store.Open(ctx, a.DataDir, false)
	if err != nil {
		return out, err
	}
	defer db.Close()
	key := provider.SessionKey{Profile: p}
	if err := db.QueryRowContext(ctx, "SELECT workspace.id, device.id FROM workspace CROSS JOIN device").Scan(&key.WorkspaceID, &key.DeviceID); err != nil {
		return out, err
	}
	credentials, err := a.secureCredentials(ctx)
	if err != nil {
		return out, err
	}
	adapter, capabilities, err := a.providerAdapter(p)
	if err != nil {
		return out, err
	}
	out.Capabilities = capabilities
	session, err := adapter.Login(ctx, p)
	defer clear(session)
	if err != nil || len(session) == 0 {
		return out, Fail("authentication-required", "provider login did not establish a session")
	}
	out.Authentication, err = providerAuthentication(ctx, adapter, key.Profile, session)
	if err != nil {
		return out, err
	}
	if out.Authentication.State != provider.Authenticated {
		return out, Fail("authentication-required", "provider did not confirm a usable session")
	}
	if credentials.Save(ctx, key, session) != nil {
		return out, Fail("credential-storage-unavailable", "could not save the session in secure device credential storage")
	}
	return out, nil
}
