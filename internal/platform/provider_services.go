package platform

import (
	"github.com/whynpc9/CtxFerry/internal/provider"
	"github.com/whynpc9/CtxFerry/internal/provider/bitwarden"
	"github.com/whynpc9/CtxFerry/internal/provider/infisical"
)

// ProviderServices configures the same device-local adapters for both clients.
func ProviderServices() provider.Services {
	return provider.Services{
		Adapters: map[string]provider.Adapter{
			bitwarden.Name: bitwarden.New(),
			infisical.Name: infisical.New(),
		},
		Credentials: SystemCredentials(),
		InjectedSession: func(key provider.SessionKey) []byte {
			if key.Profile.Provider == bitwarden.Name {
				return bitwarden.RuntimeSession.Load(key)
			}
			return infisical.InjectedSession(key)
		},
		HasInjectedSession: func(key provider.SessionKey) bool {
			return bitwarden.RuntimeSession.Present(key) || infisical.HasInjectedSession(key)
		},
	}
}
